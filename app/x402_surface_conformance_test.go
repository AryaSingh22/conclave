package app_test

import (
	"crypto/sha256"
	"encoding/json"
	"math/rand"
	"testing"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"
	"cosmossdk.io/math"

	abci "github.com/cometbft/cometbft/abci/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/AryaSingh22/conclave/app"
	mandatetypes "github.com/AryaSingh22/conclave/x/mandate/types"
	sigiltypes "github.com/AryaSingh22/conclave/x/sigil/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// x402Fixture is the gateway-shaped harness behind the docs/edge/
// X402_SURFACE.md conformance suite (task 2.3, D-083): a principal operator
// (sigil controller), a second unrelated operator, a mandate-bound gateway
// hot key, and an unbound rogue key, driven through real
// FinalizeBlock+Commit delivery exactly like the SPEC §1.3 e2e.
type x402Fixture struct {
	t   *testing.T
	app *app.ConclaveApp

	principalOpPriv, otherOpPriv, gatewayPriv, roguePriv cryptotypes.PrivKey
	principalOp, otherOp, gateway, rogue                 sdk.AccAddress
}

const x402ChainID = "conclave-dev-1"

func newX402Fixture(t *testing.T) *x402Fixture {
	t.Helper()
	f := &x402Fixture{t: t}
	f.app = app.NewConclaveApp(
		log.NewNopLogger(), dbm.NewMemDB(), nil, true, sims.EmptyAppOptions{},
		baseapp.SetChainID(x402ChainID),
	)

	f.principalOpPriv = secp256k1.GenPrivKey()
	f.otherOpPriv = secp256k1.GenPrivKey()
	f.gatewayPriv = secp256k1.GenPrivKey()
	f.roguePriv = secp256k1.GenPrivKey()
	f.principalOp = sdk.AccAddress(f.principalOpPriv.PubKey().Address())
	f.otherOp = sdk.AccAddress(f.otherOpPriv.PubKey().Address())
	f.gateway = sdk.AccAddress(f.gatewayPriv.PubKey().Address())
	f.rogue = sdk.AccAddress(f.roguePriv.PubKey().Address())

	funds := sdk.NewCoins(
		sdk.NewCoin(app.BaseDenom, math.NewInt(10_000_000_000)),
		sdk.NewCoin(app.MockUSDCDenom, math.NewInt(10_000_000_000)),
	)
	genAccs := []authtypes.GenesisAccount{
		authtypes.NewBaseAccount(f.principalOp, f.principalOpPriv.PubKey(), 0, 0),
		authtypes.NewBaseAccount(f.otherOp, f.otherOpPriv.PubKey(), 0, 0),
		authtypes.NewBaseAccount(f.gateway, f.gatewayPriv.PubKey(), 0, 0),
		authtypes.NewBaseAccount(f.rogue, f.roguePriv.PubKey(), 0, 0),
	}
	balances := []banktypes.Balance{
		{Address: f.principalOp.String(), Coins: funds},
		{Address: f.otherOp.String(), Coins: funds},
		{Address: f.gateway.String(), Coins: funds},
		{Address: f.rogue.String(), Coins: funds},
		// the §6.2 fee-abstraction CLV float: the x/mandate module account is
		// the genesis-funded pool that pays the fee collector the converted
		// native amount (SPEC §6.2 "param: fee_pool")
		{Address: authtypes.NewModuleAddress(mandatetypes.ModuleName).String(),
			Coins: sdk.NewCoins(sdk.NewCoin(app.BaseDenom, math.NewInt(100_000_000)))},
	}

	valSet, err := sims.CreateRandomValidatorSet()
	require.NoError(t, err)
	genesisState, err := sims.GenesisStateWithValSet(
		f.app.AppCodec(), f.app.DefaultGenesis(), valSet, genAccs, balances...,
	)
	require.NoError(t, err)
	stateBytes, err := json.MarshalIndent(genesisState, "", " ")
	require.NoError(t, err)

	_, err = f.app.InitChain(&abci.RequestInitChain{
		ChainId:         x402ChainID,
		Validators:      []abci.ValidatorUpdate{},
		ConsensusParams: sims.DefaultConsensusParams,
		AppStateBytes:   stateBytes,
	})
	require.NoError(t, err)

	_, err = f.app.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: f.app.LastBlockHeight() + 1,
		Hash:   f.app.LastCommitID().Hash,
	})
	require.NoError(t, err)
	_, err = f.app.Commit()
	require.NoError(t, err)
	return f
}

// deliver signs msgs with priv (fee in `fee`, which is what the
// FeeAbstractionDecorator sees) and executes them through a real block.
func (f *x402Fixture) deliver(priv cryptotypes.PrivKey, fee sdk.Coins, msgs ...sdk.Msg) *abci.ExecTxResult {
	f.t.Helper()
	ctx := f.app.NewContext(true)
	addr := sdk.AccAddress(priv.PubKey().Address())
	acc := f.app.AccountKeeper.GetAccount(ctx, addr)
	require.NotNil(f.t, acc)

	txn, err := sims.GenSignedMockTx(
		rand.New(rand.NewSource(42)), f.app.TxConfig(), msgs,
		fee, sims.DefaultGenTxGas, x402ChainID,
		[]uint64{acc.GetAccountNumber()}, []uint64{acc.GetSequence()}, priv,
	)
	require.NoError(f.t, err)
	bz, err := f.app.TxConfig().TxEncoder()(txn)
	require.NoError(f.t, err)

	res, err := f.app.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: f.app.LastBlockHeight() + 1,
		Hash:   f.app.LastCommitID().Hash,
		Txs:    [][]byte{bz},
	})
	require.NoError(f.t, err)
	_, err = f.app.Commit()
	require.NoError(f.t, err)
	require.Len(f.t, res.TxResults, 1)
	return res.TxResults[0]
}

func (f *x402Fixture) registerSigil(priv cryptotypes.PrivKey, did string) string {
	f.t.Helper()
	addr := sdk.AccAddress(priv.PubKey().Address())
	didHash := sha256.Sum256([]byte(did))
	res := f.deliver(priv, sdk.Coins{}, &sigiltypes.MsgRegisterSigil{
		Controller:  addr.String(),
		DidDocHash:  didHash[:],
		MetadataUri: "ipfs://" + did,
	})
	require.Equal(f.t, uint32(0), res.Code, "register sigil %s: %s", did, res.Log)
	ctx := f.app.NewContext(true)
	id, err := f.app.SigilKeeper.SigilIDByAddress(ctx, 0, addr)
	require.NoError(f.t, err)
	return id
}

// gatewayTerms is the X402_SURFACE.md §3.3 recommended mandate shape: the
// submit/cancel/send allowlist, uusdc ceilings plus a uclv bond budget
// (bonds count toward ceilings, D-025), and uusdc as a permitted fee denom
// (§4 fee abstraction — the "front gas without holding CLV" contract claim).
func (f *x402Fixture) gatewayTerms(sigilID string) mandatetypes.MandateTerms {
	bondBudget := sdk.NewCoins(sdk.NewCoin(app.BaseDenom, math.NewInt(10_000_000)))
	ceiling := sdk.NewCoins(sdk.NewCoin(app.MockUSDCDenom, math.NewInt(500_000_000))).Add(bondBudget...)
	return mandatetypes.MandateTerms{
		SigilId:     sigilID,
		GranteeAddr: f.gateway.String(),
		AllowedMsgTypes: []string{
			sdk.MsgTypeURL(&veiltypes.MsgSubmitIntent{}),
			sdk.MsgTypeURL(&veiltypes.MsgCancelIntent{}),
			sdk.MsgTypeURL(&banktypes.MsgSend{}),
		},
		SpendCeilingPerTx:  ceiling,
		SpendCeilingWindow: ceiling,
		WindowBlocks:       86_400,
		FeeDenoms:          []string{app.MockUSDCDenom},
		ExpiryHeight:       uint64(f.app.LastBlockHeight()) + 7*86_400,
	}
}

// submitMsg builds a MsgSubmitIntent claiming senderSigil, declaring the
// given uusdc spend_commitment (0 = the D-065 empty/zero declaration) with
// the standard 1-CLV min bond.
func (f *x402Fixture) submitMsg(sender sdk.AccAddress, senderSigil string, usdcCommitment int64) *veiltypes.MsgSubmitIntent {
	f.t.Helper()
	commitment := sdk.NewCoins()
	var give []veiltypes.IntentLeg
	if usdcCommitment > 0 {
		coin := sdk.NewCoin(app.MockUSDCDenom, math.NewInt(usdcCommitment))
		commitment = sdk.NewCoins(coin)
		give = []veiltypes.IntentLeg{{Asset: &veiltypes.IntentLeg_Coin{Coin: &coin}}}
	}
	payloadBz, err := (&veiltypes.IntentPayload{Give: give}).Marshal()
	require.NoError(f.t, err)
	envBz, err := (&veiltypes.IntentEnvelope{
		TtlHeight:       uint64(f.app.LastBlockHeight()) + 1_000,
		FeeBid:          "1",
		SpendCommitment: commitment,
	}).Marshal()
	require.NoError(f.t, err)
	return &veiltypes.MsgSubmitIntent{
		Sender:      sender.String(),
		SenderSigil: senderSigil,
		Ciphertext:  payloadBz,
		Envelope:    envBz,
		Bond:        sdk.NewCoin(app.BaseDenom, math.NewInt(1_000_000)),
	}
}

func (f *x402Fixture) balance(addr sdk.AccAddress, denom string) math.Int {
	ctx := f.app.NewContext(true)
	return f.app.BankKeeper.GetBalance(ctx, addr, denom).Amount
}

func (f *x402Fixture) outstanding(grantee sdk.AccAddress) sdk.Coins {
	f.t.Helper()
	ctx := f.app.NewContext(true)
	m, found, err := f.app.MandateKeeper.ActiveMandateByGrantee(ctx, 0, grantee)
	require.NoError(f.t, err)
	require.True(f.t, found)
	return m.OutstandingCommitment
}

func (f *x402Fixture) onlySubmittedID() string {
	f.t.Helper()
	ctx := f.app.NewContext(true)
	var id string
	require.NoError(f.t, f.app.VeilKeeper.IterateIntentRecords(ctx, func(rec veiltypes.IntentRecord) bool {
		if rec.State == veiltypes.INTENT_STATE_SUBMITTED {
			id = rec.Intent.Id
		}
		return false
	}))
	require.NotEmpty(f.t, id)
	return id
}

// TestX402SurfaceConformance is the load-bearing auth proof behind
// docs/edge/X402_SURFACE.md §7 (task 2.3, D-083): from a gateway-shaped
// fixture it proves the contract's central claims — submission authority is
// "hold a key + hold a mandate" and nothing else; the node exposes no path
// to submit as a sigil the signing key is not bound to; the mandate
// allowlist/ceilings bind the gateway; and gas fronts in uusdc through the
// §6.2 fee abstraction. The edge-service version of the D-075/D-076
// consent-gate discipline.
func TestX402SurfaceConformance(t *testing.T) {
	f := newX402Fixture(t)

	sigilP := f.registerSigil(f.principalOpPriv, "did:conclave:principal")
	sigilQ := f.registerSigil(f.otherOpPriv, "did:conclave:other")

	// ---- bind the gateway hot key to the principal's sigil ----
	res := f.deliver(f.principalOpPriv, sdk.Coins{}, &mandatetypes.MsgIssueMandate{
		Controller: f.principalOp.String(),
		Terms:      f.gatewayTerms(sigilP),
	})
	require.Equal(t, uint32(0), res.Code, "issue gateway mandate: %s", res.Log)

	// ---- POSITIVE: submit for the bound sigil, gas fronted in uusdc ----
	// (X402_SURFACE.md §3.1 + §4): the tx fee is a single uusdc coin; the
	// FeeAbstractionDecorator moves it into the pool and the pool pays the
	// fee collector the converted uclv (dev rate 1:1) — the gateway never
	// spends uclv on gas, only the intent bond.
	fee := sdk.NewCoins(sdk.NewCoin(app.MockUSDCDenom, math.NewInt(5_000)))
	gwUSDC := f.balance(f.gateway, app.MockUSDCDenom)
	poolAddr := authtypes.NewModuleAddress(mandatetypes.ModuleName)
	poolUSDC := f.balance(poolAddr, app.MockUSDCDenom)
	collectorCLV := f.balance(authtypes.NewModuleAddress(authtypes.FeeCollectorName), app.BaseDenom)

	res = f.deliver(f.gatewayPriv, fee, f.submitMsg(f.gateway, sigilP, 300_000_000))
	require.Equal(t, uint32(0), res.Code, "gateway submit for bound sigil: %s", res.Log)

	require.Equal(t, gwUSDC.SubRaw(5_000), f.balance(f.gateway, app.MockUSDCDenom),
		"§4: the uusdc fee left the gateway hot wallet")
	require.Equal(t, poolUSDC.AddRaw(5_000), f.balance(poolAddr, app.MockUSDCDenom),
		"§4: the uusdc fee landed in the fee pool")
	require.Equal(t, collectorCLV.AddRaw(5_000), f.balance(authtypes.NewModuleAddress(authtypes.FeeCollectorName), app.BaseDenom),
		"§4: the pool paid the collector the converted uclv (dev rate 1 uclv/uusdc)")

	bond := sdk.NewCoin(app.BaseDenom, math.NewInt(1_000_000))
	require.Equal(t,
		sdk.NewCoins(sdk.NewCoin(app.MockUSDCDenom, math.NewInt(300_000_000))).Add(bond).String(),
		f.outstanding(f.gateway).String(),
		"§1.1 step 3: commitment+bond charged to the mandate window (D-065)")

	// cancel to release the window for the cases below (§3.3 MsgCancelIntent)
	intentID := f.onlySubmittedID()
	res = f.deliver(f.gatewayPriv, sdk.Coins{}, &veiltypes.MsgCancelIntent{
		Sender: f.gateway.String(), IntentId: intentID,
	})
	require.Equal(t, uint32(0), res.Code, "gateway cancel: %s", res.Log)
	require.True(t, f.outstanding(f.gateway).IsZero(), "cancel releases the commitment (D-059)")

	// ---- ALLOWLIST: a mandate without MsgSubmitIntent denies it in ante ----
	terms := f.gatewayTerms(sigilP)
	terms.AllowedMsgTypes = []string{sdk.MsgTypeURL(&banktypes.MsgSend{})}
	res = f.deliver(f.principalOpPriv, sdk.Coins{}, &mandatetypes.MsgIssueMandate{
		Controller: f.principalOp.String(), Terms: terms,
	})
	require.Equal(t, uint32(0), res.Code, "re-issue without submit: %s", res.Log)

	res = f.deliver(f.gatewayPriv, sdk.Coins{}, f.submitMsg(f.gateway, sigilP, 1_000_000))
	require.Equal(t, mandatetypes.ErrMsgNotPermitted.ABCICode(), res.Code,
		"§1.1 step 3: TypeURL allowlist is deny-by-default: %s", res.Log)
	require.Equal(t, mandatetypes.ModuleName, res.Codespace)
	require.True(t, f.outstanding(f.gateway).IsZero(), "an ante-denied tx charges nothing")

	// restore the full allowlist (same-sigil REPLACE, the D2 semantics)
	res = f.deliver(f.principalOpPriv, sdk.Coins{}, &mandatetypes.MsgIssueMandate{
		Controller: f.principalOp.String(), Terms: f.gatewayTerms(sigilP),
	})
	require.Equal(t, uint32(0), res.Code, "restore allowlist: %s", res.Log)

	// ---- CEILING: an over-ceiling commitment is denied in ante ----
	res = f.deliver(f.gatewayPriv, sdk.Coins{}, f.submitMsg(f.gateway, sigilP, 600_000_000))
	require.Equal(t, mandatetypes.ErrMandateExceeded.ABCICode(), res.Code,
		"§1.1 step 3: 600 > 500 USDC per-tx ceiling: %s", res.Log)
	require.True(t, f.outstanding(f.gateway).IsZero(), "an ante-denied tx charges nothing")

	// ---- UNBOUND: a key with no sigil binding cannot submit for anyone ----
	for _, sig := range []string{sigilP, sigilQ} {
		res = f.deliver(f.roguePriv, sdk.Coins{}, f.submitMsg(f.rogue, sig, 1_000_000))
		require.Equal(t, veiltypes.ErrSigilMismatch.ABCICode(), res.Code,
			"§1.1 step 2: unbound key claiming sigil %s must be rejected: %s", sig, res.Log)
		require.Equal(t, veiltypes.ModuleName, res.Codespace)
	}
	require.True(t, f.outstanding(f.gateway).IsZero(), "rogue txs never touch the gateway mandate")

	// ---- WRONG SIGIL: the bound key cannot submit as another sigil ----
	// The gateway key resolves to sigilP; claiming sigilQ is rejected at the
	// §1.1 step-2 chokepoint. With the D-085/G2 mitigation the rejection is in
	// the ANTE (SubmitIntentPrevalidationDecorator), before the mandate charge,
	// so NOTHING strands — see TestX402FailedSubmitDoesNotStrand_G2Closed.
	res = f.deliver(f.gatewayPriv, sdk.Coins{}, f.submitMsg(f.gateway, sigilQ, 0))
	require.Equal(t, veiltypes.ErrSigilMismatch.ABCICode(), res.Code,
		"§1.1 step 2: bound key claiming a foreign sigil must be rejected: %s", res.Log)
	require.Equal(t, veiltypes.ModuleName, res.Codespace)
	require.True(t, f.outstanding(f.gateway).IsZero(),
		"G2 closed: an ante-rejected submit charges nothing (no strand)")

	// ---- BINDING STABILITY (D-084): no third party can rebind the key ----
	// the other sigil's controller signs a well-formed IssueMandate naming
	// the gateway key; only the cross-sigil binding is illegitimate
	terms = f.gatewayTerms(sigilQ)
	res = f.deliver(f.otherOpPriv, sdk.Coins{}, &mandatetypes.MsgIssueMandate{
		Controller: f.otherOp.String(), Terms: terms,
	})
	require.Equal(t, mandatetypes.ErrGranteeConflict.ABCICode(), res.Code,
		"X402_SURFACE.md §1.1 binding stability: cross-sigil rebind must be rejected: %s", res.Log)
	require.Equal(t, mandatetypes.ModuleName, res.Codespace)

	// the gateway's real binding is untouched and still works end to end
	ctx := f.app.NewContext(true)
	m, found, err := f.app.MandateKeeper.ActiveMandateByGrantee(ctx, 0, f.gateway)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, sigilP, m.SigilId, "the victim binding survives the rebind attempt")
	res = f.deliver(f.gatewayPriv, sdk.Coins{}, f.submitMsg(f.gateway, sigilP, 1_000_000))
	require.Equal(t, uint32(0), res.Code, "gateway still submits for its principal: %s", res.Log)
}

// TestX402FailedSubmitDoesNotStrand_G2Closed is the load-bearing guard for
// the D-085/G2 mitigation (SubmitIntentPrevalidationDecorator). Without the
// decorator, a mandate-bound MsgSubmitIntent that passes the mandate ante
// (window charged optimistically, SPEC §6.2 step 5) but FAILS a veil
// msg-server precondition strands its charge in outstanding_commitment
// forever — baseapp commits ante writes before runMsgs (v0.50 baseapp.go),
// the failed msg cache is discarded, no intent record exists so DecommitIntent
// never fires, and a postHandler cannot refund (its writes land in the same
// discarded cache). The strand breaks the app-level
// mandate-commitment-conservation invariant, permissionlessly and on demand.
//
// With the decorator, EVERY payload-independent msg-server failure mode is
// rejected in the ante BEFORE the charge, so nothing strands. This test drives
// each enumerated failure mode through the full app (ante+msg) and asserts:
// (1) the tx is rejected, (2) outstanding_commitment stays ZERO (no strand),
// (3) the conservation invariant stays GREEN.
//
// WATCHED-RED (Rule 3, recorded in D-085): deleting the
// NewSubmitIntentPrevalidationDecorator line from app/ante.go flips every
// no-strand assertion here RED (the strand + broken invariant return), which
// is what makes the decorator provably load-bearing.
func TestX402FailedSubmitDoesNotStrand_G2Closed(t *testing.T) {
	f := newX402Fixture(t)
	sigilP := f.registerSigil(f.principalOpPriv, "did:conclave:principal")
	sigilQ := f.registerSigil(f.otherOpPriv, "did:conclave:other")

	res := f.deliver(f.principalOpPriv, sdk.Coins{}, &mandatetypes.MsgIssueMandate{
		Controller: f.principalOp.String(),
		Terms:      f.gatewayTerms(sigilP),
	})
	require.Equal(t, uint32(0), res.Code, "issue gateway mandate: %s", res.Log)

	// each case: a MsgSubmitIntent that passes the mandate ante (in-ceiling,
	// allowlisted) but would fail a veil msg-server precondition. All must be
	// rejected in the ante (the prevalidation decorator) with the mirrored veil
	// error, charging nothing.
	lowBond := f.submitMsg(f.gateway, sigilP, 100_000_000)
	lowBond.Bond = sdk.NewCoin(app.BaseDenom, math.NewInt(500_000)) // < 1 CLV min

	badEnvelope := f.submitMsg(f.gateway, sigilP, 100_000_000)
	badEnvelope.Envelope = []byte{0xFF, 0xFF, 0xFF} // undecodable

	wrongChamber := f.submitMsg(f.gateway, sigilP, 100_000_000)
	wrongChamber.ChamberId = 7

	cases := []struct {
		name    string
		msg     *veiltypes.MsgSubmitIntent
		errCode uint32
	}{
		{"foreign sigil", f.submitMsg(f.gateway, sigilQ, 100_000_000), veiltypes.ErrSigilMismatch.ABCICode()},
		{"bond below required", lowBond, veiltypes.ErrBondInsufficient.ABCICode()},
		{"malformed envelope", badEnvelope, veiltypes.ErrInvalidEnvelope.ABCICode()},
		{"wrong chamber", wrongChamber, veiltypes.ErrInvalidChamber.ABCICode()},
	}
	for _, tc := range cases {
		res = f.deliver(f.gatewayPriv, sdk.Coins{}, tc.msg)
		require.Equal(t, tc.errCode, res.Code, "%s: expected the mirrored veil rejection: %s", tc.name, res.Log)
		require.Equal(t, veiltypes.ModuleName, res.Codespace, "%s: rejected by the veil precondition", tc.name)
		require.True(t, f.outstanding(f.gateway).IsZero(),
			"%s: G2 closed — a msg-server-failing submit charges nothing in the ante", tc.name)

		ctx := f.app.NewContext(true)
		msgStr, broken := app.CommitmentConservationInvariant(f.app.MandateKeeper, f.app.VeilKeeper)(ctx)
		require.False(t, broken, "%s: conservation invariant must stay green: %s", tc.name, msgStr)
	}

	// a VALID submit still lands (the guard rejects only would-fail submits),
	// and it DOES charge — proving the mitigation didn't just disable charging.
	res = f.deliver(f.gatewayPriv, sdk.Coins{}, f.submitMsg(f.gateway, sigilP, 100_000_000))
	require.Equal(t, uint32(0), res.Code, "a valid submit still lands through the prevalidation: %s", res.Log)
	require.False(t, f.outstanding(f.gateway).IsZero(), "a valid submit still charges the window")
	// (the unfundable-bond mode — ErrBondFundsInsufficient — is proven at the
	// precondition level in x/veil/keeper: TestValidateSubmitIntentPreconditions.)
}
