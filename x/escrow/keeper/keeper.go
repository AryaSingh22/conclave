// Package keeper implements x/escrow — conditioned-delivery escrows for
// CONCLUDE DataGrant legs (SPEC §6.5). It owns the escrow records (D-073
// ownership handoff from x/session) and resolves the HASHLOCK, OPTIMISTIC, and
// ZK_DELIVERY condition classes as explicit state machines (ZK_DELIVERY added
// in task 2.1b, D-076: release is gated on a groth16 proof verified by
// crypto/zkdelivery). Every state change routes through the single
// authoritative transition gate (transition.go) so illegal transitions are
// provably impossible (SPEC §11 rule 2).
package keeper

import (
	storetypes "cosmossdk.io/core/store"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/escrow/types"
)

// phase0Chamber is the only active chamber until Phase 3 (SPEC §1.2); escrow
// ids are derived under it by x/session conclude (D-045 layout, preserved).
const phase0Chamber uint32 = 0

// Keeper provides access to the x/escrow store (SPEC §6.5).
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService storetypes.KVStoreService

	bankKeeper  types.BankKeeper
	sigilKeeper types.SigilKeeper
	// receiptOutcome carries resolutions back onto the §6.6 receipts (task
	// 2.9). Set by the app AFTER construction — x/session cannot be a
	// constructor argument here because it takes THIS keeper as one.
	receiptOutcome types.ReceiptOutcomeWriter
}

// NewKeeper creates a new x/escrow Keeper. The escrow module account custodies
// escrowed payments and OPTIMISTIC seller bonds; the sigil keeper resolves
// payer/payee payouts and fires the §6.1 lost-dispute reputation penalty.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	bankKeeper types.BankKeeper,
	sigilKeeper types.SigilKeeper,
) Keeper {
	return Keeper{
		cdc:          cdc,
		storeService: storeService,
		bankKeeper:   bankKeeper,
		sigilKeeper:  sigilKeeper,
	}
}

// SetReceiptOutcomeWriter wires the task-2.9 escrow→receipt write-back. It must
// run BEFORE the keeper value is captured by RegisterModules and the CVM query
// plugins, mirroring SetVeilKeeper / SetLiveExposureFn / SetEpochKeeper.
//
// The writer is NIL-TOLERANT (writeBackReceiptOutcome no-ops on nil) so the
// keeper-level fixtures, which construct x/escrow with no x/session at all,
// keep working. That tolerance is exactly the kind of seam Rule 4 warns can go
// silently unwired in production, so the wiring is proven at the APP level
// rather than assumed: app/receipt_condition_outcome_test.go settles and
// resolves through real blocks and reads the outcome back off the receipt, and
// it can only pass if this setter ran.
func (k *Keeper) SetReceiptOutcomeWriter(w types.ReceiptOutcomeWriter) {
	k.receiptOutcome = w
}

// writeBackReceiptOutcome reports an escrow's new state to the §6.6 receipts
// (task 2.9, D-100). Called from the two places that change a state a receipt
// reports: commitTerminal (RELEASED/REFUNDED, the single funnel for every
// terminal path in every class) and RaiseDispute (DISPUTED).
func (k Keeper) writeBackReceiptOutcome(ctx sdk.Context, esc types.Escrow, state types.EscrowState) error {
	if k.receiptOutcome == nil {
		return nil
	}
	return k.receiptOutcome.SetReceiptOutcomeForEscrow(ctx, esc.Id, state)
}
