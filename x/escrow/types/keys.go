package types

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"

	errorsmod "cosmossdk.io/errors"
)

const (
	// ModuleName defines the module name (SPEC §0 naming canon). The module
	// account of the same name custodies conditioned-delivery escrows: the
	// buyer's escrowed payment and, for OPTIMISTIC escrows, the seller's
	// dispute bond (SPEC §6.5). It never mints or burns — funds flow back out
	// as a release (to the payee/seller) or a refund (to the payer/buyer).
	ModuleName = "escrow"

	// StoreKey defines the primary module store key.
	StoreKey = ModuleName

	// IDLength is the raw byte length of sha256-derived escrow ids, rendered
	// as 64-char hex (mirrors x/session IDLength so the conclude.go-derived
	// ids round-trip unchanged across the ownership handoff).
	IDLength = 32
)

// Store key layout (chamber-first per SPEC §5.3; numeric components are
// fixed-width big-endian). x/escrow owns these keys from Phase 2 forward
// (D-073 handoff: x/session is no longer the writer). x/receipt never read
// the old x/session 0x09 escrow stubs (it reads receipts, 0x07/0x08), so the
// handoff orphans no reader.
//
//	0x01 | chamber(4) | escrow_id(32)             → Escrow record
//	0x02 | sha256(sigil)(32) | escrow_id(32)      → participant index (payer & payee)
//	0x03 | sweep_height(8) | escrow_id(32)        → deterministic EndBlock sweep index
//	0x04 | escrow_id(32)                          → OPTIMISTIC committed content hash
//
// The 0x03 sweep index is keyed by the escrow's CURRENT sweep height
// (Escrow.timeout_height): for an ACTIVE escrow that is the HASHLOCK timeout
// or the OPTIMISTIC dispute-window end; for a DISPUTED escrow it is the
// dispute-resolution deadline. EndBlock iterates 0x03 ascending up to the
// block height (sorted by height then id — deterministic, §11.6), so a node's
// sweep order never depends on map iteration. Terminal transitions delete the
// escrow's sweep entry but never delete the record or its participant index
// (escrows stay queryable in RELEASED/REFUNDED for audit, spirit of D-017).
var (
	EscrowKeyPrefix = []byte{0x01}
	SigilIdxPrefix  = []byte{0x02}
	SweepIdxPrefix  = []byte{0x03}
	CommitKeyPrefix = []byte{0x04}
)

func chamberBytes(chamberID uint32) []byte {
	bz := make([]byte, 4)
	binary.BigEndian.PutUint32(bz, chamberID)
	return bz
}

func u64Bytes(v uint64) []byte {
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, v)
	return bz
}

// hashSigil maps a variable-length sigil id to a fixed 32-byte index component
// so the participant index has a fixed key width (prefix scans stay clean).
func hashSigil(sigil string) []byte {
	h := sha256.Sum256([]byte(sigil))
	return h[:]
}

// EscrowKey returns 0x01 | chamber(4) | escrow_id(32).
func EscrowKey(chamberID uint32, escrowIDBz []byte) []byte {
	key := make([]byte, 0, 1+4+IDLength)
	key = append(key, EscrowKeyPrefix...)
	key = append(key, chamberBytes(chamberID)...)
	key = append(key, escrowIDBz...)
	return key
}

// SigilIdxKey returns 0x02 | sha256(sigil)(32) | escrow_id(32).
func SigilIdxKey(sigil string, escrowIDBz []byte) []byte {
	key := make([]byte, 0, 1+32+IDLength)
	key = append(key, SigilIdxPrefix...)
	key = append(key, hashSigil(sigil)...)
	key = append(key, escrowIDBz...)
	return key
}

// SigilIdxPrefixKey returns 0x02 | sha256(sigil)(32) for prefix iteration of a
// participant's escrows.
func SigilIdxPrefixKey(sigil string) []byte {
	key := make([]byte, 0, 1+32)
	key = append(key, SigilIdxPrefix...)
	key = append(key, hashSigil(sigil)...)
	return key
}

// SweepIdxKey returns 0x03 | sweep_height(8) | escrow_id(32).
func SweepIdxKey(sweepHeight uint64, escrowIDBz []byte) []byte {
	key := make([]byte, 0, 1+8+IDLength)
	key = append(key, SweepIdxPrefix...)
	key = append(key, u64Bytes(sweepHeight)...)
	key = append(key, escrowIDBz...)
	return key
}

// CommitKey returns 0x04 | escrow_id(32). It holds the OPTIMISTIC committed
// content hash (the buyer-agreed DataGrant.ContentHash captured at creation),
// which the seller must produce a preimage of to release a disputed escrow.
// OptimisticCondition carries no hash field, so it lives in this side-record
// rather than the proto (D-073; logic-only, no proto change).
func CommitKey(escrowIDBz []byte) []byte {
	key := make([]byte, 0, 1+IDLength)
	key = append(key, CommitKeyPrefix...)
	key = append(key, escrowIDBz...)
	return key
}

// IDBytes decodes a 64-char hex escrow id into its raw 32 bytes for store keys.
func IDBytes(id string) ([]byte, error) {
	bz, err := hex.DecodeString(id)
	if err != nil {
		return nil, errorsmod.Wrapf(ErrInvalidID, "id %q is not hex: %s", id, err)
	}
	if len(bz) != IDLength {
		return nil, errorsmod.Wrapf(ErrInvalidID, "id %q has %d bytes, want %d", id, len(bz), IDLength)
	}
	return bz, nil
}

// IDHex renders a raw 32-byte escrow id in canonical hex.
func IDHex(idBz []byte) string { return hex.EncodeToString(idBz) }
