package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BankKeeper is the x/bank interface used for bond escrow (SPEC §6.3): the
// module account holds bonds; it never mints or burns.
type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
}

// DistrKeeper is the x/distribution interface for the SPEC §6.3 INVALID
// slash: the slashed bond fraction funds the community pool.
type DistrKeeper interface {
	FundCommunityPool(ctx context.Context, amount sdk.Coins, sender sdk.AccAddress) error
}

// SigilKeeper is the x/sigil surface x/veil depends on: sender→sigil
// resolution for MsgSubmitIntent envelope validation (the reverse index
// includes mandate-bound delegate keys, D-017(d)).
type SigilKeeper interface {
	SigilIDByAddress(ctx sdk.Context, chamberID uint32, addr sdk.AccAddress) (string, error)
}

// MandateKeeper is the x/mandate surface x/veil depends on: the SPEC §6.2
// step 5 refund path returns optimistically charged window spend when an
// intent expires or cancels unmatched (SPEC §6.4 EndBlock step 7).
type MandateKeeper interface {
	RefundWindowSpend(ctx sdk.Context, chamberID uint32, grantee sdk.AccAddress, amount sdk.Coins) error
}

// PriceSource values intent give legs exactly as the ante handler does
// (SPEC §6.2 step 4); the msg server uses it to persist charged_spend at
// submission (D-041) and to compute the D-041 face-value scalar for the
// §6.3 bond formula. Satisfied by mandatetypes.FaceValuePriceSource
// (Phase 0) and by the x/session clearing-price source at 0.8.
type PriceSource interface {
	GiveLegValue(ctx sdk.Context, leg *IntentLeg) sdk.Coins
}

// SessionKeeper is the x/session surface x/veil depends on for the
// session-boundary hand-off (SPEC §6.3 ABCI hooks). x/session lands at task
// 0.8; until then StubSessionKeeper implements it (D-038).
//
// CONCLAVE-TODO(phase-0): task 0.8 supplies the real x/session keeper and
// moves the boundary/Reveal/expiry sweep ownership under x/session
// EndBlock's §6.4 order of operations.
type SessionKeeper interface {
	// SessionBoundary reports whether intent ordering fixes at this height's
	// EndBlock and, if so, the id of the session sealed intents belong to.
	SessionBoundary(ctx sdk.Context) (isBoundary bool, sessionID uint64)
	// IntakeRevealed hands the Reveal output to the session for matching
	// (SPEC §6.4 EndBlock step 2 intake). The stub is a no-op.
	IntakeRevealed(ctx sdk.Context, sessionID uint64, intentIDs []string) error
}
