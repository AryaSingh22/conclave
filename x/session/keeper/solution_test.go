package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	escrowtypes "github.com/AryaSingh22/conclave/x/escrow/types"
	"github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// swap is a 2-party fixture: alice gives 100 uusdc for 10 CMPT, bob gives
// 10 CMPT for 100 uusdc, with per-party counterparty-class predicates.
type swap struct {
	aliceSigil, bobSigil string
	aliceID, bobID       string
	sessionID            uint64
}

func (f *fixture) setupSwap(t testingT, aliceCP, bobCP string) swap {
	t.Helper()
	alice, bob := randAddr(), randAddr()
	s := swap{aliceSigil: f.registerSigil(t, alice), bobSigil: f.registerSigil(t, bob)}
	f.bank.fund(alice.String(), sdk.NewCoins(sdk.NewInt64Coin(usdcDenom, 100_000_000)))
	usdc := sdk.NewInt64Coin(usdcDenom, 100_000_000)
	s.aliceID = f.submitOpen(t, alice, s.aliceSigil, []veiltypes.IntentLeg{coinLeg(usdc)}, []veiltypes.IntentLeg{svcLeg("cmpt", 10)}, 100, aliceCP)
	s.bobID = f.submitOpen(t, bob, s.bobSigil, []veiltypes.IntentLeg{svcLeg("cmpt", 10)}, []veiltypes.IntentLeg{coinLeg(usdc)}, 100, bobCP)
	s.sessionID = f.openSession(t)
	return s
}

func (s swap) match() types.Match {
	usdc := sdk.NewInt64Coin(usdcDenom, 100_000_000)
	return types.Match{
		IntentIds: []string{s.aliceID, s.bobID},
		Legs: []types.SettleLeg{
			{FromSigil: s.aliceSigil, ToSigil: s.bobSigil, Asset: &types.SettleLeg_Coin{Coin: &usdc}},
			{FromSigil: s.bobSigil, ToSigil: s.aliceSigil, Asset: &types.SettleLeg_Svc{Svc: &veiltypes.ServiceVoucher{Schema: "cmpt", Units: math.NewInt(10)}}},
		},
		ClearingPrices: []types.PricePoint{
			{Base: usdcDenom, Quote: usdcDenom, Price: math.LegacyOneDec()},
			{Base: svcDenom, Quote: usdcDenom, Price: math.LegacyNewDec(10_000_000)},
		},
	}
}

func (f *fixture) propose(t testingT, solver sdk.AccAddress, sessionID uint64, matches []types.Match) error {
	t.Helper()
	return f.k.ProposeSolution(f.ctx, solver, sessionID, matches, types.IDHex(contentHash("sol-"+solver.String())))
}

func TestProposeSolution_Valid(t *testing.T) {
	f := setupKeeper(t)
	c := f.setupCycle(t)
	solver := randAddr()
	f.registerSolver(t, solver, minSolverBond())

	require.NoError(t, f.propose(t, solver, c.sessionID, c.matches()))

	sols, err := f.k.SolutionsForSession(f.ctx, c.sessionID)
	require.NoError(t, err)
	require.Len(t, sols, 1)
	// a valid proposal never slashes.
	s, err := f.k.GetSolver(f.ctx, 0, solver)
	require.NoError(t, err)
	require.Equal(t, uint64(0), s.SlashedCount)
	require.Equal(t, minSolverBond().Amount, s.Bond.AmountOf(bondDenom))
}

func TestProposeSolution_Equivocation(t *testing.T) {
	f := setupKeeper(t)
	c := f.setupCycle(t)
	solver := randAddr()
	f.registerSolver(t, solver, minSolverBond())

	require.NoError(t, f.k.ProposeSolution(f.ctx, solver, c.sessionID, c.matches(), types.IDHex(contentHash("a"))))
	// second solution for the same session = equivocation → 20% slash.
	err := f.k.ProposeSolution(f.ctx, solver, c.sessionID, c.matches(), types.IDHex(contentHash("b")))
	require.ErrorIs(t, err, types.ErrEquivocation)

	s, err := f.k.GetSolver(f.ctx, 0, solver)
	require.NoError(t, err)
	require.Equal(t, uint64(1), s.SlashedCount)
	slash := minSolverBond().Amount.MulRaw(2000).QuoRaw(10_000)
	require.Equal(t, minSolverBond().Amount.Sub(slash), s.Bond.AmountOf(bondDenom))
}

func TestProposeSolution_InvalidSlashes(t *testing.T) {
	usdc := sdk.NewInt64Coin(usdcDenom, 100_000_000)
	tooMuch := sdk.NewInt64Coin(usdcDenom, 200_000_000)

	cases := []struct {
		name    string
		build   func(s swap) types.Match
		aliceCP string
		params  func(p *types.Params)
		err     error
	}{
		{
			name:  "rule1 intent not open",
			build: func(s swap) types.Match { m := s.match(); m.IntentIds = []string{s.aliceID, "00ff"}; return m },
			err:   types.ErrIntentNotOpen,
		},
		{
			name: "rule3 gives over offer",
			build: func(s swap) types.Match {
				m := s.match()
				m.Legs[0] = types.SettleLeg{FromSigil: s.aliceSigil, ToSigil: s.bobSigil, Asset: &types.SettleLeg_Coin{Coin: &tooMuch}}
				return m
			},
			err: types.ErrLimitViolation,
		},
		{
			name: "rule4 non-uniform price",
			build: func(s swap) types.Match {
				m := s.match()
				m.ClearingPrices = append(m.ClearingPrices, types.PricePoint{Base: usdcDenom, Quote: usdcDenom, Price: math.LegacyNewDec(2)})
				return m
			},
			err: types.ErrNonUniformPrice,
		},
		{
			name:    "rule5 counterparty class rejects",
			build:   func(s swap) types.Match { return s.match() },
			aliceCP: "reputation >= 100", // bob has reputation 0
			err:     types.ErrCounterpartyClass,
		},
		{
			name:    "rule5 malformed predicate fails closed",
			build:   func(s swap) types.Match { return s.match() },
			aliceCP: "reputation >= ", // malformed → reject (Decision A)
			err:     types.ErrCounterpartyClass,
		},
		{
			name:   "rule6 graph too large",
			build:  func(s swap) types.Match { return s.match() },
			params: func(p *types.Params) { p.MaxGraphLegs = 1 },
			err:    types.ErrGraphTooLarge,
		},
		{
			// ZK_DELIVERY stays disabled until 2.1b (FLAG A) — rejected at the
			// conservation gate.
			name: "rule6 ZK_DELIVERY disabled",
			build: func(s swap) types.Match {
				m := s.match()
				m.Legs[0].Condition = &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_ZkDelivery{ZkDelivery: &escrowtypes.ZkDeliveryCondition{}}}
				return m
			},
			err: types.ErrConditionsNotEnabled,
		},
		{
			// HASHLOCK/OPTIMISTIC are enabled (D-073) but only in an unambiguous
			// bilateral DataGrant+payment match. A HASHLOCK on a swap's coin leg
			// is not a DataGrant → ErrConditionUnsupportedMatch (recorded limit).
			name: "rule6 conditioned non-bilateral rejected",
			build: func(s swap) types.Match {
				m := s.match()
				m.Legs[0].Condition = &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_Hashlock{Hashlock: &escrowtypes.HashlockCondition{}}}
				return m
			},
			err: types.ErrConditionUnsupportedMatch,
		},
		{
			name: "rule2 leg to non-participant",
			build: func(s swap) types.Match {
				m := s.match()
				m.Legs[0] = types.SettleLeg{FromSigil: s.aliceSigil, ToSigil: "stranger", Asset: &types.SettleLeg_Coin{Coin: &usdc}}
				return m
			},
			err: types.ErrInvalidLeg,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := setupKeeper(t)
			if tc.params != nil {
				p := types.DefaultParams()
				tc.params(&p)
				require.NoError(t, f.k.SetParams(f.ctx, p))
			}
			s := f.setupSwap(t, tc.aliceCP, "")
			solver := randAddr()
			f.registerSolver(t, solver, minSolverBond())

			err := f.propose(t, solver, s.sessionID, []types.Match{tc.build(s)})
			require.ErrorIs(t, err, types.ErrInvalidSolution)
			require.ErrorIs(t, err, tc.err)

			// invalid solution → 5% slash, nothing stored.
			sv, gErr := f.k.GetSolver(f.ctx, 0, solver)
			require.NoError(t, gErr)
			require.Equal(t, uint64(1), sv.SlashedCount)
			expect := minSolverBond().Amount.MulRaw(500).QuoRaw(10_000)
			require.Equal(t, minSolverBond().Amount.Sub(expect), sv.Bond.AmountOf(bondDenom))
			sols, _ := f.k.SolutionsForSession(f.ctx, s.sessionID)
			require.Empty(t, sols)
		})
	}
}

// TestProposeSolution_DuplicateSigil pins Decision D: two intents from one
// sigil in a single match are rejected (per-intent limit attribution).
func TestProposeSolution_DuplicateSigil(t *testing.T) {
	f := setupKeeper(t)
	alice := randAddr()
	aliceSigil := f.registerSigil(t, alice)
	f.bank.fund(alice.String(), sdk.NewCoins(sdk.NewInt64Coin(usdcDenom, 200_000_000)))
	usdc := sdk.NewInt64Coin(usdcDenom, 100_000_000)
	id1 := f.submitOpen(t, alice, aliceSigil, []veiltypes.IntentLeg{coinLeg(usdc)}, []veiltypes.IntentLeg{svcLeg("cmpt", 10)}, 100, "")
	id2 := f.submitOpen(t, alice, aliceSigil, []veiltypes.IntentLeg{coinLeg(usdc)}, []veiltypes.IntentLeg{svcLeg("cmpt", 10)}, 100, "")
	sessionID := f.openSession(t)

	solver := randAddr()
	f.registerSolver(t, solver, minSolverBond())
	m := types.Match{IntentIds: []string{id1, id2}}
	err := f.propose(t, solver, sessionID, []types.Match{m})
	require.ErrorIs(t, err, types.ErrInvalidSolution)
	require.ErrorIs(t, err, types.ErrDuplicateSigil)
}

// TestProposeSolution_PriceBinding pins Correction 1: a solution whose legs
// satisfy every cap/floor but do NOT clear at its own stated prices is rejected
// (ErrPriceMismatch). Without this, "surplus" would be measured against prices
// the transfers need not respect.
func TestProposeSolution_PriceBinding(t *testing.T) {
	f := setupKeeper(t)
	s := f.setupSwap(t, "", "")
	solver := randAddr()
	f.registerSolver(t, solver, minSolverBond())

	m := s.match()
	// caps/floors stay satisfied (100M uusdc <-> 10 svc) but svc is mispriced at
	// 20M uusdc/unit instead of 10M, so the legs no longer clear at the stated
	// prices: alice nets +100M value, bob nets -100M.
	for i := range m.ClearingPrices {
		if m.ClearingPrices[i].Base == svcDenom {
			m.ClearingPrices[i].Price = math.LegacyNewDec(20_000_000)
		}
	}
	err := f.propose(t, solver, s.sessionID, []types.Match{m})
	require.ErrorIs(t, err, types.ErrInvalidSolution)
	require.ErrorIs(t, err, types.ErrPriceMismatch)

	// invalid solution → 5% slash, nothing stored.
	sv, _ := f.k.GetSolver(f.ctx, 0, solver)
	require.Equal(t, uint64(1), sv.SlashedCount)
	sols, _ := f.k.SolutionsForSession(f.ctx, s.sessionID)
	require.Empty(t, sols)
}

// TestProposeSolution_NotActiveOrNotAuction covers the non-slash rejections.
func TestProposeSolution_Gating(t *testing.T) {
	f := setupKeeper(t)
	c := f.setupCycle(t)

	// unregistered solver.
	require.ErrorIs(t, f.propose(t, randAddr(), c.sessionID, c.matches()), types.ErrSolverNotFound)

	// session not in AUCTION (unknown session id).
	solver := randAddr()
	f.registerSolver(t, solver, minSolverBond())
	require.ErrorIs(t, f.propose(t, solver, 999, c.matches()), types.ErrSessionNotFound)
}
