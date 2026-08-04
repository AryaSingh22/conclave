package app_test

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	abci "github.com/cometbft/cometbft/abci/types"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authz "github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/AryaSingh22/conclave/app"
	"github.com/AryaSingh22/conclave/solver"
	escrowtypes "github.com/AryaSingh22/conclave/x/escrow/types"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
)

// Task 2.17's gates beyond the two replaced boundary pins (which live in
// solver_conformance_test.go, where 2.14 wrote them).
//
// The pins prove the two PRICED rejections now cost bond. These prove the
// three things a fix that only satisfied the pins could still get wrong:
//   - an UNPRICED rejection must still cost nothing (the over-slash direction);
//   - the sole-message rule that makes the ante's verdict and the msg server's
//     verdict provably the same verdict;
//   - the THIRD slash site, which was never broken, still is not — verified
//     through a real block rather than assumed, because the claim "the SPEC
//     §6.4 slash does not land" was over-broad and correcting an over-claim in
//     reverse is its own way to be wrong.

// deliverMulti puts SEVERAL messages into ONE tx from one signer. Everything
// else in this package delivers one message per tx, which is now the only legal
// shape for a proposal — so this helper exists to build the ILLEGAL one.
func (f *solverFixture) deliverMulti(priv cryptotypes.PrivKey, msgs ...sdk.Msg) *abci.ExecTxResult {
	f.t.Helper()
	ctx := f.app.NewContext(true)
	acc := f.app.AccountKeeper.GetAccount(ctx, sdk.AccAddress(priv.PubKey().Address()))
	require.NotNil(f.t, acc)
	txn, err := sims.GenSignedMockTx(
		rand.New(rand.NewSource(42)), f.app.TxConfig(), msgs,
		sdk.Coins{}, sims.DefaultGenTxGas, solverConfChainID,
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
	return res.TxResults[0]
}

// deliverAllBlock is deliverAll that also returns the BLOCK-level events. An
// EndBlock slash belongs to no transaction, so deliverAll's []*ExecTxResult
// cannot show it — and the session concludes in the very block the proposal
// lands in, which is exactly where this task needs to look.
func (f *solverFixture) deliverAllBlock(privs []cryptotypes.PrivKey, msgs []sdk.Msg) (*abci.ResponseFinalizeBlock, error) {
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
	_, cErr := f.app.Commit()
	require.NoError(f.t, cErr)
	return res, nil
}

// slashReasonsIn extracts the reason attribute of every solver_slashed event —
// the only thing that says WHICH of x/session's three slash sites fired.
func slashReasonsIn(events []abci.Event) []string {
	var out []string
	for _, e := range events {
		if e.Type != sessiontypes.EventTypeSolverSlashed {
			continue
		}
		for _, a := range e.Attributes {
			if a.Key == sessiontypes.AttributeKeyReason {
				out = append(out, a.Value)
			}
		}
	}
	return out
}

// advanceCollectingSlashReasons is advance() that keeps the BLOCK-level events,
// which is where an EndBlock slash surfaces (it belongs to no tx).
func (f *solverFixture) advanceCollectingSlashReasons(n int) []string {
	f.t.Helper()
	var out []string
	for i := 0; i < n; i++ {
		res, err := f.app.FinalizeBlock(&abci.RequestFinalizeBlock{
			Height: f.app.LastBlockHeight() + 1,
			Hash:   f.app.LastCommitID().Hash,
		})
		require.NoError(f.t, err)
		out = append(out, slashReasonsIn(res.Events)...)
		for _, tx := range res.TxResults {
			out = append(out, slashReasonsIn(tx.Events)...)
		}
		_, err = f.app.Commit()
		require.NoError(f.t, err)
	}
	return out
}

// slashFixture is the shared setup of the tests below: a registered solver with
// a 100_000_000 bond, a conditioned bilateral pair both parties signed, and a
// live AUCTION session with the reference solver's match already computed.
type slashFixture struct {
	*solverFixture
	session sessiontypes.Session
	matches []sessiontypes.Match
}

func newSlashFixture(t *testing.T) *slashFixture {
	t.Helper()
	f := newSolverFixture(t)
	sellerSigil := f.registerSigil(f.sellerPriv, "slash-seller")
	buyerSigil := f.registerSigil(f.buyerPriv, "slash-buyer")

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
	return &slashFixture{solverFixture: f, session: session, matches: matches}
}

// TestUnpricedRejectionDoesNotSlash is the OVER-slash boundary, and it is the
// assertion that keeps the task-2.17 mechanism honest.
//
// SPEC §6.4 prices exactly two rejections: an invalid solution (5%) and
// equivocation (20%). Every other way a proposal can be refused — a closed
// session, an inactive solver, the per-session solution cap — costs gas and has
// never cost bond. Moving the slash into the ante makes it newly easy to charge
// for all of them by accident, and nothing in the two replaced pins would
// notice: they only assert that a priced rejection IS charged.
//
// So this proposes into a session that is no longer in AUCTION, and asserts the
// specific rejection (1409) together with an untouched bond.
func TestUnpricedRejectionDoesNotSlash(t *testing.T) {
	f := newSlashFixture(t)

	// run the auction out: the session leaves AUCTION and settles.
	f.advance(10)
	after := f.app.NewContext(true)
	s, err := f.app.SessionKeeper.GetSession(after, 0, f.session.Id)
	require.NoError(t, err)
	require.NotEqual(t, sessiontypes.SESSION_STATE_AUCTION, s.State,
		"the fixture must actually leave AUCTION or this test proves nothing")

	res := f.deliver(f.solverPriv, &sessiontypes.MsgProposeSolution{
		Solver: f.solverAddr.String(), SessionId: f.session.Id, Matches: f.matches,
	})
	require.Equal(t, "session", res.Codespace)
	require.Equal(t, uint32(1409), res.Code,
		"ErrSessionNotInAuction — the rejection must be this one, not a slashable one")

	sv := f.solverState()
	require.Equal(t, uint64(0), sv.SlashedCount,
		"an UNPRICED rejection must not slash: SPEC §6.4 prices invalid solutions and "+
			"equivocation, and nothing else")
	require.Equal(t, math.NewInt(100_000_000), sv.Bond.AmountOf(app.BaseDenom),
		"the bond is untouched — the ante prices the verdict, it does not charge for arriving")
}

// TestProposeSolutionMustBeSoleMessage pins the rule that makes the task-2.17
// mechanism SOUND rather than merely present (ErrProposalNotSole, session 1430).
//
// The ante evaluates a proposal against pre-tx state; the msg server evaluates
// it against state that includes every earlier message of the same tx. If a tx
// could carry both, the two verdicts could disagree, and both directions are
// defects: a sibling message that turns an invalid proposal VALID slashes a
// solver whose solution then succeeds, and one that turns a valid proposal
// INVALID is a free way to propose garbage. Requiring the proposal to be alone
// makes the two evaluations read identical state and run identical code, so
// their agreement is a property of the code and not of the tx the solver chose
// to build.
//
// Both illegal shapes are asserted, because the second is the one a type switch
// over top-level messages silently misses: a proposal WRAPPED in authz MsgExec
// still executes, so a rule enforced only at the top level would leave a
// wrapper as the bypass.
func TestProposeSolutionMustBeSoleMessage(t *testing.T) {
	f := newSlashFixture(t)
	propose := &sessiontypes.MsgProposeSolution{
		Solver: f.solverAddr.String(), SessionId: f.session.Id, Matches: f.matches,
	}

	t.Run("sibling message", func(t *testing.T) {
		send := &banktypes.MsgSend{
			FromAddress: f.solverAddr.String(),
			ToAddress:   f.seller.String(),
			Amount:      sdk.NewCoins(sdk.NewCoin(app.BaseDenom, math.NewInt(1))),
		}
		res := f.deliverMulti(f.solverPriv, send, propose)
		require.Equal(t, "session", res.Codespace)
		require.Equal(t, uint32(1430), res.Code,
			"ErrProposalNotSole — rejected for THIS reason, not for whatever the sibling would do")
	})

	t.Run("wrapped in authz MsgExec", func(t *testing.T) {
		exec := authz.NewMsgExec(f.solverAddr, []sdk.Msg{propose})
		res := f.deliverMulti(f.solverPriv, &exec)
		require.Equal(t, "session", res.Codespace)
		require.Equal(t, uint32(1430), res.Code,
			"a wrapped proposal is caught by the same nested walk the mandate chokepoint uses — "+
				"a top-level-only rule would leave MsgExec as the bypass")
	})

	// Neither shape may have moved anything: rejected before execution, and
	// certainly not slashed.
	sv := f.solverState()
	require.Equal(t, uint64(0), sv.SlashedCount, "a rejected tx shape is not a priced verdict")
	require.Equal(t, math.NewInt(100_000_000), sv.Bond.AmountOf(app.BaseDenom))
	// Nothing the SOLVER proposed was stored. Deliberately not "no solutions at
	// all": each delivery above advances a block, so the session runs out its
	// auction and the in-consensus FALLBACK writes its own synthetic Solution
	// with an EMPTY solver (D-086). Asserting emptiness would fail on that and
	// say nothing about the rule under test.
	sols, err := f.app.SessionKeeper.SolutionsForSession(f.app.NewContext(true), f.session.Id)
	require.NoError(t, err)
	for _, s := range sols {
		require.NotEqual(t, f.solverAddr.String(), s.Solver,
			"neither illegal shape may have stored a solution for the solver")
	}
}

// TestConcludeFailedSlashPersistsThroughARealBlock verifies the THIRD slash
// site — and it exists because of an over-correction risk, not a suspected bug.
//
// Task 2.14 found the proposal-time slashes did not land and recorded it as
// "the SPEC §6.4 bond slash does not land on the tx path". That statement is
// over-broad. x/session slashes in three places, and the third —
// concludeSolution, when a WINNING solution fails at settlement (conclude.go,
// "conclude_failed", 5%) — runs in EndBlock on the parent context, outside
// Conclude's cache, where no per-message branch exists to discard it. It was
// never broken.
//
// METHODOLOGY Rule 4 step 5 says a claim verified against one path is
// unverified, and that applies to MY claim as much as to the one it corrects.
// So this proves the third site through a real FinalizeBlock rather than by
// reading the code: a valid solution is accepted (no slash), the buyer is then
// drained so its settlement leg cannot pay, and the session concludes.
func TestConcludeFailedSlashPersistsThroughARealBlock(t *testing.T) {
	f := newSlashFixture(t)

	// Drain the buyer FIRST, so its settlement leg cannot pay. validateSolution
	// checks conservation, limits, prices and consent — never balances — so the
	// proposal below is still VALID and still wins; it is CONCLUDE that fails.
	// The give amount is not escrowed at submit (only the intent bond is), which
	// is what makes this reachable at all.
	//
	// Both go in ONE block, drain first. That detail is load-bearing twice over:
	// the auction window is only a few blocks wide, so spending a whole block on
	// the drain closes it and the proposal comes back 1409 instead (which is what
	// a first attempt at this test did); and executing them in one block still
	// orders them, since each tx commits its own branch before the next runs.
	ctx := f.app.NewContext(true)
	usdc := f.app.BankKeeper.GetBalance(ctx, f.buyer, app.MockUSDCDenom)
	require.True(t, usdc.Amount.IsPositive())

	block, _ := f.deliverAllBlock(
		[]cryptotypes.PrivKey{f.buyerPriv, f.solverPriv},
		[]sdk.Msg{
			&banktypes.MsgSend{
				FromAddress: f.buyer.String(),
				ToAddress:   f.seller.String(),
				Amount:      sdk.NewCoins(usdc),
			},
			&sessiontypes.MsgProposeSolution{
				Solver: f.solverAddr.String(), SessionId: f.session.Id, Matches: f.matches,
			},
		},
	)
	out := block.TxResults
	require.Equal(t, uint32(0), out[0].Code, "drain: %s", out[0].Log)
	require.Equal(t, uint32(0), out[1].Code, "the proposal is VALID: %s", out[1].Log)

	reasons := slashReasonsIn(block.Events)
	for _, tx := range out {
		reasons = append(reasons, slashReasonsIn(tx.Events)...)
	}
	reasons = append(reasons, f.advanceCollectingSlashReasons(10)...)

	sv := f.solverState()
	require.Equal(t, uint64(1), sv.SlashedCount,
		"the conclude-time 5% slash DOES land — it is written in EndBlock on the parent ctx, "+
			"outside Conclude's cache, so no message branch ever discards it")
	require.Equal(t, math.NewInt(95_000_000), sv.Bond.AmountOf(app.BaseDenom),
		"invalid_solution_slash_bps (500) is the conclude-failed rate too (conclude.go)")

	// WHICH site fired, asserted by name. The count alone cannot distinguish the
	// third site from the two this task just fixed — and the session concludes in
	// the SAME block the proposal lands in, so a "not slashed yet, then slashed"
	// discriminator does not exist to be used. The event's reason attribute does.
	require.Equal(t, []string{"conclude_failed"}, reasons,
		"exactly one slash, and it is the CONCLUDE-time one — not invalid_solution, "+
			"not equivocation: the proposal was accepted (code 0) and settlement is what failed")
}
