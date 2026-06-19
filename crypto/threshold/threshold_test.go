package threshold

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/AryaSingh22/conclave/crypto/reveal"
)

// dkgFixture runs the in-process DKG and returns the reveal-capable scheme
// (built from the public commitments) plus every participant's local material.
func dkgFixture(t *testing.T, n int) (*Scheme, []*EpochKeyMaterial, int) {
	t.Helper()
	thr := (&Scheme{}).Threshold(n)
	mats, err := RunDKGInProcess(n, thr)
	if err != nil {
		t.Fatalf("RunDKGInProcess(%d,%d): %v", n, thr, err)
	}
	if len(mats) != n {
		t.Fatalf("got %d materials, want %d", len(mats), n)
	}
	sch, err := NewScheme(mats[0].Commits, n)
	if err != nil {
		t.Fatalf("NewScheme: %v", err)
	}
	return sch, mats, thr
}

// sharesFor produces a PartialReveal share from each listed participant.
func sharesFor(t *testing.T, sch *Scheme, mats []*EpochKeyMaterial, ct []byte, idxs ...int) []reveal.RevealShare {
	t.Helper()
	out := make([]reveal.RevealShare, 0, len(idxs))
	for _, i := range idxs {
		rs, err := sch.PartialReveal(mats[i].Share, ct)
		if err != nil {
			t.Fatalf("PartialReveal[%d]: %v", i, err)
		}
		out = append(out, rs)
	}
	return out
}

// TestRoundTripTofN proves Encrypt -> PartialReveal -> Combine recovers the
// plaintext from exactly t shares, across several committee sizes, and that the
// threshold matches the SPEC §6.3 rule t = ⌊2n/3⌋+1.
func TestRoundTripTofN(t *testing.T) {
	for _, n := range []int{4, 5, 7, 10} {
		sch, mats, thr := dkgFixture(t, n)
		if thr != 2*n/3+1 {
			t.Fatalf("n=%d threshold=%d want %d", n, thr, 2*n/3+1)
		}
		payload := []byte("sealed intent payload — order-then-reveal")
		client := NewClientScheme()
		ct, err := client.Encrypt(mats[0].EpochPub, payload)
		if err != nil {
			t.Fatalf("Encrypt: %v", err)
		}
		// exactly t shares (the first t participants)
		idxs := make([]int, thr)
		for i := range idxs {
			idxs[i] = i
		}
		shares := sharesFor(t, sch, mats, ct, idxs...)
		got, err := sch.Combine(shares, ct)
		if err != nil {
			t.Fatalf("n=%d Combine: %v", n, err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("n=%d recovered %q want %q", n, got, payload)
		}
	}
}

// TestThresholdExactBoundary proves t-1 shares CANNOT recover the plaintext and
// t shares CAN — verified at the raw recovery primitive (not merely that Combine
// returns an error): with t-1 shares the Lagrange recovery genuinely fails to
// reproduce the payload.
func TestThresholdExactBoundary(t *testing.T) {
	n := 7
	sch, mats, thr := dkgFixture(t, n)
	client := NewClientScheme()
	payload := []byte("threshold boundary check")
	ct, err := client.Encrypt(mats[0].EpochPub, payload)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	_, cdem, err := decodeCiphertext(ct)
	if err != nil {
		t.Fatalf("decodeCiphertext: %v", err)
	}
	ckem, _, _ := decodeCiphertext(ct)
	id := deriveIdentity(cdem)

	all := make([]int, n)
	for i := range all {
		all[i] = i
	}

	// t-1 shares: Combine refuses, AND the raw recovery cannot reproduce it.
	sub := sharesFor(t, sch, mats, ct, all[:thr-1]...)
	if _, err := sch.Combine(sub, ct); err != ErrInsufficientShares {
		t.Fatalf("t-1 Combine: got %v want ErrInsufficientShares", err)
	}
	rawSub := toBytes(sub)
	if pt, err := sch.recoverAndDecrypt(id, rawSub, ckem, cdem); err == nil && bytes.Equal(pt, payload) {
		t.Fatalf("t-1 shares recovered the plaintext — threshold broken")
	}

	// t shares: recovers.
	full := sharesFor(t, sch, mats, ct, all[:thr]...)
	got, err := sch.Combine(full, ct)
	if err != nil {
		t.Fatalf("t Combine: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("t shares recovered %q want %q", got, payload)
	}
}

// TestCorruptShareGateIsLoadBearing proves the verifiable-share gate is the SOLE
// guard against a poisoned Combine: a syntactically-valid-but-wrong share (a
// real G1 signature point that fails the pairing check under the claimed index)
// is deterministically EXCLUDED so Combine still recovers correctly — while the
// SAME shares fed to the ungated recovery path yield garbage. Remove the gate
// (use recoverAndDecrypt directly) and the test goes RED.
func TestCorruptShareGateIsLoadBearing(t *testing.T) {
	n := 7
	sch, mats, thr := dkgFixture(t, n)
	client := NewClientScheme()
	payload := []byte("poison-the-combine attempt")
	ct, err := client.Encrypt(mats[0].EpochPub, payload)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	ckem, cdem, err := decodeCiphertext(ct)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	id := deriveIdentity(cdem)

	// Genuine shares from nodes 0..thr-1.
	good := sharesFor(t, sch, mats, ct, rangeN(thr)...)

	// Forge a share that claims node 0's index but carries node thr's real
	// signature value: a valid G1 point that fails VerifyPartial under PK_0.
	donor := sharesFor(t, sch, mats, ct, thr)[0]
	forged := forgeIndex(donor, sigShareIndex(t, good[0]))

	// VerifyShare must reject the forgery and accept the genuine share.
	if err := sch.VerifyShare(forged, ct); err == nil {
		t.Fatalf("VerifyShare accepted a forged share")
	}
	if err := sch.VerifyShare(good[0], ct); err != nil {
		t.Fatalf("VerifyShare rejected a genuine share: %v", err)
	}

	// WITH the gate: forged + (t-1) genuine + one more genuine = enough good
	// shares survive, Combine recovers the true payload.
	mixed := append([]reveal.RevealShare{forged}, good[1:]...) // forged replaces good[0]
	mixed = append(mixed, sharesFor(t, sch, mats, ct, thr)[0]) // add a fresh genuine share to keep >= t good
	got, err := sch.Combine(mixed, ct)
	if err != nil {
		t.Fatalf("gated Combine errored on mixed set: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("gated Combine recovered %q want %q", got, payload)
	}

	// WITHOUT the gate: feed the ungated recovery path a t-sized set in which
	// the forged share replaces a genuine one. Recovery must NOT reproduce the
	// payload (this is what the gate prevents). RED if the gate were removed.
	poisoned := append([][]byte{[]byte(forged)}, toBytes(good[1:thr])...)
	if pt, err := sch.recoverAndDecrypt(id, poisoned, ckem, cdem); err == nil && bytes.Equal(pt, payload) {
		t.Fatalf("ungated recovery reproduced the plaintext from a poisoned set — gate is not load-bearing")
	}
}

// TestCombineDeterministic proves the on-chain Combine path is deterministic:
// identical plaintext from the same valid shares regardless of ordering, with no
// fresh randomness (SPEC §11.6).
func TestCombineDeterministic(t *testing.T) {
	n := 7
	sch, mats, thr := dkgFixture(t, n)
	client := NewClientScheme()
	payload := []byte("determinism on the on-chain path")
	ct, err := client.Encrypt(mats[0].EpochPub, payload)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	shares := sharesFor(t, sch, mats, ct, rangeN(thr)...)

	first, err := sch.Combine(shares, ct)
	if err != nil {
		t.Fatalf("Combine: %v", err)
	}
	// repeat + reversed order must yield byte-identical plaintext
	rev := make([]reveal.RevealShare, len(shares))
	for i := range shares {
		rev[len(shares)-1-i] = shares[i]
	}
	for _, set := range [][]reveal.RevealShare{shares, rev, shares} {
		got, err := sch.Combine(set, ct)
		if err != nil {
			t.Fatalf("Combine repeat: %v", err)
		}
		if !bytes.Equal(got, first) {
			t.Fatalf("Combine non-deterministic: %q vs %q", got, first)
		}
	}

	// PartialReveal is itself deterministic (BLS signature shares carry no nonce).
	a, _ := sch.PartialReveal(mats[0].Share, ct)
	b, _ := sch.PartialReveal(mats[0].Share, ct)
	if !bytes.Equal(a, b) {
		t.Fatalf("PartialReveal non-deterministic")
	}
}

// TestDKGSharesAreNodeLocal proves the DKG output keeps shares node-local
// (D-062): every participant holds a DISTINCT secret share, but they all agree
// on the SAME epoch public key and commitments.
func TestDKGSharesAreNodeLocal(t *testing.T) {
	n := 5
	_, mats, _ := dkgFixture(t, n)
	for i := 1; i < n; i++ {
		if !bytes.Equal(mats[i].EpochPub, mats[0].EpochPub) {
			t.Fatalf("participants disagree on epoch public key")
		}
		if bytes.Equal(mats[i].Share, mats[0].Share) {
			t.Fatalf("participants %d and 0 hold identical secret shares", i)
		}
	}
}

// --- test helpers ---

func toBytes(shares []reveal.RevealShare) [][]byte {
	out := make([][]byte, len(shares))
	for i := range shares {
		out[i] = []byte(shares[i])
	}
	return out
}

func rangeN(k int) []int {
	out := make([]int, k)
	for i := range out {
		out[i] = i
	}
	return out
}

func sigShareIndex(t *testing.T, rs reveal.RevealShare) int {
	t.Helper()
	if len(rs) < 2 {
		t.Fatalf("share too short")
	}
	return int(binary.BigEndian.Uint16([]byte(rs)[:2]))
}

// forgeIndex returns donor's bytes (a valid signature point) re-labelled to
// claim claimIdx — a syntactically valid share that fails the pairing check.
func forgeIndex(donor reveal.RevealShare, claimIdx int) reveal.RevealShare {
	b := append([]byte(nil), []byte(donor)...)
	binary.BigEndian.PutUint16(b[:2], uint16(claimIdx))
	return reveal.RevealShare(b)
}
