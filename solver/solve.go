// Package solver is the reference solver's matching pipeline: it runs the cycle
// finder first (it earns surplus the pairwise matcher cannot), then the greedy
// pairwise matcher on the leftover intents, gating every candidate through the
// shared chain validator (solver/gate) so only solutions the chain will accept
// are returned. This is the off-chain brain behind the cmd/conclave-solver
// daemon; it is pure (no gRPC/keys) so it is unit-testable in isolation.
package solver

import (
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
func Solve(intents []match.OpenIntent) []sessiontypes.Match {
	used := make(map[string]bool, len(intents))
	var out []sessiontypes.Match

	admit := func(m sessiontypes.Match) bool {
		for _, id := range m.IntentIds {
			if used[id] {
				return false
			}
		}
		if err := gate.Accept(m); err != nil {
			return false
		}
		for _, id := range m.IntentIds {
			used[id] = true
		}
		out = append(out, m)
		return true
	}

	for _, m := range cycles.Find(intents) {
		admit(m)
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
