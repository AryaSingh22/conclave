// Package zktest is the TEST-ONLY prover for ZK_DELIVERY. It embeds the proving
// key (the large half of the trusted setup) and produces proofs for the
// DeliveryCircuit so the verifier and the x/escrow keeper can be exercised
// end-to-end against the SAME embedded verifying key the chain uses. It is
// imported only by tests, so the proving key never enters the consensus binary.
//
// Proving uses fresh randomness (a groth16 proof is randomized / zero-knowledge)
// and is off-chain — this package is on no consensus path.
package zktest

import (
	"bytes"
	_ "embed"
	"fmt"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	bn254mimc "github.com/consensys/gnark-crypto/ecc/bn254/fr/mimc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"

	"github.com/AryaSingh22/conclave/crypto/zkdelivery/circuit"
)

//go:embed testdata/delivery_v1.pk
var deliveryV1PKBytes []byte

// Prove generates a ZK_DELIVERY proof that MiMC(payload) == schemaCommitment,
// bound to escrowID, returning the serialized proof bytes the chain's
// MsgSubmitDeliveryProof carries. escrowID and schemaCommitment use the same
// big-endian field encoding the on-chain verifier reconstructs (circuit
// .FieldFromBytes), so a proof for one escrow id will not verify against another.
func Prove(escrowID, schemaCommitment, payload []byte) ([]byte, error) {
	ccs, err := frontend.Compile(circuit.Curve.ScalarField(), r1cs.NewBuilder, &circuit.DeliveryCircuit{})
	if err != nil {
		return nil, fmt.Errorf("compile: %w", err)
	}
	pk := groth16.NewProvingKey(circuit.Curve)
	if _, err := pk.ReadFrom(bytes.NewReader(deliveryV1PKBytes)); err != nil {
		return nil, fmt.Errorf("read proving key: %w", err)
	}
	full, err := frontend.NewWitness(
		&circuit.DeliveryCircuit{
			EscrowID:         circuit.FieldFromBytes(escrowID),
			SchemaCommitment: circuit.FieldFromBytes(schemaCommitment),
			Payload:          circuit.FieldFromBytes(payload),
		},
		circuit.Curve.ScalarField(),
	)
	if err != nil {
		return nil, fmt.Errorf("witness: %w", err)
	}
	proof, err := groth16.Prove(ccs, pk, full)
	if err != nil {
		return nil, fmt.Errorf("prove: %w", err)
	}
	var buf bytes.Buffer
	if _, err := proof.WriteTo(&buf); err != nil {
		return nil, fmt.Errorf("serialize proof: %w", err)
	}
	return buf.Bytes(), nil
}

// SchemaCommitment returns the MiMC commitment a prover/seller would publish for
// a payload: the canonical 32-byte big-endian field element MiMC(payload). Tests
// use it to build a ZkDeliveryCondition.schema_commitment that a real payload
// opens. The native gnark-crypto bn254/fr MiMC matches the in-circuit gadget
// when fed the field element's canonical bytes (the same reduction
// circuit.FieldFromBytes applies), so MiMC(payload) here == MiMC(Payload) in
// the circuit.
func SchemaCommitment(payload []byte) []byte {
	var fe fr.Element
	fe.SetBytes(payload)
	canonical := fe.Bytes()
	h := bn254mimc.NewMiMC()
	h.Write(canonical[:])
	return h.Sum(nil)
}
