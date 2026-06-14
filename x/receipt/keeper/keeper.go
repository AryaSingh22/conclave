// Package keeper implements x/receipt — the SPEC §6.6 query/proof surface over
// the settlement receipts x/session writes (D-045). It owns NO store: reads go
// through SessionReceiptReader and ICS-23 existence proofs through ProofQuerier
// (the app CommitMultiStore). Registered without a KVStoreKey, the module is
// structurally incapable of a consensus state write (D-052).
package keeper

import (
	"github.com/cosmos/cosmos-sdk/codec"

	"github.com/AryaSingh22/conclave/x/receipt/types"
)

// Keeper is the x/receipt query/proof keeper. It holds no store key (D-052):
// session reads the §6.6 receipts from the x/session store, and proofs builds
// ICS-23 existence proofs against the app root (D-053).
type Keeper struct {
	cdc     codec.BinaryCodec
	session types.SessionReceiptReader
	proofs  types.ProofQuerier
}

// NewKeeper creates the x/receipt keeper. No KVStoreService is taken — the
// module writes no consensus state (D-052).
func NewKeeper(cdc codec.BinaryCodec, session types.SessionReceiptReader, proofs types.ProofQuerier) Keeper {
	return Keeper{cdc: cdc, session: session, proofs: proofs}
}
