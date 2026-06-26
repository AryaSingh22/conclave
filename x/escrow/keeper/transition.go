package keeper

import (
	"github.com/AryaSingh22/conclave/x/escrow/types"
)

// conditionClass is the resolved condition-class of an escrow. Each class
// admits a DIFFERENT legal sub-graph over the shared EscrowState enum
// (PHASE2_DESIGN §3): HASHLOCK never reaches DISPUTED; OPTIMISTIC is the only
// class that uses DISPUTED; ZK_DELIVERY gates RELEASED on a verifying groth16
// proof (not a preimage or timer) and, like HASHLOCK, never reaches DISPUTED;
// NONE (the Phase-0 record-only DataGrant stub) has no transitions at all.
// Modeling them as separate sub-graphs is what makes the SPEC §11-rule-2
// "illegal transitions impossible" assertion meaningful.
type conditionClass int

const (
	classNone conditionClass = iota
	classHashlock
	classOptimistic
	classZk
)

// classOf resolves an escrow's condition class from its DeliveryCondition
// oneof. An unset oneof is NONE (the record-only DataGrant stub).
func classOf(esc types.Escrow) conditionClass {
	switch {
	case esc.Condition.GetHashlock() != nil:
		return classHashlock
	case esc.Condition.GetOptimistic() != nil:
		return classOptimistic
	case esc.Condition.GetZkDelivery() != nil:
		return classZk
	default:
		return classNone
	}
}

// transitionEvent enumerates every event that can drive an escrow state change.
// MsgReleaseEscrow maps to evtHashlockRelease (HASHLOCK) or evtOptimisticReveal
// (OPTIMISTIC, seller proves delivery under dispute); MsgRaiseDispute maps to
// evtOptimisticDispute; the EndBlock sweep maps to evtHashlockTimeout,
// evtOptimisticAutoRelease, or evtOptimisticResolve depending on class/state.
type transitionEvent int

const (
	evtHashlockRelease       transitionEvent = iota // HASHLOCK: buyer presents preimage
	evtHashlockTimeout                              // HASHLOCK: timeout sweep
	evtOptimisticAutoRelease                        // OPTIMISTIC: window elapsed, undisputed
	evtOptimisticDispute                            // OPTIMISTIC: dispute raised in-window
	evtOptimisticReveal                             // OPTIMISTIC: seller reveals preimage under dispute
	evtOptimisticResolve                            // OPTIMISTIC: dispute resolution sweep (seller failed to prove)
	evtZkRelease                                    // ZK_DELIVERY: seller submits a verifying groth16 proof
	evtZkTimeout                                    // ZK_DELIVERY: timeout sweep, no valid proof
)

// nextState is the SINGLE authoritative transition gate (SPEC §11 rule 2). It
// is a pure function of (condition class, current state, event): it answers
// "is this event legal from this state for this class, and what is the target
// state?", rejecting every illegal tuple with a typed error and NEVER
// performing a side effect. Data-dependent guards (height vs timeout, preimage
// vs committed hash) layer on top in the callers; this gate fixes the legal
// SHAPE of each class's sub-graph. RELEASED and REFUNDED are terminal in every
// class — they accept no event.
//
// implements SPEC §6.5 condition-class state machines (PHASE2_DESIGN §2.1
// criteria 2 & 3).
func nextState(class conditionClass, from types.EscrowState, evt transitionEvent) (types.EscrowState, error) {
	// Terminal states accept nothing — double-release / post-refund / any
	// transition out of a settled escrow is rejected uniformly.
	if from == types.ESCROW_STATE_RELEASED || from == types.ESCROW_STATE_REFUNDED {
		return types.ESCROW_STATE_UNSPECIFIED, types.ErrTerminalState
	}

	switch evt {
	case evtHashlockRelease:
		if class != classHashlock {
			return types.ESCROW_STATE_UNSPECIFIED, types.ErrWrongClass
		}
		if from != types.ESCROW_STATE_ACTIVE {
			return types.ESCROW_STATE_UNSPECIFIED, types.ErrTerminalState
		}
		return types.ESCROW_STATE_RELEASED, nil

	case evtHashlockTimeout:
		if class != classHashlock {
			return types.ESCROW_STATE_UNSPECIFIED, types.ErrWrongClass
		}
		if from != types.ESCROW_STATE_ACTIVE {
			return types.ESCROW_STATE_UNSPECIFIED, types.ErrTerminalState
		}
		return types.ESCROW_STATE_REFUNDED, nil

	case evtOptimisticAutoRelease:
		if class != classOptimistic {
			return types.ESCROW_STATE_UNSPECIFIED, types.ErrWrongClass
		}
		// auto-release must NOT fire while a dispute is open.
		if from == types.ESCROW_STATE_DISPUTED {
			return types.ESCROW_STATE_UNSPECIFIED, types.ErrAlreadyDisputed
		}
		if from != types.ESCROW_STATE_ACTIVE {
			return types.ESCROW_STATE_UNSPECIFIED, types.ErrTerminalState
		}
		return types.ESCROW_STATE_RELEASED, nil

	case evtOptimisticDispute:
		// A dispute on a non-OPTIMISTIC escrow has no path (HASHLOCK/NONE/ZK).
		if class != classOptimistic {
			return types.ESCROW_STATE_UNSPECIFIED, types.ErrNoDisputePath
		}
		if from == types.ESCROW_STATE_DISPUTED {
			return types.ESCROW_STATE_UNSPECIFIED, types.ErrAlreadyDisputed
		}
		if from != types.ESCROW_STATE_ACTIVE {
			return types.ESCROW_STATE_UNSPECIFIED, types.ErrTerminalState
		}
		return types.ESCROW_STATE_DISPUTED, nil

	case evtOptimisticReveal:
		if class != classOptimistic {
			return types.ESCROW_STATE_UNSPECIFIED, types.ErrWrongClass
		}
		// the seller can only prove delivery once a dispute is open.
		if from != types.ESCROW_STATE_DISPUTED {
			return types.ESCROW_STATE_UNSPECIFIED, types.ErrNotDisputed
		}
		return types.ESCROW_STATE_RELEASED, nil

	case evtOptimisticResolve:
		if class != classOptimistic {
			return types.ESCROW_STATE_UNSPECIFIED, types.ErrWrongClass
		}
		if from != types.ESCROW_STATE_DISPUTED {
			return types.ESCROW_STATE_UNSPECIFIED, types.ErrNotDisputed
		}
		return types.ESCROW_STATE_REFUNDED, nil

	case evtZkRelease:
		// ZK_DELIVERY releases ONLY on a verifying proof (the data guard layered
		// in the caller); a preimage/dispute cannot release it.
		if class != classZk {
			return types.ESCROW_STATE_UNSPECIFIED, types.ErrWrongClass
		}
		if from != types.ESCROW_STATE_ACTIVE {
			return types.ESCROW_STATE_UNSPECIFIED, types.ErrTerminalState
		}
		return types.ESCROW_STATE_RELEASED, nil

	case evtZkTimeout:
		if class != classZk {
			return types.ESCROW_STATE_UNSPECIFIED, types.ErrWrongClass
		}
		if from != types.ESCROW_STATE_ACTIVE {
			return types.ESCROW_STATE_UNSPECIFIED, types.ErrTerminalState
		}
		return types.ESCROW_STATE_REFUNDED, nil

	default:
		return types.ESCROW_STATE_UNSPECIFIED, types.ErrInvalidEscrow
	}
}
