package keeper

import (
	"errors"
	"sort"
	"strconv"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/AryaSingh22/conclave/crypto/reveal"
	"github.com/AryaSingh22/conclave/crypto/threshold"
	"github.com/AryaSingh22/conclave/x/veil/types"
)

// bpsDenominator converts basis points to a fraction.
const bpsDenominator = 10_000

// ValidateSubmitIntentPreconditions runs EXACTLY the payload-independent
// preconditions the SubmitIntent msg server enforces before it escrows the
// bond and writes the record — the checks that never read the (possibly
// encrypted) IntentPayload. It is the SINGLE SOURCE OF TRUTH for those
// preconditions: SubmitIntent calls it on the direct/msg-server path, and the
// SubmitIntentPrevalidationDecorator calls it in the ante BEFORE the SPEC §6.2
// mandate window charge (D-085/G2). Because BOTH callers use this one
// function, a submit that would fail in the msg server is rejected in the ante
// first, so the mandate's optimistic charge (which baseapp commits before the
// msg runs) never strands — closing the on-demand break of the D-059/D-064
// conservation invariant that a msg-server-only rejection would leave behind.
//
// Every check here mirrors a msg-server early-return, one-for-one:
//
//	chamber           → ErrInvalidChamber
//	envelope structure → ErrInvalidEnvelope (ttl, fee_bid, spend_commitment)
//	epoch-tag          → ErrEpochNotDecryptable
//	sender parses      → bech32 error
//	sender→sender_sigil → ErrSigilMismatch
//	bond valid/denom/≥required → ErrInvalidBond / ErrBondInsufficient
//	bond fundable      → ErrBondFundsInsufficient (mirrors the escrow SendCoins)
//
// A new payload-independent precondition added to SubmitIntent MUST be added
// here too, or a new strand opens for the uncovered mode. Returns the parsed
// envelope and owner so SubmitIntent does not re-derive them.
func (k Keeper) ValidateSubmitIntentPreconditions(ctx sdk.Context, msg *types.MsgSubmitIntent) (types.IntentEnvelope, sdk.AccAddress, error) {
	var env types.IntentEnvelope
	if msg.ChamberId != phase0Chamber {
		return env, nil, errorsmod.Wrapf(types.ErrInvalidChamber, "msg chamber_id %d", msg.ChamberId)
	}
	height := uint64(ctx.BlockHeight())
	env, err := types.ValidateEnvelope(msg.Envelope, height)
	if err != nil {
		return env, nil, err
	}

	// D-068 epoch-tag validation (both edges). A non-zero envelope epoch_id must
	// reference a CURRENTLY-DECRYPTABLE epoch (ACTIVE or DRAINING): this rejects
	// both a RETIRED epoch (past edge) and a not-yet-published epoch (future edge
	// the client could not have fetched to encrypt to), so the Reveal can always
	// select a live share set for an accepted ciphertext. epoch_id==0 is the
	// stub/unset path (always allowed); a nil epochKeeper means no epoch system
	// is wired (stub mode) — the check is skipped so stub-mode behavior is
	// uniform with Phase 0.
	if env.EpochId != 0 && k.epochKeeper != nil && !k.epochKeeper.IsDecryptableEpoch(ctx, env.EpochId) {
		return env, nil, errorsmod.Wrapf(types.ErrEpochNotDecryptable, "epoch_id %d", env.EpochId)
	}

	owner, err := sdk.AccAddressFromBech32(msg.Sender)
	if err != nil {
		return env, nil, err
	}

	// the sender must resolve to sender_sigil through the x/sigil reverse
	// index (which includes mandate-bound delegate keys, D-017(d)): intents
	// are identity-centric (§5.1 "sigil ID, not raw address")
	sigilID, err := k.sigilKeeper.SigilIDByAddress(ctx, msg.ChamberId, owner)
	if err != nil || sigilID != msg.SenderSigil {
		return env, owner, errorsmod.Wrapf(types.ErrSigilMismatch,
			"sender %s does not resolve to sigil %q", msg.Sender, msg.SenderSigil)
	}

	params, err := k.GetParams(ctx)
	if err != nil {
		return env, owner, err
	}

	// SPEC §6.3 bond rules: bond = max(min_bond_param, bond_bps * face_value),
	// face value per the D-041 Phase-0 scalar. The face value derives from the
	// declared spend_commitment, NOT the give legs (D-065): the msg server is
	// blind to give legs once payloads are encrypted, so a give-leg-derived
	// bond would compute a different magnitude in stub mode than the encrypted
	// path ships — the same uniformity that makes the ante commitment-bound.
	// Over-declaration therefore costs a larger bond (correct self-penalty).
	if err := msg.Bond.Validate(); err != nil {
		return env, owner, errorsmod.Wrapf(types.ErrInvalidBond, "%s", err)
	}
	if msg.Bond.Denom != params.MinBond.Denom {
		return env, owner, errorsmod.Wrapf(types.ErrInvalidBond,
			"bond denom %s, want %s", msg.Bond.Denom, params.MinBond.Denom)
	}
	required := params.RequiredBond(faceValueScalar(env.SpendCommitment))
	if msg.Bond.Amount.LT(required.Amount) {
		return env, owner, errorsmod.Wrapf(types.ErrBondInsufficient,
			"bond %s below required %s", msg.Bond, required)
	}
	// the submitter must be able to fund the bond escrow. This mirrors the
	// SendCoinsFromAccountToModule below as an up-front precondition (D-085/G2):
	// without it, an under-funded submit passes the ante (charging the window)
	// and fails the escrow in the msg server, stranding the charge. The bond
	// denom is the native bond denom (checked above); the standard fee is
	// deducted earlier in the ante and abstracted fees are a different denom, so
	// the spendable balance read here still predicts the escrow.
	if spendable := k.bankKeeper.SpendableCoin(ctx, owner, msg.Bond.Denom); spendable.Amount.LT(msg.Bond.Amount) {
		return env, owner, errorsmod.Wrapf(types.ErrBondFundsInsufficient,
			"spendable %s < bond %s", spendable, msg.Bond)
	}
	return env, owner, nil
}

// SubmitIntent implements SPEC §6.3 MsgSubmitIntent semantics: envelope
// validation, bond validation against the §6.3 formula, bond escrow into
// the module account, id assignment (§5.1 derivation via D-009), and
// storage as SUBMITTED. The payload (ciphertext) is deliberately NOT
// inspected for validity — malformedness is the Reveal's job even in stub
// mode (§6.3 INVALID path; Phase 1 cannot inspect ciphertexts here).
func (k Keeper) SubmitIntent(ctx sdk.Context, msg *types.MsgSubmitIntent) (string, error) {
	// all payload-independent preconditions in one place (D-085/G2) — the
	// SAME function the ante prevalidation decorator runs, so an ante-passing
	// submit cannot fail here and strand its optimistic charge.
	env, owner, err := k.ValidateSubmitIntentPreconditions(ctx, msg)
	if err != nil {
		return "", err
	}
	height := uint64(ctx.BlockHeight())
	commitment := env.SpendCommitment

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
		// the declared spend_commitment plus bond (D-025/D-065) — so every
		// refund path returns the stored value, never a recomputation
		// (D-041). The ante values this same commitment+bond, so charge,
		// persisted charged_spend, and DecommitIntent release all agree and
		// the D-059/D-064 commitment-conservation invariant is untouched.
		ChargedSpend: commitment.Add(msg.Bond),
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

// SealSubmitted implements the SPEC §6.3 session boundary: SUBMITTED intents
// transition to SEALED_IN_SESSION with the session id, fixing ordering. Runs in
// EndBlock only (order-then-reveal invariant: the Reveal cannot run before the
// block that executes this completes).
//
// VE size bound (Task 1.6, PHASE1_DESIGN §2 perf FLAG): at most
// max_intents_per_session intents seal into one session (0 = unbounded). Because
// ExtendVote emits one reveal share per in-flight sealed ciphertext, capping the
// sealed set bounds the per-block vote-extension size and the Combine cost. The
// SELECTION is the deterministic §6.3 0x02 state/ttl index order (ttl ascending,
// then id) — now consensus state, identical on every node — so which intents seal
// vs. roll cannot depend on map/insertion order. The overflow stays SUBMITTED and
// seals at the next boundary (natural backpressure; ttl still governs expiry).
func (k Keeper) SealSubmitted(ctx sdk.Context, sessionID uint64) error {
	ids, err := k.intentIDsInState(ctx, types.INTENT_STATE_SUBMITTED, types.MaxTTL)
	if err != nil {
		return err
	}

	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	if limit := params.MaxIntentsPerSession; limit > 0 && uint32(len(ids)) > limit {
		ids = ids[:limit]
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

// revealStatus is the per-intent outcome of one Reveal attempt (Task 1.6).
type revealStatus int

const (
	revealOK           revealStatus = iota // plaintext recovered
	revealInsufficient                     // < t valid shares: leave SEALED, retry
	revealMalformed                        // Combine errored after gating: INVALID
)

// RevealSealed implements the Reveal (SPEC §6.3 ABCI hooks): in BeginBlock,
// every intent sealed in a STRICTLY earlier block (session id = the boundary
// height, D-038) is decrypted and either OPENs, turns INVALID with the §6.3 50%
// slash (malformed payload or D-065 commitment violation), or — under the real
// threshold scheme — falls short of t shares and is retried next block.
//
// Per-intent scheme selection (Task 1.6 cutover, AUDIT F-4): envelope epoch_id==0
// is the StubScheme path (identity Reveal, the epoch-0 bootstrap window — see the
// honest confidentiality note in D-069); epoch_id!=0 uses the real BLS threshold
// scheme built from that epoch's published commits, decrypting from the
// vote-extension shares the PreBlocker collected this block. A session that stays
// short of t shares retries up to max_reveal_retries (counted from the B+2 floor,
// the earliest block a share can exist — see EnumerateInFlightForReveal) then
// CANCELs (PHASE1_DESIGN §2(c)). Decoding runs behind recover — the chain never
// panics on user input (SPEC §11.6) — and the whole path is deterministic
// (committed shares, randomness-free PartialReveal, sorted gate, D-066).
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
	// per-epoch reveal scheme cache (build once per epoch per block); and the set
	// of sessions still holding an unrevealed epoch_id!=0 intent after this block.
	schemes := make(map[uint64]*threshold.Scheme)
	pending := make(map[uint64]struct{})

	for _, id := range ids {
		rec, err := k.GetIntentRecord(ctx, phase0Chamber, id)
		if err != nil {
			return err
		}
		// the order-then-reveal invariant, explicit: ordering must have been
		// fixed in a strictly earlier block. Reveal and ordering can never share
		// a block — and under the real scheme the shares for this ciphertext do
		// not even exist until ExtendVote read its committed seal (F-4).
		if rec.SessionId >= height {
			continue
		}

		var env types.IntentEnvelope
		if err := env.Unmarshal(rec.Intent.Envelope); err != nil {
			// the envelope was validated at submission/genesis import, so a decode
			// failure here is stored-state corruption, not user input.
			return errorsmod.Wrapf(types.ErrInvalidEnvelope,
				"stored envelope of intent %s undecodable: %s", id, err)
		}

		plaintext, status := k.revealIntent(ctx, height, rec, env, schemes)
		switch status {
		case revealInsufficient:
			// not enough shares yet: leave SEALED for the retry sweep below.
			pending[rec.SessionId] = struct{}{}
			continue
		case revealMalformed:
			// §6.3 INVALID path: malformed payload / undecryptable (D-040).
			if err := k.markInvalid(ctx, &rec, params.InvalidSlashBps, types.InvalidReasonMalformedPayload); err != nil {
				return err
			}
			continue
		}

		payload, ok := decodePayload(plaintext)
		if !ok {
			if err := k.markInvalid(ctx, &rec, params.InvalidSlashBps, types.InvalidReasonMalformedPayload); err != nil {
				return err
			}
			continue
		}

		// D-065 commitment cross-check: the ante bound the ceilings to the
		// declared spend_commitment WITHOUT reading give legs (uniform in stub
		// and encrypted mode); the Reveal now reads the real give legs and turns
		// the intent INVALID — exactly like a malformed payload (50% slash,
		// D-040) — if its actual coin give exceeds the commitment in ANY denom
		// (under-declaration). Over/exact declaration and coin-give-free intents
		// (data/svc-only, the F-3 boundary) pass to OPEN.
		exceeds, err := k.giveExceedsCommitment(ctx, rec, payload)
		if err != nil {
			return err
		}
		if exceeds {
			if err := k.markInvalid(ctx, &rec, params.InvalidSlashBps, types.InvalidReasonCommitmentViolation); err != nil {
				return err
			}
			continue
		}

		// persist the recovered plaintext into the record before OPEN: post-Reveal
		// the payload is public (the auction matches on it) and the vote-extension
		// shares are gone, so x/session must read the revealed plaintext, not
		// re-decrypt. For epoch_id==0 (stub identity) plaintext == ciphertext, so
		// this is a no-op; for a real epoch it replaces the ciphertext with the
		// decrypted payload (GetOpenIntent then reads it directly). Deterministic:
		// the same shares yield the same plaintext on every node.
		rec.Intent.Ciphertext = plaintext
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

	if err := k.accountRetriesAndCancel(ctx, height, params.MaxRevealRetries, pending); err != nil {
		return err
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

// revealIntent recovers an intent's plaintext for the Reveal, selecting the
// scheme by envelope epoch_id (Task 1.6 cutover). epoch_id==0 ⇒ StubScheme
// (identity, the bootstrap window); epoch_id!=0 ⇒ the real threshold scheme over
// the epoch's published commits and the vote-extension shares collected this
// block. Runs behind recover so a crafted ciphertext can never panic the chain
// (SPEC §11.6). schemes caches one *threshold.Scheme per epoch for the block.
func (k Keeper) revealIntent(ctx sdk.Context, height uint64, rec types.IntentRecord, env types.IntentEnvelope, schemes map[uint64]*threshold.Scheme) (plaintext []byte, status revealStatus) {
	defer func() {
		if r := recover(); r != nil {
			plaintext, status = nil, revealMalformed
		}
	}()

	if env.EpochId == 0 {
		pt, err := k.scheme.Combine(nil, rec.Intent.Ciphertext)
		if err != nil {
			return nil, revealMalformed
		}
		return pt, revealOK
	}

	scheme, ok := schemes[env.EpochId]
	if !ok {
		if k.epochKeeper == nil {
			return nil, revealInsufficient
		}
		commits, n, found := k.epochKeeper.GetEpochVerification(ctx, env.EpochId)
		if !found {
			// the epoch's key material is unavailable (e.g. retired/failed): the
			// intent can no longer be revealed and will drain via retry→CANCEL.
			return nil, revealInsufficient
		}
		sc, err := threshold.NewSchemeFromCommitBytes(commits, n)
		if err != nil {
			return nil, revealInsufficient
		}
		schemes[env.EpochId] = sc
		scheme = sc
	}

	var shares []reveal.RevealShare
	if k.revealShares != nil {
		shares = k.revealShares.SharesFor(int64(height), rec.Intent.Id)
	}
	pt, err := scheme.Combine(shares, rec.Intent.Ciphertext)
	if err != nil {
		if errors.Is(err, threshold.ErrInsufficientShares) {
			return nil, revealInsufficient
		}
		// ≥ t valid shares but the IBE/AEAD open failed ⇒ the ciphertext is
		// malformed (the FO/CCA integrity check, D-066): INVALID, not a retry.
		return nil, revealMalformed
	}
	return pt, revealOK
}

// accountRetriesAndCancel applies the §2(c) retry→CANCEL policy over the sessions
// that still hold an unrevealed epoch_id!=0 intent after this block's Reveal
// (Task 1.6). It increments a per-session attempt counter — but ONLY from the
// B+2 floor (height >= session_id+2), the earliest block a share can exist
// (ExtendVote(B+1) first sees the seal committed at B, its shares reach B+2), so
// the structurally-impossible B+1 attempt never burns a retry. When the counter
// reaches max_reveal_retries the session's remaining sealed intents CANCEL (bonds
// refunded), which releases epoch rotation (drain-before-rotate). Counters for
// drained sessions are cleared. All inputs are committed state ⇒ deterministic.
func (k Keeper) accountRetriesAndCancel(ctx sdk.Context, height uint64, maxRetries uint32, pending map[uint64]struct{}) error {
	// clear counters for sessions that are no longer pending (fully revealed or
	// already cancelled) — iterate a snapshot of stored counters deterministically.
	for _, s := range k.revealAttemptSessions(ctx) {
		if _, still := pending[s]; !still {
			k.deleteRevealAttempts(ctx, s)
		}
	}

	// deterministic order over pending sessions (§11.6).
	sessionIDs := make([]uint64, 0, len(pending))
	for s := range pending {
		sessionIDs = append(sessionIDs, s)
	}
	sort.Slice(sessionIDs, func(i, j int) bool { return sessionIDs[i] < sessionIDs[j] })

	for _, s := range sessionIDs {
		if height < s+2 { // B+2 floor: reveal not yet structurally possible
			continue
		}
		attempts := k.getRevealAttempts(ctx, s) + 1
		if maxRetries > 0 && attempts >= maxRetries {
			if err := k.cancelSessionSealed(ctx, s, attempts); err != nil {
				return err
			}
			k.deleteRevealAttempts(ctx, s)
			continue
		}
		k.setRevealAttempts(ctx, s, attempts)
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeRevealRetry,
			sdk.NewAttribute(types.AttributeKeySessionID, strconv.FormatUint(s, 10)),
			sdk.NewAttribute(types.AttributeKeyAttempts, strconv.FormatUint(uint64(attempts), 10)),
		))
	}
	return nil
}

// cancelSessionSealed CANCELs every still-SEALED intent of a session whose Reveal
// exhausted max_reveal_retries (PHASE1_DESIGN §2(c)): each intent goes CANCELLED
// with a full bond refund + window decommit (settleRefund, the §5.2 cancel
// settlement), so a permanently-undecryptable session never halts the chain and
// stops counting as in-flight (releasing epoch rotation, seam (a)). The walk is
// the deterministic 0x02 state index (§11.6).
func (k Keeper) cancelSessionSealed(ctx sdk.Context, sessionID uint64, attempts uint32) error {
	ids, err := k.intentIDsInState(ctx, types.INTENT_STATE_SEALED_IN_SESSION, types.MaxTTL)
	if err != nil {
		return err
	}
	for _, id := range ids {
		rec, err := k.GetIntentRecord(ctx, phase0Chamber, id)
		if err != nil {
			return err
		}
		if rec.SessionId != sessionID {
			continue
		}
		if err := k.transitionIntent(ctx, &rec, types.INTENT_STATE_CANCELLED); err != nil {
			return err
		}
		if err := k.settleRefund(ctx, rec, types.EventTypeIntentCancelled); err != nil {
			return err
		}
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeSessionCancelled,
		sdk.NewAttribute(types.AttributeKeySessionID, strconv.FormatUint(sessionID, 10)),
		sdk.NewAttribute(types.AttributeKeyAttempts, strconv.FormatUint(uint64(attempts), 10)),
	))
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

// settleRefund applies the EXPIRED/CANCELLED/CONCLUDED settlement (SPEC §5.2
// "bond refunded"; §6.2 step 5 / §6.4 step 7 window refund): the full bond
// returns to the owner and the stored charged_spend returns to the owner's
// mandate window (a no-op for unbound root keys). The D-041 matrix gives all
// three terminal states identical refunds; only INVALID and ROLLED differ.
//
// CONCLUDED reaches this path via ConcludeIntent, called by x/session inside
// its §6.4 Conclude cache-context (task 0.8), so the refund is part of that
// all-or-nothing settlement write.
func (k Keeper) settleRefund(ctx sdk.Context, rec types.IntentRecord, eventType string) error {
	owner := sdk.MustAccAddressFromBech32(rec.Owner)
	if rec.Intent.Bond.IsPositive() {
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, owner, sdk.NewCoins(rec.Intent.Bond)); err != nil {
			return err
		}
	}
	if err := k.mandateKeeper.DecommitIntent(ctx, rec.Intent.ChamberId, owner, rec.ChargedSpend); err != nil {
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

// markInvalid transitions an intent to INVALID and applies the §6.3 slash
// settlement (D-040). reason records the cause (malformed payload vs
// spend_commitment violation, D-065) for observability; the settlement is
// identical for both.
func (k Keeper) markInvalid(ctx sdk.Context, rec *types.IntentRecord, slashBps uint32, reason string) error {
	if err := k.transitionIntent(ctx, rec, types.INTENT_STATE_INVALID); err != nil {
		return err
	}
	return k.slashInvalid(ctx, *rec, slashBps, reason)
}

// slashInvalid applies the §6.3 INVALID settlement: invalid_slash_bps of
// the bond funds the community pool (D-007: 5000 = 50%), the remainder
// returns to the owner, and the mandate window is deliberately NOT
// refunded (D-040): §6.2 step 5 and §6.4 step 7 name only expired and
// cancelled intents, and keeping the charge preserves D-025's bound on
// malformed-intent spam from a stolen key. The same NO-refund rule bounds a
// commitment-violation INVALID (D-065): an under-declared intent keeps its
// charged commitment+bond, so the conservation invariant stays intact and
// the slash is the deterrent.
func (k Keeper) slashInvalid(ctx sdk.Context, rec types.IntentRecord, slashBps uint32, reason string) error {
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
		sdk.NewAttribute(types.AttributeKeyReason, reason),
	))
	return nil
}

// decodePayload proto-unmarshals a recovered plaintext into an IntentPayload
// behind recover. ok=false is the §6.3 INVALID path ("payload that fails
// proto-unmarshal at Reveal") — this never panics the chain (SPEC §11.6). The
// plaintext itself comes from revealIntent (stub identity or real threshold
// decryption, per epoch_id).
func decodePayload(plaintext []byte) (payload types.IntentPayload, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
		}
	}()
	if err := payload.Unmarshal(plaintext); err != nil {
		return payload, false
	}
	return payload, true
}

// giveExceedsCommitment reports whether a revealed intent's ACTUAL coin give
// legs exceed its declared spend_commitment in any denom — the D-065 Reveal
// cross-check. The comparison iterates the ACTUAL give coins and checks each
// against commitment.AmountOf(denom): a give in a denom the commitment does
// not mention compares against 0 and therefore fires (an undeclared-denom
// give is itself an under-declaration). Both sides are valued in the same
// FaceValuePriceSource units, so the comparison is apples-to-apples;
// non-coin (data/svc) give legs value 0 and never appear in `give`, so a
// coin-give-free intent is correctly unbounded by a coin commitment (the
// inherited F-3 boundary — CONCLAVE-TODO(phase-3): the D-044 price-source
// swap values data/svc legs and would bound them; owned by task 3.4 with the
// swap itself. RE-TAGGED from phase-0 at Rev 3, D-090).
func (k Keeper) giveExceedsCommitment(ctx sdk.Context, rec types.IntentRecord, payload types.IntentPayload) (bool, error) {
	var env types.IntentEnvelope
	if err := env.Unmarshal(rec.Intent.Envelope); err != nil {
		// the envelope was validated at submission/genesis import, so a decode
		// failure here is state corruption, not user input.
		return false, errorsmod.Wrapf(types.ErrInvalidEnvelope,
			"stored envelope of intent %s undecodable: %s", rec.Intent.Id, err)
	}
	give := k.giveLegValue(ctx, payload)
	for _, c := range give {
		if c.Amount.GT(env.SpendCommitment.AmountOf(c.Denom)) {
			return true, nil
		}
	}
	return false, nil
}

// giveLegValue values a decoded payload's give legs per SPEC §6.2 step 4 (the
// per-denom max of give legs via the PriceSource), behind recover so a crafted
// payload can never panic the chain (§11.6). It is the actual-give valuation
// the D-065 Reveal cross-check compares against the declared spend_commitment.
func (k Keeper) giveLegValue(ctx sdk.Context, payload types.IntentPayload) (spend sdk.Coins) {
	defer func() {
		if r := recover(); r != nil {
			spend = sdk.NewCoins()
		}
	}()

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

// faceValueScalar reduces a give-leg valuation to the D-041 face value: the
// denom-blind sum of the per-denom max amounts in base units. It is the only
// deterministic cross-denom-free scalar available here, and it scales the §6.3
// bond with intent size.
//
// CONCLAVE-TODO(phase-3): normalize give legs to the settlement denom via a
// clearing-price source, replacing this approximation — owned by task 3.4,
// the same D-044 swap as `PriceSource` (AUDIT F-3). RE-TAGGED from phase-0 at
// Rev 3 (D-090): the marker said "0.8's clearing-price source", and 0.8 shipped
// without changing this. Cross-denom prices DO exist now — task 2.5's session
// TWAP (D-082) — but 3.4 owns the decision to consume them here, under the
// containment review D-082 recorded.
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
