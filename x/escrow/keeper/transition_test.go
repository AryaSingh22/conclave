package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AryaSingh22/conclave/x/escrow/types"
)

// TestTransitionTable_Exhaustive is the SPEC §11 rule-2 transition-table test:
// it drives the single authoritative gate (nextState) over the FULL product of
// {condition class} × {from state} × {event} and asserts that EXACTLY the legal
// cells transition — every other tuple is rejected with a typed error and
// yields no target state. Because the legal sets below are the complete legal
// sub-graphs of HASHLOCK, OPTIMISTIC, and ZK_DELIVERY (PHASE2_DESIGN §2.1
// criteria 2–4, §3), this proves "illegal transitions are impossible": a
// preimage cannot release a non-HASHLOCK escrow, a dispute cannot touch a
// HASHLOCK or a ZK escrow, an auto-release cannot fire while DISPUTED, a ZK
// escrow releases only on a proof event, nothing leaves a terminal state.
//
// This table is load-bearing: deleting any guard in nextState flips an illegal
// cell to legal and fails here.
func TestTransitionTable_Exhaustive(t *testing.T) {
	allStates := []types.EscrowState{
		types.ESCROW_STATE_UNSPECIFIED,
		types.ESCROW_STATE_ACTIVE,
		types.ESCROW_STATE_RELEASED,
		types.ESCROW_STATE_REFUNDED,
		types.ESCROW_STATE_DISPUTED,
	}
	allEvents := []transitionEvent{
		evtHashlockRelease,
		evtHashlockTimeout,
		evtOptimisticAutoRelease,
		evtOptimisticDispute,
		evtOptimisticReveal,
		evtOptimisticResolve,
		evtZkRelease,
		evtZkTimeout,
	}

	type cell struct {
		from types.EscrowState
		evt  transitionEvent
	}
	// the COMPLETE legal sub-graph per class — anything not here is illegal.
	legal := map[conditionClass]map[cell]types.EscrowState{
		classHashlock: {
			{types.ESCROW_STATE_ACTIVE, evtHashlockRelease}: types.ESCROW_STATE_RELEASED,
			{types.ESCROW_STATE_ACTIVE, evtHashlockTimeout}: types.ESCROW_STATE_REFUNDED,
		},
		classOptimistic: {
			{types.ESCROW_STATE_ACTIVE, evtOptimisticAutoRelease}: types.ESCROW_STATE_RELEASED,
			{types.ESCROW_STATE_ACTIVE, evtOptimisticDispute}:     types.ESCROW_STATE_DISPUTED,
			{types.ESCROW_STATE_DISPUTED, evtOptimisticReveal}:    types.ESCROW_STATE_RELEASED,
			{types.ESCROW_STATE_DISPUTED, evtOptimisticResolve}:   types.ESCROW_STATE_REFUNDED,
		},
		classZk: {
			{types.ESCROW_STATE_ACTIVE, evtZkRelease}: types.ESCROW_STATE_RELEASED,
			{types.ESCROW_STATE_ACTIVE, evtZkTimeout}: types.ESCROW_STATE_REFUNDED,
		},
	}

	for _, class := range []conditionClass{classHashlock, classOptimistic, classZk} {
		for _, from := range allStates {
			for _, evt := range allEvents {
				to, err := nextState(class, from, evt)
				want, isLegal := legal[class][cell{from, evt}]
				if isLegal {
					require.NoErrorf(t, err, "class=%d from=%v evt=%d should be LEGAL", class, from, evt)
					require.Equalf(t, want, to, "class=%d from=%v evt=%d wrong target", class, from, evt)
				} else {
					require.Errorf(t, err, "class=%d from=%v evt=%d must be ILLEGAL", class, from, evt)
					require.Equalf(t, types.ESCROW_STATE_UNSPECIFIED, to, "illegal transition must yield no state")
				}
			}
		}
	}
}

// TestTransitionTable_TerminalStatesAbsorbing proves RELEASED and REFUNDED
// accept no event for any class (double-release / post-refund impossible).
func TestTransitionTable_TerminalStatesAbsorbing(t *testing.T) {
	for _, class := range []conditionClass{classHashlock, classOptimistic, classZk} {
		for _, term := range []types.EscrowState{types.ESCROW_STATE_RELEASED, types.ESCROW_STATE_REFUNDED} {
			for _, evt := range []transitionEvent{
				evtHashlockRelease, evtHashlockTimeout, evtOptimisticAutoRelease,
				evtOptimisticDispute, evtOptimisticReveal, evtOptimisticResolve,
				evtZkRelease, evtZkTimeout,
			} {
				_, err := nextState(class, term, evt)
				require.ErrorIs(t, err, types.ErrTerminalState, "terminal %v must absorb every event", term)
			}
		}
	}
}

// TestTransitionTable_CrossClassGuards pins the named cross-class rejections the
// three-table design exists to guarantee (PHASE2_DESIGN §3).
func TestTransitionTable_CrossClassGuards(t *testing.T) {
	// a dispute on a HASHLOCK has no path.
	_, err := nextState(classHashlock, types.ESCROW_STATE_ACTIVE, evtOptimisticDispute)
	require.ErrorIs(t, err, types.ErrNoDisputePath)

	// a preimage (hashlock-release) cannot release an OPTIMISTIC escrow.
	_, err = nextState(classOptimistic, types.ESCROW_STATE_ACTIVE, evtHashlockRelease)
	require.ErrorIs(t, err, types.ErrWrongClass)

	// auto-release cannot fire while DISPUTED.
	_, err = nextState(classOptimistic, types.ESCROW_STATE_DISPUTED, evtOptimisticAutoRelease)
	require.ErrorIs(t, err, types.ErrAlreadyDisputed)

	// a second dispute on a DISPUTED escrow is rejected.
	_, err = nextState(classOptimistic, types.ESCROW_STATE_DISPUTED, evtOptimisticDispute)
	require.ErrorIs(t, err, types.ErrAlreadyDisputed)

	// seller reveal / dispute resolution require an open dispute.
	_, err = nextState(classOptimistic, types.ESCROW_STATE_ACTIVE, evtOptimisticReveal)
	require.ErrorIs(t, err, types.ErrNotDisputed)
	_, err = nextState(classOptimistic, types.ESCROW_STATE_ACTIVE, evtOptimisticResolve)
	require.ErrorIs(t, err, types.ErrNotDisputed)

	// a ZK proof-release cannot release a HASHLOCK (and a preimage cannot
	// release a ZK escrow) — the proof/preimage paths are class-exclusive.
	_, err = nextState(classHashlock, types.ESCROW_STATE_ACTIVE, evtZkRelease)
	require.ErrorIs(t, err, types.ErrWrongClass)
	_, err = nextState(classZk, types.ESCROW_STATE_ACTIVE, evtHashlockRelease)
	require.ErrorIs(t, err, types.ErrWrongClass)

	// a dispute has no path on a ZK_DELIVERY escrow (like HASHLOCK).
	_, err = nextState(classZk, types.ESCROW_STATE_ACTIVE, evtOptimisticDispute)
	require.ErrorIs(t, err, types.ErrNoDisputePath)
}
