// Package fillback implements the docs/edge/7683_SURFACE.md §6.3 origin-settler
// admissibility rule — ONCE.
//
// It is a BRIDGE-side rule, not a chain rule: nothing on Conclave binds an
// intent to an origin-chain order (§1.2), so this is the binding, and it runs in
// the settler. It lives in the node repo anyway because two in-tree consumers
// must demonstrate it — the app-level conformance suite and the §6 exit demo —
// and before task 2.9 they implemented it SEPARATELY and had already drifted:
// the conformance test tested `leg.Condition == nil` on a coin leg while the
// demo tested `leg.Condition == nil` on a DataGrant leg. One mandated rule, two
// implementations, disagreeing — D-085's shape at a contract boundary. A rule a
// bridge is told to implement is worth exactly as much as the one statement of
// it everyone can point at.
//
// WHAT TASK 2.9 CHANGED, and why the old rule could not be kept (finding F-16,
// D-100). §6.3 check 3 used to require the paying leg to carry no delivery
// condition, and D-087 accepted that as making the v1 rail "safe by
// construction" INSTEAD of fixing Receipt.condition_outcome. It did not:
// x/session's validateConditionedMatch admits exactly one conditioned shape —
// one conditioned DataGrant leg seller→buyer plus coin payment legs
// buyer→seller — so in a conditioned match every COIN leg necessarily has a nil
// condition, and openConditionedEscrows routes precisely those coin legs into
// escrow. The clause could therefore never reject anything a chain-valid Match
// contains, while an ESCROWED payment to the order's recipient satisfied it in
// full. A resolver reimbursed on the origin chain could watch the condition time
// out and take its funds back on Conclave, with no collusion required.
//
// The correct discriminator is not the leg — it is the settlement outcome, and
// task 2.9 is what made that readable: condition_outcome is NONE only when
// nothing was escrowed (an unconditioned DataGrant escrows a zero amount and its
// coin legs settle directly through applyLeg) and RELEASED only when the escrow
// paid the payee. Both are terminal, and the receipt's outcome is monotone, so a
// verified admissible reading can never be contradicted later.
package fillback

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"

	receipttypes "github.com/AryaSingh22/conclave/x/receipt/types"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
)

// Order is the origin-chain order a fill is claimed against — the parts §6.3
// check 3 needs and nothing else.
type Order struct {
	// ID is the origin settler's order identifier (error text only).
	ID string
	// RecipientSigil must be paid on Conclave.
	RecipientSigil string
	// Denom is the order's output denom.
	Denom string
	// MinReceived is the minimum the recipient must receive.
	MinReceived math.Int
}

var (
	// ErrNoPayingLeg means no leg of the verified Match pays the order's
	// recipient its minimum in the order's denom (§6.3 check 3, first conjunct).
	ErrNoPayingLeg = errors.New("no leg pays the order's recipient its minimum")
	// ErrOutcomeNotFinal means the settlement's delivery condition has not
	// resolved in the payee's favour: the funds are escrowed and may still
	// refund, so the receipt is not delivery evidence (§6.3 check 3, second
	// conjunct — the task 2.9 replacement for the inert leg-condition clause).
	ErrOutcomeNotFinal = errors.New("condition outcome does not admit this fill")
)

// PaysOrder is check 3's FIRST conjunct: some coin leg of the verified Match
// pays the order's recipient at least MinReceived of the order's denom.
func PaysOrder(m sessiontypes.Match, o Order) error {
	for i := range m.Legs {
		leg := &m.Legs[i]
		coin := leg.GetCoin()
		if leg.ToSigil == o.RecipientSigil && coin != nil &&
			coin.Denom == o.Denom && coin.Amount.GTE(o.MinReceived) {
			return nil
		}
	}
	return fmt.Errorf("%w: order %s wants %s%s to %s", ErrNoPayingLeg, o.ID, o.MinReceived, o.Denom, o.RecipientSigil)
}

// OutcomeAdmits is check 3's SECOND conjunct: the receipt's condition outcome
// must be NONE (nothing was ever escrowed) or RELEASED (the escrow paid the
// payee). PENDING and DISPUTED are live escrows whose funds may still refund;
// REFUNDED is a fill that was undone. Rejecting all three is the whole content
// of the fix — the paying leg looks identical in every one of these cases.
func OutcomeAdmits(rec receipttypes.Receipt) error {
	switch rec.ConditionOutcome {
	case receipttypes.CONDITION_OUTCOME_NONE, receipttypes.CONDITION_OUTCOME_RELEASED:
		return nil
	default:
		return fmt.Errorf("%w: receipt %s reads %s", ErrOutcomeNotFinal, rec.Id, rec.ConditionOutcome)
	}
}

// Admissible is §6.3 check 3 whole: both conjuncts, in order. Checks 1 (root
// authenticity), 2 (terms authenticity) and 4 (first-claim uniqueness) are the
// settler's and are demonstrated separately — this function is deliberately the
// ONE thing the two in-tree consumers share, so they cannot drift again.
func Admissible(rec receipttypes.Receipt, m sessiontypes.Match, o Order) error {
	if err := PaysOrder(m, o); err != nil {
		return err
	}
	return OutcomeAdmits(rec)
}
