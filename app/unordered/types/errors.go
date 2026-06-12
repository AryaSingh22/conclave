package types

import errorsmod "cosmossdk.io/errors"

// Unordered-tx error registry. The codespace extends the SPEC §13 ladder
// (sigil=11xx … escrow=15xx) with unordered=16xx for the app-level layer
// (D-030).
var (
	ErrTxReplayed     = errorsmod.Register(ModuleName, 1600, "unordered tx already processed")
	ErrInvalidTimeout = errorsmod.Register(ModuleName, 1601, "unordered tx timeout_timestamp must be after block time")
	ErrTTLTooLong     = errorsmod.Register(ModuleName, 1602, "unordered tx timeout_timestamp exceeds the max TTL")
	ErrInvalidExt     = errorsmod.Register(ModuleName, 1603, "malformed unordered tx extension")
)
