package ante

import (
	"context"
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"

	errorsmod "cosmossdk.io/errors"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"github.com/AryaSingh22/conclave/x/mandate/keeper"
	"github.com/AryaSingh22/conclave/x/mandate/types"
	sigilkeeper "github.com/AryaSingh22/conclave/x/sigil/keeper"
	sigiltypes "github.com/AryaSingh22/conclave/x/sigil/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// ---- fixture -------------------------------------------------------------

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
	ctx      sdk.Context
	k        keeper.Keeper
	sigil    sigilkeeper.Keeper
	bank     *mockBank
	txConfig client.TxConfig

	controller sdk.AccAddress
	sigilID    string
	grantee    sdk.AccAddress
}

func setup(t testing.TB) *fixture {
	t.Helper()
	mandateKey := storetypes.NewKVStoreKey(types.StoreKey)
	sigilKey := storetypes.NewKVStoreKey(sigiltypes.StoreKey)
	ctx := testutil.DefaultContextWithKeys(
		map[string]*storetypes.KVStoreKey{types.StoreKey: mandateKey, sigiltypes.StoreKey: sigilKey},
		nil, nil,
	)
	encCfg := moduletestutil.MakeTestEncodingConfig()
	banktypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	authz.RegisterInterfaces(encCfg.InterfaceRegistry)
	govv1.RegisterInterfaces(encCfg.InterfaceRegistry)
	types.RegisterInterfaces(encCfg.InterfaceRegistry)
	encCfg.InterfaceRegistry.RegisterImplementations((*sdk.Msg)(nil),
		&veiltypes.MsgSubmitIntent{}, &veiltypes.MsgCancelIntent{})

	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	sk := sigilkeeper.NewKeeper(encCfg.Codec, runtime.NewKVStoreService(sigilKey), authority)
	bank := newMockBank()
	k := keeper.NewKeeper(encCfg.Codec, runtime.NewKVStoreService(mandateKey), sk, bank, mockAccount{}, authority)

	ctx = ctx.WithBlockHeight(10)
	require.NoError(t, sk.SetParams(ctx, sigiltypes.DefaultParams()))
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	controller := randAddr()
	h := sha256.Sum256([]byte("did-doc"))
	sigilID, err := sk.RegisterSigil(ctx, controller, h[:], "", nil, 0, 0)
	require.NoError(t, err)

	return &fixture{
		ctx: ctx, k: k, sigil: sk, bank: bank, txConfig: encCfg.TxConfig,
		controller: controller, sigilID: sigilID, grantee: randAddr(),
	}
}

func randAddr() sdk.AccAddress {
	return sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
}

func usdc(amt int64) sdk.Coins { return sdk.NewCoins(sdk.NewInt64Coin("uusdc", amt)) }
func uclv(amt int64) sdk.Coins { return sdk.NewCoins(sdk.NewInt64Coin("uclv", amt)) }

// issue issues a mandate to f.grantee with the given allowlist, the
// standard 500-uusdc per-tx and window ceilings, and a 100-uclv allowance
// covering intent bonds (D-025: bonds count toward ceilings, so a mandate
// permitting MsgSubmitIntent must budget the bond denom).
func (f *fixture) issue(t testing.TB, allowed []string, mutate ...func(*types.MandateTerms)) string {
	t.Helper()
	terms := types.MandateTerms{
		SigilId:            f.sigilID,
		GranteeAddr:        f.grantee.String(),
		AllowedMsgTypes:    allowed,
		SpendCeilingPerTx:  usdc(500).Add(uclv(100)...),
		SpendCeilingWindow: usdc(500).Add(uclv(100)...),
		WindowBlocks:       100,
		FeeDenoms:          []string{"uusdc"},
		ExpiryHeight:       1_000_000,
	}
	for _, m := range mutate {
		m(&terms)
	}
	id, err := f.k.IssueMandate(f.ctx, f.controller, terms)
	require.NoError(t, err)
	return id
}

func (f *fixture) buildTx(t testing.TB, fee sdk.Coins, msgs ...sdk.Msg) sdk.Tx {
	t.Helper()
	b := f.txConfig.NewTxBuilder()
	require.NoError(t, b.SetMsgs(msgs...))
	b.SetFeeAmount(fee)
	b.SetGasLimit(200_000)
	return b.GetTx()
}

var nextOK sdk.AnteHandler = func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
	return ctx, nil
}

func (f *fixture) runMandateDecorator(t testing.TB, tx sdk.Tx) error {
	t.Helper()
	d := NewMandateDecorator(f.k)
	_, err := d.AnteHandle(f.ctx, tx, false, nextOK)
	return err
}

func (f *fixture) windowSpent(t testing.TB) sdk.Coins {
	t.Helper()
	m, found, err := f.k.ActiveMandateByGrantee(f.ctx, 0, f.grantee)
	require.NoError(t, err)
	require.True(t, found)
	return m.WindowSpent
}

// outstandingCommitment returns the grantee's lifetime intent-commitment
// accumulator (D-059): MsgSubmitIntent charges land here, not in window_spent.
func (f *fixture) outstandingCommitment(t testing.TB) sdk.Coins {
	t.Helper()
	m, found, err := f.k.ActiveMandateByGrantee(f.ctx, 0, f.grantee)
	require.NoError(t, err)
	require.True(t, found)
	return m.OutstandingCommitment
}

// submitIntentMsg builds a MsgSubmitIntent from f.grantee whose payload
// gives the provided legs.
func (f *fixture) submitIntentMsg(t testing.TB, give ...sdk.Coin) *veiltypes.MsgSubmitIntent {
	t.Helper()
	payload := veiltypes.IntentPayload{}
	for i := range give {
		coin := give[i]
		payload.Give = append(payload.Give, veiltypes.IntentLeg{
			Asset: &veiltypes.IntentLeg_Coin{Coin: &coin},
		})
	}
	bz, err := payload.Marshal()
	require.NoError(t, err)
	// the ante values the intent at spend_commitment + bond and never reads
	// give legs (D-065); the test declares the honest commitment — the
	// per-denom max of the give legs, exactly what giveLegValue computes at
	// the Reveal — so the §6.2 step-4 charge is the give value + bond, as
	// before, and the Reveal would not slash it.
	commitment := sdk.NewCoins()
	for i := range give {
		commitment = commitment.Max(sdk.NewCoins(give[i]))
	}
	env := veiltypes.IntentEnvelope{SpendCommitment: commitment}
	envBz, err := env.Marshal()
	require.NoError(t, err)
	return &veiltypes.MsgSubmitIntent{
		Sender:      f.grantee.String(),
		SenderSigil: f.sigilID,
		Ciphertext:  bz,
		Envelope:    envBz,
		Bond:        sdk.NewInt64Coin("uclv", 1),
	}
}

func bankSend(from sdk.AccAddress, amt sdk.Coins) *banktypes.MsgSend {
	return &banktypes.MsgSend{
		FromAddress: from.String(),
		ToAddress:   randAddr().String(),
		Amount:      amt,
	}
}

var (
	sendURL   = sdk.MsgTypeURL(&banktypes.MsgSend{})
	execURL   = sdk.MsgTypeURL(&authz.MsgExec{})
	intentURL = sdk.MsgTypeURL(&veiltypes.MsgSubmitIntent{})
)

// wrapExec wraps msgs in a MsgExec executed by addr.
func wrapExec(t testing.TB, addr sdk.AccAddress, msgs ...sdk.Msg) *authz.MsgExec {
	t.Helper()
	exec := authz.NewMsgExec(addr, msgs)
	return &exec
}

// ---- MandateDecorator: SPEC §6.2 steps 1–6 --------------------------------

// TestRootKeyPassthrough is SPEC §6.2 step 1: a signer with no mandate index
// entry is a root key and passes regardless of msg types or amounts.
func TestRootKeyPassthrough(t *testing.T) {
	f := setup(t)
	root := randAddr()
	tx := f.buildTx(t, nil, bankSend(root, usdc(1_000_000)))
	require.NoError(t, f.runMandateDecorator(t, tx))
}

// TestExpiryAndRevocation is SPEC §6.2 step 2.
func TestExpiryAndRevocation(t *testing.T) {
	f := setup(t)
	id := f.issue(t, []string{sendURL})
	tx := f.buildTx(t, nil, bankSend(f.grantee, usdc(1)))

	// active → passes
	require.NoError(t, f.runMandateDecorator(t, tx))

	// expired → ErrMandateExpired
	expired := f.ctx
	f.ctx = f.ctx.WithBlockHeight(1_000_000)
	require.ErrorIs(t, f.runMandateDecorator(t, tx), types.ErrMandateExpired)
	f.ctx = expired

	// revoked → ErrMandateExpired, and the key does NOT regain root
	// passthrough (D-017)
	require.NoError(t, f.k.RevokeMandate(f.ctx, f.controller, id))
	require.ErrorIs(t, f.runMandateDecorator(t, tx), types.ErrMandateExpired)
}

// TestMsgAllowlist is SPEC §6.2 step 3, flat msgs.
func TestMsgAllowlist(t *testing.T) {
	f := setup(t)
	f.issue(t, []string{intentURL})

	require.NoError(t, f.runMandateDecorator(t, f.buildTx(t, nil, f.submitIntentMsg(t, sdk.NewInt64Coin("uusdc", 10)))))

	err := f.runMandateDecorator(t, f.buildTx(t, nil, bankSend(f.grantee, usdc(1))))
	require.ErrorIs(t, err, types.ErrMsgNotPermitted)

	// one disallowed msg poisons the whole tx
	err = f.runMandateDecorator(t, f.buildTx(t, nil,
		f.submitIntentMsg(t, sdk.NewInt64Coin("uusdc", 10)), bankSend(f.grantee, usdc(1))))
	require.ErrorIs(t, err, types.ErrMsgNotPermitted)
}

// TestNestedMsgUnwrap is SPEC §6.2 step 3, nested msgs: inner TypeURLs are
// checked through authz MsgExec and gov MsgSubmitProposal wrappers, and
// nested spend draws down the same window.
func TestNestedMsgUnwrap(t *testing.T) {
	f := setup(t)
	f.issue(t, []string{execURL, sendURL})

	// allowed wrapper + allowed inner: passes, inner spend charged
	tx := f.buildTx(t, nil, wrapExec(t, f.grantee, bankSend(f.grantee, usdc(120))))
	require.NoError(t, f.runMandateDecorator(t, tx))
	require.Equal(t, usdc(120), f.windowSpent(t))

	// allowed wrapper + disallowed inner: rejected
	tx = f.buildTx(t, nil, wrapExec(t, f.grantee, f.submitIntentMsg(t)))
	require.ErrorIs(t, f.runMandateDecorator(t, tx), types.ErrMsgNotPermitted)

	// gov wrapper is unwrapped too
	prop, err := govv1.NewMsgSubmitProposal(
		[]sdk.Msg{bankSend(f.grantee, usdc(1))}, nil, f.grantee.String(), "", "t", "s", false)
	require.NoError(t, err)
	tx = f.buildTx(t, nil, prop)
	require.ErrorIs(t, f.runMandateDecorator(t, tx), types.ErrMsgNotPermitted,
		"MsgSubmitProposal itself is not allowlisted")

	// gov wrapper allowed: its inner msgs are still checked
	propURL := sdk.MsgTypeURL(&govv1.MsgSubmitProposal{})
	f.issue(t, []string{propURL, sendURL}) // REPLACE semantics: fresh mandate
	inner, err := govv1.NewMsgSubmitProposal(
		[]sdk.Msg{f.submitIntentMsg(t)}, nil, f.grantee.String(), "", "t", "s", false)
	require.NoError(t, err)
	require.ErrorIs(t, f.runMandateDecorator(t, f.buildTx(t, nil, inner)), types.ErrMsgNotPermitted)
}

// TestAuthzGrantEscalationBlocked proves the D-016 attack end to end at the
// decorator: even if a controller could somehow allow MsgExec, a nested
// authz MsgGrant can never execute because MsgGrant can never be in the
// allowlist (issuance rejects it — see
// keeper.TestIssueMandatePrivilegeEscalationGuard) and the recursive unwrap
// checks it against the allowlist.
func TestAuthzGrantEscalationBlocked(t *testing.T) {
	f := setup(t)
	f.issue(t, []string{execURL, sendURL})

	grant, err := authz.NewGrant(f.ctx.BlockTime(),
		authz.NewGenericAuthorization(sendURL), nil)
	require.NoError(t, err)
	grantMsg := &authz.MsgGrant{
		Granter: f.grantee.String(),
		Grantee: randAddr().String(),
		Grant:   grant,
	}

	tx := f.buildTx(t, nil, wrapExec(t, f.grantee, grantMsg))
	require.ErrorIs(t, f.runMandateDecorator(t, tx), types.ErrMsgNotPermitted)
}

// TestNestingDepthCap is SPEC §6.2 step 3's recursion cap: 8 wrapper levels
// pass, 9 reject with ErrMsgTooNested.
func TestNestingDepthCap(t *testing.T) {
	f := setup(t)
	f.issue(t, []string{execURL, sendURL})

	wrap := func(depth int) sdk.Msg {
		var msg sdk.Msg = bankSend(f.grantee, usdc(1))
		for i := 0; i < depth; i++ {
			msg = wrapExec(t, f.grantee, msg)
		}
		return msg
	}

	require.NoError(t, f.runMandateDecorator(t, f.buildTx(t, nil, wrap(types.DefaultMaxMsgNestingDepth))))
	require.ErrorIs(t,
		f.runMandateDecorator(t, f.buildTx(t, nil, wrap(types.DefaultMaxMsgNestingDepth+1))),
		types.ErrMsgTooNested)
}

// TestPerTxCeiling is SPEC §6.2 step 4 over a single msg.
func TestPerTxCeiling(t *testing.T) {
	f := setup(t)
	f.issue(t, []string{sendURL})

	require.NoError(t, f.runMandateDecorator(t, f.buildTx(t, nil, bankSend(f.grantee, usdc(500)))))
	require.ErrorIs(t,
		f.runMandateDecorator(t, f.buildTx(t, nil, bankSend(f.grantee, usdc(501)))),
		types.ErrMandateExceeded)
}

// TestPerTxCeilingAggregatesAcrossMsgs: the spend intent is computed per TX —
// N msgs each under the ceiling whose sum exceeds it are rejected.
func TestPerTxCeilingAggregatesAcrossMsgs(t *testing.T) {
	f := setup(t)
	f.issue(t, []string{sendURL})

	tx := f.buildTx(t, nil,
		bankSend(f.grantee, usdc(200)),
		bankSend(f.grantee, usdc(200)),
		bankSend(f.grantee, usdc(200)),
	)
	require.ErrorIs(t, f.runMandateDecorator(t, tx), types.ErrMandateExceeded)
	require.True(t, f.windowSpent(t).IsZero(), "rejected txs charge nothing")
}

// TestIntentSpendValuation is SPEC §6.2 step 4 for MsgSubmitIntent: max of
// give legs per denom under face-value pricing, plus the intent bond
// (D-025).
func TestIntentSpendValuation(t *testing.T) {
	f := setup(t)
	f.issue(t, []string{intentURL})

	// max(300, 200) = 300 charged (not 500), plus the 1uclv bond
	msg := f.submitIntentMsg(t, sdk.NewInt64Coin("uusdc", 300), sdk.NewInt64Coin("uusdc", 200))
	require.NoError(t, f.runMandateDecorator(t, f.buildTx(t, nil, msg)))
	require.Equal(t, usdc(300).Add(uclv(1)...), f.outstandingCommitment(t))

	// an over-ceiling give leg is rejected — §1.3 step 3 in miniature
	msg = f.submitIntentMsg(t, sdk.NewInt64Coin("uusdc", 600))
	require.ErrorIs(t, f.runMandateDecorator(t, f.buildTx(t, nil, msg)), types.ErrMandateExceeded)

	// malformed payload contributes zero give-leg spend (D-021), but its
	// bond still charges the window (D-025: the burn is bounded)
	bad := &veiltypes.MsgSubmitIntent{
		Sender: f.grantee.String(), SenderSigil: f.sigilID,
		Ciphertext: []byte{0xde, 0xad, 0xbe, 0xef},
		Bond:       sdk.NewInt64Coin("uclv", 1),
	}
	require.NoError(t, f.runMandateDecorator(t, f.buildTx(t, nil, bad)))
	require.Equal(t, usdc(300).Add(uclv(2)...), f.outstandingCommitment(t),
		"malformed payload commits its bond and nothing else")
}

// TestBondAloneDrainsWindow is the D-025 attack in isolation: a stolen key
// spamming malformed intents burns CLV through 50% bond slashes, so bonds
// alone must exhaust a window — the third 4-uclv bond against a 10-uclv
// window ceiling bounces with ErrMandateExceeded.
func TestBondAloneDrainsWindow(t *testing.T) {
	f := setup(t)
	f.issue(t, []string{intentURL}, func(terms *types.MandateTerms) {
		terms.SpendCeilingPerTx = uclv(4)
		terms.SpendCeilingWindow = uclv(10)
	})

	malformedIntent := func() *veiltypes.MsgSubmitIntent {
		return &veiltypes.MsgSubmitIntent{
			Sender: f.grantee.String(), SenderSigil: f.sigilID,
			Ciphertext: []byte{0xde, 0xad},
			Bond:       sdk.NewInt64Coin("uclv", 4),
		}
	}

	require.NoError(t, f.runMandateDecorator(t, f.buildTx(t, nil, malformedIntent())))
	require.NoError(t, f.runMandateDecorator(t, f.buildTx(t, nil, malformedIntent())))
	require.ErrorIs(t,
		f.runMandateDecorator(t, f.buildTx(t, nil, malformedIntent())),
		types.ErrMandateExceeded,
		"over-window via bonds alone must be rejected")
	// D-059: intent bonds accumulate in outstanding_commitment, and the window
	// ceiling still bounds them via the exposure check (outstanding + settled).
	require.Equal(t, uclv(8), f.outstandingCommitment(t))
}

// TestWindowAccountingAcrossTxsAndResets is SPEC §6.2 step 5: optimistic
// accumulation within the window, reset at the boundary.
func TestWindowAccountingAcrossTxsAndResets(t *testing.T) {
	f := setup(t)
	f.issue(t, []string{sendURL}) // window_blocks = 100, start = 10

	require.NoError(t, f.runMandateDecorator(t, f.buildTx(t, nil, bankSend(f.grantee, usdc(300)))))
	require.NoError(t, f.runMandateDecorator(t, f.buildTx(t, nil, bankSend(f.grantee, usdc(200)))))
	require.Equal(t, usdc(500), f.windowSpent(t))

	// window exhausted
	require.ErrorIs(t,
		f.runMandateDecorator(t, f.buildTx(t, nil, bankSend(f.grantee, usdc(1)))),
		types.ErrMandateExceeded)

	// height 109 is the last block of the window; 110 resets
	f.ctx = f.ctx.WithBlockHeight(109)
	require.ErrorIs(t,
		f.runMandateDecorator(t, f.buildTx(t, nil, bankSend(f.grantee, usdc(1)))),
		types.ErrMandateExceeded)

	f.ctx = f.ctx.WithBlockHeight(110)
	require.NoError(t, f.runMandateDecorator(t, f.buildTx(t, nil, bankSend(f.grantee, usdc(500)))))
	require.Equal(t, usdc(500), f.windowSpent(t))
}

// TestDenialEmitsEnforcementEventInCheckTx is the SPEC §6.2 mandate_enforced
// observability event.
func TestDenialEmitsEnforcementEventInCheckTx(t *testing.T) {
	f := setup(t)
	f.issue(t, []string{sendURL})

	f.ctx = f.ctx.WithIsCheckTx(true)
	err := f.runMandateDecorator(t, f.buildTx(t, nil, bankSend(f.grantee, usdc(501))))
	require.ErrorIs(t, err, types.ErrMandateExceeded)

	var seen bool
	for _, ev := range f.ctx.EventManager().Events() {
		if ev.Type == types.EventTypeMandateEnforced {
			seen = true
		}
	}
	require.True(t, seen, "mandate_enforced must be emitted on CheckTx denial")
}

// ---- FeeAbstractionDecorator ----------------------------------------------

type countingDecorator struct{ called *int }

func (d countingDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	*d.called++
	return next(ctx, tx, simulate)
}

func TestFeeAbstractionDecorator(t *testing.T) {
	f := setup(t)
	f.issue(t, []string{intentURL})

	poolAddr := authtypes.NewModuleAddress(types.ModuleName)
	collector := authtypes.NewModuleAddress(authtypes.FeeCollectorName)
	f.bank.fund(poolAddr.String(), sdk.NewCoins(sdk.NewInt64Coin("uclv", 1_000_000)))
	f.bank.fund(f.grantee.String(), sdk.NewCoins(sdk.NewInt64Coin("uusdc", 1_000_000)))

	d := NewFeeAbstractionDecorator(f.k)

	// permitted uusdc fee: converted through the pool at the 1:1 dev rate
	tx := f.buildTx(t, usdc(700), f.submitIntentMsg(t, sdk.NewInt64Coin("uusdc", 10)))
	_, err := d.AnteHandle(f.ctx, tx, false, nextOK)
	require.NoError(t, err)
	require.Equal(t, int64(700), f.bank.GetBalance(f.ctx, poolAddr, "uusdc").Amount.Int64())
	require.Equal(t, int64(700), f.bank.GetBalance(f.ctx, collector, "uclv").Amount.Int64())
	require.Equal(t, int64(1_000_000-700), f.bank.GetBalance(f.ctx, poolAddr, "uclv").Amount.Int64())

	// unlisted foreign denom from a mandate-bound payer: rejected (D-021)
	tx = f.buildTx(t, sdk.NewCoins(sdk.NewInt64Coin("uatom", 5)), f.submitIntentMsg(t))
	_, err = d.AnteHandle(f.ctx, tx, false, nextOK)
	require.ErrorIs(t, err, types.ErrFeeDenomNotPermitted)

	// native fee from a mandate-bound payer: standard path, no conversion
	tx = f.buildTx(t, sdk.NewCoins(sdk.NewInt64Coin("uclv", 5)), f.submitIntentMsg(t))
	_, err = d.AnteHandle(f.ctx, tx, false, nextOK)
	require.NoError(t, err)

	// root key with any fee: untouched
	root := randAddr()
	tx = f.buildTx(t, sdk.NewCoins(sdk.NewInt64Coin("uatom", 5)), bankSend(root, usdc(1)))
	_, err = d.AnteHandle(f.ctx, tx, false, nextOK)
	require.NoError(t, err)
}

func TestConditionalDeductFeeDecorator(t *testing.T) {
	f := setup(t)
	f.issue(t, []string{intentURL})

	calls := 0
	d := NewConditionalDeductFeeDecorator(f.k, countingDecorator{called: &calls})

	// abstraction applies → inner DeductFee skipped
	tx := f.buildTx(t, usdc(700), f.submitIntentMsg(t))
	_, err := d.AnteHandle(f.ctx, tx, false, nextOK)
	require.NoError(t, err)
	require.Equal(t, 0, calls)

	// native fee → inner DeductFee runs
	tx = f.buildTx(t, sdk.NewCoins(sdk.NewInt64Coin("uclv", 5)), f.submitIntentMsg(t))
	_, err = d.AnteHandle(f.ctx, tx, false, nextOK)
	require.NoError(t, err)
	require.Equal(t, 1, calls)

	// root key → inner DeductFee runs
	tx = f.buildTx(t, usdc(700), bankSend(randAddr(), usdc(1)))
	_, err = d.AnteHandle(f.ctx, tx, false, nextOK)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}
