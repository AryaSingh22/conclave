package types

import (
	"context"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	sigiltypes "github.com/AryaSingh22/conclave/x/sigil/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// BankKeeper is the x/bank surface x/session depends on. The session module
// account is the SPEC §6.4 settlement escrow account (coin legs route through
// it and it nets zero per Conclude), holds solver bonds and the fee pool, and
// mints svc/{schema} service vouchers.
type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, sender sdk.AccAddress, module string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, module string, recipient sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
	MintCoins(ctx context.Context, module string, amt sdk.Coins) error
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
}

// DistrKeeper is the x/distribution surface for the SPEC §6.4 solver slash:
// the slashed bond fraction funds the community pool.
type DistrKeeper interface {
	FundCommunityPool(ctx context.Context, amount sdk.Coins, sender sdk.AccAddress) error
}

// AccountKeeper is the x/auth surface for module-account resolution.
type AccountKeeper interface {
	GetModuleAddress(name string) sdk.AccAddress
}

// SigilKeeper is the x/sigil surface x/session depends on: SettleLeg sigils
// resolve to controller addresses at Conclude (D-006), and the SPEC §6.4
// rule-5 counterparty-class predicate evaluates against reputation/credential
// state (the §6.1 invariant-3 accessor, resolving the 0.4 CONCLAVE-TODO).
type SigilKeeper interface {
	ControllerAddress(ctx sdk.Context, chamberID uint32, sigilID string) (sdk.AccAddress, error)
	GetReputation(ctx sdk.Context, chamberID uint32, sigilID string) (sigiltypes.Reputation, error)
	HasActiveCredentialSchema(ctx sdk.Context, chamberID uint32, sigilID, schema string) bool
	// AddSettlementReputation fires the SPEC §6.1 reputation hook on CONCLUDED,
	// inside the Conclude cache (Half B): score += base_gain·value_weight of the
	// party's settled value (whole USDC).
	AddSettlementReputation(ctx sdk.Context, chamberID uint32, sigilID string, valueUSDC math.LegacyDec) error
}

// VeilKeeper is the x/veil surface x/session drives during settlement. x/veil
// owns the intent store, bonds, and the D-041 refund matrix; x/session calls
// these inside its §6.4 Conclude cache-context so the intent state change and
// the bond/window refunds are part of the same all-or-nothing write.
type VeilKeeper interface {
	// GetOpenIntent returns an OPEN intent's record and decoded payload for
	// solution validation and settlement (Phase-1 ciphertexts are readable
	// only through x/veil).
	GetOpenIntent(ctx sdk.Context, chamberID uint32, intentID string) (veiltypes.IntentRecord, veiltypes.IntentPayload, error)
	// ConcludeIntent transitions an OPEN intent to CONCLUDED and applies the
	// D-041 CONCLUDED refunds (bond + charged_spend window).
	ConcludeIntent(ctx sdk.Context, intentID string) error
	// RollIntent drives the SPEC §6.4 step-5 rollover (OPEN→ROLLED→OPEN, re-homed
	// into the next session). The D-041 charge PERSISTS — no refund, no re-charge.
	RollIntent(ctx sdk.Context, intentID string, nextSessionID uint64) error
}
