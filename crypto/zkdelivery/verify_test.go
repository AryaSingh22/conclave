package zkdelivery_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AryaSingh22/conclave/crypto/zkdelivery"
	"github.com/AryaSingh22/conclave/crypto/zkdelivery/zktest"
)

// TestVerify_HappyPath: a proof that MiMC(payload)==schema, generated for
// escrow A, verifies under the embedded v1 key for escrow A.
func TestVerify_HappyPath(t *testing.T) {
	escrowA := []byte("escrow-instance-A")
	payload := []byte("delivered-content-digest")
	schema := zktest.SchemaCommitment(payload)

	proof, err := zktest.Prove(escrowA, schema, payload)
	require.NoError(t, err)
	require.NoError(t, zkdelivery.Verify(zkdelivery.VKIDDeliveryV1, proof, escrowA, schema))
}

// TestVerify_InstanceBinding_ReplayRejected is THE D-072 load-bearing test: a
// valid proof for escrow A, with the SAME schema, MUST be rejected against
// escrow B. The proof carries only proof bytes; the verifier reconstructs the
// public input EscrowID from the (caller-supplied, on-chain) escrow id, so the
// proof is bound to the instance. If EscrowID were dropped from the
// reconstructed public witness (schema-only binding), the replay would succeed
// and this test would fail red — that is the watch.
func TestVerify_InstanceBinding_ReplayRejected(t *testing.T) {
	escrowA := []byte("escrow-A")
	escrowB := []byte("escrow-B")
	payload := []byte("identical payload + identical schema across two escrows")
	schema := zktest.SchemaCommitment(payload)

	proof, err := zktest.Prove(escrowA, schema, payload)
	require.NoError(t, err)

	require.NoError(t, zkdelivery.Verify(zkdelivery.VKIDDeliveryV1, proof, escrowA, schema),
		"proof must verify against its own escrow")
	require.ErrorIs(t, zkdelivery.Verify(zkdelivery.VKIDDeliveryV1, proof, escrowB, schema),
		zkdelivery.ErrInvalidProof, "a proof for escrow A must NOT verify against escrow B")
}

// TestVerify_WrongSchemaRejected: a proof bound to one schema does not verify
// against a different schema commitment (the proof attests THIS payload).
func TestVerify_WrongSchemaRejected(t *testing.T) {
	escrowA := []byte("escrow-A")
	payload := []byte("payload")
	schema := zktest.SchemaCommitment(payload)
	proof, err := zktest.Prove(escrowA, schema, payload)
	require.NoError(t, err)

	wrong := zktest.SchemaCommitment([]byte("a different payload"))
	require.ErrorIs(t, zkdelivery.Verify(zkdelivery.VKIDDeliveryV1, proof, escrowA, wrong),
		zkdelivery.ErrInvalidProof)
}

// TestVerify_MalformedProof_NeverPanics: arbitrary/garbage proof bytes are
// rejected with ErrInvalidProof and never panic (§11.5 — a panic here is a
// release blocker).
func TestVerify_MalformedProof_NeverPanics(t *testing.T) {
	for _, p := range [][]byte{nil, {}, []byte("garbage"), make([]byte, 4096)} {
		err := zkdelivery.Verify(zkdelivery.VKIDDeliveryV1, p, []byte("escrow"), []byte("schema"))
		require.ErrorIs(t, err, zkdelivery.ErrInvalidProof)
	}
}

// TestVerify_UnknownVK: a vk_id with no compiled-in key is rejected as unknown
// (the keeper maps this to a fail-closed escrow-creation rejection).
func TestVerify_UnknownVK(t *testing.T) {
	err := zkdelivery.Verify("delivery/v999", []byte("x"), []byte("escrow"), []byte("schema"))
	require.ErrorIs(t, err, zkdelivery.ErrUnknownVK)
}

// TestVerify_Deterministic: verification is a pure function of
// (vk, proof, public inputs) — repeated verification yields the same result
// (§11.6).
func TestVerify_Deterministic(t *testing.T) {
	escrowA := []byte("escrow-A")
	payload := []byte("payload")
	schema := zktest.SchemaCommitment(payload)
	proof, err := zktest.Prove(escrowA, schema, payload)
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		require.NoError(t, zkdelivery.Verify(zkdelivery.VKIDDeliveryV1, proof, escrowA, schema))
	}
}
