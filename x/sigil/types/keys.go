package types

import (
	"encoding/binary"
	"encoding/hex"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// ModuleName defines the module name (SPEC §0 naming canon).
	ModuleName = "sigil"

	// StoreKey defines the primary module store key.
	StoreKey = ModuleName

	// IDLength is the raw byte length of sigil and credential ids
	// (sha256 output; rendered as 64-char hex in protos and APIs).
	IDLength = 32
)

// Store key layout per SPEC §5.3: single-byte prefix per object type,
// big-endian fixed-width numeric components, chamber_id (4 bytes) as the
// first component after the prefix (always 0x00000000 until Phase 3).
var (
	// SigilKeyPrefix: 0x01 | chamber(4) | sigil_id(32) → Sigil
	SigilKeyPrefix = []byte{0x01}
	// CredentialKeyPrefix: 0x02 | chamber(4) | sigil_id(32) | cred_id(32) → CredentialAnchor
	CredentialKeyPrefix = []byte{0x02}
	// ReputationKeyPrefix: 0x03 | chamber(4) | sigil_id(32) → Reputation
	ReputationKeyPrefix = []byte{0x03}
	// AddrIndexKeyPrefix: 0x04 | chamber(4) | addr → sigil_id (reverse index,
	// SPEC §6.1; includes delegate keys registered via Mandates from task 0.4)
	AddrIndexKeyPrefix = []byte{0x04}
	// ParamsKey: 0x05 → Params
	ParamsKey = []byte{0x05}
	// IDSequenceKey: 0x06 → uint64 global id sequence (salt for id derivation)
	IDSequenceKey = []byte{0x06}
)

// SigilIDBytes decodes a sigil/credential id from its 64-char hex form into
// the raw 32 bytes used in store keys.
func SigilIDBytes(id string) ([]byte, error) {
	bz, err := hex.DecodeString(id)
	if err != nil {
		return nil, errorsmod.Wrapf(ErrInvalidSigilID, "id %q is not hex: %s", id, err)
	}
	if len(bz) != IDLength {
		return nil, errorsmod.Wrapf(ErrInvalidSigilID, "id %q has %d bytes, want %d", id, len(bz), IDLength)
	}
	return bz, nil
}

func chamberBytes(chamberID uint32) []byte {
	bz := make([]byte, 4)
	binary.BigEndian.PutUint32(bz, chamberID)
	return bz
}

// SigilKey returns 0x01 | chamber(4) | sigil_id(32).
func SigilKey(chamberID uint32, idBz []byte) []byte {
	key := make([]byte, 0, 1+4+IDLength)
	key = append(key, SigilKeyPrefix...)
	key = append(key, chamberBytes(chamberID)...)
	return append(key, idBz...)
}

// CredentialKey returns 0x02 | chamber(4) | sigil_id(32) | cred_id(32).
func CredentialKey(chamberID uint32, sigilIDBz, credIDBz []byte) []byte {
	key := make([]byte, 0, 1+4+2*IDLength)
	key = append(key, CredentialKeyPrefix...)
	key = append(key, chamberBytes(chamberID)...)
	key = append(key, sigilIDBz...)
	return append(key, credIDBz...)
}

// CredentialIterPrefix returns the prefix scanning all credentials of a sigil.
func CredentialIterPrefix(chamberID uint32, sigilIDBz []byte) []byte {
	key := make([]byte, 0, 1+4+IDLength)
	key = append(key, CredentialKeyPrefix...)
	key = append(key, chamberBytes(chamberID)...)
	return append(key, sigilIDBz...)
}

// ReputationKey returns 0x03 | chamber(4) | sigil_id(32).
func ReputationKey(chamberID uint32, idBz []byte) []byte {
	key := make([]byte, 0, 1+4+IDLength)
	key = append(key, ReputationKeyPrefix...)
	key = append(key, chamberBytes(chamberID)...)
	return append(key, idBz...)
}

// AddrIndexKey returns 0x04 | chamber(4) | addr.
func AddrIndexKey(chamberID uint32, addr sdk.AccAddress) []byte {
	key := make([]byte, 0, 1+4+len(addr))
	key = append(key, AddrIndexKeyPrefix...)
	key = append(key, chamberBytes(chamberID)...)
	return append(key, addr.Bytes()...)
}
