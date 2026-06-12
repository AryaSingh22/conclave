package types

import errorsmod "cosmossdk.io/errors"

// x/mandate error registry. Stable codespace per SPEC §13: mandate = 12xx.
var (
	ErrMandateNotFound      = errorsmod.Register(ModuleName, 1200, "mandate not found")
	ErrMandateExpired       = errorsmod.Register(ModuleName, 1201, "mandate expired or revoked")
	ErrMsgNotPermitted      = errorsmod.Register(ModuleName, 1202, "msg type not permitted by mandate")
	ErrMandateExceeded      = errorsmod.Register(ModuleName, 1203, "mandate spend ceiling exceeded")
	ErrMsgTooNested         = errorsmod.Register(ModuleName, 1204, "msg nesting exceeds mandate depth cap")
	ErrInvalidMandateID     = errorsmod.Register(ModuleName, 1205, "invalid mandate id")
	ErrInvalidTerms         = errorsmod.Register(ModuleName, 1206, "invalid mandate terms")
	ErrPrivilegeEscalation  = errorsmod.Register(ModuleName, 1207, "msg type can never be mandate-permitted")
	ErrGranteeConflict      = errorsmod.Register(ModuleName, 1208, "grantee key conflicts with an existing sigil binding")
	ErrUnauthorized         = errorsmod.Register(ModuleName, 1209, "unauthorized")
	ErrInvalidParams        = errorsmod.Register(ModuleName, 1210, "invalid params")
	ErrFeePoolInsufficient  = errorsmod.Register(ModuleName, 1211, "fee conversion pool insufficient")
	ErrFeeDenomNotPermitted = errorsmod.Register(ModuleName, 1212, "fee denom not permitted by mandate")
)
