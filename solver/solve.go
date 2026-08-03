// Package solver is the reference solver's matching pipeline: it runs the cycle
// finder first (it earns surplus the pairwise matcher cannot), then the greedy
// pairwise matcher on the leftover intents, gating every candidate through the
// shared chain validator (solver/gate) so only solutions the chain will accept
// are returned. This is the off-chain brain behind the cmd/conclave-solver
// daemon; it is pure (no gRPC/keys) so it is unit-testable in isolation.
package solver

import (
	"github.com/AryaSingh22/conclave/solver/condition"
	"github.com/AryaSingh22/conclave/solver/cycles"
	"github.com/AryaSingh22/conclave/solver/gate"
	"github.com/AryaSingh22/conclave/solver/match"
	"github.com/AryaSingh22/conclave/solver/naive"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
)

// Solve computes a set of intent-disjoint matches over the given OPEN intents:
// cycles first, then pairwise swaps on whatever the cycles did not consume.
// Every candidate is admitted only if it passes the shared chain gate (D-055),
// so a cycle whose prices are not exactly representable is dropped and its
// intents fall through to the pairwise pass. Returns nil if nothing clears.
//
// DELIVERY CONDITIONS (task 2.14, D-097). A signed condition is a REQUIREMENT
// since task 2.7, so every candidate is put through the attachment path
// (solver/condition) and then through the chain's own consent predicate
// (gate.AcceptConsent) before it is admitted. A candidate that would settle a
// party's signed condition as anything other than what that party agreed to is
// DROPPED here — it is never proposed, so it is never slashed. The intents in it
// simply go unmatched this session and roll to their TTL, which is the same
// posture the chain takes (D-094: no fill beats a wrong fill, because a wrong
// fill cannot be undone).
func Solve(intents []match.OpenIntent) []sessiontypes.Match {
	used := make(map[string]bool, len(intents))
	byID := make(map[string]match.OpenIntent, len(intents))
	for _, oi := range intents {
		byID[oi.ID] = oi
	}
	var out []sessiontypes.Match

	admit := func(m sessiontypes.Match) bool {
		for _, id := range m.IntentIds {
			if used[id] {
				return false
			}
		}
		// attach first, then check: the consent predicate must see the finished
		// leg. Attachment is best-effort matching; AcceptConsent is the invariant.
		parts := condition.Participants(m, byID)
		condition.Attach(&m, parts)
		if err := gate.Accept(m); err != nil {
			return false
		}
		// the two conditioned-settlement rules, each with its own error and each
		// independently sufficient to get a solution rejected and slashed: the
		// D-074 SHAPE (1426) and the §6.5 CONSENT binding (1427/1429).
		if err := gate.AcceptConditionShape(m); err != nil {
			return false
		}
		if err := gate.AcceptConsent(m, condition.PayloadFor(parts)); err != nil {
			return false
		}
		for _, id := range m.IntentIds {
			used[id] = true
		}
		out = append(out, m)
		return true
	}

	// cycles returns one GROUP per cycle: the same cycle priced under each of its
	// coin give-assets (the task-2.6 alternate numeraire anchorings), first being
	// the pre-2.6 default. Anchoring decides which prices are exactly
	// representable, so try them in order and take the first the chain gate
	// accepts — the gate stays the sole arbiter (D-055), and one cycle can never
	// contribute two clearings because `admit` marks its intents used.
	//
	// CycleEligible withholds condition-signing intents from this pass: D-074
	// permits a conditioned leg only in a two-party data-for-coin match, so in an
	// n-party cycle both attaching (1426) and dropping (1429) are slashable. They
	// are not lost — never marked used here, they fall through to the pairwise
	// pass below, which builds exactly the shape D-074 allows. It is an
	// efficiency/liveness guard and NOT the guarantee (that is
	// gate.AcceptConditionShape); see its doc comment for why the distinction is
	// spelled out.
	for _, group := range cycles.Find(condition.CycleEligible(intents)) {
		for _, m := range group {
			if admit(m) {
				break
			}
		}
	}

	leftover := make([]match.OpenIntent, 0, len(intents))
	for _, oi := range intents {
		if !used[oi.ID] {
			leftover = append(leftover, oi)
		}
	}
	for _, m := range naive.Find(leftover) {
		admit(m)
	}

	return out
}
