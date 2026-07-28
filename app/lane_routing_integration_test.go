package app_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	abci "github.com/cometbft/cometbft/abci/types"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"

	"github.com/AryaSingh22/conclave/app"
	mandatetypes "github.com/AryaSingh22/conclave/x/mandate/types"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
	sigiltypes "github.com/AryaSingh22/conclave/x/sigil/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// The SPEC §10 row 0.6 acceptance suite: msgs of each class land in their
// designated lanes with correct priority and space limits, via the real
// PrepareProposal/ProcessProposal handlers over an in-process app.
//
// Veil and session msgs route and verify through proposals (their types
// are decodable since 0.6, D-033). Veil msgs also EXECUTE through
// FinalizeBlock since task 0.7 (TestVeilLaneFinalizeBlockExecution below;
// D-024 resolved). Session txs are still covered only through
// PrepareProposal/ProcessProposal here, never through FinalizeBlock execution.
//
// CONCLAVE-TODO(phase-3): extend to FinalizeBlock execution coverage of
// session txs — owned by task 3.5 (gate-coverage expansion). RE-TAGGED from
// phase-0 at PHASE2_DESIGN Rev 3 (D-090): the marker deferred this to task
// 0.8, which completed two phases ago without adding the coverage, so it was
// an unowned debt rather than a pending one — AUDIT F-8's exact class.

// prepare runs PrepareProposal for the next height.
func (c *conformanceChain) prepare(maxTxBytes int64) *abci.ResponsePrepareProposal {
	c.t.Helper()
	res, err := c.app.PrepareProposal(&abci.RequestPrepareProposal{
		Height:     c.app.LastBlockHeight() + 1,
		Time:       c.now.Add(time.Second),
		MaxTxBytes: maxTxBytes,
	})
	require.NoError(c.t, err)
	return res
}

// process runs ProcessProposal for the next height over the given txs.
func (c *conformanceChain) process(txs [][]byte) abci.ResponseProcessProposal_ProposalStatus {
	c.t.Helper()
	res, err := c.app.ProcessProposal(&abci.RequestProcessProposal{
		Height: c.app.LastBlockHeight() + 1,
		Time:   c.now.Add(time.Second),
		Txs:    txs,
	})
	// the Block SDK handler returns the rejection error alongside REJECT;
	// only the status is the consensus-relevant output
	_ = err
	return res.Status
}

// insertLaneTx decodes signed tx bytes and inserts them directly into the
// laned mempool, bypassing CheckTx. Used where CheckTx cannot admit the tx
// yet (veil/session msgs have no msg server until 0.7/0.8 — D-024) and to
// isolate proposal-time behavior from CheckTx-time behavior.
func (c *conformanceChain) insertLaneTx(bz []byte) {
	c.t.Helper()
	tx, err := c.app.TxConfig().TxDecoder()(bz)
	require.NoError(c.t, err)
	require.NoError(c.t, c.app.LanedMempool.Insert(c.app.NewContext(true), tx))
}

// syntheticNonce mirrors the D-034 adapter: BE uint64 of sha256(tx)[:8].
func syntheticNonce(bz []byte) uint64 {
	hash := sha256.Sum256(bz)
	return binary.BigEndian.Uint64(hash[:8])
}

func laneVeilSubmit(t *testing.T, sender cryptotypes.PrivKey, feeBid string) *veiltypes.MsgSubmitIntent {
	t.Helper()
	env := veiltypes.IntentEnvelope{TtlHeight: 1000, FeeBid: feeBid}
	bz, err := env.Marshal()
	require.NoError(t, err)
	return &veiltypes.MsgSubmitIntent{
		Sender:      sdk.AccAddress(sender.PubKey().Address()).String(),
		SenderSigil: "sigil-test",
		Ciphertext:  []byte{0x01},
		Envelope:    bz,
		Bond:        sdk.NewInt64Coin(app.BaseDenom, 1_000_000),
	}
}

// TestLaneRoutingProposalRoundTrip is the core §10 row 0.6 acceptance:
// one tx per lane class (two for veil, with distinct fee bids) inserted in
// scrambled order; PrepareProposal partitions the block veil → session →
// commerce → ibc → general with the veil lane ordered by fee_bid; the
// identical proposal passes ProcessProposal; misordered variants are
// rejected.
func TestLaneRoutingProposalRoundTrip(t *testing.T) {
	alice := secp256k1.GenPrivKey()    // veil, fee_bid 10
	bob := secp256k1.GenPrivKey()      // veil, fee_bid 5
	solver := secp256k1.GenPrivKey()   // session
	operator := secp256k1.GenPrivKey() // commerce
	relayer := secp256k1.GenPrivKey()  // ibc
	carol := secp256k1.GenPrivKey()    // general
	accs, balances := confAccounts(t, alice, bob, solver, operator, relayer, carol)
	c := newConformanceChain(t, accs, balances)

	veil10 := c.signTx(alice, 0, nil, laneVeilSubmit(t, alice, "10"))
	veil5 := c.signTx(bob, 0, nil, laneVeilSubmit(t, bob, "5"))
	session := c.signTx(solver, 0, nil, &sessiontypes.MsgRegisterSolver{
		Solver: sdk.AccAddress(solver.PubKey().Address()).String(),
		Bond:   sdk.NewCoins(sdk.NewInt64Coin(app.BaseDenom, 1_000_000)),
	})
	didHash := sha256.Sum256([]byte("did:conclave:lane-test"))
	commerce := c.signTx(operator, 0, nil, &sigiltypes.MsgRegisterSigil{
		Controller: sdk.AccAddress(operator.PubKey().Address()).String(),
		DidDocHash: didHash[:],
	})
	ibc := c.signTx(relayer, 0, nil, ibctransfertypes.NewMsgTransfer(
		"transfer", "channel-0", sdk.NewInt64Coin(app.BaseDenom, 1),
		sdk.AccAddress(relayer.PubKey().Address()).String(),
		sdk.AccAddress(carol.PubKey().Address()).String(),
		clienttypes.NewHeight(1, 100_000), 0, "",
	))
	general := c.signTx(carol, 0, nil, confSend(carol, sdk.AccAddress(operator.PubKey().Address()), 1))

	// insert in scrambled order — lane routing and intra-lane priority,
	// not insertion order, must decide the proposal
	for _, bz := range [][]byte{general, veil5, ibc, session, veil10, commerce} {
		c.insertLaneTx(bz)
	}

	dist := c.app.LanedMempool.GetTxDistribution()
	require.Equal(t, uint64(2), dist[app.LaneVeil], "veil lane holds both intents")
	require.Equal(t, uint64(1), dist[app.LaneSession])
	require.Equal(t, uint64(1), dist[app.LaneCommerce])
	require.Equal(t, uint64(1), dist[app.LaneIBC])
	require.Equal(t, uint64(1), dist[app.LaneGeneral])

	res := c.prepare(1_000_000)
	want := [][]byte{veil10, veil5, session, commerce, ibc, general}
	require.Len(t, res.Txs, len(want))
	for i, bz := range want {
		require.True(t, bytes.Equal(bz, res.Txs[i]),
			"proposal position %d: lanes must partition veil(fee desc) → session → commerce → ibc → general", i)
	}

	// the honest proposal passes ProcessProposal
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, c.process(want))

	// a general tx ahead of the veil partition violates lane contiguity
	misordered := [][]byte{general, veil10, veil5, session, commerce, ibc}
	require.Equal(t, abci.ResponseProcessProposal_REJECT, c.process(misordered),
		"partition order is consensus-validated")

	// veil txs out of fee_bid order violate intra-lane priority ordering
	feeSwapped := [][]byte{veil5, veil10, session, commerce, ibc, general}
	require.Equal(t, abci.ResponseProcessProposal_REJECT, c.process(feeSwapped),
		"veil fee_bid ordering is consensus-validated")
}

// TestLaneSpaceLimitQueueing pins the D-036 over-share behavior: a lane
// over its block-space share QUEUES the excess in its mempool for later
// blocks — txs are neither dropped nor spilled into other lanes' space.
func TestLaneSpaceLimitQueueing(t *testing.T) {
	privs := make([]cryptotypes.PrivKey, 4)
	for i := range privs {
		privs[i] = secp256k1.GenPrivKey()
	}
	accs, balances := confAccounts(t, privs...)
	c := newConformanceChain(t, accs, balances)

	recipient := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	txs := make([][]byte, len(privs))
	maxSize := 0
	for i, priv := range privs {
		txs[i] = c.signTx(priv, 0, nil, confSend(priv, recipient, int64(1+i)))
		c.insertLaneTx(txs[i])
		if len(txs[i]) > maxSize {
			maxSize = len(txs[i])
		}
	}

	// size the request so the general lane's 15% share fits exactly two txs
	maxTxBytes := int64(maxSize) * 5 / 3 * 10 // 15% of this ≈ 2.5 × maxSize
	res := c.prepare(maxTxBytes)
	require.Len(t, res.Txs, 2, "general lane must stop at its 15%% share")

	// nothing was dropped: the two excess txs remain queued in the lane
	require.Equal(t, uint64(4), c.app.LanedMempool.GetTxDistribution()[app.LaneGeneral],
		"over-share txs queue in the lane mempool (D-036)")

	// commit the proposed block; the queued txs drain into the next one
	results := c.block(res.Txs...)
	for _, r := range results {
		require.Equal(t, uint32(0), r.Code, "%s", r.Log)
	}

	res2 := c.prepare(maxTxBytes)
	require.Len(t, res2.Txs, 2, "the queued remainder drains next block")
	for _, bz := range res2.Txs {
		require.False(t, bytes.Equal(bz, res.Txs[0]) || bytes.Equal(bz, res.Txs[1]),
			"drained txs are the previously queued ones, not re-proposals")
	}
}

// TestProcessProposalRejectsOverShareLane: a proposal whose general-lane
// partition exceeds the lane's 15% of the consensus max block bytes is
// rejected by every honest node, while an under-share subset of the same
// txs is accepted — lane space limits are consensus-validated.
func TestProcessProposalRejectsOverShareLane(t *testing.T) {
	const nSenders = 24
	privs := make([]cryptotypes.PrivKey, nSenders)
	for i := range privs {
		privs[i] = secp256k1.GenPrivKey()
	}
	accs, balances := confAccounts(t, privs...)
	c := newConformanceChain(t, accs, balances)

	recipient := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	// 10 msgs per tx to bulk the partition without breaching gas limits
	txs := make([][]byte, nSenders)
	totalSize := int64(0)
	for i, priv := range privs {
		msgs := make([]sdk.Msg, 10)
		for j := range msgs {
			msgs[j] = confSend(priv, recipient, int64(1+j))
		}
		txs[i] = c.signTx(priv, 0, nil, msgs...)
		totalSize += int64(len(txs[i]))
	}

	// ProcessProposal lane limits derive from the consensus block params
	// the chain was initialized with (proposals.GetBlockLimits)
	maxBlockBytes := sims.DefaultConsensusParams.Block.MaxBytes
	laneLimit := math.LegacyMustNewDecFromStr("0.15").MulInt64(maxBlockBytes).TruncateInt64()
	require.Greater(t, totalSize, laneLimit,
		"test setup: the full batch must exceed the general lane's share (%d > %d)", totalSize, laneLimit)

	require.Equal(t, abci.ResponseProcessProposal_REJECT, c.process(txs),
		"a lane partition above its share must be rejected")

	// control: a two-tx subset of the same batch is well under the share
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, c.process(txs[:2]),
		"the same txs under the share are accepted — the limit is what rejects")
}

// TestUnorderedSameSequenceLaneSurvival is the D-034 regression in full:
// two unordered txs from ONE key carrying the SAME claimed sequence
// transit CheckTx → laned mempool → PrepareProposal → ProcessProposal →
// FinalizeBlock. Without the synthetic-nonce signer extractor the second
// tx would silently replace the first on insert, and a proposal carrying
// both would be rejected by the lane comparator's same-sequence error.
func TestUnorderedSameSequenceLaneSurvival(t *testing.T) {
	sender := secp256k1.GenPrivKey()
	recipient := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	accs, balances := confAccounts(t, sender)
	c := newConformanceChain(t, accs, balances)

	timeout := c.now.Add(5 * time.Minute)
	txA := c.signTx(sender, 7, &timeout, confSend(sender, recipient, 1))
	txB := c.signTx(sender, 7, &timeout, confSend(sender, recipient, 2))

	for _, bz := range [][]byte{txA, txB} {
		res, err := c.app.CheckTx(&abci.RequestCheckTx{Tx: bz, Type: abci.CheckTxType_New})
		require.NoError(t, err)
		require.Equal(t, uint32(0), res.Code, "CheckTx must admit both: %s", res.Log)
	}

	require.Equal(t, uint64(2), c.app.LanedMempool.GetTxDistribution()[app.LaneGeneral],
		"both same-sequence unordered txs must coexist in the lane mempool")

	res := c.prepare(1_000_000)
	require.Len(t, res.Txs, 2, "both txs must be proposed")
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, c.process(res.Txs))

	results := c.block(res.Txs...)
	for i, r := range results {
		require.Equal(t, uint32(0), r.Code, "tx %d must execute: %s", i, r.Log)
	}
}

// TestProposalWindowCoherence is the proposal-time mandate-window gate:
// K+1 unordered txs from one mandate-bound key where only K fit the window
// ceiling. PrepareProposal includes exactly K (the K+1th fails lane ante
// verification against the accumulated window) and ProcessProposal rejects
// a proposal carrying all K+1 — proposal verification and block execution
// agree on mandate aggregation (SPEC §6.2 step 5).
func TestProposalWindowCoherence(t *testing.T) {
	operator := secp256k1.GenPrivKey()
	session := secp256k1.GenPrivKey()
	operatorAddr := sdk.AccAddress(operator.PubKey().Address())
	accs, balances := confAccounts(t, operator, session)
	c := newConformanceChain(t, accs, balances)

	didHash := sha256.Sum256([]byte("did:conclave:window-coherence"))
	res := c.block(c.signTx(operator, 0, nil, &sigiltypes.MsgRegisterSigil{
		Controller: operatorAddr.String(),
		DidDocHash: didHash[:],
	}))
	require.Equal(t, uint32(0), res[0].Code, "register sigil: %s", res[0].Log)

	sigilID, err := c.app.SigilKeeper.SigilIDByAddress(c.app.NewContext(true), 0, operatorAddr)
	require.NoError(t, err)

	const sendAmount = 10_000_000 // 10 USDC per tx
	const admitted = 3            // the window fits exactly K = 3
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

	// K+1 unordered sends, ALL claiming sequence 0 (the natural client
	// choice the synthetic nonce exists for) with distinct recipients so
	// each tx is distinct, inserted directly into the laned mempool to
	// isolate proposal-time verification from CheckTx
	timeout := c.now.Add(5 * time.Minute)
	txs := make([][]byte, admitted+1)
	for i := range txs {
		recipient := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
		txs[i] = c.signTx(session, 0, &timeout, confSend(session, recipient, sendAmount))
		c.insertLaneTx(txs[i])
	}

	prepared := c.prepare(1_000_000)
	require.Len(t, prepared.Txs, admitted,
		"PrepareProposal must include exactly the K window-fitting txs")

	// the prepare-time window charge ran on discarded proposal state
	m, found, err := c.app.MandateKeeper.ActiveMandateByGrantee(
		c.app.NewContext(true), 0, sdk.AccAddress(session.PubKey().Address()))
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, m.WindowSpent.IsZero(),
		"proposal preparation must never charge the committed window")

	// hand-build proposals in lane comparator order (ascending synthetic
	// nonce for one signer) so window aggregation, not tx ordering, is the
	// only variable between the K and K+1 proposals
	sorted := make([][]byte, len(txs))
	copy(sorted, txs)
	sort.Slice(sorted, func(i, j int) bool {
		return syntheticNonce(sorted[i]) < syntheticNonce(sorted[j])
	})

	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, c.process(sorted[:admitted]),
		"K window-fitting txs must be accepted")
	require.Equal(t, abci.ResponseProcessProposal_REJECT, c.process(sorted),
		"K+1 txs over the window ceiling must be rejected at ProcessProposal")
}

// TestVeilLaneFinalizeBlockExecution closes the task-0.7 half of the
// 0.6 phase-0 TODO: a veil MsgSubmitIntent now transits the WHOLE path —
// CheckTx admission (which D-024 forbade before the msg server landed) →
// veil lane → PrepareProposal → ProcessProposal → FinalizeBlock execution
// — and lands as a SUBMITTED intent with its bond escrowed. The submitter
// is a sigil-controlling root key, so the MandateDecorator passes it
// through (step 1) and the only gating is x/veil's own validation.
func TestVeilLaneFinalizeBlockExecution(t *testing.T) {
	agent := secp256k1.GenPrivKey()
	agentAddr := sdk.AccAddress(agent.PubKey().Address())
	accs, balances := confAccounts(t, agent)
	c := newConformanceChain(t, accs, balances)

	// register the agent's sigil (commerce lane) so SubmitIntent's
	// sender→sigil resolution succeeds
	didHash := sha256.Sum256([]byte("did:conclave:veil-exec"))
	res := c.block(c.signTx(agent, 0, nil, &sigiltypes.MsgRegisterSigil{
		Controller: agentAddr.String(),
		DidDocHash: didHash[:],
	}))
	require.Equal(t, uint32(0), res[0].Code, "register sigil: %s", res[0].Log)

	sigilID, err := c.app.SigilKeeper.SigilIDByAddress(c.app.NewContext(true), 0, agentAddr)
	require.NoError(t, err)

	coin := sdk.NewInt64Coin(app.MockUSDCDenom, 100_000_000)
	payloadBz, err := (&veiltypes.IntentPayload{
		Give: []veiltypes.IntentLeg{{Asset: &veiltypes.IntentLeg_Coin{Coin: &coin}}},
	}).Marshal()
	require.NoError(t, err)
	envBz, err := (&veiltypes.IntentEnvelope{TtlHeight: 100, FeeBid: "7"}).Marshal()
	require.NoError(t, err)
	bond := sdk.NewInt64Coin(app.BaseDenom, 1_000_000)
	submit := &veiltypes.MsgSubmitIntent{
		Sender:      agentAddr.String(),
		SenderSigil: sigilID,
		Ciphertext:  payloadBz,
		Envelope:    envBz,
		Bond:        bond,
	}

	// CheckTx must now ADMIT the veil tx (D-024 resolved: the msg server is
	// routed), and it must route into the veil lane
	txBz := c.signTx(agent, 1, nil, submit)
	checkRes, err := c.app.CheckTx(&abci.RequestCheckTx{Tx: txBz, Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.Equal(t, uint32(0), checkRes.Code, "CheckTx must admit the veil tx: %s", checkRes.Log)
	require.Equal(t, uint64(1), c.app.LanedMempool.GetTxDistribution()[app.LaneVeil],
		"the submit intent routes into the veil lane")

	// prepare → process → finalize the proposal carrying it
	prepared := c.prepare(1_000_000)
	require.Len(t, prepared.Txs, 1)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, c.process(prepared.Txs))
	results := c.block(prepared.Txs...)
	require.Equal(t, uint32(0), results[0].Code, "veil submit must execute: %s", results[0].Log)

	// the intent landed mid-lifecycle with its bond escrowed (it is
	// SUBMITTED, or SEALED_IN_SESSION if the exec block was a boundary —
	// either way it executed and is progressing)
	ctx := c.app.NewContext(true)
	var rec veiltypes.IntentRecord
	require.NoError(t, c.app.VeilKeeper.IterateIntentRecords(ctx, func(r veiltypes.IntentRecord) bool {
		rec = r
		return true
	}))
	require.True(t, veiltypes.IsPreOpen(rec.State),
		"the intent must land in a live pre-OPEN state, got %s", rec.State)
	require.Equal(t, agentAddr.String(), rec.Owner)
	require.Equal(t, bond.Amount,
		c.app.BankKeeper.GetBalance(ctx, authtypes.NewModuleAddress(veiltypes.ModuleName), app.BaseDenom).Amount,
		"bond escrowed in the veil module account through full block execution")
}
