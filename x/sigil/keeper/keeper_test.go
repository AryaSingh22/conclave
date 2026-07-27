package keeper

import (
	"crypto/sha256"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/AryaSingh22/conclave/x/sigil/types"
)

func setupKeeper(t testing.TB) (sdk.Context, Keeper) {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("transient_test"))
	encCfg := moduletestutil.MakeTestEncodingConfig()
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	k := NewKeeper(encCfg.Codec, runtime.NewKVStoreService(key), authority)
	require.NoError(t, k.SetParams(testCtx.Ctx, types.DefaultParams()))
	return testCtx.Ctx.WithBlockHeight(10), k
}

// fixtureAddrSeq backs randAddr's deterministic fixture addresses.
var fixtureAddrSeq uint32

// randAddr returns a distinct fixture address. Despite the name (kept so the
// call sites read unchanged) it draws NO randomness — D-088. A fixture that
// generates a fresh key per call turns a test into a low-duty-cycle sampler of
// its own preconditions, which is how the 0xFF range-end defect hid as a
// 1-in-256 flake instead of a standing failure. These keeper tests never
// verify signatures, so a derived address suffices.
func randAddr() sdk.AccAddress {
	fixtureAddrSeq++
	b := make([]byte, 20)
	binary.BigEndian.PutUint32(b[16:], fixtureAddrSeq)
	return sdk.AccAddress(b)
}

func validDIDHash() []byte {
	h := sha256.Sum256([]byte("did-doc"))
	return h[:]
}

func mustRegister(t testing.TB, ctx sdk.Context, k Keeper, controller sdk.AccAddress, recovery []string, threshold uint32) string {
	t.Helper()
	id, err := k.RegisterSigil(ctx, controller, validDIDHash(), "ipfs://meta", recovery, threshold, 0)
	require.NoError(t, err)
	return id
}

func TestRegisterSigil(t *testing.T) {
	recovery := []string{randAddr().String(), randAddr().String(), randAddr().String()}

	cases := []struct {
		name      string
		didHash   []byte
		recovery  []string
		threshold uint32
		setup     func(ctx sdk.Context, k Keeper, controller sdk.AccAddress)
		wantErr   error
	}{
		{
			name:      "happy path",
			didHash:   validDIDHash(),
			recovery:  recovery,
			threshold: 2,
		},
		{
			name:      "no recovery set",
			didHash:   validDIDHash(),
			recovery:  nil,
			threshold: 0,
		},
		{
			name:      "bad did hash length",
			didHash:   []byte("short"),
			recovery:  recovery,
			threshold: 2,
			wantErr:   types.ErrInvalidDIDDocHash,
		},
		{
			name:      "threshold exceeds set",
			didHash:   validDIDHash(),
			recovery:  recovery,
			threshold: 4,
			wantErr:   types.ErrInvalidRecoverySet,
		},
		{
			name:      "duplicate recovery member",
			didHash:   validDIDHash(),
			recovery:  []string{recovery[0], recovery[0]},
			threshold: 1,
			wantErr:   types.ErrInvalidRecoverySet,
		},
		{
			name:      "controller already has a sigil",
			didHash:   validDIDHash(),
			recovery:  recovery,
			threshold: 2,
			setup: func(ctx sdk.Context, k Keeper, controller sdk.AccAddress) {
				_, err := k.RegisterSigil(ctx, controller, validDIDHash(), "", nil, 0, 0)
				require.NoError(t, err)
			},
			wantErr: types.ErrSigilExists,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, k := setupKeeper(t)
			controller := randAddr()
			if tc.setup != nil {
				tc.setup(ctx, k, controller)
			}

			id, err := k.RegisterSigil(ctx, controller, tc.didHash, "uri", tc.recovery, tc.threshold, 0)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, id, 64) // 32-byte hex

			stored, err := k.GetSigil(ctx, 0, id)
			require.NoError(t, err)
			require.Equal(t, controller.String(), stored.ControllerAddr)
			require.Equal(t, uint64(ctx.BlockHeight()), stored.CreatedHeight)

			indexed, err := k.SigilIDByAddress(ctx, 0, controller)
			require.NoError(t, err)
			require.Equal(t, id, indexed)

			rep, err := k.GetReputation(ctx, 0, id)
			require.NoError(t, err)
			require.True(t, rep.Score.IsZero())
		})
	}
}

func TestRotateController(t *testing.T) {
	type fixture struct {
		ctx        sdk.Context
		k          Keeper
		sigilID    string
		controller sdk.AccAddress
		recovery   []sdk.AccAddress
	}

	newFixture := func(t *testing.T, threshold uint32) fixture {
		ctx, k := setupKeeper(t)
		controller := randAddr()
		recovery := []sdk.AccAddress{randAddr(), randAddr(), randAddr()}
		recoveryStr := make([]string, len(recovery))
		for i, r := range recovery {
			recoveryStr[i] = r.String()
		}
		id := mustRegister(t, ctx, k, controller, recoveryStr, threshold)
		return fixture{ctx, k, id, controller, recovery}
	}

	t.Run("by current controller", func(t *testing.T) {
		f := newFixture(t, 2)
		newCtrl := randAddr()
		require.NoError(t, f.k.RotateController(f.ctx, 0, f.sigilID, []sdk.AccAddress{f.controller}, newCtrl))

		sigil, err := f.k.GetSigil(f.ctx, 0, f.sigilID)
		require.NoError(t, err)
		require.Equal(t, newCtrl.String(), sigil.ControllerAddr)
	})

	t.Run("by recovery threshold", func(t *testing.T) {
		f := newFixture(t, 2)
		newCtrl := randAddr()
		signers := []sdk.AccAddress{f.recovery[0], f.recovery[2]}
		require.NoError(t, f.k.RotateController(f.ctx, 0, f.sigilID, signers, newCtrl))
	})

	t.Run("below threshold rejected", func(t *testing.T) {
		f := newFixture(t, 2)
		err := f.k.RotateController(f.ctx, 0, f.sigilID, []sdk.AccAddress{f.recovery[0]}, randAddr())
		require.ErrorIs(t, err, types.ErrUnauthorized)
	})

	t.Run("duplicate recovery signer does not double count", func(t *testing.T) {
		f := newFixture(t, 2)
		signers := []sdk.AccAddress{f.recovery[0], f.recovery[0]}
		err := f.k.RotateController(f.ctx, 0, f.sigilID, signers, randAddr())
		require.ErrorIs(t, err, types.ErrUnauthorized)
	})

	t.Run("strangers rejected", func(t *testing.T) {
		f := newFixture(t, 2)
		err := f.k.RotateController(f.ctx, 0, f.sigilID, []sdk.AccAddress{randAddr(), randAddr()}, randAddr())
		require.ErrorIs(t, err, types.ErrUnauthorized)
	})

	t.Run("zero threshold means recovery disabled", func(t *testing.T) {
		f := newFixture(t, 0)
		err := f.k.RotateController(f.ctx, 0, f.sigilID, []sdk.AccAddress{f.recovery[0], f.recovery[1], f.recovery[2]}, randAddr())
		require.ErrorIs(t, err, types.ErrUnauthorized)
	})

	t.Run("new controller already controls a sigil", func(t *testing.T) {
		f := newFixture(t, 2)
		other := randAddr()
		mustRegister(t, f.ctx, f.k, other, nil, 0)
		err := f.k.RotateController(f.ctx, 0, f.sigilID, []sdk.AccAddress{f.controller}, other)
		require.ErrorIs(t, err, types.ErrSigilExists)
	})

	t.Run("unknown sigil", func(t *testing.T) {
		f := newFixture(t, 2)
		unknown := deriveID(0x01, 0, []byte("nope"), 999)
		err := f.k.RotateController(f.ctx, 0, unknown, []sdk.AccAddress{f.controller}, randAddr())
		require.ErrorIs(t, err, types.ErrSigilNotFound)
	})

	// Settlement-critical edge (D-006 / SPEC §6.4): SettleLeg sigils are
	// resolved to addresses at Conclude time via the reverse index. A
	// controller rotation that lands mid-session must atomically repoint
	// resolution: the new controller resolves, the old one does not.
	t.Run("mid-session rotation repoints settlement resolution", func(t *testing.T) {
		f := newFixture(t, 2)

		// pre-rotation: Conclude-time resolution returns the old controller
		addr, err := f.k.ControllerAddress(f.ctx, 0, f.sigilID)
		require.NoError(t, err)
		require.Equal(t, f.controller, addr)

		// rotation lands (recovery path) between session open and Conclude
		newCtrl := randAddr()
		require.NoError(t, f.k.RotateController(f.ctx, 0, f.sigilID, []sdk.AccAddress{f.recovery[0], f.recovery[1]}, newCtrl))

		// Conclude-time resolution now returns the new controller…
		addr, err = f.k.ControllerAddress(f.ctx, 0, f.sigilID)
		require.NoError(t, err)
		require.Equal(t, newCtrl, addr)

		// …the new controller is indexed, and the old key resolves nowhere
		id, err := f.k.SigilIDByAddress(f.ctx, 0, newCtrl)
		require.NoError(t, err)
		require.Equal(t, f.sigilID, id)
		_, err = f.k.SigilIDByAddress(f.ctx, 0, f.controller)
		require.ErrorIs(t, err, types.ErrSigilNotFound)

		// and the address-index invariant still holds
		_, broken := AddressIndexInvariant(f.k)(f.ctx)
		require.False(t, broken)
	})
}

func TestCredentials(t *testing.T) {
	ctx, k := setupKeeper(t)

	issuerCtrl, subjectCtrl := randAddr(), randAddr()
	issuerSigil := mustRegister(t, ctx, k, issuerCtrl, nil, 0)
	subjectSigil := mustRegister(t, ctx, k, subjectCtrl, nil, 0)

	claim := validDIDHash()

	t.Run("anchor by non-controller rejected", func(t *testing.T) {
		_, err := k.AnchorCredential(ctx, randAddr(), issuerSigil, subjectSigil, "kyc.v1", claim, 0, 0)
		require.ErrorIs(t, err, types.ErrUnauthorized)
	})

	t.Run("anchor to missing subject rejected", func(t *testing.T) {
		missing := deriveID(0x01, 0, []byte("missing"), 7)
		_, err := k.AnchorCredential(ctx, issuerCtrl, issuerSigil, missing, "kyc.v1", claim, 0, 0)
		require.ErrorIs(t, err, types.ErrSigilNotFound)
	})

	t.Run("expiry in the past rejected", func(t *testing.T) {
		_, err := k.AnchorCredential(ctx, issuerCtrl, issuerSigil, subjectSigil, "kyc.v1", claim, 5, 0)
		require.ErrorIs(t, err, types.ErrInvalidExpiry)
	})

	credID := ""
	t.Run("anchor happy path", func(t *testing.T) {
		var err error
		credID, err = k.AnchorCredential(ctx, issuerCtrl, issuerSigil, subjectSigil, "kyc.v1", claim, 1000, 0)
		require.NoError(t, err)
		require.True(t, k.IsCredentialActive(ctx, 0, subjectSigil, credID))
	})

	t.Run("revoke by non-issuer rejected", func(t *testing.T) {
		err := k.RevokeCredential(ctx, subjectCtrl, 0, subjectSigil, credID)
		require.ErrorIs(t, err, types.ErrUnauthorized)
	})

	t.Run("revoke by issuer", func(t *testing.T) {
		require.NoError(t, k.RevokeCredential(ctx, issuerCtrl, 0, subjectSigil, credID))
		require.False(t, k.IsCredentialActive(ctx, 0, subjectSigil, credID))
	})

	t.Run("double revoke rejected", func(t *testing.T) {
		err := k.RevokeCredential(ctx, issuerCtrl, 0, subjectSigil, credID)
		require.ErrorIs(t, err, types.ErrCredentialRevoked)
	})

	t.Run("expired credential inactive", func(t *testing.T) {
		id, err := k.AnchorCredential(ctx, issuerCtrl, issuerSigil, subjectSigil, "kyc.v2", claim, uint64(ctx.BlockHeight())+5, 0)
		require.NoError(t, err)
		require.True(t, k.IsCredentialActive(ctx, 0, subjectSigil, id))
		future := ctx.WithBlockHeight(ctx.BlockHeight() + 5)
		require.False(t, k.IsCredentialActive(future, 0, subjectSigil, id))
	})

	t.Run("revoked-credentials invariant holds", func(t *testing.T) {
		_, broken := RevokedCredentialsInactiveInvariant(k)(ctx)
		require.False(t, broken)
	})
}

func TestAddressIndexInvariantDetectsCorruption(t *testing.T) {
	ctx, k := setupKeeper(t)
	controller := randAddr()
	mustRegister(t, ctx, k, controller, nil, 0)

	_, broken := AddressIndexInvariant(k)(ctx)
	require.False(t, broken)

	// corrupt: point a fresh address at a nonexistent sigil
	bogus := deriveID(0x01, 0, []byte("bogus"), 1234)
	require.NoError(t, k.SetAddressIndex(ctx, 0, randAddr(), bogus))

	_, broken = AddressIndexInvariant(k)(ctx)
	require.True(t, broken)
}
