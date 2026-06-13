package types

import (
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
)

// ValidateEnvelope unmarshals and validates a plaintext IntentEnvelope per
// SPEC §6.3 "validates envelope" (D-042 rules). The envelope is the ONLY
// part of an intent the msg server inspects — payload (ciphertext)
// malformedness is the Reveal's job even in stub mode (§6.3 INVALID path;
// Phase 1 cannot inspect ciphertexts at submission).
//
// Rules:
//   - the bytes must unmarshal as IntentEnvelope;
//   - ttl_height must be strictly beyond the submission height;
//   - chamber_id must be 0 until Phase 3 (SPEC §1.2);
//   - fee_bid must be empty (= zero bid) or parse as a non-negative
//     decimal — D-035 lets lane ordering tolerate garbage bids precisely
//     because this validation rejects them at execution.
func ValidateEnvelope(bz []byte, submitHeight uint64) (IntentEnvelope, error) {
	var env IntentEnvelope
	if err := env.Unmarshal(bz); err != nil {
		return env, errorsmod.Wrapf(ErrInvalidEnvelope, "undecodable envelope: %s", err)
	}
	if env.TtlHeight <= submitHeight {
		return env, errorsmod.Wrapf(ErrInvalidEnvelope,
			"ttl_height %d not beyond submit height %d", env.TtlHeight, submitHeight)
	}
	if env.ChamberId != 0 {
		return env, errorsmod.Wrapf(ErrInvalidChamber, "envelope chamber_id %d", env.ChamberId)
	}
	if env.FeeBid != "" {
		bid, err := math.LegacyNewDecFromStr(env.FeeBid)
		if err != nil {
			return env, errorsmod.Wrapf(ErrInvalidEnvelope, "fee_bid %q: %s", env.FeeBid, err)
		}
		if bid.IsNegative() {
			return env, errorsmod.Wrapf(ErrInvalidEnvelope, "fee_bid %q is negative", env.FeeBid)
		}
	}
	return env, nil
}
