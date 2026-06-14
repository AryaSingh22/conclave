package types

import (
	storetypes "cosmossdk.io/store/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
)

// SessionReceiptReader is the read surface x/receipt needs over the §6.6
// receipts x/session writes (D-045). x/receipt never touches the session store
// directly — x/session stays authoritative over its own byte layout, exposing
// typed reads and the physical proof key (D-052). Satisfied by
// *sessionkeeper.Keeper; wired in app.go. The dependency direction is
// receipt→session only, so this interface references no session package.
type SessionReceiptReader interface {
	// GetReceiptByID reads a receipt by id (0x08 secondary index). Returns
	// (_, false, nil) when absent.
	GetReceiptByID(ctx sdk.Context, id string) (Receipt, bool, error)
	// GetReceiptsBySigil paginates a sigil's receipts over the 0x07 per-agent
	// chronological index.
	GetReceiptsBySigil(ctx sdk.Context, sigilID string, page *query.PageRequest) ([]Receipt, *query.PageResponse, error)
	// ReceiptStoreName returns the store the receipts live in — the proof path's
	// store name.
	ReceiptStoreName() string
	// ReceiptByIDStoreKey returns the physical 0x08 key x/receipt proves against
	// the session IAVL root.
	ReceiptByIDStoreKey(id string) ([]byte, error)
}

// ProofQuerier builds ICS-23 store proofs. Satisfied by the app's
// CommitMultiStore (*rootmulti.Store), whose Query appends the multistore commit
// op so the proof chains value → session IAVL root → app hash (D-053).
// x/receipt holds this querier rather than a store key: it READS proofs and
// never writes consensus state (D-052).
type ProofQuerier interface {
	Query(req *storetypes.RequestQuery) (*storetypes.ResponseQuery, error)
}
