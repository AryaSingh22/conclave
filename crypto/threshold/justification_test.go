package threshold

import (
	"bytes"
	"testing"

	"github.com/AryaSingh22/conclave/crypto/reveal"
)

// justification_test.go proves the Task-1.7 / D-070 closure of the
// justification-grief seam at the CRYPTO layer (offline; the chain-transport and
// live-network proofs are x/epoch + the localnet). It exercises the realistic
// Byzantine-dealer model RunDKGInProcessFaulty constructs: a dealer that submits
// an AUTHENTIC (validly-signed) but cryptographically-bad deal and refuses to
// justify. Build conditions 1 and 3.

// TestJustificationExcludesFaultyDealerAndCompletes proves BUILD CONDITION 1:
// with one faulty dealer (f=1) in an n=4 committee (t=3), the justification round
// EXCLUDES that dealer and the DKG STILL COMPLETES — the honest nodes derive a
// consistent epoch key that decrypts a t-of-n reveal. The before/after contrast
// is structural: without the justification round (the pre-1.7 Finalize that
// errored on any complaint) this same input made the DKG collapse for every node
// → epoch 1 never activates (the single-point veto). With it, epoch 1 activates.
func TestJustificationExcludesFaultyDealerAndCompletes(t *testing.T) {
	const n, thr = 4, 3
	mats, err := RunDKGInProcessFaulty(n, thr, map[int]bool{1: true})
	if err != nil {
		t.Fatalf("faulty (f=1) DKG should complete excluding the faulty dealer, got: %v", err)
	}
	if len(mats) != n-1 {
		t.Fatalf("expected %d honest materials, got %d", n-1, len(mats))
	}
	// every honest node agrees on the same epoch master key (the QUAL-set key).
	for i := 1; i < len(mats); i++ {
		if !bytes.Equal(mats[i].EpochPub, mats[0].EpochPub) {
			t.Fatalf("honest node %d epoch key differs — DKG not consistent after exclusion", i)
		}
	}
	// and that key actually works: encrypt to it, reveal with t honest shares.
	client := NewClientScheme()
	payload := []byte("post-justification epoch key must decrypt")
	ct, err := client.Encrypt(mats[0].EpochPub, payload)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	sch, err := NewScheme(mats[0].Commits, n)
	if err != nil {
		t.Fatalf("NewScheme: %v", err)
	}
	shares := make([]reveal.RevealShare, 0, thr)
	for i := 0; i < thr; i++ {
		rs, err := sch.PartialReveal(mats[i].Share, ct)
		if err != nil {
			t.Fatalf("PartialReveal[%d]: %v", i, err)
		}
		shares = append(shares, rs)
	}
	got, err := sch.Combine(shares, ct)
	if err != nil {
		t.Fatalf("Combine after exclusion: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("decrypt mismatch after exclusion: %q != %q", got, payload)
	}
}

// TestJustificationFailsBelowFloor proves BUILD CONDITION 3: the security floor
// is enforced, not silently bypassed. With f=2 faulty dealers in an n=4 / t=3
// committee, only 2 dealers remain qualified (< t=3), so kyber ABORTS the DKG
// rather than completing over an insecure too-small qualified set. On chain this
// surfaces as no ≥t agreement → epoch FAILED (the prior key stays). Excluding
// faulty dealers can never silently drop the effective threshold below t.
func TestJustificationFailsBelowFloor(t *testing.T) {
	const n, thr = 4, 3
	_, err := RunDKGInProcessFaulty(n, thr, map[int]bool{1: true, 2: true})
	if err == nil {
		t.Fatalf("f=2 (qualified dealers %d < t=%d) must ABORT, not complete with an insecure threshold", n-2, thr)
	}
}
