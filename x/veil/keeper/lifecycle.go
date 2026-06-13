package keeper

import (
	"strconv"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/AryaSingh22/conclave/x/veil/types"
)

// bpsDenominator converts basis points to a fraction.
const bpsDenominator = 10_000

// SubmitIntent implements SPEC §6.3 MsgSubmitIntent semantics: envelope
// validation, bond validation against the §6.3 formula, bond escrow into
// the module account, id assignment (§5.1 derivation via D-009), and
// storage as SUBMITTED. The payload (ciphertext) is deliberately NOT
// inspected for validity — malformedness is the Reveal's job even in stub
// mode (§6.3 INVALID path; Phase 1 cannot inspect ciphertexts here).
func (k Keeper) SubmitIntent(ctx sdk.Context, msg *types.MsgSubmitIntent) (string, error) {
	if msg.ChamberId != phase0Chamber {
		return "", errorsmod.Wrapf(types.ErrInvalidChamber, "msg chamber_id %d", msg.ChamberId)
	}
	height := uint64(ctx.BlockHeight())
	env, err := types.ValidateEnvelope(msg.Envelope, height)
	if err != nil {
		return "", err
	}

	owner, err := sdk.AccAddressFromBech32(msg.Sender)
	if err != nil {
		return "", err
	}

	// the sender must resolve to sender_sigil through the x/sigil reverse
	// index (which includes mandate-bound delegate keys, D-017(d)): intents
	// are identity-centric (§5.1 "sigil ID, not raw address")
	sigilID, err := k.sigilKeeper.SigilIDByAddress(ctx, msg.ChamberId, owner)
	if err != nil || sigilID != msg.SenderSigil {
		return "", errorsmod.Wrapf(types.ErrSigilMismatch,
			"sender %s does not resolve to sigil %q", msg.Sender, msg.SenderSigil)
	}

	params, err := k.GetParams(ctx)
	if err != nil {
		return "", err
	}

	// SPEC §6.3 bond rules: bond = max(min_bond_param, bond_bps * face_value),
	// face value per the D-041 Phase-0 scalar
	if err := msg.Bond.Validate(); err != nil {
		return "", errorsmod.Wrapf(types.ErrInvalidBond, "%s", err)
	}
	if msg.Bond.Denom != params.MinBond.Denom {
		return "", errorsmod.Wrapf(types.ErrInvalidBond,
			"bond denom %s, want %s", msg.Bond.Denom, params.MinBond.Denom)
	}
	giveSpend := k.giveLegSpend(ctx, msg.Ciphertext)
	required := params.RequiredBond(faceValueScalar(giveSpend))
	if msg.Bond.Amount.LT(required.Amount) {
		return "", errorsmod.Wrapf(types.ErrBondInsufficient,
			"bond %s below required %s", msg.Bond, required)
	}

	// escrow the bond in the module account (refund/slash at lifecycle end)
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, owner, types.ModuleName, sdk.NewCoins(msg.Bond)); err != nil {
		return "", err
	}

	seq, err := k.nextIDSequence(ctx)
	if err != nil {
		return "", err
	}
	id := deriveID(domainIntent, msg.ChamberId, []byte(msg.SenderSigil), seq)

	rec := types.IntentRecord{
		Intent: types.SealedIntent{
			Id:           id,
			ChamberId:    msg.ChamberId,
			SenderSigil:  msg.SenderSigil,
			Ciphertext:   msg.Ciphertext,
			Envelope:     msg.Envelope,
			Bond:         msg.Bond,
			SubmitHeight: height,
		},
		State: types.INTENT_STATE_SUBMITTED,
		Owner: msg.Sender,
		// persist exactly what the ante charged to the mandate window —
		// give-leg per-denom max plus bond (D-025) — so every refund path
		// returns the stored value, never a recomputation (D-041)
		ChargedSpend: giveSpend.Add(msg.Bond),
	}
	if err := k.setIntentRecord(ctx, rec, types.INTENT_STATE_UNSPECIFIED); err != nil {
		return "", err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeIntentSubmitted,
		sdk.NewAttribute(types.AttributeKeyIntentID, id),
		sdk.NewAttribute(types.AttributeKeySenderSigil, msg.SenderSigil),
		sdk.NewAttribute(types.AttributeKeyOwner, msg.Sender),
		sdk.NewAttribute(types.AttributeKeyBond, msg.Bond.String()),
		sdk.NewAttribute(types.AttributeKeyTTLHeight, strconv.FormatUint(env.TtlHeight, 10)),
	))
	return id, nil
}

// CancelIntent implements SPEC §6.3 MsgCancelIntent / §5.2 "any pre-OPEN
// state → CANCELLED": owner-only, full bond refund, and the SPEC §6.2
// step 5 window refund of the stored charged_spend. The cancel/boundary
// race is resolved by ABCI phase ordering (D-039): cancels execute in
// DeliverTx, before the boundary EndBlock seals and after the Reveal
// BeginBlock opens — so a cancel wins in the boundary block and loses
// (rejects) once the Reveal has run.
func (k Keeper) CancelIntent(ctx sdk.Context, sender, intentID string) error {
	rec, err := k.GetIntentRecord(ctx, phase0Chamber, intentID)
	if err != nil {
		return err
	}
	if rec.Owner != sender {
		return errorsmod.Wrapf(types.ErrNotIntentOwner, "intent %s", intentID)
	}
	if !types.IsPreOpen(rec.State) {
		return errorsmod.Wrapf(types.ErrIntentNotCancellable, "intent %s is %s", intentID, rec.State)
	}
	if err := k.transitionIntent(ctx, &rec, types.INTENT_STATE_CANCELLED); err != nil {
		return err
	}
	if err := k.settleRefund(ctx, rec, types.EventTypeIntentCancelled); err != nil {
		return err
	}
	return nil
}

// SealSubmitted implements the SPEC §6.3 session boundary: every SUBMITTED
// intent transitions to SEALED_IN_SESSION with the session id, fixing
// ordering. Runs in EndBlock only (order-then-reveal invariant: the Reveal
// cannot run before the block that executes this completes).
func (k Keeper) SealSubmitted(ctx sdk.Context, sessionID uint64) error {
	ids, err := k.intentIDsInState(ctx, types.INTENT_STATE_SUBMITTED, types.MaxTTL)
	if err != nil {
		return err
	}
	for _, id := range ids {
		rec, err := k.GetIntentRecord(ctx, phase0Chamber, id)
		if err != nil {
			return err
		}
		rec.SessionId = sessionID
		if err := k.transitionIntent(ctx, &rec, types.INTENT_STATE_SEALED_IN_SESSION); err != nil {
			return err
		}
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeIntentSealed,
			sdk.NewAttribute(types.AttributeKeyIntentID, id),
			sdk.NewAttribute(types.AttributeKeySessionID, strconv.FormatUint(sessionID, 10)),
		))
	}
	return nil
}

// RevealSealed implements the Reveal (SPEC §6.3 ABCI hooks): in BeginBlock,
// every intent sealed in a STRICTLY earlier block (stub session id = the
// boundary height, D-038) is decrypted via the ThresholdScheme and either
// opens (OPEN) or, on a malformed payload, turns INVALID with the §6.3
// 50% bond slash to the community pool. Decoding runs behind recover —
// the chain never panics on user input (SPEC §11.6).
func (k Keeper) RevealSealed(ctx sdk.Context) error {
	height := uint64(ctx.BlockHeight())
	ids, err := k.intentIDsInState(ctx, types.INTENT_STATE_SEALED_IN_SESSION, types.MaxTTL)
	if err != nil {
		return err
	}

	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}

	// session id → opened intent ids, for the x/session intake hand-off;
	// keys iterated in ascending order below (deterministic, §11.6)
	opened := make(map[uint64][]string)
	var sessions []uint64

	for _, id := range ids {
		rec, err := k.GetIntentRecord(ctx, phase0Chamber, id)
		if err != nil {
			return err
		}
		// the order-then-reveal invariant, explicit: ordering must have been
		// fixed in a strictly earlier block. With the stub cadence this is
		// always true (sealed in EndBlock h-1, revealed in BeginBlock h),
		// but the check is what ProcessProposal-grade paranoia costs here.
		if rec.SessionId >= height {
			continue
		}

		if _, ok := k.revealPayload(rec.Intent.Ciphertext); !ok {
			if err := k.transitionIntent(ctx, &rec, types.INTENT_STATE_INVALID); err != nil {
				return err
			}
			if err := k.slashInvalid(ctx, rec, params.InvalidSlashBps); err != nil {
				return err
			}
			continue
		}

		if err := k.transitionIntent(ctx, &rec, types.INTENT_STATE_OPEN); err != nil {
			return err
		}
		if _, seen := opened[rec.SessionId]; !seen {
			sessions = append(sessions, rec.SessionId)
		}
		opened[rec.SessionId] = append(opened[rec.SessionId], id)
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeIntentRevealed,
			sdk.NewAttribute(types.AttributeKeyIntentID, id),
			sdk.NewAttribute(types.AttributeKeySessionID, strconv.FormatUint(rec.SessionId, 10)),
		))
	}

	// sessions appended in index iteration order is already deterministic;
	// hand each revealed session to x/session (stub: no-op)
	for _, sessionID := range sessions {
		if err := k.sessionKeeper.IntakeRevealed(ctx, sessionID, opened[sessionID]); err != nil {
			return err
		}
	}
	return nil
}

// ExpireOpen implements §5.2 "OPEN ──(unmatched, ttl reached)──► EXPIRED":
// the EndBlock expiry sweep over the (state, ttl_height) index, with full
// bond refund and the §6.2 step 5 window refund.
func (k Keeper) ExpireOpen(ctx sdk.Context) error {
	height := uint64(ctx.BlockHeight())
	ids, err := k.intentIDsInState(ctx, types.INTENT_STATE_OPEN, height)
	if err != nil {
		return err
	}
	for _, id := range ids {
		rec, err := k.GetIntentRecord(ctx, phase0Chamber, id)
		if err != nil {
			return err
		}
		if err := k.transitionIntent(ctx, &rec, types.INTENT_STATE_EXPIRED); err != nil {
			return err
		}
		if err := k.settleRefund(ctx, rec, types.EventTypeIntentExpired); err != nil {
			return err
		}
	}
	return nil
}

// settleRefund applies the EXPIRED/CANCELLED settlement (SPEC §5.2 "bond
// refunded"; §6.2 step 5 / §6.4 step 7 window refund): the full bond
// returns to the owner and the stored charged_spend returns to the owner's
// mandate window (a no-op for unbound root keys).
//
// CONCLAVE-TODO(phase-0): CONCLUDED refunds (bond + window) are x/session's
// job at task 0.8, inside the §6.4 Conclude cache-context.
func (k Keeper) settleRefund(ctx sdk.Context, rec types.IntentRecord, eventType string) error {
	owner := sdk.MustAccAddressFromBech32(rec.Owner)
	if rec.Intent.Bond.IsPositive() {
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, owner, sdk.NewCoins(rec.Intent.Bond)); err != nil {
			return err
		}
	}
	if err := k.mandateKeeper.RefundWindowSpend(ctx, rec.Intent.ChamberId, owner, rec.ChargedSpend); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		eventType,
		sdk.NewAttribute(types.AttributeKeyIntentID, rec.Intent.Id),
		sdk.NewAttribute(types.AttributeKeyOwner, rec.Owner),
		sdk.NewAttribute(types.AttributeKeyRefunded, rec.Intent.Bond.String()),
	))
	return nil
}

// slashInvalid applies the §6.3 INVALID settlement: invalid_slash_bps of
// the bond funds the community pool (D-007: 5000 = 50%), the remainder
// returns to the owner, and the mandate window is deliberately NOT
// refunded (D-040): §6.2 step 5 and §6.4 step 7 name only expired and
// cancelled intents, and keeping the charge preserves D-025's bound on
// malformed-intent spam from a stolen key.
func (k Keeper) slashInvalid(ctx sdk.Context, rec types.IntentRecord, slashBps uint32) error {
	owner := sdk.MustAccAddressFromBech32(rec.Owner)
	slash := rec.Intent.Bond.Amount.MulRaw(int64(slashBps)).QuoRaw(bpsDenominator)
	remainder := rec.Intent.Bond.Amount.Sub(slash)

	if slash.IsPositive() {
		slashCoins := sdk.NewCoins(sdk.NewCoin(rec.Intent.Bond.Denom, slash))
		if err := k.distrKeeper.FundCommunityPool(ctx, slashCoins, authtypes.NewModuleAddress(types.ModuleName)); err != nil {
			return err
		}
	}
	if remainder.IsPositive() {
		refund := sdk.NewCoins(sdk.NewCoin(rec.Intent.Bond.Denom, remainder))
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, owner, refund); err != nil {
			return err
		}
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeIntentInvalid,
		sdk.NewAttribute(types.AttributeKeyIntentID, rec.Intent.Id),
		sdk.NewAttribute(types.AttributeKeyOwner, rec.Owner),
		sdk.NewAttribute(types.AttributeKeySlashed, slash.String()),
		sdk.NewAttribute(types.AttributeKeyRefunded, remainder.String()),
	))
	return nil
}

// revealPayload runs the ThresholdScheme Reveal (stub: identity) and the
// payload proto-unmarshal behind recover. ok=false means the §6.3 INVALID
// path: "payload that fails proto-unmarshal at Reveal" — this never
// panics the chain (SPEC §11.6).
func (k Keeper) revealPayload(ciphertext []byte) (payload types.IntentPayload, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
		}
	}()
	plaintext, err := k.scheme.Combine(nil, ciphertext)
	if err != nil {
		return payload, false
	}
	if err := payload.Unmarshal(plaintext); err != nil {
		return payload, false
	}
	return payload, true
}

// giveLegSpend mirrors the ante handler's SPEC §6.2 step 4 valuation
// exactly (per-denom max of give legs via the PriceSource, behind recover;
// malformed payloads value zero per D-021) — what it returns plus the bond
// is what the ante charged, persisted as charged_spend (D-041).
func (k Keeper) giveLegSpend(ctx sdk.Context, ciphertext []byte) (spend sdk.Coins) {
	defer func() {
		if r := recover(); r != nil {
			spend = sdk.NewCoins()
		}
	}()

	var payload types.IntentPayload
	if err := payload.Unmarshal(ciphertext); err != nil {
		return sdk.NewCoins()
	}

	maxSpend := sdk.NewCoins()
	for i := range payload.Give {
		maxSpend = maxSpend.Max(k.priceSource.GiveLegValue(ctx, &payload.Give[i]))
	}
	return maxSpend
}

// maxFaceValue caps the D-041 face-value scalar at 10^50 base units — far
// beyond any reachable balance, yet small enough that the §6.3 bps
// multiplication can never overflow math.Int's 256-bit bound (§11.6: user
// input must never panic the chain; a clamped face value just demands an
// unpayable bond and rejects as ErrBondInsufficient).
var maxFaceValue = math.NewIntWithDecimal(1, 50)

// faceValueScalar reduces a give-leg valuation to the D-041 Phase-0 face
// value: the denom-blind sum of the per-denom max amounts in base units.
// No cross-denom prices exist before task 0.8; this is the only
// deterministic scalar available and it scales the §6.3 bond with intent
// size. CONCLAVE-TODO(phase-0): 0.8's clearing-price source normalizes
// give legs to uusdc, replacing this approximation.
func faceValueScalar(giveSpend sdk.Coins) math.Int {
	total := math.ZeroInt()
	for _, coin := range giveSpend {
		if coin.Amount.GT(maxFaceValue) {
			return maxFaceValue
		}
		total = total.Add(coin.Amount)
		if total.GT(maxFaceValue) {
			return maxFaceValue
		}
	}
	return total
}
