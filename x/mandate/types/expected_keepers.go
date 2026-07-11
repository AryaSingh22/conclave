package types

import (
	"context"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SigilKeeper is the x/sigil interface x/mandate depends on: controller
// resolution for issuance authorization (SPEC §6.2 "signer must be sigil
// controller") and the addr → sigil_id reverse index, which includes
// delegate keys registered via Mandates (SPEC §6.1 key 0x04).
type SigilKeeper interface {
	// ControllerAddress resolves a sigil id to its current controller.
	ControllerAddress(ctx sdk.Context, chamberID uint32, sigilID string) (sdk.AccAddress, error)
	// SigilIDByAddress resolves an address through the reverse index.
	SigilIDByAddress(ctx sdk.Context, chamberID uint32, addr sdk.AccAddress) (string, error)
	// SetAddressIndex registers a delegate (grantee) key for a sigil.
	SetAddressIndex(ctx sdk.Context, chamberID uint32, addr sdk.AccAddress, sigilID string) error
}

// BankKeeper is the x/bank interface used by fee abstraction (SPEC §6.2):
// the module never mints or burns, it only moves coins through the
// genesis-funded fee pool module account.
type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
}

// AccountKeeper is the x/auth interface used to materialize the fee pool
// module account.
type AccountKeeper interface {
	GetModuleAddress(name string) sdk.AccAddress
	GetModuleAccount(ctx context.Context, moduleName string) sdk.ModuleAccountI
}

// TwapSource supplies the session TWAP behind §6.2 fee conversion (task
// 2.5): the windowed clearing-price average of base quoted in quote, and
// whether it is warm enough to serve (in-window, enough observations,
// positive). Implemented by the x/session keeper and injected at app
// construction (SetTwapSource) so x/mandate keeps no compile-time x/session
// edge — the same cycle-breaking pattern as SetLiveExposureFn (D-058/D-064).
// This is the price index's ONLY consumer: ceiling valuation stays on
// FaceValuePriceSource (AUDIT F-3 boundary; the D-044 swap is Phase-3 3.4).
type TwapSource interface {
	TwapPrice(ctx sdk.Context, base, quote string) (math.LegacyDec, bool)
}
