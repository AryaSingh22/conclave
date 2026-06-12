// Package unordered implements SPEC §6.7's unordered transactions: the
// ADR-070 pattern with a TTL'd dedup cache of tx hashes replacing
// sequence-based replay protection. App-level by design — no msgs, no
// queries, no user-facing module surface.
package unordered

import (
	"time"

	storetypes "cosmossdk.io/core/store"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/app/unordered/types"
)

// Manager owns the dedup cache (SPEC §6.7: 0x0F|expiry|txhash, swept in
// EndBlock). Replay protection is consensus state: entries written in
// DeliverTx commit; the same writes in CheckTx bound mempool-level replays
// (the check state re-branches from committed state each block) — the
// identical dual-mode pattern as the x/mandate window charge (D-028).
type Manager struct {
	storeService storetypes.KVStoreService
}

// NewManager creates a dedup-cache manager over the unordered store.
func NewManager(storeService storetypes.KVStoreService) Manager {
	return Manager{storeService: storeService}
}

// Initialized reports whether the manager was constructed via NewManager;
// the ante builder refuses a zero-value manager.
func (m Manager) Initialized() bool { return m.storeService != nil }

// Contains reports whether the (timeout, txhash) dedup entry exists. The
// key is reconstructed from the tx's own signed timeout_timestamp, so the
// lookup is a single O(1) store read (D-028).
func (m Manager) Contains(ctx sdk.Context, timeout time.Time, txHash []byte) (bool, error) {
	return m.storeService.OpenKVStore(ctx).Has(types.DedupKey(timeout, txHash))
}

// Add records the dedup entry for a verified unordered tx.
func (m Manager) Add(ctx sdk.Context, timeout time.Time, txHash []byte) error {
	return m.storeService.OpenKVStore(ctx).Set(types.DedupKey(timeout, txHash), []byte{})
}

// Sweep deletes every dedup entry with expiry <= block time. Implements
// the SPEC §6.7 EndBlock sweep; the walk is time-ordered because the key
// is expiry-first. Sweeping can never re-open a replay: a tx whose
// timeout_timestamp <= block time fails the UnorderedTxDecorator's
// timeout validation in the same breath (the predicates are exact
// complements).
func (m Manager) Sweep(ctx sdk.Context) (int, error) {
	store := m.storeService.OpenKVStore(ctx)
	it, err := store.Iterator(types.DedupKeyPrefix, types.DedupSweepEnd(ctx.BlockTime()))
	if err != nil {
		return 0, err
	}
	defer it.Close()

	var expired [][]byte
	for ; it.Valid(); it.Next() {
		key := make([]byte, len(it.Key()))
		copy(key, it.Key())
		expired = append(expired, key)
	}
	for _, key := range expired {
		if err := store.Delete(key); err != nil {
			return 0, err
		}
	}
	return len(expired), nil
}
