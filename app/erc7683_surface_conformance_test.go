package app_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"
	"cosmossdk.io/math"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtprotocrypto "github.com/cometbft/cometbft/proto/tendermint/crypto"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	commitmenttypes "github.com/cosmos/ibc-go/v8/modules/core/23-commitment/types"

	"github.com/AryaSingh22/conclave/app"
	"github.com/AryaSingh22/conclave/fillback"
	mandatetypes "github.com/AryaSingh22/conclave/x/mandate/types"
	receiptkeeper "github.com/AryaSingh22/conclave/x/receipt/keeper"
	receipttypes "github.com/AryaSingh22/conclave/x/receipt/types"
	sessionkeeper "github.com/AryaSingh22/conclave/x/session/keeper"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
	sigiltypes "github.com/AryaSingh22/conclave/x/sigil/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// erc7683Fixture is the resolver-shaped harness behind the
// docs/edge/7683_SURFACE.md conformance suite (task 2.4, D-086): a resolver
// operator (sigil controller, Model A — the canonical 7683 filler shape),
// its mandate-bound hot key, two Conclave-native counterparties (order
// recipients), and an unbound rogue key, driven through real
// FinalizeBlock+Commit blocks so the session lifecycle (seal → reveal →
// auction → fallback → Conclude → receipts) runs exactly as consensus runs
// it. The x402 sibling (x402_surface_conformance_test.go) proves the
// submission-authority claims; this suite proves the FILL-BACK EVIDENCE
// claims — what the ICS-23 receipt proof does and, load-bearingly, does NOT
// prove (7683_SURFACE.md §6).
type erc7683Fixture struct {
	t   *testing.T
	app *app.ConclaveApp

	// roots records the committed app root per height — the bridge-side
	// verifier needs the app hash AT ProofHeight (a real bridge reads it
	// from the block header at ProofHeight+1 via its light client).
	roots map[uint64][]byte

	resolverOpPriv, resolverHotPriv, userAPriv, userBPriv, roguePriv cryptotypes.PrivKey
	resolverOp, resolverHot, userA, userB, rogue                     sdk.AccAddress
}

const erc7683ChainID = "conclave-dev-1"

func newERC7683Fixture(t *testing.T) *erc7683Fixture {
	t.Helper()
	f := &erc7683Fixture{t: t, roots: make(map[uint64][]byte)}
	f.app = app.NewConclaveApp(
		log.NewNopLogger(), dbm.NewMemDB(), nil, true, sims.EmptyAppOptions{},
		baseapp.SetChainID(erc7683ChainID),
	)

	f.resolverOpPriv = secp256k1.GenPrivKey()
	f.resolverHotPriv = secp256k1.GenPrivKey()
	f.userAPriv = secp256k1.GenPrivKey()
	f.userBPriv = secp256k1.GenPrivKey()
	f.roguePriv = secp256k1.GenPrivKey()
	f.resolverOp = sdk.AccAddress(f.resolverOpPriv.PubKey().Address())
	f.resolverHot = sdk.AccAddress(f.resolverHotPriv.PubKey().Address())
	f.userA = sdk.AccAddress(f.userAPriv.PubKey().Address())
	f.userB = sdk.AccAddress(f.userBPriv.PubKey().Address())
	f.rogue = sdk.AccAddress(f.roguePriv.PubKey().Address())

	// Model A custody split (7683_SURFACE.md §1.2): the RESOLVER OPERATOR
	// (sigil controller) holds the destination liquidity the fills settle
	// from; the hot key holds only gas + bond float.
	funds := sdk.NewCoins(
		sdk.NewCoin(app.BaseDenom, math.NewInt(10_000_000_000)),
		sdk.NewCoin(app.MockUSDCDenom, math.NewInt(10_000_000_000)),
	)
	genAccs := []authtypes.GenesisAccount{
		authtypes.NewBaseAccount(f.resolverOp, f.resolverOpPriv.PubKey(), 0, 0),
		authtypes.NewBaseAccount(f.resolverHot, f.resolverHotPriv.PubKey(), 0, 0),
		authtypes.NewBaseAccount(f.userA, f.userAPriv.PubKey(), 0, 0),
		authtypes.NewBaseAccount(f.userB, f.userBPriv.PubKey(), 0, 0),
		authtypes.NewBaseAccount(f.rogue, f.roguePriv.PubKey(), 0, 0),
	}
	balances := []banktypes.Balance{
		{Address: f.resolverOp.String(), Coins: funds},
		{Address: f.resolverHot.String(), Coins: funds},
		{Address: f.userA.String(), Coins: funds},
		{Address: f.userB.String(), Coins: funds},
		{Address: f.rogue.String(), Coins: funds},
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
		ChainId:         erc7683ChainID,
		Validators:      []abci.ValidatorUpdate{},
		ConsensusParams: sims.DefaultConsensusParams,
		AppStateBytes:   stateBytes,
	})
	require.NoError(t, err)

	f.advance(1)
	return f
}

// advance runs n empty blocks (FinalizeBlock+Commit) — the session/veil
// EndBlock pipeline (seal → reveal → auction → fallback → Conclude) runs
// inside them exactly as on a live node.
func (f *erc7683Fixture) advance(n int) {
	f.t.Helper()
	for i := 0; i < n; i++ {
		_, err := f.app.FinalizeBlock(&abci.RequestFinalizeBlock{
			Height: f.app.LastBlockHeight() + 1,
			Hash:   f.app.LastCommitID().Hash,
		})
		require.NoError(f.t, err)
		_, err = f.app.Commit()
		require.NoError(f.t, err)
		f.roots[uint64(f.app.LastBlockHeight())] = f.app.LastCommitID().Hash
	}
}

// deliverAll executes the given (priv, msg) pairs in ONE block and returns
// the per-tx results. One block matters for the fill/counterpart pair: both
// intents enter SUBMITTED together and seal into the same session.
func (f *erc7683Fixture) deliverAll(privs []cryptotypes.PrivKey, msgs []sdk.Msg) []*abci.ExecTxResult {
	f.t.Helper()
	require.Equal(f.t, len(privs), len(msgs))
	ctx := f.app.NewContext(true)
	txs := make([][]byte, len(msgs))
	for i := range msgs {
		addr := sdk.AccAddress(privs[i].PubKey().Address())
		acc := f.app.AccountKeeper.GetAccount(ctx, addr)
		require.NotNil(f.t, acc)
		txn, err := sims.GenSignedMockTx(
			rand.New(rand.NewSource(42)), f.app.TxConfig(), []sdk.Msg{msgs[i]},
			sdk.Coins{}, sims.DefaultGenTxGas, erc7683ChainID,
			[]uint64{acc.GetAccountNumber()}, []uint64{acc.GetSequence()}, privs[i],
		)
		require.NoError(f.t, err)
		txs[i], err = f.app.TxConfig().TxEncoder()(txn)
		require.NoError(f.t, err)
	}
	res, err := f.app.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: f.app.LastBlockHeight() + 1,
		Hash:   f.app.LastCommitID().Hash,
		Txs:    txs,
	})
	require.NoError(f.t, err)
	_, err = f.app.Commit()
	require.NoError(f.t, err)
	f.roots[uint64(f.app.LastBlockHeight())] = f.app.LastCommitID().Hash
	require.Len(f.t, res.TxResults, len(msgs))
	return res.TxResults
}

func (f *erc7683Fixture) deliver(priv cryptotypes.PrivKey, msg sdk.Msg) *abci.ExecTxResult {
	f.t.Helper()
	return f.deliverAll([]cryptotypes.PrivKey{priv}, []sdk.Msg{msg})[0]
}

func (f *erc7683Fixture) registerSigil(priv cryptotypes.PrivKey, did string) string {
	f.t.Helper()
	addr := sdk.AccAddress(priv.PubKey().Address())
	didHash := sha256.Sum256([]byte(did))
	res := f.deliver(priv, &sigiltypes.MsgRegisterSigil{
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

// resolverTerms is the 7683_SURFACE.md §3 recommended mandate shape for a
// resolver hot key: submit/cancel only (a fill resolver has no MsgSend
// business), uusdc fill ceilings plus a uclv bond budget.
func (f *erc7683Fixture) resolverTerms(sigilID string) mandatetypes.MandateTerms {
	bondBudget := sdk.NewCoins(sdk.NewCoin(app.BaseDenom, math.NewInt(10_000_000)))
	ceiling := sdk.NewCoins(sdk.NewCoin(app.MockUSDCDenom, math.NewInt(500_000_000))).Add(bondBudget...)
	return mandatetypes.MandateTerms{
		SigilId:     sigilID,
		GranteeAddr: f.resolverHot.String(),
		AllowedMsgTypes: []string{
			sdk.MsgTypeURL(&veiltypes.MsgSubmitIntent{}),
			sdk.MsgTypeURL(&veiltypes.MsgCancelIntent{}),
		},
		SpendCeilingPerTx:  ceiling,
		SpendCeilingWindow: ceiling,
		WindowBlocks:       86_400,
		FeeDenoms:          []string{app.MockUSDCDenom},
		ExpiryHeight:       uint64(f.app.LastBlockHeight()) + 7*86_400,
	}
}

// fillMsg builds the translated fill: give `giveUsdc` uusdc / want `units`
// cmpt service units, declaring `commitUsdc` as spend_commitment. The
// declared/actual split is the D-065 lever the under-declaration case pulls.
func (f *erc7683Fixture) fillMsg(sender sdk.AccAddress, senderSigil string, giveUsdc, commitUsdc, units int64) *veiltypes.MsgSubmitIntent {
	f.t.Helper()
	give := sdk.NewCoin(app.MockUSDCDenom, math.NewInt(giveUsdc))
	payloadBz, err := (&veiltypes.IntentPayload{
		Give: []veiltypes.IntentLeg{{Asset: &veiltypes.IntentLeg_Coin{Coin: &give}}},
		Want: []veiltypes.IntentLeg{erc7683SvcLeg(units)},
	}).Marshal()
	require.NoError(f.t, err)
	envBz, err := (&veiltypes.IntentEnvelope{
		TtlHeight:       uint64(f.app.LastBlockHeight()) + 1_000,
		SpendCommitment: sdk.NewCoins(sdk.NewCoin(app.MockUSDCDenom, math.NewInt(commitUsdc))),
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

// counterpartMsg is the Conclave-native side: give `units` cmpt / want
// `wantUsdc` uusdc (the mirror the fallback matcher pairs with a fill).
func (f *erc7683Fixture) counterpartMsg(sender sdk.AccAddress, senderSigil string, units, wantUsdc int64) *veiltypes.MsgSubmitIntent {
	f.t.Helper()
	want := sdk.NewCoin(app.MockUSDCDenom, math.NewInt(wantUsdc))
	payloadBz, err := (&veiltypes.IntentPayload{
		Give: []veiltypes.IntentLeg{erc7683SvcLeg(units)},
		Want: []veiltypes.IntentLeg{{Asset: &veiltypes.IntentLeg_Coin{Coin: &want}}},
	}).Marshal()
	require.NoError(f.t, err)
	envBz, err := (&veiltypes.IntentEnvelope{
		TtlHeight: uint64(f.app.LastBlockHeight()) + 1_000,
		// svc-only give: no coin legs, empty commitment is the honest zero
		// declaration (D-065)
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

func erc7683SvcLeg(units int64) veiltypes.IntentLeg {
	return veiltypes.IntentLeg{Asset: &veiltypes.IntentLeg_Svc{
		Svc: &veiltypes.ServiceVoucher{Schema: "cmpt", Units: math.NewInt(units)}}}
}

// activeIDBySigil returns the unique in-flight intent for a sigil — callers
// sequence submissions so exactly one is live per sigil at a time. A submit
// landing exactly on a session-boundary height is already
// SEALED_IN_SESSION by the time the block commits (the EndBlock seal sweep
// runs in the same block), so any pre-terminal state counts.
func (f *erc7683Fixture) activeIDBySigil(sigil string) string {
	f.t.Helper()
	ctx := f.app.NewContext(true)
	var id string
	require.NoError(f.t, f.app.VeilKeeper.IterateIntentRecords(ctx, func(rec veiltypes.IntentRecord) bool {
		switch rec.State {
		case veiltypes.INTENT_STATE_SUBMITTED, veiltypes.INTENT_STATE_SEALED_IN_SESSION,
			veiltypes.INTENT_STATE_OPEN, veiltypes.INTENT_STATE_ROLLED:
			if rec.Intent.SenderSigil == sigil {
				require.Empty(f.t, id, "more than one in-flight intent for sigil %s", sigil)
				id = rec.Intent.Id
			}
		}
		return false
	}))
	require.NotEmpty(f.t, id, "no in-flight intent for sigil %s", sigil)
	return id
}

func (f *erc7683Fixture) intentState(id string) veiltypes.IntentState {
	f.t.Helper()
	ctx := f.app.NewContext(true)
	rec, err := f.app.VeilKeeper.GetIntentRecord(ctx, 0, id)
	require.NoError(f.t, err)
	return rec.State
}

// advanceUntil drives blocks until the intent reaches `want` (bounded) —
// the in-process analog of the live sweep's driveToState.
func (f *erc7683Fixture) advanceUntil(id string, want veiltypes.IntentState, maxBlocks int) {
	f.t.Helper()
	for i := 0; i < maxBlocks; i++ {
		if f.intentState(id) == want {
			return
		}
		f.advance(1)
	}
	if f.intentState(id) != want {
		ctx := f.app.NewContext(true)
		rec, err := f.app.VeilKeeper.GetIntentRecord(ctx, 0, id)
		require.NoError(f.t, err)
		f.t.Logf("DIAG intent %s: state=%s session=%d height=%d", id, rec.State, rec.SessionId, f.app.LastBlockHeight())
		sq := sessionkeeper.NewQueryServerImpl(f.app.SessionKeeper)
		if sres, e := sq.Session(ctx, &sessiontypes.QuerySessionRequest{Id: rec.SessionId}); e == nil {
			s := sres.Session
			f.t.Logf("DIAG session %d: state=%s start=%d auctionEnd=%d intents=%d winner=%q",
				s.Id, s.State, s.StartHeight, s.AuctionEndHeight, len(s.IntentIds), s.WinningSolutionId)
		} else {
			f.t.Logf("DIAG session %d: query error %v", rec.SessionId, e)
		}
	}
	require.Equal(f.t, want, f.intentState(id),
		"intent %s did not reach %s within %d blocks", id, want, maxBlocks)
}

func (f *erc7683Fixture) balance(addr sdk.AccAddress, denom string) math.Int {
	ctx := f.app.NewContext(true)
	return f.app.BankKeeper.GetBalance(ctx, addr, denom).Amount
}

// --- the fill-back evidence bundle and the bridge-side verifier ---

// erc7683Order is the minimal origin-chain order shape the bridge checks a
// fill against: deliver ≥ minReceived of denom to recipientSigil on
// Conclave, unconditioned. (Real 7683 resolved orders carry more; these are
// the fields the RECEIPT evidence can and must satisfy.)
type erc7683Order struct {
	id             string
	recipientSigil string
	denom          string
	minReceived    math.Int
}

// erc7683Evidence is what the resolver ships to the origin chain: the
// ReceiptProof response (receipt + ICS-23 proof + height) and the winning
// Match bytes (the terms_hash pre-image, fetched via session Solutions).
type erc7683Evidence struct {
	receipt     receipttypes.Receipt
	proof       []byte
	proofHeight uint64
	matchBz     []byte
}

// erc7683BridgeOpts are the WATCHED-RED switches: each skip demonstrates,
// permanently and in-tree, that the corresponding check is load-bearing —
// i.e. that the ICS-23 proof ALONE does not provide the property
// (7683_SURFACE.md §6 "what the proof does NOT prove").
// There is deliberately NO skipOutcome switch for check 3(b). Every fill in
// THIS suite is unconditioned, so its receipts read NONE and a skip variant
// here would demonstrate nothing — an option no case can exercise is exactly
// the decoration Rule 4 rejects. 3(b)'s load-bearing demonstration lives where
// a conditioned settlement exists: receipt_condition_outcome_test.go runs
// check 3 MINUS 3(b) (i.e. fillback.PaysOrder alone, which is what the pre-2.9
// rule reduced to) across PENDING, DISPUTED and REFUNDED and shows it accepts
// all three.
type erc7683BridgeOpts struct {
	skipLegCheck bool // drop §6 check 3(a) (order↔fill binding)
	skipDedup    bool // drop §6 check 4 (one-claim-per-receipt)
}

// erc7683BridgeVerify implements the 7683_SURFACE.md §6 mandated
// origin-settler verification, exactly and in order:
//  1. the ICS-23 proof verifies the receipt bytes under the 0x08 key
//     against the trusted Conclave app root at proofHeight;
//  2. sha256(matchBz) == receipt.terms_hash and the receipt's intent is in
//     the match (the pre-image really is this settlement's terms);
//  3. (a) some match leg pays order.recipientSigil ≥ order.minReceived of
//     order.denom, AND (b) the receipt's condition_outcome admits the fill
//     (NONE or RELEASED — the funds were never escrowed, or the escrow paid
//     the payee). Both conjuncts come from package fillback, the ONE
//     implementation of this rule (task 2.9, finding F-16);
//     ~~UNCONDITIONED (condition == nil — the D-087 rule)~~ — that clause was
//     INERT and is retired: a conditioned match's coin legs necessarily carry
//     a nil condition, so it could never reject the escrowed payment it was
//     written to reject. 7683_SURFACE §6.3/§6.4 carry the full record;
//  4. receipt_id has not been claimed before (replay defense — a bridge
//     obligation, not a node property).
func erc7683BridgeVerify(
	order erc7683Order, root []byte, ev erc7683Evidence,
	storeKey func(id string) ([]byte, error),
	claimed map[string]bool, opts erc7683BridgeOpts,
) error {
	// (1) proof → receipt bytes at the id-derived key
	key, err := storeKey(ev.receipt.Id)
	if err != nil {
		return err
	}
	value, err := ev.receipt.Marshal()
	if err != nil {
		return err
	}
	var proofOps cmtprotocrypto.ProofOps
	if err := proofOps.Unmarshal(ev.proof); err != nil {
		return err
	}
	merkleProof, err := commitmenttypes.ConvertProofs(&proofOps)
	if err != nil {
		return err
	}
	if err := merkleProof.VerifyMembership(
		commitmenttypes.GetSDKSpecs(),
		commitmenttypes.NewMerkleRoot(root),
		commitmenttypes.NewMerklePath(sessiontypes.StoreKey, string(key)),
		value,
	); err != nil {
		return fmt.Errorf("ics23: %w", err)
	}

	// (2) terms pre-image
	sum := sha256.Sum256(ev.matchBz)
	if string(sum[:]) != string(ev.receipt.TermsHash) {
		return fmt.Errorf("terms_hash mismatch")
	}
	var match sessiontypes.Match
	if err := match.Unmarshal(ev.matchBz); err != nil {
		return err
	}
	inMatch := false
	for _, iid := range match.IntentIds {
		if iid == ev.receipt.IntentId {
			inMatch = true
			break
		}
	}
	if !inMatch {
		return fmt.Errorf("receipt intent %s not in match", ev.receipt.IntentId)
	}

	// (3) order↔fill binding — nothing on Conclave binds an intent to an
	// origin-chain order (7683_SURFACE.md §1.3); THIS check is the binding.
	// Both conjuncts route through package fillback so this suite and the §6
	// exit demo cannot state the rule differently — which they did, until task
	// 2.9 (finding F-17).
	fbOrder := fillback.Order{
		ID: order.id, RecipientSigil: order.recipientSigil,
		Denom: order.denom, MinReceived: order.minReceived,
	}
	if !opts.skipLegCheck {
		if err := fillback.PaysOrder(match, fbOrder); err != nil {
			return err
		}
	}
	if err := fillback.OutcomeAdmits(ev.receipt); err != nil {
		return err
	}

	// (4) one-claim-per-receipt — the ONLY replay defense.
	if !opts.skipDedup {
		if claimed[ev.receipt.Id] {
			return fmt.Errorf("receipt %s already claimed", ev.receipt.Id)
		}
		claimed[ev.receipt.Id] = true
	}
	return nil
}

// settleFill submits the fill/counterpart pair in one block, drives the
// session lifecycle to CONCLUDED, and returns (fillIntentID, sessionID).
func (f *erc7683Fixture) settleFill(resolverSigil, counterSigil string, counterPriv cryptotypes.PrivKey, counterAddr sdk.AccAddress, usdcAmt, units int64) (string, uint64) {
	f.t.Helper()
	results := f.deliverAll(
		[]cryptotypes.PrivKey{f.resolverHotPriv, counterPriv},
		[]sdk.Msg{
			f.fillMsg(f.resolverHot, resolverSigil, usdcAmt, usdcAmt, units),
			f.counterpartMsg(counterAddr, counterSigil, units, usdcAmt),
		},
	)
	require.Equal(f.t, uint32(0), results[0].Code, "fill submit: %s", results[0].Log)
	require.Equal(f.t, uint32(0), results[1].Code, "counterpart submit: %s", results[1].Log)

	fillID := f.activeIDBySigil(resolverSigil)
	counterID := f.activeIDBySigil(counterSigil)
	f.advanceUntil(fillID, veiltypes.INTENT_STATE_CONCLUDED, 40)
	require.Equal(f.t, veiltypes.INTENT_STATE_CONCLUDED, f.intentState(counterID))

	ctx := f.app.NewContext(true)
	rec, err := f.app.VeilKeeper.GetIntentRecord(ctx, 0, fillID)
	require.NoError(f.t, err)
	counterRec, err := f.app.VeilKeeper.GetIntentRecord(ctx, 0, counterID)
	require.NoError(f.t, err)
	require.Equal(f.t, rec.SessionId, counterRec.SessionId,
		"fill and counterpart must have sealed into one session")
	return fillID, rec.SessionId
}

// gatherEvidence assembles the §6 evidence bundle for a settled fill the
// way the resolver repo does: Receipts(sigil) → the fill's receipt →
// ReceiptProof, then Session(winning_solution_id) → Solutions → the winning
// match containing the intent → deterministic Match bytes.
func (f *erc7683Fixture) gatherEvidence(resolverSigil, fillID string, sessionID uint64) erc7683Evidence {
	f.t.Helper()
	ctx := f.app.NewContext(true)
	rq := receiptkeeper.NewQueryServerImpl(f.app.ReceiptKeeper)
	sq := sessionkeeper.NewQueryServerImpl(f.app.SessionKeeper)

	list, err := rq.Receipts(ctx, &receipttypes.QueryReceiptsRequest{SigilId: resolverSigil})
	require.NoError(f.t, err)
	var receiptID string
	for _, r := range list.Receipts {
		if r.IntentId == fillID {
			receiptID = r.Id
			break
		}
	}
	require.NotEmpty(f.t, receiptID, "no receipt for fill %s", fillID)

	proofResp, err := rq.ReceiptProof(ctx, &receipttypes.QueryReceiptProofRequest{Id: receiptID})
	require.NoError(f.t, err)
	require.NotEmpty(f.t, proofResp.Proof)
	// A height-0 prove query is served at latest−1 — the newest version whose
	// app hash is already anchored in an EXISTING block header (ProofHeight
	// H's root is header H+1's AppHash). The bridge verifies against that
	// root; the contract documents this (7683_SURFACE.md §6).
	require.Equal(f.t, uint64(f.app.LastCommitID().Version)-1, proofResp.ProofHeight,
		"ReceiptProof at the tip proves at the last header-anchored version")

	sres, err := sq.Session(ctx, &sessiontypes.QuerySessionRequest{Id: sessionID})
	require.NoError(f.t, err)
	require.NotEmpty(f.t, sres.Session.WinningSolutionId, "settled session records its winning solution")
	sols, err := sq.Solutions(ctx, &sessiontypes.QuerySolutionsRequest{SessionId: sessionID})
	require.NoError(f.t, err)
	var matchBz []byte
	for i := range sols.Solutions {
		if sols.Solutions[i].Id != sres.Session.WinningSolutionId {
			continue
		}
		for j := range sols.Solutions[i].Matches {
			m := &sols.Solutions[i].Matches[j]
			for _, iid := range m.IntentIds {
				if iid == fillID {
					bz, err := m.Marshal()
					require.NoError(f.t, err)
					matchBz = bz
				}
			}
		}
	}
	require.NotEmpty(f.t, matchBz, "winning match containing the fill must be queryable (terms pre-image)")

	return erc7683Evidence{
		receipt:     proofResp.Receipt,
		proof:       proofResp.Proof,
		proofHeight: proofResp.ProofHeight,
		matchBz:     matchBz,
	}
}

// TestERC7683SurfaceConformance is the load-bearing proof behind
// docs/edge/7683_SURFACE.md §6/§7 (task 2.4, D-086): from a resolver-shaped
// fixture it proves the fill-back evidence chain end to end, and then — the
// contract's honesty core — that each "does NOT prove" claim is real: the
// ICS-23 proof alone binds neither order↔fill nor first-claim; those are
// the mandated bridge checks, shown load-bearing by their skip variants.
func TestERC7683SurfaceConformance(t *testing.T) {
	f := newERC7683Fixture(t)

	resolverSigil := f.registerSigil(f.resolverOpPriv, "erc7683-resolver-op")
	sigilA := f.registerSigil(f.userAPriv, "erc7683-user-a")
	sigilB := f.registerSigil(f.userBPriv, "erc7683-user-b")

	res := f.deliver(f.resolverOpPriv, &mandatetypes.MsgIssueMandate{
		Controller: f.resolverOp.String(),
		Terms:      f.resolverTerms(resolverSigil),
	})
	require.Equal(t, uint32(0), res.Code, "issue resolver mandate: %s", res.Log)

	// --- settlement 1: the genuine fill for order A (100 uusdc → sigilA) ---
	fill1, session1 := f.settleFill(resolverSigil, sigilA, f.userAPriv, f.userA, 100_000_000, 10)

	// --- settlement 2: an ORDER-LESS mint — the honest §1.3 non-binding
	// pin: nothing on Conclave distinguishes a fill backed by a real
	// origin-chain order from one the resolver invented; it settles, bounded
	// only by the resolver's mandate and funds. If this assert ever fails,
	// an order-binding was added and 7683_SURFACE.md §1.3 must be rewritten.
	// (8/4 keeps the fallback price terminating — the matcher never rounds.)
	fill2, session2 := f.settleFill(resolverSigil, sigilB, f.userBPriv, f.userB, 8_000_000, 4)

	// a receipt written at height H is provable once header H+1 exists (a
	// tip query proves at latest−1) — one block after the settlement, which
	// on a live chain has always already passed. In-process, advance it.
	f.advance(1)
	ev1 := f.gatherEvidence(resolverSigil, fill1, session1)
	ev2 := f.gatherEvidence(resolverSigil, fill2, session2)
	require.Equal(t, ev1.proofHeight, ev2.proofHeight, "both gathered at one committed tip")
	// the root a bridge verifies against is the app hash AT ProofHeight
	// (header ProofHeight+1's AppHash — here, the recorded commit root).
	root := f.roots[ev1.proofHeight]
	require.NotNil(t, root)
	storeKey := f.app.SessionKeeper.ReceiptByIDStoreKey

	orderA := erc7683Order{id: "order-A", recipientSigil: sigilA,
		denom: app.MockUSDCDenom, minReceived: math.NewInt(100_000_000)}
	// order A′: same terms as A from a second, distinct origin order — the
	// replay target.
	orderAPrime := erc7683Order{id: "order-A-prime", recipientSigil: sigilA,
		denom: app.MockUSDCDenom, minReceived: math.NewInt(100_000_000)}

	// --- §6 positive: the genuine claim verifies through all four checks ---
	claimed := map[string]bool{}
	require.NoError(t,
		erc7683BridgeVerify(orderA, root, ev1, storeKey, claimed, erc7683BridgeOpts{}),
		"genuine fill evidence must verify")

	// --- "does NOT prove" #1: order↔fill binding lives in the LEG CHECK,
	// not the proof. Settlement 2's evidence is cryptographically genuine
	// (its proof verifies) but pays the wrong party the wrong amount; the
	// leg check rejects it — and WITHOUT the leg check it is accepted, which
	// is the in-tree red proving the check is load-bearing. ---
	require.ErrorContains(t,
		erc7683BridgeVerify(orderA, root, ev2, storeKey, map[string]bool{}, erc7683BridgeOpts{}),
		"no leg pays the order's recipient its minimum",
		"wrong settlement's evidence must fail the LEG CHECK specifically (its proof is genuine)")
	require.NoError(t,
		erc7683BridgeVerify(orderA, root, ev2, storeKey, map[string]bool{}, erc7683BridgeOpts{skipLegCheck: true}),
		"WITHOUT the leg check the wrong settlement is accepted — the ICS-23 proof alone does not bind order↔fill")

	// --- "does NOT prove" #2: replay defense is the DEDUP, nothing else.
	// The same genuine evidence presented for order A′ passes every
	// cryptographic check; only the claimed-set rejects it. ---
	require.ErrorContains(t,
		erc7683BridgeVerify(orderAPrime, root, ev1, storeKey, claimed, erc7683BridgeOpts{}),
		"already claimed",
		"replaying claimed evidence against a second order must fail DEDUP specifically")
	require.NoError(t,
		erc7683BridgeVerify(orderAPrime, root, ev1, storeKey, claimed, erc7683BridgeOpts{skipDedup: true}),
		"WITHOUT dedup the replay is accepted — the proof alone does not prevent cross-order reuse")

	// --- proof still height-bound (D-053, re-checked at the app level):
	// the same evidence fails against a LATER root. ---
	f.advance(1)
	staleRoot := f.app.LastCommitID().Hash
	require.NotEqual(t, root, staleRoot)
	require.ErrorContains(t,
		erc7683BridgeVerify(orderA, staleRoot, ev1, storeKey, map[string]bool{}, erc7683BridgeOpts{}),
		"ics23",
		"evidence bound to ProofHeight must fail the PROOF check against a later root")

	// --- ante negatives from the resolver fixture (§1.1 chokepoints; the
	// full matrix is 2.3's suite — these re-assert the two the resolver
	// contract leans on) ---
	over := f.fillMsg(f.resolverHot, resolverSigil, 600_000_000, 600_000_000, 10)
	res = f.deliver(f.resolverHotPriv, over)
	require.Equal(t, mandatetypes.ErrMandateExceeded.ABCICode(), res.Code,
		"over-ceiling fill must be rejected in the ante: %s", res.Log)

	foreign := f.fillMsg(f.resolverHot, sigilA, 1_000_000, 1_000_000, 1)
	res = f.deliver(f.resolverHotPriv, foreign)
	require.Equal(t, veiltypes.ErrSigilMismatch.ABCICode(), res.Code,
		"resolver key claiming a foreign sigil must be ErrSigilMismatch: %s", res.Log)

	rogueMsg := f.fillMsg(f.rogue, resolverSigil, 1_000_000, 1_000_000, 1)
	res = f.deliver(f.roguePriv, rogueMsg)
	require.Equal(t, veiltypes.ErrSigilMismatch.ABCICode(), res.Code,
		"unbound key claiming the resolver sigil must be ErrSigilMismatch: %s", res.Log)
}

// TestERC7683UnderDeclaredFillNeverSettles pins the D-065 enforcement
// boundary from the resolver fixture (7683_SURFACE.md §3: "a resolver CAN
// under-declare; an under-declared fill can NEVER settle"): a fill whose
// actual coin give (100 uusdc) exceeds its declared spend_commitment
// (10 uusdc) passes the ante — the ante never reads give legs, stub or
// encrypted (D-065) — and is then turned INVALID at the Reveal
// (lifecycle.go giveExceedsCommitment), 50% bond slashed, excluded from
// matching, no receipt ever written, and its mandate window charge is
// RETAINED (the conservation posture). Watched RED with the Reveal
// cross-check bypassed (task 2.4 record, D-086).
func TestERC7683UnderDeclaredFillNeverSettles(t *testing.T) {
	f := newERC7683Fixture(t)

	resolverSigil := f.registerSigil(f.resolverOpPriv, "erc7683-resolver-op")
	sigilA := f.registerSigil(f.userAPriv, "erc7683-user-a")

	res := f.deliver(f.resolverOpPriv, &mandatetypes.MsgIssueMandate{
		Controller: f.resolverOp.String(),
		Terms:      f.resolverTerms(resolverSigil),
	})
	require.Equal(t, uint32(0), res.Code, "issue resolver mandate: %s", res.Log)

	bondBefore := f.balance(f.resolverHot, app.BaseDenom)

	// give 100 uusdc, declare 10 uusdc — plus an honest counterpart that
	// WOULD match if the under-declared fill reached the auction.
	results := f.deliverAll(
		[]cryptotypes.PrivKey{f.resolverHotPriv, f.userAPriv},
		[]sdk.Msg{
			f.fillMsg(f.resolverHot, resolverSigil, 100_000_000, 10_000_000, 10),
			f.counterpartMsg(f.userA, sigilA, 10, 100_000_000),
		},
	)
	// the CAN half: the under-declared submit passes signature, sigil,
	// prevalidation (D-085) and mandate ante (charged 10 uusdc + 1 uclv).
	require.Equal(t, uint32(0), results[0].Code,
		"under-declared submit must pass the ante (D-065 uniform valuation): %s", results[0].Log)
	require.Equal(t, uint32(0), results[1].Code)
	fillID := f.activeIDBySigil(resolverSigil)

	// the CANNOT half: INVALID at the Reveal, before matching.
	f.advanceUntil(fillID, veiltypes.INTENT_STATE_INVALID, 10)

	// 50% bond slash (InvalidSlashBps 5000): 1 CLV escrowed, 0.5 CLV back.
	bondAfter := f.balance(f.resolverHot, app.BaseDenom)
	require.Equal(t, math.NewInt(500_000), bondBefore.Sub(bondAfter),
		"net uclv cost must be exactly the 50%% bond slash")

	// no receipt exists for the dead fill — it never reached settlement.
	ctx := f.app.NewContext(true)
	rq := receiptkeeper.NewQueryServerImpl(f.app.ReceiptKeeper)
	list, err := rq.Receipts(ctx, &receipttypes.QueryReceiptsRequest{SigilId: resolverSigil})
	require.NoError(t, err)
	require.Empty(t, list.Receipts, "an under-declared fill must never produce settlement evidence")

	// the window charge is RETAINED (charged commitment+bond stay bound to
	// the mandate — under-declaration is self-punishing, not an escape).
	m, found, err := f.app.MandateKeeper.ActiveMandateByGrantee(ctx, 0, f.resolverHot)
	require.NoError(t, err)
	require.True(t, found)
	retained := sdk.NewCoins(
		sdk.NewCoin(app.MockUSDCDenom, math.NewInt(10_000_000)),
		sdk.NewCoin(app.BaseDenom, math.NewInt(1_000_000)),
	)
	require.True(t, m.OutstandingCommitment.Equal(retained),
		"INVALID retains its charged commitment+bond: have %s want %s", m.OutstandingCommitment, retained)
}
