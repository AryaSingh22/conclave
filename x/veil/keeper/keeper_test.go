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
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/AryaSingh22/conclave/crypto/reveal"
	mandatekeeper "github.com/AryaSingh22/conclave/x/mandate/keeper"
	mandatetypes "github.com/AryaSingh22/conclave/x/mandate/types"
	sigilkeeper "github.com/AryaSingh22/conclave/x/sigil/keeper"
	sigiltypes "github.com/AryaSingh22/conclave/x/sigil/types"
	"github.com/AryaSingh22/conclave/x/veil/types"
)

const (
	bondDenom = "uclv"
	usdcDenom = "uusdc"

	// dev session cadence used throughout the tests (SPEC §6.4 default 3)
	testSessionLength = 3
)

// mockBank is a minimal in-memory bank keeper satisfying both the x/veil
// and x/mandate BankKeeper interfaces; module accounts live under their
// derived module addresses.
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
		return errorsmod.Wrapf(types.ErrInvalidBond, "mock bank: %s holds %s, needs %s", from, have, amt)
	}
	b.balances[from] = have.Sub(amt...)
	b.balances[to] = b.balances[to].Add(amt...)
	return nil
}

func (b *mockBank) SendCoinsFromAccountToModule(_ context.Context, sender sdk.AccAddress, module string, amt sdk.Coins) error {
	return b.send(sender.String(), authtypes.NewModuleAddress(module).String(), amt)
}

func (b *mockBank) SendCoinsFromModuleToAccount(_ context.Context, module string, recipient sdk.AccAddress, amt sdk.Coins) error {
	return b.send(authtypes.NewModuleAddress(module).String(), recipient.String(), amt)
}

func (b *mockBank) SendCoinsFromModuleToModule(_ context.Context, senderModule, recipientModule string, amt sdk.Coins) error {
	return b.send(authtypes.NewModuleAddress(senderModule).String(), authtypes.NewModuleAddress(recipientModule).String(), amt)
}

func (b *mockBank) GetBalance(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	return sdk.NewCoin(denom, b.balances[addr.String()].AmountOf(denom))
}

func (b *mockBank) balanceOf(addr string) sdk.Coins { return b.balances[addr] }

// total sums every balance the mock holds (bond-conservation assertions).
func (b *mockBank) total(denom string) math.Int {
	sum := math.ZeroInt()
	for _, coins := range b.balances {
		sum = sum.Add(coins.AmountOf(denom))
	}
	return sum
}

// mockDistr routes FundCommunityPool through the mock bank into the
// distribution module address, mirroring the real keeper's money flow.
type mockDistr struct {
	bank *mockBank
}

func (d mockDistr) FundCommunityPool(_ context.Context, amount sdk.Coins, sender sdk.AccAddress) error {
	return d.bank.send(sender.String(), authtypes.NewModuleAddress(distrtypes.ModuleName).String(), amount)
}

type mockAccount struct{}

func (mockAccount) GetModuleAddress(name string) sdk.AccAddress {
	return authtypes.NewModuleAddress(name)
}

func (mockAccount) GetModuleAccount(_ context.Context, name string) sdk.ModuleAccountI {
	return authtypes.NewEmptyModuleAccount(name)
}

// testingT is the test surface shared by *testing.T and *rapid.T (rapid's
// T lacks testing.TB's Cleanup), so the fixture serves units and fuzz alike.
type testingT interface {
	require.TestingT
	Helper()
	Fatalf(format string, args ...any)
}

type fixture struct {
	ctx     sdk.Context
	k       Keeper
	sigil   sigilkeeper.Keeper
	mandate mandatekeeper.Keeper
	bank    *mockBank
}

var (
	veilModuleAddr  = authtypes.NewModuleAddress(types.ModuleName).String()
	distrModuleAddr = authtypes.NewModuleAddress(distrtypes.ModuleName).String()
)

func setupKeeper(t testingT) *fixture {
	t.Helper()
	veilKey := storetypes.NewKVStoreKey(types.StoreKey)
	sigilKey := storetypes.NewKVStoreKey(sigiltypes.StoreKey)
	mandateKey := storetypes.NewKVStoreKey(mandatetypes.StoreKey)
	ctx := testutil.DefaultContextWithKeys(
		map[string]*storetypes.KVStoreKey{
			types.StoreKey:        veilKey,
			sigiltypes.StoreKey:   sigilKey,
			mandatetypes.StoreKey: mandateKey,
		},
		nil, nil,
	)
	encCfg := moduletestutil.MakeTestEncodingConfig()
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	bank := newMockBank()
	sk := sigilkeeper.NewKeeper(encCfg.Codec, runtime.NewKVStoreService(sigilKey), authority)
	mk := mandatekeeper.NewKeeper(
		encCfg.Codec, runtime.NewKVStoreService(mandateKey),
		sk, bank, mockAccount{}, authority,
	)
	k := NewKeeper(
		encCfg.Codec, runtime.NewKVStoreService(veilKey),
		reveal.StubScheme{},
		bank, mockDistr{bank: bank}, sk, mk,
		types.NewStubSessionKeeper(testSessionLength),
		mandatetypes.FaceValuePriceSource{},
		authority,
	)

	ctx = ctx.WithBlockHeight(1)
	require.NoError(t, sk.SetParams(ctx, sigiltypes.DefaultParams()))
	require.NoError(t, mk.SetParams(ctx, mandatetypes.DefaultParams()))
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))
	return &fixture{ctx: ctx, k: k, sigil: sk, mandate: mk, bank: bank}
}

func randAddr() sdk.AccAddress {
	return sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
}

func (f *fixture) registerSigil(t testingT, controller sdk.AccAddress) string {
	t.Helper()
	h := sha256.Sum256([]byte("did-doc"))
	id, err := f.sigil.RegisterSigil(f.ctx, controller, h[:], "ipfs://meta", nil, 0, 0)
	require.NoError(t, err)
	return id
}

// beginBlock advances the fixture to height h and runs the veil
// BeginBlocker (the Reveal), mirroring real ABCI phase order.
func (f *fixture) beginBlock(t testingT, h int64) {
	t.Helper()
	f.ctx = f.ctx.WithBlockHeight(h)
	require.NoError(t, f.k.BeginBlocker(f.ctx))
}

// endBlock runs the veil EndBlocker (expiry sweep + boundary seal) at the
// fixture's current height.
func (f *fixture) endBlock(t testingT) {
	t.Helper()
	require.NoError(t, f.k.EndBlocker(f.ctx))
}

func payloadBytes(t testingT, give ...sdk.Coin) []byte {
	t.Helper()
	payload := types.IntentPayload{}
	for i := range give {
		coin := give[i]
		payload.Give = append(payload.Give, types.IntentLeg{Asset: &types.IntentLeg_Coin{Coin: &coin}})
	}
	bz, err := payload.Marshal()
	require.NoError(t, err)
	return bz
}

func envelopeBytes(t testingT, ttl uint64, feeBid string) []byte {
	t.Helper()
	env := types.IntentEnvelope{TtlHeight: ttl, FeeBid: feeBid}
	bz, err := env.Marshal()
	require.NoError(t, err)
	return bz
}

func minBond() sdk.Coin { return sdk.NewInt64Coin(bondDenom, 1_000_000) }

func submitMsg(owner sdk.AccAddress, sigilID string, ciphertext, envelope []byte, bond sdk.Coin) *types.MsgSubmitIntent {
	return &types.MsgSubmitIntent{
		Sender:      owner.String(),
		SenderSigil: sigilID,
		Ciphertext:  ciphertext,
		Envelope:    envelope,
		Bond:        bond,
	}
}

// submitValid registers a sigil for owner (if needed), funds it, and
// submits a valid 100-USDC give intent, returning the intent id.
func (f *fixture) submitValid(t testingT, owner sdk.AccAddress, sigilID string, ttl uint64) string {
	t.Helper()
	f.bank.fund(owner.String(), sdk.NewCoins(sdk.NewInt64Coin(bondDenom, 100_000_000)))
	msg := submitMsg(owner, sigilID,
		payloadBytes(t, sdk.NewInt64Coin(usdcDenom, 100_000_000)),
		envelopeBytes(t, ttl, "1.5"), minBond())
	id, err := f.k.SubmitIntent(f.ctx, msg)
	require.NoError(t, err)
	return id
}

func (f *fixture) mustRecord(t testingT, id string) types.IntentRecord {
	t.Helper()
	rec, err := f.k.GetIntentRecord(f.ctx, 0, id)
	require.NoError(t, err)
	return rec
}

func TestSubmitIntent(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		f := setupKeeper(t)
		owner := randAddr()
		sigilID := f.registerSigil(t, owner)
		f.bank.fund(owner.String(), sdk.NewCoins(sdk.NewInt64Coin(bondDenom, 10_000_000)))

		msg := submitMsg(owner, sigilID,
			payloadBytes(t, sdk.NewInt64Coin(usdcDenom, 100_000_000)),
			envelopeBytes(t, 100, "2.5"), minBond())
		id, err := f.k.SubmitIntent(f.ctx, msg)
		require.NoError(t, err)
		require.Len(t, id, 64, "ids are 64-char hex (D-009)")

		rec := f.mustRecord(t, id)
		require.Equal(t, types.INTENT_STATE_SUBMITTED, rec.State)
		require.Equal(t, owner.String(), rec.Owner)
		require.Equal(t, sigilID, rec.Intent.SenderSigil)
		require.Equal(t, uint64(1), rec.Intent.SubmitHeight)
		require.Equal(t, uint64(0), rec.SessionId)

		// charged_spend persists exactly the D-025 ante charge: per-denom
		// max of give legs + bond
		require.Equal(t,
			sdk.NewCoins(sdk.NewInt64Coin(usdcDenom, 100_000_000), minBond()).String(),
			rec.ChargedSpend.String())

		// bond escrowed in the module account
		require.Equal(t, minBond().Amount,
			f.bank.balanceOf(veilModuleAddr).AmountOf(bondDenom))
	})

	t.Run("failure paths", func(t *testing.T) {
		f := setupKeeper(t)
		owner := randAddr()
		sigilID := f.registerSigil(t, owner)
		f.bank.fund(owner.String(), sdk.NewCoins(sdk.NewInt64Coin(bondDenom, 10_000_000)))
		goodPayload := payloadBytes(t, sdk.NewInt64Coin(usdcDenom, 100_000_000))
		goodEnv := envelopeBytes(t, 100, "")

		cases := []struct {
			name string
			msg  *types.MsgSubmitIntent
			err  error
		}{
			{"undecodable envelope",
				submitMsg(owner, sigilID, goodPayload, []byte{0xFF, 0xFF, 0xFF}, minBond()),
				types.ErrInvalidEnvelope},
			{"ttl not beyond submit height",
				submitMsg(owner, sigilID, goodPayload, envelopeBytes(t, 1, ""), minBond()),
				types.ErrInvalidEnvelope},
			{"garbage fee_bid",
				submitMsg(owner, sigilID, goodPayload, envelopeBytes(t, 100, "not-a-dec"), minBond()),
				types.ErrInvalidEnvelope},
			{"negative fee_bid",
				submitMsg(owner, sigilID, goodPayload, envelopeBytes(t, 100, "-1"), minBond()),
				types.ErrInvalidEnvelope},
			{"nonzero msg chamber",
				&types.MsgSubmitIntent{Sender: owner.String(), SenderSigil: sigilID,
					Ciphertext: goodPayload, Envelope: goodEnv, Bond: minBond(), ChamberId: 7},
				types.ErrInvalidChamber},
			{"unregistered sender",
				submitMsg(randAddr(), sigilID, goodPayload, goodEnv, minBond()),
				types.ErrSigilMismatch},
			{"wrong sigil id",
				submitMsg(owner, "deadbeef", goodPayload, goodEnv, minBond()),
				types.ErrSigilMismatch},
			{"wrong bond denom",
				submitMsg(owner, sigilID, goodPayload, goodEnv, sdk.NewInt64Coin(usdcDenom, 1_000_000)),
				types.ErrInvalidBond},
			{"bond below min_bond",
				submitMsg(owner, sigilID, goodPayload, goodEnv, sdk.NewInt64Coin(bondDenom, 999_999)),
				types.ErrBondInsufficient},
			{"bond below bond_bps requirement",
				// face value 10^12 → bps requirement 10^9 uclv > min_bond
				submitMsg(owner, sigilID,
					payloadBytes(t, sdk.NewCoin(usdcDenom, math.NewInt(1_000_000_000_000))),
					goodEnv, minBond()),
				types.ErrBondInsufficient},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := f.k.SubmitIntent(f.ctx, tc.msg)
				require.ErrorIs(t, err, tc.err)
			})
		}

		// no bond movement on any rejection
		require.True(t, f.bank.balanceOf(veilModuleAddr).IsZero())
	})

	t.Run("insufficient funds for bond", func(t *testing.T) {
		f := setupKeeper(t)
		owner := randAddr()
		sigilID := f.registerSigil(t, owner)
		// owner unfunded
		_, err := f.k.SubmitIntent(f.ctx, submitMsg(owner, sigilID,
			payloadBytes(t, sdk.NewInt64Coin(usdcDenom, 1)),
			envelopeBytes(t, 100, ""), minBond()))
		require.Error(t, err)
	})

	t.Run("malformed payload accepted at submit", func(t *testing.T) {
		// payload validity is the Reveal's job (SPEC §6.3): submit must
		// accept garbage ciphertext with the min bond (face value zero)
		f := setupKeeper(t)
		owner := randAddr()
		sigilID := f.registerSigil(t, owner)
		f.bank.fund(owner.String(), sdk.NewCoins(sdk.NewInt64Coin(bondDenom, 10_000_000)))

		id, err := f.k.SubmitIntent(f.ctx, submitMsg(owner, sigilID,
			[]byte{0xDE, 0xAD, 0xBE, 0xEF}, envelopeBytes(t, 100, ""), minBond()))
		require.NoError(t, err)
		rec := f.mustRecord(t, id)
		// malformed → zero give spend (D-021); charged_spend is bond only
		require.Equal(t, sdk.NewCoins(minBond()).String(), rec.ChargedSpend.String())
	})
}

func TestRequiredBond(t *testing.T) {
	params := types.DefaultParams() // min 1 CLV, 10 bps
	cases := []struct {
		name      string
		faceValue math.Int
		want      math.Int
	}{
		{"zero face value → min bond", math.ZeroInt(), math.NewInt(1_000_000)},
		{"small face value → min bond", math.NewInt(100_000_000), math.NewInt(1_000_000)},
		{"breakeven face value", math.NewInt(1_000_000_000), math.NewInt(1_000_000)},
		{"large face value → bps", math.NewInt(1_000_000_000_000), math.NewInt(1_000_000_000)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, params.RequiredBond(tc.faceValue).Amount)
		})
	}
}

// TestLifecycleSealRevealOpen drives the §5.2 happy path through the real
// ABCI phase order and proves the order-then-reveal invariant: ordering
// fixes at the boundary EndBlock, the Reveal opens strictly in the NEXT
// block's BeginBlock — never the same block.
func TestLifecycleSealRevealOpen(t *testing.T) {
	f := setupKeeper(t)
	owner := randAddr()
	sigilID := f.registerSigil(t, owner)

	// block 1: submit
	f.beginBlock(t, 1)
	id := f.submitValid(t, owner, sigilID, 100)
	f.endBlock(t)
	require.Equal(t, types.INTENT_STATE_SUBMITTED, f.mustRecord(t, id).State,
		"height 1 is not a boundary (cadence 3)")

	// block 2: nothing happens
	f.beginBlock(t, 2)
	f.endBlock(t)
	require.Equal(t, types.INTENT_STATE_SUBMITTED, f.mustRecord(t, id).State)

	// block 3 = boundary: BeginBlock must NOT open anything; EndBlock seals
	f.beginBlock(t, 3)
	require.Equal(t, types.INTENT_STATE_SUBMITTED, f.mustRecord(t, id).State)
	f.endBlock(t)
	rec := f.mustRecord(t, id)
	require.Equal(t, types.INTENT_STATE_SEALED_IN_SESSION, rec.State)
	require.Equal(t, uint64(3), rec.SessionId, "stub session id = boundary height (D-038)")

	// block 4: the Reveal opens it in BeginBlock — strictly the next block
	f.beginBlock(t, 4)
	require.Equal(t, types.INTENT_STATE_OPEN, f.mustRecord(t, id).State)
}

// TestRevealRequiresStrictlyEarlierOrdering pins the invariant directly:
// a record sealed at the CURRENT height (session id == height) must not
// open — ordering and Reveal can never share a block.
func TestRevealRequiresStrictlyEarlierOrdering(t *testing.T) {
	f := setupKeeper(t)
	owner := randAddr()
	sigilID := f.registerSigil(t, owner)

	f.beginBlock(t, 3)
	id := f.submitValid(t, owner, sigilID, 100)
	require.NoError(t, f.k.SealSubmitted(f.ctx, 3))

	// a (hypothetical) Reveal in the same block must skip it
	require.NoError(t, f.k.RevealSealed(f.ctx))
	require.Equal(t, types.INTENT_STATE_SEALED_IN_SESSION, f.mustRecord(t, id).State)

	// next block opens it
	f.beginBlock(t, 4)
	require.Equal(t, types.INTENT_STATE_OPEN, f.mustRecord(t, id).State)
}

// TestRevealInvalidSlash is the §10 row 0.7 INVALID acceptance: a payload
// failing proto-unmarshal at the Reveal turns INVALID, 50% of the bond
// funds the community pool, the remainder refunds, and the mandate window
// is NOT refunded (D-040).
func TestRevealInvalidSlash(t *testing.T) {
	f := setupKeeper(t)
	controller := randAddr()
	grantee := randAddr()
	sigilID := f.registerSigil(t, controller)

	// bind the grantee to the sigil via a real mandate so the window-refund
	// behavior is observable end to end
	_, err := f.mandate.IssueMandate(f.ctx, controller, mandatetypes.MandateTerms{
		SigilId:            sigilID,
		GranteeAddr:        grantee.String(),
		AllowedMsgTypes:    []string{sdk.MsgTypeURL(&types.MsgSubmitIntent{})},
		SpendCeilingPerTx:  sdk.NewCoins(sdk.NewInt64Coin(bondDenom, 50_000_000)),
		SpendCeilingWindow: sdk.NewCoins(sdk.NewInt64Coin(bondDenom, 50_000_000)),
		WindowBlocks:       1000,
		ExpiryHeight:       1_000_000,
	})
	require.NoError(t, err)

	f.beginBlock(t, 1)
	f.bank.fund(grantee.String(), sdk.NewCoins(sdk.NewInt64Coin(bondDenom, 10_000_000)))
	msg := submitMsg(grantee, sigilID, []byte{0xDE, 0xAD, 0xBE, 0xEF},
		envelopeBytes(t, 100, ""), minBond())
	id, err := f.k.SubmitIntent(f.ctx, msg)
	require.NoError(t, err)

	// charge the window as the ante would (bond only — malformed payload)
	m, found, err := f.mandate.ActiveMandateByGrantee(f.ctx, 0, grantee)
	require.NoError(t, err)
	require.True(t, found)
	require.NoError(t, f.mandate.ChargeWindow(f.ctx, m, f.mustRecord(t, id).ChargedSpend, sdk.NewCoins()))

	// boundary seals, next BeginBlock reveals → INVALID
	f.ctx = f.ctx.WithBlockHeight(3)
	f.endBlock(t)
	f.beginBlock(t, 4)

	rec := f.mustRecord(t, id)
	require.Equal(t, types.INTENT_STATE_INVALID, rec.State)

	// 50% slash to the community pool, 50% back to the owner
	half := minBond().Amount.QuoRaw(2)
	require.Equal(t, half, f.bank.balanceOf(distrModuleAddr).AmountOf(bondDenom))
	require.Equal(t, math.NewInt(10_000_000).Sub(minBond().Amount).Add(half),
		f.bank.balanceOf(grantee.String()).AmountOf(bondDenom))
	require.True(t, f.bank.balanceOf(veilModuleAddr).IsZero(),
		"the module account holds nothing after slash+refund")

	// the window stays charged: INVALID never refunds (D-040)
	m, found, err = f.mandate.ActiveMandateByGrantee(f.ctx, 0, grantee)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, sdk.NewCoins(minBond()).String(), m.OutstandingCommitment.String(),
		"INVALID intents keep their outstanding commitment (D-040/D-059)")
}

// TestExpiry drives OPEN → EXPIRED through the EndBlock sweep: full bond
// refund plus the §6.2 step 5 window refund of the stored charged_spend.
func TestExpiry(t *testing.T) {
	f := setupKeeper(t)
	controller := randAddr()
	grantee := randAddr()
	sigilID := f.registerSigil(t, controller)

	_, err := f.mandate.IssueMandate(f.ctx, controller, mandatetypes.MandateTerms{
		SigilId:            sigilID,
		GranteeAddr:        grantee.String(),
		AllowedMsgTypes:    []string{sdk.MsgTypeURL(&types.MsgSubmitIntent{})},
		SpendCeilingPerTx:  sdk.NewCoins(sdk.NewInt64Coin(usdcDenom, 500_000_000), sdk.NewInt64Coin(bondDenom, 50_000_000)),
		SpendCeilingWindow: sdk.NewCoins(sdk.NewInt64Coin(usdcDenom, 500_000_000), sdk.NewInt64Coin(bondDenom, 50_000_000)),
		WindowBlocks:       1000,
		ExpiryHeight:       1_000_000,
	})
	require.NoError(t, err)

	f.beginBlock(t, 1)
	f.bank.fund(grantee.String(), sdk.NewCoins(sdk.NewInt64Coin(bondDenom, 10_000_000)))
	id, err := f.k.SubmitIntent(f.ctx, submitMsg(grantee, sigilID,
		payloadBytes(t, sdk.NewInt64Coin(usdcDenom, 100_000_000)),
		envelopeBytes(t, 5, ""), minBond()))
	require.NoError(t, err)

	rec := f.mustRecord(t, id)
	m, found, err := f.mandate.ActiveMandateByGrantee(f.ctx, 0, grantee)
	require.NoError(t, err)
	require.True(t, found)
	require.NoError(t, f.mandate.ChargeWindow(f.ctx, m, rec.ChargedSpend, sdk.NewCoins()))

	// seal at 3, open at 4; ttl 5 hits at EndBlock(5)
	f.ctx = f.ctx.WithBlockHeight(3)
	f.endBlock(t)
	f.beginBlock(t, 4)
	require.Equal(t, types.INTENT_STATE_OPEN, f.mustRecord(t, id).State)
	f.endBlock(t)
	require.Equal(t, types.INTENT_STATE_OPEN, f.mustRecord(t, id).State, "ttl 5 not reached at 4")

	f.beginBlock(t, 5)
	f.endBlock(t)
	rec = f.mustRecord(t, id)
	require.Equal(t, types.INTENT_STATE_EXPIRED, rec.State)

	// full bond refund
	require.Equal(t, math.NewInt(10_000_000),
		f.bank.balanceOf(grantee.String()).AmountOf(bondDenom))
	// full window refund of the stored charge
	m, found, err = f.mandate.ActiveMandateByGrantee(f.ctx, 0, grantee)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, m.OutstandingCommitment.IsZero(),
		"EXPIRED decommits the stored charged_spend (D-041/D-059)")
}

func TestCancelIntent(t *testing.T) {
	t.Run("cancel from SUBMITTED refunds bond and window", func(t *testing.T) {
		f := setupKeeper(t)
		controller := randAddr()
		grantee := randAddr()
		sigilID := f.registerSigil(t, controller)
		_, err := f.mandate.IssueMandate(f.ctx, controller, mandatetypes.MandateTerms{
			SigilId:            sigilID,
			GranteeAddr:        grantee.String(),
			AllowedMsgTypes:    []string{sdk.MsgTypeURL(&types.MsgSubmitIntent{})},
			SpendCeilingPerTx:  sdk.NewCoins(sdk.NewInt64Coin(usdcDenom, 500_000_000), sdk.NewInt64Coin(bondDenom, 50_000_000)),
			SpendCeilingWindow: sdk.NewCoins(sdk.NewInt64Coin(usdcDenom, 500_000_000), sdk.NewInt64Coin(bondDenom, 50_000_000)),
			WindowBlocks:       1000,
			ExpiryHeight:       1_000_000,
		})
		require.NoError(t, err)

		f.beginBlock(t, 1)
		f.bank.fund(grantee.String(), sdk.NewCoins(sdk.NewInt64Coin(bondDenom, 10_000_000)))
		id, err := f.k.SubmitIntent(f.ctx, submitMsg(grantee, sigilID,
			payloadBytes(t, sdk.NewInt64Coin(usdcDenom, 100_000_000)),
			envelopeBytes(t, 100, ""), minBond()))
		require.NoError(t, err)
		rec := f.mustRecord(t, id)
		m, found, err := f.mandate.ActiveMandateByGrantee(f.ctx, 0, grantee)
		require.NoError(t, err)
		require.True(t, found)
		require.NoError(t, f.mandate.ChargeWindow(f.ctx, m, rec.ChargedSpend, sdk.NewCoins()))

		require.NoError(t, f.k.CancelIntent(f.ctx, grantee.String(), id))
		require.Equal(t, types.INTENT_STATE_CANCELLED, f.mustRecord(t, id).State)
		require.Equal(t, math.NewInt(10_000_000),
			f.bank.balanceOf(grantee.String()).AmountOf(bondDenom), "full bond refund")

		m, _, err = f.mandate.ActiveMandateByGrantee(f.ctx, 0, grantee)
		require.NoError(t, err)
		require.True(t, m.OutstandingCommitment.IsZero(), "commitment released on cancel (D-059)")
	})

	t.Run("owner-only", func(t *testing.T) {
		f := setupKeeper(t)
		owner := randAddr()
		sigilID := f.registerSigil(t, owner)
		f.beginBlock(t, 1)
		id := f.submitValid(t, owner, sigilID, 100)
		err := f.k.CancelIntent(f.ctx, randAddr().String(), id)
		require.ErrorIs(t, err, types.ErrNotIntentOwner)
	})

	t.Run("unknown intent", func(t *testing.T) {
		f := setupKeeper(t)
		err := f.k.CancelIntent(f.ctx, randAddr().String(), "00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff")
		require.ErrorIs(t, err, types.ErrIntentNotFound)
	})

	t.Run("post-OPEN cancel rejected", func(t *testing.T) {
		f := setupKeeper(t)
		owner := randAddr()
		sigilID := f.registerSigil(t, owner)
		f.beginBlock(t, 1)
		id := f.submitValid(t, owner, sigilID, 100)
		f.ctx = f.ctx.WithBlockHeight(3)
		f.endBlock(t)
		f.beginBlock(t, 4)
		require.Equal(t, types.INTENT_STATE_OPEN, f.mustRecord(t, id).State)

		err := f.k.CancelIntent(f.ctx, owner.String(), id)
		require.ErrorIs(t, err, types.ErrIntentNotCancellable)
	})

	t.Run("cancel beats the boundary in the boundary block (D-039)", func(t *testing.T) {
		f := setupKeeper(t)
		owner := randAddr()
		sigilID := f.registerSigil(t, owner)
		f.beginBlock(t, 1)
		id := f.submitValid(t, owner, sigilID, 100)

		// boundary block 3: the cancel tx executes (DeliverTx) BEFORE the
		// EndBlock seal — the cancel wins and the seal never sees the intent
		f.beginBlock(t, 3)
		require.NoError(t, f.k.CancelIntent(f.ctx, owner.String(), id))
		f.endBlock(t)
		require.Equal(t, types.INTENT_STATE_CANCELLED, f.mustRecord(t, id).State)
	})
}

// TestGenesisRoundTrip proves init/export equality and that the
// (state, ttl) index is rebuilt on import: an imported OPEN intent expires
// through the normal sweep.
func TestGenesisRoundTrip(t *testing.T) {
	f := setupKeeper(t)
	owner := randAddr()
	sigilID := f.registerSigil(t, owner)

	f.beginBlock(t, 1)
	id1 := f.submitValid(t, owner, sigilID, 5)
	id2 := f.submitValid(t, owner, sigilID, 100)
	f.ctx = f.ctx.WithBlockHeight(3)
	f.endBlock(t)
	f.beginBlock(t, 4)
	require.Equal(t, types.INTENT_STATE_OPEN, f.mustRecord(t, id1).State)
	require.Equal(t, types.INTENT_STATE_OPEN, f.mustRecord(t, id2).State)

	exported := f.k.ExportGenesis(f.ctx)
	require.NoError(t, exported.Validate())
	require.Len(t, exported.Intents, 2)

	// import into a fresh keeper
	f2 := setupKeeper(t)
	f2.k.InitGenesis(f2.ctx, *exported)
	reExported := f2.k.ExportGenesis(f2.ctx)
	require.Equal(t, exported, reExported, "genesis round-trips losslessly")

	// the rebuilt index drives the expiry sweep on the imported state
	f2.beginBlock(t, 5)
	f2.bank.fund(veilModuleAddr, sdk.NewCoins(sdk.NewInt64Coin(bondDenom, 2_000_000))) // bonds re-escrowed off-genesis
	f2.endBlock(t)
	require.Equal(t, types.INTENT_STATE_EXPIRED, f2.mustRecord(t, id1).State)
	require.Equal(t, types.INTENT_STATE_OPEN, f2.mustRecord(t, id2).State)
}

func TestParamsValidate(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(p *types.Params)
		ok     bool
	}{
		{"defaults", func(*types.Params) {}, true},
		{"zero min bond", func(p *types.Params) { p.MinBond.Amount = math.ZeroInt() }, false},
		{"bps over 10000", func(p *types.Params) { p.BondBps = 10_001 }, false},
		{"slash over 10000", func(p *types.Params) { p.InvalidSlashBps = 10_001 }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := types.DefaultParams()
			tc.mutate(&p)
			err := p.Validate()
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, types.ErrInvalidParams)
			}
		})
	}
}
