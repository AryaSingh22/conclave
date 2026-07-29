package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/AryaSingh22/conclave/crypto/zkdelivery"
	"github.com/AryaSingh22/conclave/crypto/zkdelivery/zktest"
	escrowtypes "github.com/AryaSingh22/conclave/x/escrow/types"
	"github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// bilateral is a two-party data-for-coin fixture: the buyer gives usdc and
// wants data D; the seller gives data D and wants usdc. It is the exact shape a
// conditioned (HASHLOCK/OPTIMISTIC) escrow requires (D-074 bilateral pairing).
// The seller SIGNS `signed` as its IntentPayload.condition (D-075 consent); the
// SettleLeg condition must equal it for the escrow to open.
type bilateral struct {
	buyerSigil, sellerSigil string
	buyer, seller           sdk.AccAddress
	buyerID, sellerID       string
	dataHash                []byte
	sessionID               uint64
	payment                 sdk.Coin
}

// setupBilateral submits the two intents; `sellerSigned` is the condition the
// seller puts in its signed IntentPayload (nil = the seller signed NONE).
func (f *fixture) setupBilateral(t testingT, sellerSigned *escrowtypes.DeliveryCondition) bilateral {
	t.Helper()
	b := bilateral{dataHash: contentHash("dataset-cond"), payment: sdk.NewInt64Coin(usdcDenom, 100_000_000)}
	b.buyer, b.seller = randAddr(), randAddr()
	b.buyerSigil = f.registerSigil(t, b.buyer)
	b.sellerSigil = f.registerSigil(t, b.seller)
	f.bank.fund(b.buyer.String(), sdk.NewCoins(b.payment))

	b.buyerID = f.submitOpen(t, b.buyer, b.buyerSigil, []veiltypes.IntentLeg{coinLeg(b.payment)}, []veiltypes.IntentLeg{dataLeg(b.dataHash)}, 100, "")
	b.sellerID = f.submitOpenCond(t, b.seller, b.sellerSigil, []veiltypes.IntentLeg{dataLeg(b.dataHash)}, []veiltypes.IntentLeg{coinLeg(b.payment)}, 100, sellerSigned)
	b.sessionID = f.openSession(t)
	return b
}

// submitOpenCond is submitOpen with a signed IntentPayload.condition (D-075).
func (f *fixture) submitOpenCond(t testingT, owner sdk.AccAddress, sigilID string, give, want []veiltypes.IntentLeg, ttl uint64, cond *escrowtypes.DeliveryCondition) string {
	t.Helper()
	f.bank.fund(owner.String(), sdk.NewCoins(minBond()))
	p := veiltypes.IntentPayload{Give: give, Want: want, Condition: cond}
	bz, err := p.Marshal()
	require.NoError(t, err)
	msg := &veiltypes.MsgSubmitIntent{
		Sender:      owner.String(),
		SenderSigil: sigilID,
		Ciphertext:  bz,
		Envelope:    envelopeBytes(t, ttl, false, coinGiveValue(give)...),
		Bond:        minBond(),
	}
	id, err := f.veil.SubmitIntent(f.ctx, msg)
	require.NoError(t, err)
	return id
}

// setupBilateralBoth is setupBilateral with BOTH parties signing a condition —
// the shape a ZK_DELIVERY escrow requires (D-076 buyer consent: the buyer must
// sign the same condition as the seller, since ZK release is seller-driven on a
// schema not otherwise tied to the buyer's want).
func (f *fixture) setupBilateralBoth(t testingT, buyerSigned, sellerSigned *escrowtypes.DeliveryCondition) bilateral {
	t.Helper()
	b := bilateral{dataHash: contentHash("dataset-cond"), payment: sdk.NewInt64Coin(usdcDenom, 100_000_000)}
	b.buyer, b.seller = randAddr(), randAddr()
	b.buyerSigil = f.registerSigil(t, b.buyer)
	b.sellerSigil = f.registerSigil(t, b.seller)
	f.bank.fund(b.buyer.String(), sdk.NewCoins(b.payment))

	b.buyerID = f.submitOpenCond(t, b.buyer, b.buyerSigil, []veiltypes.IntentLeg{coinLeg(b.payment)}, []veiltypes.IntentLeg{dataLeg(b.dataHash)}, 100, buyerSigned)
	b.sellerID = f.submitOpenCond(t, b.seller, b.sellerSigil, []veiltypes.IntentLeg{dataLeg(b.dataHash)}, []veiltypes.IntentLeg{coinLeg(b.payment)}, 100, sellerSigned)
	b.sessionID = f.openSession(t)
	return b
}

// match builds the bilateral settlement with the given condition on the data
// leg (nil = NONE).
func (b bilateral) match(cond *escrowtypes.DeliveryCondition) []types.Match {
	dataLegSettle := types.SettleLeg{
		FromSigil: b.sellerSigil, ToSigil: b.buyerSigil,
		Asset:     &types.SettleLeg_Data{Data: &veiltypes.DataGrant{ContentHash: b.dataHash}},
		Condition: cond,
	}
	payLeg := types.SettleLeg{
		FromSigil: b.buyerSigil, ToSigil: b.sellerSigil,
		Asset: &types.SettleLeg_Coin{Coin: &b.payment},
	}
	return []types.Match{{
		IntentIds: []string{b.buyerID, b.sellerID},
		Legs:      []types.SettleLeg{dataLegSettle, payLeg},
		ClearingPrices: []types.PricePoint{
			{Base: usdcDenom, Quote: usdcDenom, Price: math.LegacyOneDec()},
			{Base: "data/" + types.IDHex(b.dataHash), Quote: usdcDenom, Price: math.LegacyNewDec(100_000_000)},
		},
	}}
}

func escrowModuleAddr() string { return authtypes.NewModuleAddress(escrowtypes.ModuleName).String() }

// TestConcludeConditioned_Hashlock proves the end-to-end handoff: a conditioned
// CONCLUDE routes the buyer's payment into x/escrow (not the seller) and opens a
// funded HASHLOCK escrow owned by x/escrow.
func TestConcludeConditioned_Hashlock(t *testing.T) {
	f := setupKeeper(t)
	cond := &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_Hashlock{
		Hashlock: &escrowtypes.HashlockCondition{PayloadHash: contentHash("preimage"), TimeoutBlocks: 50},
	}}
	b := f.setupBilateral(t, cond) // the seller SIGNS this exact condition

	require.NoError(t, f.k.Conclude(f.ctx, b.sessionID, b.match(cond)))

	require.Equal(t, 1, countEscrows(t, f))
	require.Equal(t, escrowtypes.ESCROW_STATE_ACTIVE, mustEscrow(t, f).State)
	esc := mustEscrow(t, f)
	require.Equal(t, b.buyerSigil, esc.PayerSigil, "buyer funds the escrow")
	require.Equal(t, b.sellerSigil, esc.PayeeSigil, "seller is paid on release")
	require.Equal(t, sdk.NewCoins(b.payment), esc.Amount, "payment escrowed")
	require.NotNil(t, esc.Condition.GetHashlock())

	// the payment is HELD by the escrow module, not delivered to the seller.
	// (the seller's wallet still holds its refunded uclv intent bond, D-041 —
	// so we assert the PAYMENT denom specifically.)
	require.Equal(t, sdk.NewCoins(b.payment), f.bank.balanceOf(escrowModuleAddr()))
	require.True(t, f.bank.balanceOf(b.seller.String()).AmountOf(usdcDenom).IsZero(), "seller not paid until release")
}

// TestConcludeConditioned_Optimistic proves the seller bond is posted into
// x/escrow at creation (on top of the buyer's escrowed payment).
func TestConcludeConditioned_Optimistic(t *testing.T) {
	f := setupKeeper(t)
	cond := &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_Optimistic{
		Optimistic: &escrowtypes.OptimisticCondition{DisputeWindowBlocks: 50},
	}}
	b := f.setupBilateral(t, cond) // the seller SIGNS this exact condition
	bond := escrowtypes.SellerBondCoins(sdk.NewCoins(b.payment))
	f.bank.fund(b.seller.String(), bond) // seller must fund the dispute bond

	require.NoError(t, f.k.Conclude(f.ctx, b.sessionID, b.match(cond)))

	esc := mustEscrow(t, f)
	require.Equal(t, escrowtypes.ESCROW_STATE_ACTIVE, esc.State)
	require.NotNil(t, esc.Condition.GetOptimistic())
	// escrow holds payment + seller bond; the seller's uusdc bond wallet is
	// drained (its refunded uclv intent bond remains, D-041).
	require.Equal(t, sdk.NewCoins(b.payment).Add(bond...), f.bank.balanceOf(escrowModuleAddr()))
	require.True(t, f.bank.balanceOf(b.seller.String()).AmountOf(usdcDenom).IsZero(), "seller bond posted")
}

// TestConcludeConditioned_Zk is the end-to-end ZK_DELIVERY path (D-076): a
// conditioned CONCLUDE opens a funded ZK escrow (buyer payment in, NO seller
// bond), and a groth16 proof bound to THAT escrow instance releases it to the
// seller — the §6 demo's "ZK-verified data" leg, exercised through the real
// CONCLUDE→escrow handoff and the real verifier.
func TestConcludeConditioned_Zk(t *testing.T) {
	f := setupKeeper(t)
	payload := []byte("delivered ciphertext digest")
	schema := zktest.SchemaCommitment(payload)
	cond := &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_ZkDelivery{
		ZkDelivery: &escrowtypes.ZkDeliveryCondition{SchemaCommitment: schema, VkId: zkdelivery.VKIDDeliveryV1},
	}}
	b := f.setupBilateralBoth(t, cond, cond) // BOTH parties SIGN this exact ZK condition (D-076)

	require.NoError(t, f.k.Conclude(f.ctx, b.sessionID, b.match(cond)))

	esc := mustEscrow(t, f)
	require.Equal(t, escrowtypes.ESCROW_STATE_ACTIVE, esc.State)
	require.NotNil(t, esc.Condition.GetZkDelivery())
	require.Equal(t, sdk.NewCoins(b.payment), esc.Amount, "buyer payment escrowed")
	// ZK has no seller bond → the escrow account holds exactly the payment.
	require.Equal(t, sdk.NewCoins(b.payment), f.bank.balanceOf(escrowModuleAddr()))

	// a proof bound to THIS escrow id releases it to the seller.
	idBz, err := escrowtypes.IDBytes(esc.Id)
	require.NoError(t, err)
	proof, err := zktest.Prove(idBz, schema, payload)
	require.NoError(t, err)
	require.NoError(t, f.escrow.ReleaseZk(f.ctx, esc, proof))

	released, err := f.escrow.GetEscrow(f.ctx, 0, esc.Id)
	require.NoError(t, err)
	require.Equal(t, escrowtypes.ESCROW_STATE_RELEASED, released.State)
	require.Equal(t, b.payment.Amount, f.bank.balanceOf(b.seller.String()).AmountOf(usdcDenom), "seller paid on proof release")
}

// TestConcludeConditioned_Zk_BuyerNotConsented is the load-bearing buyer-side
// consent watch (D-076, outcome B): the SELLER signs a ZK condition but the
// BUYER signed a PLAIN intent (NONE). The settlement-boundary guard must REJECT
// it — no escrow opened, the buyer's payment not routed into a release gate it
// never agreed to. If the buyer-side gate were removed, the escrow would open
// and this test would fail red (it is the buyer-side mirror of
// TestConcludeConditioned_UnsignedConditionRejected).
func TestConcludeConditioned_Zk_BuyerNotConsented(t *testing.T) {
	f := setupKeeper(t)
	cond := &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_ZkDelivery{
		ZkDelivery: &escrowtypes.ZkDeliveryCondition{SchemaCommitment: zktest.SchemaCommitment([]byte("p")), VkId: zkdelivery.VKIDDeliveryV1},
	}}
	b := f.setupBilateral(t, cond) // seller signs the ZK cond; buyer signed NONE

	err := f.k.Conclude(f.ctx, b.sessionID, b.match(cond))
	require.ErrorIs(t, err, types.ErrConditionNotConsented)
	require.Equal(t, 0, countEscrows(t, f), "no ZK escrow opened for a non-consenting buyer")
}

// TestConcludeConditioned_Zk_BuyerMismatch proves the buyer gate binds the FULL
// params: both sign, but the buyer signed a DIFFERENT schema_commitment than the
// one settled — the buyer did not agree to release against THIS schema.
func TestConcludeConditioned_Zk_BuyerMismatch(t *testing.T) {
	f := setupKeeper(t)
	sellerCond := &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_ZkDelivery{
		ZkDelivery: &escrowtypes.ZkDeliveryCondition{SchemaCommitment: zktest.SchemaCommitment([]byte("seller-schema")), VkId: zkdelivery.VKIDDeliveryV1},
	}}
	buyerCond := &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_ZkDelivery{
		ZkDelivery: &escrowtypes.ZkDeliveryCondition{SchemaCommitment: zktest.SchemaCommitment([]byte("buyer-wanted-other")), VkId: zkdelivery.VKIDDeliveryV1},
	}}
	b := f.setupBilateralBoth(t, buyerCond, sellerCond)

	err := f.k.Conclude(f.ctx, b.sessionID, b.match(sellerCond))
	require.ErrorIs(t, err, types.ErrConditionNotConsented)
	require.Equal(t, 0, countEscrows(t, f))
}

// TestProposeConditioned_Zk_BuyerNotConsentedAtValidation proves the buyer gate
// also fires at SOLUTION VALIDATION (propose time): a solver proposing a ZK
// condition the buyer never signed has its solution rejected as invalid.
func TestProposeConditioned_Zk_BuyerNotConsentedAtValidation(t *testing.T) {
	f := setupKeeper(t)
	cond := &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_ZkDelivery{
		ZkDelivery: &escrowtypes.ZkDeliveryCondition{SchemaCommitment: zktest.SchemaCommitment([]byte("p")), VkId: zkdelivery.VKIDDeliveryV1},
	}}
	b := f.setupBilateral(t, cond) // seller signs; buyer signed NONE
	solver := randAddr()
	f.registerSolver(t, solver, minSolverBond())

	err := f.propose(t, solver, b.sessionID, b.match(cond))
	require.ErrorIs(t, err, types.ErrInvalidSolution)
	require.ErrorIs(t, err, types.ErrConditionNotConsented)
}

// TestSignedConditionIsPermissionNotRequirement_BoundaryPin records task
// 2.13(a)'s verdict IN EXECUTABLE FORM (D-093), the way the F-2 and D-091 F-1
// boundaries were pinned: it asserts what is TRUE TODAY, by name, so the fix
// turns it red deliberately instead of drifting.
//
// THE BOUNDARY. Consent is enforced in ONE direction only. Every guard above
// proves `attached ⇒ signed`: a solver cannot IMPOSE a condition (D-075 seller,
// D-076 buyer, at validation and at settlement). Nothing anywhere enforces
// `signed ⇒ attached` — both chokepoints skip a leg whose condition is NONE
// (validateConditionConsent and openConditionedEscrows each `continue`). So a
// signed IntentPayload.condition is a PERMISSION CEILING, never a REQUIREMENT:
// a party can bound what may be imposed on it, and cannot obtain what it asked
// for. Nothing in x/session even reads the signed condition unless a leg
// carries one.
//
// Both halves below are the same boundary from the two paths that can clear a
// session, and the SECOND is what makes this a protocol property rather than a
// solver-behaviour question.
//
// Owned by task 2.7 (D-075-FW-1(b)/FW-3, written into 2.7's criteria by D-093).
// When 2.7 lands, this test SHOULD fail — replace it with the enforcement
// assertions; do not relax it.
func TestSignedConditionIsPermissionNotRequirement_BoundaryPin(t *testing.T) {
	cond := &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_ZkDelivery{
		ZkDelivery: &escrowtypes.ZkDeliveryCondition{SchemaCommitment: zktest.SchemaCommitment([]byte("p")), VkId: zkdelivery.VKIDDeliveryV1},
	}}

	// (1) A SOLVER may simply omit it. Both parties signed the exact ZK
	// condition; the solver attaches none. The solution validates and CONCLUDE
	// succeeds. Note what the buyer gets instead of the escrow it signed for:
	// an unconditioned DataGrant still opens an x/escrow entry, but a RECORD-ONLY
	// one (conclude.go applyLeg) — zero amount, NONE condition, no release gate —
	// while the payment goes STRAIGHT to the seller through the ordinary coin
	// leg. The buyer has paid in full, up front, for a delivery it can no longer
	// withhold payment against. And the solver was not slashed, rejected, or
	// scored lower for dropping it.
	f := setupKeeper(t)
	b := f.setupBilateralBoth(t, cond, cond)
	solver := randAddr()
	f.registerSolver(t, solver, minSolverBond())
	sellerBefore := f.bank.balanceOf(b.seller.String()).AmountOf(usdcDenom)

	require.NoError(t, f.propose(t, solver, b.sessionID, b.match(nil)),
		"BOUNDARY: an unconditioned settlement of a pair who BOTH signed a condition is accepted")
	require.NoError(t, f.k.Conclude(f.ctx, b.sessionID, b.match(nil)))

	esc := mustEscrow(t, f)
	require.Nil(t, esc.Condition.Condition,
		"BOUNDARY: the escrow opened is the NONE record-only kind — no release gate at all")
	require.True(t, esc.Amount.IsZero(),
		"BOUNDARY: it holds no funds, so there is nothing to release or refund")
	require.Equal(t, sellerBefore.Add(b.payment.Amount), f.bank.balanceOf(b.seller.String()).AmountOf(usdcDenom),
		"BOUNDARY: the payment went straight to the seller instead of into the signed ZK escrow")

	sv, err := f.k.GetSolver(f.ctx, 0, solver)
	require.NoError(t, err)
	require.Equal(t, uint64(0), sv.SlashedCount,
		"BOUNDARY: dropping a signed condition is not a punishable offence, so a rational solver never attaches one")

	// (2) The IN-CONSENSUS FALLBACK does the same, and cannot do otherwise:
	// buildFallbackMatch emits SettleLegs with no Condition field set at all.
	// The fallback is the DEFAULT clearing path for every session no solver
	// wins, so this downgrade is performed by the state machine itself — not by
	// an untrusted third party. That is why 2.13(a)'s verdict is GAP and not
	// "solver-attached by design".
	f2 := setupKeeper(t)
	b2 := f2.setupBilateralBoth(t, cond, cond)
	session, err := f2.k.GetSession(f2.ctx, 0, b2.sessionID)
	require.NoError(t, err)
	params, err := f2.k.GetParams(f2.ctx)
	require.NoError(t, err)

	matches, err := f2.k.buildFallbackMatches(f2.ctx, session, params)
	require.NoError(t, err)
	require.Len(t, matches, 1, "the pair is mutually fillable, so the fallback clears it")
	dataLegs := 0
	for _, leg := range matches[0].Legs {
		if leg.GetData() != nil {
			dataLegs++
			require.Nil(t, leg.Condition,
				"BOUNDARY: the in-consensus fallback clears a both-parties-signed condition UNCONDITIONED")
		}
	}
	require.Equal(t, 1, dataLegs, "the fixture's fallback match must carry the DataGrant leg")
}

// mustEscrow returns the single escrow in the store (test convenience).
func mustEscrow(t testingT, f *fixture) escrowtypes.Escrow {
	var out escrowtypes.Escrow
	n := 0
	require.NoError(t, f.escrow.IterateEscrows(f.ctx, func(e escrowtypes.Escrow) bool {
		out = e
		n++
		return false
	}))
	require.Equal(t, 1, n)
	return out
}

// TestValidateConditionedMatch_AcceptsBilateral proves the shape policy ACCEPTS
// a well-formed bilateral conditioned match (the reject cases live in
// solution_test.go).
func TestValidateConditionedMatch_AcceptsBilateral(t *testing.T) {
	b := bilateral{dataHash: contentHash("d"), buyerSigil: "buyer", sellerSigil: "seller", payment: sdk.NewInt64Coin(usdcDenom, 5)}
	cond := &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_Hashlock{Hashlock: &escrowtypes.HashlockCondition{}}}
	require.NoError(t, validateConditionedMatch(b.match(cond)[0]))
}

// TestConcludeConditioned_UnsignedConditionRejected is the load-bearing CONSENT
// test (D-075): a solver attaches a condition the SELLER never signed (the
// seller signed NONE). The settlement-boundary consent guard must REJECT it —
// no escrow opened, no seller bond debited. This is the hole the consent
// binding closes: an untrusted solver manufacturing seller liability. If the
// consent guard were removed, the escrow would open against a non-consenting
// seller and this test would fail.
func TestConcludeConditioned_UnsignedConditionRejected(t *testing.T) {
	f := setupKeeper(t)
	b := f.setupBilateral(t, nil) // seller signed NONE
	bond := escrowtypes.SellerBondCoins(sdk.NewCoins(b.payment))
	f.bank.fund(b.seller.String(), bond)

	cond := &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_Optimistic{
		Optimistic: &escrowtypes.OptimisticCondition{DisputeWindowBlocks: 50},
	}}
	err := f.k.Conclude(f.ctx, b.sessionID, b.match(cond))
	require.ErrorIs(t, err, types.ErrConditionNotConsented)
	require.Equal(t, 0, countEscrows(t, f), "no escrow opened for an unconsented condition")
	require.Equal(t, bond.AmountOf(usdcDenom), f.bank.balanceOf(b.seller.String()).AmountOf(usdcDenom), "seller bond not debited")
}

// TestConcludeConditioned_MismatchedConditionRejected proves the consent guard
// binds the FULL params: the seller signed an OPTIMISTIC window of 50 but the
// solver settled one of 999 — a different obligation the seller never signed.
func TestConcludeConditioned_MismatchedConditionRejected(t *testing.T) {
	f := setupKeeper(t)
	signed := &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_Optimistic{
		Optimistic: &escrowtypes.OptimisticCondition{DisputeWindowBlocks: 50},
	}}
	b := f.setupBilateral(t, signed)
	f.bank.fund(b.seller.String(), escrowtypes.SellerBondCoins(sdk.NewCoins(b.payment)))

	tampered := &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_Optimistic{
		Optimistic: &escrowtypes.OptimisticCondition{DisputeWindowBlocks: 999},
	}}
	err := f.k.Conclude(f.ctx, b.sessionID, b.match(tampered))
	require.ErrorIs(t, err, types.ErrConditionNotConsented)
	require.Equal(t, 0, countEscrows(t, f))
}

// TestProposeConditioned_UnsignedRejectedAtValidation proves the consent guard
// also fires at SOLUTION VALIDATION (propose time, before settlement): a solver
// proposing a condition the seller never signed has its solution rejected as
// invalid (and is slashed, D-024). Consent is checked before price binding, so
// this isolates the consent rejection.
func TestProposeConditioned_UnsignedRejectedAtValidation(t *testing.T) {
	f := setupKeeper(t)
	b := f.setupBilateral(t, nil) // seller signed NONE
	solver := randAddr()
	f.registerSolver(t, solver, minSolverBond())

	cond := &escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_Optimistic{
		Optimistic: &escrowtypes.OptimisticCondition{DisputeWindowBlocks: 50},
	}}
	err := f.propose(t, solver, b.sessionID, b.match(cond))
	require.ErrorIs(t, err, types.ErrInvalidSolution)
	require.ErrorIs(t, err, types.ErrConditionNotConsented)
}
