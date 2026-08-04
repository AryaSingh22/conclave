// Package condition is the reference solver's DELIVERY-CONDITION ATTACHMENT
// PATH (SPEC §6.5; task 2.14, D-097) — the in-tree answer to "what must a
// third-party solver do when an intent signs a delivery condition?"
//
// WHY IT EXISTS. Since task 2.7 (D-094) a signed delivery condition is a
// REQUIREMENT, not a permission ceiling: `signed ⇒ attached` is enforced at
// both chain chokepoints, so a solution that DROPS a condition its giver
// signed is invalid (ErrConditionNotHonored, session 1429) and costs the
// solver a 5% bond slash. That slash is applied in the ANTE handler (task
// 2.17) — this comment used to say it "commits with the failing tx", which was
// false: a msg-server write is discarded together with the error that rejects
// the proposal, so the penalty did not exist (D-097 F-1). Attaching one the
// counterparty never consented to is the mirror failure
// (ErrConditionNotConsented, 1427), and putting a conditioned leg in a match
// shape the escrow funding cannot serve is a third (ErrConditionUnsupportedMatch,
// 1426). Until 2.14 the reference solver read no condition anywhere, so it
// walked into the first of those the moment it met a condition-signing intent.
//
// WHAT IS AUTHORED HERE AND WHAT IS CALLED. solver/match's contract is that the
// MATCHING idea is reimplemented off-chain (solver/cycles, solver/naive) while a
// VALIDATION rule is never reimplemented — it is called, so the solver's verdict
// and the chain's cannot drift (D-054/D-055). "Which condition governs this leg"
// is a validation rule, and a subtle one: it resolves the per-leg
// IntentLeg.condition refinement before the payload-level IntentPayload.condition
// (the FW-2 order, D-094), which is exactly what a mirrored implementation would
// get wrong — silently, and only against intents that use the newer field. So
// this package CALLS keeper.EffectiveSellerCondition and authors only the two
// things that are genuinely the solver's own: WHERE the resolved condition goes,
// and WHICH intents may enter an n-party cycle at all.
//
// THE SELF-CHECK IS SEPARATE AND IS THE INVARIANT. Attachment here is
// best-effort matching; solver/gate.AcceptConsent runs the chain's own binding
// predicate over the finished match and is what actually guarantees "never
// propose a downgrade". That split is deliberate — the guarantee must not
// depend on this file being exhaustive.
package condition

import (
	"bytes"

	"github.com/AryaSingh22/conclave/solver/match"
	escrowtypes "github.com/AryaSingh22/conclave/x/escrow/types"
	sessionkeeper "github.com/AryaSingh22/conclave/x/session/keeper"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// Participants resolves a match's intent ids to the OpenIntents that produced
// them, in m.IntentIds order. The lookup is built PER MATCH rather than once
// per session on purpose: §6.4 rule 1 guarantees one intent per sigil WITHIN a
// match, but the OPEN set may hold several intents for the same sigil, so a
// session-wide sigil→intent map could resolve a leg against the wrong signature.
// This mirrors the chain, whose byID map is likewise per match.
func Participants(m sessiontypes.Match, byID map[string]match.OpenIntent) []match.OpenIntent {
	out := make([]match.OpenIntent, 0, len(m.IntentIds))
	for _, id := range m.IntentIds {
		if oi, ok := byID[id]; ok {
			out = append(out, oi)
		}
	}
	return out
}

// PayloadFor returns the sigil → signed-payload lookup keeper.CheckConditionBinding
// takes. A sigil with no participating intent returns ok=false, which the chain
// treats as a rejection rather than as consent — fail-closed, as it must be.
func PayloadFor(participants []match.OpenIntent) func(string) (veiltypes.IntentPayload, bool) {
	return func(sigil string) (veiltypes.IntentPayload, bool) {
		oi, ok := bySigil(participants, sigil)
		if !ok {
			return veiltypes.IntentPayload{}, false
		}
		return oi.Payload(), true
	}
}

// Attach puts each giver's signed delivery condition onto its own DataGrant
// settlement leg — the reference implementation of a solver's attachment duty.
//
// Only a DataGrant leg can bear a condition (SPEC §6.5; a conditioned coin or
// service leg is ErrConditionUnsupportedMatch), and the giver of that leg is its
// SELLER, so the condition resolved is the seller's. The COUNTERPARTY's signed
// condition is deliberately not consulted here: a solver does not negotiate the
// two sides into agreement, it attaches what the seller signed and lets the
// unchanged consent predicate decide whether the buyer accepts it
// (gate.AcceptConsent). That is the same division the in-consensus fallback
// makes (attachFallbackCondition, x/session/keeper/fallback.go) and for the same
// reason: one definition of consent, in one place.
//
// A giver that signed nothing attaches nothing, so an unconditioned match is
// left byte-identical to what the matchers built.
func Attach(m *sessiontypes.Match, participants []match.OpenIntent) {
	if m == nil {
		return
	}
	for li := range m.Legs {
		leg := &m.Legs[li]
		data := leg.GetData()
		if data == nil {
			continue
		}
		giver, ok := bySigil(participants, leg.FromSigil)
		if !ok {
			continue
		}
		// Find the giver's OWN give leg that produced this settlement leg, so the
		// condition is resolved against the right asset key even for an intent
		// with several give legs. Matching on ContentHash rather than on a
		// locally-formatted key keeps the key format the chain's business.
		for gi := range giver.Give {
			gd := giver.Give[gi].GetData()
			if gd == nil || !bytes.Equal(gd.ContentHash, data.ContentHash) {
				continue
			}
			key, _, ok := match.LegKeyAmount(giver.Give[gi])
			if !ok {
				break
			}
			if cond := sessionkeeper.EffectiveSellerCondition(giver.Payload(), key); cond != nil && cond.Condition != nil {
				leg.Condition = cond
			}
			break
		}
	}
}

// SignsDataCondition reports whether any DataGrant leg of this intent — on
// either side — is governed by a non-NONE delivery condition: the leg's own
// IntentLeg.condition if it sets one, else the payload-level
// IntentPayload.condition, which governs every such leg at once.
//
// It answers a solver-side question the chain never has to ask, so it is
// authored rather than called: is this intent eligible for an n-party cycle?
func SignsDataCondition(oi match.OpenIntent) bool {
	if isSet(oi.Condition) {
		return hasDataLeg(oi.Give) || hasDataLeg(oi.Want)
	}
	for _, legs := range [][]veiltypes.IntentLeg{oi.Give, oi.Want} {
		for i := range legs {
			if legs[i].GetData() != nil && isSet(legs[i].Condition) {
				return true
			}
		}
	}
	return false
}

// CycleEligible withholds every condition-signing intent from a cycle-finder's
// input, because D-074 permits a conditioned leg only in an unambiguous
// bilateral exchange — exactly one conditioned DataGrant leg seller→buyer, paid
// by coin legs buyer→seller, across two distinct sigils. An n-party cycle is
// none of those, so for such an intent inside a cycle BOTH moves are slashable:
// attaching is ErrConditionUnsupportedMatch (1426) and dropping is
// ErrConditionNotHonored (1429).
//
// It tests BOTH sides. A BUYER's signed condition alone is enough, because the
// chain resolves the binding for the seller AND the buyer of every DataGrant
// leg, so a dropped buyer-side condition is 1429 just the same.
//
// HONEST STATEMENT OF WHAT THIS IS (D-089's lesson, applied to my own code
// rather than inherited): this is an EFFICIENCY AND LIVENESS guard, NOT the
// guarantee. The guarantee that no such candidate is ever proposed is
// gate.AcceptConditionShape, which runs the chain's own D-074 rule over the
// finished match — remove CycleEligible and the pipeline still cannot propose a
// conditioned cycle, it merely wastes work building one. What CycleEligible
// actually buys is liveness: cycles.Find marks an intent used inside its own
// traversal, so a doomed conditioned cycle can consume intents that would
// otherwise have formed a different, valid cycle. Withheld intents are never
// discarded — they fall through to the pairwise pass, which builds exactly the
// two-party shape D-074 allows.
//
// That distinction is stated because task 2.6 got the mirror-image case wrong:
// the solver-side allow_partial candidate gates were BELIEVED to be the
// fill-or-kill guarantee and removing them changed nothing (§6.4 rule 3 was the
// guarantee all along, D-089). A candidate gate whose removal is unobservable
// is an optimisation, and calling it a guarantee is how a project ends up
// trusting the wrong line.
func CycleEligible(intents []match.OpenIntent) []match.OpenIntent {
	out := make([]match.OpenIntent, 0, len(intents))
	for _, oi := range intents {
		if SignsDataCondition(oi) {
			continue
		}
		out = append(out, oi)
	}
	return out
}

func bySigil(participants []match.OpenIntent, sigil string) (match.OpenIntent, bool) {
	for _, oi := range participants {
		if oi.Sigil == sigil {
			return oi, true
		}
	}
	return match.OpenIntent{}, false
}

func hasDataLeg(legs []veiltypes.IntentLeg) bool {
	for i := range legs {
		if legs[i].GetData() != nil {
			return true
		}
	}
	return false
}

// isSet reports a non-NONE condition. A present-but-EMPTY oneof is NONE, the
// same fail-closed reading the chain's effectiveCondition takes (D-094): an
// empty submessage must never be mistaken for "this leg needs no protection".
func isSet(c *escrowtypes.DeliveryCondition) bool {
	return c != nil && c.Condition != nil
}
