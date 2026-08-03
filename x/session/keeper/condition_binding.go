package keeper

import (
	"bytes"

	errorsmod "cosmossdk.io/errors"

	escrowtypes "github.com/AryaSingh22/conclave/x/escrow/types"
	"github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// This file is the SPEC §6.5 delivery-condition consent binding, in one place
// (task 2.7, D-094). Before 2.7 the binding ran in ONE direction: every guard
// proved `attached ⇒ signed` — a solver (SPEC §1.1: untrusted) could not IMPOSE
// a condition — while `signed ⇒ attached` was enforced NOWHERE, because both
// chokepoints skipped a NONE leg before ever reaching the comparison. A signed
// condition was therefore a PERMISSION CEILING and never a REQUIREMENT: a party
// could bound what was done to it and could not obtain what it asked for
// (D-093).
//
// That asymmetry was internal inconsistency in a single signed message, not
// merely a missing feature. Every OTHER field of the same IntentPayload is a
// requirement — the give/want quantities, the §5.1 `limit` rate (since task
// 2.6), the counterparty_class predicate — and only `condition` was a
// permission, with nothing in the tree or the SPEC saying so. The same shape
// one field over was closed by 2.6 as D-049/F-7 ("SIGNED by the agent and read
// by nobody, the worst of both worlds"); the consequence here is larger, since
// a dropped condition sent the buyer's payment straight to the seller and left
// a record-only zero-amount escrow shell with nothing to release, refund, or
// dispute.
//
// The rule is now ONE predicate under two quantifiers, which is what makes it
// impossible to implement for one direction, one role, or one class and not the
// others.

// conditionRole is the side of a DataGrant leg whose consent is being checked.
// The roles differ because they can pre-commit different things: the SELLER
// generates the deliverable and can name every parameter in advance, while the
// BUYER cannot know a HASHLOCK payload_hash — a post-delivery receipt-of-access
// token — at intent time (D-075's recorded nuance, the reason FW-1's buyer
// consent needed a class/window form rather than equality).
type conditionRole int

const (
	roleSeller conditionRole = iota // the conditioned DataGrant leg's FromSigil
	roleBuyer                       // the conditioned DataGrant leg's ToSigil
)

func (r conditionRole) String() string {
	if r == roleBuyer {
		return "buyer"
	}
	return "seller"
}

// conditionClassName names a condition class for error text (NONE for an unset
// or empty oneof). Pure and total — it never panics on an unknown variant.
func conditionClassName(c *escrowtypes.DeliveryCondition) string {
	if c == nil || c.Condition == nil {
		return "NONE"
	}
	switch c.Condition.(type) {
	case *escrowtypes.DeliveryCondition_Hashlock:
		return "HASHLOCK"
	case *escrowtypes.DeliveryCondition_Optimistic:
		return "OPTIMISTIC"
	case *escrowtypes.DeliveryCondition_ZkDelivery:
		return "ZK"
	default:
		return "UNKNOWN"
	}
}

// conditionSatisfies is THE consent predicate: does `attached` satisfy what this
// party `signed`? Both directions of consent are this one question under
// different quantifiers —
//
//   - ATTACH direction ("the attached condition satisfies what I signed"): an
//     untrusted solver must not bind a seller to a delivery obligation and slash
//     exposure it never signed (D-075), nor route a buyer's payment into a
//     release gate or a timeline it never signed (D-076 for ZK; task 2.7 extends
//     it to HASHLOCK/OPTIMISTIC — D-075-FW-1(a)).
//   - REQUIREMENT direction ("SOME condition satisfying what I signed was
//     attached"): the settlement may not silently drop a condition a party
//     signed (D-075-FW-1(b)/FW-3, the D-093 downgrade).
//
// ErrConditionNotHonored (1429) is returned for EXACTLY ONE case — attached is
// NONE while signed is not — and every other rejection stays
// ErrConditionNotConsented (1427). That split is deliberate: a negative test
// must assert the SPECIFIC error, and two negatives that cannot fail with
// different messages test one thing twice. Each of these two errors now
// identifies one direction of the binding and nothing else.
func conditionSatisfies(attached, signed *escrowtypes.DeliveryCondition, role conditionRole, sigil string) error {
	attachedNone := attached == nil || attached.Condition == nil
	signedNone := signed == nil || signed.Condition == nil

	switch {
	case attachedNone && signedNone:
		return nil

	case attachedNone:
		// THE DOWNGRADE (task 2.7). This is the case both chokepoints used to
		// `continue` past before comparing anything.
		return errorsmod.Wrapf(types.ErrConditionNotHonored,
			"%s %s signed a %s delivery condition but the settlement attaches none",
			role, sigil, conditionClassName(signed))

	case signedNone:
		return errorsmod.Wrapf(types.ErrConditionNotConsented,
			"%s %s did not sign this %s condition",
			role, sigil, conditionClassName(attached))
	}

	if role == roleSeller {
		// The seller can pre-commit every parameter, so its consent is EXACT: a
		// solver may not alter the timeout, payload hash, or dispute window the
		// seller signed (D-075, unchanged by 2.7).
		if !conditionEqual(signed, attached) {
			return errorsmod.Wrapf(types.ErrConditionNotConsented,
				"seller %s did not sign this %s condition (it signed a %s one with different parameters)",
				sigil, conditionClassName(attached), conditionClassName(signed))
		}
		return nil
	}
	return buyerAccepts(attached, signed, sigil)
}

// buyerAccepts is the BUYER's half of the predicate (D-075-FW-1, task 2.7). It
// is NOT equality, and the shape of each class's relation is derived from what
// that class's timer actually protects — not by analogy from the seller side:
//
//   - ZK_DELIVERY — EXACT (unchanged, D-076). schema_commitment and vk_id are
//     the advance-known "committed schema/hash" of SPEC §6.5, and release is
//     SELLER-DRIVEN against them on a schema not otherwise tied to the buyer's
//     want-key, so nothing may differ.
//   - HASHLOCK — the timeout is when the buyer's LOCKED PAYMENT auto-refunds, so
//     a LONGER timeout is strictly worse for the buyer: its signed value is a
//     MAXIMUM. The payload_hash is a post-delivery token the buyer cannot know
//     at intent time, so an empty one is UNCONSTRAINED; a buyer that does know
//     it out of band may pin it, which can only tighten the bound.
//   - OPTIMISTIC — the dispute window IS the buyer's protection: its time to
//     inspect the delivery and dispute before the funds release to the seller.
//     A SHORTER window is strictly worse: its signed value is a MINIMUM.
//
// The HASHLOCK and OPTIMISTIC bounds therefore run in OPPOSITE directions, and
// that is the substance of the rule rather than an accident of it: a single
// "the window must match" relation would be wrong for both, and a single "at
// least as large" relation would be wrong for HASHLOCK. Each bound is the
// direction that is monotone-SAFE for the party that carries the risk.
func buyerAccepts(attached, signed *escrowtypes.DeliveryCondition, sigil string) error {
	switch s := signed.Condition.(type) {
	case *escrowtypes.DeliveryCondition_Hashlock:
		a, ok := attached.Condition.(*escrowtypes.DeliveryCondition_Hashlock)
		if !ok {
			return errorsmod.Wrapf(types.ErrConditionNotConsented,
				"buyer %s signed a HASHLOCK condition but the settlement attaches a %s one",
				sigil, conditionClassName(attached))
		}
		if a.Hashlock.TimeoutBlocks > s.Hashlock.TimeoutBlocks {
			return errorsmod.Wrapf(types.ErrConditionNotConsented,
				"buyer %s signed a HASHLOCK timeout of at most %d blocks but the settlement attaches %d — its payment would stay locked longer than it agreed",
				sigil, s.Hashlock.TimeoutBlocks, a.Hashlock.TimeoutBlocks)
		}
		if len(s.Hashlock.PayloadHash) > 0 && !bytes.Equal(s.Hashlock.PayloadHash, a.Hashlock.PayloadHash) {
			return errorsmod.Wrapf(types.ErrConditionNotConsented,
				"buyer %s pinned a HASHLOCK payload_hash the settlement does not carry", sigil)
		}
		return nil

	case *escrowtypes.DeliveryCondition_Optimistic:
		a, ok := attached.Condition.(*escrowtypes.DeliveryCondition_Optimistic)
		if !ok {
			return errorsmod.Wrapf(types.ErrConditionNotConsented,
				"buyer %s signed an OPTIMISTIC condition but the settlement attaches a %s one",
				sigil, conditionClassName(attached))
		}
		if a.Optimistic.DisputeWindowBlocks < s.Optimistic.DisputeWindowBlocks {
			return errorsmod.Wrapf(types.ErrConditionNotConsented,
				"buyer %s signed an OPTIMISTIC dispute window of at least %d blocks but the settlement attaches %d — less time to inspect and dispute than it agreed",
				sigil, s.Optimistic.DisputeWindowBlocks, a.Optimistic.DisputeWindowBlocks)
		}
		return nil

	case *escrowtypes.DeliveryCondition_ZkDelivery:
		if !conditionEqual(signed, attached) {
			return errorsmod.Wrapf(types.ErrConditionNotConsented,
				"buyer %s did not sign this ZK condition", sigil)
		}
		return nil

	default:
		return errorsmod.Wrapf(types.ErrConditionNotConsented,
			"buyer %s signed an unrecognized delivery condition class", sigil)
	}
}

// conditionEqual reports whether two delivery conditions are byte-for-byte the
// same (including class and all parameters), treating nil / empty-oneof as the
// same NONE. It is the EXACT-equality case of the predicate above — the seller's
// consent in every class, and the buyer's for ZK_DELIVERY.
func conditionEqual(a, b *escrowtypes.DeliveryCondition) bool {
	aNone := a == nil || a.Condition == nil
	bNone := b == nil || b.Condition == nil
	if aNone || bNone {
		return aNone && bNone
	}
	switch at := a.Condition.(type) {
	case *escrowtypes.DeliveryCondition_Hashlock:
		bt, ok := b.Condition.(*escrowtypes.DeliveryCondition_Hashlock)
		return ok &&
			bytes.Equal(at.Hashlock.PayloadHash, bt.Hashlock.PayloadHash) &&
			at.Hashlock.TimeoutBlocks == bt.Hashlock.TimeoutBlocks
	case *escrowtypes.DeliveryCondition_Optimistic:
		bt, ok := b.Condition.(*escrowtypes.DeliveryCondition_Optimistic)
		return ok && at.Optimistic.DisputeWindowBlocks == bt.Optimistic.DisputeWindowBlocks
	case *escrowtypes.DeliveryCondition_ZkDelivery:
		bt, ok := b.Condition.(*escrowtypes.DeliveryCondition_ZkDelivery)
		return ok &&
			bytes.Equal(at.ZkDelivery.SchemaCommitment, bt.ZkDelivery.SchemaCommitment) &&
			at.ZkDelivery.VkId == bt.ZkDelivery.VkId
	default:
		return false
	}
}

// effectiveCondition resolves the signed delivery condition governing ONE
// settlement leg for ONE party — the FW-2 resolver (D-075-FW-2, task 2.7), and
// the READER that makes the new IntentLeg.condition field load-bearing rather
// than decorative (METHODOLOGY Rule 4).
//
// The per-leg field REFINES the payload-level IntentPayload.condition, which
// governs every data/service leg of an intent at once: a matching signed leg
// that SETS a condition is governed by it; one that leaves it unset falls back
// to the payload-level field. An intent written before the field existed
// therefore resolves exactly as it did pre-2.7.
//
// A party's own side is scanned — a seller's delivery obligation lives on its
// GIVE legs, a buyer's protection on its WANT legs — matching by conservation
// asset key, in signed-slice order. No map is consulted, so the result is
// deterministic on every node (§11.6).
//
// RECORDED LIMITATION (spec-unpinned, decided here and fail-CLOSED): a per-leg
// field that is present but EMPTY does not exempt a leg from a payload-level
// condition; it falls back like an absent one. proto3 can distinguish the two,
// so per-leg exemption is expressible later — but reading "empty message" as
// "this leg needs no protection" is a fail-OPEN default that a client emitting
// empty submessages could trip into silently, which is exactly the shape Rule 4
// warns about. Refinement may only ever ADD a condition here, never remove one.
func effectiveCondition(p veiltypes.IntentPayload, legKey string, role conditionRole) *escrowtypes.DeliveryCondition {
	legs := p.Give
	if role == roleBuyer {
		legs = p.Want
	}
	for i := range legs {
		key, _, err := intentLegKeyAmount(legs[i])
		if err != nil {
			continue // malformed leg: the limit checks reject it on their own
		}
		if key != legKey {
			continue
		}
		if c := legs[i].Condition; c != nil && c.Condition != nil {
			return c
		}
	}
	return p.Condition
}

// checkLegConditionBinding applies the predicate to ONE DataGrant leg from BOTH
// sides. It is the single definition of the rule: the two chokepoints (solution
// validation and the settlement boundary) differ only in how they LOAD the two
// signed payloads, never in what the rule says — so the pair cannot drift the
// way an ante decorator and a msg server once did (D-085). The seller is
// checked first so a seller-side rejection keeps the message it had before 2.7.
func checkLegConditionBinding(leg types.SettleLeg, sellerSigned, buyerSigned *escrowtypes.DeliveryCondition) error {
	if err := conditionSatisfies(leg.Condition, sellerSigned, roleSeller, leg.FromSigil); err != nil {
		return err
	}
	return conditionSatisfies(leg.Condition, buyerSigned, roleBuyer, leg.ToSigil)
}

// EffectiveSellerCondition resolves the delivery condition a party's signed
// payload imposes on ONE of its own give legs, identified by conservation asset
// key: the per-leg IntentLeg.condition if that leg sets one, else the
// payload-level IntentPayload.condition (the FW-2 refinement order, D-094).
//
// EXPORTED FOR THE REFERENCE SOLVER (task 2.14, D-097). An off-chain solver
// must attach exactly the condition the chain will resolve here, and the
// resolution ORDER is part of that: a solver that read only the payload-level
// field would drop a per-leg refinement and be slashed 5% for a downgrade it
// could not see. Reusing this function rather than mirroring it is the same
// no-drift discipline solver/gate applies to CheckConservation and
// CheckPriceBinding (D-054/D-055).
//
// The buyer-side resolution is deliberately NOT exported: a solver never needs
// to resolve it, because it never chooses what the buyer requires — it attaches
// the seller's condition and asks CheckConditionBinding whether the buyer
// accepts. Exporting a resolver with no reader would be decoration (Rule 4).
func EffectiveSellerCondition(p veiltypes.IntentPayload, legKey string) *escrowtypes.DeliveryCondition {
	return effectiveCondition(p, legKey, roleSeller)
}

// CheckConditionedMatchShape reports whether a match carrying a delivery
// condition has the D-074 shape the escrow funding can serve: exactly one
// conditioned DataGrant leg seller→buyer, paid only by coin legs buyer→seller,
// across exactly two distinct sigils. An unconditioned match is always fine.
//
// EXPORTED FOR THE REFERENCE SOLVER (task 2.14, D-097), and it is a SEPARATE
// rule from the consent binding rather than part of it: consent asks whether
// the parties agreed to this condition, the shape asks whether the settlement
// machinery can carry it at all. They fail with different errors —
// ErrConditionUnsupportedMatch (1426) here, 1427/1429 there — and a solver that
// self-checked only consent would still be slashed for putting a perfectly
// consented condition into an n-party cycle.
func CheckConditionedMatchShape(m types.Match) error {
	return validateConditionedMatch(m)
}

// CheckConditionBinding enforces the SPEC §6.5 consent binding in BOTH
// directions over one match. It is pure and ctx-free: `payloadFor` returns the
// IntentPayload a sigil signed, and everything else is a function of the match.
//
// TWO CALLERS, ONE IMPLEMENTATION (task 2.14, D-097):
//   - the chain, at solution validation (validateConditionBinding, solution.go);
//   - the off-chain reference solver's pre-submission self-check
//     (solver/gate.AcceptConsent), so a candidate the chain would reject on
//     consent is DROPPED instead of proposed and slashed.
//
// That second caller is why this is exported rather than mirrored. A solver
// that reimplements the predicate and drifts one bit — a per-class inequality
// in the wrong direction, a missed per-leg refinement — loses 5% of its bond,
// silently, on a path no test of its own would show. This is the rule the
// solver cannot afford to guess at, so it calls the chain's copy of it.
//
// THE LOOP BOUND IS THE FIX, and it is worth stating plainly because the
// predicate alone would not have closed anything. Before task 2.7 this walk
// visited only legs that ALREADY carried a condition and `continue`d past every
// other one — so the single case it could never observe was the case that
// mattered: a leg whose signed condition had been DROPPED. It walks every
// DataGrant leg (the legs that CAN carry a condition) and asks the predicate
// about each, which is what makes the requirement direction reachable at all.
// A test that proves the predicate works but not that this loop reaches the
// right legs has proven nothing about the downgrade.
func CheckConditionBinding(m types.Match, payloadFor func(sigil string) (veiltypes.IntentPayload, bool)) error {
	for li := range m.Legs {
		leg := m.Legs[li]
		key, applies := legConditionKey(leg)
		if !applies {
			// Not a DataGrant: it may carry no condition at all, and no signed
			// condition governs it (validateConditionedMatch rejects a conditioned
			// non-DataGrant leg; this is the same bar, reached independently).
			if leg.Condition != nil && leg.Condition.Condition != nil {
				return errorsmod.Wrapf(types.ErrConditionUnsupportedMatch, "leg %d: conditioned leg is not a DataGrant", li)
			}
			continue
		}
		sellerPayload, ok := payloadFor(leg.FromSigil)
		if !ok {
			return errorsmod.Wrapf(types.ErrConditionNotConsented, "leg %d: no seller intent for sigil %s", li, leg.FromSigil)
		}
		buyerPayload, ok := payloadFor(leg.ToSigil)
		if !ok {
			return errorsmod.Wrapf(types.ErrConditionNotConsented, "leg %d: no buyer intent for sigil %s", li, leg.ToSigil)
		}
		if err := checkLegConditionBinding(leg,
			effectiveCondition(sellerPayload, key, roleSeller),
			effectiveCondition(buyerPayload, key, roleBuyer),
		); err != nil {
			return errorsmod.Wrapf(err, "leg %d", li)
		}
	}
	return nil
}

// legConditionKey returns the conservation asset key a DataGrant settlement leg
// is matched against in the signed payload, and whether the leg is one the
// binding applies to at all. Only DataGrant legs can carry a condition (SPEC
// §6.5; validateConditionedMatch rejects a conditioned non-DataGrant leg), so
// coin and service-voucher legs are outside the binding entirely.
func legConditionKey(leg types.SettleLeg) (string, bool) {
	if leg.GetData() == nil {
		return "", false
	}
	key, _, err := legAssetKey(leg)
	if err != nil {
		return "", false
	}
	return key, true
}
