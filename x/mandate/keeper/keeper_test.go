package keeper

import (
	"context"
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/AryaSingh22/conclave/x/mandate/types"
	sigilkeeper "github.com/AryaSingh22/conclave/x/sigil/keeper"
	sigiltypes "github.com/AryaSingh22/conclave/x/sigil/types"
)

// mockBank is a minimal in-memory BankKeeper: balances keyed by bech32
// address (module accounts under their derived module address).
type mockBank struct {
	balances map[string]sdk.Coins
}

func newMockBank() *mockBank {
	return &mockBank{balances: make(map[string]sdk.Coins)}
}

func (b *mockBank) fund(addr string, coins sdk.Coins) {
	b.balances[addr] = b.balances[addr].Add(coins...)
}

func (b *mockBank) send(from, to string, amt sdk.Coins) error {
	have := b.balances[from]
	if !amt.IsAllLTE(have) {
		return errorsmod.Wrapf(types.ErrFeePoolInsufficient, "mock bank: %s holds %s, needs %s", from, have, amt)
	}
	b.balances[from] = have.Sub(amt...)
	b.balances[to] = b.balances[to].Add(amt...)
	return nil
}

func (b *mockBank) SendCoinsFromAccountToModule(_ context.Context, sender sdk.AccAddress, module string, amt sdk.Coins) error {
	return b.send(sender.String(), authtypes.NewModuleAddress(module).String(), amt)
}

func (b *mockBank) SendCoinsFromModuleToModule(_ context.Context, senderModule, recipientModule string, amt sdk.Coins) error {
	return b.send(authtypes.NewModuleAddress(senderModule).String(), authtypes.NewModuleAddress(recipientModule).String(), amt)
}

func (b *mockBank) GetBalance(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	return sdk.NewCoin(denom, b.balances[addr.String()].AmountOf(denom))
}

type mockAccount struct{}

func (mockAccount) GetModuleAddress(name string) sdk.AccAddress {
	return authtypes.NewModuleAddress(name)
}

func (mockAccount) GetModuleAccount(_ context.Context, name string) sdk.ModuleAccountI {
	return authtypes.NewEmptyModuleAccount(name)
}

type fixture struct {
	ctx   sdk.Context
	k     Keeper
	sigil sigilkeeper.Keeper
	bank  *mockBank
}

func setupKeeper(t testing.TB) *fixture {
	t.Helper()
	mandateKey := storetypes.NewKVStoreKey(types.StoreKey)
	sigilKey := storetypes.NewKVStoreKey(sigiltypes.StoreKey)
	ctx := testutil.DefaultContextWithKeys(
		map[string]*storetypes.KVStoreKey{types.StoreKey: mandateKey, sigiltypes.StoreKey: sigilKey},
		nil, nil,
	)
	encCfg := moduletestutil.MakeTestEncodingConfig()
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	sk := sigilkeeper.NewKeeper(encCfg.Codec, runtime.NewKVStoreService(sigilKey), authority)
	bank := newMockBank()
	k := NewKeeper(encCfg.Codec, runtime.NewKVStoreService(mandateKey), sk, bank, mockAccount{}, authority)

	ctx = ctx.WithBlockHeight(10)
	require.NoError(t, sk.SetParams(ctx, sigiltypes.DefaultParams()))
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))
	return &fixture{ctx: ctx, k: k, sigil: sk, bank: bank}
}

func randAddr() sdk.AccAddress {
	return sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
}

func (f *fixture) registerSigil(t testing.TB, controller sdk.AccAddress) string {
	t.Helper()
	h := sha256.Sum256([]byte("did-doc"))
	id, err := f.sigil.RegisterSigil(f.ctx, controller, h[:], "ipfs://meta", nil, 0, 0)
	require.NoError(t, err)
	return id
}

func validTerms(sigilID string, grantee sdk.AccAddress) types.MandateTerms {
	return types.MandateTerms{
		SigilId:            sigilID,
		GranteeAddr:        grantee.String(),
		AllowedMsgTypes:    []string{"/conclave.veil.v1.MsgSubmitIntent"},
		SpendCeilingPerTx:  sdk.NewCoins(sdk.NewInt64Coin("uusdc", 500_000_000)),
		SpendCeilingWindow: sdk.NewCoins(sdk.NewInt64Coin("uusdc", 500_000_000)),
		WindowBlocks:       86_400,
		FeeDenoms:          []string{"uusdc"},
		ExpiryHeight:       1_000_000,
	}
}

func TestIssueMandate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(f *fixture, terms *types.MandateTerms, controller sdk.AccAddress)
		signer  func(controller sdk.AccAddress) sdk.AccAddress
		wantErr error
	}{
		{
			name: "happy path",
		},
		{
			name:    "signer is not the sigil controller",
			signer:  func(sdk.AccAddress) sdk.AccAddress { return randAddr() },
			wantErr: types.ErrUnauthorized,
		},
		{
			name: "expiry not beyond current height",
			mutate: func(f *fixture, terms *types.MandateTerms, _ sdk.AccAddress) {
				terms.ExpiryHeight = uint64(f.ctx.BlockHeight())
			},
			wantErr: types.ErrInvalidTerms,
		},
		{
			name: "missing sigil",
			mutate: func(_ *fixture, terms *types.MandateTerms, _ sdk.AccAddress) {
				terms.SigilId = "00000000000000000000000000000000000000000000000000000000000000ff"
			},
			wantErr: sigiltypes.ErrSigilNotFound,
		},
		{
			name: "grantee controls a sigil",
			mutate: func(f *fixture, terms *types.MandateTerms, _ sdk.AccAddress) {
				other := randAddr()
				f.registerSigil(t, other)
				terms.GranteeAddr = other.String()
			},
			wantErr: types.ErrGranteeConflict,
		},
		{
			name: "grantee is the controller itself",
			mutate: func(_ *fixture, terms *types.MandateTerms, controller sdk.AccAddress) {
				terms.GranteeAddr = controller.String()
			},
			wantErr: types.ErrGranteeConflict,
		},
		{
			name: "window ceiling without window_blocks",
			mutate: func(_ *fixture, terms *types.MandateTerms, _ sdk.AccAddress) {
				terms.WindowBlocks = 0
			},
			wantErr: types.ErrInvalidTerms,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := setupKeeper(t)
			controller := randAddr()
			sigilID := f.registerSigil(t, controller)
			terms := validTerms(sigilID, randAddr())
			if tc.mutate != nil {
				tc.mutate(f, &terms, controller)
			}
			signer := controller
			if tc.signer != nil {
				signer = tc.signer(controller)
			}

			id, err := f.k.IssueMandate(f.ctx, signer, terms)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, id, 64)

			grantee := sdk.MustAccAddressFromBech32(terms.GranteeAddr)
			m, found, err := f.k.ActiveMandateByGrantee(f.ctx, 0, grantee)
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, id, m.Id)
			require.Equal(t, uint64(f.ctx.BlockHeight()), m.WindowStartHeight)
			require.True(t, m.WindowSpent.IsZero())

			// the grantee is registered as a delegate key in the sigil
			// reverse index (SPEC §6.1 key 0x04)
			indexed, err := f.sigil.SigilIDByAddress(f.ctx, 0, grantee)
			require.NoError(t, err)
			require.Equal(t, sigilID, indexed)
		})
	}
}

// TestIssueMandatePrivilegeEscalationGuard proves the SPEC §6.2 step 3 guard
// at issuance for every forbidden TypeURL, including authz MsgGrant and
// feegrant MsgGrantAllowance (D-016): a mandate-bound key that could MsgGrant
// authz over its own account would mint an unbound delegate key that
// bypasses step 1 entirely.
func TestIssueMandatePrivilegeEscalationGuard(t *testing.T) {
	for _, url := range types.ForbiddenMsgTypes() {
		t.Run(url, func(t *testing.T) {
			f := setupKeeper(t)
			controller := randAddr()
			sigilID := f.registerSigil(t, controller)
			terms := validTerms(sigilID, randAddr())
			terms.AllowedMsgTypes = append(terms.AllowedMsgTypes, url)

			_, err := f.k.IssueMandate(f.ctx, controller, terms)
			require.ErrorIs(t, err, types.ErrPrivilegeEscalation)
		})
	}
}

// TestIssueMandateReplace asserts the D2 REPLACE semantics: one active
// mandate per key; reissuing supersedes, and the superseded record is gone.
func TestIssueMandateReplace(t *testing.T) {
	f := setupKeeper(t)
	controller := randAddr()
	sigilID := f.registerSigil(t, controller)
	grantee := randAddr()

	first, err := f.k.IssueMandate(f.ctx, controller, validTerms(sigilID, grantee))
	require.NoError(t, err)

	terms := validTerms(sigilID, grantee)
	terms.SpendCeilingPerTx = sdk.NewCoins(sdk.NewInt64Coin("uusdc", 1))
	second, err := f.k.IssueMandate(f.ctx, controller, terms)
	require.NoError(t, err)
	require.NotEqual(t, first, second)

	m, found, err := f.k.ActiveMandateByGrantee(f.ctx, 0, grantee)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, second, m.Id)

	_, err = f.k.MandateByID(f.ctx, first)
	require.ErrorIs(t, err, types.ErrMandateNotFound)

	bySigil, err := f.k.MandatesBySigil(f.ctx, 0, sigilID)
	require.NoError(t, err)
	require.Len(t, bySigil, 1)
	require.Equal(t, second, bySigil[0].Id)
}

func TestRevokeMandate(t *testing.T) {
	f := setupKeeper(t)
	controller := randAddr()
	sigilID := f.registerSigil(t, controller)
	grantee := randAddr()
	id, err := f.k.IssueMandate(f.ctx, controller, validTerms(sigilID, grantee))
	require.NoError(t, err)

	require.ErrorIs(t, f.k.RevokeMandate(f.ctx, randAddr(), id), types.ErrUnauthorized)
	require.ErrorIs(t, f.k.RevokeMandate(f.ctx, controller, "00ff"), types.ErrInvalidMandateID)

	require.NoError(t, f.k.RevokeMandate(f.ctx, controller, id))

	// D-017: the record and its index entry are retained — the grantee can
	// never silently regain root passthrough
	m, found, err := f.k.ActiveMandateByGrantee(f.ctx, 0, grantee)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, m.Revoked)

	require.ErrorIs(t, f.k.RevokeMandate(f.ctx, controller, id), types.ErrMandateExpired)
}

func TestAmendMandate(t *testing.T) {
	f := setupKeeper(t)
	controller := randAddr()
	sigilID := f.registerSigil(t, controller)
	grantee := randAddr()
	id, err := f.k.IssueMandate(f.ctx, controller, validTerms(sigilID, grantee))
	require.NoError(t, err)

	// charge a live intent COMMITMENT and some per-period SETTLED spend, then
	// amend. D-059: amendment resets the per-period window_spent (settled
	// budget) but CARRIES the lifetime outstanding_commitment, because the
	// grantee's in-flight intents are still live and their funds still committed.
	m, _, err := f.k.ActiveMandateByGrantee(f.ctx, 0, grantee)
	require.NoError(t, err)
	require.NoError(t, f.k.ChargeWindow(f.ctx, m,
		sdk.NewCoins(sdk.NewInt64Coin("uusdc", 100)), // commitment
		sdk.NewCoins(sdk.NewInt64Coin("uusdc", 30)))) // settled

	amended := validTerms(sigilID, grantee)
	amended.SpendCeilingPerTx = sdk.NewCoins(sdk.NewInt64Coin("uusdc", 42))

	require.ErrorIs(t, f.k.AmendMandate(f.ctx, randAddr(), id, amended), types.ErrUnauthorized)

	wrongGrantee := validTerms(sigilID, randAddr())
	require.ErrorIs(t, f.k.AmendMandate(f.ctx, controller, id, wrongGrantee), types.ErrInvalidTerms)

	require.NoError(t, f.k.AmendMandate(f.ctx, controller, id, amended))
	m, found, err := f.k.ActiveMandateByGrantee(f.ctx, 0, grantee)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, amended.SpendCeilingPerTx, m.SpendCeilingPerTx)
	require.True(t, m.WindowSpent.IsZero(), "amendment resets the per-period window_spent")
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("uusdc", 100)).String(), m.OutstandingCommitment.String(),
		"amendment carries forward the lifetime outstanding_commitment (D-059)")

	// amending a revoked mandate is rejected: reissue instead
	require.NoError(t, f.k.RevokeMandate(f.ctx, controller, id))
	require.ErrorIs(t, f.k.AmendMandate(f.ctx, controller, id, amended), types.ErrMandateExpired)
}

func TestChargeWindow(t *testing.T) {
	usdc := func(amt int64) sdk.Coins { return sdk.NewCoins(sdk.NewInt64Coin("uusdc", amt)) }

	cases := []struct {
		name    string
		spends  []sdk.Coins
		heights []int64 // per spend; 0 = keep fixture height
		wantErr error
	}{
		{
			name:   "within ceilings",
			spends: []sdk.Coins{usdc(200_000_000), usdc(300_000_000)},
		},
		{
			name:    "per-tx ceiling exceeded",
			spends:  []sdk.Coins{usdc(500_000_001)},
			wantErr: types.ErrMandateExceeded,
		},
		{
			name:    "window ceiling exceeded by accumulation",
			spends:  []sdk.Coins{usdc(300_000_000), usdc(300_000_000)},
			wantErr: types.ErrMandateExceeded,
		},
		{
			name:    "denom not in ceiling is denied (D-019)",
			spends:  []sdk.Coins{sdk.NewCoins(sdk.NewInt64Coin("uclv", 1))},
			wantErr: types.ErrMandateExceeded,
		},
		{
			name:    "window reset clears spent",
			spends:  []sdk.Coins{usdc(400_000_000), usdc(400_000_000)},
			heights: []int64{0, 10 + 86_400},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := setupKeeper(t)
			controller := randAddr()
			sigilID := f.registerSigil(t, controller)
			grantee := randAddr()
			_, err := f.k.IssueMandate(f.ctx, controller, validTerms(sigilID, grantee))
			require.NoError(t, err)

			var lastErr error
			for i, spend := range tc.spends {
				ctx := f.ctx
				if tc.heights != nil && tc.heights[i] != 0 {
					ctx = f.ctx.WithBlockHeight(tc.heights[i])
				}
				m, found, err := f.k.ActiveMandateByGrantee(ctx, 0, grantee)
				require.NoError(t, err)
				require.True(t, found)
				// these cases exercise per-period SETTLED spend (bank-send
				// style): the window_spent counter that resets each period.
				lastErr = f.k.ChargeWindow(ctx, m, sdk.NewCoins(), spend)
			}
			if tc.wantErr != nil {
				require.ErrorIs(t, lastErr, tc.wantErr)
				return
			}
			require.NoError(t, lastErr)
		})
	}
}

func TestDecommitIntent(t *testing.T) {
	f := setupKeeper(t)
	controller := randAddr()
	sigilID := f.registerSigil(t, controller)
	grantee := randAddr()
	_, err := f.k.IssueMandate(f.ctx, controller, validTerms(sigilID, grantee))
	require.NoError(t, err)

	// an intent commitment lands in outstanding_commitment (not window_spent)
	m, _, err := f.k.ActiveMandateByGrantee(f.ctx, 0, grantee)
	require.NoError(t, err)
	require.NoError(t, f.k.ChargeWindow(f.ctx, m, sdk.NewCoins(sdk.NewInt64Coin("uusdc", 100)), sdk.NewCoins()))
	m, _, err = f.k.ActiveMandateByGrantee(f.ctx, 0, grantee)
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("uusdc", 100)).String(), m.OutstandingCommitment.String())
	require.True(t, m.WindowSpent.IsZero(), "intent commitments never touch the per-period window_spent")

	// decommit more than committed: clamps to zero, never negative (defensive)
	require.NoError(t, f.k.DecommitIntent(f.ctx, 0, grantee, sdk.NewCoins(sdk.NewInt64Coin("uusdc", 250))))
	m, _, err = f.k.ActiveMandateByGrantee(f.ctx, 0, grantee)
	require.NoError(t, err)
	require.True(t, m.OutstandingCommitment.IsZero())

	// decommit to a key without a mandate is a no-op
	require.NoError(t, f.k.DecommitIntent(f.ctx, 0, randAddr(), sdk.NewCoins(sdk.NewInt64Coin("uusdc", 1))))
}

// TestCommitmentSurvivesRolloverNoLeak is the D-059 leak test (the task-0.11
// finding, fixed). An intent commitment is charged, the window period rolls
// over, and the intent then terminates: the decommit must release EXACTLY the
// charge from the lifetime outstanding_commitment and never clamp-and-lose at
// the period boundary. Under the pre-D-059 single window_spent, the rollover
// zeroed the live commitment and this decommit clamped to a no-op — the silent
// refund/capacity leak the simulation surfaced (it broke the global invariant
// at block 2). With outstanding_commitment split out and never reset, the
// release is exact.
func TestCommitmentSurvivesRolloverNoLeak(t *testing.T) {
	f := setupKeeper(t)
	controller := randAddr()
	sigilID := f.registerSigil(t, controller)
	grantee := randAddr()
	terms := validTerms(sigilID, grantee)
	terms.WindowBlocks = 10
	_, err := f.k.IssueMandate(f.ctx, controller, terms)
	require.NoError(t, err)

	commit := sdk.NewCoins(sdk.NewInt64Coin("uusdc", 100))
	m, _, err := f.k.ActiveMandateByGrantee(f.ctx, 0, grantee)
	require.NoError(t, err)
	require.NoError(t, f.k.ChargeWindow(f.ctx, m, commit, sdk.NewCoins()))

	// roll the window over (advance well past window_blocks); a settled charge
	// triggers the per-period reset branch
	rolled := f.ctx.WithBlockHeight(f.ctx.BlockHeight() + 100)
	m, _, err = f.k.ActiveMandateByGrantee(rolled, 0, grantee)
	require.NoError(t, err)
	require.NoError(t, f.k.ChargeWindow(rolled, m, sdk.NewCoins(), sdk.NewCoins(sdk.NewInt64Coin("uusdc", 5))))

	// the commitment SURVIVES the rollover; only window_spent reset
	m, _, err = f.k.ActiveMandateByGrantee(rolled, 0, grantee)
	require.NoError(t, err)
	require.Equal(t, commit.String(), m.OutstandingCommitment.String(),
		"the live intent commitment survives the window rollover")
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("uusdc", 5)).String(), m.WindowSpent.String(),
		"only the per-period settled counter reset")

	// the intent terminates AFTER the rollover: the decommit releases exactly
	// the charge — no clamp-loss
	require.NoError(t, f.k.DecommitIntent(rolled, 0, grantee, commit))
	m, _, err = f.k.ActiveMandateByGrantee(rolled, 0, grantee)
	require.NoError(t, err)
	require.True(t, m.OutstandingCommitment.IsZero(),
		"decommit after a rollover is exact and never clamp-loses (D-059)")
}

// TestOverCommitmentRejectedAcrossRollover proves Option Y (free the live
// commitment at the period boundary) would have been WRONG, and is the §1.1
// goal-3 "leaked key is bounded" guarantee made explicit across a reset. An
// intent commits the full window ceiling; the window rolls over; a NEW intent
// is REJECTED because the still-live commitment continues to count against the
// ceiling. Only once the intent terminates (decommit) does capacity free up.
func TestOverCommitmentRejectedAcrossRollover(t *testing.T) {
	f := setupKeeper(t)
	controller := randAddr()
	sigilID := f.registerSigil(t, controller)
	grantee := randAddr()
	terms := validTerms(sigilID, grantee)
	terms.WindowBlocks = 10
	terms.SpendCeilingPerTx = sdk.NewCoins(sdk.NewInt64Coin("uusdc", 100))
	terms.SpendCeilingWindow = sdk.NewCoins(sdk.NewInt64Coin("uusdc", 100))
	_, err := f.k.IssueMandate(f.ctx, controller, terms)
	require.NoError(t, err)

	// commit the full window ceiling as one live intent
	m, _, err := f.k.ActiveMandateByGrantee(f.ctx, 0, grantee)
	require.NoError(t, err)
	require.NoError(t, f.k.ChargeWindow(f.ctx, m, sdk.NewCoins(sdk.NewInt64Coin("uusdc", 100)), sdk.NewCoins()))

	// roll the window over, then attempt a NEW intent commitment: REJECTED,
	// because outstanding_commitment (100) + new (1) exceeds the ceiling (100).
	// The live commitment is NOT freed by the rollover — this is the
	// over-commitment that Option Y would have allowed.
	rolled := f.ctx.WithBlockHeight(f.ctx.BlockHeight() + 100)
	m, _, err = f.k.ActiveMandateByGrantee(rolled, 0, grantee)
	require.NoError(t, err)
	require.ErrorIs(t,
		f.k.ChargeWindow(rolled, m, sdk.NewCoins(sdk.NewInt64Coin("uusdc", 1)), sdk.NewCoins()),
		types.ErrMandateExceeded,
		"a live commitment must keep bounding the ceiling across a rollover")

	// once the intent terminates, the freed capacity is usable again
	require.NoError(t, f.k.DecommitIntent(rolled, 0, grantee, sdk.NewCoins(sdk.NewInt64Coin("uusdc", 100))))
	m, _, err = f.k.ActiveMandateByGrantee(rolled, 0, grantee)
	require.NoError(t, err)
	require.NoError(t, f.k.ChargeWindow(rolled, m, sdk.NewCoins(sdk.NewInt64Coin("uusdc", 80)), sdk.NewCoins()),
		"after the intent terminates the ceiling capacity is available again")
}

func TestFeeAbstraction(t *testing.T) {
	f := setupKeeper(t)
	controller := randAddr()
	sigilID := f.registerSigil(t, controller)
	grantee := randAddr()
	sponsor := randAddr()

	terms := validTerms(sigilID, grantee)
	terms.FeeSponsor = sponsor.String()
	_, err := f.k.IssueMandate(f.ctx, controller, terms)
	require.NoError(t, err)
	m, _, err := f.k.ActiveMandateByGrantee(f.ctx, 0, grantee)
	require.NoError(t, err)

	usdcFee := sdk.NewCoins(sdk.NewInt64Coin("uusdc", 1_000))

	// predicate table
	_, applies := f.k.FeeAbstractionMandate(f.ctx, grantee, usdcFee)
	require.True(t, applies)
	_, applies = f.k.FeeAbstractionMandate(f.ctx, randAddr(), usdcFee)
	require.False(t, applies, "root keys never abstract")
	_, applies = f.k.FeeAbstractionMandate(f.ctx, grantee, sdk.NewCoins(sdk.NewInt64Coin("uclv", 1)))
	require.False(t, applies, "native denom uses the standard path")
	_, applies = f.k.FeeAbstractionMandate(f.ctx, grantee, sdk.NewCoins(sdk.NewInt64Coin("uatom", 1)))
	require.False(t, applies, "unlisted denom never abstracts")
	_, applies = f.k.FeeAbstractionMandate(f.ctx, grantee,
		sdk.NewCoins(sdk.NewInt64Coin("uusdc", 1), sdk.NewInt64Coin("uclv", 1)))
	require.False(t, applies, "multi-coin fees never abstract")

	// pool unfunded → ErrFeePoolInsufficient. (The mock bank has no
	// rollback; in production the ante failure discards the partial uusdc
	// transfer along with all other ante writes.)
	f.bank.fund(sponsor.String(), sdk.NewCoins(sdk.NewInt64Coin("uusdc", 1_000_000)))
	require.ErrorIs(t, f.k.ExecuteFeeAbstraction(f.ctx, m, usdcFee), types.ErrFeePoolInsufficient)

	// funded pool: sponsor pays uusdc into the pool, the pool pays uclv to
	// the fee collector at the 1:1 dev rate — no mint, no burn
	poolAddr := authtypes.NewModuleAddress(types.ModuleName)
	collectorAddr := authtypes.NewModuleAddress(authtypes.FeeCollectorName)
	f.bank.fund(poolAddr.String(), sdk.NewCoins(sdk.NewInt64Coin("uclv", 1_000_000)))

	require.NoError(t, f.k.ExecuteFeeAbstraction(f.ctx, m, usdcFee))
	// 2_000 uusdc total: 1_000 from the rolled-back-in-production attempt
	// above, 1_000 from the successful conversion
	require.Equal(t, math.NewInt(2_000), f.bank.GetBalance(f.ctx, poolAddr, "uusdc").Amount)
	require.Equal(t, math.NewInt(1_000_000-1_000), f.bank.GetBalance(f.ctx, poolAddr, "uclv").Amount)
	require.Equal(t, math.NewInt(1_000), f.bank.GetBalance(f.ctx, collectorAddr, "uclv").Amount)
	require.Equal(t, math.NewInt(1_000_000-2_000), f.bank.GetBalance(f.ctx, sponsor, "uusdc").Amount)
	require.True(t, f.bank.GetBalance(f.ctx, sdk.MustAccAddressFromBech32(m.GranteeAddr), "uusdc").IsZero(),
		"grantee never pays when a sponsor is set")
}

func TestInvariants(t *testing.T) {
	f := setupKeeper(t)
	controller := randAddr()
	sigilID := f.registerSigil(t, controller)
	grantee := randAddr()
	_, err := f.k.IssueMandate(f.ctx, controller, validTerms(sigilID, grantee))
	require.NoError(t, err)

	_, broken := GranteeResolvesToSigilInvariant(f.k)(f.ctx)
	require.False(t, broken)
	_, broken = GranteeIndexInvariant(f.k)(f.ctx)
	require.False(t, broken)

	// break the sigil reverse index: the cross-module invariant must trip
	require.NoError(t, f.sigil.SetAddressIndex(f.ctx, 0,
		grantee, "00000000000000000000000000000000000000000000000000000000000000ff"))
	_, broken = GranteeResolvesToSigilInvariant(f.k)(f.ctx)
	require.True(t, broken)
}

func TestGenesisRoundTrip(t *testing.T) {
	f := setupKeeper(t)
	controller := randAddr()
	sigilID := f.registerSigil(t, controller)
	grantee := randAddr()
	id, err := f.k.IssueMandate(f.ctx, controller, validTerms(sigilID, grantee))
	require.NoError(t, err)

	exported := f.k.ExportGenesis(f.ctx)
	require.NoError(t, exported.Validate())
	require.Len(t, exported.Mandates, 1)
	require.Equal(t, id, exported.Mandates[0].Id)

	f2 := setupKeeper(t)
	// the sigil must exist on the fresh state for the delegate index write
	h := sha256.Sum256([]byte("did-doc"))
	require.NoError(t, f2.sigil.SetSigil(f2.ctx, sigiltypes.Sigil{
		Id: sigilID, ControllerAddr: controller.String(), DidDocHash: h[:],
	}))
	f2.k.InitGenesis(f2.ctx, *exported)

	m, found, err := f2.k.ActiveMandateByGrantee(f2.ctx, 0, grantee)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, id, m.Id)
}
