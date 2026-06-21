package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// EndBlocker drives the epoch lifecycle once per block (PHASE1_DESIGN §2(b),
// D-068). All three steps are deterministic functions of committed state:
//
//  1. finalize the FORMING epoch's COMMITTEE at the register-window close (the
//     registered subset of candidates → dense dkg_index + threshold, or FAILED
//     if too few registered — Task 1.7 A2, D-070);
//  2. finalize the FORMING epoch if its confirm window has closed (publish the
//     agreed key → ACTIVE + rotate, or FAILED → no rotation);
//  3. retire any DRAINING epoch whose in-flight ciphertexts have all drained
//     (the rotation release, gated on drain — seam (a));
//  4. open a new FORMING epoch at an epoch boundary (candidates = the staking-
//     finalized bonded set; committee is the subset that registers).
//
// It is registered AFTER staking in the EndBlocker order (app_config.go) so the
// boundary snapshot reads the validator set staking has committed for the block.
// The chain never halts: every degraded path (too few validators, failed DKG,
// faulty dealer, stuck drain) leaves the prior key live and continues.
func (k Keeper) EndBlocker(ctx sdk.Context) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}

	height := uint64(ctx.BlockHeight())
	if formingID := k.FormingEpochID(ctx); formingID != 0 {
		if e, ok := k.GetEpoch(ctx, formingID); ok {
			// committee finalization at the register-window close (once).
			if height >= registerDeadline(e, params) && !k.IsCommitteeFinalized(ctx, formingID) {
				k.FinalizeCommittee(ctx, e, params)
				// FinalizeCommittee may have FAILEDd the epoch (too few registrants)
				// and cleared the forming id; re-read for the confirm step below.
				e, _ = k.GetEpoch(ctx, formingID)
			}
			// key finalization at the confirm-window close (only if still forming).
			if k.FormingEpochID(ctx) == formingID && height >= e.ConfirmDeadline {
				k.FinalizeForming(ctx, e)
			}
		}
	}

	k.RetireDrained(ctx)
	k.MaybeOpenEpoch(ctx, params)
	return nil
}
