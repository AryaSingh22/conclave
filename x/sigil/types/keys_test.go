package types

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// TestKeyLayout asserts the exact SPEC §5.3 byte layout: single-byte prefix,
// big-endian fixed-width chamber_id first, fixed-width 32-byte ids.
func TestKeyLayout(t *testing.T) {
	idHex := strings.Repeat("ab", 32)
	idBz, err := SigilIDBytes(idHex)
	if err != nil {
		t.Fatalf("SigilIDBytes: %v", err)
	}
	credHex := strings.Repeat("cd", 32)
	credBz, err := SigilIDBytes(credHex)
	if err != nil {
		t.Fatalf("SigilIDBytes: %v", err)
	}

	chamber := uint32(0x01020304)
	chamberBE := []byte{0x01, 0x02, 0x03, 0x04}

	sigilKey := SigilKey(chamber, idBz)
	want := append(append([]byte{0x01}, chamberBE...), idBz...)
	if !bytes.Equal(sigilKey, want) {
		t.Fatalf("SigilKey layout mismatch:\n got %s\nwant %s", hex.EncodeToString(sigilKey), hex.EncodeToString(want))
	}
	if len(sigilKey) != 1+4+32 {
		t.Fatalf("SigilKey length %d, want 37", len(sigilKey))
	}

	credKey := CredentialKey(chamber, idBz, credBz)
	want = append(append(append([]byte{0x02}, chamberBE...), idBz...), credBz...)
	if !bytes.Equal(credKey, want) {
		t.Fatalf("CredentialKey layout mismatch")
	}
	if len(credKey) != 1+4+32+32 {
		t.Fatalf("CredentialKey length %d, want 69", len(credKey))
	}

	repKey := ReputationKey(chamber, idBz)
	if repKey[0] != 0x03 || !bytes.Equal(repKey[1:5], chamberBE) {
		t.Fatalf("ReputationKey layout mismatch")
	}

	addr := sdk.AccAddress(bytes.Repeat([]byte{0x42}, 20))
	addrKey := AddrIndexKey(chamber, addr)
	want = append(append([]byte{0x04}, chamberBE...), addr.Bytes()...)
	if !bytes.Equal(addrKey, want) {
		t.Fatalf("AddrIndexKey layout mismatch")
	}

	// chamber 0 encodes as 4 zero bytes (Phase 0 fixed value, SPEC §5.3)
	if !bytes.Equal(SigilKey(0, idBz)[1:5], []byte{0, 0, 0, 0}) {
		t.Fatalf("chamber 0 must encode as 0x00000000")
	}
}

func TestSigilIDBytes(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"valid 32-byte hex", strings.Repeat("ab", 32), false},
		{"not hex", strings.Repeat("zz", 32), true},
		{"too short", "abcd", true},
		{"too long", strings.Repeat("ab", 33), true},
		{"empty", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := SigilIDBytes(tc.id)
			if (err != nil) != tc.wantErr {
				t.Fatalf("SigilIDBytes(%q) error = %v, wantErr %v", tc.id, err, tc.wantErr)
			}
		})
	}
}
