// Package reveal defines the ThresholdScheme interface behind the Veil's
// Reveal (SPEC §6.3, §8). Phase 0 ships StubScheme (identity); Phase 1
// replaces the implementation with BLS12-381 threshold encryption behind
// this exact interface — the consuming state machines never change.
package reveal

// Validator is the minimal validator identity a DKG epoch setup needs.
// Phase 0 carries it only to pin the §6.3 interface shape.
//
// CONCLAVE-TODO(phase-1): align with CometBFT validator updates when the
// epoch DKG lands (SPEC §8: epochs reuse validator-set update triggers).
type Validator struct {
	// Address is the validator's consensus address bytes.
	Address []byte
	// Power is the validator's voting power.
	Power int64
}

// EpochPubKey is the epoch-wide threshold public key intents encrypt to.
type EpochPubKey []byte

// KeyShare is one validator's secret key share from the epoch DKG.
type KeyShare []byte

// RevealShare is one validator's partial decryption of a ciphertext,
// transported in the vote extension of the ordering-fixing block (SPEC §8).
type RevealShare []byte

// ThresholdScheme is the SPEC §6.3 threshold-decryption interface. The
// order-then-reveal invariant (§1.1 goal 1) rests on Combine being
// impossible before ordering is fixed; in Phase 0's StubScheme that
// impossibility is enforced by the x/veil state machine instead of by
// cryptography, against the same interface.
type ThresholdScheme interface {
	// EpochSetup runs the epoch DKG over the validator set (Phase 1).
	EpochSetup(valset []Validator) (EpochPubKey, []KeyShare, error)
	// Encrypt encrypts an intent payload to the epoch key (client side).
	Encrypt(epk EpochPubKey, payload []byte) ([]byte, error)
	// PartialReveal produces one validator's reveal share for a ciphertext.
	PartialReveal(share KeyShare, ct []byte) (RevealShare, error)
	// Combine recovers the plaintext from t-of-n reveal shares.
	Combine(shares []RevealShare, ct []byte) ([]byte, error)
	// Threshold returns the share threshold t for n validators (= 2n/3+1).
	Threshold(n int) int
}
