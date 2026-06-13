package reveal

// StubScheme is the Phase-0 ThresholdScheme (SPEC §6.3 STUB mode):
// "encryption" is the identity function, so SealedIntent.ciphertext carries
// plaintext IntentPayload bytes and Reveal (Combine) returns them unchanged.
// The order-then-reveal invariant stays real: x/veil only calls Combine in
// the BeginBlock strictly after the EndBlock that fixed ordering.
//
// CONCLAVE-TODO(phase-1): replace with the BLS12-381 implementation per
// SPEC §8 (library evaluation first — do not implement DKG ad hoc).
type StubScheme struct{}

var _ ThresholdScheme = StubScheme{}

// EpochSetup is a no-op in stub mode: there is no epoch key material.
func (StubScheme) EpochSetup(valset []Validator) (EpochPubKey, []KeyShare, error) {
	return EpochPubKey{}, make([]KeyShare, len(valset)), nil
}

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
