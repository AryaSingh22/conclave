package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BankKeeper is the x/bank interface used for bond escrow (SPEC §6.3): the
// module account holds bonds; it never mints or burns. SpendableCoin lets the
// submit-time precondition check (D-085/G2) verify the submitter can fund the
// bond BEFORE the §6.2 window charge, so an under-funded submit is rejected in
// the ante rather than stranding its optimistic charge at the msg-server
// escrow.
type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	SpendableCoin(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
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

// MandateKeeper is the x/mandate surface x/veil depends on: when an intent
// terminates (expired/cancelled/concluded) x/veil releases its optimistic
// commitment from the grantee's lifetime outstanding_commitment (SPEC §6.4
// EndBlock step 7; D-059 — the split of the lifetime commitment from the
// per-period window_spent).
type MandateKeeper interface {
	DecommitIntent(ctx sdk.Context, chamberID uint32, grantee sdk.AccAddress, amount sdk.Coins) error
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

// EpochKeeper is the x/epoch surface x/veil depends on for submit-time epoch-tag
// validation (PHASE1_DESIGN §2(b), D-068). An intent encrypted to a DKG epoch
// carries that epoch's id in its envelope; submission rejects a tag that is not
// CURRENTLY DECRYPTABLE — rejecting both a RETIRED epoch (past edge) and a
// not-yet-published epoch (future edge the client could not have fetched). Wired
// after construction via SetEpochKeeper; nil ⇒ no epoch system (stub mode), and
// epoch_id==0 always passes (the StubScheme path).
type EpochKeeper interface {
	IsDecryptableEpoch(ctx sdk.Context, epochID uint64) bool
	// GetEpochVerification returns an epoch's published DKG verification material
	// (the commitment points and committee size n) for building a reveal-capable
	// threshold scheme at the Reveal (Task 1.6); ok=false unless the epoch is
	// ACTIVE/DRAINING with a published key. Lets the per-intent Reveal select the
	// correct epoch's share set across a drain-before-rotate overlap (D-068).
	GetEpochVerification(ctx sdk.Context, epochID uint64) (commits [][]byte, n int, ok bool)
}
