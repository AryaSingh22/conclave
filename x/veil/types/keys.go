package types

import (
	"encoding/binary"
	"encoding/hex"

	errorsmod "cosmossdk.io/errors"
)

const (
	// ModuleName defines the module name (SPEC §0 naming canon). The module
	// account of the same name escrows intent anti-spam bonds (SPEC §6.3).
	ModuleName = "veil"

	// StoreKey defines the primary module store key.
	StoreKey = ModuleName

	// IDLength is the raw byte length of intent ids (sha256 output, rendered
	// as 64-char hex), per the D-009 derivation scheme: §5.1's
	// hash(sender_sigil, nonce, chamber) with the module id sequence as the
	// nonce and domain byte 0x04 (D-037).
	IDLength = 32
)

// Store key layout per SPEC §5.3, implemented exactly as the section's veil
// example writes it (D-037):
//
//	0x01 | chamber(4) | intent_id(32)        → IntentRecord
//	0x02 | state(1)   | ttl(8) | intent_id(32) → state/expiry index (no value)
//
// The 0x02 index deliberately carries NO chamber component — §5.3's own
// example omits it, and every sweep (boundary seal, Reveal, expiry) is
// state-scoped, not chamber-scoped, until Phase 3.
// CONCLAVE-TODO(phase-3): revisit index sharding when chamber_id > 0
// activates.
var (
	// IntentKeyPrefix: 0x01 | chamber(4) | intent_id(32) → IntentRecord
	IntentKeyPrefix = []byte{0x01}
	// StateIndexKeyPrefix: 0x02 | state(1) | ttl(8) | intent_id(32) → []byte{}
	StateIndexKeyPrefix = []byte{0x02}
	// ParamsKey: 0x03 → Params
	ParamsKey = []byte{0x03}
	// IDSequenceKey: 0x04 → uint64 global id sequence (nonce for D-009 id
	// derivation, intent domain byte 0x04 per D-037)
	IDSequenceKey = []byte{0x04}
)

// IntentIDBytes decodes an intent id from its 64-char hex form into the raw
// 32 bytes used in store keys.
func IntentIDBytes(id string) ([]byte, error) {
	bz, err := hex.DecodeString(id)
	if err != nil {
		return nil, errorsmod.Wrapf(ErrInvalidIntentID, "id %q is not hex: %s", id, err)
	}
	if len(bz) != IDLength {
		return nil, errorsmod.Wrapf(ErrInvalidIntentID, "id %q has %d bytes, want %d", id, len(bz), IDLength)
	}
	return bz, nil
}

func chamberBytes(chamberID uint32) []byte {
	bz := make([]byte, 4)
	binary.BigEndian.PutUint32(bz, chamberID)
	return bz
}

// IntentKey returns 0x01 | chamber(4) | intent_id(32).
func IntentKey(chamberID uint32, idBz []byte) []byte {
	key := make([]byte, 0, 1+4+IDLength)
	key = append(key, IntentKeyPrefix...)
	key = append(key, chamberBytes(chamberID)...)
	return append(key, idBz...)
}

// StateIndexKey returns 0x02 | state(1) | ttl(8) | intent_id(32).
func StateIndexKey(state IntentState, ttlHeight uint64, idBz []byte) []byte {
	key := make([]byte, 0, 1+1+8+IDLength)
	key = append(key, StateIndexKeyPrefix...)
	key = append(key, byte(state))
	ttl := make([]byte, 8)
	binary.BigEndian.PutUint64(ttl, ttlHeight)
	key = append(key, ttl...)
	return append(key, idBz...)
}

// StateIndexIterPrefix returns the prefix scanning every indexed intent in
// one state, in ascending ttl order (the §5.3 "efficient expiry sweeps"
// shape — and the boundary/Reveal sweeps reuse it per state).
func StateIndexIterPrefix(state IntentState) []byte {
	return append(append([]byte{}, StateIndexKeyPrefix...), byte(state))
}

// MaxTTL selects every index entry of a state regardless of ttl_height.
const MaxTTL = ^uint64(0)

// StateIndexExpiryEnd returns the exclusive end key for an expiry sweep over
// one state: every index entry with ttl_height <= height sorts before it.
// height == MaxTTL covers the whole state (the boundary and Reveal sweeps).
func StateIndexExpiryEnd(state IntentState, height uint64) []byte {
	if height == MaxTTL {
		// state bytes are tiny (≤ 8), so state+1 is a safe exclusive bound
		return []byte{StateIndexKeyPrefix[0], byte(state) + 1}
	}
	key := make([]byte, 0, 1+1+8)
	key = append(key, StateIndexKeyPrefix...)
	key = append(key, byte(state))
	ttl := make([]byte, 8)
	binary.BigEndian.PutUint64(ttl, height+1)
	return append(key, ttl...)
}

// IntentIDFromStateIndexKey extracts the raw intent id from a 0x02 index key.
func IntentIDFromStateIndexKey(key []byte) []byte {
	return key[1+1+8:]
}

// IntentIDHex renders a raw 32-byte intent id in its canonical hex form.
func IntentIDHex(idBz []byte) string {
	return hex.EncodeToString(idBz)
}
