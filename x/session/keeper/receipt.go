package keeper

import (
	"crypto/sha256"

	sdk "github.com/cosmos/cosmos-sdk/types"

	receipttypes "github.com/AryaSingh22/conclave/x/receipt/types"
	"github.com/AryaSingh22/conclave/x/session/types"
)

// writeMatchReceipts emits one SPEC §6.6 Receipt per matched intent into the
// x/session store (D-045: x/session is the writer; 0.9's x/receipt is the
// read/proof surface). Each receipt is keyed under the §6.6 primary
// 0x07|chamber|sigil|height|rid (per-agent chronological) and the secondary
// 0x08|rid, both pinned in keys.go. Receipts live in IAVL, so 0.9's ICS-23
// existence proofs come free. Called inside the Conclude cache so receipts are
// part of the all-or-nothing settlement write (§6.4).
//
// TASK 2.9 (D-100) MADE THE §6.6 condition_outcome STAMP CONDITIONAL. It was
// CONDITION_OUTCOME_NONE unconditionally — including for matches whose legs
// carried HASHLOCK/OPTIMISTIC/ZK conditions — so an ICS-23-provable artifact
// told external verifiers that a conditioned settlement carried no condition
// while its funds sat in escrow and could still refund (D-087). It now stamps
// PENDING for a conditioned match and NONE only when NONE is true, and records
// the 0x0C escrow→receipt index the resolution write-back walks
// (receipt_outcome.go). Both writes land inside this same Conclude cache, so an
// index entry cannot outlive the receipt it points at or the escrow it is keyed
// by.
func (k Keeper) writeMatchReceipts(ctx sdk.Context, sessionID uint64, m types.Match, intents []matchIntent) error {
	height := uint64(ctx.BlockHeight())
	termsHash := matchTermsHash(k, m)

	// The conditioned leg, if any. validateConditionedMatch admits at most one
	// per match (D-074), and openConditionedEscrows has already created its
	// escrow earlier in this same cache — so the id derived here names a record
	// that exists.
	outcome := receipttypes.CONDITION_OUTCOME_NONE
	var condEscrowIDBz []byte
	if li, ok := conditionedLegIndex(m); ok {
		outcome = receipttypes.CONDITION_OUTCOME_PENDING
		escrowIDBz, err := types.IDBytes(escrowIDForLeg(sessionID, m.Legs[li]))
		if err != nil {
			return err
		}
		condEscrowIDBz = escrowIDBz
	}

	for i := range intents {
		mi := intents[i]
		counterparties := make([]string, 0, len(intents)-1)
		for j := range intents {
			if j != i {
				counterparties = append(counterparties, intents[j].sigil)
			}
		}

		idPayload := append(append([]byte{}, u64be(sessionID)...), []byte(mi.rec.Intent.Id)...)
		receiptID := deriveID(domainReceipt, phase0Chamber, idPayload)
		ridBz, err := types.IDBytes(receiptID)
		if err != nil {
			return err
		}
		sigilBz, err := types.IDBytes(mi.sigil)
		if err != nil {
			return err
		}

		rec := receipttypes.Receipt{
			Id:                 receiptID,
			SessionId:          sessionID,
			IntentId:           mi.rec.Intent.Id,
			SigilId:            mi.sigil,
			CounterpartySigils: counterparties,
			TermsHash:          termsHash,
			ClearingPrices:     m.ClearingPrices,
			ConditionOutcome:   outcome,
			Height:             height,
			ChamberId:          phase0Chamber,
		}
		bz, err := k.cdc.Marshal(&rec)
		if err != nil {
			return err
		}
		store := k.storeService.OpenKVStore(ctx)
		if err := store.Set(types.ReceiptKey(phase0Chamber, sigilBz, height, ridBz), bz); err != nil {
			return err
		}
		if err := store.Set(types.ReceiptByIDKey(ridBz), bz); err != nil {
			return err
		}
		if condEscrowIDBz != nil {
			if err := store.Set(types.EscrowReceiptIdxKey(condEscrowIDBz, ridBz), []byte{}); err != nil {
				return err
			}
		}
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeReceiptEmitted,
			sdk.NewAttribute(types.AttributeKeyReceiptID, receiptID),
			sdk.NewAttribute(types.AttributeKeyIntentID, mi.rec.Intent.Id),
			sdk.NewAttribute(types.AttributeKeySigilID, mi.sigil),
		))
	}
	return nil
}

// conditionedLegIndex returns the index of the match's conditioned DataGrant
// leg. validateConditionedMatch (SPEC §6.5 / D-074) admits at most ONE per
// match and requires it to be a DataGrant, so a second hit is an internal
// invariant break rather than a shape to handle: the first is returned and the
// settlement boundary has already rejected anything else.
func conditionedLegIndex(m types.Match) (int, bool) {
	for li := range m.Legs {
		if c := m.Legs[li].Condition; c != nil && c.Condition != nil {
			return li, true
		}
	}
	return 0, false
}

// matchTermsHash commits to the full settled terms of a match (§6.6 terms_hash).
// The match proto marshals deterministically (gogoproto), so the hash is a
// stable consensus value identical on every node.
func matchTermsHash(k Keeper, m types.Match) []byte {
	bz, err := k.cdc.Marshal(&m)
	if err != nil {
		// Marshal of a value already in a validated graph cannot fail; hash the
		// empty input rather than panic on the settlement path (§11.6).
		bz = nil
	}
	h := sha256.Sum256(bz)
	return h[:]
}
