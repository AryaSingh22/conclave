package types

import errorsmod "cosmossdk.io/errors"

// x/escrow error registry. Stable codespace per SPEC §13: escrow = 15xx.
var (
	ErrInvalidID        = errorsmod.Register(ModuleName, 1500, "invalid escrow id")
	ErrEscrowNotFound   = errorsmod.Register(ModuleName, 1501, "escrow not found")
	ErrTerminalState    = errorsmod.Register(ModuleName, 1502, "escrow is in a terminal state")
	ErrPreimageMismatch = errorsmod.Register(ModuleName, 1503, "preimage does not hash to the committed payload hash")
	ErrTimedOut         = errorsmod.Register(ModuleName, 1504, "release window has closed")
	ErrNoDisputePath    = errorsmod.Register(ModuleName, 1505, "condition class has no dispute path")
	ErrDisputeWindow    = errorsmod.Register(ModuleName, 1506, "dispute window has closed")
	ErrAlreadyDisputed  = errorsmod.Register(ModuleName, 1507, "escrow is already under dispute")
	ErrNotDisputed      = errorsmod.Register(ModuleName, 1508, "escrow is not under dispute")
	ErrWrongClass       = errorsmod.Register(ModuleName, 1509, "message does not match the escrow's condition class")
	ErrZkNotEnabled     = errorsmod.Register(ModuleName, 1510, "ZK_DELIVERY is not enabled until task 2.1b")
	ErrUnauthorized     = errorsmod.Register(ModuleName, 1511, "signer is not authorized for this transition")
	ErrInvalidEscrow    = errorsmod.Register(ModuleName, 1512, "invalid escrow record")
	ErrInsufficientBond = errorsmod.Register(ModuleName, 1513, "seller cannot fund the dispute bond")
)
