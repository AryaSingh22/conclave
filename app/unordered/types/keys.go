// Package types holds the app-level unordered-tx types (SPEC §6.7 —
// "Lanes and Unordered Transactions (app-level, not a module)").
package types

import (
	"encoding/binary"
	"time"
)

const (
	// ModuleName names the dedup-cache store and the EndBlock sweeper shim.
	// No user-facing module surface exists: no msgs, no queries (SPEC §6.7).
	ModuleName = "unordered"

	// StoreKey defines the dedup-cache store key.
	StoreKey = ModuleName

	// MaxUnorderedTTL caps an unordered tx's timeout_timestamp at 10
	// minutes past block time — SPEC §6.7's "10 minutes of blocks"
	// state-growth guard, expressed in time because timeout_timestamp is
	// time (≈600 dev blocks at the §12 1s cadence; D-029).
	MaxUnorderedTTL = 10 * time.Minute

	// TxHashLength is the raw byte length of a dedup tx hash (sha256, the
	// same hash CometBFT assigns the tx).
	TxHashLength = 32
)

// DedupKeyPrefix is the SPEC §6.7 dedup-cache prefix:
// 0x0F | expiry_unix_nanos(8, big-endian) | txhash(32). Expiry-first makes
// the EndBlock sweep a single time-ordered prefix walk; the expiry is the
// tx's own signed timeout_timestamp, so existence checks reconstruct the
// full key from the tx and stay O(1) (D-028).
var DedupKeyPrefix = []byte{0x0F}

// DedupKey returns 0x0F | expiry(8) | txhash(32).
func DedupKey(expiry time.Time, txHash []byte) []byte {
	key := make([]byte, 0, 1+8+TxHashLength)
	key = append(key, DedupKeyPrefix...)
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], uint64(expiry.UnixNano()))
	key = append(key, ts[:]...)
	return append(key, txHash...)
}

// DedupSweepEnd returns the exclusive iteration end covering every entry
// with expiry <= blockTime: 0x0F | (blockTime+1ns)(8).
func DedupSweepEnd(blockTime time.Time) []byte {
	key := make([]byte, 0, 1+8)
	key = append(key, DedupKeyPrefix...)
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], uint64(blockTime.UnixNano())+1)
	return append(key, ts[:]...)
}
