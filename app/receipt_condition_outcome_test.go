package app_test

import (
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	cmtprotocrypto "github.com/cometbft/cometbft/proto/tendermint/crypto"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"

	commitmenttypes "github.com/cosmos/ibc-go/v8/modules/core/23-commitment/types"

	"github.com/AryaSingh22/conclave/app"
	"github.com/AryaSingh22/conclave/fillback"
	escrowtypes "github.com/AryaSingh22/conclave/x/escrow/types"
	receiptkeeper "github.com/AryaSingh22/conclave/x/receipt/keeper"
	receipttypes "github.com/AryaSingh22/conclave/x/receipt/types"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
)

// This file is task 2.9's ACCEPTANCE gate (D-100). The finding it closes was
// about an EXTERNALLY VERIFIABLE artifact (D-087), so the gate is deliberately
// not keeper-level: every assertion below runs through real
// FinalizeBlock+Commit blocks and reads condition_outcome back through the SAME
// surface an external verifier uses — the x/receipt query server and an ICS-23
// existence proof verified against a header-anchored app root.
//
// A keeper test would have proven the write-back function works. It could not
// have proven that the app WIRES it (the escrow→session writer seam is set in
// app.go and is nil in every keeper fixture), that both of the receipt's store
// keys move together, or that a proof issued before resolution still verifies
// afterwards — which is the whole content of the mutability contract 2.9 adds.
//
// THE VECTOR COVERS NONE OF THIS, MEASURED RATHER THAN ASSUMED (D-100). Over
// the five committed seeds the determinism sim writes 1,524 receipts through
// 508 settlements and creates 508 escrows — every single one of them class
// NONE, with zero conditioned escrows, zero terminal transitions and zero
// disputes. So the generator exercises the receipt path richly and the
// condition_outcome field only ever in its NONE branch. These tests are the
// primary gate; the vector is a regression backstop (METHODOLOGY Rule 7 step 5).

const (
	t29Price = 100
	// t29ReleaseWin is wide enough that the escrow never times out during a
	// release test; t29ShortWin is the timeout/dispute window a test deliberately
	// runs OUT, and it must comfortably exceed the blocks t29Settle itself
	// consumes after CONCLUDE.
	t29ReleaseWin = 1_000
	t29ShortWin   = 20
)

// t29Fixture wraps the task-2.14/2.17 solver fixture (which already settles a
// D-074 conditioned pair through real blocks) with the app-root bookkeeping an
// ICS-23 verification needs.
type t29Fixture struct {
	*solverFixture
	sellerSigil, buyerSigil string
	escrowID                string
	sessionID               uint64
}

// t29Settle drives a conditioned settlement to CONCLUDE and returns the
// fixture. No solver proposes: the in-consensus FALLBACK clears it and attaches
// the seller's signed condition (task 2.7, D-094), which is the DEFAULT path
// and therefore the one that matters most for this defect.
func t29Settle(t *testing.T, cond *escrowtypes.DeliveryCondition) *t29Fixture {
	t.Helper()
	sf := newSolverFixture(t)
	f := &t29Fixture{solverFixture: sf}
	f.sellerSigil = sf.registerSigil(sf.sellerPriv, "t29-seller")
	f.buyerSigil = sf.registerSigil(sf.buyerPriv, "t29-buyer")

	sf.deliverAll(
		[]cryptotypes.PrivKey{sf.sellerPriv, sf.buyerPriv},
		sf.conditionedIntents(f.sellerSigil, f.buyerSigil, cond, t29Price),
	)
	session := sf.advanceToAuction(40)
	f.sessionID = session.Id

	// Advance only until the settlement lands, never a fixed count: the escrow's
	// timeout is measured from the CONCLUDE height, so slack here would burn the
	// window before a test can observe PENDING at all. (It did: a fixed
	// advance(6) with a 5-block window produced an already-REFUNDED escrow.)
	var escs []escrowtypes.Escrow
	for i := 0; i < 20 && len(escs) == 0; i++ {
		sf.advance(1)
		escs = sf.escrows()
	}
	require.Len(t, escs, 1, "the settlement must have opened exactly one escrow for its DataGrant leg")
	require.Equal(t, escrowtypes.ESCROW_STATE_ACTIVE, escs[0].State)
	f.escrowID = escs[0].Id
	return f
}

// receipt returns one sigil's single receipt, read through the §6.6 query
// surface (the 0x07 per-agent index) rather than out of the store.
func (f *t29Fixture) receipt(t *testing.T, sigil string) receipttypes.Receipt {
	t.Helper()
	rq := receiptkeeper.NewQueryServerImpl(f.app.ReceiptKeeper)
	list, err := rq.Receipts(f.app.NewContext(true), &receipttypes.QueryReceiptsRequest{SigilId: sigil})
	require.NoError(t, err)
	require.Len(t, list.Receipts, 1, "one receipt per party per settlement (SPEC §6.4)")
	return list.Receipts[0]
}

// outcomes returns (seller, buyer) condition outcomes — both parties, because
// one escrow governs both receipts and a one-sided write-back would be a bug
// only a two-sided assertion can see.
func (f *t29Fixture) outcomes(t *testing.T) (receipttypes.ConditionOutcome, receipttypes.ConditionOutcome) {
	t.Helper()
	return f.receipt(t, f.sellerSigil).ConditionOutcome, f.receipt(t, f.buyerSigil).ConditionOutcome
}

// proofAndRoot fetches a tip ReceiptProof together with the app root it must be
// verified against. A height-0 prove query is served at `latest−1`, so the root
// recorded BEFORE the extra block is exactly the root at ProofHeight
// (7683_SURFACE §6.2).
func (f *t29Fixture) proofAndRoot(t *testing.T, receiptID string) (*receipttypes.QueryReceiptProofResponse, []byte) {
	t.Helper()
	root := f.app.LastCommitID().Hash
	want := uint64(f.app.LastCommitID().Version)
	f.advance(1)

	rq := receiptkeeper.NewQueryServerImpl(f.app.ReceiptKeeper)
	resp, err := rq.ReceiptProof(f.app.NewContext(true), &receipttypes.QueryReceiptProofRequest{Id: receiptID})
	require.NoError(t, err)
	require.Equal(t, want, resp.ProofHeight, "tip proof is served at the last header-anchored version")
	return resp, root
}

// t29Verify runs the §6.3 check-1 ICS-23 membership verification exactly as an
// origin settler would: the PROVEN bytes are the receipt bytes, so this is what
// makes "the field is truthful" a statement about an externally verifiable
// artifact rather than about a local query.
func t29Verify(resp *receipttypes.QueryReceiptProofResponse, root []byte, storeKey []byte) error {
	value, err := resp.Receipt.Marshal()
	if err != nil {
		return err
	}
	var ops cmtprotocrypto.ProofOps
	if err := ops.Unmarshal(resp.Proof); err != nil {
		return err
	}
	merkle, err := commitmenttypes.ConvertProofs(&ops)
	if err != nil {
		return err
	}
	return merkle.VerifyMembership(
		commitmenttypes.GetSDKSpecs(),
		commitmenttypes.NewMerkleRoot(root),
		commitmenttypes.NewMerklePath(sessiontypes.StoreKey, string(storeKey)),
		value,
	)
}

func (f *t29Fixture) storeKey(t *testing.T, receiptID string) []byte {
	t.Helper()
	key, err := f.app.SessionKeeper.ReceiptByIDStoreKey(receiptID)
	require.NoError(t, err)
	return key
}

func t29Hashlock(preimage []byte, window uint64) *escrowtypes.DeliveryCondition {
	h := sha256.Sum256(preimage)
	return &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_Hashlock{
		Hashlock: &escrowtypes.HashlockCondition{PayloadHash: h[:], TimeoutBlocks: window},
	}}
}

func t29Optimistic(window uint64) *escrowtypes.DeliveryCondition {
	return &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_Optimistic{
		Optimistic: &escrowtypes.OptimisticCondition{DisputeWindowBlocks: window},
	}}
}

func t29Order(recipientSigil string) fillback.Order {
	return fillback.Order{
		ID: "t29-order", RecipientSigil: recipientSigil,
		Denom: app.MockUSDCDenom, MinReceived: math.NewInt(t29Price),
	}
}

// TestConditionOutcome_PendingThenReleased is the primary gate: the full
// PENDING → RELEASED life of a conditioned settlement's receipt, read back
// through the proof surface at both ends, with the pre-resolution proof
// re-verified after the mutation.
func TestConditionOutcome_PendingThenReleased(t *testing.T) {
	preimage := []byte("t29-receipt-of-access")
	f := t29Settle(t, t29Hashlock(preimage, t29ReleaseWin))

	// --- at settlement: PENDING, and provably so ---
	seller, buyer := f.outcomes(t)
	require.Equal(t, receipttypes.CONDITION_OUTCOME_PENDING, seller,
		"a conditioned settlement stamps PENDING at CONCLUDE, never NONE (D-087 was the unconditional NONE)")
	require.Equal(t, receipttypes.CONDITION_OUTCOME_PENDING, buyer,
		"both parties' receipts report the same escrow")

	rec := f.receipt(t, f.sellerSigil)
	pendingProof, pendingRoot := f.proofAndRoot(t, rec.Id)
	key := f.storeKey(t, rec.Id)
	require.Equal(t, receipttypes.CONDITION_OUTCOME_PENDING, pendingProof.Receipt.ConditionOutcome)
	require.NoError(t, t29Verify(pendingProof, pendingRoot, key),
		"the PENDING receipt is ICS-23-provable against the header-anchored root")

	// --- release through a real transaction ---
	res := f.deliver(f.buyerPriv, &escrowtypes.MsgReleaseEscrow{
		Signer: f.buyer.String(), EscrowId: f.escrowID, Preimage: preimage,
	})
	require.Equal(t, uint32(0), res.Code, "release: %s", res.Log)

	escs := f.escrows()
	require.Len(t, escs, 1)
	require.Equal(t, escrowtypes.ESCROW_STATE_RELEASED, escs[0].State)

	// --- the write-back reached BOTH receipts ---
	seller, buyer = f.outcomes(t)
	require.Equal(t, receipttypes.CONDITION_OUTCOME_RELEASED, seller,
		"escrow resolution must reach the seller's receipt — this is the D-087 write-back")
	require.Equal(t, receipttypes.CONDITION_OUTCOME_RELEASED, buyer,
		"and the buyer's")

	// --- and it is provable at the new root ---
	releasedProof, releasedRoot := f.proofAndRoot(t, rec.Id)
	require.Equal(t, receipttypes.CONDITION_OUTCOME_RELEASED, releasedProof.Receipt.ConditionOutcome)
	require.NoError(t, t29Verify(releasedProof, releasedRoot, key),
		"the RELEASED receipt is ICS-23-provable against the new header-anchored root")

	// --- THE MIGRATION ANSWER, AS EVIDENCE RATHER THAN ARGUMENT ---
	// A receipt's bytes changed. No proof issued before the change was
	// invalidated by it: an ICS-23 proof is bound to ProofHeight and proves the
	// value AT that version, so each remains a true statement about its own
	// height. That is why the write-back needs no migration of stored receipts
	// and why the mutability is safe to document normatively (7683_SURFACE
	// §6.1/§6.2).
	require.NotEqual(t, pendingRoot, releasedRoot)
	require.NoError(t, t29Verify(pendingProof, pendingRoot, key),
		"the pre-resolution proof STILL verifies against its own root after the receipt changed")
	require.Error(t, t29Verify(pendingProof, releasedRoot, key),
		"and it does not verify against the later root — the height binding is real in both directions")
}

// TestConditionOutcome_RefundedByTimeoutSweep proves the OTHER terminal outcome,
// and it matters more than the release: a refund is the case where receipt
// existence would otherwise be read as delivery completion while the funds went
// back to the payer (the concrete D-087 harm). It also proves the write-back
// works from the EndBlock sweep, not only from a transaction.
func TestConditionOutcome_RefundedByTimeoutSweep(t *testing.T) {
	f := t29Settle(t, t29Hashlock([]byte("never-presented"), t29ShortWin))

	seller, buyer := f.outcomes(t)
	require.Equal(t, receipttypes.CONDITION_OUTCOME_PENDING, seller)
	require.Equal(t, receipttypes.CONDITION_OUTCOME_PENDING, buyer)

	f.advance(t29ShortWin + 2) // the HASHLOCK timeout sweep fires in EndBlock

	escs := f.escrows()
	require.Len(t, escs, 1)
	require.Equal(t, escrowtypes.ESCROW_STATE_REFUNDED, escs[0].State,
		"the timeout sweep refunds the buyer")

	seller, buyer = f.outcomes(t)
	require.Equal(t, receipttypes.CONDITION_OUTCOME_REFUNDED, seller,
		"a refund must reach the receipt — otherwise the receipt still claims a settlement that was undone")
	require.Equal(t, receipttypes.CONDITION_OUTCOME_REFUNDED, buyer)
}

// TestConditionOutcome_DisputedIsReportedAndIsNotTerminal proves the third
// value, which had no writer at all before 2.9, and pins the semantic decision
// that came with it: DISPUTED is an OPEN dispute, not a resolution, so the
// receipt follows it onward to a terminal outcome.
func TestConditionOutcome_DisputedIsReportedAndIsNotTerminal(t *testing.T) {
	f := t29Settle(t, t29Optimistic(t29ShortWin))

	seller, buyer := f.outcomes(t)
	require.Equal(t, receipttypes.CONDITION_OUTCOME_PENDING, seller)
	require.Equal(t, receipttypes.CONDITION_OUTCOME_PENDING, buyer)

	// the buyer (payer) disputes in-window
	res := f.deliver(f.buyerPriv, &escrowtypes.MsgRaiseDispute{
		Signer: f.buyer.String(), EscrowId: f.escrowID, EvidenceHash: []byte("mismatch"),
	})
	require.Equal(t, uint32(0), res.Code, "raise dispute: %s", res.Log)

	seller, buyer = f.outcomes(t)
	require.Equal(t, receipttypes.CONDITION_OUTCOME_DISPUTED, seller,
		"an open dispute is reported on the receipt")
	require.Equal(t, receipttypes.CONDITION_OUTCOME_DISPUTED, buyer)

	// the seller never proves delivery: the resolution sweep refunds the buyer
	f.advance(t29ShortWin + 2)
	escs := f.escrows()
	require.Len(t, escs, 1)
	require.Equal(t, escrowtypes.ESCROW_STATE_REFUNDED, escs[0].State)

	seller, buyer = f.outcomes(t)
	require.Equal(t, receipttypes.CONDITION_OUTCOME_REFUNDED, seller,
		"DISPUTED is NOT terminal — the receipt moves on to the real outcome")
	require.Equal(t, receipttypes.CONDITION_OUTCOME_REFUNDED, buyer)
}

// TestConditionOutcome_UnconditionedStaysNone is the no-regression half: the
// stamp is now conditional, so the case that was always correct must stay
// correct. It uses the ordinary unconditioned settlement path.
func TestConditionOutcome_UnconditionedStaysNone(t *testing.T) {
	f := t29Settle(t, nil)

	require.Nil(t, f.escrows()[0].Condition.Condition,
		"an unconditioned DataGrant still opens the SPEC §6.4 record-only escrow stub")

	seller, buyer := f.outcomes(t)
	require.Equal(t, receipttypes.CONDITION_OUTCOME_NONE, seller,
		"an unconditioned settlement reads NONE — and NONE now MEANS unconditioned")
	require.Equal(t, receipttypes.CONDITION_OUTCOME_NONE, buyer)
}

// TestConditionOutcome_BothStoreViewsAgree pins the risk the in-place rewrite
// creates: the receipt lives under TWO keys (0x07 per-agent, 0x08 by id) and a
// one-sided update would make Receipts(sigil) and Receipt(id) disagree about
// the same settlement — a divergence no single-surface test can see.
func TestConditionOutcome_BothStoreViewsAgree(t *testing.T) {
	preimage := []byte("t29-both-views")
	f := t29Settle(t, t29Hashlock(preimage, t29ReleaseWin))

	res := f.deliver(f.buyerPriv, &escrowtypes.MsgReleaseEscrow{
		Signer: f.buyer.String(), EscrowId: f.escrowID, Preimage: preimage,
	})
	require.Equal(t, uint32(0), res.Code, "release: %s", res.Log)

	rq := receiptkeeper.NewQueryServerImpl(f.app.ReceiptKeeper)
	ctx := f.app.NewContext(true)
	for _, sigil := range []string{f.sellerSigil, f.buyerSigil} {
		byIndex := f.receipt(t, sigil) // 0x07
		byID, err := rq.Receipt(ctx, &receipttypes.QueryReceiptRequest{Id: byIndex.Id})
		require.NoError(t, err)
		require.Equal(t, receipttypes.CONDITION_OUTCOME_RELEASED, byIndex.ConditionOutcome)
		require.Equal(t, byIndex.ConditionOutcome, byID.Receipt.ConditionOutcome,
			"the 0x07 and 0x08 views of receipt %s must report the same outcome", byIndex.Id)
		require.Equal(t, byIndex, byID.Receipt, "and must be the same record entirely")
	}
}

// TestConditionOutcome_TerminalIsAbsorbing pins the MONOTONICITY that bounds the
// mutability D-100 introduces: PENDING → [DISPUTED] → terminal, and never out
// of terminal. It calls the writer DIRECTLY with a contradicting state, which
// the escrow gate would never emit — that is the point. The receipt's guarantee
// must not depend on x/escrow continuing to make terminal states absorbing,
// because an external verifier who cached a RELEASED reading is relying on THIS
// property, not on that one.
func TestConditionOutcome_TerminalIsAbsorbing(t *testing.T) {
	preimage := []byte("t29-absorbing")
	f := t29Settle(t, t29Hashlock(preimage, t29ReleaseWin))
	require.Equal(t, uint32(0), f.deliver(f.buyerPriv, &escrowtypes.MsgReleaseEscrow{
		Signer: f.buyer.String(), EscrowId: f.escrowID, Preimage: preimage,
	}).Code)

	seller, _ := f.outcomes(t)
	require.Equal(t, receipttypes.CONDITION_OUTCOME_RELEASED, seller)

	// An uncached context over the committed store: these two tests call the
	// write-back DIRECTLY, which is not something a block ever does, so there is
	// no FinalizeBlock state to borrow.
	ctx := f.app.NewUncachedContext(false, cmtproto.Header{Height: f.app.LastBlockHeight()})
	require.NoError(t, f.app.SessionKeeper.SetReceiptOutcomeForEscrow(
		ctx, f.escrowID, escrowtypes.ESCROW_STATE_REFUNDED))
	rq := receiptkeeper.NewQueryServerImpl(f.app.ReceiptKeeper)
	list, err := rq.Receipts(ctx, &receipttypes.QueryReceiptsRequest{SigilId: f.sellerSigil})
	require.NoError(t, err)
	require.Equal(t, receipttypes.CONDITION_OUTCOME_RELEASED, list.Receipts[0].ConditionOutcome,
		"a terminal outcome is absorbing — the receipt is never rewritten out of it")
}

// TestConditionOutcome_UnmappedEscrowStateIsNamed asserts the SPECIFIC error for
// a state the receipt does not report, rather than "an error": a negative that
// cannot distinguish its own failure mode tests nothing (D-091 F-3).
func TestConditionOutcome_UnmappedEscrowStateIsNamed(t *testing.T) {
	f := t29Settle(t, t29Hashlock([]byte("t29-unmapped"), t29ReleaseWin))
	// An uncached context over the committed store: these two tests call the
	// write-back DIRECTLY, which is not something a block ever does, so there is
	// no FinalizeBlock state to borrow.
	ctx := f.app.NewUncachedContext(false, cmtproto.Header{Height: f.app.LastBlockHeight()})

	err := f.app.SessionKeeper.SetReceiptOutcomeForEscrow(ctx, f.escrowID, escrowtypes.ESCROW_STATE_ACTIVE)
	require.ErrorIs(t, err, sessiontypes.ErrReceiptOutcomeUnmapped)
	require.Contains(t, err.Error(), "ESCROW_STATE_ACTIVE")

	err = f.app.SessionKeeper.SetReceiptOutcomeForEscrow(ctx, f.escrowID, escrowtypes.ESCROW_STATE_UNSPECIFIED)
	require.ErrorIs(t, err, sessiontypes.ErrReceiptOutcomeUnmapped)
}

// TestFillbackAdmissibility_TracksTheOutcome is the CONTRACT half — finding F-16
// (D-100). It walks 7683_SURFACE §6.3 check 3 across a conditioned settlement's
// whole life against the ONE shared implementation both in-tree consumers now
// call, and it is the direct proof that the pre-2.9 rule was inert:
// `PaysOrder` — which is check 3 minus the outcome conjunct, and is exactly
// what the old rule reduced to for any chain-valid Match — is satisfied at
// EVERY stage below, including while the funds sit in escrow and after they
// have refunded to the payer.
func TestFillbackAdmissibility_TracksTheOutcome(t *testing.T) {
	f := t29Settle(t, t29Optimistic(t29ShortWin))
	order := t29Order(f.sellerSigil) // the order's recipient is the data SELLER, paid in coin

	winning := f.winningMatch(t)

	// (1) PENDING — the funds are escrowed. The paying leg looks final.
	require.NoError(t, fillback.PaysOrder(winning, order),
		"the escrowed coin leg pays the recipient in full — this is what the pre-2.9 rule saw, "+
			"and it is why the leg-condition clause could never reject it (finding F-16)")
	rec := f.receipt(t, f.sellerSigil)
	require.ErrorIs(t, fillback.Admissible(rec, winning, order), fillback.ErrOutcomeNotFinal,
		"check 3 must REJECT a fill whose escrow has not resolved")

	// (2) DISPUTED — still not final.
	require.Equal(t, uint32(0), f.deliver(f.buyerPriv, &escrowtypes.MsgRaiseDispute{
		Signer: f.buyer.String(), EscrowId: f.escrowID, EvidenceHash: []byte("mismatch"),
	}).Code)
	require.ErrorIs(t, fillback.Admissible(f.receipt(t, f.sellerSigil), winning, order),
		fillback.ErrOutcomeNotFinal)

	// (3) REFUNDED — the fill was UNDONE, and this is the harm: the settler
	// would have reimbursed a resolver whose money came back.
	f.advance(t29ShortWin + 2)
	require.Equal(t, escrowtypes.ESCROW_STATE_REFUNDED, f.escrows()[0].State)
	require.NoError(t, fillback.PaysOrder(winning, order),
		"the Match is unchanged — a refunded fill is INDISTINGUISHABLE from a delivered one "+
			"by any rule that reads only the legs")
	require.ErrorIs(t, fillback.Admissible(f.receipt(t, f.sellerSigil), winning, order),
		fillback.ErrOutcomeNotFinal)
}

// TestFillbackAdmissibility_ReleasedIsAdmissible is C1's other half: with a
// truthful field, a conditioned fill that actually delivered becomes admissible
// — which is the capability 7683_SURFACE §6.4 said was out of scope "until task
// 2.9".
func TestFillbackAdmissibility_ReleasedIsAdmissible(t *testing.T) {
	preimage := []byte("t29-admissible")
	f := t29Settle(t, t29Hashlock(preimage, t29ReleaseWin))
	order := t29Order(f.sellerSigil)

	require.Equal(t, uint32(0), f.deliver(f.buyerPriv, &escrowtypes.MsgReleaseEscrow{
		Signer: f.buyer.String(), EscrowId: f.escrowID, Preimage: preimage,
	}).Code)

	require.NoError(t, fillback.Admissible(f.receipt(t, f.sellerSigil), f.winningMatch(t), order),
		"a RELEASED conditioned settlement IS delivery evidence — the escrow paid the payee")

	// and the first conjunct still discriminates on its own terms
	wrong := t29Order(f.buyerSigil)
	require.ErrorIs(t, fillback.Admissible(f.receipt(t, f.sellerSigil), f.winningMatch(t), wrong),
		fillback.ErrNoPayingLeg)
}

// winningMatch returns the settled Match — the terms pre-image a bridge fetches
// (7683_SURFACE §5), read through the query surface.
func (f *t29Fixture) winningMatch(t *testing.T) sessiontypes.Match {
	t.Helper()
	ctx := f.app.NewContext(true)
	sols, err := f.app.SessionKeeper.SolutionsForSession(ctx, f.sessionID)
	require.NoError(t, err)
	require.Len(t, sols, 1)
	require.Len(t, sols[0].Matches, 1)
	return sols[0].Matches[0]
}
