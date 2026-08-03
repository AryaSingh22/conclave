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
	"github.com/AryaSingh22/conclave/solver"
	"github.com/AryaSingh22/conclave/solver/match"
	escrowtypes "github.com/AryaSingh22/conclave/x/escrow/types"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
	sigiltypes "github.com/AryaSingh22/conclave/x/sigil/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// This file is task 2.14's ACCEPTANCE gate (D-097): the in-tree reference
// solver, unmodified, driving a real ConclaveApp through real
// FinalizeBlock+Commit blocks — no hand-built keeper Matches anywhere, which is
// exactly what 2.14's acceptance criterion asks for. Its sibling
// solver/condition_attachment_test.go proves the attachment RULES; this proves
// the reference actually clears a conditioned settlement on the chain's own
// terms, and that it is not slashed for trying.
//
// WHY THE ASSERTIONS ARE ABOUT THE SOLVER'S SOLUTION AND NOT ABOUT THE ESCROW.
// Since task 2.7 the in-consensus FALLBACK attaches conditions too
// (attachFallbackCondition, D-094). So a test that asserted only "a conditioned
// escrow exists" would pass with the defect fully present: the solver's
// downgrade solution is rejected 1429, zero valid solutions remain, the session
// drops to FALLBACK, and the fallback settles it correctly anyway. The test
// would be green, the reference solver would still be broken, and nothing would
// say so. The load-bearing observables are therefore the PROPOSAL's fate — tx
// code, the solver's bond, its slashed_count, and whose solution won.

const solverConfChainID = "conclave-dev-1"

type solverFixture struct {
	t   *testing.T
	app *app.ConclaveApp

	sellerPriv, buyerPriv, solverPriv cryptotypes.PrivKey
	seller, buyer, solverAddr         sdk.AccAddress
}

func newSolverFixture(t *testing.T) *solverFixture {
	t.Helper()
	f := &solverFixture{t: t}
	f.app = app.NewConclaveApp(
		log.NewNopLogger(), dbm.NewMemDB(), nil, true, sims.EmptyAppOptions{},
		baseapp.SetChainID(solverConfChainID),
	)

	f.sellerPriv = secp256k1.GenPrivKey()
	f.buyerPriv = secp256k1.GenPrivKey()
	f.solverPriv = secp256k1.GenPrivKey()
	f.seller = sdk.AccAddress(f.sellerPriv.PubKey().Address())
	f.buyer = sdk.AccAddress(f.buyerPriv.PubKey().Address())
	f.solverAddr = sdk.AccAddress(f.solverPriv.PubKey().Address())

	funds := sdk.NewCoins(
		sdk.NewCoin(app.BaseDenom, math.NewInt(10_000_000_000)),
		sdk.NewCoin(app.MockUSDCDenom, math.NewInt(10_000_000_000)),
	)
	genAccs := []authtypes.GenesisAccount{
		authtypes.NewBaseAccount(f.seller, f.sellerPriv.PubKey(), 0, 0),
		authtypes.NewBaseAccount(f.buyer, f.buyerPriv.PubKey(), 0, 0),
		authtypes.NewBaseAccount(f.solverAddr, f.solverPriv.PubKey(), 0, 0),
	}
	balances := []banktypes.Balance{
		{Address: f.seller.String(), Coins: funds},
		{Address: f.buyer.String(), Coins: funds},
		{Address: f.solverAddr.String(), Coins: funds},
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
		ChainId:         solverConfChainID,
		Validators:      []abci.ValidatorUpdate{},
		ConsensusParams: sims.DefaultConsensusParams,
		AppStateBytes:   stateBytes,
	})
	require.NoError(t, err)
	f.advance(1)
	return f
}

func (f *solverFixture) advance(n int) {
	f.t.Helper()
	for i := 0; i < n; i++ {
		_, err := f.app.FinalizeBlock(&abci.RequestFinalizeBlock{
			Height: f.app.LastBlockHeight() + 1,
			Hash:   f.app.LastCommitID().Hash,
		})
		require.NoError(f.t, err)
		_, err = f.app.Commit()
		require.NoError(f.t, err)
	}
}

func (f *solverFixture) deliverAll(privs []cryptotypes.PrivKey, msgs []sdk.Msg) []*abci.ExecTxResult {
	f.t.Helper()
	ctx := f.app.NewContext(true)
	txs := make([][]byte, len(msgs))
	for i := range msgs {
		addr := sdk.AccAddress(privs[i].PubKey().Address())
		acc := f.app.AccountKeeper.GetAccount(ctx, addr)
		require.NotNil(f.t, acc)
		txn, err := sims.GenSignedMockTx(
			rand.New(rand.NewSource(42)), f.app.TxConfig(), []sdk.Msg{msgs[i]},
			sdk.Coins{}, sims.DefaultGenTxGas, solverConfChainID,
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
	return res.TxResults
}

func (f *solverFixture) deliver(priv cryptotypes.PrivKey, msg sdk.Msg) *abci.ExecTxResult {
	f.t.Helper()
	return f.deliverAll([]cryptotypes.PrivKey{priv}, []sdk.Msg{msg})[0]
}

// deliverSameSigner puts several txs from ONE signer into ONE block, at
// consecutive sequences. Needed whenever two txs must land inside the same
// short window (the solution window is only SolutionWindowBlocks wide, so
// one-tx-per-block would close the auction between them).
func (f *solverFixture) deliverSameSigner(priv cryptotypes.PrivKey, msgs []sdk.Msg) []*abci.ExecTxResult {
	f.t.Helper()
	ctx := f.app.NewContext(true)
	acc := f.app.AccountKeeper.GetAccount(ctx, sdk.AccAddress(priv.PubKey().Address()))
	require.NotNil(f.t, acc)
	txs := make([][]byte, len(msgs))
	for i := range msgs {
		txn, err := sims.GenSignedMockTx(
			rand.New(rand.NewSource(42)), f.app.TxConfig(), []sdk.Msg{msgs[i]},
			sdk.Coins{}, sims.DefaultGenTxGas, solverConfChainID,
			[]uint64{acc.GetAccountNumber()}, []uint64{acc.GetSequence() + uint64(i)}, priv,
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
	require.Len(f.t, res.TxResults, len(msgs))
	return res.TxResults
}

func (f *solverFixture) registerSigil(priv cryptotypes.PrivKey, did string) string {
	f.t.Helper()
	addr := sdk.AccAddress(priv.PubKey().Address())
	h := sha256.Sum256([]byte(did))
	res := f.deliver(priv, &sigiltypes.MsgRegisterSigil{
		Controller: addr.String(), DidDocHash: h[:], MetadataUri: "ipfs://" + did,
	})
	require.Equal(f.t, uint32(0), res.Code, "register sigil %s: %s", did, res.Log)
	ctx := f.app.NewContext(true)
	id, err := f.app.SigilKeeper.SigilIDByAddress(ctx, 0, addr)
	require.NoError(f.t, err)
	return id
}

// conditionedIntents builds the D-074 bilateral pair, BOTH parties signing the
// same delivery condition at payload level: the seller gives the dataset for
// coin, the buyer gives the coin for the dataset.
func (f *solverFixture) conditionedIntents(sellerSigil, buyerSigil string,
	cond *escrowtypes.DeliveryCondition, price int64) []sdk.Msg {
	f.t.Helper()
	h := sha256.Sum256([]byte(solverConfDataset))
	pay := sdk.NewCoin(app.MockUSDCDenom, math.NewInt(price))
	ttl := uint64(f.app.LastBlockHeight()) + 1_000

	sellerPayload, err := (&veiltypes.IntentPayload{
		Give:      []veiltypes.IntentLeg{{Asset: &veiltypes.IntentLeg_Data{Data: &veiltypes.DataGrant{ContentHash: h[:]}}}},
		Want:      []veiltypes.IntentLeg{{Asset: &veiltypes.IntentLeg_Coin{Coin: &pay}}},
		Condition: cond,
	}).Marshal()
	require.NoError(f.t, err)
	sellerEnv, err := (&veiltypes.IntentEnvelope{TtlHeight: ttl}).Marshal()
	require.NoError(f.t, err)

	buyerPayload, err := (&veiltypes.IntentPayload{
		Give:      []veiltypes.IntentLeg{{Asset: &veiltypes.IntentLeg_Coin{Coin: &pay}}},
		Want:      []veiltypes.IntentLeg{{Asset: &veiltypes.IntentLeg_Data{Data: &veiltypes.DataGrant{ContentHash: h[:]}}}},
		Condition: cond,
	}).Marshal()
	require.NoError(f.t, err)
	buyerEnv, err := (&veiltypes.IntentEnvelope{
		TtlHeight: ttl, SpendCommitment: sdk.NewCoins(pay),
	}).Marshal()
	require.NoError(f.t, err)

	bond := sdk.NewCoin(app.BaseDenom, math.NewInt(1_000_000))
	return []sdk.Msg{
		&veiltypes.MsgSubmitIntent{
			Sender: f.seller.String(), SenderSigil: sellerSigil,
			Ciphertext: sellerPayload, Envelope: sellerEnv, Bond: bond,
		},
		&veiltypes.MsgSubmitIntent{
			Sender: f.buyer.String(), SenderSigil: buyerSigil,
			Ciphertext: buyerPayload, Envelope: buyerEnv, Bond: bond,
		},
	}
}

const solverConfDataset = "solver-conformance-dataset"

// openIntentsInSession reads the session's OPEN intents through exactly the
// decoding path cmd/conclave-solver uses (match.DecodeOpenIntent over the veil
// records), so what the reference solver sees here is what it sees on a node.
func (f *solverFixture) openIntentsInSession(sessionID uint64) []match.OpenIntent {
	f.t.Helper()
	ctx := f.app.NewContext(true)
	s, err := f.app.SessionKeeper.GetSession(ctx, 0, sessionID)
	require.NoError(f.t, err)
	in := make(map[string]bool, len(s.IntentIds))
	for _, id := range s.IntentIds {
		in[id] = true
	}
	var out []match.OpenIntent
	require.NoError(f.t, f.app.VeilKeeper.IterateIntentRecords(ctx, func(rec veiltypes.IntentRecord) bool {
		if rec.State == veiltypes.INTENT_STATE_OPEN && in[rec.Intent.Id] {
			if oi, ok := match.DecodeOpenIntent(rec); ok {
				out = append(out, oi)
			}
		}
		return false
	}))
	return out
}

// advanceToAuction runs blocks until some session is in AUCTION and returns it.
// The solution window is only SolutionWindowBlocks wide, so the caller proposes
// in the very next block.
func (f *solverFixture) advanceToAuction(maxBlocks int) sessiontypes.Session {
	f.t.Helper()
	for i := 0; i < maxBlocks; i++ {
		ctx := f.app.NewContext(true)
		var found sessiontypes.Session
		var ok bool
		require.NoError(f.t, f.app.SessionKeeper.IterateSessions(ctx, func(s sessiontypes.Session) bool {
			if s.State == sessiontypes.SESSION_STATE_AUCTION {
				found, ok = s, true
				return true
			}
			return false
		}))
		if ok {
			return found
		}
		f.advance(1)
	}
	f.t.Fatalf("no session reached AUCTION within %d blocks", maxBlocks)
	return sessiontypes.Session{}
}

func (f *solverFixture) solverState() sessiontypes.Solver {
	f.t.Helper()
	ctx := f.app.NewContext(true)
	s, err := f.app.SessionKeeper.GetSolver(ctx, 0, f.solverAddr)
	require.NoError(f.t, err)
	return s
}

func (f *solverFixture) escrows() []escrowtypes.Escrow {
	f.t.Helper()
	ctx := f.app.NewContext(true)
	var out []escrowtypes.Escrow
	require.NoError(f.t, f.app.EscrowKeeper.IterateEscrows(ctx, func(e escrowtypes.Escrow) bool {
		out = append(out, e)
		return false
	}))
	return out
}

// TestDroppingTheConditionIsRejectedAtTheWire pins the CONSEQUENCE the reference
// solver now avoids, permanently rather than only inside a sabotage run.
//
// It takes the reference solver's own match and strips the condition off the
// DataGrant leg — a faithful, minimal reproduction of exactly what non-test
// solver/ produced before task 2.14, when it read no Condition anywhere — then
// submits it. Every assertion is specific: the (codespace, code) PAIR that
// reaches the wire (2.13/D-093 made 1412 dispatchable at all) and the 1429
// message identifying the DOWNGRADE direction rather than the 1427 mirror.
//
// BOUNDARY PIN — THE SLASH DOES NOT PERSIST, AND THAT IS FINDING F-1 OF TASK
// 2.14, OWNED BY NEW TASK 2.17. SPEC §6.4 and D-024 price an invalid solution
// at a 5% bond slash, and x/session/keeper/solution.go:114 does call
// slashSolver — but it then RETURNS AN ERROR, so baseapp discards the failing
// message's cache context and the slash write goes with it. Through the msg
// server an invalid solution therefore costs the solver GAS ONLY. Verified on
// both tx paths: invalid-solution (1412) here, and equivocation (1410,
// solution.go:97, nominally 20%) by the same shape.
//
// Every in-tree slash assertion is green because they all call
// k.ProposeSolution on a RAW keeper context with no tx cache-wrap
// (x/session/keeper/solution_test.go:53), where the write does persist — the
// D-093 shape exactly: one path verified, the sibling never enumerated
// (METHODOLOGY Rule 4 step 5).
//
// The assertions below pin the CURRENT behaviour so the finding cannot be lost.
// WHEN TASK 2.17 FIXES IT, THIS BLOCK MUST GO RED — replace it with the slash
// assertions (SlashedCount 1, bond 95_000_000), do not relax it.
func TestDroppingTheConditionIsRejectedAtTheWire(t *testing.T) {
	f := newSolverFixture(t)
	sellerSigil := f.registerSigil(f.sellerPriv, "solver-conf-seller")
	buyerSigil := f.registerSigil(f.buyerPriv, "solver-conf-buyer")

	bond := sdk.NewCoins(sdk.NewCoin(app.BaseDenom, math.NewInt(100_000_000)))
	require.Equal(t, uint32(0), f.deliver(f.solverPriv, &sessiontypes.MsgRegisterSolver{
		Solver: f.solverAddr.String(), Bond: bond,
	}).Code)

	cond := &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_Optimistic{
		Optimistic: &escrowtypes.OptimisticCondition{DisputeWindowBlocks: 50},
	}}
	results := f.deliverAll(
		[]cryptotypes.PrivKey{f.sellerPriv, f.buyerPriv},
		f.conditionedIntents(sellerSigil, buyerSigil, cond, 100),
	)
	require.Equal(t, uint32(0), results[0].Code)
	require.Equal(t, uint32(0), results[1].Code)

	session := f.advanceToAuction(40)
	matches := solver.Solve(f.openIntentsInSession(session.Id))
	require.Len(t, matches, 1)

	// strip the condition: the pre-2.14 reference solver's output, exactly.
	for li := range matches[0].Legs {
		if matches[0].Legs[li].GetData() != nil {
			matches[0].Legs[li].Condition = nil
		}
	}

	propose := f.deliver(f.solverPriv, &sessiontypes.MsgProposeSolution{
		Solver: f.solverAddr.String(), SessionId: session.Id, Matches: matches,
	})
	require.Equal(t, "session", propose.Codespace, "the registered codespace must reach the wire (D-093)")
	require.Equal(t, uint32(1412), propose.Code, "ErrInvalidSolution")
	require.Contains(t, propose.Log, "attaches none",
		"the 1429 DOWNGRADE message — not the 1427 mirror, which would mean a different bug")

	// THE BOUNDARY PIN (finding F-1 → task 2.17). Read the doc comment above
	// before changing either line.
	after := f.solverState()
	require.Equal(t, uint64(0), after.SlashedCount,
		"F-1/task-2.17: the 5% invalid-solution slash is REVERTED with the failing tx, so it never lands")
	require.Equal(t, math.NewInt(100_000_000), after.Bond.AmountOf(app.BaseDenom),
		"F-1/task-2.17: the bond is untouched — an invalid solution costs gas only, not the SPEC §6.4 5%")

	// and the session still settles — via the FALLBACK, which is precisely why
	// asserting on the escrow instead of on the proposal would have hidden all
	// of the above.
	f.advance(6)
	settled, err := f.app.SessionKeeper.GetSession(f.app.NewContext(true), 0, session.Id)
	require.NoError(t, err)
	require.NotEmpty(t, settled.WinningSolutionId)
	sols, err := f.app.SessionKeeper.SolutionsForSession(f.app.NewContext(true), session.Id)
	require.NoError(t, err)
	require.Len(t, sols, 1, "exactly one solution is on record")
	require.Empty(t, sols[0].Solver,
		"and it is the FALLBACK's synthetic Solution (D-086 writes it with an empty solver), "+
			"not the rejected proposal — the reference solver contributed nothing")
	require.NotEqual(t, f.solverAddr.String(), sols[0].Solver)
	escs := f.escrows()
	require.Len(t, escs, 1)
	require.Equal(t, uint64(50), escs[0].Condition.GetOptimistic().DisputeWindowBlocks,
		"the FALLBACK attached the condition (task 2.7) — a test asserting only this would be green with the defect present")
}

// TestEquivocationSlashAlsoDoesNotPersist is the SECOND half of finding F-1
// (task 2.17), pinned in-tree so a reader with only this repository can check
// it rather than take the claim on trust.
//
// Equivocation — two solutions from one solver for one session — is the harsher
// of the two penalties (20%, `equivocation_slash_bps`) and takes the identical
// shape at x/session/keeper/solution.go:97: slashSolver, then return an error.
// The slash is discarded with the failing message exactly as the 5% one is, so
// equivocation on the msg-server path also costs gas only.
//
// Both proposals go in ONE block at consecutive sequences. That detail is
// load-bearing: one-tx-per-block closes the auction between them and the second
// proposal fails 1409 (session closed) WITHOUT ever reaching the equivocation
// branch — which is what a first attempt at this test did, and it would have
// "passed" while proving nothing.
//
// WHEN TASK 2.17 FIXES THIS, THIS TEST MUST GO RED — replace it with the slash
// assertions (SlashedCount 1, bond 80_000_000), do not relax it.
func TestEquivocationSlashAlsoDoesNotPersist(t *testing.T) {
	f := newSolverFixture(t)
	sellerSigil := f.registerSigil(f.sellerPriv, "solver-conf-seller")
	buyerSigil := f.registerSigil(f.buyerPriv, "solver-conf-buyer")

	bond := sdk.NewCoins(sdk.NewCoin(app.BaseDenom, math.NewInt(100_000_000)))
	require.Equal(t, uint32(0), f.deliver(f.solverPriv, &sessiontypes.MsgRegisterSolver{
		Solver: f.solverAddr.String(), Bond: bond,
	}).Code)

	cond := &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_Optimistic{
		Optimistic: &escrowtypes.OptimisticCondition{DisputeWindowBlocks: 50},
	}}
	f.deliverAll(
		[]cryptotypes.PrivKey{f.sellerPriv, f.buyerPriv},
		f.conditionedIntents(sellerSigil, buyerSigil, cond, 100),
	)

	session := f.advanceToAuction(40)
	matches := solver.Solve(f.openIntentsInSession(session.Id))
	require.Len(t, matches, 1)

	msg := &sessiontypes.MsgProposeSolution{
		Solver: f.solverAddr.String(), SessionId: session.Id, Matches: matches,
	}
	res := f.deliverSameSigner(f.solverPriv, []sdk.Msg{msg, msg})
	require.Equal(t, uint32(0), res[0].Code, "the first proposal is valid: %s", res[0].Log)
	require.Equal(t, "session", res[1].Codespace)
	require.Equal(t, uint32(1410), res[1].Code, "ErrEquivocation — the branch really was reached")
	require.Contains(t, res[1].Log, "already proposed for this session")

	after := f.solverState()
	require.Equal(t, uint64(0), after.SlashedCount,
		"F-1/task-2.17: the 20% equivocation slash is REVERTED with the failing tx too")
	require.Equal(t, math.NewInt(100_000_000), after.Bond.AmountOf(app.BaseDenom),
		"F-1/task-2.17: bond untouched — the harsher penalty is lost by the same mechanism")
}

// TestReferenceSolverClearsAConditionedSettlement is task 2.14's acceptance.
//
// The reference solver — solver.Solve, unmodified, fed by the same
// match.DecodeOpenIntent path the daemon uses — proposes a solution for a pair
// that BOTH signed an OPTIMISTIC delivery condition, and the chain ACCEPTS it:
// code 0, no slash, and that solution is the one that settles. Before task 2.14
// this exact pair produced a solution with a bare DataGrant leg, which the chain
// rejects as ErrConditionNotHonored (session 1429) while slashing 5% of the
// solver's bond — the live functional defect this task exists to close.
func TestReferenceSolverClearsAConditionedSettlement(t *testing.T) {
	f := newSolverFixture(t)
	sellerSigil := f.registerSigil(f.sellerPriv, "solver-conf-seller")
	buyerSigil := f.registerSigil(f.buyerPriv, "solver-conf-buyer")

	bond := sdk.NewCoins(sdk.NewCoin(app.BaseDenom, math.NewInt(100_000_000)))
	res := f.deliver(f.solverPriv, &sessiontypes.MsgRegisterSolver{
		Solver: f.solverAddr.String(), Bond: bond,
	})
	require.Equal(t, uint32(0), res.Code, "register solver: %s", res.Log)
	bondBefore := f.solverState().Bond

	cond := &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_Optimistic{
		Optimistic: &escrowtypes.OptimisticCondition{DisputeWindowBlocks: 50},
	}}
	const price = 100
	results := f.deliverAll(
		[]cryptotypes.PrivKey{f.sellerPriv, f.buyerPriv},
		f.conditionedIntents(sellerSigil, buyerSigil, cond, price),
	)
	require.Equal(t, uint32(0), results[0].Code, "seller submit: %s", results[0].Log)
	require.Equal(t, uint32(0), results[1].Code, "buyer submit: %s", results[1].Log)

	session := f.advanceToAuction(40)
	open := f.openIntentsInSession(session.Id)
	require.Len(t, open, 2, "both conditioned intents must be OPEN in the auction session")

	// THE REFERENCE SOLVER, unmodified.
	matches := solver.Solve(open)
	require.Len(t, matches, 1, "the reference solver must clear the conditioned pair")

	var attached *escrowtypes.DeliveryCondition
	for _, leg := range matches[0].Legs {
		if leg.GetData() != nil {
			attached = leg.Condition
		}
	}
	require.NotNil(t, attached, "the proposed DataGrant leg must carry the signed condition")
	require.Equal(t, cond, attached)

	propose := f.deliver(f.solverPriv, &sessiontypes.MsgProposeSolution{
		Solver: f.solverAddr.String(), SessionId: session.Id, Matches: matches,
	})
	require.Equal(t, uint32(0), propose.Code,
		"the chain must ACCEPT the reference solver's conditioned solution; got codespace %q code %d: %s",
		propose.Codespace, propose.Code, propose.Log)

	// the solver was not slashed — the observable the defect would have moved.
	after := f.solverState()
	require.Equal(t, uint64(0), after.SlashedCount, "a valid solution must not slash")
	require.Equal(t, bondBefore.String(), after.Bond.String(), "bond untouched")

	ctx := f.app.NewContext(true)
	sols, err := f.app.SessionKeeper.SolutionsForSession(ctx, session.Id)
	require.NoError(t, err)
	require.Len(t, sols, 1, "exactly the reference solver's solution is on record")
	require.Equal(t, f.solverAddr.String(), sols[0].Solver)
	solutionID := sols[0].Id

	// let the auction close and settle.
	f.advance(6)
	settled, err := f.app.SessionKeeper.GetSession(f.app.NewContext(true), 0, session.Id)
	require.NoError(t, err)
	require.Equal(t, solutionID, settled.WinningSolutionId,
		"the SOLVER's solution must be the one that settled — not the in-consensus fallback, "+
			"which also attaches conditions since task 2.7 and would otherwise mask the defect entirely")

	escs := f.escrows()
	require.Len(t, escs, 1, "the conditioned DataGrant leg opened exactly one escrow")
	require.Equal(t, cond.GetOptimistic().DisputeWindowBlocks,
		escs[0].Condition.GetOptimistic().DisputeWindowBlocks,
		"and it carries the condition both parties signed")
	require.Equal(t, math.NewInt(price), escs[0].Amount.AmountOf(app.MockUSDCDenom),
		"funded with the buyer's payment — the protection the pre-2.7 downgrade removed")
}
