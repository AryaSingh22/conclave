package types

import (
	"encoding/binary"
	"encoding/hex"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/address"
)

const (
	// ModuleName defines the module name (SPEC §0 naming canon). The module
	// account of the same name holds the CLV fee-conversion float funded at
	// genesis (SPEC §6.2 "param: fee_pool").
	ModuleName = "mandate"

	// StoreKey defines the primary module store key.
	StoreKey = ModuleName

	// IDLength is the raw byte length of mandate ids (sha256 output;
	// rendered as 64-char hex in protos and APIs), matching the D-009
	// id derivation scheme.
	IDLength = 32
)

// Store key layout per SPEC §5.3 and §6.2: single-byte prefix per object
// type, big-endian fixed-width numeric components, chamber_id (4 bytes) as
// the first component after the prefix (always 0x00000000 until Phase 3;
// chamber-first per D-011 even where §6.2 writes keys without it).
//
// Grantee addresses inside the composite 0x01 key are length-prefixed
// (address.MustLengthPrefix) so a shorter address can never alias a prefix
// of a longer one; terminal address components (0x02) need no prefix.
var (
	// MandateKeyPrefix: 0x01 | chamber(4) | len(1) | grantee_addr | mandate_id(32) → Mandate
	// (SPEC §6.2 state key 0x01|chamber|grantee_addr|mandate_id)
	MandateKeyPrefix = []byte{0x01}
	// GranteeIndexKeyPrefix: 0x02 | chamber(4) | grantee_addr → mandate_id
	// (SPEC §6.2 per-grantee active-mandate index for O(1) ante lookup;
	// one active mandate per key, REPLACE semantics per open decision D2)
	GranteeIndexKeyPrefix = []byte{0x02}
	// SigilIndexKeyPrefix: 0x03 | chamber(4) | sigil_id(32) | mandate_id(32) → grantee_addr
	// (per-sigil secondary index serving the Mandates(sigil_id) query)
	SigilIndexKeyPrefix = []byte{0x03}
	// ParamsKey: 0x04 → Params
	ParamsKey = []byte{0x04}
	// IDSequenceKey: 0x05 → uint64 global id sequence (salt for id derivation,
	// D-009 scheme with mandate domain byte 0x03)
	IDSequenceKey = []byte{0x05}
)

// MandateIDBytes decodes a mandate id from its 64-char hex form into the raw
// 32 bytes used in store keys.
func MandateIDBytes(id string) ([]byte, error) {
	bz, err := hex.DecodeString(id)
	if err != nil {
		return nil, errorsmod.Wrapf(ErrInvalidMandateID, "id %q is not hex: %s", id, err)
	}
	if len(bz) != IDLength {
		return nil, errorsmod.Wrapf(ErrInvalidMandateID, "id %q has %d bytes, want %d", id, len(bz), IDLength)
	}
	return bz, nil
}

func chamberBytes(chamberID uint32) []byte {
	bz := make([]byte, 4)
	binary.BigEndian.PutUint32(bz, chamberID)
	return bz
}

// MandateKey returns 0x01 | chamber(4) | len(1) | grantee_addr | mandate_id(32).
func MandateKey(chamberID uint32, grantee sdk.AccAddress, idBz []byte) []byte {
	addrBz := address.MustLengthPrefix(grantee.Bytes())
	key := make([]byte, 0, 1+4+len(addrBz)+IDLength)
	key = append(key, MandateKeyPrefix...)
	key = append(key, chamberBytes(chamberID)...)
	key = append(key, addrBz...)
	return append(key, idBz...)
}

// MandateIterPrefix returns the prefix scanning all mandate records of a
// grantee within a chamber.
func MandateIterPrefix(chamberID uint32, grantee sdk.AccAddress) []byte {
	addrBz := address.MustLengthPrefix(grantee.Bytes())
	key := make([]byte, 0, 1+4+len(addrBz))
	key = append(key, MandateKeyPrefix...)
	key = append(key, chamberBytes(chamberID)...)
	return append(key, addrBz...)
}

// GranteeIndexKey returns 0x02 | chamber(4) | grantee_addr.
func GranteeIndexKey(chamberID uint32, grantee sdk.AccAddress) []byte {
	key := make([]byte, 0, 1+4+len(grantee))
	key = append(key, GranteeIndexKeyPrefix...)
	key = append(key, chamberBytes(chamberID)...)
	return append(key, grantee.Bytes()...)
}

// SigilIndexKey returns 0x03 | chamber(4) | sigil_id(32) | mandate_id(32).
func SigilIndexKey(chamberID uint32, sigilIDBz, mandateIDBz []byte) []byte {
	key := make([]byte, 0, 1+4+2*IDLength)
	key = append(key, SigilIndexKeyPrefix...)
	key = append(key, chamberBytes(chamberID)...)
	key = append(key, sigilIDBz...)
	return append(key, mandateIDBz...)
}

// SigilIndexIterPrefix returns the prefix scanning all mandates of a sigil.
func SigilIndexIterPrefix(chamberID uint32, sigilIDBz []byte) []byte {
	key := make([]byte, 0, 1+4+IDLength)
	key = append(key, SigilIndexKeyPrefix...)
	key = append(key, chamberBytes(chamberID)...)
	return append(key, sigilIDBz...)
}
