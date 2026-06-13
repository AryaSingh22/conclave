package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// processSessionAt advances the block height and runs the x/session EndBlock
// pipeline (advance → scoring → conclude/fallback → rollover → unbond sweep).
func (f *fixture) processSessionAt(t testingT, height int64) {
	t.Helper()
	f.ctx = f.ctx.WithBlockHeight(height)
	require.NoError(t, f.k.EndBlocker(f.ctx))
}

// sessionPoolUUSDC is the fee pool's uusdc balance (the session module account).
func (f *fixture) sessionPoolUUSDC() math.Int {
	return f.bank.balanceOf(sessionModuleAddr).AmountOf(usdcDenom)
}

// TestFullPathZeroSurplusCycle is STOP #1 build-condition #3: the canonical
// 3-party cycle driven through the FULL scoring → skim → Conclude path (propose
// a solution, then the EndBlock winner-selection + atomic settle) moves IDENTICAL
// balances to the Half-A direct-Conclude test (TestThreePartyCycleClears), with
// ZERO skim and the fee pool UNCHANGED. It proves the Half-B economic layer is a
// true superset that never perturbs the validated core when surplus is zero.
func TestFullPathZeroSurplusCycle(t *testing.T) {
	f := setupKeeper(t)
	c := f.setupCycle(t)

	solver := randAddr()
	f.registerSolver(t, solver, minSolverBond())
	solID := types.IDHex(contentHash("winning-sol"))
	require.NoError(t, f.k.ProposeSolution(f.ctx, solver, c.sessionID, c.matches(), solID))

	usdcSupplyBefore := f.bank.total(usdcDenom)
	require.True(t, f.sessionPoolUUSDC().IsZero(), "pool starts empty")

	f.processSessionAt(t, 5) // auction_end_height == 5

	// every intent CONCLUDED.
	for _, id := range []string{c.aliceID, c.bobID, c.carolID} {
		rec, err := f.veil.GetIntentRecord(f.ctx, 0, id)
		require.NoError(t, err)
		require.Equal(t, veiltypes.INTENT_STATE_CONCLUDED, rec.State, "intent %s", id)
	}

	// IDENTICAL balances to the direct-Conclude test.
	require.True(t, f.bank.balanceOf(c.alice.String()).AmountOf(usdcDenom).IsZero())
	require.Equal(t, math.NewInt(100_000_000), f.bank.balanceOf(c.carol.String()).AmountOf(usdcDenom))
	require.Equal(t, usdcSupplyBefore, f.bank.total(usdcDenom), "no uusdc created or destroyed")
	require.Equal(t, math.NewInt(10), f.bank.balanceOf(c.bob.String()).AmountOf(svcDenom))
	for _, addr := range []sdk.AccAddress{c.alice, c.bob, c.carol} {
		require.Equal(t, minBond().Amount, f.bank.balanceOf(addr.String()).AmountOf(bondDenom), "bond refunded to %s", addr)
	}

	// ZERO skim, fee pool UNCHANGED (still empty).
	require.True(t, f.sessionPoolUUSDC().IsZero(), "zero-surplus cycle takes no skim — pool unchanged")

	// session CLOSED with the winning solution recorded; solver paid nothing and
	// was not slashed.
	session, err := f.k.GetSession(f.ctx, 0, c.sessionID)
	require.NoError(t, err)
	require.Equal(t, types.SESSION_STATE_CLOSED, session.State)
	require.Equal(t, solID, session.WinningSolutionId)
	sv, err := f.k.GetSolver(f.ctx, 0, solver)
	require.NoError(t, err)
	require.Equal(t, uint64(0), sv.SlashedCount)
	require.True(t, sv.LifetimeSurplus.IsZero(), "no surplus → no fee accrued")

	// receipts were emitted for every party (one per intent).
	require.True(t, hasEvent(f.ctx, types.EventTypeReceiptEmitted))
}

// setupSurplusSwap builds a 2-party swap with a deliberate surplus: bob's
// want-floor (90 uusdc) is below the 100 uusdc he receives, so the match leaves
// bob 10 uusdc of realized numeraire surplus for the skim to bite.
type surplusSwap struct {
	aliceSigil, bobSigil string
	alice, bob           sdk.AccAddress
	aliceID, bobID       string
	sessionID            uint64
}

func (f *fixture) setupSurplusSwap(t testingT) surplusSwap {
	t.Helper()
	s := surplusSwap{alice: randAddr(), bob: randAddr()}
	s.aliceSigil = f.registerSigil(t, s.alice)
	s.bobSigil = f.registerSigil(t, s.bob)
	f.bank.fund(s.alice.String(), sdk.NewCoins(sdk.NewInt64Coin(usdcDenom, 100_000_000)))

	give100 := coinLeg(sdk.NewInt64Coin(usdcDenom, 100_000_000))
	want90 := coinLeg(sdk.NewInt64Coin(usdcDenom, 90_000_000)) // bob's floor < 100
	s.aliceID = f.submitOpen(t, s.alice, s.aliceSigil, []veiltypes.IntentLeg{give100}, []veiltypes.IntentLeg{svcLeg("cmpt", 10)}, 100, "")
	s.bobID = f.submitOpen(t, s.bob, s.bobSigil, []veiltypes.IntentLeg{svcLeg("cmpt", 10)}, []veiltypes.IntentLeg{want90}, 100, "")
	s.sessionID = f.openSession(t)
	return s
}

// match clears alice's full 100 uusdc against bob's 10 svc at svc=10 uusdc/unit.
// bob receives 100 uusdc (floor 90) → 10 uusdc surplus; alice's terms are exact.
func (s surplusSwap) match() types.Match {
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

// TestSkimAndPayoutWithSurplus exercises the full economic layer on a match with
// surplus: the 0.5% skim (default surplus_skim_bps=50) of bob's 10 uusdc
// receive-excess flows to the fee pool, bob keeps ≥ his 90 uusdc floor
// (principal untouched), and the winning solver is paid from the pool, capped at
// the pool balance.
func TestSkimAndPayoutWithSurplus(t *testing.T) {
	f := setupKeeper(t)
	s := f.setupSurplusSwap(t)

	solver := randAddr()
	f.registerSolver(t, solver, minSolverBond())
	solID := types.IDHex(contentHash("surplus-sol"))
	require.NoError(t, f.k.ProposeSolution(f.ctx, solver, s.sessionID, []types.Match{s.match()}, solID))

	solverUUSDCBefore := f.bank.balanceOf(solver.String()).AmountOf(usdcDenom)
	f.processSessionAt(t, 5)

	// skim = floor(50/10000 * 10_000_000) = 50_000 uusdc taken from bob's excess.
	const skim = int64(50_000)
	// bob keeps 100M received − skim, still far above his 90M floor (principal safe).
	require.Equal(t, math.NewInt(100_000_000-skim), f.bank.balanceOf(s.bob.String()).AmountOf(usdcDenom))
	require.True(t, f.bank.balanceOf(s.bob.String()).AmountOf(usdcDenom).GTE(math.NewInt(90_000_000)), "bob keeps ≥ floor")

	// solver fee (5% of 10M surplus = 500k) is capped at the 50k pool, so the
	// solver receives the whole skim and the pool ends empty.
	require.Equal(t, solverUUSDCBefore.AddRaw(skim), f.bank.balanceOf(solver.String()).AmountOf(usdcDenom))
	require.True(t, f.sessionPoolUUSDC().IsZero(), "pool drained to the solver (fee capped at balance)")

	// alice got her svc; both intents concluded; lifetime surplus accrued.
	require.Equal(t, math.NewInt(10), f.bank.balanceOf(s.alice.String()).AmountOf(svcDenom))
	sv, err := f.k.GetSolver(f.ctx, 0, solver)
	require.NoError(t, err)
	require.True(t, sv.LifetimeSurplus.Equal(math.LegacyNewDec(10_000_000)), "lifetime surplus = total surplus")
	require.True(t, hasEvent(f.ctx, types.EventTypeSkimTaken))
	require.True(t, hasEvent(f.ctx, types.EventTypeSolverPaid))
}

// TestWinnerTieBreakLowerID pins the SPEC §6.4 deterministic tie-break: two
// solvers propose equally-scoring solutions; the LOWER solution id wins.
func TestWinnerTieBreakLowerID(t *testing.T) {
	f := setupKeeper(t)
	c := f.setupCycle(t)

	solverA, solverB := randAddr(), randAddr()
	f.registerSolver(t, solverA, minSolverBond())
	f.registerSolver(t, solverB, minSolverBond())

	// two ids; the lexicographically lower one must win the tie (both score 0).
	idLow := "00" + types.IDHex(contentHash("low"))[2:]
	idHigh := "ff" + types.IDHex(contentHash("high"))[2:]
	require.NoError(t, f.k.ProposeSolution(f.ctx, solverA, c.sessionID, c.matches(), idLow))
	require.NoError(t, f.k.ProposeSolution(f.ctx, solverB, c.sessionID, c.matches(), idHigh))

	winner, ok, err := f.k.selectWinner(f.ctx, c.sessionID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, idLow, winner.Id, "tie breaks to the lower solution id")
}
