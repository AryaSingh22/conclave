package keeper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	escrowtypes "github.com/AryaSingh22/conclave/x/escrow/types"
	"github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// coinSettleLeg builds a coin leg via a struct literal (not NewInt64Coin) so
// the checker can be tested against negative/zero amounts and short denoms it
// must REJECT rather than panic on.
func coinSettleLeg(from, to, denom string, amt int64) types.SettleLeg {
	c := sdk.Coin{Denom: denom, Amount: math.NewInt(amt)}
	return types.SettleLeg{FromSigil: from, ToSigil: to, Asset: &types.SettleLeg_Coin{Coin: &c}}
}

// TestCheckConservation_Units pins the SPEC §6.4 rule-2 checker against the
// adversarial graphs the fuzz target names (cycles, self-trades, duplicate
// legs) plus structural violations.
func TestCheckConservation_Units(t *testing.T) {
	svc := func(from, to string, units int64) types.SettleLeg {
		return types.SettleLeg{FromSigil: from, ToSigil: to, Asset: &types.SettleLeg_Svc{Svc: &veiltypes.ServiceVoucher{Schema: "cmpt", Units: math.NewInt(units)}}}
	}
	cases := []struct {
		name string
		legs []types.SettleLeg
		err  error
	}{
		{"balanced 3-cycle", []types.SettleLeg{
			coinSettleLeg("a", "b", "x", 10), coinSettleLeg("b", "c", "x", 10), coinSettleLeg("c", "a", "x", 10),
		}, nil},
		{"balanced two-asset", []types.SettleLeg{
			coinSettleLeg("a", "b", "x", 5), coinSettleLeg("b", "a", "x", 5), svc("a", "b", 3), svc("b", "a", 3),
		}, nil},
		{"duplicate legs balanced", []types.SettleLeg{
			coinSettleLeg("a", "b", "x", 10), coinSettleLeg("b", "a", "x", 10),
			coinSettleLeg("a", "b", "x", 10), coinSettleLeg("b", "a", "x", 10),
		}, nil},
		{"self-trade rejected", []types.SettleLeg{coinSettleLeg("a", "a", "x", 10)}, types.ErrInvalidLeg},
		{"empty endpoint rejected", []types.SettleLeg{coinSettleLeg("", "b", "x", 10)}, types.ErrInvalidLeg},
		{"non-positive amount rejected", []types.SettleLeg{coinSettleLeg("a", "b", "x", 0)}, types.ErrInvalidLeg},
		{"condition rejected", []types.SettleLeg{{
			FromSigil: "a", ToSigil: "b",
			Asset: &types.SettleLeg_Coin{Coin: ptrCoin(sdk.Coin{Denom: "x", Amount: math.NewInt(10)})},
			Condition: &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_Hashlock{
				Hashlock: &escrowtypes.HashlockCondition{},
			}},
		}}, types.ErrConditionsNotEnabled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CheckConservation([]types.Match{{Legs: tc.legs}})
			if tc.err == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.err)
			}
		})
	}
}

// TestCheckConservation_Fuzz drives random graphs (the standalone, exhaustively
// fuzzed component): the checker never panics, is deterministic across two
// runs, and whenever it accepts, the per-asset net is independently zero with
// no self-trades or non-positive amounts (Decision/Guard 2 — the pure half).
func TestCheckConservation_Fuzz(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		sigils := []string{"s0", "s1", "s2", "s3"}
		denoms := []string{"da", "db", "dc"}
		n := rapid.IntRange(0, 8).Draw(rt, "n")
		legs := make([]types.SettleLeg, 0, n)
		for i := 0; i < n; i++ {
			from := rapid.SampledFrom(sigils).Draw(rt, fmt.Sprintf("from%d", i))
			to := rapid.SampledFrom(sigils).Draw(rt, fmt.Sprintf("to%d", i))
			denom := rapid.SampledFrom(denoms).Draw(rt, fmt.Sprintf("denom%d", i))
			amt := rapid.Int64Range(-3, 50).Draw(rt, fmt.Sprintf("amt%d", i))
			legs = append(legs, coinSettleLeg(from, to, denom, amt))
		}
		match := []types.Match{{Legs: legs}}

		flows1, err1 := CheckConservation(match)
		flows2, err2 := CheckConservation(match)
		// determinism: identical accept/reject across runs.
		require.Equal(rt, err1 == nil, err2 == nil)

		if err1 != nil {
			return
		}
		// when accepted, independently confirm the structural rules (the checker's
		// teeth) hold for every leg, and the returned flows balance to zero per
		// asset across all parties.
		require.Equal(rt, len(flows1), len(flows2))
		for _, leg := range legs {
			require.NotEqual(rt, leg.FromSigil, leg.ToSigil, "accepted self-trade")
			require.True(rt, leg.GetCoin().Amount.IsPositive(), "accepted non-positive amount")
		}
		assetNet := map[string]math.Int{}
		for _, row := range flows1 {
			for asset, v := range row {
				cur, ok := assetNet[asset]
				if !ok {
					cur = math.ZeroInt()
				}
				assetNet[asset] = cur.Add(v)
			}
		}
		for asset, v := range assetNet {
			require.True(rt, v.IsZero(), "asset %s nets %s", asset, v)
		}
	})
}

// TestConservation_ExecutionPreservesSupply proves the property the standalone
// checker guards: applying any conserving coin graph through the module escrow
// account neither creates nor destroys supply, and the module nets zero — over
// random cycles (the adversarial shape §6.4 names), across many seeds.
func TestConservation_ExecutionPreservesSupply(t *testing.T) {
	f := setupKeeper(t)
	// pre-register a fixed, generously funded party set.
	parties := make([]sdk.AccAddress, 5)
	sigils := make([]string, 5)
	for i := range parties {
		parties[i] = randAddr()
		sigils[i] = f.registerSigil(t, parties[i])
		f.bank.fund(parties[i].String(), sdk.NewCoins(sdk.NewInt64Coin(usdcDenom, 1_000_000_000)))
	}
	resolve := func(sigil string) (sdk.AccAddress, error) {
		return f.k.sigilKeeper.ControllerAddress(f.ctx, 0, sigil)
	}

	rapid.Check(t, func(rt *rapid.T) {
		// build a random cycle over a random subset (size ≥ 2) with one amount —
		// conserves uusdc by construction (each party -amt +amt).
		k := rapid.IntRange(2, 5).Draw(rt, "cycleLen")
		perm := rapid.Permutation(sigils).Draw(rt, "perm")[:k]
		amt := rapid.Int64Range(1, 1_000_000).Draw(rt, "amt")

		var legs []types.SettleLeg
		for i := 0; i < k; i++ {
			legs = append(legs, coinSettleLeg(perm[i], perm[(i+1)%k], usdcDenom, amt))
		}
		match := types.Match{Legs: legs}
		_, err := CheckConservation([]types.Match{match})
		require.NoError(rt, err, "random cycle must conserve")

		supplyBefore := f.bank.total(usdcDenom)
		moduleBefore := f.bank.balanceOf(sessionModuleAddr).AmountOf(usdcDenom)
		for li := range legs {
			require.NoError(rt, f.k.applyLeg(f.ctx, 0, legs[li], resolve))
		}
		require.Equal(rt, supplyBefore, f.bank.total(usdcDenom), "no supply created/destroyed")
		require.Equal(rt, moduleBefore, f.bank.balanceOf(sessionModuleAddr).AmountOf(usdcDenom), "module nets zero")
	})
}

func ptrCoin(c sdk.Coin) *sdk.Coin { return &c }

// countEscrows counts the escrow stubs in the session store.
func countEscrows(t testingT, f *fixture) int {
	t.Helper()
	store := f.k.storeService.OpenKVStore(f.ctx)
	it, err := store.Iterator(types.EscrowKeyPrefix, types.PrefixEnd(types.EscrowKeyPrefix))
	require.NoError(t, err)
	defer it.Close()
	n := 0
	for ; it.Valid(); it.Next() {
		n++
	}
	return n
}

// TestExecutionInvariant_Svc proves the svc/{schema} ledger invariant —
// AUTHORIZED MINT, not value conservation (D-048): concluding the cycle raises
// svc supply by exactly the Units the giver's intent authorized, and a svc leg
// with no authorizing give-leg is rejected at validation (never minted).
func TestExecutionInvariant_Svc(t *testing.T) {
	f := setupKeeper(t)
	c := f.setupCycle(t)
	svcBefore := f.bank.total(svcDenom)

	require.NoError(t, f.k.Conclude(f.ctx, c.sessionID, c.matches()))
	// carol's give leg authorized 10 CMPT; exactly 10 svc minted to bob, supply +10.
	require.Equal(t, svcBefore.AddRaw(10), f.bank.total(svcDenom), "svc supply rises by the authorized Units")
	require.Equal(t, math.NewInt(10), f.bank.balanceOf(c.bob.String()).AmountOf(svcDenom))

	// unauthorized mint: alice (give=uusdc, want=svc) is made the svc GIVER —
	// no give-leg authorizes it, so validation rejects and nothing is minted.
	f2 := setupKeeper(t)
	s := f2.setupSwap(t, "", "")
	solver := randAddr()
	f2.registerSolver(t, solver, minSolverBond())
	svcBefore2 := f2.bank.total(svcDenom)
	bad := s.match()
	bad.Legs[1] = types.SettleLeg{FromSigil: s.aliceSigil, ToSigil: s.bobSigil, Asset: &types.SettleLeg_Svc{Svc: &veiltypes.ServiceVoucher{Schema: "cmpt", Units: math.NewInt(10)}}}
	err := f2.propose(t, solver, s.sessionID, []types.Match{bad})
	require.ErrorIs(t, err, types.ErrInvalidSolution)
	require.Equal(t, svcBefore2, f2.bank.total(svcDenom), "no svc minted without an authorizing give-leg")
}

// TestExecutionInvariant_Data proves the data/{hash} invariant — AUTHORIZED
// CREATION, not conservation (D-048): an escrow stub is created iff a giver
// offered the data and a receiver wanted it; an unauthorized data leg is
// rejected and no escrow is created.
func TestExecutionInvariant_Data(t *testing.T) {
	f := setupKeeper(t)
	c := f.setupCycle(t)
	require.Equal(t, 0, countEscrows(t, f))

	require.NoError(t, f.k.Conclude(f.ctx, c.sessionID, c.matches()))
	require.Equal(t, 1, countEscrows(t, f), "bob offered D and alice wanted it → exactly one escrow stub")

	// unauthorized data leg: a swap (no data intents) with a data leg injected;
	// alice never offered data, so validation rejects and no escrow is created.
	f2 := setupKeeper(t)
	s := f2.setupSwap(t, "", "")
	solver := randAddr()
	f2.registerSolver(t, solver, minSolverBond())
	bad := s.match()
	bad.Legs = append(bad.Legs, types.SettleLeg{FromSigil: s.aliceSigil, ToSigil: s.bobSigil, Asset: &types.SettleLeg_Data{Data: &veiltypes.DataGrant{ContentHash: contentHash("D2")}}})
	err := f2.propose(t, solver, s.sessionID, []types.Match{bad})
	require.ErrorIs(t, err, types.ErrInvalidSolution)
	require.Equal(t, 0, countEscrows(t, f2), "no escrow created for an unauthorized data leg")
}
