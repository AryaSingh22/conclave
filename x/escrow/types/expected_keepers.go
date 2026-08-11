package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BankKeeper is the x/bank surface x/escrow depends on. The escrow module
// account custodies the buyer's escrowed payment and the OPTIMISTIC seller
// bond (SPEC §6.5): funds move IN at creation (during the atomic CONCLUDE,
// via x/session's keeper) and OUT here on release/refund/slash.
type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, sender sdk.AccAddress, module string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, module string, recipient sdk.AccAddress, amt sdk.Coins) error
}

// SigilKeeper is the x/sigil surface x/escrow depends on: payer/payee sigils
// resolve to controller addresses for payouts (D-006), and the SPEC §6.5
// OPTIMISTIC lost-dispute path fires the §6.1 reputation penalty.
type SigilKeeper interface {
	ControllerAddress(ctx sdk.Context, chamberID uint32, sigilID string) (sdk.AccAddress, error)
	// PenalizeLostDispute applies the SPEC §6.1 lost-dispute reputation
	// penalty to the seller on a proven OPTIMISTIC non-delivery (the hook was
	// authored in task 0.3 for "x/escrow dispute resolution (Phase 2)").
	PenalizeLostDispute(ctx sdk.Context, chamberID uint32, sigilID string) error
}

// ReceiptOutcomeWriter carries an escrow's resolution back onto the SPEC §6.6
// receipts the settlement that created it wrote (task 2.9, D-100). x/session
// implements it; the app wires it in with the same set-before-capture contract
// as SetVeilKeeper, because x/session is constructed AFTER x/escrow (CONCLUDE
// writes escrows through it) and the dependency therefore runs both ways.
//
// IT IS TYPED IN THIS MODULE'S OWN VOCABULARY — EscrowState, never the
// receipt's ConditionOutcome — because naming receipttypes here would close a
// cycle: x/escrow/types → x/receipt/types → x/session/types → x/escrow/types.
// The projection from one to the other lives in x/session, which owns what a
// receipt means. The signature is the awkward one on purpose; the pretty one
// does not compile.
type ReceiptOutcomeWriter interface {
	SetReceiptOutcomeForEscrow(ctx sdk.Context, escrowID string, state EscrowState) error
}
