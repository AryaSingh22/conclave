package types

import (
	"encoding/binary"
	"encoding/hex"

	errorsmod "cosmossdk.io/errors"
)

const (
	// ModuleName defines the module name (SPEC §0 naming canon). The module
	// account of the same name is the SPEC §6.4 settlement escrow account
	// (coin legs route through it; it nets zero per Conclude), holds solver
	// bonds and the protocol fee pool, and is the minter of svc/{schema}
	// service-voucher tokens.
	ModuleName = "session"

	// StoreKey defines the primary module store key.
	StoreKey = ModuleName

	// IDLength is the raw byte length of sha256-derived ids (intent ids,
	// solution ids, receipt/escrow ids) rendered as 64-char hex.
	IDLength = 32
)

// Store key layout (chamber-first per SPEC §5.3; numeric components are
// fixed-width big-endian). Session ids are the session-boundary block height
// (D-038, preserved by the real keeper so x/veil's order-then-reveal check is
// unchanged).
//
//	0x01 | chamber(4) | id(8)                              → Session
//	0x02 | state(1)   | id(8)                              → active-session index
//	0x03 | chamber(4) | addr                              → Solver
//	0x04 | unbond_height(8) | addr                        → solver unbonding index
//	0x05 | chamber(4) | session_id(8) | solution_id(32)   → Solution
//	0x06                                                  → Params
//	0x07 | chamber(4) | sigil_id(32) | height(8) | rid(32) → Receipt   (§6.6 primary, Half B)
//	0x08 | rid(32)                                        → Receipt    (§6.6 secondary, Half B)
//	0x09 | chamber(4) | escrow_id(32)                     → Escrow stub (DataGrant legs)
//	0x0A                                                  → next_session_id (genesis bookkeeping)
//
// The §6.6 receipt layout (0x07 primary `chamber|sigil_id|height|receipt_id`,
// 0x08 secondary `receipt_id`) is pinned here exactly even though receipts are
// written in Half B: per Decision C the receipts live in the x/session store
// and 0.9's x/receipt reads them cross-module (never migrates), so the byte
// layout is a frozen contract from now on.
// CONCLAVE-TODO(phase-3): revisit index sharding when chamber_id > 0 activates.
var (
	SessionKeyPrefix      = []byte{0x01}
	SessionStateIdxPrefix = []byte{0x02}
	SolverKeyPrefix       = []byte{0x03}
	SolverUnbondIdxPrefix = []byte{0x04}
	SolutionKeyPrefix     = []byte{0x05}
	ParamsKey             = []byte{0x06}
	ReceiptKeyPrefix      = []byte{0x07}
	ReceiptByIDPrefix     = []byte{0x08}
	EscrowKeyPrefix       = []byte{0x09}
	// NextSessionIDKey persists genesis next_session_id. Session ids are the
	// boundary block height (D-038), so this is bookkeeping for genesis
	// fidelity rather than an allocation counter.
	NextSessionIDKey = []byte{0x0A}
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

// IDBytes decodes a 64-char hex id into its raw 32 bytes for store keys.
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

// IDHex renders a raw 32-byte id in canonical hex.
func IDHex(idBz []byte) string { return hex.EncodeToString(idBz) }

// SessionKey returns 0x01 | chamber(4) | id(8).
func SessionKey(chamberID uint32, id uint64) []byte {
	key := make([]byte, 0, 1+4+8)
	key = append(key, SessionKeyPrefix...)
	key = append(key, chamberBytes(chamberID)...)
	return append(key, u64Bytes(id)...)
}

// SessionStateIdxKey returns 0x02 | state(1) | id(8). Lets the EndBlocker
// scan only sessions in a given lifecycle state, in ascending id order
// (deterministic, §11.6).
func SessionStateIdxKey(state SessionState, id uint64) []byte {
	key := make([]byte, 0, 1+1+8)
	key = append(key, SessionStateIdxPrefix...)
	key = append(key, byte(state))
	return append(key, u64Bytes(id)...)
}

// SessionStateIdxIterPrefix scans every session indexed in one state.
func SessionStateIdxIterPrefix(state SessionState) []byte {
	return append(append([]byte{}, SessionStateIdxPrefix...), byte(state))
}

// SessionChamberPrefix returns 0x01 | chamber(4) — scans every session of a
// chamber in ascending id order.
func SessionChamberPrefix(chamberID uint32) []byte {
	return append(append([]byte{}, SessionKeyPrefix...), chamberBytes(chamberID)...)
}

// SessionIDFromStateIdxKey extracts the session id from a 0x02 index key.
func SessionIDFromStateIdxKey(key []byte) uint64 {
	return binary.BigEndian.Uint64(key[1+1:])
}

// SolverKey returns 0x03 | chamber(4) | addr.
func SolverKey(chamberID uint32, addr []byte) []byte {
	key := make([]byte, 0, 1+4+len(addr))
	key = append(key, SolverKeyPrefix...)
	key = append(key, chamberBytes(chamberID)...)
	return append(key, addr...)
}

// SolverUnbondIdxKey returns 0x04 | unbond_height(8) | addr — the unbonding
// sweep walks this prefix up to the current height in ascending order.
func SolverUnbondIdxKey(unbondHeight uint64, addr []byte) []byte {
	key := make([]byte, 0, 1+8+len(addr))
	key = append(key, SolverUnbondIdxPrefix...)
	key = append(key, u64Bytes(unbondHeight)...)
	return append(key, addr...)
}

// AddrFromUnbondIdxKey extracts the solver address from a 0x04 index key.
func AddrFromUnbondIdxKey(key []byte) []byte { return key[1+8:] }

// SolutionKey returns 0x05 | chamber(4) | session_id(8) | solution_id(32).
func SolutionKey(chamberID uint32, sessionID uint64, solutionIDBz []byte) []byte {
	key := make([]byte, 0, 1+4+8+IDLength)
	key = append(key, SolutionKeyPrefix...)
	key = append(key, chamberBytes(chamberID)...)
	key = append(key, u64Bytes(sessionID)...)
	return append(key, solutionIDBz...)
}

// SolutionIterPrefix scans every solution stored for one session.
func SolutionIterPrefix(chamberID uint32, sessionID uint64) []byte {
	key := make([]byte, 0, 1+4+8)
	key = append(key, SolutionKeyPrefix...)
	key = append(key, chamberBytes(chamberID)...)
	return append(key, u64Bytes(sessionID)...)
}

// EscrowKey returns 0x09 | chamber(4) | escrow_id(32).
func EscrowKey(chamberID uint32, escrowIDBz []byte) []byte {
	key := make([]byte, 0, 1+4+IDLength)
	key = append(key, EscrowKeyPrefix...)
	key = append(key, chamberBytes(chamberID)...)
	return append(key, escrowIDBz...)
}

// ReceiptKey returns the §6.6 primary 0x07 | chamber(4) | sigil_id(32) |
// height(8) | receipt_id(32) (per-agent chronological scans). Used in Half B.
func ReceiptKey(chamberID uint32, sigilIDBz []byte, height uint64, receiptIDBz []byte) []byte {
	key := make([]byte, 0, 1+4+IDLength+8+IDLength)
	key = append(key, ReceiptKeyPrefix...)
	key = append(key, chamberBytes(chamberID)...)
	key = append(key, sigilIDBz...)
	key = append(key, u64Bytes(height)...)
	return append(key, receiptIDBz...)
}

// ReceiptByIDKey returns the §6.6 secondary 0x08 | receipt_id(32). Used in
// Half B.
func ReceiptByIDKey(receiptIDBz []byte) []byte {
	return append(append([]byte{}, ReceiptByIDPrefix...), receiptIDBz...)
}

// PrefixEnd returns the exclusive upper bound for iterating a key prefix.
func PrefixEnd(prefix []byte) []byte {
	end := make([]byte, len(prefix))
	copy(end, prefix)
	for i := len(end) - 1; i >= 0; i-- {
		if end[i] != 0xFF {
			end[i]++
			return end[:i+1]
		}
	}
	return nil
}
