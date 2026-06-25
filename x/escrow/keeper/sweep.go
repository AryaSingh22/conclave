package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/escrow/types"
)

// EndBlocker runs the deterministic timeout sweep (SPEC §6.5 / §11.6). It
// iterates the 0x03 sweep index for every entry whose sweep height is ≤ the
// current block height, in ascending (height, id) order, and fires the due
// transition for each: HASHLOCK timeout → REFUNDED; OPTIMISTIC window elapsed
// (undisputed) → RELEASED; OPTIMISTIC dispute-resolution deadline (seller never
// proved delivery) → REFUNDED + bond slash + reputation penalty. The order is
// fixed by the sorted index, never by map iteration, so every node sweeps
// identically. A panic on any malformed record is recovered into an error
// rather than halting the node (§11.5).
//
// implements SPEC §6.5 escrow timeout sweeps.
func (k Keeper) EndBlocker(ctx sdk.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("x/escrow EndBlock sweep recovered: %v", r)
		}
	}()

	h := uint64(ctx.BlockHeight())
	store := k.storeService.OpenKVStore(ctx)

	// [0x03, 0x03|h+1|0…0) is every entry with sweep height ≤ h. Collect the
	// ids first (ascending), then process — mutating the store while iterating
	// it is unsafe.
	end := types.SweepIdxKey(h+1, make([]byte, types.IDLength))
	it, err := store.Iterator(types.SweepIdxPrefix, end)
	if err != nil {
		return err
	}
	var ids [][]byte
	for ; it.Valid(); it.Next() {
		key := it.Key()
		idBz := make([]byte, types.IDLength)
		copy(idBz, key[len(types.SweepIdxPrefix)+8:])
		ids = append(ids, idBz)
	}
	if err := it.Close(); err != nil {
		return err
	}

	for _, idBz := range ids {
		if err := k.sweepOne(ctx, idBz); err != nil {
			return err
		}
	}
	return nil
}

// sweepOne fires the due transition for one escrow id, dispatching on its
// condition class and current state. Every outcome is terminal (RELEASED or
// REFUNDED), so no new sweep entry is produced — the sweep cannot loop.
func (k Keeper) sweepOne(ctx sdk.Context, idBz []byte) error {
	esc, err := k.GetEscrow(ctx, phase0Chamber, types.IDHex(idBz))
	if err != nil {
		return err
	}
	class := classOf(esc)

	switch {
	case class == classHashlock && esc.State == types.ESCROW_STATE_ACTIVE:
		to, err := nextState(classHashlock, esc.State, evtHashlockTimeout)
		if err != nil {
			return err
		}
		return k.finalizeRefund(ctx, esc, classHashlock, to)

	case class == classOptimistic && esc.State == types.ESCROW_STATE_ACTIVE:
		to, err := nextState(classOptimistic, esc.State, evtOptimisticAutoRelease)
		if err != nil {
			return err
		}
		return k.finalizeRelease(ctx, esc, classOptimistic, to)

	case class == classOptimistic && esc.State == types.ESCROW_STATE_DISPUTED:
		to, err := nextState(classOptimistic, esc.State, evtOptimisticResolve)
		if err != nil {
			return err
		}
		return k.finalizeRefund(ctx, esc, classOptimistic, to)

	default:
		// A sweep entry for a non-sweepable (terminal/NONE) escrow should never
		// exist — terminal transitions delete their entry. Drop it defensively
		// rather than error the whole block.
		return k.deleteSweepIdx(ctx, esc)
	}
}
