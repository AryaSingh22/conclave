package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/crypto/zkdelivery"
	"github.com/AryaSingh22/conclave/crypto/zkdelivery/zktest"
	escrowtypes "github.com/AryaSingh22/conclave/x/escrow/types"
	"github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// This file is task 2.7's gate. The determinism vector covers NONE of it: the
// simulation's intent generator never sets IntentPayload.condition or the new
// IntentLeg.condition (zero occurrences of either in x/session/simulation or
// solver/, test or non-test), so every path below is structurally unreachable by
// the vector no matter how many seeds it runs — a Category-1 coverage shadow in
// the D-093 taxonomy, whose remedy is a wider generator (PHASE3_PLAN 3.5), not
// more seeds. These hand-built fixtures ARE the primary gate; the vector is a
// regression backstop only.

func hashlockCond(hash []byte, timeout uint64) *escrowtypes.DeliveryCondition {
	return &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_Hashlock{
		Hashlock: &escrowtypes.HashlockCondition{PayloadHash: hash, TimeoutBlocks: timeout},
	}}
}

func optimisticCond(window uint64) *escrowtypes.DeliveryCondition {
	return &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_Optimistic{
		Optimistic: &escrowtypes.OptimisticCondition{DisputeWindowBlocks: window},
	}}
}

func zkCond(seed string) *escrowtypes.DeliveryCondition {
	return &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_ZkDelivery{
		ZkDelivery: &escrowtypes.ZkDeliveryCondition{
			SchemaCommitment: zktest.SchemaCommitment([]byte(seed)), VkId: zkdelivery.VKIDDeliveryV1,
		},
	}}
}

// ---------------------------------------------------------------------------
// THE DOWNGRADE — `signed ⇒ attached`, the direction enforced NOWHERE before 2.7
// ---------------------------------------------------------------------------

// TestDowngradeRejected_EveryClassEverySigner is the requirement direction over
// the full product it has to hold on: three condition classes × which party
// signed × both chokepoints. One case passing proves nothing about the others —
// the pre-2.7 hole existed for every one of them at once, and a fix that closed
// only the ZK case (the one D-093 demonstrated) would read as closed while
// leaving HASHLOCK and OPTIMISTIC open.
//
// Each case asserts ErrConditionNotHonored SPECIFICALLY. It is a different error
// from ErrConditionNotConsented on purpose: 1429 means "you dropped what I
// signed" and 1427 means "you attached what I did not sign", so a test that
// confused the two directions could not pass.
func TestDowngradeRejected_EveryClassEverySigner(t *testing.T) {
	cases := []struct {
		name                     string
		buyerSigned, sellerSigne *escrowtypes.DeliveryCondition
	}{
		{"HASHLOCK signed by the seller", nil, hashlockCond(contentHash("p"), 50)},
		{"HASHLOCK signed by the buyer", hashlockCond(nil, 50), nil},
		{"OPTIMISTIC signed by the seller", nil, optimisticCond(50)},
		{"OPTIMISTIC signed by the buyer", optimisticCond(50), nil},
		{"ZK signed by the seller", nil, zkCond("s")},
		{"ZK signed by the buyer", zkCond("s"), nil},
		{"ZK signed by BOTH", zkCond("s"), zkCond("s")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// (a) solution validation
			f := setupKeeper(t)
			b := f.setupBilateralBoth(t, tc.buyerSigned, tc.sellerSigne)
			solver := randAddr()
			f.registerSolver(t, solver, minSolverBond())
			err := f.propose(t, solver, b.sessionID, b.match(nil))
			require.ErrorIs(t, err, types.ErrConditionNotHonored)

			// (b) the settlement boundary, independently — Conclude does not
			// re-run validateSolution, so a guard only at (a) would be skippable.
			f2 := setupKeeper(t)
			b2 := f2.setupBilateralBoth(t, tc.buyerSigned, tc.sellerSigne)
			require.ErrorIs(t, f2.k.Conclude(f2.ctx, b2.sessionID, b2.match(nil)), types.ErrConditionNotHonored)
			require.Equal(t, 0, countEscrows(t, f2),
				"the record-only shell that could be mistaken for protection is not created either")
		})
	}
}

// TestUnconditionedPairStillClears is the other side of the rule, and it is
// load-bearing in the opposite way: a requirement rule that rejected everything
// would also pass every test above. A pair who signed NOTHING must settle
// exactly as it did before 2.7 — record-only escrow, payment to the seller.
func TestUnconditionedPairStillClears(t *testing.T) {
	f := setupKeeper(t)
	b := f.setupBilateral(t, nil) // neither party signed a condition
	sellerBefore := f.bank.balanceOf(b.seller.String()).AmountOf(usdcDenom)

	require.NoError(t, f.k.Conclude(f.ctx, b.sessionID, b.match(nil)))

	esc := mustEscrow(t, f)
	require.Nil(t, esc.Condition.Condition, "an unconditioned DataGrant still opens the SPEC §6.4 record-only entry")
	require.True(t, esc.Amount.IsZero())
	require.Equal(t, sellerBefore.Add(b.payment.Amount), f.bank.balanceOf(b.seller.String()).AmountOf(usdcDenom),
		"and its payment still settles directly — 2.7 changes nothing for a pair that signed nothing")
}

// ---------------------------------------------------------------------------
// FW-1(a) — the buyer's ATTACH consent for HASHLOCK / OPTIMISTIC
// ---------------------------------------------------------------------------

// TestConcludeConditioned_Hashlock_BuyerNotConsented preserves the coverage the
// TestConcludeConditioned_Hashlock fixture migration would otherwise have
// deleted. That test used to pass with the buyer signing NONE; under
// D-075-FW-1(a) that exact shape is now a rejection, and this pins it. The
// buyer's exposure is real though milder than the seller's: its payment is
// escrowed behind a timeline it never agreed to (D-075's "imposed timeline").
func TestConcludeConditioned_Hashlock_BuyerNotConsented(t *testing.T) {
	f := setupKeeper(t)
	cond := hashlockCond(contentHash("preimage"), 50)
	b := f.setupBilateral(t, cond) // seller signs; buyer signed NONE

	err := f.k.Conclude(f.ctx, b.sessionID, b.match(cond))
	require.ErrorIs(t, err, types.ErrConditionNotConsented)
	require.Equal(t, 0, countEscrows(t, f), "no escrow opened against a non-consenting buyer")
}

// TestConcludeConditioned_Optimistic_BuyerNotConsented is the same pin for
// OPTIMISTIC, where the imposition also delays the buyer's finality by a dispute
// window it never signed — and, unlike HASHLOCK, drags the seller's bond in too.
func TestConcludeConditioned_Optimistic_BuyerNotConsented(t *testing.T) {
	f := setupKeeper(t)
	cond := optimisticCond(50)
	b := f.setupBilateral(t, cond) // seller signs; buyer signed NONE
	bond := escrowtypes.SellerBondCoins(sdk.NewCoins(b.payment))
	f.bank.fund(b.seller.String(), bond)

	err := f.k.Conclude(f.ctx, b.sessionID, b.match(cond))
	require.ErrorIs(t, err, types.ErrConditionNotConsented)
	require.Equal(t, 0, countEscrows(t, f), "no escrow opened against a non-consenting buyer")
	require.Equal(t, bond.AmountOf(usdcDenom), f.bank.balanceOf(b.seller.String()).AmountOf(usdcDenom),
		"and the seller's bond is not debited for a match that cannot clear")
}

// ---------------------------------------------------------------------------
// R3 — the per-class buyer relation, at its boundaries
// ---------------------------------------------------------------------------

// TestBuyerRelation_HashlockTimeoutIsACeiling pins the direction of the HASHLOCK
// bound at the boundary value itself. The timeout is when the buyer's LOCKED
// payment auto-refunds, so a longer one is strictly worse for it: the signed
// value is a MAXIMUM. Equal must pass and one block over must fail — an
// off-by-one in the comparison flips exactly one of these.
func TestBuyerRelation_HashlockTimeoutIsACeiling(t *testing.T) {
	hash := contentHash("preimage")
	t.Run("attached == signed ceiling: accepted", func(t *testing.T) {
		f := setupKeeper(t)
		b := f.setupBilateralBoth(t, hashlockCond(nil, 50), hashlockCond(hash, 50))
		require.NoError(t, f.k.Conclude(f.ctx, b.sessionID, b.match(hashlockCond(hash, 50))))
		require.Equal(t, 1, countEscrows(t, f))
	})
	t.Run("attached one block OVER the ceiling: rejected", func(t *testing.T) {
		f := setupKeeper(t)
		b := f.setupBilateralBoth(t, hashlockCond(nil, 50), hashlockCond(hash, 51))
		err := f.k.Conclude(f.ctx, b.sessionID, b.match(hashlockCond(hash, 51)))
		require.ErrorIs(t, err, types.ErrConditionNotConsented)
		require.ErrorContains(t, err, "stay locked longer than it agreed")
	})
	t.Run("attached UNDER the ceiling: accepted (strictly better for the buyer)", func(t *testing.T) {
		f := setupKeeper(t)
		b := f.setupBilateralBoth(t, hashlockCond(nil, 50), hashlockCond(hash, 10))
		require.NoError(t, f.k.Conclude(f.ctx, b.sessionID, b.match(hashlockCond(hash, 10))))
	})
}

// TestBuyerRelation_OptimisticWindowIsAFloor pins the OPPOSITE direction, and
// the pair of tests together is the real assertion: the dispute window IS the
// buyer's protection — its time to inspect and dispute before funds release to
// the seller — so a SHORTER window is worse and the signed value is a MINIMUM.
// A single "windows must match" or "at most" rule would break one of these two
// tests, which is why both exist.
func TestBuyerRelation_OptimisticWindowIsAFloor(t *testing.T) {
	fund := func(f *fixture, b bilateral) {
		f.bank.fund(b.seller.String(), escrowtypes.SellerBondCoins(sdk.NewCoins(b.payment)))
	}
	t.Run("attached == signed floor: accepted", func(t *testing.T) {
		f := setupKeeper(t)
		b := f.setupBilateralBoth(t, optimisticCond(50), optimisticCond(50))
		fund(f, b)
		require.NoError(t, f.k.Conclude(f.ctx, b.sessionID, b.match(optimisticCond(50))))
		require.Equal(t, 1, countEscrows(t, f))
	})
	t.Run("attached one block UNDER the floor: rejected", func(t *testing.T) {
		f := setupKeeper(t)
		b := f.setupBilateralBoth(t, optimisticCond(50), optimisticCond(49))
		fund(f, b)
		err := f.k.Conclude(f.ctx, b.sessionID, b.match(optimisticCond(49)))
		require.ErrorIs(t, err, types.ErrConditionNotConsented)
		require.ErrorContains(t, err, "less time to inspect and dispute than it agreed")
	})
	t.Run("attached OVER the floor: accepted (strictly better for the buyer)", func(t *testing.T) {
		f := setupKeeper(t)
		b := f.setupBilateralBoth(t, optimisticCond(50), optimisticCond(500))
		fund(f, b)
		require.NoError(t, f.k.Conclude(f.ctx, b.sessionID, b.match(optimisticCond(500))))
	})
}

// TestBuyerRelation_HashlockPayloadHash covers the parameter a buyer usually
// CANNOT pre-commit: the preimage receipt token is generated by the seller's
// delivery, so an empty one means UNCONSTRAINED. A buyer that does know it out
// of band may pin it, and pinning may only ever TIGHTEN the bound.
func TestBuyerRelation_HashlockPayloadHash(t *testing.T) {
	sellerHash := contentHash("the real preimage")
	t.Run("empty buyer hash is unconstrained", func(t *testing.T) {
		f := setupKeeper(t)
		b := f.setupBilateralBoth(t, hashlockCond(nil, 50), hashlockCond(sellerHash, 50))
		require.NoError(t, f.k.Conclude(f.ctx, b.sessionID, b.match(hashlockCond(sellerHash, 50))))
	})
	t.Run("a pinned buyer hash must match", func(t *testing.T) {
		f := setupKeeper(t)
		b := f.setupBilateralBoth(t, hashlockCond(contentHash("some other"), 50), hashlockCond(sellerHash, 50))
		err := f.k.Conclude(f.ctx, b.sessionID, b.match(hashlockCond(sellerHash, 50)))
		require.ErrorIs(t, err, types.ErrConditionNotConsented)
		require.ErrorContains(t, err, "pinned a HASHLOCK payload_hash")
	})
}

// TestBuyerRelation_ClassMismatchRejected proves the relation is not merely a
// numeric comparison: a buyer that signed one class has not consented to
// another, however generous the other's parameters look.
func TestBuyerRelation_ClassMismatchRejected(t *testing.T) {
	f := setupKeeper(t)
	b := f.setupBilateralBoth(t, optimisticCond(50), hashlockCond(contentHash("p"), 1))
	err := f.k.Conclude(f.ctx, b.sessionID, b.match(hashlockCond(contentHash("p"), 1)))
	require.ErrorIs(t, err, types.ErrConditionNotConsented)
	require.ErrorContains(t, err, "signed an OPTIMISTIC condition but the settlement attaches a HASHLOCK one")
}

// TestSellerRelation_StaysExact proves 2.7 did NOT loosen the seller side while
// building the buyer's relation. The seller can pre-commit every parameter, so
// its consent stays exact equality (D-075) — a window it did not sign is
// rejected even though the same value would satisfy a buyer's floor.
func TestSellerRelation_StaysExact(t *testing.T) {
	f := setupKeeper(t)
	b := f.setupBilateralBoth(t, optimisticCond(20), optimisticCond(50))
	f.bank.fund(b.seller.String(), escrowtypes.SellerBondCoins(sdk.NewCoins(b.payment)))
	err := f.k.Conclude(f.ctx, b.sessionID, b.match(optimisticCond(500)))
	require.ErrorIs(t, err, types.ErrConditionNotConsented)
	require.ErrorContains(t, err, "seller")
}

// ---------------------------------------------------------------------------
// FW-2 — the per-leg IntentLeg.condition field has a READER
// ---------------------------------------------------------------------------

// TestPerLegConditionOverridesPayloadLevel is the Rule-4 test for the new proto
// field: it names the behaviour that changes when the field differs. Without the
// effectiveCondition resolver the field would be signed and read by nobody —
// exactly the D-049/F-7 defect this task exists to stop repeating.
//
// The seller's payload-level condition says OPTIMISTIC(50); its data GIVE leg
// says HASHLOCK. The per-leg field must win, so a settlement carrying the
// payload-level one is rejected and one carrying the per-leg one is accepted.
func TestPerLegConditionOverridesPayloadLevel(t *testing.T) {
	perLeg := hashlockCond(contentHash("p"), 50)
	payloadLevel := optimisticCond(50)

	build := func(f *fixture) bilateral {
		b := bilateral{dataHash: contentHash("dataset-cond"), payment: sdk.NewInt64Coin(usdcDenom, 100_000_000)}
		b.buyer, b.seller = randAddr(), randAddr()
		b.buyerSigil = f.registerSigil(t, b.buyer)
		b.sellerSigil = f.registerSigil(t, b.seller)
		f.bank.fund(b.buyer.String(), sdk.NewCoins(b.payment))
		f.bank.fund(b.seller.String(), escrowtypes.SellerBondCoins(sdk.NewCoins(b.payment)))

		// the seller's GIVE leg carries its own condition, overriding the payload
		sellerGive := dataLeg(b.dataHash)
		sellerGive.Condition = perLeg
		// the buyer's WANT leg does the same, so both sides resolve per-leg
		buyerWant := dataLeg(b.dataHash)
		buyerWant.Condition = hashlockCond(nil, 100) // a ceiling the attached 50 clears

		b.buyerID = f.submitOpenCond(t, b.buyer, b.buyerSigil,
			[]veiltypes.IntentLeg{coinLeg(b.payment)}, []veiltypes.IntentLeg{buyerWant}, 100, payloadLevel)
		b.sellerID = f.submitOpenCond(t, b.seller, b.sellerSigil,
			[]veiltypes.IntentLeg{sellerGive}, []veiltypes.IntentLeg{coinLeg(b.payment)}, 100, payloadLevel)
		b.sessionID = f.openSession(t)
		return b
	}

	t.Run("the PER-LEG condition is what must be attached", func(t *testing.T) {
		f := setupKeeper(t)
		b := build(f)
		require.NoError(t, f.k.Conclude(f.ctx, b.sessionID, b.match(perLeg)))
		esc := mustEscrow(t, f)
		require.NotNil(t, esc.Condition.GetHashlock(),
			"the escrow opened under the per-leg condition, not the payload-level one")
	})
	t.Run("the PAYLOAD-LEVEL condition is no longer what governs that leg", func(t *testing.T) {
		f := setupKeeper(t)
		b := build(f)
		err := f.k.Conclude(f.ctx, b.sessionID, b.match(payloadLevel))
		require.ErrorIs(t, err, types.ErrConditionNotConsented)
	})
}

// TestPerLegEmptyConditionDoesNotExemptALeg pins the fail-CLOSED reading of a
// present-but-empty per-leg field (the recorded limitation on effectiveCondition):
// it falls back to the payload-level condition rather than exempting the leg.
// Reading "empty submessage" as "this leg needs no protection" would be a
// fail-OPEN default a client emitting empty messages could trip into silently.
func TestPerLegEmptyConditionDoesNotExemptALeg(t *testing.T) {
	f := setupKeeper(t)
	payloadLevel := zkCond("s")

	b := bilateral{dataHash: contentHash("dataset-cond"), payment: sdk.NewInt64Coin(usdcDenom, 100_000_000)}
	b.buyer, b.seller = randAddr(), randAddr()
	b.buyerSigil = f.registerSigil(t, b.buyer)
	b.sellerSigil = f.registerSigil(t, b.seller)
	f.bank.fund(b.buyer.String(), sdk.NewCoins(b.payment))

	sellerGive := dataLeg(b.dataHash)
	sellerGive.Condition = &escrowtypes.DeliveryCondition{} // present, empty oneof
	b.buyerID = f.submitOpenCond(t, b.buyer, b.buyerSigil,
		[]veiltypes.IntentLeg{coinLeg(b.payment)}, []veiltypes.IntentLeg{dataLeg(b.dataHash)}, 100, payloadLevel)
	b.sellerID = f.submitOpenCond(t, b.seller, b.sellerSigil,
		[]veiltypes.IntentLeg{sellerGive}, []veiltypes.IntentLeg{coinLeg(b.payment)}, 100, payloadLevel)
	b.sessionID = f.openSession(t)

	require.ErrorIs(t, f.k.Conclude(f.ctx, b.sessionID, b.match(nil)), types.ErrConditionNotHonored,
		"an empty per-leg field does not exempt the leg from the payload-level condition")
}

// ---------------------------------------------------------------------------
// R2 — the fallback as attacher, and its affordability pre-check
// ---------------------------------------------------------------------------

// TestFallbackLeavesUnconsentedPairUnmatched is the negative half of R2. The
// fallback attaches the SELLER's signed condition and then faces the same
// validateSolution bar as any solver, so a buyer who never consented cannot be
// swept into the escrow — and, critically, the pair is left UNMATCHED rather
// than silently downgraded to the unconditioned clearing that used to happen.
func TestFallbackLeavesUnconsentedPairUnmatched(t *testing.T) {
	f := setupKeeper(t)
	b := f.setupBilateral(t, hashlockCond(contentHash("p"), 50)) // buyer signed NONE
	session, err := f.k.GetSession(f.ctx, 0, b.sessionID)
	require.NoError(t, err)
	params, err := f.k.GetParams(f.ctx)
	require.NoError(t, err)

	matches, err := f.k.buildFallbackMatches(f.ctx, session, params)
	require.NoError(t, err)
	require.Empty(t, matches,
		"the pair is mutually fillable but not mutually consenting: unmatched, never downgraded")
}

// TestFallbackSkipsUnaffordableConditionedPair covers the liveness hazard R2
// creates: concludeInner wraps every fallback match of a session in ONE cache,
// so a conditioned pair whose seller cannot post its OPTIMISTIC bond would void
// every OTHER pair's clearing in that session. The candidate is dropped at
// build time instead.
//
// The seller here is funded for nothing, so FundingRequirement's bond is
// unaffordable and the pair never becomes a match.
func TestFallbackSkipsUnaffordableConditionedPair(t *testing.T) {
	f := setupKeeper(t)
	cond := optimisticCond(50)
	b := f.setupBilateralBoth(t, cond, cond) // both consent; seller has NO bond funds
	session, err := f.k.GetSession(f.ctx, 0, b.sessionID)
	require.NoError(t, err)
	params, err := f.k.GetParams(f.ctx)
	require.NoError(t, err)

	matches, err := f.k.buildFallbackMatches(f.ctx, session, params)
	require.NoError(t, err)
	require.Empty(t, matches, "an unaffordable conditioned candidate is skipped, not admitted then failed")

	// funded, the identical pair DOES clear — proving the skip was about the
	// bond and not about some other property of the fixture.
	f.bank.fund(b.seller.String(), escrowtypes.SellerBondCoins(sdk.NewCoins(b.payment)))
	matches, err = f.k.buildFallbackMatches(f.ctx, session, params)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.NoError(t, f.k.Conclude(f.ctx, b.sessionID, matches))
	esc := mustEscrow(t, f)
	require.NotNil(t, esc.Condition.GetOptimistic())
}

// TestFundingRequirementIsTheSingleDefinition pins the D-085 shape structurally:
// the amount the fallback's pre-check reasons about is the amount CreateEscrow
// moves, because both read this one function. Only OPTIMISTIC carries a bond.
func TestFundingRequirementIsTheSingleDefinition(t *testing.T) {
	payment := sdk.NewCoins(sdk.NewInt64Coin(usdcDenom, 100_000_000))

	payer, payee := escrowtypes.FundingRequirement(escrowtypes.Escrow{Amount: payment, Condition: *optimisticCond(50)})
	require.Equal(t, payment, payer)
	require.Equal(t, escrowtypes.SellerBondCoins(payment), payee, "OPTIMISTIC posts a seller bond")

	payer, payee = escrowtypes.FundingRequirement(escrowtypes.Escrow{Amount: payment, Condition: *hashlockCond(nil, 50)})
	require.Equal(t, payment, payer)
	require.True(t, payee.IsZero(), "HASHLOCK release is preimage-gated, not bond-gated")

	payer, payee = escrowtypes.FundingRequirement(escrowtypes.Escrow{Amount: payment, Condition: *zkCond("s")})
	require.Equal(t, payment, payer)
	require.True(t, payee.IsZero(), "ZK release is proof-gated, not bond-gated")

	payer, payee = escrowtypes.FundingRequirement(escrowtypes.Escrow{Condition: escrowtypes.DeliveryCondition{}})
	require.True(t, payer.IsZero() && payee.IsZero(), "a NONE record-only grant costs neither side anything")
}
