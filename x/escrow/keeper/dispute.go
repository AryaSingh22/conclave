package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/escrow/types"
)

// RaiseDispute executes the OPTIMISTIC ACTIVE→DISPUTED transition. It must be
// raised in-window (height < the dispute-window end, which is the ACTIVE
// escrow's TimeoutHeight). On success the sweep index moves from the
// auto-release window-end to a dispute-resolution deadline one dispute-window
// further out — the seller's window to prove delivery (RevealOptimistic). If
// the seller never proves it, the resolution sweep refunds the buyer and
// slashes the bond (the proven-non-delivery path).
//
// implements SPEC §6.5 OPTIMISTIC dispute (D5: hash-mismatch challenge with a
// seller bond, not subjective arbitration).
func (k Keeper) RaiseDispute(ctx sdk.Context, esc types.Escrow) error {
	if uint64(ctx.BlockHeight()) >= esc.TimeoutHeight {
		return types.ErrDisputeWindow
	}
	to, err := nextState(classOptimistic, esc.State, evtOptimisticDispute)
	if err != nil {
		return err
	}
	idBz, err := types.IDBytes(esc.Id)
	if err != nil {
		return err
	}
	store := k.storeService.OpenKVStore(ctx)

	// Move the sweep entry: delete the auto-release window-end entry, add the
	// dispute-resolution entry. The resolution window equals the dispute
	// window (the seller gets as long to prove delivery as the buyer had to
	// dispute).
	if err := store.Delete(types.SweepIdxKey(esc.TimeoutHeight, idBz)); err != nil {
		return err
	}
	window := esc.Condition.GetOptimistic().DisputeWindowBlocks
	esc.TimeoutHeight = uint64(ctx.BlockHeight()) + window
	esc.State = to
	if err := k.setEscrow(ctx, esc); err != nil {
		return err
	}
	if err := store.Set(types.SweepIdxKey(esc.TimeoutHeight, idBz), []byte{}); err != nil {
		return err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeEscrowDisputed,
		sdk.NewAttribute(types.AttributeKeyEscrowID, esc.Id),
		sdk.NewAttribute(types.AttributeKeyState, esc.State.String()),
	))
	return nil
}
