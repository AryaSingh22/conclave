package types

// LegalTransitions is the SPEC §5.2 intent lifecycle state machine, encoded
// exactly:
//
//	SUBMITTED ──(session boundary, ordering fixed)──► SEALED_IN_SESSION
//	SEALED_IN_SESSION ──(Reveal)──► OPEN
//	OPEN ──(matched in winning solution)──► CONCLUDED
//	OPEN ──(unmatched, ttl not reached)──► ROLLED
//	OPEN ──(unmatched, ttl reached)──► EXPIRED
//	SEALED_IN_SESSION ──(Reveal fails / malformed)──► INVALID
//	any pre-OPEN state ──(MsgCancelIntent by owner)──► CANCELLED
//
// ROLLED re-enters the next session's open set at task 0.8; CONCLUDED is
// driven by x/session at 0.8. Every transition the keeper performs goes
// through this table — there is no other write path for IntentRecord.state
// (§11.2: illegal transitions are impossible).
var LegalTransitions = map[IntentState][]IntentState{
	INTENT_STATE_SUBMITTED: {
		INTENT_STATE_SEALED_IN_SESSION,
		INTENT_STATE_CANCELLED,
	},
	INTENT_STATE_SEALED_IN_SESSION: {
		INTENT_STATE_OPEN,
		INTENT_STATE_INVALID,
		INTENT_STATE_CANCELLED,
	},
	INTENT_STATE_OPEN: {
		INTENT_STATE_CONCLUDED,
		INTENT_STATE_ROLLED,
		INTENT_STATE_EXPIRED,
	},
	// task 0.8 (§6.4 step 5): a ttl-ok unmatched intent rolls OPEN→ROLLED then
	// immediately ROLLED→OPEN re-homed in the next session (x/session's rollover
	// sweep drives this via RollIntent). The charge PERSISTS across the roll
	// (D-041): no refund, no re-charge.
	INTENT_STATE_ROLLED: {
		INTENT_STATE_OPEN,
	},
}

// CanTransition reports whether from → to is a legal §5.2 transition.
func CanTransition(from, to IntentState) bool {
	for _, legal := range LegalTransitions[from] {
		if legal == to {
			return true
		}
	}
	return false
}

// IsPreOpen reports whether a state is "pre-OPEN" per §5.2's cancellation
// rule (cancellable by the owner with full bond refund).
func IsPreOpen(state IntentState) bool {
	return state == INTENT_STATE_SUBMITTED || state == INTENT_STATE_SEALED_IN_SESSION
}

// IsTerminal reports whether a state has no outgoing transitions in Phase 0.
func IsTerminal(state IntentState) bool {
	return len(LegalTransitions[state]) == 0
}
