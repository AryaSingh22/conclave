package types

import "encoding/binary"

const (
	// ModuleName defines the module name. x/epoch owns the DKG epoch key
	// lifecycle (PHASE1_DESIGN §2(b), D-068): boundary detection, the on-chain
	// DKG transport, EpochPubKey publication, and drain-before-rotate. It holds
	// NO module account (no bond/coin custody) and writes no secret material —
	// per-validator secret shares are node-local (D-062).
	ModuleName = "epoch"

	// StoreKey defines the primary module store key.
	StoreKey = ModuleName
)

// Store key layout (D-068). Single-byte prefixes; all multi-byte integers are
// big-endian so iteration order is numeric (SPEC §11.6 determinism).
//
//	0x01                                  → Params
//	0x02                                  → current ACTIVE epoch id (uint64)
//	0x03                                  → next epoch id (uint64)
//	0x04 | id(8)                          → Epoch record
//	0x05 | epoch(8) | dkg_index(4)        → Round-1 deal bundle bytes (opaque)
//	0x06 | epoch(8) | dkg_index(4)        → Round-2 response bundle bytes (opaque)
//	0x07 | epoch(8) | dkg_index(4)        → Round-3 key-confirmation bytes
//	0x08                                  → FORMING epoch id (uint64; 0 = none)
var (
	ParamsKey         = []byte{0x01}
	CurrentEpochIDKey = []byte{0x02}
	NextEpochIDKey    = []byte{0x03}
	EpochPrefix       = []byte{0x04}
	DealPrefix        = []byte{0x05}
	ResponsePrefix    = []byte{0x06}
	ConfirmPrefix     = []byte{0x07}
	FormingEpochIDKey = []byte{0x08}
)

func u64(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

func u32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

// EpochKey returns 0x04 | id(8).
func EpochKey(id uint64) []byte {
	return append(append([]byte{}, EpochPrefix...), u64(id)...)
}

// bundleKey returns prefix | epoch(8) | dkg_index(4).
func bundleKey(prefix []byte, epochID uint64, dkgIndex uint32) []byte {
	key := append([]byte{}, prefix...)
	key = append(key, u64(epochID)...)
	return append(key, u32(dkgIndex)...)
}

// DealKey returns 0x05 | epoch(8) | dkg_index(4).
func DealKey(epochID uint64, dkgIndex uint32) []byte {
	return bundleKey(DealPrefix, epochID, dkgIndex)
}

// ResponseKey returns 0x06 | epoch(8) | dkg_index(4).
func ResponseKey(epochID uint64, dkgIndex uint32) []byte {
	return bundleKey(ResponsePrefix, epochID, dkgIndex)
}

// ConfirmKey returns 0x07 | epoch(8) | dkg_index(4).
func ConfirmKey(epochID uint64, dkgIndex uint32) []byte {
	return bundleKey(ConfirmPrefix, epochID, dkgIndex)
}

// BundleIterPrefix returns prefix | epoch(8) — the scan prefix over all
// per-index bundles of one epoch (deals / responses / confirmations).
func BundleIterPrefix(prefix []byte, epochID uint64) []byte {
	return append(append([]byte{}, prefix...), u64(epochID)...)
}
