package keeper

import (
	"strconv"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/veil/types"
)

// GetOpenIntent returns an OPEN intent's record and decoded payload for
// x/session solution validation and settlement (SPEC §6.4; task 0.8). The
// payload is decoded through the ThresholdScheme (stub: identity) behind
// recover, so a corrupted stored payload yields an error, never a panic
// (§11.6). Forward-compatible: Phase-1 encrypted payloads are readable only
// through x/veil, never by x/session directly.
//
// implements the x/session VeilKeeper hand-off (SPEC §6.4).
func (k Keeper) GetOpenIntent(ctx sdk.Context, chamberID uint32, intentID string) (types.IntentRecord, types.IntentPayload, error) {
	rec, err := k.GetIntentRecord(ctx, chamberID, intentID)
	if err != nil {
		return types.IntentRecord{}, types.IntentPayload{}, err
	}
	if rec.State != types.INTENT_STATE_OPEN {
		return types.IntentRecord{}, types.IntentPayload{},
			errorsmod.Wrapf(types.ErrIntentNotOpen, "intent %s is %s", intentID, rec.State)
	}
	payload, ok := k.revealPayload(rec.Intent.Ciphertext)
	if !ok {
		// an OPEN intent decoded cleanly at the Reveal, so a failure here is
		// state corruption — surfaced as an error, never a panic.
		return types.IntentRecord{}, types.IntentPayload{},
			errorsmod.Wrapf(types.ErrInvalidEnvelope, "intent %s payload undecodable", intentID)
	}
	return rec, payload, nil
}

// ConcludeIntent transitions an OPEN intent to CONCLUDED and applies the D-041
// CONCLUDED refunds — full bond back to the owner and the stored charged_spend
// back to the owner's mandate window — reusing the EXPIRED/CANCELLED refund
// path. x/session calls this inside its §6.4 Conclude cache-context (task 0.8),
// so the state change and both refunds are part of that all-or-nothing write.
//
// implements the x/session VeilKeeper hand-off (SPEC §6.4; §5.2 OPEN→CONCLUDED).
func (k Keeper) ConcludeIntent(ctx sdk.Context, intentID string) error {
	rec, err := k.GetIntentRecord(ctx, phase0Chamber, intentID)
	if err != nil {
		return err
	}
	if rec.State != types.INTENT_STATE_OPEN {
		return errorsmod.Wrapf(types.ErrIntentNotOpen, "intent %s is %s", intentID, rec.State)
	}
	if err := k.transitionIntent(ctx, &rec, types.INTENT_STATE_CONCLUDED); err != nil {
		return err
	}
	return k.settleRefund(ctx, rec, types.EventTypeIntentConcluded)
}

// RollIntent drives the SPEC §6.4 step-5 rollover: an OPEN intent left unmatched
// by a concluded session, with its ttl not yet reached, rolls into the next
// session — OPEN→ROLLED→OPEN, re-homed under nextSessionID. Per the D-041 refund
// matrix the charge PERSISTS: the intent was never un-spent, so neither the bond
// nor the mandate window is refunded or re-charged (the contrast with EXPIRED/
// CANCELLED, which DO refund via settleRefund). x/session calls this from its
// §6.4 EndBlock rollover sweep, BEFORE x/veil's expiry sweep runs (D-038), so a
// ttl-ok intent re-opens rather than expiring.
//
// implements the x/session VeilKeeper hand-off (SPEC §6.4 step 5; §5.2
// OPEN→ROLLED→OPEN).
func (k Keeper) RollIntent(ctx sdk.Context, intentID string, nextSessionID uint64) error {
	rec, err := k.GetIntentRecord(ctx, phase0Chamber, intentID)
	if err != nil {
		return err
	}
	if rec.State != types.INTENT_STATE_OPEN {
		return errorsmod.Wrapf(types.ErrIntentNotOpen, "intent %s is %s", intentID, rec.State)
	}
	if err := k.transitionIntent(ctx, &rec, types.INTENT_STATE_ROLLED); err != nil {
		return err
	}
	rec.SessionId = nextSessionID
	if err := k.transitionIntent(ctx, &rec, types.INTENT_STATE_OPEN); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeIntentRolled,
		sdk.NewAttribute(types.AttributeKeyIntentID, intentID),
		sdk.NewAttribute(types.AttributeKeySessionID, strconv.FormatUint(nextSessionID, 10)),
	))
	return nil
}
