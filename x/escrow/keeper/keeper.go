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
