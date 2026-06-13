package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// nextBoundary returns the smallest session boundary height strictly greater
// than h for cadence k (the id the next session takes, D-038). Rolled intents
// re-home here; the carryover session opens at that boundary.
func nextBoundary(h, k uint64) uint64 {
	if k == 0 {
		return h + 1
	}
	return (h/k + 1) * k
}

// rolloverSession implements the SPEC §6.4 step-5 rollover sweep: every intent
// of a just-closed session still OPEN (unmatched) and not yet at its ttl rolls
// into the next session via x/veil's RollIntent (OPEN→ROLLED→OPEN, charge
// PERSISTS — D-041). ttl-reached intents are left OPEN for x/veil's expiry sweep
// (D-038: it runs immediately after this, in veil EndBlock) so they EXPIRE with
// a full refund rather than rolling forever. The carryover session is
// pre-created/extended in FORMING and opened to AUCTION at its boundary.
func (k Keeper) rolloverSession(ctx sdk.Context, session types.Session, params types.Params) error {
	height := uint64(ctx.BlockHeight())
	carryoverID := nextBoundary(height, params.SessionLengthBlocks)

	for _, id := range session.IntentIds {
		rec, _, err := k.veil().GetOpenIntent(ctx, phase0Chamber, id)
		if err != nil {
			continue // not OPEN (concluded this session, or already rolled) — skip
		}
		ttl, ok := envelopeTTL(rec.Intent.Envelope)
		if !ok || ttl <= height {
			continue // ttl reached (or undecodable) — leave for the veil expiry sweep
		}
		if err := k.appendToCarryover(ctx, carryoverID, id); err != nil {
			return err
		}
		if err := k.veil().RollIntent(ctx, id, carryoverID); err != nil {
			return err
		}
	}
	return nil
}

// appendToCarryover adds a rolled intent id to the next session's FORMING set,
// creating that session in FORMING if it does not exist yet. The session opens
// to AUCTION at its boundary (IntakeRevealed if new intents reveal into it, else
// openMatureFormingSessions).
func (k Keeper) appendToCarryover(ctx sdk.Context, carryoverID uint64, intentID string) error {
	height := uint64(ctx.BlockHeight())
	s, err := k.GetSession(ctx, phase0Chamber, carryoverID)
	if err != nil {
		s = types.Session{
			Id:          carryoverID,
			ChamberId:   phase0Chamber,
			State:       types.SESSION_STATE_FORMING,
			StartHeight: height,
			IntentIds:   []string{intentID},
		}
		return k.setSession(ctx, s, types.SESSION_STATE_UNSPECIFIED)
	}
	s.IntentIds = append(s.IntentIds, intentID)
	return k.setSession(ctx, s, s.State)
}

// envelopeTTL decodes the ttl_height from a stored intent envelope. The
// envelope was validated at submission, so a decode failure is treated as
// "no ttl" (the intent is left for the expiry path), never a panic (§11.6).
func envelopeTTL(envelopeBz []byte) (uint64, bool) {
	var env veiltypes.IntentEnvelope
	if err := env.Unmarshal(envelopeBz); err != nil {
		return 0, false
	}
	return env.TtlHeight, true
}
