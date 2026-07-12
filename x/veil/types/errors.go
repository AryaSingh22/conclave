package types

import errorsmod "cosmossdk.io/errors"

// x/veil error registry. Stable codespace per SPEC §13: veil = 13xx.
var (
	ErrIntentNotFound       = errorsmod.Register(ModuleName, 1300, "intent not found")
	ErrInvalidIntentID      = errorsmod.Register(ModuleName, 1301, "invalid intent id")
	ErrInvalidEnvelope      = errorsmod.Register(ModuleName, 1302, "invalid intent envelope")
	ErrInvalidBond          = errorsmod.Register(ModuleName, 1303, "invalid intent bond")
	ErrBondInsufficient     = errorsmod.Register(ModuleName, 1304, "bond below SPEC §6.3 minimum")
	ErrSigilMismatch        = errorsmod.Register(ModuleName, 1305, "sender does not resolve to sender_sigil")
	ErrNotIntentOwner       = errorsmod.Register(ModuleName, 1306, "sender does not own the intent")
	ErrIntentNotCancellable = errorsmod.Register(ModuleName, 1307, "intent is not in a pre-OPEN state")
	ErrIllegalTransition    = errorsmod.Register(ModuleName, 1308, "illegal intent state transition")
	ErrInvalidParams        = errorsmod.Register(ModuleName, 1309, "invalid params")
	ErrUnauthorized         = errorsmod.Register(ModuleName, 1310, "unauthorized")
	ErrInvalidChamber       = errorsmod.Register(ModuleName, 1311, "chamber_id must be 0 until Phase 3")
	ErrIntentNotOpen        = errorsmod.Register(ModuleName, 1312, "intent is not OPEN")
	ErrEpochNotDecryptable  = errorsmod.Register(ModuleName, 1313, "envelope epoch_id is not a currently-decryptable epoch")
	ErrInvalidVoteExtension = errorsmod.Register(ModuleName, 1314, "invalid reveal vote extension")
	ErrEpochMaterial        = errorsmod.Register(ModuleName, 1315, "epoch verification material unavailable for reveal")
	// ErrBondFundsInsufficient mirrors the bond-escrow SendCoins failure as an
	// up-front precondition (D-085/G2): distinct from ErrBondInsufficient (bond
	// below the §6.3 minimum), this is "the submitter cannot fund the bond it
	// declared". Surfacing it as a precondition lets the ante reject the submit
	// BEFORE the §6.2 window charge, so a submit that would fail the escrow in
	// the msg server never strands its optimistic charge.
	ErrBondFundsInsufficient = errorsmod.Register(ModuleName, 1316, "insufficient spendable funds to escrow the bond")
)
