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
	storetypes "cosmossdk.io/store/types"

	abci "github.com/cometbft/cometbft/abci/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/AryaSingh22/conclave/app"
	mandatetypes "github.com/AryaSingh22/conclave/x/mandate/types"
	sigiltypes "github.com/AryaSingh22/conclave/x/sigil/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// TestMandateE2ESpec13Steps2And3 is the SPEC §1.3 steps 2–3 acceptance
// scenario for task 0.4, against the real app:
//
//	step 2: the operator (sigil controller) issues a Mandate to an ephemeral
//	        session key — max 500 USDC-equivalent/day, allowed msgs =
//	        [MsgSubmitIntent], expiry +7 days;
//	step 3: the session key submits an intent exceeding the ceiling → the tx
//	        is REJECTED IN THE ANTE HANDLER with ErrMandateExceeded,
//	        consuming no gas beyond ante.
//
// Two phases, because baseapp's runTx rejects any msg without a registered
// handler BEFORE the ante handler runs, and x/veil's msg server lands at
// task 0.7 (D-024):
//
//	phase 1 — the exact §1.3 msg flow (MsgSubmitIntent) through the
//	          production ante pipeline (app.NewAnteHandler over the app's
//	          keepers) on real signed tx bytes;
//	phase 2 — the identical enforcement through full FinalizeBlock+Commit
//	          consensus delivery, using a routable msg (MsgSend) under the
//	          same ceilings.
//
// CONCLAVE-TODO(phase-0): at task 0.7 fold phase 1 into phase 2 — the §1.3
// intent tx then transits a real block, and scripts/demo_e2e.sh exercises it
// against a live localnet at task 0.10.
func TestMandateE2ESpec13Steps2And3(t *testing.T) {
	const chainID = "conclave-dev-1"
	const dayBlocks = 86_400 // dev cadence: 1s blocks (SPEC §12)

	conclaveApp := app.NewConclaveApp(
		log.NewNopLogger(), dbm.NewMemDB(), nil, true, sims.EmptyAppOptions{},
		baseapp.SetChainID(chainID),
	)

	// x/veil is wired at task 0.7; registering the msg type lets txs carry
	// it so the MandateDecorator (which needs no veil keeper) can act on it
	conclaveApp.InterfaceRegistry().RegisterImplementations((*sdk.Msg)(nil),
		&veiltypes.MsgSubmitIntent{})

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

	usdcCoins := func(amt int64) sdk.Coins {
		return sdk.NewCoins(sdk.NewCoin(app.MockUSDCDenom, math.NewInt(amt)))
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
	issueTerms := func(allowed ...string) mandatetypes.MandateTerms {
		return mandatetypes.MandateTerms{
			SigilId:            sigilID,
			GranteeAddr:        session.String(),
			AllowedMsgTypes:    allowed,
			SpendCeilingPerTx:  usdcCoins(500_000_000).Add(bondBudget), // 500 USDC
			SpendCeilingWindow: usdcCoins(500_000_000).Add(bondBudget), // 500 USDC/day
			WindowBlocks:       dayBlocks,
			FeeDenoms:          []string{app.MockUSDCDenom},
			ExpiryHeight:       uint64(conclaveApp.LastBlockHeight()) + 7*dayBlocks,
		}
	}
	res = deliver(operatorPriv, &mandatetypes.MsgIssueMandate{
		Controller: operator.String(),
		Terms:      issueTerms(sdk.MsgTypeURL(&veiltypes.MsgSubmitIntent{})),
	})
	require.Equal(t, uint32(0), res.Code, "issue mandate: %s", res.Log)

	ctx = conclaveApp.NewContext(true)
	_, found, err := conclaveApp.MandateKeeper.ActiveMandateByGrantee(ctx, 0, session)
	require.NoError(t, err)
	require.True(t, found, "the session key must hold the issued mandate")

	// ---- phase 1: §1.3 step 3 through the production ante pipeline ----

	anteHandler, err := app.NewAnteHandler(app.HandlerOptions{
		HandlerOptions: ante.HandlerOptions{
			AccountKeeper:   conclaveApp.AccountKeeper,
			BankKeeper:      conclaveApp.BankKeeper,
			SignModeHandler: conclaveApp.TxConfig().SignModeHandler(),
			FeegrantKeeper:  conclaveApp.FeeGrantKeeper,
			SigGasConsumer:  ante.DefaultSigVerificationGasConsumer,
		},
		IBCKeeper:        conclaveApp.IBCKeeper,
		MandateKeeper:    conclaveApp.MandateKeeper,
		PriceSource:      mandatetypes.FaceValuePriceSource{},
		UnorderedManager: conclaveApp.UnorderedManager,
	})
	require.NoError(t, err)

	submitIntent := func(usdcAmount int64) *veiltypes.MsgSubmitIntent {
		coin := sdk.NewCoin(app.MockUSDCDenom, math.NewInt(usdcAmount))
		payload := veiltypes.IntentPayload{
			Give: []veiltypes.IntentLeg{{Asset: &veiltypes.IntentLeg_Coin{Coin: &coin}}},
		}
		bz, err := payload.Marshal()
		require.NoError(t, err)
		return &veiltypes.MsgSubmitIntent{
			Sender:      session.String(),
			SenderSigil: sigilID,
			Ciphertext:  bz,
			Bond:        sdk.NewCoin(app.BaseDenom, math.NewInt(1_000_000)),
		}
	}

	runAnte := func(txBytes []byte) (sdk.Context, error) {
		t.Helper()
		decoded, err := conclaveApp.TxConfig().TxDecoder()(txBytes)
		require.NoError(t, err)
		anteCtx := conclaveApp.NewContext(true).
			WithChainID(chainID).
			WithBlockHeight(conclaveApp.LastBlockHeight() + 1).
			WithGasMeter(storetypes.NewGasMeter(sims.DefaultGenTxGas))
		return anteHandler(anteCtx, decoded, false)
	}

	// over-ceiling intent (600 > 500 USDC) → ErrMandateExceeded in ante
	outCtx, err := runAnte(signTx(sessionPriv, submitIntent(600_000_000)))
	require.ErrorIs(t, err, mandatetypes.ErrMandateExceeded,
		"§1.3 step 3: over-ceiling intent must be rejected in the ante handler")
	// "no gas beyond ante": the rejection cost only ante overhead
	require.Less(t, outCtx.GasMeter().GasConsumed(), storetypes.Gas(100_000),
		"gas must not exceed ante overhead")

	// under-ceiling intent passes the same pipeline and charges the window
	// optimistically — the give leg AND the 1-CLV bond (D-025); the charge
	// is visible through the ante ctx's branch
	outCtx, err = runAnte(signTx(sessionPriv, submitIntent(100_000_000)))
	require.NoError(t, err, "under-ceiling intent must clear the ante pipeline")
	m, found, err := conclaveApp.MandateKeeper.ActiveMandateByGrantee(outCtx, 0, session)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t,
		usdcCoins(100_000_000).Add(sdk.NewCoin(app.BaseDenom, math.NewInt(1_000_000))).String(),
		m.WindowSpent.String(),
		"SPEC §6.2 step 5: the window is charged optimistically in ante, bond included")

	// ---- phase 2: the same enforcement through full block delivery ----

	// reissue with MsgSend allowed (REPLACE semantics, D2): MsgSend has a
	// live route today, so the whole consensus path can be exercised
	res = deliver(operatorPriv, &mandatetypes.MsgIssueMandate{
		Controller: operator.String(),
		Terms:      issueTerms(sdk.MsgTypeURL(&banktypes.MsgSend{})),
	})
	require.Equal(t, uint32(0), res.Code, "reissue mandate: %s", res.Log)

	// over-ceiling send → rejected in ante during FinalizeBlock
	res = deliver(sessionPriv, &banktypes.MsgSend{
		FromAddress: session.String(),
		ToAddress:   operator.String(),
		Amount:      usdcCoins(600_000_000),
	})
	require.Equal(t, mandatetypes.ErrMandateExceeded.ABCICode(), res.Code,
		"over-ceiling send must be rejected with ErrMandateExceeded: %s", res.Log)
	require.Equal(t, mandatetypes.ModuleName, res.Codespace)
	require.Less(t, res.GasUsed, int64(100_000), "no gas beyond ante")

	// the rejected tx charged nothing
	ctx = conclaveApp.NewContext(true)
	m, found, err = conclaveApp.MandateKeeper.ActiveMandateByGrantee(ctx, 0, session)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, m.WindowSpent.IsZero())

	// under-ceiling send → executes, and the optimistic charge is committed
	res = deliver(sessionPriv, &banktypes.MsgSend{
		FromAddress: session.String(),
		ToAddress:   operator.String(),
		Amount:      usdcCoins(100_000_000),
	})
	require.Equal(t, uint32(0), res.Code, "under-ceiling send: %s", res.Log)

	ctx = conclaveApp.NewContext(true)
	m, found, err = conclaveApp.MandateKeeper.ActiveMandateByGrantee(ctx, 0, session)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, usdcCoins(100_000_000).String(), m.WindowSpent.String(),
		"the optimistic window charge persists in committed state")
}
