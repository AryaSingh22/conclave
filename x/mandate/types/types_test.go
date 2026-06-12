package types

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func testAddr() string {
	return sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String()
}

var testSigilID = strings.Repeat("ab", 32)

func validTestTerms() MandateTerms {
	return MandateTerms{
		SigilId:            testSigilID,
		GranteeAddr:        testAddr(),
		AllowedMsgTypes:    []string{"/conclave.veil.v1.MsgSubmitIntent"},
		SpendCeilingPerTx:  sdk.NewCoins(sdk.NewInt64Coin("uusdc", 500)),
		SpendCeilingWindow: sdk.NewCoins(sdk.NewInt64Coin("uusdc", 500)),
		WindowBlocks:       100,
		ExpiryHeight:       1000,
	}
}

func TestMandateTermsValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*MandateTerms)
		wantErr error
	}{
		{name: "valid", mutate: func(*MandateTerms) {}},
		{name: "bad grantee", mutate: func(tm *MandateTerms) { tm.GranteeAddr = "not-bech32" }, wantErr: ErrInvalidTerms},
		{name: "bad sigil id", mutate: func(tm *MandateTerms) { tm.SigilId = "short" }, wantErr: ErrInvalidTerms},
		{name: "empty allowed type", mutate: func(tm *MandateTerms) { tm.AllowedMsgTypes = []string{""} }, wantErr: ErrInvalidTerms},
		{
			name:    "duplicate allowed type",
			mutate:  func(tm *MandateTerms) { tm.AllowedMsgTypes = append(tm.AllowedMsgTypes, tm.AllowedMsgTypes[0]) },
			wantErr: ErrInvalidTerms,
		},
		{
			name:    "window ceiling without window blocks",
			mutate:  func(tm *MandateTerms) { tm.WindowBlocks = 0 },
			wantErr: ErrInvalidTerms,
		},
		{name: "zero expiry", mutate: func(tm *MandateTerms) { tm.ExpiryHeight = 0 }, wantErr: ErrInvalidTerms},
		{name: "bad sponsor", mutate: func(tm *MandateTerms) { tm.FeeSponsor = "nope" }, wantErr: ErrInvalidTerms},
		{name: "bad fee denom", mutate: func(tm *MandateTerms) { tm.FeeDenoms = []string{"!"} }, wantErr: ErrInvalidTerms},
		{
			name:    "duplicate fee denom",
			mutate:  func(tm *MandateTerms) { tm.FeeDenoms = []string{"uusdc", "uusdc"} },
			wantErr: ErrInvalidTerms,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			terms := validTestTerms()
			tc.mutate(&terms)
			err := terms.Validate()
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}

	// the SPEC §6.2 step 3 privilege-escalation guard, every forbidden URL
	// (incl. authz MsgGrant and feegrant MsgGrantAllowance — D-016)
	require.Len(t, ForbiddenMsgTypes(), 6)
	for _, url := range ForbiddenMsgTypes() {
		terms := validTestTerms()
		terms.AllowedMsgTypes = []string{url}
		require.ErrorIs(t, terms.Validate(), ErrPrivilegeEscalation, url)
	}
}

func TestParamsValidate(t *testing.T) {
	require.NoError(t, DefaultParams().Validate())

	p := DefaultParams()
	p.FeeConversionRate = math.LegacyZeroDec()
	require.ErrorIs(t, p.Validate(), ErrInvalidParams)

	p = DefaultParams()
	p.MaxMsgNestingDepth = 0
	require.ErrorIs(t, p.Validate(), ErrInvalidParams)
}

func TestGenesisValidate(t *testing.T) {
	require.NoError(t, DefaultGenesis().Validate())

	mandate := func(mutate func(*Mandate)) Mandate {
		m := Mandate{
			Id:           strings.Repeat("cd", 32),
			SigilId:      testSigilID,
			GranteeAddr:  testAddr(),
			ExpiryHeight: 1000,
		}
		mutate(&m)
		return m
	}

	gs := DefaultGenesis()
	gs.Mandates = []Mandate{mandate(func(*Mandate) {})}
	require.NoError(t, gs.Validate())

	// duplicate mandate id
	gs.Mandates = []Mandate{mandate(func(*Mandate) {}), mandate(func(*Mandate) {})}
	require.ErrorIs(t, gs.Validate(), ErrInvalidMandateID)

	// duplicate grantee (one active mandate per key, D2)
	shared := testAddr()
	gs.Mandates = []Mandate{
		mandate(func(m *Mandate) { m.GranteeAddr = shared }),
		mandate(func(m *Mandate) { m.Id = strings.Repeat("ef", 32); m.GranteeAddr = shared }),
	}
	require.ErrorIs(t, gs.Validate(), ErrGranteeConflict)

	// forbidden allowed type
	gs.Mandates = []Mandate{mandate(func(m *Mandate) { m.AllowedMsgTypes = ForbiddenMsgTypes()[:1] })}
	require.ErrorIs(t, gs.Validate(), ErrPrivilegeEscalation)

	// zero expiry
	gs.Mandates = []Mandate{mandate(func(m *Mandate) { m.ExpiryHeight = 0 })}
	require.ErrorIs(t, gs.Validate(), ErrInvalidTerms)
}

func TestMandateIDBytes(t *testing.T) {
	bz, err := MandateIDBytes(strings.Repeat("ab", 32))
	require.NoError(t, err)
	require.Len(t, bz, IDLength)

	_, err = MandateIDBytes("zz")
	require.ErrorIs(t, err, ErrInvalidMandateID)
	_, err = MandateIDBytes("abcd")
	require.ErrorIs(t, err, ErrInvalidMandateID)
}
