package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// EndBlocker drives the epoch lifecycle once per block (PHASE1_DESIGN §2(b),
// D-068). All three steps are deterministic functions of committed state:
//
//  1. finalize the FORMING epoch if its confirm window has closed (publish the
//     agreed key → ACTIVE + rotate, or FAILED → no rotation);
//  2. retire any DRAINING epoch whose in-flight ciphertexts have all drained
//     (the rotation release, gated on drain — seam (a));
//  3. open a new FORMING epoch at an epoch boundary (committee = the staking-
//     finalized bonded set).
//
// It is registered AFTER staking in the EndBlocker order (app_config.go) so the
// boundary snapshot reads the validator set staking has committed for the block.
// The chain never halts: every degraded path (too few validators, failed DKG,
// stuck drain) leaves the prior key live and continues.
func (k Keeper) EndBlocker(ctx sdk.Context) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}

	height := uint64(ctx.BlockHeight())
	if formingID := k.FormingEpochID(ctx); formingID != 0 {
		if e, ok := k.GetEpoch(ctx, formingID); ok && height >= e.ConfirmDeadline {
			k.FinalizeForming(ctx, e)
		}
	}

	k.RetireDrained(ctx)
	k.MaybeOpenEpoch(ctx, params)
	return nil
}
