package keeper

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/rand"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// pairSpec is one mutually-fillable uusdc↔svc swap with a controllable surplus:
// the uusdc giver pays svcUnits×pricePerUnit (exact, so price binding holds with
// an integer price), and the svc giver's floor sits surplusMargin below that, so
// the match leaves exactly surplusMargin uusdc of receive-excess for the skim.
type pairSpec struct {
	svcUnits      int64
	pricePerUnit  int64 // uusdc per svc unit
	surplusMargin int64 // uusdc the svc giver receives above its floor
}

type scenario struct {
	pairs       []pairSpec
	solverCount int // 0 → fallback path; 1 → single winner; 2 → tie-break path
}

// genScenario deterministically draws a settlement scenario from a seed: 1–3
// exact, matchable swaps and a solver count selecting the fallback, single-
// winner, or tie-break path.
func genScenario(rng *rand.Rand) scenario {
	n := 1 + rng.Intn(3)
	sc := scenario{solverCount: rng.Intn(3)}
	for i := 0; i < n; i++ {
		units := int64(1 + rng.Intn(20))
		price := int64(1+rng.Intn(20)) * 1_000_000
		full := units * price
		margin := int64(rng.Intn(int(full/2) + 1)) // 0..full/2, never below 0
		sc.pairs = append(sc.pairs, pairSpec{svcUnits: units, pricePerUnit: price, surplusMargin: margin})
	}
	return sc
}

// detAddr derives a deterministic account address from a seed and index, so two
// independent keeper instances run the SAME parties (unlike random keys).
func detAddr(seed int64, i int) sdk.AccAddress {
	b := make([]byte, 20)
	binary.BigEndian.PutUint64(b, uint64(seed))
	binary.BigEndian.PutUint32(b[12:], uint32(i))
	return sdk.AccAddress(b)
}

// runScenario applies a scenario to a fresh keeper (a simulated node) and returns
// a digest of the full settlement outcome — balances (legs), the fee pool (skim),
// session records (winner), and the event stream (receipts/skim/payout). Two
// instances given the same scenario must return the same digest.
func runScenario(t testingT, seed int64, sc scenario) string {
	t.Helper()
	f := setupKeeper(t)

	var addrs []sdk.AccAddress
	idx := 0
	var matches []types.Match

	for _, p := range sc.pairs {
		uusdcGive := p.svcUnits * p.pricePerUnit
		bobFloor := uusdcGive - p.surplusMargin

		alice := detAddr(seed, idx)
		idx++
		bob := detAddr(seed, idx)
		idx++
		addrs = append(addrs, alice, bob)

		aliceSigil := f.registerSigil(t, alice)
		bobSigil := f.registerSigil(t, bob)
		f.bank.fund(alice.String(), sdk.NewCoins(sdk.NewInt64Coin(usdcDenom, uusdcGive)))

		aliceID := f.submitOpen(t, alice, aliceSigil,
			[]veiltypes.IntentLeg{coinLeg(sdk.NewInt64Coin(usdcDenom, uusdcGive))},
			[]veiltypes.IntentLeg{svcLeg("cmpt", p.svcUnits)}, 100, "")
		bobID := f.submitOpen(t, bob, bobSigil,
			[]veiltypes.IntentLeg{svcLeg("cmpt", p.svcUnits)},
			[]veiltypes.IntentLeg{coinLeg(sdk.NewInt64Coin(usdcDenom, bobFloor))}, 100, "")

		usdc := sdk.NewInt64Coin(usdcDenom, uusdcGive)
		matches = append(matches, types.Match{
			IntentIds: []string{aliceID, bobID},
			Legs: []types.SettleLeg{
				{FromSigil: aliceSigil, ToSigil: bobSigil, Asset: &types.SettleLeg_Coin{Coin: &usdc}},
				{FromSigil: bobSigil, ToSigil: aliceSigil, Asset: &types.SettleLeg_Svc{Svc: &veiltypes.ServiceVoucher{Schema: "cmpt", Units: math.NewInt(p.svcUnits)}}},
			},
			ClearingPrices: []types.PricePoint{
				{Base: usdcDenom, Quote: usdcDenom, Price: math.LegacyOneDec()},
				{Base: svcDenom, Quote: usdcDenom, Price: math.LegacyNewDec(p.pricePerUnit)},
			},
		})
	}

	sessionID := f.openSession(t)

	for sNo := 0; sNo < sc.solverCount; sNo++ {
		solver := detAddr(seed, 1_000+sNo)
		addrs = append(addrs, solver)
		f.bank.fund(solver.String(), sdk.NewCoins(minSolverBond()))
		require.NoError(t, f.k.RegisterSolver(f.ctx, solver, sdk.NewCoins(minSolverBond())))
		solID := types.IDHex(contentHash(fmt.Sprintf("sol-%d-%d", seed, sNo)))
		// each solver proposes the same valid clearing → a tie when count==2.
		require.NoError(t, f.k.ProposeSolution(f.ctx, solver, sessionID, matches, solID))
	}

	f.processSessionAt(t, 5)
	return f.outcomeDigest(addrs)
}

// outcomeDigest hashes the settlement-visible state into a stable string:
// balances (the settled legs + skim), session records (the winner), and the
// emitted events (receipts, skim, payout). Map-iteration nondeterminism anywhere
// in the pipeline would change this digest on one node but not the other.
func (f *fixture) outcomeDigest(addrs []sdk.AccAddress) string {
	h := sha256.New()

	uniq := map[string]bool{}
	var addrStrs []string
	for _, a := range addrs {
		addrStrs = append(addrStrs, a.String())
	}
	addrStrs = append(addrStrs, sessionModuleAddr, veilModuleAddr, distrModuleAddr)
	for _, s := range addrStrs {
		if uniq[s] {
			continue
		}
		uniq[s] = true
	}
	sortedAddrs := make([]string, 0, len(uniq))
	for s := range uniq {
		sortedAddrs = append(sortedAddrs, s)
	}
	sort.Strings(sortedAddrs)
	for _, s := range sortedAddrs {
		fmt.Fprintf(h, "bal|%s|%s\n", s, f.bank.balanceOf(s).String())
	}

	_ = f.k.IterateSessions(f.ctx, func(ses types.Session) bool {
		fmt.Fprintf(h, "sess|%d|%d|%s\n", ses.Id, ses.State, ses.WinningSolutionId)
		return false
	})

	// Receipt store BYTES, not just the receipt_emitted events: x/receipt (0.9)
	// reads these 0x07/0x08 records for ICS-23 proofs, so a divergent-bytes /
	// identical-events bug must fail HERE, not surface as a 0.9 proof mismatch.
	// The iterator yields keys in sorted order — deterministic.
	store := f.k.storeService.OpenKVStore(f.ctx)
	for _, prefix := range [][]byte{types.ReceiptKeyPrefix, types.ReceiptByIDPrefix} {
		it, err := store.Iterator(prefix, types.PrefixEnd(prefix))
		if err != nil {
			continue
		}
		for ; it.Valid(); it.Next() {
			fmt.Fprintf(h, "rcpt|%x|%x\n", it.Key(), it.Value())
		}
		_ = it.Close()
	}

	for _, ev := range f.ctx.EventManager().Events() {
		attrs := make([]string, 0, len(ev.Attributes))
		for _, a := range ev.Attributes {
			attrs = append(attrs, a.Key+"="+a.Value)
		}
		sort.Strings(attrs)
		fmt.Fprintf(h, "ev|%s|%v\n", ev.Type, attrs)
	}

	return hex.EncodeToString(h.Sum(nil))
}

// TestDeterminism_ScoringAndFallback is the §10 row-0.8 HARD GATE: scoring and
// the fallback matcher must be byte-for-byte deterministic. Over 1000+ seeds,
// the same scenario is settled on TWO independent keeper instances; any
// divergence in winner / legs / receipts / skim is a consensus-fatal BLOCKER.
// Seeds span the fallback (0 solvers), single-winner (1), and tie-break (2)
// paths with randomized amounts and surpluses.
func TestDeterminism_ScoringAndFallback(t *testing.T) {
	const seeds = 1200
	for seed := int64(0); seed < seeds; seed++ {
		rng := rand.New(rand.NewSource(seed))
		sc := genScenario(rng)
		a := runScenario(t, seed, sc)
		b := runScenario(t, seed, sc)
		require.Equal(t, a, b, "seed %d: two keeper instances diverged (winner/legs/receipts/skim)", seed)
	}
}
