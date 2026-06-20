package threshold

// Task 1.4 — OFFLINE adversarial harness for the Task-1.3 threshold construction
// (D-066). Where threshold_test.go ASSERTED the threshold / gate / determinism
// properties on happy-path inputs, these tests PROVE them under an adversary:
//
//   - SECRECY: a coalition of up-to-(t-1) malicious validators holding their REAL
//     secret key shares learns nothing (TestSecrecyCoalitionBelowThreshold).
//   - ROBUSTNESS (the property D-066 says the GATE actually guarantees —
//     liveness, not silent-corruption-prevention): withheld shares don't block a
//     decryptable reveal (TestRobustnessWithheldShares); f forged shares cannot
//     force ErrInsufficientShares when >= t honest shares exist
//     (TestRobustnessCorruptCannotForceInsufficient — LOAD-BEARING, RED without
//     the gate); and a forged share cannot fake progress to t — it collapses to
//     the "not yet" retry signal, not a wrong plaintext or a different error
//     (TestForgedShareCannotFakeQuorum — LOAD-BEARING for the semantic).
//   - CHURN / RESHARING (the 1.5/1.6 prerequisite): in-flight ciphertexts drain
//     under their own epoch's shares across a validator-set change, and epochs are
//     isolated (TestChurnDrainBeforeRotate); the threshold tolerates up to n-t
//     leavers (TestChurnLeaverTolerance); a permanently-stuck reveal yields a
//     terminal, deterministic, PURE "cannot decrypt" signal — the crypto
//     precondition for CANCEL-as-drain-terminus (TestStuckSessionTerminalSignal).
//
// The adversary is genuinely adversarial: forged shares are real G1 points that
// deserialize and reach the pairing check, then FAIL it (index-forge via
// forgeIndex, and cross-ciphertext replay via crossCtForgery); coalitions hold
// real secret shares; withholding/leaving is real absence. No happy path is
// relabeled. OFFLINE only — StubScheme stays the live chain scheme, no consensus
// wiring (that is Task 1.6). The 1.3 helpers in threshold_test.go are reused.

import (
	"bytes"
	"errors"
	"testing"

	"github.com/drand/kyber/share"
	"github.com/drand/kyber/util/random"

	"github.com/AryaSingh22/conclave/crypto/reveal"
)

// ungatedRecover runs the recovery path WITHOUT the verifiable-share gate — the
// raw, trusting primitive that Combine calls only AFTER gating. It is the bypass
// used to prove the gate is load-bearing: a forged share the gate would exclude
// here reaches share.RecoverCommit (lowest-t-by-index selection) and the IBE FO
// check, where it can poison recovery.
func ungatedRecover(t *testing.T, sch *Scheme, ct []byte, shares []reveal.RevealShare) ([]byte, error) {
	t.Helper()
	ckem, cdem, err := decodeCiphertext(ct)
	if err != nil {
		t.Fatalf("decodeCiphertext: %v", err)
	}
	return sch.recoverAndDecrypt(deriveIdentity(cdem), toBytes(shares), ckem, cdem)
}

// crossCtForgery builds a syntactically-valid-but-wrong share: node donorIdx's
// GENUINE partial reveal on a DIFFERENT ciphertext (otherCt), relabelled to claim
// claimIdx. The bytes deserialize as a real G1 point and reach VerifyPartial, but
// σ = sk_donor·H(id_other) ≠ sk_claim·H(id_target), so the pairing check fails —
// a replayed/stale share, the second corruption flavor alongside forgeIndex.
func crossCtForgery(t *testing.T, sch *Scheme, mats []*EpochKeyMaterial, otherCt []byte, donorIdx, claimIdx int) reveal.RevealShare {
	t.Helper()
	rs, err := sch.PartialReveal(mats[donorIdx].Share, otherCt)
	if err != nil {
		t.Fatalf("PartialReveal(cross-ct): %v", err)
	}
	return forgeIndex(rs, claimIdx)
}

// withShare appends one share to a fresh copy of base (never mutating base).
func withShares(base []reveal.RevealShare, extra ...reveal.RevealShare) []reveal.RevealShare {
	out := append([]reveal.RevealShare{}, base...)
	return append(out, extra...)
}

// TestSecrecyCoalitionBelowThreshold proves secrecy holds below the threshold
// against a coalition of t-1 MALICIOUS validators who pool their REAL secret key
// shares (not merely t-1 reveal shares — they can sign any ciphertext) plus all
// public material and a target ciphertext they did not author. They cannot
// recover; fabricating a t-th share cannot fake quorum; and the moment a genuine
// t-th honest holder joins, recovery succeeds (proving the failure was the
// threshold, not breakage). Distinct from 1.3's passive t-1 reveal-share subset:
// here the adversary holds the secret material and actively attacks.
func TestSecrecyCoalitionBelowThreshold(t *testing.T) {
	const n = 7
	sch, mats, thr := dkgFixture(t, n)
	client := NewClientScheme()
	payload := []byte("coalition must learn nothing below threshold")
	ct, err := client.Encrypt(mats[0].EpochPub, payload)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// The coalition: t-1 validators (nodes 0..thr-2) producing honest reveal
	// shares from their real secret shares.
	coalition := sharesFor(t, sch, mats, ct, rangeN(thr-1)...)

	// (1) Combine refuses with the "not yet" signal — distinct from a malformed
	// rejection (D-066: t-1 VALID shares -> ErrInsufficientShares, a retry signal).
	if _, err := sch.Combine(coalition, ct); !errors.Is(err, ErrInsufficientShares) {
		t.Fatalf("t-1 coalition Combine: got %v want ErrInsufficientShares", err)
	}

	// (2) Information-theoretic secrecy at the raw primitive: with t-1 shares the
	// degree-(t-1) polynomial is underdetermined, share.RecoverCommit cannot
	// produce a σ, so the coalition obtains NO DEK and cannot open the DEM. There
	// is no "wrong σ" — only failure (D-066, Shamir perfect secrecy).
	if pt, err := ungatedRecover(t, sch, ct, coalition); err == nil && bytes.Equal(pt, payload) {
		t.Fatalf("t-1 coalition recovered the plaintext — secrecy broken")
	}

	// (3) The coalition fabricates a t-th share to fake quorum: a forged share at
	// a slot it does not hold. The gate strips it -> still t-1 valid -> still the
	// "not yet" signal. A bad share cannot manufacture the missing honest holder.
	donor := sharesFor(t, sch, mats, ct, thr)[0]
	faked := withShares(coalition, forgeIndex(donor, thr+1))
	if _, err := sch.Combine(faked, ct); !errors.Is(err, ErrInsufficientShares) {
		t.Fatalf("coalition+forged Combine: got %v want ErrInsufficientShares", err)
	}

	// Positive control ("t can"): a genuine t-th honest holder (node thr-1) joins
	// -> recovery succeeds.
	full := withShares(coalition, sharesFor(t, sch, mats, ct, thr-1)[0])
	got, err := sch.Combine(full, ct)
	if err != nil {
		t.Fatalf("t honest Combine: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("t honest recovered %q want %q", got, payload)
	}
}

// TestRobustnessWithheldShares proves silent validators cannot block a
// decryptable reveal: with n-t validators withholding, ANY t honest shares
// recover — from arbitrary honest subsets, not only the first t (robustness to
// WHICH validators are silent). Functional (holds with or without the gate, since
// every submitted share is honest).
func TestRobustnessWithheldShares(t *testing.T) {
	const n = 7
	sch, mats, thr := dkgFixture(t, n) // thr=5, so up to n-thr=2 may withhold
	client := NewClientScheme()
	payload := []byte("withheld shares must not block a decryptable reveal")
	ct, err := client.Encrypt(mats[0].EpochPub, payload)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	subsets := [][]int{
		rangeN(thr),     // {0..thr-1}
		{2, 3, 4, 5, 6}, // a different thr-subset (nodes 0,1 silent)
		{0, 1, 3, 5, 6}, // a scattered thr-subset (nodes 2,4 silent)
	}
	for _, idxs := range subsets {
		if len(idxs) != thr {
			t.Fatalf("subset %v size %d want thr=%d", idxs, len(idxs), thr)
		}
		got, err := sch.Combine(sharesFor(t, sch, mats, ct, idxs...), ct)
		if err != nil {
			t.Fatalf("withheld subset %v Combine: %v", idxs, err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("subset %v recovered %q want %q", idxs, got, payload)
		}
	}
}

// TestRobustnessCorruptCannotForceInsufficient is the LOAD-BEARING robustness
// test. It proves the precise property: when >= t honest shares exist, no forged
// shares can force ErrInsufficientShares. f forged shares (f in {1, n-t}) are
// placed at the LOW indices {0,1} so that kyber's RecoverCommit — which selects
// the lowest-t shares by index (xyCommit; pinned by the control+guard below) —
// would pick them over the honest HIGH-index shares in an ungated combiner.
//
// Two assertions, NOT one conflated pass:
//   - HALF 1 (ROBUSTNESS): gated Combine excludes the forged in O(n), the t honest
//     shares survive, the true payload is recovered. f=1 is the most important
//     case: a SINGLE bad validator cannot DoS a decryptable reveal (1.6 liveness).
//   - HALF 2 (THE GATE IS WHY): the SAME set through the ungated path selects the
//     low-index forged shares and FAILS to reproduce the payload.
//
// HALF 2 fails LOUDLY if the gate is removed from Combine OR if kyber changes
// RecoverCommit's lowest-index selection — either makes the gate non-load-bearing,
// and this test must catch that, not pass silently.
func TestRobustnessCorruptCannotForceInsufficient(t *testing.T) {
	const n = 7
	sch, mats, thr := dkgFixture(t, n) // thr=5, n-thr=2
	honestIdx := []int{2, 3, 4, 5, 6}  // the HIGH indices; exactly thr honest
	if len(honestIdx) != thr {
		t.Fatalf("expected thr=%d honest indices, have %d", thr, len(honestIdx))
	}

	for _, f := range []int{1, 2} { // f=1 (single bad validator) and f=n-thr (max)
		client := NewClientScheme()
		payload := []byte("a decryptable reveal survives forged shares")
		ct, err := client.Encrypt(mats[0].EpochPub, payload)
		if err != nil {
			t.Fatalf("f=%d Encrypt: %v", f, err)
		}
		honest := sharesFor(t, sch, mats, ct, honestIdx...)

		// f forged shares at LOW indices 0..f-1: real G1 points (high-index honest
		// values as donors) re-labelled, so they deserialize and reach the pairing
		// check but fail it under the claimed low-index public key.
		forged := make([]reveal.RevealShare, 0, f)
		for i := 0; i < f; i++ {
			donor := sharesFor(t, sch, mats, ct, honestIdx[len(honestIdx)-1-i])[0]
			forged = append(forged, forgeIndex(donor, i))
		}

		// CONTROL: the honest thr-set alone recovers through the UNGATED path. This
		// isolates causation — any subsequent ungated failure is caused by the
		// forged low-index shares, not by the honest set being unusable.
		if pt, err := ungatedRecover(t, sch, ct, honest); err != nil || !bytes.Equal(pt, payload) {
			t.Fatalf("f=%d ungated honest-only failed (pt=%q err=%v) — control broken", f, pt, err)
		}

		mixed := append(append([]reveal.RevealShare{}, forged...), honest...)

		// HALF 1 — ROBUSTNESS: gated Combine recovers the true payload.
		got, err := sch.Combine(mixed, ct)
		if err != nil {
			t.Fatalf("f=%d gated Combine errored on (t honest + forged): %v", f, err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("f=%d gated Combine recovered %q want %q", f, got, payload)
		}

		// HALF 2 — THE GATE IS WHY: the same set through the ungated path selects
		// the low-index forged shares and fails to reproduce the payload. RED if the
		// gate is removed OR kyber's lowest-index selection changes.
		if pt, err := ungatedRecover(t, sch, ct, mixed); err == nil && bytes.Equal(pt, payload) {
			t.Fatalf("f=%d ungated recovery reproduced the plaintext from a poisoned set — "+
				"gate not load-bearing OR RecoverCommit selection rule changed", f)
		}
	}
}

// TestKyberRecoverCommitSelectsLowestIndex is the standalone, payload-independent
// probe pinning the LOAD-BEARING PREMISE of the corrupt-share strategy: kyber's
// share.RecoverCommit selects the lowest-t shares BY INDEX (poly.go xyCommit sorts
// byIndexPub, then breaks at len==t). This is undocumented-in-contract kyber
// behavior — it would change, if ever, via a silent dependency bump, not this diff.
//
// TestRobustnessCorruptCannotForceInsufficient forges shares at LOW indices
// PRECISELY so an ungated combiner SELECTS them; if a future kyber picked the
// honest subset instead, the forged share would be excluded by SELECTION (not by
// the gate) and HALF 2 there would pass VACUOUSLY — green for the wrong reason.
// This probe fails BY NAME at the real broken assumption, not disguised as a gate
// or forgery failure. It carries no payload, no gate, no IBE — only the selection
// rule, isolated.
func TestKyberRecoverCommitSelectsLowestIndex(t *testing.T) {
	group := newSuite().G1() // the exact group RecoverCommit runs in (recoverAndDecrypt)
	const t1, n1 = 3, 6

	// Two DISTINCT secrets with DISTINCT commitments g^secret.
	secretA := group.Scalar().SetInt64(11)
	secretB := group.Scalar().SetInt64(22)
	commitA := group.Point().Mul(secretA, nil)
	commitB := group.Point().Mul(secretB, nil)
	if commitA.Equal(commitB) {
		t.Fatalf("probe misconstructed: commitA == commitB")
	}

	aShares := share.NewPriPoly(group, t1, secretA, random.New()).Shares(n1)
	bShares := share.NewPriPoly(group, t1, secretB, random.New()).Shares(n1)
	pub := func(ps *share.PriShare) *share.PubShare {
		return &share.PubShare{I: ps.I, V: group.Point().Mul(ps.V, nil)}
	}

	// LOW indices {0,1,2} encode secret A; HIGH indices {3,4,5} encode secret B.
	low := []*share.PubShare{pub(aShares[0]), pub(aShares[1]), pub(aShares[2])}
	high := []*share.PubShare{pub(bShares[3]), pub(bShares[4]), pub(bShares[5])}

	// Constructed-case arms — each homogeneous t-subset recovers its OWN secret. The
	// HIGH arm proves a selection rule that picked high indices would return commitB
	// (≠ commitA): i.e., the pin below WOULD FAIL if RecoverCommit selected
	// differently. This is the "would fail if selected differently" evidence, in-test.
	if got, err := share.RecoverCommit(group, low, t1, n1); err != nil || !got.Equal(commitA) {
		t.Fatalf("low subset recovered (%v, %v), want commitA", got, err)
	}
	if got, err := share.RecoverCommit(group, high, t1, n1); err != nil || !got.Equal(commitB) {
		t.Fatalf("high subset recovered (%v, %v), want commitB", got, err)
	}

	// THE PIN: feed all 2t shares (low=A, high=B). Lowest-t-by-index selection picks
	// {0,1,2} -> commitA. If kyber ever selects the high indices, or self-searches a
	// consistent subset, this returns commitB and fails HERE, by name, at the
	// premise — not as a disguised gate/forgery failure in the corrupt-share test.
	mixed := append(append([]*share.PubShare{}, low...), high...)
	got, err := share.RecoverCommit(group, mixed, t1, n1)
	if err != nil {
		t.Fatalf("mixed RecoverCommit: %v", err)
	}
	if !got.Equal(commitA) {
		t.Fatalf("RecoverCommit did NOT select the lowest-t shares by index " +
			"(got commitB-or-other, want commitA) — kyber selection rule changed; the " +
			"low-index forgery premise of TestRobustnessCorruptCannotForceInsufficient is broken")
	}
}

// TestForgedShareCannotFakeQuorum proves the "not yet vs rejected" semantic under
// adversary: t-1 genuine shares + forged shares collapse to t-1 VALID -> the clean
// ErrInsufficientShares retry signal, NOT a recovered wrong plaintext and NOT some
// other error. Because the gate runs FIRST, a forged share cannot fake progress to
// t. LOAD-BEARING for the semantic: without the gate this set would surface an
// FO/decrypt error instead of the clean retry signal that 1.6's retry/CANCEL
// counter keys on. Exercises both forgery flavors (cross-ciphertext replay and
// index-forge flood).
func TestForgedShareCannotFakeQuorum(t *testing.T) {
	const n = 7
	sch, mats, thr := dkgFixture(t, n)
	client := NewClientScheme()
	payload := []byte("a forged share cannot fake progress to t")
	ct, err := client.Encrypt(mats[0].EpochPub, payload)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	otherCt, err := client.Encrypt(mats[0].EpochPub, []byte("a different intent"))
	if err != nil {
		t.Fatalf("Encrypt(other): %v", err)
	}

	honest := sharesFor(t, sch, mats, ct, rangeN(thr-1)...) // t-1 genuine

	// Cross-ciphertext replay: node thr's genuine share on otherCt, submitted
	// against THIS ct. Deserializes, fails the pairing check.
	replay := crossCtForgery(t, sch, mats, otherCt, thr, thr)
	if err := sch.VerifyShare(replay, ct); err == nil {
		t.Fatalf("VerifyShare accepted a cross-ciphertext replay")
	}
	if _, err := sch.Combine(withShares(honest, replay), ct); !errors.Is(err, ErrInsufficientShares) {
		t.Fatalf("t-1 honest + 1 replay: got %v want ErrInsufficientShares", err)
	}

	// A FLOOD of index-forged shares at every silent slot cannot manufacture quorum
	// either — the gate strips them all -> still t-1 valid. Each forgery uses a
	// donor whose index differs from the claimed slot so it fails the pairing check.
	flood := append([]reveal.RevealShare{}, honest...)
	for idx := thr; idx < n; idx++ {
		donor := sharesFor(t, sch, mats, ct, (idx+1)%n)[0]
		flood = append(flood, forgeIndex(donor, idx))
	}
	if _, err := sch.Combine(flood, ct); !errors.Is(err, ErrInsufficientShares) {
		t.Fatalf("t-1 honest + forged flood: got %v want ErrInsufficientShares", err)
	}
}

// TestChurnDrainBeforeRotate proves drain-before-rotate at the crypto layer: an
// in-flight ciphertext sealed to epoch e's key remains decryptable under epoch e's
// shares AFTER an independent epoch e+1 DKG exists (a validator-set change), while
// epoch e+1's shares CANNOT decrypt an epoch-e ciphertext (cross-epoch isolation —
// why clients tag each ciphertext with its epoch id and the Reveal selects the
// matching share set). The two epochs are two independent RunDKGInProcess runs.
func TestChurnDrainBeforeRotate(t *testing.T) {
	const n = 7
	schE, matsE, thr := dkgFixture(t, n)
	schE1, matsE1, _ := dkgFixture(t, n) // the rotated epoch (fresh DKG)

	if bytes.Equal(matsE[0].EpochPub, matsE1[0].EpochPub) {
		t.Fatalf("epoch e and e+1 share an epoch public key — not an independent rotation")
	}

	client := NewClientScheme()
	payload := []byte("in-flight under epoch e must drain under epoch e")
	ct, err := client.Encrypt(matsE[0].EpochPub, payload)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// drain-before-rotate: epoch e's shares still decrypt the in-flight ct after
	// epoch e+1 exists.
	got, err := schE.Combine(sharesFor(t, schE, matsE, ct, rangeN(thr)...), ct)
	if err != nil {
		t.Fatalf("epoch-e drain Combine: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("epoch-e drain recovered %q want %q", got, payload)
	}

	// Cross-epoch isolation: epoch e+1's (valid, gate-passing) shares recover a
	// DIFFERENT threshold signature; the IBE FO check then errors on the epoch-e
	// ciphertext — it never decrypts to e's plaintext.
	if pt, err := schE1.Combine(sharesFor(t, schE1, matsE1, ct, rangeN(thr)...), ct); err == nil && bytes.Equal(pt, payload) {
		t.Fatalf("epoch e+1 shares decrypted an epoch-e ciphertext — epochs not isolated")
	}
}

// TestChurnLeaverTolerance proves the threshold tolerates up to n-t leavers for
// in-flight ciphertexts: with exactly n-t validators gone (their shares absent),
// the remaining t shares still decrypt; one more leaver crosses the bound and
// yields the "not yet" signal, never silent loss.
func TestChurnLeaverTolerance(t *testing.T) {
	const n = 7
	sch, mats, thr := dkgFixture(t, n) // thr=5, tolerates n-thr=2 leavers
	client := NewClientScheme()
	payload := []byte("threshold tolerates up to n-t leavers in-flight")
	ct, err := client.Encrypt(mats[0].EpochPub, payload)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Exactly n-thr leavers (nodes thr..n-1 gone): thr shares remain -> recovers.
	got, err := sch.Combine(sharesFor(t, sch, mats, ct, rangeN(thr)...), ct)
	if err != nil {
		t.Fatalf("n-thr leavers Combine: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("n-thr leavers recovered %q want %q", got, payload)
	}

	// One more leaver (n-thr+1 missing): only thr-1 shares remain -> the terminal
	// "not yet" signal.
	if _, err := sch.Combine(sharesFor(t, sch, mats, ct, rangeN(thr-1)...), ct); !errors.Is(err, ErrInsufficientShares) {
		t.Fatalf("n-thr+1 leavers: got %v want ErrInsufficientShares", err)
	}
}

// TestStuckSessionTerminalSignal proves the crypto precondition for
// CANCEL-as-drain-terminus: a permanently-undecryptable reveal (only t-1 shares
// will ever exist) yields a TERMINAL, DETERMINISTIC, PURE "cannot decrypt" signal.
// Combine over the same insufficient set N times returns byte-identical
// ErrInsufficientShares with no state mutation between calls, and a sufficient
// Combine still succeeds afterward — so 1.6's retry/CANCEL counter can rely on
// Combine being a pure function of its inputs.
//
// SCOPE: the session-state CANCEL + "rotation released" assertion lives in
// x/session / x/veil and is Task 1.5/1.6; the grace-window MECHANISM (bounded
// epoch-material retention / overlap) is Task 1.5 (its crypto basis is proven by
// TestChurnDrainBeforeRotate). Here we prove ONLY the terminal, deterministic,
// pure signal the 1.5/1.6 state machine consumes — neither is pulled into 1.4.
func TestStuckSessionTerminalSignal(t *testing.T) {
	const n = 7
	sch, mats, thr := dkgFixture(t, n)
	client := NewClientScheme()
	payload := []byte("a stuck reveal yields a terminal, pure signal")
	ct, err := client.Encrypt(mats[0].EpochPub, payload)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	stuck := sharesFor(t, sch, mats, ct, rangeN(thr-1)...) // can never reach t

	const retries = 5
	var firstErr string
	for i := 0; i < retries; i++ {
		pt, err := sch.Combine(stuck, ct)
		if !errors.Is(err, ErrInsufficientShares) {
			t.Fatalf("retry %d: got (%q, %v) want ErrInsufficientShares", i, pt, err)
		}
		if i == 0 {
			firstErr = err.Error()
		} else if err.Error() != firstErr {
			t.Fatalf("retry %d: error text %q != first %q — Combine not deterministic", i, err.Error(), firstErr)
		}
	}

	// PURITY / no state mutation: after the repeated insufficient retries the SAME
	// scheme still recovers a sufficient set, and the sufficient Combine is itself
	// repeatable byte-for-byte (Combine is a pure function of its inputs).
	full := sharesFor(t, sch, mats, ct, rangeN(thr)...)
	a, err := sch.Combine(full, ct)
	if err != nil {
		t.Fatalf("sufficient Combine after retries: %v", err)
	}
	b, err := sch.Combine(full, ct)
	if err != nil {
		t.Fatalf("sufficient Combine repeat: %v", err)
	}
	if !bytes.Equal(a, payload) || !bytes.Equal(a, b) {
		t.Fatalf("Combine not pure: a=%q b=%q want %q", a, b, payload)
	}
}
