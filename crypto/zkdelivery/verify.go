// Package zkdelivery is the on-chain ZK_DELIVERY groth16 verifier (SPEC §6.5,
// PHASE2_DESIGN §2.1 criterion 4, task 2.1b). It exposes a single deterministic,
// randomness-free, never-panicking Verify over a COMPILED-IN verifying-key
// registry: the verifying keys are embedded in the binary (//go:embed), so every
// node verifies against byte-identical keys with no consensus state, no genesis
// param, and no governance round (the 2.1b vk_id-registry decision — consistent
// with x/escrow's compile-time-constant posture, D-073). The matching proving
// keys live only in crypto/zkdelivery/zktest (test-only) and never enter this
// binary.
//
// Determinism (§11.6): groth16.Verify is a fixed pairing computation over a
// fixed verifying key and a public witness reconstructed from on-chain state —
// no fresh randomness. The setup randomness ("toxic waste") is off-chain/build-
// time (crypto/zkdelivery/gen), the analog of the DKG's off-chain crypto/rand
// (D-066). Safety (§11.5): every proof byte and witness path is wrapped
// recover → ErrInvalidProof; the chain never panics on a malformed proof.
package zkdelivery

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"

	"github.com/AryaSingh22/conclave/crypto/zkdelivery/circuit"
)

// VKIDDeliveryV1 is the vk_id a ZkDeliveryCondition names to select the v1
// DeliveryCircuit verifying key. Versioned so a future circuit revision ships as
// a new id (delivery/v2) without invalidating live v1 escrows.
const VKIDDeliveryV1 = "delivery/v1"

//go:embed vk/delivery_v1.vk
var deliveryV1VKBytes []byte

// ErrUnknownVK is returned when a proof names a vk_id with no compiled-in key.
// ErrInvalidProof is returned for a malformed, non-verifying, or instance-
// mismatched proof. The x/escrow keeper maps both into its 15xx codespace.
var (
	ErrUnknownVK    = errors.New("zkdelivery: unknown vk_id")
	ErrInvalidProof = errors.New("zkdelivery: proof does not verify")
)

// registry maps vk_id → verifying key. Populated once at init from the embedded
// bytes; read-only thereafter (no map-iteration into state, §11.6).
var registry = map[string]groth16.VerifyingKey{}

func init() {
	vk := groth16.NewVerifyingKey(circuit.Curve)
	if _, err := vk.ReadFrom(bytes.NewReader(deliveryV1VKBytes)); err != nil {
		// A corrupt embedded key is a build/release defect, not user input —
		// fail fast at boot rather than at first proof.
		panic(fmt.Sprintf("zkdelivery: embedded verifying key %q is corrupt: %v", VKIDDeliveryV1, err))
	}
	registry[VKIDDeliveryV1] = vk
}

// KnownVK reports whether vk_id resolves to a compiled-in verifying key. The
// keeper calls it at ZK-escrow creation so an escrow naming an unknown circuit
// fails closed (→ fallback) rather than becoming unreleasable.
func KnownVK(vkID string) bool {
	_, ok := registry[vkID]
	return ok
}

// Verify checks a ZK_DELIVERY proof. The caller supplies ONLY proofBytes; the
// public witness (escrowID, schemaCommitment) is reconstructed by the keeper
// from the escrow record — never trusted from the prover — which is what makes
// the binding instance-specific: a proof generated for escrow A's id is rejected
// when escrowID is escrow B's (D-072). Returns nil iff the proof verifies under
// vk_id's key for exactly this (escrowID, schemaCommitment).
//
// Deterministic and never-panicking: any malformed proof / witness failure is
// recovered into ErrInvalidProof.
func Verify(vkID string, proofBytes, escrowID, schemaCommitment []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: recovered panic: %v", ErrInvalidProof, r)
		}
	}()

	vk, ok := registry[vkID]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownVK, vkID)
	}

	proof := groth16.NewProof(circuit.Curve)
	if _, err := proof.ReadFrom(bytes.NewReader(proofBytes)); err != nil {
		return fmt.Errorf("%w: malformed proof bytes: %v", ErrInvalidProof, err)
	}

	pub, err := frontend.NewWitness(
		&circuit.DeliveryCircuit{
			EscrowID:         circuit.FieldFromBytes(escrowID),
			SchemaCommitment: circuit.FieldFromBytes(schemaCommitment),
		},
		circuit.Curve.ScalarField(),
		frontend.PublicOnly(),
	)
	if err != nil {
		return fmt.Errorf("%w: building public witness: %v", ErrInvalidProof, err)
	}

	if err := groth16.Verify(proof, vk, pub); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidProof, err)
	}
	return nil
}
