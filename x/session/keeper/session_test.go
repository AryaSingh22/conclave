package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AryaSingh22/conclave/x/session/types"
)

// TestSessionBoundary proves the real keeper preserves the D-038 cadence
// (session id == boundary height) using the governable §6.4 param, so x/veil's
// order-then-reveal check is unchanged.
func TestSessionBoundary(t *testing.T) {
	f := setupKeeper(t) // session_length_blocks = 3
	cases := []struct {
		height   int64
		boundary bool
		id       uint64
	}{
		{0, false, 0}, {1, false, 0}, {2, false, 0},
		{3, true, 3}, {4, false, 0}, {6, true, 6}, {9, true, 9},
	}
	for _, tc := range cases {
		f.ctx = f.ctx.WithBlockHeight(tc.height)
		isB, id := f.k.SessionBoundary(f.ctx)
		require.Equal(t, tc.boundary, isB, "height %d", tc.height)
		require.Equal(t, tc.id, id, "height %d", tc.height)
	}
}

// TestIntakeRevealedCreatesAuction proves the Reveal hand-off materializes a
// session in AUCTION with its fixed intent set and the auction window.
func TestIntakeRevealedCreatesAuction(t *testing.T) {
	f := setupKeeper(t)
	f.ctx = f.ctx.WithBlockHeight(4)
	require.NoError(t, f.k.IntakeRevealed(f.ctx, 3, []string{"i1", "i2"}))

	s, err := f.k.GetSession(f.ctx, 0, 3)
	require.NoError(t, err)
	require.Equal(t, types.SESSION_STATE_AUCTION, s.State)
	require.Equal(t, []string{"i1", "i2"}, s.IntentIds)
	require.Equal(t, uint64(4), s.StartHeight)
	// auction_end = height + solution_window_blocks - 1 = 4 + 2 - 1 = 5.
	require.Equal(t, uint64(5), s.AuctionEndHeight)
}

func TestSessionTransitions(t *testing.T) {
	legal := [][2]types.SessionState{
		{types.SESSION_STATE_FORMING, types.SESSION_STATE_AUCTION},
		{types.SESSION_STATE_AUCTION, types.SESSION_STATE_SCORING},
		{types.SESSION_STATE_SCORING, types.SESSION_STATE_CONCLUDING},
		{types.SESSION_STATE_CONCLUDING, types.SESSION_STATE_CLOSED},
		{types.SESSION_STATE_CONCLUDING, types.SESSION_STATE_FALLBACK},
		{types.SESSION_STATE_FALLBACK, types.SESSION_STATE_CLOSED},
		{types.SESSION_STATE_AUCTION, types.SESSION_STATE_FALLBACK},
	}
	for _, e := range legal {
		require.True(t, canTransitionSession(e[0], e[1]), "%s -> %s should be legal", e[0], e[1])
	}

	illegal := [][2]types.SessionState{
		{types.SESSION_STATE_CLOSED, types.SESSION_STATE_AUCTION},
		{types.SESSION_STATE_AUCTION, types.SESSION_STATE_CONCLUDING}, // must pass through SCORING
		{types.SESSION_STATE_FORMING, types.SESSION_STATE_SCORING},
		{types.SESSION_STATE_SCORING, types.SESSION_STATE_AUCTION},
	}
	for _, e := range illegal {
		require.False(t, canTransitionSession(e[0], e[1]), "%s -> %s should be illegal", e[0], e[1])
	}
}

// TestTransitionSessionChokepoint proves the keeper rejects illegal session
// transitions (§11.2).
func TestTransitionSessionChokepoint(t *testing.T) {
	f := setupKeeper(t)
	f.ctx = f.ctx.WithBlockHeight(4)
	require.NoError(t, f.k.IntakeRevealed(f.ctx, 3, []string{"i1"}))
	s, err := f.k.GetSession(f.ctx, 0, 3)
	require.NoError(t, err)

	// AUCTION → CONCLUDING is illegal (must pass SCORING).
	require.Error(t, f.k.transitionSession(f.ctx, &s, types.SESSION_STATE_CONCLUDING))

	// AUCTION → SCORING → CONCLUDING → CLOSED is legal.
	require.NoError(t, f.k.transitionSession(f.ctx, &s, types.SESSION_STATE_SCORING))
	require.NoError(t, f.k.transitionSession(f.ctx, &s, types.SESSION_STATE_CONCLUDING))
	require.NoError(t, f.k.transitionSession(f.ctx, &s, types.SESSION_STATE_CLOSED))

	reloaded, err := f.k.GetSession(f.ctx, 0, 3)
	require.NoError(t, err)
	require.Equal(t, types.SESSION_STATE_CLOSED, reloaded.State)
}

func TestCurrentSession(t *testing.T) {
	f := setupKeeper(t)
	f.ctx = f.ctx.WithBlockHeight(4)
	require.NoError(t, f.k.IntakeRevealed(f.ctx, 3, []string{"i1"}))
	f.ctx = f.ctx.WithBlockHeight(7)
	require.NoError(t, f.k.IntakeRevealed(f.ctx, 6, []string{"i2"}))

	cur, err := f.k.CurrentSession(f.ctx, 0)
	require.NoError(t, err)
	require.Equal(t, uint64(6), cur.Id, "latest non-terminal session")

	// close session 6; current falls back to the still-open 3.
	s6, _ := f.k.GetSession(f.ctx, 0, 6)
	require.NoError(t, f.k.transitionSession(f.ctx, &s6, types.SESSION_STATE_SCORING))
	require.NoError(t, f.k.transitionSession(f.ctx, &s6, types.SESSION_STATE_CONCLUDING))
	require.NoError(t, f.k.transitionSession(f.ctx, &s6, types.SESSION_STATE_CLOSED))
	cur, err = f.k.CurrentSession(f.ctx, 0)
	require.NoError(t, err)
	require.Equal(t, uint64(3), cur.Id)
}

// TestGenesisRoundTrip proves init/export equality including solvers, sessions,
// and next_session_id.
func TestGenesisRoundTrip(t *testing.T) {
	f := setupKeeper(t)
	solver := randAddr()
	f.registerSolver(t, solver, minSolverBond())
	f.ctx = f.ctx.WithBlockHeight(4)
	require.NoError(t, f.k.IntakeRevealed(f.ctx, 3, []string{"i1"}))

	exported := f.k.ExportGenesis(f.ctx)
	exported.NextSessionId = 6
	require.NoError(t, exported.Validate())

	f2 := setupKeeper(t)
	f2.k.InitGenesis(f2.ctx, *exported)
	reExported := f2.k.ExportGenesis(f2.ctx)
	require.Equal(t, exported.Params, reExported.Params)
	require.Equal(t, exported.NextSessionId, reExported.NextSessionId)
	require.Len(t, reExported.Solvers, 1)
	require.Len(t, reExported.Sessions, 1)
}
