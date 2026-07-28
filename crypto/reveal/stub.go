package reveal

// StubScheme is the identity ThresholdScheme (SPEC §6.3 STUB mode):
// "encryption" is the identity function, so SealedIntent.ciphertext carries
// plaintext IntentPayload bytes and Reveal (Combine) returns them unchanged.
// The order-then-reveal invariant stays real: x/veil only calls Combine in
// the BeginBlock strictly after the EndBlock that fixed ordering.
//
// NOT A TODO — this is a recorded design decision. The former phase-1 marker
// here asked to "replace with the BLS12-381 implementation
// per SPEC §8", and tasks 1.3/1.6 DID: `crypto/threshold` is the real
// tlock-style IBE-over-threshold-BLS scheme (D-066), and the live Reveal
// selects between the two PER INTENT on the envelope's `epoch_id` (D-069) —
// `epoch_id == 0` routes here, anything else routes to
// `threshold.NewSchemeFromCommitBytes`.
//
// StubScheme therefore persists deliberately as the **epoch-0 bootstrap
// window**: from genesis until the first DKG reaches ACTIVE there is no epoch
// key, so intents are effectively plaintext. That is limitation **L1** in
// docs/AUDIT_SCOPE_1.8.md, and the honest security claim is stated there:
// operators MUST NOT route sensitive flow before epoch 1 is ACTIVE. Its
// operational detection is owned by task 3.7 (the epoch-0 alarm) and its
// genesis drill by 3.8; a protocol-level `require_encryption` gate is a 3.8
// plan-stop proposal. Removing StubScheme is not the goal — closing the window
// operationally is.
//
// (Marker retired at PHASE2_DESIGN Rev 3, D-090: a completed decision must not
// keep wearing a TODO's clothing, or the greppable-marker discipline decays
// into noise — AUDIT F-8's class.)
type StubScheme struct{}

var _ ThresholdScheme = StubScheme{}

// Encrypt is the identity: the "ciphertext" is the plaintext payload.
func (StubScheme) Encrypt(_ EpochPubKey, payload []byte) ([]byte, error) {
	return payload, nil
}

// PartialReveal returns an empty share: stub Combine needs none.
func (StubScheme) PartialReveal(_ KeyShare, _ []byte) (RevealShare, error) {
	return RevealShare{}, nil
}

// Combine is the identity Reveal: the plaintext is the ciphertext.
func (StubScheme) Combine(_ []RevealShare, ct []byte) ([]byte, error) {
	return ct, nil
}

// Threshold implements the SPEC §6.3 t-of-n rule: t = 2n/3 + 1.
func (StubScheme) Threshold(n int) int {
	return 2*n/3 + 1
}
