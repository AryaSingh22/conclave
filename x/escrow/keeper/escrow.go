package keeper

import (
	"strconv"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/escrow/types"
)

// GetEscrow returns an escrow record by id, or ErrEscrowNotFound.
//
// implements SPEC §6.5 escrow lookup.
func (k Keeper) GetEscrow(ctx sdk.Context, chamberID uint32, id string) (types.Escrow, error) {
	idBz, err := types.IDBytes(id)
	if err != nil {
		return types.Escrow{}, err
	}
	bz, err := k.storeService.OpenKVStore(ctx).Get(types.EscrowKey(chamberID, idBz))
	if err != nil {
		return types.Escrow{}, err
	}
	if bz == nil {
		return types.Escrow{}, errorsmod.Wrapf(types.ErrEscrowNotFound, "escrow %s", id)
	}
	var esc types.Escrow
	if err := k.cdc.Unmarshal(bz, &esc); err != nil {
		return types.Escrow{}, err
	}
	return esc, nil
}

// setEscrow writes an escrow record (no index maintenance — callers manage the
// participant and sweep indexes explicitly so each transition is auditable).
func (k Keeper) setEscrow(ctx sdk.Context, esc types.Escrow) error {
	idBz, err := types.IDBytes(esc.Id)
	if err != nil {
		return err
	}
	bz, err := k.cdc.Marshal(&esc)
	if err != nil {
		return err
	}
	return k.storeService.OpenKVStore(ctx).Set(types.EscrowKey(esc.ChamberId, idBz), bz)
}

// IterateEscrows visits every escrow record in deterministic key order
// (chamber-then-id; the 0x01 prefix is iterated ascending). Used by genesis
// export.
func (k Keeper) IterateEscrows(ctx sdk.Context, fn func(types.Escrow) (stop bool)) error {
	it, err := k.storeService.OpenKVStore(ctx).Iterator(types.EscrowKeyPrefix, storePrefixEnd(types.EscrowKeyPrefix))
	if err != nil {
		return err
	}
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var esc types.Escrow
		if err := k.cdc.Unmarshal(it.Value(), &esc); err != nil {
			return err
		}
		if fn(esc) {
			break
		}
	}
	return nil
}

// setCommit persists the OPTIMISTIC committed content hash side-record.
func (k Keeper) setCommit(ctx sdk.Context, idBz, contentHash []byte) error {
	return k.storeService.OpenKVStore(ctx).Set(types.CommitKey(idBz), contentHash)
}

// getCommit reads the OPTIMISTIC committed content hash side-record.
func (k Keeper) getCommit(ctx sdk.Context, idBz []byte) ([]byte, error) {
	bz, err := k.storeService.OpenKVStore(ctx).Get(types.CommitKey(idBz))
	if err != nil {
		return nil, err
	}
	if bz == nil {
		return nil, errorsmod.Wrap(types.ErrInvalidEscrow, "missing committed content hash")
	}
	return bz, nil
}

// conditionWindow returns the per-escrow timeout/dispute window (blocks) for an
// escrow's condition class and whether the class is swept at all. NONE has no
// timer; ZK is rejected upstream (and here, defensively).
func conditionWindow(esc types.Escrow) (window uint64, swept bool, err error) {
	switch classOf(esc) {
	case classHashlock:
		return esc.Condition.GetHashlock().TimeoutBlocks, true, nil
	case classOptimistic:
		return esc.Condition.GetOptimistic().DisputeWindowBlocks, true, nil
	case classNone:
		return 0, false, nil
	default: // classZk
		return 0, false, types.ErrZkNotEnabled
	}
}

// CreateEscrow records a conditioned-delivery escrow and moves its funds into
// the escrow module account: the buyer's (payer's) escrowed payment, plus —
// for OPTIMISTIC — the seller's (payee's) dispute bond. It is called by
// x/session CONCLUDE inside the atomic cache context (D-073 ownership handoff:
// x/escrow is the writer, x/session no longer touches 0x09). For a NONE
// condition it writes the Phase-0 fund-less DataGrant stub record unchanged.
// HASHLOCK/OPTIMISTIC escrows enter ACTIVE with a sweep-index entry at their
// timeout/window height; the OPTIMISTIC committed content hash (the
// buyer-agreed DataGrant.ContentHash) is persisted for the dispute-reveal
// check.
//
// implements SPEC §6.5 escrow instantiation (the §6.4 "DataGrant legs
// instantiate x/escrow entries" handoff).
func (k Keeper) CreateEscrow(ctx sdk.Context, esc types.Escrow, payerAddr, payeeAddr sdk.AccAddress, contentHash []byte) error {
	class := classOf(esc)
	if class == classZk {
		return types.ErrZkNotEnabled
	}
	idBz, err := types.IDBytes(esc.Id)
	if err != nil {
		return err
	}

	window, swept, err := conditionWindow(esc)
	if err != nil {
		return err
	}
	if swept && window == 0 {
		return errorsmod.Wrapf(types.ErrInvalidEscrow, "escrow %s: zero timeout/dispute window", esc.Id)
	}

	esc.State = types.ESCROW_STATE_ACTIVE
	esc.CreatedHeight = uint64(ctx.BlockHeight())
	if swept {
		esc.TimeoutHeight = esc.CreatedHeight + window
	}

	// Fund the escrow with the buyer's payment.
	if !esc.Amount.Empty() {
		if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, payerAddr, types.ModuleName, esc.Amount); err != nil {
			return err
		}
	}

	// OPTIMISTIC: the seller posts the dispute bond and we persist the
	// buyer-agreed committed content hash for the dispute-reveal check.
	if class == classOptimistic {
		bond := types.SellerBondCoins(esc.Amount)
		if !bond.IsZero() {
			if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, payeeAddr, types.ModuleName, bond); err != nil {
				return errorsmod.Wrap(types.ErrInsufficientBond, err.Error())
			}
		}
		if err := k.setCommit(ctx, idBz, contentHash); err != nil {
			return err
		}
	}

	if err := k.setEscrow(ctx, esc); err != nil {
		return err
	}

	store := k.storeService.OpenKVStore(ctx)
	// Participant index for both sides (escrows stay queryable through
	// terminal states — never deleted, spirit of D-017).
	if err := store.Set(types.SigilIdxKey(esc.PayerSigil, idBz), []byte{}); err != nil {
		return err
	}
	if err := store.Set(types.SigilIdxKey(esc.PayeeSigil, idBz), []byte{}); err != nil {
		return err
	}
	// Deterministic sweep index at the timeout/window height.
	if swept {
		if err := store.Set(types.SweepIdxKey(esc.TimeoutHeight, idBz), []byte{}); err != nil {
			return err
		}
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeEscrowOpened,
		sdk.NewAttribute(types.AttributeKeyEscrowID, esc.Id),
		sdk.NewAttribute(types.AttributeKeySessionID, strconv.FormatUint(esc.SessionId, 10)),
	))
	return nil
}

// storePrefixEnd returns the exclusive end key for a prefix scan (the prefix
// with its last byte incremented; nil if the prefix is all 0xff).
func storePrefixEnd(prefix []byte) []byte {
	end := make([]byte, len(prefix))
	copy(end, prefix)
	for i := len(end) - 1; i >= 0; i-- {
		end[i]++
		if end[i] != 0 {
			return end[:i+1]
		}
	}
	return nil
}
