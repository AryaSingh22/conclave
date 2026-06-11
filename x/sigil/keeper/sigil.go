package keeper

import (
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/sigil/types"
)

// SetSigil stores a sigil record. Implements SPEC §6.1 (key 0x01|chamber|sigil_id).
func (k Keeper) SetSigil(ctx sdk.Context, sigil types.Sigil) error {
	idBz, err := types.SigilIDBytes(sigil.Id)
	if err != nil {
		return err
	}
	bz, err := k.cdc.Marshal(&sigil)
	if err != nil {
		return err
	}
	return k.storeService.OpenKVStore(ctx).Set(types.SigilKey(sigil.ChamberId, idBz), bz)
}

// GetSigil returns a sigil by chamber and id. Implements SPEC §6.1.
func (k Keeper) GetSigil(ctx sdk.Context, chamberID uint32, id string) (types.Sigil, error) {
	idBz, err := types.SigilIDBytes(id)
	if err != nil {
		return types.Sigil{}, err
	}
	bz, err := k.storeService.OpenKVStore(ctx).Get(types.SigilKey(chamberID, idBz))
	if err != nil {
		return types.Sigil{}, err
	}
	if bz == nil {
		return types.Sigil{}, errorsmod.Wrapf(types.ErrSigilNotFound, "id %s", id)
	}
	var sigil types.Sigil
	if err := k.cdc.Unmarshal(bz, &sigil); err != nil {
		return types.Sigil{}, err
	}
	return sigil, nil
}

// SetAddressIndex writes the addr → sigil_id reverse index entry
// (SPEC §6.1 key 0x04). Also used by x/mandate to register delegate keys
// (task 0.4).
func (k Keeper) SetAddressIndex(ctx sdk.Context, chamberID uint32, addr sdk.AccAddress, sigilID string) error {
	return k.storeService.OpenKVStore(ctx).Set(types.AddrIndexKey(chamberID, addr), []byte(sigilID))
}

// removeAddressIndex deletes a reverse index entry.
func (k Keeper) removeAddressIndex(ctx sdk.Context, chamberID uint32, addr sdk.AccAddress) error {
	return k.storeService.OpenKVStore(ctx).Delete(types.AddrIndexKey(chamberID, addr))
}

// SigilIDByAddress resolves an address to its sigil id via the reverse index.
// Implements SPEC §6.1 (key 0x04). Settlement-critical: SettleLeg sigils are
// resolved to addresses through this index at Conclude time (D-006).
func (k Keeper) SigilIDByAddress(ctx sdk.Context, chamberID uint32, addr sdk.AccAddress) (string, error) {
	bz, err := k.storeService.OpenKVStore(ctx).Get(types.AddrIndexKey(chamberID, addr))
	if err != nil {
		return "", err
	}
	if bz == nil {
		return "", errorsmod.Wrapf(types.ErrSigilNotFound, "no sigil for address %s", addr)
	}
	return string(bz), nil
}

// ControllerAddress resolves a sigil id to its current controller address —
// the settlement-side counterpart of SigilIDByAddress.
func (k Keeper) ControllerAddress(ctx sdk.Context, chamberID uint32, sigilID string) (sdk.AccAddress, error) {
	sigil, err := k.GetSigil(ctx, chamberID, sigilID)
	if err != nil {
		return nil, err
	}
	return sdk.AccAddressFromBech32(sigil.ControllerAddr)
}

// RegisterSigil registers a new sigil for the controller and initializes its
// reputation at zero. Implements SPEC §6.1 MsgRegisterSigil semantics.
// One sigil per controller address per chamber (D-010): the reverse index is
// single-valued, which is what makes the §6.1 "resolves to exactly one
// Sigil" invariant possible.
func (k Keeper) RegisterSigil(
	ctx sdk.Context,
	controller sdk.AccAddress,
	didDocHash []byte,
	metadataURI string,
	recoverySet []string,
	recoveryThreshold uint32,
	chamberID uint32,
) (string, error) {
	if len(didDocHash) != 32 {
		return "", errorsmod.Wrapf(types.ErrInvalidDIDDocHash, "got %d bytes, want 32", len(didDocHash))
	}
	if int(recoveryThreshold) > len(recoverySet) {
		return "", errorsmod.Wrapf(types.ErrInvalidRecoverySet,
			"threshold %d exceeds recovery set size %d", recoveryThreshold, len(recoverySet))
	}
	seen := make(map[string]bool, len(recoverySet))
	for _, r := range recoverySet {
		if _, err := sdk.AccAddressFromBech32(r); err != nil {
			return "", errorsmod.Wrapf(types.ErrInvalidRecoverySet, "bad address %s", r)
		}
		if seen[r] {
			return "", errorsmod.Wrapf(types.ErrInvalidRecoverySet, "duplicate address %s", r)
		}
		seen[r] = true
	}

	// the reverse index is single-valued: a key controls at most one sigil
	if _, err := k.SigilIDByAddress(ctx, chamberID, controller); err == nil {
		return "", errorsmod.Wrapf(types.ErrSigilExists, "address %s", controller)
	}

	seq, err := k.nextIDSequence(ctx)
	if err != nil {
		return "", err
	}
	id := deriveID(0x01, chamberID, controller.Bytes(), seq)

	sigil := types.Sigil{
		Id:                id,
		ControllerAddr:    controller.String(),
		DidDocHash:        didDocHash,
		MetadataUri:       metadataURI,
		RecoverySet:       recoverySet,
		RecoveryThreshold: recoveryThreshold,
		CreatedHeight:     uint64(ctx.BlockHeight()),
		ChamberId:         chamberID,
	}
	if err := k.SetSigil(ctx, sigil); err != nil {
		return "", err
	}
	if err := k.SetAddressIndex(ctx, chamberID, controller, id); err != nil {
		return "", err
	}
	if err := k.SetReputation(ctx, types.Reputation{
		SigilId:         id,
		Score:           math.LegacyZeroDec(),
		LastDecayHeight: uint64(ctx.BlockHeight()),
		ChamberId:       chamberID,
	}); err != nil {
		return "", err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeRegisterSigil,
		sdk.NewAttribute(types.AttributeKeySigilID, id),
		sdk.NewAttribute(types.AttributeKeyController, controller.String()),
	))
	return id, nil
}

// RotateController replaces the controller of a sigil and atomically moves
// the reverse index entry. Implements SPEC §6.1 MsgRotateController:
// authorized by the current controller OR >= recovery_threshold distinct
// recovery-set members (all signers are signature-verified in the ante).
func (k Keeper) RotateController(
	ctx sdk.Context,
	chamberID uint32,
	sigilID string,
	signers []sdk.AccAddress,
	newController sdk.AccAddress,
) error {
	sigil, err := k.GetSigil(ctx, chamberID, sigilID)
	if err != nil {
		return err
	}

	if !k.rotationAuthorized(sigil, signers) {
		return errorsmod.Wrapf(types.ErrUnauthorized,
			"rotation requires the current controller or %d recovery signatures", sigil.RecoveryThreshold)
	}

	// the new controller must not already control another sigil (D-010)
	if existing, err := k.SigilIDByAddress(ctx, chamberID, newController); err == nil && existing != sigilID {
		return errorsmod.Wrapf(types.ErrSigilExists, "new controller %s already controls sigil %s", newController, existing)
	}

	oldController, err := sdk.AccAddressFromBech32(sigil.ControllerAddr)
	if err != nil {
		return err
	}
	if err := k.removeAddressIndex(ctx, chamberID, oldController); err != nil {
		return err
	}
	sigil.ControllerAddr = newController.String()
	if err := k.SetSigil(ctx, sigil); err != nil {
		return err
	}
	if err := k.SetAddressIndex(ctx, chamberID, newController, sigilID); err != nil {
		return err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeRotateController,
		sdk.NewAttribute(types.AttributeKeySigilID, sigilID),
		sdk.NewAttribute(types.AttributeKeyController, newController.String()),
	))
	return nil
}

// rotationAuthorized implements the SPEC §6.1 rotation rule: the current
// controller alone, or >= recovery_threshold distinct recovery-set members.
func (k Keeper) rotationAuthorized(sigil types.Sigil, signers []sdk.AccAddress) bool {
	recovery := make(map[string]bool, len(sigil.RecoverySet))
	for _, r := range sigil.RecoverySet {
		recovery[r] = true
	}

	distinctRecovery := make(map[string]bool, len(signers))
	for _, s := range signers {
		addr := s.String()
		if addr == sigil.ControllerAddr {
			return true
		}
		if recovery[addr] {
			distinctRecovery[addr] = true
		}
	}
	return sigil.RecoveryThreshold > 0 && uint32(len(distinctRecovery)) >= sigil.RecoveryThreshold
}

// IterateSigils walks all sigils (all chambers), stopping when cb returns true.
func (k Keeper) IterateSigils(ctx sdk.Context, cb func(types.Sigil) bool) error {
	store := k.storeService.OpenKVStore(ctx)
	it, err := store.Iterator(types.SigilKeyPrefix, append(types.SigilKeyPrefix, 0xFF))
	if err != nil {
		return err
	}
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var sigil types.Sigil
		if err := k.cdc.Unmarshal(it.Value(), &sigil); err != nil {
			return err
		}
		if cb(sigil) {
			break
		}
	}
	return nil
}

// IterateAddressIndex walks all reverse index entries (addr → sigil_id).
func (k Keeper) IterateAddressIndex(ctx sdk.Context, cb func(addr sdk.AccAddress, sigilID string, chamberID uint32) bool) error {
	store := k.storeService.OpenKVStore(ctx)
	it, err := store.Iterator(types.AddrIndexKeyPrefix, append(types.AddrIndexKeyPrefix, 0xFF))
	if err != nil {
		return err
	}
	defer it.Close()
	for ; it.Valid(); it.Next() {
		key := it.Key()
		// key = 0x04 | chamber(4) | addr
		chamberID := uint32(key[1])<<24 | uint32(key[2])<<16 | uint32(key[3])<<8 | uint32(key[4])
		addr := sdk.AccAddress(key[5:])
		if cb(addr, string(it.Value()), chamberID) {
			break
		}
	}
	return nil
}
