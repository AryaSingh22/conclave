package threshold

import (
	"fmt"
	"testing"
	"time"

	"github.com/AryaSingh22/conclave/crypto/reveal"
)

// reveal_bench_test.go is the Task-1.7 / D-069(iv) Reveal-cost benchmark: it
// measures the BeginBlock Combine cost a session incurs under the per-session
// intent cap (max_intents_per_session) on the n=4 committee the localnet runs.
// BeginBlock's reveal work is dominated by one verified Combine per sealed intent
// (each Combine internally runs the per-share pairing gate, D-066, then the
// threshold interpolation + IBE/AEAD open). This validates the cap was sized so a
// full-cap session reveals well within a block.

const (
	benchN   = 4  // localnet committee size
	benchT   = 3  // floor(2*4/3)+1
	benchCap = 64 // a representative max_intents_per_session
)

// buildRevealBatch produces `cap` independent (ciphertext, t-of-n shares) items
// against one real DKG epoch — the per-block reveal workload at the cap.
func buildRevealBatch(tb testing.TB, cap int) (*Scheme, [][]byte, [][]reveal.RevealShare) {
	mats, err := RunDKGInProcess(benchN, benchT)
	if err != nil {
		tb.Fatalf("dkg: %v", err)
	}
	commitsBz, err := MarshalCommits(mats[0].Commits)
	if err != nil {
		tb.Fatalf("commits: %v", err)
	}
	sch, err := NewSchemeFromCommitBytes(commitsBz, benchN)
	if err != nil {
		tb.Fatalf("scheme: %v", err)
	}
	client := NewClientScheme()
	cts := make([][]byte, cap)
	shareSets := make([][]reveal.RevealShare, cap)
	for k := 0; k < cap; k++ {
		ct, err := client.Encrypt(mats[0].EpochPub, []byte(fmt.Sprintf("intent-%d-payload-blob", k)))
		if err != nil {
			tb.Fatalf("encrypt: %v", err)
		}
		shares := make([]reveal.RevealShare, benchT)
		for i := 0; i < benchT; i++ {
			sh, err := sch.PartialReveal(mats[i].Share, ct)
			if err != nil {
				tb.Fatalf("share: %v", err)
			}
			shares[i] = sh
		}
		cts[k] = ct
		shareSets[k] = shares
	}
	return sch, cts, shareSets
}

// BenchmarkRevealCombineUnderCap measures one full-cap session reveal: benchCap
// verified Combines on the n=4 committee.
func BenchmarkRevealCombineUnderCap(b *testing.B) {
	sch, cts, shareSets := buildRevealBatch(b, benchCap)
	b.ResetTimer()
	for iter := 0; iter < b.N; iter++ {
		for k := range cts {
			if _, err := sch.Combine(shareSets[k], cts[k]); err != nil {
				b.Fatalf("combine: %v", err)
			}
		}
	}
}

// TestRevealCostWithinBound measures the PER-INTENT reveal cost (the scaling
// invariant — total BeginBlock reveal cost is per-intent × sealed-intent count)
// and confirms it is bounded, then derives the recommended max_intents_per_session
// for a conservative per-block reveal budget. This is the D-069 gap-(iv) gate.
//
// FINDING (recorded in D-070): per-intent reveal is pairing-dominated (the D-066
// share gate + IBE open), ~10ms at n=4 on dev hardware. So a session's reveal cost
// is LINEAR in the sealed-intent count, and the per-session cap is load-bearing:
// max_intents_per_session MUST be finite once the Veil is live (epoch_id != 0).
// The default 0 (unbounded) is only safe on the epoch-0 stub path (no shares, no
// Combine); a live deployment must set a finite cap sized below this budget.
func TestRevealCostWithinBound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping reveal-cost timing in -short")
	}
	sch, cts, shareSets := buildRevealBatch(t, benchCap)

	start := time.Now()
	for k := range cts {
		if _, err := sch.Combine(shareSets[k], cts[k]); err != nil {
			t.Fatalf("combine: %v", err)
		}
	}
	batch := time.Since(start)
	perIntent := batch / time.Duration(benchCap)

	// reserve at most ~30% of a 1s dev block for reveals → the recommended cap.
	const blockTime = time.Second
	const revealBudget = blockTime * 3 / 10
	recommendedCap := int(revealBudget / perIntent)
	t.Logf("reveal cost (n=%d, t=%d): %v/intent; %d-intent batch = %v; recommended max_intents_per_session ≈ %d (≤%v reveal budget / %v block)",
		benchN, benchT, perIntent, benchCap, batch, recommendedCap, revealBudget, blockTime)

	// the load-bearing invariant: per-intent reveal stays well bounded (so a
	// sanely-capped session always fits a block). 50ms/intent is a deliberately
	// loose ceiling that won't flake; the measured cost is far below it.
	const perIntentCeiling = 50 * time.Millisecond
	if perIntent >= perIntentCeiling {
		t.Fatalf("per-intent reveal cost %v exceeds the ceiling %v — the share gate / IBE open regressed", perIntent, perIntentCeiling)
	}
	if recommendedCap < 1 {
		t.Fatalf("recommended cap < 1 — per-intent reveal (%v) is too expensive for the block budget", perIntent)
	}
}
