package types

import errorsmod "cosmossdk.io/errors"

// x/sigil error registry. Stable codespace per SPEC §13: sigil = 11xx.
var (
	ErrSigilNotFound      = errorsmod.Register(ModuleName, 1100, "sigil not found")
	ErrSigilExists        = errorsmod.Register(ModuleName, 1101, "address already controls a sigil")
	ErrInvalidSigilID     = errorsmod.Register(ModuleName, 1102, "invalid sigil id")
	ErrUnauthorized       = errorsmod.Register(ModuleName, 1103, "unauthorized")
	ErrInvalidRecoverySet = errorsmod.Register(ModuleName, 1104, "invalid recovery set")
	ErrCredentialNotFound = errorsmod.Register(ModuleName, 1105, "credential not found")
	ErrInvalidExpiry      = errorsmod.Register(ModuleName, 1106, "invalid expiry height")
	ErrInvalidDIDDocHash  = errorsmod.Register(ModuleName, 1107, "invalid did document hash")
	ErrInvalidParams      = errorsmod.Register(ModuleName, 1108, "invalid params")
	ErrCredentialRevoked  = errorsmod.Register(ModuleName, 1109, "credential already revoked")
)
