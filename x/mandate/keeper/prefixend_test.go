package keeper

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/mandate/types"
)

// detGranteeAddr builds a deterministic 20-byte account address from an index.
// Fixture addresses are DERIVED, never randomly generated: a fixture that draws
// fresh keys per run turns a test into a low-duty-cycle sampler of its own
// preconditions (D-088 — that is exactly how the 0xFF range-end defect stayed
// hidden, surfacing as a 1-in-256 "flake" instead of a failing assertion).
func detGranteeAddr(i byte) sdk.AccAddress {
	b := make([]byte, 20)
	b[0] = 0xAA
	b[19] = i
	return sdk.AccAddress(b)
}

// hexID renders a 32-byte id whose FIRST byte is `first` — the byte that
// decides whether the record sorts inside or outside a prefix‖0xFF range.
func hexID(first string) string {
	return first + strings.Repeat("a1", 31)
}

// TestMandatesBySigilFindsHighByteMandateID is the D-088 load-bearing
// regression: a mandate whose id begins with 0xFF must be returned by the
// per-sigil (0x03) index walk.
//
// WATCHED RED against the pre-fix code: with the old range end
// `append(prefix, 0xFF)` the "ff…" mandate is silently omitted (the end key is
// SHORTER than the record's key, so the record sorts after it) and this test
// fails with 1 mandate instead of 2 — the same symptom that appeared as a
// 1-in-256 TestIssueMandateReplace flake. The 0x00-prefixed control proves the
// walk is not simply broken for everything.
func TestMandatesBySigilFindsHighByteMandateID(t *testing.T) {
	f := setupKeeper(t)
	controller := detGranteeAddr(1)
	sigilID := f.registerSigil(t, controller)

	// two mandates on the SAME sigil, differing only in the leading id byte.
	control := types.Mandate{
		Id:                    hexID("00"),
		SigilId:               sigilID,
		GranteeAddr:           detGranteeAddr(2).String(),
		OutstandingCommitment: sdk.NewCoins(),
	}
	highByte := types.Mandate{
		Id:                    hexID("ff"),
		SigilId:               sigilID,
		GranteeAddr:           detGranteeAddr(3).String(),
		OutstandingCommitment: sdk.NewCoins(),
	}

	for _, m := range []types.Mandate{control, highByte} {
		grantee := sdk.MustAccAddressFromBech32(m.GranteeAddr)
		require.NoError(t, f.k.SetMandate(f.ctx, m))
		require.NoError(t, f.k.setIndexes(f.ctx, m, grantee))
	}

	got, err := f.k.MandatesBySigil(f.ctx, 0, sigilID)
	require.NoError(t, err)

	ids := make(map[string]bool, len(got))
	for _, m := range got {
		ids[m.Id] = true
	}
	require.True(t, ids[control.Id], "control mandate (id 0x00…) must be indexed")
	require.True(t, ids[highByte.Id],
		"mandate with id 0xFF… is missing from the 0x03 index walk — the range end truncates it (D-088)")
	require.Len(t, got, 2)
}

// TestIterateMandatesFindsHighByteChamber pins the LATENT half of D-088. The
// bare-prefix walks (0x01/0x02) are correct today only because the 4 chamber
// bytes follow the prefix and chamber_id is 0x00000000 until Phase 3. A
// chamber id with a 0xFF high byte re-arms the identical defect, so the
// correct range end is asserted rather than left to that accident.
func TestIterateMandatesFindsHighByteChamber(t *testing.T) {
	f := setupKeeper(t)
	const highChamber = uint32(0xFF000001)

	m := types.Mandate{
		Id:                    hexID("0b"),
		SigilId:               hexID("0c"),
		GranteeAddr:           detGranteeAddr(4).String(),
		ChamberId:             highChamber,
		OutstandingCommitment: sdk.NewCoins(),
	}
	require.NoError(t, f.k.SetMandate(f.ctx, m))

	var seen int
	require.NoError(t, f.k.IterateMandates(f.ctx, func(types.Mandate) bool {
		seen++
		return false
	}))
	require.Equal(t, 1, seen,
		"a mandate in a chamber whose id starts 0xFF is invisible to the 0x01 walk (D-088 latent case)")
}
