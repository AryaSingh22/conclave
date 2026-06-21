package types

import errorsmod "cosmossdk.io/errors"

// x/epoch error registry. New module codespace = 16xx (sigil 11xx · mandate
// 12xx · veil 13xx · session 14xx · escrow 15xx · epoch 16xx; D-068).
var (
	ErrInvalidParams     = errorsmod.Register(ModuleName, 1600, "invalid epoch params")
	ErrUnauthorized      = errorsmod.Register(ModuleName, 1601, "unauthorized")
	ErrNoFormingEpoch    = errorsmod.Register(ModuleName, 1602, "no FORMING epoch is open for DKG transport")
	ErrEpochMismatch     = errorsmod.Register(ModuleName, 1603, "message epoch id does not match the FORMING epoch")
	ErrWindowClosed      = errorsmod.Register(ModuleName, 1604, "DKG transport window is closed for this round")
	ErrNotCommittee      = errorsmod.Register(ModuleName, 1605, "sender is not the committee member at this dkg index")
	ErrDuplicateBundle   = errorsmod.Register(ModuleName, 1606, "a bundle was already submitted for this dkg index")
	ErrBundleTooLarge    = errorsmod.Register(ModuleName, 1607, "DKG bundle exceeds max_bundle_bytes")
	ErrInvalidBundle     = errorsmod.Register(ModuleName, 1608, "malformed DKG bundle")
	ErrEpochNotFound     = errorsmod.Register(ModuleName, 1609, "epoch not found")
	ErrInvalidGenesis    = errorsmod.Register(ModuleName, 1610, "invalid epoch genesis state")
	ErrNotCandidate      = errorsmod.Register(ModuleName, 1611, "sender is not a bonded candidate for this FORMING epoch")
	ErrDuplicateRegister = errorsmod.Register(ModuleName, 1612, "a DKG key was already registered for this validator")
)
