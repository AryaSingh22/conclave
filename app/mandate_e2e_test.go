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

// TestMandateE2ESpec13Steps2And3 is the SPEC §1.3 steps 2–3 acceptance
// scenario, folded into ONE full-consensus flow at task 0.7 (resolving
// D-024 — x/veil's msg server is routed, so the §1.3 intent tx transits
// real FinalizeBlock+Commit delivery end to end):
//
//	step 2: the operator (sigil controller) issues a Mandate to an ephemeral
//	        session key — max 500 USDC-equivalent/day, allowed msgs =
//	        [MsgSubmitIntent], expiry +7 days;
//	step 3: the session key submits an intent exceeding the ceiling → the tx
//	        is REJECTED IN THE ANTE HANDLER with ErrMandateExceeded,
//	        consuming no gas beyond ante;
//
// then drives the §5.2 lifecycle through real blocks: the under-ceiling
// intent lands (bond escrowed, window charged optimistically), ordering
// fixes at the session boundary EndBlock, the Reveal opens it the next
// BeginBlock (order-then-reveal through consensus), expiry refunds bond
// AND window — proven by a follow-up intent that bounced against the
// consumed window landing once the capacity is restored — and a cancel
// through a real block refunds the same way.
func TestMandateE2ESpec13Steps2And3(t *testing.T) {
	const chainID = "conclave-dev-1"
	const dayBlocks = 86_400 // dev cadence: 1s blocks (SPEC §12)

	conclaveApp := app.NewConclaveApp(
		log.NewNopLogger(), dbm.NewMemDB(), nil, true, sims.EmptyAppOptions{},
		baseapp.SetChainID(chainID),
	)

	operatorPriv := secp256k1.GenPrivKey()
	sessionPriv := secp256k1.GenPrivKey()
	operator := sdk.AccAddress(operatorPriv.PubKey().Address())
	session := sdk.AccAddress(sessionPriv.PubKey().Address())

	funds := sdk.NewCoins(
		sdk.NewCoin(app.BaseDenom, math.NewInt(10_000_000_000)),
		sdk.NewCoin(app.MockUSDCDenom, math.NewInt(10_000_000_000)),
	)
	genAccs := []authtypes.GenesisAccount{
		authtypes.NewBaseAccount(operator, operatorPriv.PubKey(), 0, 0),
		authtypes.NewBaseAccount(session, sessionPriv.PubKey(), 0, 0),
	}
	balances := []banktypes.Balance{
		{Address: operator.String(), Coins: funds},
		{Address: session.String(), Coins: funds},
	}

	valSet, err := sims.CreateRandomValidatorSet()
	require.NoError(t, err)
	genesisState, err := sims.GenesisStateWithValSet(
		conclaveApp.AppCodec(), conclaveApp.DefaultGenesis(), valSet, genAccs, balances...,
	)
	require.NoError(t, err)
	stateBytes, err := json.MarshalIndent(genesisState, "", " ")
	require.NoError(t, err)

	_, err = conclaveApp.InitChain(&abci.RequestInitChain{
		ChainId:         chainID,
		Validators:      []abci.ValidatorUpdate{},
		ConsensusParams: sims.DefaultConsensusParams,
		AppStateBytes:   stateBytes,
	})
	require.NoError(t, err)

	// commit the genesis block so query contexts see the genesis accounts
	_, err = conclaveApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: conclaveApp.LastBlockHeight() + 1,
		Hash:   conclaveApp.LastCommitID().Hash,
	})
	require.NoError(t, err)
	_, err = conclaveApp.Commit()
	require.NoError(t, err)

	signTx := func(priv cryptotypes.PrivKey, msgs ...sdk.Msg) []byte {
		t.Helper()
		ctx := conclaveApp.NewContext(true)
		addr := sdk.AccAddress(priv.PubKey().Address())
		acc := conclaveApp.AccountKeeper.GetAccount(ctx, addr)
		require.NotNil(t, acc)

		txn, err := sims.GenSignedMockTx(
			rand.New(rand.NewSource(42)), conclaveApp.TxConfig(), msgs,
			sdk.Coins{}, sims.DefaultGenTxGas, chainID,
			[]uint64{acc.GetAccountNumber()}, []uint64{acc.GetSequence()}, priv,
		)
		require.NoError(t, err)
		bz, err := conclaveApp.TxConfig().TxEncoder()(txn)
		require.NoError(t, err)
		return bz
	}

	deliver := func(priv cryptotypes.PrivKey, msgs ...sdk.Msg) *abci.ExecTxResult {
		t.Helper()
		bz := signTx(priv, msgs...)
		res, err := conclaveApp.FinalizeBlock(&abci.RequestFinalizeBlock{
			Height: conclaveApp.LastBlockHeight() + 1,
			Hash:   conclaveApp.LastCommitID().Hash,
			Txs:    [][]byte{bz},
		})
		require.NoError(t, err)
		_, err = conclaveApp.Commit()
		require.NoError(t, err)
		require.Len(t, res.TxResults, 1)
		return res.TxResults[0]
	}

	// advance commits one empty block (BeginBlock + EndBlock still run, so
	// the veil boundary/Reveal/expiry sweeps execute in real consensus flow)
	advance := func() {
		t.Helper()
		_, err := conclaveApp.FinalizeBlock(&abci.RequestFinalizeBlock{
			Height: conclaveApp.LastBlockHeight() + 1,
			Hash:   conclaveApp.LastCommitID().Hash,
		})
		require.NoError(t, err)
		_, err = conclaveApp.Commit()
		require.NoError(t, err)
	}

	usdcCoins := func(amt int64) sdk.Coins {
		return sdk.NewCoins(sdk.NewCoin(app.MockUSDCDenom, math.NewInt(amt)))
	}
	outstandingCommitment := func() sdk.Coins {
		t.Helper()
		ctx := conclaveApp.NewContext(true)
		m, found, err := conclaveApp.MandateKeeper.ActiveMandateByGrantee(ctx, 0, session)
		require.NoError(t, err)
		require.True(t, found)
		return m.OutstandingCommitment
	}
	veilEscrow := func() math.Int {
		ctx := conclaveApp.NewContext(true)
		return conclaveApp.BankKeeper.GetBalance(ctx,
			authtypes.NewModuleAddress(veiltypes.ModuleName), app.BaseDenom).Amount
	}
	intentState := func(id string) veiltypes.IntentState {
		t.Helper()
		ctx := conclaveApp.NewContext(true)
		rec, err := conclaveApp.VeilKeeper.GetIntentRecord(ctx, 0, id)
		require.NoError(t, err)
		return rec.State
	}
	onlySubmittedID := func() string {
		t.Helper()
		ctx := conclaveApp.NewContext(true)
		var id string
		require.NoError(t, conclaveApp.VeilKeeper.IterateIntentRecords(ctx, func(rec veiltypes.IntentRecord) bool {
			if rec.State == veiltypes.INTENT_STATE_SUBMITTED {
				id = rec.Intent.Id
			}
			return false
		}))
		require.NotEmpty(t, id)
		return id
	}

	// ---- §1.3 step 1 (prerequisite): register alice's sigil ----
	didHash := sha256.Sum256([]byte("did:conclave:agent-alice"))
	res := deliver(operatorPriv, &sigiltypes.MsgRegisterSigil{
		Controller:  operator.String(),
		DidDocHash:  didHash[:],
		MetadataUri: "ipfs://agent-alice",
	})
	require.Equal(t, uint32(0), res.Code, "register sigil: %s", res.Log)

	ctx := conclaveApp.NewContext(true)
	sigilID, err := conclaveApp.SigilKeeper.SigilIDByAddress(ctx, 0, operator)
	require.NoError(t, err)

	// ---- §1.3 step 2: issue the Mandate to the session key ----
	// the ceilings budget 500 USDC plus 10 CLV for intent bonds — bonds
	// count toward ceilings (D-025) and unlisted denoms are denied (D-019)
	bondBudget := sdk.NewCoin(app.BaseDenom, math.NewInt(10_000_000)) // 10 CLV
	res = deliver(operatorPriv, &mandatetypes.MsgIssueMandate{
		Controller: operator.String(),
		Terms: mandatetypes.MandateTerms{
			SigilId:            sigilID,
			GranteeAddr:        session.String(),
			AllowedMsgTypes:    []string{sdk.MsgTypeURL(&veiltypes.MsgSubmitIntent{}), sdk.MsgTypeURL(&veiltypes.MsgCancelIntent{})},
			SpendCeilingPerTx:  usdcCoins(500_000_000).Add(bondBudget), // 500 USDC
			SpendCeilingWindow: usdcCoins(500_000_000).Add(bondBudget), // 500 USDC/day
			WindowBlocks:       dayBlocks,
			FeeDenoms:          []string{app.MockUSDCDenom},
			ExpiryHeight:       uint64(conclaveApp.LastBlockHeight()) + 7*dayBlocks,
		},
	})
	require.Equal(t, uint32(0), res.Code, "issue mandate: %s", res.Log)

	bond := sdk.NewCoin(app.BaseDenom, math.NewInt(1_000_000)) // 1 CLV
	submitIntent := func(usdcAmount int64, ttl uint64) *veiltypes.MsgSubmitIntent {
		t.Helper()
		coin := sdk.NewCoin(app.MockUSDCDenom, math.NewInt(usdcAmount))
		payload := veiltypes.IntentPayload{
			Give: []veiltypes.IntentLeg{{Asset: &veiltypes.IntentLeg_Coin{Coin: &coin}}},
		}
		payloadBz, err := payload.Marshal()
		require.NoError(t, err)
		envBz, err := (&veiltypes.IntentEnvelope{TtlHeight: ttl, FeeBid: "1"}).Marshal()
		require.NoError(t, err)
		return &veiltypes.MsgSubmitIntent{
			Sender:      session.String(),
			SenderSigil: sigilID,
			Ciphertext:  payloadBz,
			Envelope:    envBz,
			Bond:        bond,
		}
	}

	// ---- §1.3 step 3: over-ceiling intent through a REAL block ----
	// 600 > 500 USDC → ErrMandateExceeded in the ante handler during
	// FinalizeBlock; no gas beyond ante; nothing stored, escrowed, charged
	res = deliver(sessionPriv, submitIntent(600_000_000, uint64(conclaveApp.LastBlockHeight())+100))
	require.Equal(t, mandatetypes.ErrMandateExceeded.ABCICode(), res.Code,
		"§1.3 step 3: over-ceiling intent must be rejected with ErrMandateExceeded: %s", res.Log)
	require.Equal(t, mandatetypes.ModuleName, res.Codespace)
	require.Less(t, res.GasUsed, int64(100_000), "no gas beyond ante")
	require.True(t, outstandingCommitment().IsZero(), "a rejected intent charges nothing")
	require.True(t, veilEscrow().IsZero(), "a rejected intent escrows nothing")

	// ---- under-ceiling intent lands; bond escrowed; window charged ----
	// the submit height is the next block; derive the boundary (cadence 3,
	// D-038) and a ttl that lets the §5.2 happy path play out first
	submitHeight := uint64(conclaveApp.LastBlockHeight()) + 1
	sealHeight := ((submitHeight + 2) / 3) * 3 // first boundary >= submit height
	revealHeight := sealHeight + 1
	ttl := revealHeight + 2

	res = deliver(sessionPriv, submitIntent(300_000_000, ttl))
	require.Equal(t, uint32(0), res.Code, "under-ceiling intent must land: %s", res.Log)

	// the optimistic window charge committed: give max + bond (D-025)
	require.Equal(t, usdcCoins(300_000_000).Add(bond).String(), outstandingCommitment().String(),
		"SPEC §6.2 step 5 (D-059): intent commitment charged to outstanding_commitment, bond included")
	// the bond sits in the veil module account
	require.Equal(t, bond.Amount, veilEscrow(), "bond escrowed (SPEC §6.3)")

	intentID := onlySubmittedID()
	require.Equal(t, veiltypes.INTENT_STATE_SUBMITTED, intentState(intentID))

	// ---- a second intent bounces against the consumed window ----
	res = deliver(sessionPriv, submitIntent(300_000_000, ttl+100))
	require.Equal(t, mandatetypes.ErrMandateExceeded.ABCICode(), res.Code,
		"300+300 > 500 USDC: the optimistic charge must hold the window: %s", res.Log)

	// ---- order-then-reveal through real consensus blocks ----
	for uint64(conclaveApp.LastBlockHeight()) < sealHeight {
		advance()
	}
	require.Equal(t, veiltypes.INTENT_STATE_SEALED_IN_SESSION, intentState(intentID),
		"ordering fixed at the boundary EndBlock (height %d)", sealHeight)

	advance() // revealHeight: BeginBlock runs the Reveal
	require.Equal(t, veiltypes.INTENT_STATE_OPEN, intentState(intentID),
		"the Reveal opens the intent strictly the NEXT block (height %d)", revealHeight)

	// ---- expiry refunds bond AND window through real blocks ----
	for uint64(conclaveApp.LastBlockHeight()) < ttl {
		advance()
	}
	require.Equal(t, veiltypes.INTENT_STATE_EXPIRED, intentState(intentID),
		"ttl %d reached → EXPIRED (SPEC §5.2)", ttl)
	require.True(t, veilEscrow().IsZero(), "bond refunded on expiry")
	require.True(t, outstandingCommitment().IsZero(),
		"SPEC §6.2 step 5 / D-059: EXPIRED decommits the stored charged_spend from outstanding_commitment")

	// ---- the refund is real capacity: the bounced intent now lands ----
	resubmitTTL := uint64(conclaveApp.LastBlockHeight()) + 100
	res = deliver(sessionPriv, submitIntent(300_000_000, resubmitTTL))
	require.Equal(t, uint32(0), res.Code,
		"window capacity demonstrably restored after expiry: %s", res.Log)
	require.Equal(t, usdcCoins(300_000_000).Add(bond).String(), outstandingCommitment().String())

	// ---- MsgCancelIntent through a real block refunds the same way ----
	secondID := onlySubmittedID()
	res = deliver(sessionPriv, &veiltypes.MsgCancelIntent{
		Sender:   session.String(),
		IntentId: secondID,
	})
	require.Equal(t, uint32(0), res.Code, "cancel intent: %s", res.Log)
	require.Equal(t, veiltypes.INTENT_STATE_CANCELLED, intentState(secondID))
	require.True(t, veilEscrow().IsZero(), "bond refunded on cancel")
	require.True(t, outstandingCommitment().IsZero(), "commitment released on cancel (D-059)")
}
