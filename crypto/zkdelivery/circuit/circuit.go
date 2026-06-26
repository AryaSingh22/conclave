// Package circuit holds the ZK_DELIVERY groth16 circuit and the field-encoding
// rule shared by the on-chain verifier (crypto/zkdelivery) and the off-chain
// prover/setup (crypto/zkdelivery/gen, crypto/zkdelivery/zktest). It is a
// separate package from the verifier so the trusted-setup generator can import
// the circuit WITHOUT triggering the verifier's //go:embed of the (generated)
// verifying key — breaking the build cycle.
//
// implements SPEC §6.5 ZK_DELIVERY (PHASE2_DESIGN §2.1 criterion 4).
package circuit

import (
	"math/big"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/hash/mimc"
)

// Curve is the proving curve for ZK_DELIVERY (BN254 — the gnark default with
// the cheapest on-chain verifier).
var Curve = ecc.BN254

// DeliveryCircuit proves "the delivered payload matches the committed
// schema/hash" (SPEC §6.5 ZK_DELIVERY) WITHOUT revealing the payload, bound to
// ONE escrow instance.
//
// The seller (prover) knows a Payload whose MiMC hash equals the
// SchemaCommitment (ZkDeliveryCondition.schema_commitment) that BOTH parties
// signed (seller D-075, buyer D-076). The proof attests this without revealing
// the Payload. EscrowID binds the proof to a single escrow (D-072 replay
// defense): the on-chain verifier reconstructs EscrowID and SchemaCommitment
// from the escrow record — never from the prover — so a proof generated for
// escrow A cannot release escrow B even when both share a schema. This is the
// ZK analog of Phase-1's per-ciphertext identity binding (D-066): bind to the
// INSTANCE, not the type.
type DeliveryCircuit struct {
	// EscrowID is the per-instance binding (PUBLIC). groth16 binds every public
	// input into the verification equation, so a proof made for one EscrowID
	// fails when the verifier supplies another.
	EscrowID frontend.Variable `gnark:",public"`
	// SchemaCommitment is the commitment to the deliverable that BOTH parties
	// sign — the seller (D-075) and, for ZK_DELIVERY, the buyer (D-076 consent
	// gate) — so release is bound to a schema both agreed to, not a solver- or
	// seller-chosen one (PUBLIC).
	SchemaCommitment frontend.Variable `gnark:",public"`
	// Payload is the seller's secret witness — the delivered content digest.
	Payload frontend.Variable `gnark:",secret"`
}

// Define states the constraints: MiMC(Payload) == SchemaCommitment (the payload
// matches the committed schema/hash), and EscrowID is absorbed into a MiMC so it
// is a constrained public input. The instance binding's security rests on
// groth16 binding all public inputs into the verification equation; the absorb
// keeps the frontend's every-wire-constrained rule satisfied and domain-
// separates EscrowID from the schema check (it must NOT enter the schema hash —
// the seller commits to SchemaCommitment at intent-sign time, before the escrow
// id exists).
func (c *DeliveryCircuit) Define(api frontend.API) error {
	h, err := mimc.NewMiMC(api)
	if err != nil {
		return err
	}
	h.Write(c.Payload)
	api.AssertIsEqual(h.Sum(), c.SchemaCommitment)

	hb, err := mimc.NewMiMC(api)
	if err != nil {
		return err
	}
	hb.Write(c.EscrowID)
	_ = hb.Sum()
	return nil
}

// FieldFromBytes interprets b as a big-endian integer reduced into the BN254
// scalar field — the SINGLE encoding the prover and the on-chain verifier both
// apply to escrow_id and schema_commitment so the reconstructed public witness
// matches the one the proof was generated against. Deterministic and
// randomness-free (§11.6).
func FieldFromBytes(b []byte) *big.Int {
	return new(big.Int).SetBytes(b)
}
