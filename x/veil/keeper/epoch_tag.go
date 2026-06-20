package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/veil/types"
)

// HasInFlightForEpoch implements epochtypes.VeilKeeper (PHASE1_DESIGN §2(b),
// D-068; the drain-before-rotate / seam-(a) hook). It reports whether any intent
// that is sealed-but-not-yet-revealed (state SEALED_IN_SESSION) is tagged to
// epochID via its envelope. x/epoch retires a DRAINING epoch only once this
// returns false — so an epoch's key material is retained exactly until its
// in-flight ciphertexts reach a terminal Reveal outcome (OPEN-and-cleared or
// CANCELLED). A CANCELLED stuck session leaves SEALED_IN_SESSION, so it stops
// counting here — CANCEL releases the rotation.
//
// epochID==0 (stub/unset intents) is never in flight for a DKG epoch. The walk
// is deterministic (the SEALED_IN_SESSION state index is key-ordered, §11.6); a
// store/decode error returns true (conservative — never retire an epoch whose
// drain cannot be confirmed).
func (k Keeper) HasInFlightForEpoch(ctx sdk.Context, epochID uint64) bool {
	if epochID == 0 {
		return false
	}
	ids, err := k.intentIDsInState(ctx, types.INTENT_STATE_SEALED_IN_SESSION, types.MaxTTL)
	if err != nil {
		return true
	}
	for _, id := range ids {
		rec, err := k.GetIntentRecord(ctx, phase0Chamber, id)
		if err != nil {
			return true
		}
		var env types.IntentEnvelope
		if err := env.Unmarshal(rec.Intent.Envelope); err != nil {
			// a stored envelope that does not decode is corruption, not user
			// input; do not let it silently un-gate retirement.
			return true
		}
		if env.EpochId == epochID {
			return true
		}
	}
	return false
}
