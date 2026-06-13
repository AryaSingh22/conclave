package keeper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/veil/types"
)

// allStates is every storable lifecycle state (UNSPECIFIED is never stored).
var allStates = []types.IntentState{
	types.INTENT_STATE_SUBMITTED,
	types.INTENT_STATE_SEALED_IN_SESSION,
	types.INTENT_STATE_OPEN,
	types.INTENT_STATE_CONCLUDED,
	types.INTENT_STATE_ROLLED,
	types.INTENT_STATE_EXPIRED,
	types.INTENT_STATE_INVALID,
	types.INTENT_STATE_CANCELLED,
}

// spec52Legal is the SPEC §5.2 transition table, written out INDEPENDENTLY
// of types.LegalTransitions — this test pins the spec, not the
// implementation's own table:
//
//	SUBMITTED         → SEALED_IN_SESSION | CANCELLED
//	SEALED_IN_SESSION → OPEN | INVALID | CANCELLED
//	OPEN              → CONCLUDED | ROLLED | EXPIRED
//	everything else   → nothing (terminal in Phase 0)
//
// Note SEALED_IN_SESSION → CANCELLED is legal per §5.2 ("any pre-OPEN
// state") but unreachable in live block flow (D-039): the state exists
// only between EndBlock and the next BeginBlock, where no tx executes.
// The edge is still pinned here at the keeper level.
var spec52Legal = map[types.IntentState]map[types.IntentState]bool{
	types.INTENT_STATE_SUBMITTED: {
		types.INTENT_STATE_SEALED_IN_SESSION: true,
		types.INTENT_STATE_CANCELLED:         true,
	},
	types.INTENT_STATE_SEALED_IN_SESSION: {
		types.INTENT_STATE_OPEN:      true,
		types.INTENT_STATE_INVALID:   true,
		types.INTENT_STATE_CANCELLED: true,
	},
	types.INTENT_STATE_OPEN: {
		types.INTENT_STATE_CONCLUDED: true,
		types.INTENT_STATE_ROLLED:    true,
		types.INTENT_STATE_EXPIRED:   true,
	},
}

// TestTransitionTable is the SPEC §11.2 requirement: an explicit
// transition-table test asserting illegal transitions are impossible.
// Every (from, to) pair goes through the keeper's single transition
// chokepoint; exactly the §5.2 edges succeed.
func TestTransitionTable(t *testing.T) {
	f := setupKeeper(t)
	owner := randAddr()
	sigilID := f.registerSigil(t, owner)
	f.beginBlock(t, 1)

	for _, from := range allStates {
		for _, to := range allStates {
			t.Run(fmt.Sprintf("%s->%s", from, to), func(t *testing.T) {
				// a fresh record per pair, forced into the from-state via
				// the same write path the keeper uses
				id := f.submitValid(t, owner, sigilID, 1000)
				rec := f.mustRecord(t, id)
				rec.State = from
				require.NoError(t, f.k.setIntentRecord(f.ctx, rec, types.INTENT_STATE_SUBMITTED))

				err := f.k.transitionIntent(f.ctx, &rec, to)
				if spec52Legal[from][to] {
					require.NoError(t, err, "SPEC §5.2 allows %s -> %s", from, to)
					require.Equal(t, to, f.mustRecord(t, id).State)
				} else {
					require.ErrorIs(t, err, types.ErrIllegalTransition,
						"SPEC §5.2 forbids %s -> %s", from, to)
					require.Equal(t, from, f.mustRecord(t, id).State,
						"a rejected transition must not change state")
				}
			})
		}
	}
}

// TestTransitionTableMatchesTypesTable guards against the implementation
// table drifting from the spec table written out above.
func TestTransitionTableMatchesTypesTable(t *testing.T) {
	for _, from := range allStates {
		for _, to := range allStates {
			require.Equal(t, spec52Legal[from][to], types.CanTransition(from, to),
				"types.LegalTransitions disagrees with SPEC §5.2 for %s -> %s", from, to)
		}
	}
}

// TestSweepsCannotTouchTerminalStates: the EndBlock/BeginBlock sweeps are
// driven by the state index, so a terminal-state record can never re-enter
// the lifecycle even across boundaries and reveals.
func TestSweepsCannotTouchTerminalStates(t *testing.T) {
	f := setupKeeper(t)
	owner := randAddr()
	sigilID := f.registerSigil(t, owner)

	f.beginBlock(t, 1)
	id := f.submitValid(t, owner, sigilID, 2) // ttl 2: expires fast
	f.bank.fund(owner.String(), sdk.NewCoins(minBond()))

	// cancel → CANCELLED (terminal)
	require.NoError(t, f.k.CancelIntent(f.ctx, owner.String(), id))

	// drive several full cycles incl. boundaries and expiry heights
	for h := int64(2); h <= 9; h++ {
		f.beginBlock(t, h)
		f.endBlock(t)
	}
	require.Equal(t, types.INTENT_STATE_CANCELLED, f.mustRecord(t, id).State)
}

// TestOrderThenRevealInvariantGuard is the §1.1 goal-1 / §6.3 order-then-
// reveal invariant-protection guard — the analog of 0.6's
// registration-order guard. It emulates "a test build that runs the Reveal
// in the boundary EndBlock instead of the next BeginBlock" by sealing and
// then immediately revealing within the SAME block height, and asserts the
// intent is NOT opened: RevealSealed's strict-earlier-block check
// (rec.SessionId < height) rejects it. The intent opens only in the
// strictly-later block.
//
// This test MUST FAIL if a future refactor collapses the two phases
// (e.g. moves the Reveal into the boundary EndBlock, or drops the
// SessionId < height guard) — which is exactly what protects the Phase-1
// stub strategy: Phase 1 binds the inability-to-decrypt-before-ordering to
// vote-extension shares of the ordering block, and that only holds if
// ordering is fixed a full block before any Reveal runs.
func TestOrderThenRevealInvariantGuard(t *testing.T) {
	f := setupKeeper(t)
	owner := randAddr()
	sigilID := f.registerSigil(t, owner)

	const boundary = 3 // a session boundary at cadence 3 (D-038)
	f.beginBlock(t, 1)
	id := f.submitValid(t, owner, sigilID, 100)

	// emulate the collapsed schedule: seal AND Reveal both at the boundary
	// height, as a refactor running the Reveal in the boundary EndBlock
	// would. Ordering fixes with session id == boundary height.
	f.ctx = f.ctx.WithBlockHeight(boundary)
	require.NoError(t, f.k.SealSubmitted(f.ctx, boundary))
	require.Equal(t, types.INTENT_STATE_SEALED_IN_SESSION, f.mustRecord(t, id).State)

	// the Reveal in the SAME block must be a no-op — the invariant holds
	require.NoError(t, f.k.RevealSealed(f.ctx))
	require.Equal(t, types.INTENT_STATE_SEALED_IN_SESSION, f.mustRecord(t, id).State,
		"INVARIANT GUARD: a Reveal in the boundary block (session_id >= height) "+
			"must NOT open the intent — ordering must be fixed a strictly earlier "+
			"block (SPEC §6.3). If this fails, the order-then-reveal phases were "+
			"collapsed and the Phase-1 stub strategy is broken.")

	// only the strictly-later block's Reveal opens it
	f.beginBlock(t, boundary+1)
	require.Equal(t, types.INTENT_STATE_OPEN, f.mustRecord(t, id).State,
		"the Reveal opens the intent strictly the block after ordering was fixed")
}
