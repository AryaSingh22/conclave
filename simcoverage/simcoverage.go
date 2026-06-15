// Package simcoverage is a tiny, sim-only counter registry used by the task
// 0.11 SDK simulation to MEASURE the operation mix that actually executed —
// the same discipline as task 0.10's branch-distribution proof: coverage is
// reported, not asserted. The module simulation operations call Inc as they
// run; the app/sim harness prints a Snapshot alongside the final-state outcome
// tallies so a reviewer can see the run truly exercised every module (intents
// submitted, sessions cleared, solutions proposed, mandates enforced) rather
// than vacuously registering sigils and stopping.
//
// These counters are OBSERVATION-ONLY: they are never read back into consensus
// state, so they cannot affect the app hash and are irrelevant to the same-seed
// determinism check. They are package-global because the SDK's
// WeightedOperations(simState) signature offers no per-run injection point.
package simcoverage

import (
	"fmt"
	"sort"
	"sync"
)

var (
	mu      sync.Mutex
	counts  = map[string]int64{}
	enabled bool
)

// Reset clears all counters and enables counting. The harness calls it once at
// the start of the metered run.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	counts = map[string]int64{}
	enabled = true
}

// Inc bumps the named counter by one when counting is enabled. Disabled by
// default so ordinary (non-sim) module operation never accumulates state.
func Inc(name string) {
	mu.Lock()
	defer mu.Unlock()
	if enabled {
		counts[name]++
	}
}

// Snapshot returns a copy of the current counters.
func Snapshot() map[string]int64 {
	mu.Lock()
	defer mu.Unlock()
	out := make(map[string]int64, len(counts))
	for k, v := range counts {
		out[k] = v
	}
	return out
}

// Report renders the snapshot as a stable, sorted, human-readable block.
func Report() string {
	snap := Snapshot()
	keys := make([]string, 0, len(snap))
	for k := range snap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	s := ""
	for _, k := range keys {
		s += fmt.Sprintf("  %-40s %d\n", k, snap[k])
	}
	return s
}
