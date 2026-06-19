// Package reveal defines the ThresholdScheme interface behind the Veil's
// Reveal (SPEC §6.3, §8). Phase 0 ships StubScheme (identity); Phase 1
// supplies a BLS12-381 threshold-encryption implementation (crypto/threshold)
// behind this exact interface — the CONSUMING state machines (Encrypt,
// PartialReveal, Combine, Threshold) never change their signatures.
//
// Key GENERATION is deliberately NOT on this interface. The Phase-0 stub
// carried an EpochSetup(valset) ([]KeyShare) method, but a real distributed
// DKG never returns all key shares to a single caller (that defeats the
// threshold model). Per D-062 / PHASE1_DESIGN §3 (FLAG II), epoch key
// generation is a multi-round DKG sub-protocol with node-local shares; it
// lives in crypto/threshold (DKGParticipant), not here. This interface is
// the pure CONSUMING surface.
package reveal

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
	// Encrypt encrypts an intent payload to the epoch key (client side).
	Encrypt(epk EpochPubKey, payload []byte) ([]byte, error)
	// PartialReveal produces one validator's reveal share for a ciphertext.
	PartialReveal(share KeyShare, ct []byte) (RevealShare, error)
	// Combine recovers the plaintext from t-of-n reveal shares.
	Combine(shares []RevealShare, ct []byte) ([]byte, error)
	// Threshold returns the share threshold t for n validators (= 2n/3+1).
	Threshold(n int) int
}
