package keeper

import (
	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	escrowtypes "github.com/AryaSingh22/conclave/x/escrow/types"
	receipttypes "github.com/AryaSingh22/conclave/x/receipt/types"
	"github.com/AryaSingh22/conclave/x/session/types"
)

// This file is the task 2.9 escrow→receipt write-back (D-100). receipt.go makes
// Receipt.condition_outcome true AT settlement (PENDING for a conditioned
// match, NONE for an unconditioned one); this file makes it true AFTER it, by
// carrying every escrow resolution back onto the receipts the settlement wrote.
//
// Before 2.9 the field was stamped CONDITION_OUTCOME_NONE unconditionally and
// never updated, so an ICS-23-provable artifact told external verifiers that a
// conditioned settlement carried no condition — while its funds sat in escrow
// and could still refund (D-087).
//
// THE DIRECTION IS FORCED, and so is the seam. x/escrow owns every resolution
// path (msg server, the CVM release binding, and the EndBlock sweep) while
// x/session owns the receipt bytes; x/session is constructed AFTER x/escrow
// because CONCLUDE writes escrows through it. So x/escrow reaches back through
// a set-before-capture writer seam, exactly like SetVeilKeeper (D-068) and
// SetLiveExposureFn (D-064).
//
// THE INTERFACE IS TYPED IN ESCROW'S OWN VOCABULARY (EscrowState, never
// ConditionOutcome) BECAUSE THE OBVIOUS SIGNATURE IS A CYCLE:
// x/escrow/types → x/receipt/types → x/session/types → x/escrow/types.
// The projection therefore lives here, in the module that owns what a receipt
// means — which is also where it belongs on the merits.

var _ escrowtypes.ReceiptOutcomeWriter = Keeper{}

// outcomeForEscrowState projects an x/escrow lifecycle state onto the §6.6
// outcome a receipt reports. It is TOTAL over the states the two callers can
// produce and rejects every other one with a named error rather than writing a
// wrong value: commitTerminal passes only what nextState returns for a terminal
// event (RELEASED/REFUNDED) and RaiseDispute passes only DISPUTED, so ACTIVE
// and UNSPECIFIED are unreachable — and an unreachable case that silently wrote
// something would be exactly the decorative-value defect 2.9 exists to close.
func outcomeForEscrowState(state escrowtypes.EscrowState) (receipttypes.ConditionOutcome, error) {
	switch state {
	case escrowtypes.ESCROW_STATE_RELEASED:
		return receipttypes.CONDITION_OUTCOME_RELEASED, nil
	case escrowtypes.ESCROW_STATE_REFUNDED:
		return receipttypes.CONDITION_OUTCOME_REFUNDED, nil
	case escrowtypes.ESCROW_STATE_DISPUTED:
		// NOT terminal: an OPTIMISTIC dispute is OPEN. It resolves onward to
		// RELEASED (seller reveals) or REFUNDED (resolution sweep), and the
		// receipt follows it there.
		return receipttypes.CONDITION_OUTCOME_DISPUTED, nil
	default:
		return receipttypes.CONDITION_OUTCOME_UNSPECIFIED,
			errorsmod.Wrapf(types.ErrReceiptOutcomeUnmapped, "escrow state %s", state)
	}
}

// terminalOutcome reports whether an outcome is final for the receipt — the
// three the proto documents as TERMINAL. RELEASED and REFUNDED are terminal in
// the escrow transition gate too, and this predicate is what makes that a
// property of the RECEIPT rather than an assumption inherited from that gate
// (see stampReceiptOutcome). NONE is included because it is a statement about
// the settlement itself — nothing was ever escrowed — that no later event can
// revise. The write-back cannot currently reach a NONE receipt at all, since
// only conditioned settlements are indexed; this is what keeps that safe by
// construction rather than by that coincidence.
func terminalOutcome(o receipttypes.ConditionOutcome) bool {
	switch o {
	case receipttypes.CONDITION_OUTCOME_NONE,
		receipttypes.CONDITION_OUTCOME_RELEASED,
		receipttypes.CONDITION_OUTCOME_REFUNDED:
		return true
	default:
		return false
	}
}

// SetReceiptOutcomeForEscrow carries one escrow's new state onto every receipt
// the settlement that created it wrote — both parties', through the 0x0C
// escrow→receipt index CONCLUDE populated inside the same atomic cache.
//
// A MISSING INDEX IS A NO-OP, NOT AN ERROR, AND THAT IS SAFETY-CRITICAL. Escrow
// resolution runs in EndBlock (the timeout sweep), where a returned error
// propagates and halts the chain — so this path may never fail on absent data.
// It is also CORRECT rather than merely safe: x/session's genesis proto carries
// no receipts at all, so an escrow restored from a genesis import has no
// receipt to update and nothing is being skipped.
//
// implements SPEC §6.6 condition_outcome (the escrow-resolution half; D-100).
func (k Keeper) SetReceiptOutcomeForEscrow(ctx sdk.Context, escrowID string, state escrowtypes.EscrowState) error {
	outcome, err := outcomeForEscrowState(state)
	if err != nil {
		return err
	}
	escrowIDBz, err := types.IDBytes(escrowID)
	if err != nil {
		return err
	}

	prefix := types.EscrowReceiptIdxPrefixFor(escrowIDBz)
	store := k.storeService.OpenKVStore(ctx)
	// PrefixEnd, never prefix‖0xFF — a truncated range end silently drops every
	// key whose next byte is 0xFF, which is 1 receipt id in 256 (D-088).
	it, err := store.Iterator(prefix, types.PrefixEnd(prefix))
	if err != nil {
		return err
	}
	var receiptIDs [][]byte
	for ; it.Valid(); it.Next() {
		key := it.Key()
		if len(key) != len(prefix)+types.IDLength {
			continue
		}
		rid := make([]byte, types.IDLength)
		copy(rid, key[len(prefix):])
		receiptIDs = append(receiptIDs, rid)
	}
	if err := it.Close(); err != nil {
		return err
	}

	// Collected first, then written: mutating a store while iterating it is
	// unsafe, and the index order (ascending receipt id) fixes the write order
	// on every node (§11.6).
	for _, rid := range receiptIDs {
		if err := k.stampReceiptOutcome(ctx, rid, outcome); err != nil {
			return err
		}
	}
	return nil
}

// stampReceiptOutcome rewrites ONE receipt's condition_outcome under BOTH of its
// keys — the §6.6 primary 0x07 (per-agent chronological) and the secondary 0x08
// (by id). Both, because Receipts(sigil) and Receipt(id) serve different copies
// of the same record and a one-sided update would make the two query surfaces
// disagree about a settlement. The 0x07 key embeds the SETTLEMENT height, which
// does not move, so the record is rewritten in place under an unchanged key.
//
// MONOTONICITY IS ENFORCED HERE, not merely asserted in a test. A receipt that
// has reached RELEASED or REFUNDED is never rewritten again, so the outcome a
// verifier proved at some height can only have been PENDING → [DISPUTED] →
// terminal, and a cached terminal reading can never be contradicted later. The
// escrow gate already makes those states absorbing; this makes the receipt's
// guarantee independent of that, which is what bounds the mutability the D-100
// write-back introduces.
func (k Keeper) stampReceiptOutcome(ctx sdk.Context, receiptIDBz []byte, outcome receipttypes.ConditionOutcome) error {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(types.ReceiptByIDKey(receiptIDBz))
	if err != nil {
		return err
	}
	if bz == nil {
		// Indexed but absent: same genesis-asymmetry reasoning as above.
		return nil
	}
	var rec receipttypes.Receipt
	if err := k.cdc.Unmarshal(bz, &rec); err != nil {
		return err
	}
	if terminalOutcome(rec.ConditionOutcome) || rec.ConditionOutcome == outcome {
		return nil
	}

	rec.ConditionOutcome = outcome
	updated, err := k.cdc.Marshal(&rec)
	if err != nil {
		return err
	}
	sigilBz, err := types.IDBytes(rec.SigilId)
	if err != nil {
		return err
	}
	if err := store.Set(types.ReceiptKey(rec.ChamberId, sigilBz, rec.Height, receiptIDBz), updated); err != nil {
		return err
	}
	if err := store.Set(types.ReceiptByIDKey(receiptIDBz), updated); err != nil {
		return err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeReceiptOutcome,
		sdk.NewAttribute(types.AttributeKeyReceiptID, rec.Id),
		sdk.NewAttribute(types.AttributeKeySigilID, rec.SigilId),
		sdk.NewAttribute(types.AttributeKeyConditionOutcome, outcome.String()),
	))
	return nil
}
