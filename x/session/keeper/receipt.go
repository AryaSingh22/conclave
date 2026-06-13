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
func (k Keeper) writeMatchReceipts(ctx sdk.Context, sessionID uint64, m types.Match, intents []matchIntent) error {
	height := uint64(ctx.BlockHeight())
	termsHash := matchTermsHash(k, m)

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
			ConditionOutcome:   receipttypes.CONDITION_OUTCOME_NONE,
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
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeReceiptEmitted,
			sdk.NewAttribute(types.AttributeKeyReceiptID, receiptID),
			sdk.NewAttribute(types.AttributeKeyIntentID, mi.rec.Intent.Id),
			sdk.NewAttribute(types.AttributeKeySigilID, mi.sigil),
		))
	}
	return nil
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
