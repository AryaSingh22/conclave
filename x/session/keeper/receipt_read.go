package keeper

import (
	"cosmossdk.io/store/prefix"

	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	receipttypes "github.com/AryaSingh22/conclave/x/receipt/types"
	"github.com/AryaSingh22/conclave/x/session/types"
)

// Read surface over the §6.6 receipts x/session writes (D-045). These methods
// are READ-ONLY — no state writes, no behaviour change to 0.8 — and exist so
// task 0.9's x/receipt can serve its query/proof surface across the module
// boundary without touching the session store directly (D-052: x/session stays
// authoritative over its own byte layout). They satisfy
// receipttypes.SessionReceiptReader; the app wires the concrete keeper in.

// ReceiptStoreName returns the store the §6.6 receipts live in — the x/session
// store (D-045). x/receipt uses it to build the ICS-23 proof path
// "/<store>/key" (ReceiptProof, §6.6).
func (k Keeper) ReceiptStoreName() string { return types.StoreKey }

// ReceiptByIDStoreKey returns the physical 0x08|receipt_id key for a receipt id.
// x/receipt proves THIS key against the session IAVL root (D-053). Keeping the
// key construction here (not in x/receipt) means a future layout change is a
// single-module edit.
func (k Keeper) ReceiptByIDStoreKey(id string) ([]byte, error) {
	ridBz, err := types.IDBytes(id)
	if err != nil {
		return nil, err
	}
	return types.ReceiptByIDKey(ridBz), nil
}

// GetReceiptByID reads a receipt from the 0x08 secondary index. Returns
// (_, false, nil) when absent. Cross-module read for x/receipt (§6.6 Receipt(id)).
func (k Keeper) GetReceiptByID(ctx sdk.Context, id string) (receipttypes.Receipt, bool, error) {
	ridBz, err := types.IDBytes(id)
	if err != nil {
		return receipttypes.Receipt{}, false, err
	}
	bz, err := k.storeService.OpenKVStore(ctx).Get(types.ReceiptByIDKey(ridBz))
	if err != nil {
		return receipttypes.Receipt{}, false, err
	}
	if bz == nil {
		return receipttypes.Receipt{}, false, nil
	}
	var rec receipttypes.Receipt
	if err := k.cdc.Unmarshal(bz, &rec); err != nil {
		return receipttypes.Receipt{}, false, err
	}
	return rec, true, nil
}

// GetReceiptsBySigil paginates one sigil's receipts over the 0x07 per-agent
// chronological index (chamber|sigil_id|height|receipt_id), ascending by height
// then receipt id. Cross-module read for x/receipt (§6.6 Receipts(sigil_id,
// pagination)); pagination lives here because the store does.
func (k Keeper) GetReceiptsBySigil(ctx sdk.Context, sigilID string, page *query.PageRequest) ([]receipttypes.Receipt, *query.PageResponse, error) {
	sigilBz, err := types.IDBytes(sigilID)
	if err != nil {
		return nil, nil, err
	}
	store := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	recStore := prefix.NewStore(store, types.ReceiptBySigilPrefix(phase0Chamber, sigilBz))

	var receipts []receipttypes.Receipt
	pageRes, err := query.Paginate(recStore, page, func(_ []byte, value []byte) error {
		var rec receipttypes.Receipt
		if err := k.cdc.Unmarshal(value, &rec); err != nil {
			return err
		}
		receipts = append(receipts, rec)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return receipts, pageRes, nil
}
