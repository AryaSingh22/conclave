package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// TestFallbackOnNoSolver is the §10 row-0.8 fallback acceptance: with NO solver
// proposing, the EndBlock pipeline falls back to the in-protocol matcher, which
// clears the mutually-fillable swap at degraded (full-give) terms — liveness
// holds. The skim still runs (to the pool; no solver to pay).
func TestFallbackOnNoSolver(t *testing.T) {
	f := setupKeeper(t)
	s := f.setupSurplusSwap(t)
	// deliberately register NO solver and propose NO solution.

	f.processSessionAt(t, 5)

	// both intents settled via the fallback matcher.
	for _, id := range []string{s.aliceID, s.bobID} {
		rec, err := f.veil.GetIntentRecord(f.ctx, 0, id)
		require.NoError(t, err)
		require.Equal(t, veiltypes.INTENT_STATE_CONCLUDED, rec.State, "intent %s settled by fallback", id)
	}
	// degraded full-give clearing moved the assets; bob keeps ≥ his floor.
	require.Equal(t, math.NewInt(10), f.bank.balanceOf(s.alice.String()).AmountOf(svcDenom))
	require.True(t, f.bank.balanceOf(s.bob.String()).AmountOf(usdcDenom).GTE(math.NewInt(90_000_000)), "bob ≥ floor")

	// no solver existed, so the skim sits in the pool (nothing to pay out).
	require.Equal(t, math.NewInt(50_000), f.sessionPoolUUSDC(), "fallback skim retained in pool")

	session, err := f.k.GetSession(f.ctx, 0, s.sessionID)
	require.NoError(t, err)
	require.Equal(t, types.SESSION_STATE_CLOSED, session.State)
}

// TestRolloverUnmatchedTTLOK pins the SPEC §6.4 step-5 / D-041 rollover: an
// unmatched intent whose ttl is not yet reached rolls OPEN→ROLLED→OPEN into the
// next session, its charge PERSISTS (bond stays escrowed — no refund), and the
// carryover FORMING session opens to AUCTION at its boundary even without new
// reveals (liveness).
func TestRolloverUnmatchedTTLOK(t *testing.T) {
	f := setupKeeper(t)
	alice := randAddr()
	aliceSigil := f.registerSigil(t, alice)
	f.bank.fund(alice.String(), sdk.NewCoins(sdk.NewInt64Coin(usdcDenom, 100_000_000)))

	// alice alone in the session: no counterparty, so nothing matches.
	usdc := coinLeg(sdk.NewInt64Coin(usdcDenom, 100_000_000))
	aliceID := f.submitOpen(t, alice, aliceSigil, []veiltypes.IntentLeg{usdc}, []veiltypes.IntentLeg{svcLeg("cmpt", 10)}, 100, "")
	sessionID := f.openSession(t) // session 3, auction_end 5

	veilBondBefore := f.bank.balanceOf(veilModuleAddr).AmountOf(bondDenom)
	require.True(t, veilBondBefore.IsPositive(), "bond escrowed at submit")

	f.processSessionAt(t, 5)

	// the intent rolled: still OPEN, re-homed to the next boundary session (6).
	rec, err := f.veil.GetIntentRecord(f.ctx, 0, aliceID)
	require.NoError(t, err)
	require.Equal(t, veiltypes.INTENT_STATE_OPEN, rec.State)
	require.Equal(t, uint64(6), rec.SessionId, "rolled into the next boundary session")

	// charge PERSISTS: the bond is still escrowed (no refund on roll, D-041).
	require.Equal(t, veilBondBefore, f.bank.balanceOf(veilModuleAddr).AmountOf(bondDenom), "bond not refunded on roll")

	// the carryover session 6 exists in FORMING carrying the rolled intent.
	s6, err := f.k.GetSession(f.ctx, 0, uint64(6))
	require.NoError(t, err)
	require.Equal(t, types.SESSION_STATE_FORMING, s6.State)
	require.Contains(t, s6.IntentIds, aliceID)

	// the original session is CLOSED.
	s3, err := f.k.GetSession(f.ctx, 0, sessionID)
	require.NoError(t, err)
	require.Equal(t, types.SESSION_STATE_CLOSED, s3.State)

	// liveness: the carryover opens to AUCTION at its boundary even with no new
	// reveals (openMatureFormingSessions, height > id).
	f.processSessionAt(t, 7)
	s6, err = f.k.GetSession(f.ctx, 0, uint64(6))
	require.NoError(t, err)
	require.Equal(t, types.SESSION_STATE_AUCTION, s6.State)
}

// TestConcludeFailFallsBackViaEndBlock proves the §6.4 winner-Conclude-failure
// path through the live pipeline: a winning solution whose graph fails mid-apply
// slashes the solver and the session falls back to the in-protocol matcher in the
// SAME EndBlock — the chain settles or rolls, never stalls.
func TestConcludeFailFallsBackViaEndBlock(t *testing.T) {
	f := setupKeeper(t)
	s := f.setupSurplusSwap(t)
	solver := randAddr()
	f.registerSolver(t, solver, minSolverBond())
	require.NoError(t, f.k.ProposeSolution(f.ctx, solver, s.sessionID, []types.Match{s.match()}, types.IDHex(contentHash("doomed"))))

	// drain alice so her 100 uusdc leg fails when the winner's graph applies.
	require.NoError(t, f.bank.send(s.alice.String(), randAddr().String(), sdk.NewCoins(sdk.NewInt64Coin(usdcDenom, 100_000_000))))

	f.processSessionAt(t, 5)

	// solver was slashed 5% for the failed conclude.
	sv, err := f.k.GetSolver(f.ctx, 0, solver)
	require.NoError(t, err)
	require.Equal(t, uint64(1), sv.SlashedCount)

	// session ends CLOSED; the fallback also cannot clear (alice drained), so the
	// intents roll — liveness holds, nothing settled wrongly.
	session, err := f.k.GetSession(f.ctx, 0, s.sessionID)
	require.NoError(t, err)
	require.Equal(t, types.SESSION_STATE_CLOSED, session.State)
	rec, err := f.veil.GetIntentRecord(f.ctx, 0, s.bobID)
	require.NoError(t, err)
	require.Equal(t, veiltypes.INTENT_STATE_OPEN, rec.State, "unsettled intent rolled, not lost")
}
