package app_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"testing"
	"time"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"
	"cosmossdk.io/math"

	abci "github.com/cometbft/cometbft/abci/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	clienttx "github.com/cosmos/cosmos-sdk/client/tx"

	"github.com/AryaSingh22/conclave/app"
	unorderedtypes "github.com/AryaSingh22/conclave/app/unordered/types"
	mandatetypes "github.com/AryaSingh22/conclave/x/mandate/types"
	sigiltypes "github.com/AryaSingh22/conclave/x/sigil/types"
)

// The SPEC §6.7 unordered-tx conformance suite — the behavioral pin for
// the sigverify fork (see the provenance pin in app/unordered/sigverify.go)
// and the §10 row 0.5 acceptance:
//
//   - the exact upstream-bug reproduction: unordered txs with deliberately
//     WRONG sequence numbers MUST succeed — 50 from one key in one block;
//   - ordered txs with wrong sequences still fail (no weakening);
//   - replay rejection: same tx twice → second rejected, same block and
//     across blocks;
//   - TTL-cap enforcement and expiry-sweep correctness, including the
//     no-reopen property;
//   - the 0.4×0.5 intersection: 50 unordered txs against a mandate window
//     → exactly the under-ceiling prefix lands.

// conformanceChain is an in-process app with controllable block time.
type conformanceChain struct {
	t   *testing.T
	app *app.ConclaveApp
	now time.Time
}

const confChainID = "conclave-dev-1"

func newConformanceChain(t *testing.T, accs []authtypes.GenesisAccount, balances []banktypes.Balance) *conformanceChain {
	t.Helper()
	conclaveApp := app.NewConclaveApp(
		log.NewNopLogger(), dbm.NewMemDB(), nil, true, sims.EmptyAppOptions{},
		baseapp.SetChainID(confChainID),
	)

	valSet, err := sims.CreateRandomValidatorSet()
	require.NoError(t, err)
	genesisState, err := sims.GenesisStateWithValSet(
		conclaveApp.AppCodec(), conclaveApp.DefaultGenesis(), valSet, accs, balances...,
	)
	require.NoError(t, err)
	stateBytes, err := json.MarshalIndent(genesisState, "", " ")
	require.NoError(t, err)

	c := &conformanceChain{t: t, app: conclaveApp, now: time.Unix(1_750_000_000, 0).UTC()}

	_, err = conclaveApp.InitChain(&abci.RequestInitChain{
		ChainId:         confChainID,
		Time:            c.now,
		Validators:      []abci.ValidatorUpdate{},
		ConsensusParams: sims.DefaultConsensusParams,
		AppStateBytes:   stateBytes,
	})
	require.NoError(t, err)
	c.block() // commit genesis so query contexts see genesis accounts
	return c
}

// block finalizes and commits one block at c.now+1s carrying txs.
func (c *conformanceChain) block(txs ...[]byte) []*abci.ExecTxResult {
	c.t.Helper()
	c.now = c.now.Add(time.Second)
	res, err := c.app.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: c.app.LastBlockHeight() + 1,
		Hash:   c.app.LastCommitID().Hash,
		Time:   c.now,
		Txs:    txs,
	})
	require.NoError(c.t, err)
	_, err = c.app.Commit()
	require.NoError(c.t, err)
	require.Len(c.t, res.TxResults, len(txs))
	return res.TxResults
}

// blockAt advances block time to the given instant (must be monotonic).
func (c *conformanceChain) blockAt(at time.Time, txs ...[]byte) []*abci.ExecTxResult {
	c.t.Helper()
	require.True(c.t, at.After(c.now), "block time must be monotonic")
	c.now = at.Add(-time.Second) // block() adds 1s
	return c.block(txs...)
}

// signTx signs msgs with SIGN_MODE_DIRECT. seq is the sequence embedded in
// auth_info and signed over — deliberately arbitrary for unordered txs.
// timeout != nil attaches the UnorderedTxExtension.
func (c *conformanceChain) signTx(priv cryptotypes.PrivKey, seq uint64, timeout *time.Time, msgs ...sdk.Msg) []byte {
	c.t.Helper()
	txConfig := c.app.TxConfig()
	b := txConfig.NewTxBuilder()
	require.NoError(c.t, b.SetMsgs(msgs...))
	b.SetGasLimit(400_000)

	if timeout != nil {
		ext, err := codectypes.NewAnyWithValue(&unorderedtypes.UnorderedTxExtension{TimeoutTimestamp: *timeout})
		require.NoError(c.t, err)
		extBuilder, ok := b.(interface{ SetExtensionOptions(...*codectypes.Any) })
		require.True(c.t, ok, "tx builder must support extension options")
		extBuilder.SetExtensionOptions(ext)
	}

	addr := sdk.AccAddress(priv.PubKey().Address())
	acc := c.app.AccountKeeper.GetAccount(c.app.NewContext(true), addr)
	require.NotNil(c.t, acc)

	// round 1: populate signer_infos so sign bytes include them
	require.NoError(c.t, b.SetSignatures(signingtypes.SignatureV2{
		PubKey: priv.PubKey(),
		Data: &signingtypes.SingleSignatureData{
			SignMode: signingtypes.SignMode_SIGN_MODE_DIRECT,
		},
		Sequence: seq,
	}))
	sig, err := clienttx.SignWithPrivKey(
		context.Background(), signingtypes.SignMode_SIGN_MODE_DIRECT,
		authsigning.SignerData{
			Address:       addr.String(),
			ChainID:       confChainID,
			AccountNumber: acc.GetAccountNumber(),
			Sequence:      seq,
			PubKey:        priv.PubKey(),
		},
		b, priv, txConfig, seq,
	)
	require.NoError(c.t, err)
	require.NoError(c.t, b.SetSignatures(sig))

	bz, err := txConfig.TxEncoder()(b.GetTx())
	require.NoError(c.t, err)
	return bz
}

func confFunds() sdk.Coins {
	return sdk.NewCoins(
		sdk.NewCoin(app.BaseDenom, math.NewInt(100_000_000_000)),
		sdk.NewCoin(app.MockUSDCDenom, math.NewInt(100_000_000_000)),
	)
}

func confAccounts(t *testing.T, privs ...cryptotypes.PrivKey) ([]authtypes.GenesisAccount, []banktypes.Balance) {
	t.Helper()
	accs := make([]authtypes.GenesisAccount, len(privs))
	balances := make([]banktypes.Balance, len(privs))
	for i, priv := range privs {
		addr := sdk.AccAddress(priv.PubKey().Address())
		accs[i] = authtypes.NewBaseAccount(addr, priv.PubKey(), 0, 0)
		balances[i] = banktypes.Balance{Address: addr.String(), Coins: confFunds()}
	}
	return accs, balances
}

func confSend(from cryptotypes.PrivKey, to sdk.AccAddress, uusdc int64) *banktypes.MsgSend {
	return &banktypes.MsgSend{
		FromAddress: sdk.AccAddress(from.PubKey().Address()).String(),
		ToAddress:   to.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin(app.MockUSDCDenom, math.NewInt(uusdc))),
	}
}

// TestUnorderedWrongSequenceSucceeds50InOneBlock is the §10 row 0.5
// acceptance AND the exact §6.7 upstream-bug reproduction: 50 unordered
// txs from ONE key, every one carrying a deliberately wrong sequence
// number, land in one block.
func TestUnorderedWrongSequenceSucceeds50InOneBlock(t *testing.T) {
	sender := secp256k1.GenPrivKey()
	recipient := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	accs, balances := confAccounts(t, sender)
	c := newConformanceChain(t, accs, balances)

	timeout := c.now.Add(5 * time.Minute)
	txs := make([][]byte, 50)
	for i := range txs {
		// sequences 1000, 1001, … — all wrong (the account is at 0); each
		// tx is distinct, so each gets its own dedup entry
		txs[i] = c.signTx(sender, uint64(1000+i), &timeout, confSend(sender, recipient, int64(1+i)))
	}

	results := c.block(txs...)
	for i, res := range results {
		require.Equal(t, uint32(0), res.Code,
			"unordered tx %d with wrong sequence must succeed: %s", i, res.Log)
	}

	// the account sequence was never touched
	acc := c.app.AccountKeeper.GetAccount(c.app.NewContext(true), sdk.AccAddress(sender.PubKey().Address()))
	require.Equal(t, uint64(0), acc.GetSequence(), "unordered txs must not consume sequence numbers")
}

// TestOrderedWrongSequenceStillFails proves the fork did not weaken
// ordered txs: without the extension, a wrong sequence still rejects with
// the upstream ErrWrongSequence.
func TestOrderedWrongSequenceStillFails(t *testing.T) {
	sender := secp256k1.GenPrivKey()
	recipient := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	accs, balances := confAccounts(t, sender)
	c := newConformanceChain(t, accs, balances)

	res := c.block(c.signTx(sender, 7, nil, confSend(sender, recipient, 1)))
	require.Equal(t, sdkerrors.ErrWrongSequence.ABCICode(), res[0].Code,
		"ordered wrong-sequence tx must keep failing: %s", res[0].Log)

	// and a correctly sequenced ordered tx still works
	res = c.block(c.signTx(sender, 0, nil, confSend(sender, recipient, 1)))
	require.Equal(t, uint32(0), res[0].Code, "ordered tx with correct sequence: %s", res[0].Log)
}

// TestUnorderedReplayRejected: identical bytes twice in one block → the
// second hits the dedup cache; and again in a later block → still cached.
func TestUnorderedReplayRejected(t *testing.T) {
	sender := secp256k1.GenPrivKey()
	recipient := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	accs, balances := confAccounts(t, sender)
	c := newConformanceChain(t, accs, balances)

	timeout := c.now.Add(5 * time.Minute)
	tx := c.signTx(sender, 42, &timeout, confSend(sender, recipient, 5))

	results := c.block(tx, tx)
	require.Equal(t, uint32(0), results[0].Code, "first delivery: %s", results[0].Log)
	require.Equal(t, unorderedtypes.ErrTxReplayed.ABCICode(), results[1].Code,
		"same-block replay must be rejected: %s", results[1].Log)
	require.Equal(t, unorderedtypes.ModuleName, results[1].Codespace)

	results = c.block(tx)
	require.Equal(t, unorderedtypes.ErrTxReplayed.ABCICode(), results[0].Code,
		"cross-block replay must be rejected: %s", results[0].Log)
}

// TestUnorderedTTLCap: a timeout beyond block time + 10 minutes rejects
// (SPEC §6.7 state-growth guard), and a missing/expired timeout rejects.
func TestUnorderedTTLCap(t *testing.T) {
	sender := secp256k1.GenPrivKey()
	recipient := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	accs, balances := confAccounts(t, sender)
	c := newConformanceChain(t, accs, balances)

	tooLong := c.now.Add(unorderedtypes.MaxUnorderedTTL + 2*time.Second)
	res := c.block(c.signTx(sender, 1, &tooLong, confSend(sender, recipient, 1)))
	require.Equal(t, unorderedtypes.ErrTTLTooLong.ABCICode(), res[0].Code, "TTL cap: %s", res[0].Log)

	expired := c.now.Add(-time.Second)
	res = c.block(c.signTx(sender, 2, &expired, confSend(sender, recipient, 1)))
	require.Equal(t, unorderedtypes.ErrInvalidTimeout.ABCICode(), res[0].Code, "expired timeout: %s", res[0].Log)
}

// TestUnorderedExpirySweepNoReopen: the dedup entry exists exactly while
// the tx could still be valid; the EndBlock sweep removes it only once the
// timeout itself rejects the tx — sweeping can never re-open a replay.
func TestUnorderedExpirySweepNoReopen(t *testing.T) {
	sender := secp256k1.GenPrivKey()
	recipient := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	accs, balances := confAccounts(t, sender)
	c := newConformanceChain(t, accs, balances)

	timeout := c.now.Add(30 * time.Second)
	tx := c.signTx(sender, 9, &timeout, confSend(sender, recipient, 5))
	txHash := sha256.Sum256(tx)

	res := c.block(tx)
	require.Equal(t, uint32(0), res[0].Code, "%s", res[0].Log)

	seen, err := c.app.UnorderedManager.Contains(c.app.NewContext(true), timeout, txHash[:])
	require.NoError(t, err)
	require.True(t, seen, "dedup entry must exist while the tx is in its window")

	// an empty block past the timeout sweeps the entry in EndBlock
	c.blockAt(timeout.Add(2 * time.Second))
	seen, err = c.app.UnorderedManager.Contains(c.app.NewContext(true), timeout, txHash[:])
	require.NoError(t, err)
	require.False(t, seen, "the EndBlock sweep must remove the expired entry")

	// no-reopen: replaying after the sweep fails on the timeout itself
	// (ErrInvalidTimeout, not ErrTxReplayed) — the predicates are exact
	// complements, so there is no height at which the replay lands
	res = c.block(tx)
	require.Equal(t, unorderedtypes.ErrInvalidTimeout.ABCICode(), res[0].Code,
		"post-sweep replay must die on its own timeout: %s", res[0].Log)
}

// TestUnorderedMandateIntersection is the 0.4×0.5 payoff: 50 parallel
// unordered txs from one Mandate-bound session key whose aggregate exceeds
// the window ceiling → exactly the under-ceiling prefix lands in the
// block, the rest are rejected by the optimistic window charge
// (ErrMandateExceeded), and the next attempt bounces in CheckTx.
func TestUnorderedMandateIntersection(t *testing.T) {
	operator := secp256k1.GenPrivKey()
	session := secp256k1.GenPrivKey()
	operatorAddr := sdk.AccAddress(operator.PubKey().Address())
	accs, balances := confAccounts(t, operator, session)
	c := newConformanceChain(t, accs, balances)

	// register the sigil and issue the mandate (ordered operator txs)
	didHash := sha256.Sum256([]byte("did:conclave:agent-alice"))
	res := c.block(c.signTx(operator, 0, nil, &sigiltypes.MsgRegisterSigil{
		Controller: operatorAddr.String(),
		DidDocHash: didHash[:],
	}))
	require.Equal(t, uint32(0), res[0].Code, "register sigil: %s", res[0].Log)

	sigilID, err := c.app.SigilKeeper.SigilIDByAddress(c.app.NewContext(true), 0, operatorAddr)
	require.NoError(t, err)

	const sendAmount = 10_000_000 // 10 USDC per tx
	const admitted = 10           // window admits exactly 10 of the 50
	res = c.block(c.signTx(operator, 1, nil, &mandatetypes.MsgIssueMandate{
		Controller: operatorAddr.String(),
		Terms: mandatetypes.MandateTerms{
			SigilId:            sigilID,
			GranteeAddr:        sdk.AccAddress(session.PubKey().Address()).String(),
			AllowedMsgTypes:    []string{sdk.MsgTypeURL(&banktypes.MsgSend{})},
			SpendCeilingPerTx:  sdk.NewCoins(sdk.NewCoin(app.MockUSDCDenom, math.NewInt(sendAmount))),
			SpendCeilingWindow: sdk.NewCoins(sdk.NewCoin(app.MockUSDCDenom, math.NewInt(admitted*sendAmount))),
			WindowBlocks:       86_400,
			FeeDenoms:          []string{app.MockUSDCDenom},
			ExpiryHeight:       1_000_000,
		},
	}))
	require.Equal(t, uint32(0), res[0].Code, "issue mandate: %s", res[0].Log)

	// 50 parallel unordered sends, wrong sequences, one block
	timeout := c.now.Add(5 * time.Minute)
	txs := make([][]byte, 50)
	for i := range txs {
		txs[i] = c.signTx(session, uint64(2000+i), &timeout, confSend(session, operatorAddr, sendAmount))
	}
	results := c.block(txs...)

	var landed, bounced int
	for i, r := range results {
		switch r.Code {
		case 0:
			landed++
		case mandatetypes.ErrMandateExceeded.ABCICode():
			require.Equal(t, mandatetypes.ModuleName, r.Codespace, "tx %d: %s", i, r.Log)
			bounced++
		default:
			t.Fatalf("tx %d: unexpected code %d (%s): %s", i, r.Code, r.Codespace, r.Log)
		}
	}
	require.Equal(t, admitted, landed, "exactly the under-ceiling prefix lands")
	require.Equal(t, 50-admitted, bounced, "the rest bounce on the window ceiling")

	// the prefix property: every success precedes every bounce (DeliverTx
	// executes in block order against one window)
	firstBounce := -1
	for i, r := range results {
		if r.Code != 0 && firstBounce == -1 {
			firstBounce = i
		}
		if firstBounce != -1 {
			require.NotEqual(t, uint32(0), r.Code, "tx %d landed after the window closed", i)
		}
	}

	// committed window state equals the ceiling
	m, found, err := c.app.MandateKeeper.ActiveMandateByGrantee(
		c.app.NewContext(true), 0, sdk.AccAddress(session.PubKey().Address()))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t,
		sdk.NewCoins(sdk.NewCoin(app.MockUSDCDenom, math.NewInt(admitted*sendAmount))).String(),
		m.WindowSpent.String())

	// a 51st attempt bounces in CheckTx — the mempool-facing half of the
	// SPEC §6.2 step 5 guarantee
	extra := c.signTx(session, 9999, &timeout, confSend(session, operatorAddr, sendAmount))
	checkRes, err := c.app.CheckTx(&abci.RequestCheckTx{Tx: extra, Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.Equal(t, mandatetypes.ErrMandateExceeded.ABCICode(), checkRes.Code,
		"the 51st unordered tx must bounce in CheckTx: %s", checkRes.Log)
	require.Equal(t, mandatetypes.ModuleName, checkRes.Codespace)
}
