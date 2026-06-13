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
	"github.com/AryaSingh22/conclave/x/session/types"
	sigilkeeper "github.com/AryaSingh22/conclave/x/sigil/keeper"
	sigiltypes "github.com/AryaSingh22/conclave/x/sigil/types"
	veilkeeper "github.com/AryaSingh22/conclave/x/veil/keeper"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

const (
	bondDenom = "uclv"
	usdcDenom = "uusdc"
	svcDenom  = "svc/cmpt"
)

// mockBank is an in-memory bank keeper satisfying the x/session, x/veil, and
// x/mandate BankKeeper interfaces. MintCoins increases supply (svc/{schema}
// vouchers); module accounts live under their derived addresses.
type mockBank struct {
	balances map[string]sdk.Coins
}

func newMockBank() *mockBank { return &mockBank{balances: make(map[string]sdk.Coins)} }

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

func (b *mockBank) MintCoins(_ context.Context, module string, amt sdk.Coins) error {
	addr := authtypes.NewModuleAddress(module).String()
	b.balances[addr] = b.balances[addr].Add(amt...)
	return nil
}

func (b *mockBank) GetBalance(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	return sdk.NewCoin(denom, b.balances[addr.String()].AmountOf(denom))
}

func (b *mockBank) balanceOf(addr string) sdk.Coins { return b.balances[addr] }

// total sums every balance for a denom (supply-conservation assertions).
func (b *mockBank) total(denom string) math.Int {
	sum := math.ZeroInt()
	for _, coins := range b.balances {
		sum = sum.Add(coins.AmountOf(denom))
	}
	return sum
}

// mockDistr routes FundCommunityPool through the mock bank.
type mockDistr struct{ bank *mockBank }

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

// testingT serves both *testing.T and *rapid.T.
type testingT interface {
	require.TestingT
	Helper()
	Fatalf(format string, args ...any)
}

type fixture struct {
	ctx     sdk.Context
	k       *Keeper
	veil    veilkeeper.Keeper
	sigil   sigilkeeper.Keeper
	mandate mandatekeeper.Keeper
	bank    *mockBank
}

var (
	sessionModuleAddr = authtypes.NewModuleAddress(types.ModuleName).String()
	veilModuleAddr    = authtypes.NewModuleAddress(veiltypes.ModuleName).String()
	distrModuleAddr   = authtypes.NewModuleAddress(distrtypes.ModuleName).String()
)

func setupKeeper(t testingT) *fixture {
	t.Helper()
	sessionKey := storetypes.NewKVStoreKey(types.StoreKey)
	veilKey := storetypes.NewKVStoreKey(veiltypes.StoreKey)
	sigilKey := storetypes.NewKVStoreKey(sigiltypes.StoreKey)
	mandateKey := storetypes.NewKVStoreKey(mandatetypes.StoreKey)
	ctx := testutil.DefaultContextWithKeys(
		map[string]*storetypes.KVStoreKey{
			types.StoreKey:        sessionKey,
			veiltypes.StoreKey:    veilKey,
			sigiltypes.StoreKey:   sigilKey,
			mandatetypes.StoreKey: mandateKey,
		}, nil, nil,
	)
	encCfg := moduletestutil.MakeTestEncodingConfig()
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	bank := newMockBank()
	sk := sigilkeeper.NewKeeper(encCfg.Codec, runtime.NewKVStoreService(sigilKey), authority)
	mk := mandatekeeper.NewKeeper(encCfg.Codec, runtime.NewKVStoreService(mandateKey), sk, bank, mockAccount{}, authority)

	// wire the veil↔session cycle exactly as app.go does: session first, veil
	// with session, then SetVeilKeeper (Guard 1).
	sessionK := NewKeeper(encCfg.Codec, runtime.NewKVStoreService(sessionKey), bank, mockDistr{bank: bank}, mockAccount{}, sk, authority)
	veilK := veilkeeper.NewKeeper(
		encCfg.Codec, runtime.NewKVStoreService(veilKey),
		reveal.StubScheme{}, bank, mockDistr{bank: bank}, sk, mk,
		sessionK, mandatetypes.FaceValuePriceSource{}, authority,
	)
	sessionK.SetVeilKeeper(veilK)

	ctx = ctx.WithBlockHeight(1)
	require.NoError(t, sk.SetParams(ctx, sigiltypes.DefaultParams()))
	require.NoError(t, mk.SetParams(ctx, mandatetypes.DefaultParams()))
	require.NoError(t, veilK.SetParams(ctx, veiltypes.DefaultParams()))
	require.NoError(t, sessionK.SetParams(ctx, types.DefaultParams()))
	return &fixture{ctx: ctx, k: sessionK, veil: veilK, sigil: sk, mandate: mk, bank: bank}
}

func randAddr() sdk.AccAddress {
	return sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
}

func (f *fixture) registerSigil(t testingT, controller sdk.AccAddress) string {
	t.Helper()
	h := sha256.Sum256([]byte("did-doc" + controller.String()))
	id, err := f.sigil.RegisterSigil(f.ctx, controller, h[:], "ipfs://meta", nil, 0, 0)
	require.NoError(t, err)
	return id
}

// --- intent payload / submission helpers ---

func coinLeg(c sdk.Coin) veiltypes.IntentLeg {
	return veiltypes.IntentLeg{Asset: &veiltypes.IntentLeg_Coin{Coin: &c}}
}

func svcLeg(schema string, units int64) veiltypes.IntentLeg {
	return veiltypes.IntentLeg{Asset: &veiltypes.IntentLeg_Svc{Svc: &veiltypes.ServiceVoucher{Schema: schema, Units: math.NewInt(units)}}}
}

func dataLeg(contentHash []byte) veiltypes.IntentLeg {
	return veiltypes.IntentLeg{Asset: &veiltypes.IntentLeg_Data{Data: &veiltypes.DataGrant{ContentHash: contentHash}}}
}

func payloadBytes(t testingT, give, want []veiltypes.IntentLeg, counterparty string) []byte {
	t.Helper()
	p := veiltypes.IntentPayload{Give: give, Want: want, CounterpartyClass: counterparty}
	bz, err := p.Marshal()
	require.NoError(t, err)
	return bz
}

func envelopeBytes(t testingT, ttl uint64, allowPartial bool) []byte {
	t.Helper()
	env := veiltypes.IntentEnvelope{TtlHeight: ttl, AllowPartial: allowPartial}
	bz, err := env.Marshal()
	require.NoError(t, err)
	return bz
}

func minBond() sdk.Coin { return sdk.NewInt64Coin(bondDenom, 1_000_000) }

// submitOpen submits an intent via x/veil and returns its id (state SUBMITTED).
// owner == controller in tests, so settlement (ControllerAddress) and refunds
// (owner) resolve to the same account.
func (f *fixture) submitOpen(t testingT, owner sdk.AccAddress, sigilID string, give, want []veiltypes.IntentLeg, ttl uint64, counterparty string) string {
	t.Helper()
	f.bank.fund(owner.String(), sdk.NewCoins(minBond()))
	msg := &veiltypes.MsgSubmitIntent{
		Sender:      owner.String(),
		SenderSigil: sigilID,
		Ciphertext:  payloadBytes(t, give, want, counterparty),
		Envelope:    envelopeBytes(t, ttl, false),
		Bond:        minBond(),
	}
	id, err := f.veil.SubmitIntent(f.ctx, msg)
	require.NoError(t, err)
	return id
}

// openSession seals + reveals the SUBMITTED intents into an AUCTION session at
// the boundary cadence (length 3): seal at EndBlock(3), reveal at BeginBlock(4).
// Returns the session id (the boundary height, D-038).
func (f *fixture) openSession(t testingT) uint64 {
	t.Helper()
	f.ctx = f.ctx.WithBlockHeight(3)
	require.NoError(t, f.veil.SealSubmitted(f.ctx, 3))
	f.ctx = f.ctx.WithBlockHeight(4)
	require.NoError(t, f.veil.RevealSealed(f.ctx)) // → OPEN + IntakeRevealed
	return 3
}

func (f *fixture) registerSolver(t testingT, addr sdk.AccAddress, bond sdk.Coin) {
	t.Helper()
	f.bank.fund(addr.String(), sdk.NewCoins(bond))
	require.NoError(t, f.k.RegisterSolver(f.ctx, addr, sdk.NewCoins(bond)))
}

func minSolverBond() sdk.Coin { return types.DefaultSolverMinBond() }

// hash returns a deterministic 32-byte content hash for test data grants.
func contentHash(tag string) []byte {
	h := sha256.Sum256([]byte(tag))
	return h[:]
}

// cycle holds the 3-party cycle fixture: alice 100 uusdc → data D; bob data D →
// 10 svc; carol 10 svc → 100 uusdc.
type cycle struct {
	aliceSigil, bobSigil, carolSigil string
	alice, bob, carol                sdk.AccAddress
	aliceID, bobID, carolID          string
	dataHash                         []byte
	sessionID                        uint64
}

func (f *fixture) setupCycle(t testingT) cycle {
	t.Helper()
	c := cycle{dataHash: contentHash("dataset-D")}
	c.alice, c.bob, c.carol = randAddr(), randAddr(), randAddr()
	c.aliceSigil = f.registerSigil(t, c.alice)
	c.bobSigil = f.registerSigil(t, c.bob)
	c.carolSigil = f.registerSigil(t, c.carol)

	// alice funds the 100 uusdc settlement leg from her controller account.
	f.bank.fund(c.alice.String(), sdk.NewCoins(sdk.NewInt64Coin(usdcDenom, 100_000_000)))

	usdc := sdk.NewInt64Coin(usdcDenom, 100_000_000)
	c.aliceID = f.submitOpen(t, c.alice, c.aliceSigil, []veiltypes.IntentLeg{coinLeg(usdc)}, []veiltypes.IntentLeg{dataLeg(c.dataHash)}, 100, "")
	c.bobID = f.submitOpen(t, c.bob, c.bobSigil, []veiltypes.IntentLeg{dataLeg(c.dataHash)}, []veiltypes.IntentLeg{svcLeg("cmpt", 10)}, 100, "")
	c.carolID = f.submitOpen(t, c.carol, c.carolSigil, []veiltypes.IntentLeg{svcLeg("cmpt", 10)}, []veiltypes.IntentLeg{coinLeg(usdc)}, 100, "")

	c.sessionID = f.openSession(t)
	return c
}

// matches builds the canonical 3-leg cycle settlement.
func (c cycle) matches() []types.Match {
	usdc := sdk.NewInt64Coin(usdcDenom, 100_000_000)
	return []types.Match{{
		IntentIds: []string{c.aliceID, c.bobID, c.carolID},
		Legs: []types.SettleLeg{
			{FromSigil: c.aliceSigil, ToSigil: c.carolSigil, Asset: &types.SettleLeg_Coin{Coin: &usdc}},
			{FromSigil: c.bobSigil, ToSigil: c.aliceSigil, Asset: &types.SettleLeg_Data{Data: &veiltypes.DataGrant{ContentHash: c.dataHash}}},
			{FromSigil: c.carolSigil, ToSigil: c.bobSigil, Asset: &types.SettleLeg_Svc{Svc: &veiltypes.ServiceVoucher{Schema: "cmpt", Units: math.NewInt(10)}}},
		},
		ClearingPrices: []types.PricePoint{
			{Base: usdcDenom, Quote: usdcDenom, Price: math.LegacyOneDec()},
			{Base: "data/" + types.IDHex(c.dataHash), Quote: usdcDenom, Price: math.LegacyNewDec(100_000_000)},
			{Base: svcDenom, Quote: usdcDenom, Price: math.LegacyNewDec(10_000_000)},
		},
	}}
}

func TestParamsValidate(t *testing.T) {
	require.NoError(t, types.DefaultParams().Validate())

	cases := []struct {
		name   string
		mutate func(p *types.Params)
	}{
		{"zero session length", func(p *types.Params) { p.SessionLengthBlocks = 0 }},
		{"zero window", func(p *types.Params) { p.SolutionWindowBlocks = 0 }},
		{"zero min bond", func(p *types.Params) { p.SolverMinBond.Amount = math.ZeroInt() }},
		{"fee bps over 10000", func(p *types.Params) { p.SolverFeeBps = 10_001 }},
		{"skim bps over 10000", func(p *types.Params) { p.SurplusSkimBps = 10_001 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := types.DefaultParams()
			tc.mutate(&p)
			require.ErrorIs(t, p.Validate(), types.ErrInvalidParams)
		})
	}
}

// TestSetVeilKeeperGuard proves Guard 1: a session keeper whose veil dependency
// was never wired panics with a clear message on first settlement use.
func TestSetVeilKeeperGuard(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	bank := newMockBank()
	sk := sigilkeeper.NewKeeper(encCfg.Codec, runtime.NewKVStoreService(key), authority)
	k := NewKeeper(encCfg.Codec, runtime.NewKVStoreService(key), bank, mockDistr{bank: bank}, mockAccount{}, sk, authority)
	// veilKeeper never set: any settlement path must panic clearly.
	require.PanicsWithValue(t,
		"x/session: VeilKeeper is nil — app wiring must call SetVeilKeeper before any block",
		func() { _ = k.veil() })
}
