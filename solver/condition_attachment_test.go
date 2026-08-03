package solver_test

import (
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AryaSingh22/conclave/crypto/zkdelivery"
	"github.com/AryaSingh22/conclave/crypto/zkdelivery/zktest"
	"github.com/AryaSingh22/conclave/solver"
	"github.com/AryaSingh22/conclave/solver/condition"
	"github.com/AryaSingh22/conclave/solver/cycles"
	"github.com/AryaSingh22/conclave/solver/gate"
	"github.com/AryaSingh22/conclave/solver/match"
	escrowtypes "github.com/AryaSingh22/conclave/x/escrow/types"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// This file is task 2.14's solver-side gate (D-097). The determinism vector
// covers NONE of it, and that is structural rather than incidental: the
// simulation's intent generator builds IntentPayload{Give, Want} and never sets
// IntentPayload.condition, and its coinLeg/dataLeg/svcLeg helpers never set
// IntentLeg.condition (x/veil/simulation/operations.go) — so every path below is
// unreachable by the vector under any seed count. A Category-1 coverage shadow
// in the D-093 taxonomy, whose remedy is a wider generator (PHASE3_PLAN 3.5),
// not more seeds. These fixtures plus app/solver_conformance_test.go ARE the
// gate; the vector is a regression backstop only.

const attachDataset = "dataset-D"

func hashlock(hash []byte, timeout uint64) *escrowtypes.DeliveryCondition {
	return &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_Hashlock{
		Hashlock: &escrowtypes.HashlockCondition{PayloadHash: hash, TimeoutBlocks: timeout},
	}}
}

func optimistic(window uint64) *escrowtypes.DeliveryCondition {
	return &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_Optimistic{
		Optimistic: &escrowtypes.OptimisticCondition{DisputeWindowBlocks: window},
	}}
}

func zk(seed string) *escrowtypes.DeliveryCondition {
	return &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_ZkDelivery{
		ZkDelivery: &escrowtypes.ZkDeliveryCondition{
			SchemaCommitment: zktest.SchemaCommitment([]byte(seed)), VkId: zkdelivery.VKIDDeliveryV1,
		},
	}}
}

// conditionedPair is the D-074 bilateral shape the whole task turns on: a
// SELLER giving a dataset for coin, and a BUYER giving that coin for the
// dataset. Each side's payload-level signed condition is supplied separately,
// because the seller's and the buyer's are governed by DIFFERENT relations
// (exact vs per-class) and the interesting cases are where they differ.
func conditionedPair(sellerSigned, buyerSigned *escrowtypes.DeliveryCondition) []match.OpenIntent {
	seller := oi("i-seller", "sigil-seller", dataLeg(attachDataset), coinLeg("uusdc", 100))
	seller.Condition = sellerSigned
	buyer := oi("i-buyer", "sigil-buyer", coinLeg("uusdc", 100), dataLeg(attachDataset))
	buyer.Condition = buyerSigned
	return []match.OpenIntent{seller, buyer}
}

// dataLegCondition returns the condition attached to the match's DataGrant leg,
// which is the single observable this whole task is about.
func dataLegCondition(t *testing.T, m sessiontypes.Match) *escrowtypes.DeliveryCondition {
	t.Helper()
	for _, leg := range m.Legs {
		if leg.GetData() != nil {
			return leg.Condition
		}
	}
	t.Fatalf("match has no DataGrant leg")
	return nil
}

// ---------------------------------------------------------------------------
// THE DEFECT ITSELF — the reference solver attaches what the seller signed
// ---------------------------------------------------------------------------

// TestReferenceSolverAttachesTheSignedCondition is the closing of task 2.14's
// live defect, at the library level. Before 2.14 non-test solver/ read no
// Condition anywhere, so this pair produced a match with a BARE DataGrant leg —
// a downgrade, invalid since task 2.7 (ErrConditionNotHonored, session 1429),
// and a 5% bond slash that commits with the failing tx.
//
// All three classes, because the pre-2.14 blindness was total: a fix that
// attached only the class someone happened to test would read as closed.
func TestReferenceSolverAttachesTheSignedCondition(t *testing.T) {
	hash := sha256.Sum256([]byte("receipt-of-access"))
	cases := []struct {
		name   string
		signed *escrowtypes.DeliveryCondition
	}{
		{"HASHLOCK", hashlock(hash[:], 50)},
		{"OPTIMISTIC", optimistic(100)},
		{"ZK_DELIVERY", zk("schema-v1")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := solver.Solve(conditionedPair(tc.signed, tc.signed))
			require.Len(t, got, 1, "the conditioned pair must clear")

			attached := dataLegCondition(t, got[0])
			require.NotNil(t, attached, "the DataGrant leg must carry a condition — a bare leg is the 1429 downgrade")
			require.Equal(t, tc.signed, attached,
				"the seller's relation is EXACT: the attached condition must be what it signed, parameters included")

			// and the whole match must pass the chain's own bars, which is the
			// property that makes this a valid solution rather than a shaped one.
			require.NoError(t, gate.Accept(got[0]))
			require.NoError(t, gate.AcceptConditionShape(got[0]))
			require.NoError(t, gate.AcceptConsent(got[0],
				condition.PayloadFor(condition.Participants(got[0], byIDOf(conditionedPair(tc.signed, tc.signed))))))
		})
	}
}

// TestAttachmentIsIndependentOfLegOrder covers what dosing cannot here. D-089's
// lesson is that a one-shot result can exercise one role by accident, because
// naive.Find sorts candidates by intent id and the pair's seller/buyer roles
// follow that order. This suite's ids are literals, so repeating the run would
// pin the SAME ordering every time and prove nothing — the fixture has to be
// varied instead. Here the seller sorts first; in conditionedPair the buyer
// does, so between them both assignments are exercised.
func TestAttachmentIsIndependentOfLegOrder(t *testing.T) {
	signed := optimistic(50)
	seller := oi("a-seller", "sigil-seller", dataLeg(attachDataset), coinLeg("uusdc", 100))
	seller.Condition = signed
	buyer := oi("z-buyer", "sigil-buyer", coinLeg("uusdc", 100), dataLeg(attachDataset))
	buyer.Condition = signed

	got := solver.Solve([]match.OpenIntent{seller, buyer})
	require.Len(t, got, 1)
	require.Equal(t, signed, dataLegCondition(t, got[0]),
		"the condition follows the DataGrant leg's giver, not the position of the intent in the sorted order")
}

func byIDOf(intents []match.OpenIntent) map[string]match.OpenIntent {
	out := make(map[string]match.OpenIntent, len(intents))
	for _, o := range intents {
		out[o.ID] = o
	}
	return out
}

// ---------------------------------------------------------------------------
// THE PER-CLASS BUYER RELATIONS (D-094 R3) — opposite directions, per class
// ---------------------------------------------------------------------------

// TestBuyerRelationsAreHonoredPerClass exercises each class's relation from the
// SOLVER's side: the reference must clear the pairs the chain would accept and
// must DROP the ones it would reject, rather than proposing them and being
// slashed.
//
// The HASHLOCK and OPTIMISTIC bounds run in OPPOSITE directions and that is the
// substance, not an accident (D-094 R3): a signed HASHLOCK timeout is a MAXIMUM
// (it is when the buyer's locked payment refunds, so longer is worse) while a
// signed OPTIMISTIC window is a MINIMUM (it IS the buyer's time to inspect and
// dispute, so shorter is worse). A solver that mirrored one relation onto the
// other would pass half of this table.
func TestBuyerRelationsAreHonoredPerClass(t *testing.T) {
	hash := sha256.Sum256([]byte("receipt-of-access"))
	cases := []struct {
		name          string
		seller, buyer *escrowtypes.DeliveryCondition
		clears        bool
	}{
		{"HASHLOCK attached under the buyer's ceiling clears", hashlock(hash[:], 50), hashlock(nil, 100), true},
		{"HASHLOCK attached at the buyer's ceiling clears", hashlock(hash[:], 100), hashlock(nil, 100), true},
		{"HASHLOCK one block OVER the ceiling is dropped", hashlock(hash[:], 101), hashlock(nil, 100), false},

		{"OPTIMISTIC attached over the buyer's floor clears", optimistic(100), optimistic(50), true},
		{"OPTIMISTIC attached at the buyer's floor clears", optimistic(50), optimistic(50), true},
		{"OPTIMISTIC one block UNDER the floor is dropped", optimistic(49), optimistic(50), false},

		{"ZK matching commitment clears", zk("schema-v1"), zk("schema-v1"), true},
		{"ZK differing commitment is dropped", zk("schema-v1"), zk("schema-v2"), false},

		{"class mismatch is dropped", hashlock(hash[:], 50), optimistic(50), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := solver.Solve(conditionedPair(tc.seller, tc.buyer))
			if tc.clears {
				require.Len(t, got, 1, "the chain would accept this pair, so the reference must propose it")
				require.Equal(t, tc.seller, dataLegCondition(t, got[0]))
				return
			}
			require.Empty(t, got,
				"the chain would REJECT this pair, so the reference must leave it unmatched rather than be slashed for it")
		})
	}
}

// TestBuyerSignedButSellerDidNotClearsNothing is the asymmetric case that only
// exists because consent is checked for BOTH parties of every DataGrant leg. The
// buyer asked for protection and the seller signed none, so there is no
// condition the reference could attach that both would accept: attaching is 1427
// against the seller, dropping is 1429 against the buyer. The only correct move
// is no fill.
func TestBuyerSignedButSellerDidNotClearsNothing(t *testing.T) {
	require.Empty(t, solver.Solve(conditionedPair(nil, optimistic(50))),
		"a buyer-signed condition the seller never agreed to cannot clear at all")
}

// TestUnconditionedPairIsUntouched is the other direction, and it is what makes
// the whole change ADDITIVE: a pair that signed nothing must produce exactly the
// match it produced before 2.14, with a bare DataGrant leg. A rule that attached
// something to everything would pass every test above.
func TestUnconditionedPairIsUntouched(t *testing.T) {
	got := solver.Solve(conditionedPair(nil, nil))
	require.Len(t, got, 1, "an unconditioned data-for-coin pair still clears")
	require.Nil(t, dataLegCondition(t, got[0]), "and its DataGrant leg carries no condition")
}

// ---------------------------------------------------------------------------
// FW-2 — the per-leg refinement, and why the resolver is CALLED not COPIED
// ---------------------------------------------------------------------------

// TestPerLegConditionRefinesPayloadLevel is the single most load-bearing test
// for the §2b decision to call keeper.EffectiveSellerCondition rather than
// mirror it. The seller's payload-level field says OPTIMISTIC while its DataGrant
// give leg refines that to HASHLOCK (IntentLeg.condition, FW-2/D-094), and the
// chain resolves the per-leg field FIRST.
//
// A reference solver that read oi.Condition directly — the obvious, wrong,
// one-line implementation — attaches the OPTIMISTIC condition, the chain
// resolves HASHLOCK, and the solution is rejected 1427 and slashed. This test
// goes red under exactly that mistake, which is what makes "call the chain's
// resolver" an executable claim instead of an argument.
func TestPerLegConditionRefinesPayloadLevel(t *testing.T) {
	hash := sha256.Sum256([]byte("receipt-of-access"))
	perLeg := hashlock(hash[:], 50)

	intents := conditionedPair(optimistic(999), perLeg)
	// refine the SELLER's data give leg; its payload-level field stays OPTIMISTIC.
	intents[0].Give[0].Condition = perLeg

	got := solver.Solve(intents)
	require.Len(t, got, 1, "the refined pair clears")
	require.Equal(t, perLeg, dataLegCondition(t, got[0]),
		"the PER-LEG condition governs; reading the payload-level field would attach the wrong one")
}

// TestPerLegEmptyConditionDoesNotExempt mirrors the chain's fail-closed reading
// (D-094): a per-leg field that is PRESENT but EMPTY is not "this leg needs no
// protection" — it falls back to the payload-level condition like an absent one.
// Refinement may only ever ADD a condition, never remove one.
func TestPerLegEmptyConditionDoesNotExempt(t *testing.T) {
	signed := optimistic(50)
	intents := conditionedPair(signed, signed)
	intents[0].Give[0].Condition = &escrowtypes.DeliveryCondition{} // present, empty oneof

	got := solver.Solve(intents)
	require.Len(t, got, 1)
	require.Equal(t, signed, dataLegCondition(t, got[0]),
		"an empty per-leg message must NOT exempt the leg from the payload-level condition")
}

// ---------------------------------------------------------------------------
// D-074 — conditioned legs may not enter an n-party cycle
// ---------------------------------------------------------------------------

// conditionedCycle is the §1.3 three-party cycle with a condition signed on its
// dataset leg: alice 100 uusdc → D ; bob D → 10 cmpt ; carol 10 cmpt → 100 uusdc.
// Bob is the dataset seller and signs an OPTIMISTIC condition.
func conditionedCycle(sellerSigned *escrowtypes.DeliveryCondition) []match.OpenIntent {
	alice := oi("alice", "sigil-alice", coinLeg("uusdc", 100), dataLeg(attachDataset))
	bob := oi("bob", "sigil-bob", dataLeg(attachDataset), svcLeg("cmpt", 10))
	bob.Condition = sellerSigned
	carol := oi("carol", "sigil-carol", svcLeg("cmpt", 10), coinLeg("uusdc", 100))
	return []match.OpenIntent{alice, bob, carol}
}

// TestConditionedIntentNeverEntersACycle is D-074 enforced from the solver side.
// The identical cycle clears when nobody signs a condition and clears NOTHING
// when its dataset seller does — because a conditioned leg is legal only in the
// two-party data-for-coin shape, so inside a cycle attaching is 1426 and
// dropping is 1429. Both moves are slashable; no fill is the only correct one.
func TestConditionedIntentNeverEntersACycle(t *testing.T) {
	require.Len(t, solver.Solve(conditionedCycle(nil)), 1,
		"the same cycle clears when no condition is signed — so the exclusion below is about the condition, not the shape")

	require.Empty(t, solver.Solve(conditionedCycle(optimistic(50))),
		"a condition-signing intent must not be settled through an n-party cycle")
}

// TestCycleEligibleWithholdsBothSides pins the withholding predicate directly.
// It is tested at the unit level ON PURPOSE: CycleEligible is an efficiency and
// liveness guard, not the guarantee — gate.AcceptConditionShape is — so its
// removal is NOT observable through the pipeline, and asserting it via
// solver.Solve would be asserting the gate instead while believing it was
// asserting this. That is exactly the D-089 mistake (the allow_partial candidate
// gates were believed to be the fill-or-kill guarantee and were not), so this
// suite states the distinction rather than reproducing it.
func TestCycleEligibleWithholdsBothSides(t *testing.T) {
	hash := sha256.Sum256([]byte("x"))
	seller := oi("s", "sigil-s", dataLeg(attachDataset), coinLeg("uusdc", 100))
	buyer := oi("b", "sigil-b", coinLeg("uusdc", 100), dataLeg(attachDataset))
	plain := oi("p", "sigil-p", coinLeg("uusdc", 5), svcLeg("cmpt", 1))

	t.Run("payload-level condition on the SELLER side is withheld", func(t *testing.T) {
		s := seller
		s.Condition = hashlock(hash[:], 50)
		require.True(t, condition.SignsDataCondition(s))
		require.Len(t, condition.CycleEligible([]match.OpenIntent{s, plain}), 1)
	})
	t.Run("payload-level condition on the BUYER side is withheld too", func(t *testing.T) {
		b := buyer
		b.Condition = optimistic(50)
		require.True(t, condition.SignsDataCondition(b),
			"the chain checks consent for the buyer of every DataGrant leg, so a buyer-side condition is equally binding")
		require.Len(t, condition.CycleEligible([]match.OpenIntent{b, plain}), 1)
	})
	t.Run("per-leg condition is withheld", func(t *testing.T) {
		s := oi("s2", "sigil-s2", dataLeg(attachDataset), coinLeg("uusdc", 100))
		s.Give = []veiltypes.IntentLeg{dataLeg(attachDataset)}
		s.Give[0].Condition = optimistic(50)
		require.True(t, condition.SignsDataCondition(s))
	})
	t.Run("a coin-only intent with a payload condition is NOT withheld", func(t *testing.T) {
		require.False(t, condition.SignsDataCondition(plain),
			"no data leg, so no condition can govern anything — withholding it would cost liveness for nothing")
		c := plain
		c.Condition = optimistic(50)
		require.False(t, condition.SignsDataCondition(c))
	})
	t.Run("an unconditioned intent is eligible", func(t *testing.T) {
		require.False(t, condition.SignsDataCondition(seller))
		require.Len(t, condition.CycleEligible([]match.OpenIntent{seller, buyer, plain}), 3)
	})
}

// bothSigningConditionedCycle is the fixture that ISOLATES the D-074 shape rule
// from the consent rule, and it exists because the obvious fixture cannot.
//
// In conditionedCycle only the dataset SELLER signs, so a conditioned cycle is
// rejected by the CONSENT predicate (1427 — the buyer never signed it) before
// the shape rule is ever the reason. Here the dataset buyer (alice) signs the
// SAME condition on her want leg, so consent passes for both parties and the
// D-074 shape is the ONLY thing standing between the solver and a slashed
// solution. Without a fixture like this, "the shape gate is load-bearing" is an
// untested claim wearing a passing test.
func bothSigningConditionedCycle(signed *escrowtypes.DeliveryCondition) []match.OpenIntent {
	alice := oi("alice", "sigil-alice", coinLeg("uusdc", 100), dataLeg(attachDataset))
	alice.Condition = signed
	bob := oi("bob", "sigil-bob", dataLeg(attachDataset), svcLeg("cmpt", 10))
	bob.Condition = signed
	carol := oi("carol", "sigil-carol", svcLeg("cmpt", 10), coinLeg("uusdc", 100))
	return []match.OpenIntent{alice, bob, carol}
}

// TestConsentedConditionInACycleIsStillRejected is the shape rule standing
// alone. Both parties to the dataset leg signed the identical condition, so
// every consent question answers YES — and the match is STILL invalid, because
// payment-into-escrow needs one unambiguous coin payment against one deliverable
// and a three-party cycle does not have one (D-074).
//
// This is the case that makes gate.AcceptConditionShape a guarantee rather than
// a redundancy: consent and shape are independent, and only this fixture shows
// it.
func TestConsentedConditionInACycleIsStillRejected(t *testing.T) {
	signed := optimistic(50)
	intents := bothSigningConditionedCycle(signed)

	require.Empty(t, solver.Solve(intents),
		"a fully-consented condition still cannot settle through an n-party cycle")

	// and the reason is the SHAPE, not consent — asserted separately so the two
	// rules cannot be confused for one another.
	groups := cycles.Find(intents) // deliberately unfiltered
	require.Len(t, groups, 1)
	m := groups[0][0]
	parts := condition.Participants(m, byIDOf(intents))
	condition.Attach(&m, parts)
	require.NoError(t, gate.AcceptConsent(m, condition.PayloadFor(parts)),
		"both parties signed it, so consent is satisfied")
	require.ErrorIs(t, gate.AcceptConditionShape(m), sessiontypes.ErrConditionUnsupportedMatch,
		"and the D-074 shape rule is what rejects it")
}

// TestBothMovesInACycleAreSlashable is the executable form of the claim
// CycleEligible's doc rests on — that a condition-signing intent inside an
// n-party cycle has NO non-slashable move — and it is also the direct assertion
// of gate.AcceptConditionShape, which the pipeline tests cannot reach because
// CycleEligible withholds the input before it can fire.
//
// Both halves assert the SPECIFIC error, and they are different errors on
// purpose: that is what proves these are two distinct rules rather than one
// checked twice.
func TestBothMovesInACycleAreSlashable(t *testing.T) {
	intents := conditionedCycle(optimistic(50))
	byID := byIDOf(intents)

	t.Run("ATTACHING is ErrConditionUnsupportedMatch (1426)", func(t *testing.T) {
		groups := cycles.Find(intents) // deliberately unfiltered
		require.Len(t, groups, 1, "the cycle exists; only CycleEligible keeps it out of the pipeline")
		m := groups[0][0]
		parts := condition.Participants(m, byID)
		condition.Attach(&m, parts)
		require.NotNil(t, dataLegCondition(t, m), "precondition: the condition really was attached")
		require.ErrorIs(t, gate.AcceptConditionShape(m), sessiontypes.ErrConditionUnsupportedMatch,
			"D-074 permits a conditioned leg only in a two-party data-for-coin match")
	})

	t.Run("DROPPING is ErrConditionNotHonored (1429)", func(t *testing.T) {
		groups := cycles.Find(intents) // fresh: Attach above mutated the other copy
		m := groups[0][0]
		require.Nil(t, dataLegCondition(t, m), "precondition: nothing attached")
		parts := condition.Participants(m, byID)
		require.ErrorIs(t, gate.AcceptConsent(m, condition.PayloadFor(parts)),
			sessiontypes.ErrConditionNotHonored,
			"a signed condition is a REQUIREMENT since task 2.7, so omitting it is not the safe option")
	})
}

// TestCyclesStillClearUnconditioned guards the additivity of the withholding:
// every pre-2.14 cycle fixture must be untouched, since none of them signs a
// condition.
func TestCyclesStillClearUnconditioned(t *testing.T) {
	require.Len(t, cycles.Find(condition.CycleEligible(representableCycle())), 1)
	require.Len(t, condition.CycleEligible(representableCycle()), 3)
	require.Len(t, solver.Solve(representableCycle()), 1)
}
