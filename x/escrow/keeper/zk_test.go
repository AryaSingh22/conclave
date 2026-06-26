package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/crypto/zkdelivery"
	"github.com/AryaSingh22/conclave/crypto/zkdelivery/zktest"
	"github.com/AryaSingh22/conclave/x/escrow/types"
)

// ZK_DELIVERY (task 2.1b, D-076): release is gated on a groth16 proof that the
// delivered payload matches the seller-signed schema_commitment, bound to the
// escrow instance. These tests drive the keeper paths (CreateEscrow, ReleaseZk
// via MsgSubmitDeliveryProof, the timeout sweep) and prove every illegal release
// is a load-bearing REJECT — including the D-072 cross-instance replay.

func newZkEscrow(id string, schema []byte, vkID string, amount sdk.Coins) types.Escrow {
	return types.Escrow{
		Id: id, PayerSigil: buyerSigil, PayeeSigil: sellerSigil, Amount: amount,
		Condition: types.DeliveryCondition{Condition: &types.DeliveryCondition_ZkDelivery{
			ZkDelivery: &types.ZkDeliveryCondition{SchemaCommitment: schema, VkId: vkID},
		}},
		ChamberId: 0,
	}
}

func (f *escrowFixture) createZk(t *testing.T, id string, schema []byte, amount sdk.Coins) {
	t.Helper()
	f.bank.fund(f.buyer.String(), amount)
	esc := newZkEscrow(id, schema, zkdelivery.VKIDDeliveryV1, amount)
	require.NoError(t, f.k.CreateEscrow(f.ctx, esc, f.buyer, f.selr, nil))
	require.Equal(t, types.ESCROW_STATE_ACTIVE, f.get(t, id).State)
}

// idBytes is the raw 32-byte form of an escrow id — the instance binding the
// prover must target (the verifier reconstructs the same from the record).
func idBytes(t *testing.T, id string) []byte {
	t.Helper()
	b, err := types.IDBytes(id)
	require.NoError(t, err)
	return b
}

// proveFor builds a valid proof for a given escrow id + payload.
func proveFor(t *testing.T, id string, payload []byte) (proof, schema []byte) {
	t.Helper()
	schema = zktest.SchemaCommitment(payload)
	p, err := zktest.Prove(idBytes(t, id), schema, payload)
	require.NoError(t, err)
	return p, schema
}

// TestZk_ReleaseHappy: a valid proof releases the escrow and pays the seller.
func TestZk_ReleaseHappy(t *testing.T) {
	f := setupEscrow(t)
	f.atHeight(100)
	payload := []byte("delivered ciphertext digest")
	proof, schema := proveFor(t, testID(60), payload)
	amt := coins(denom, 1000)
	f.createZk(t, testID(60), schema, amt)

	_, err := f.ms().SubmitDeliveryProof(f.ctx, &types.MsgSubmitDeliveryProof{
		Signer: f.buyer.String(), EscrowId: testID(60), Proof: proof,
	})
	require.NoError(t, err)
	require.Equal(t, types.ESCROW_STATE_RELEASED, f.get(t, testID(60)).State)
	require.Equal(t, amt, f.bank.get(f.selr.String()), "payment paid to the seller on a valid proof")
	require.True(t, f.bank.get(types.ModuleName).IsZero(), "escrow account drained")
}

// TestZk_ReleaseWithoutProof: an empty proof must REJECT and leave funds
// escrowed (load-bearing — release is impossible without a verifying proof).
func TestZk_ReleaseWithoutProof(t *testing.T) {
	f := setupEscrow(t)
	f.atHeight(100)
	_, schema := proveFor(t, testID(61), []byte("payload"))
	amt := coins(denom, 1000)
	f.createZk(t, testID(61), schema, amt)

	_, err := f.ms().SubmitDeliveryProof(f.ctx, &types.MsgSubmitDeliveryProof{
		Signer: f.buyer.String(), EscrowId: testID(61), Proof: nil,
	})
	require.ErrorIs(t, err, types.ErrInvalidProof)
	require.Equal(t, types.ESCROW_STATE_ACTIVE, f.get(t, testID(61)).State)
	require.Equal(t, amt, f.bank.get(types.ModuleName), "funds stay escrowed without a proof")
}

// TestZk_InvalidProof: garbage proof bytes REJECT (ErrInvalidProof) and never
// panic; funds stay escrowed.
func TestZk_InvalidProof(t *testing.T) {
	f := setupEscrow(t)
	f.atHeight(100)
	_, schema := proveFor(t, testID(62), []byte("payload"))
	f.createZk(t, testID(62), schema, coins(denom, 1000))

	for _, bad := range [][]byte{[]byte("garbage"), make([]byte, 200)} {
		_, err := f.ms().SubmitDeliveryProof(f.ctx, &types.MsgSubmitDeliveryProof{
			Signer: f.buyer.String(), EscrowId: testID(62), Proof: bad,
		})
		require.ErrorIs(t, err, types.ErrInvalidProof)
	}
	require.Equal(t, types.ESCROW_STATE_ACTIVE, f.get(t, testID(62)).State)
}

// TestZk_ReplayCrossInstance is THE D-072 load-bearing keeper test: a valid
// proof for escrow A is REJECTED when submitted against escrow B (same schema,
// same vk) — the verifier reconstructs the public input from B's id, so the
// proof bound to A fails. If ReleaseZk dropped the escrow id from the
// reconstructed witness (schema-only binding) this would release B, so this
// test fails red if the instance binding is removed. The proof still releases
// its OWN escrow A.
func TestZk_ReplayCrossInstance(t *testing.T) {
	f := setupEscrow(t)
	f.atHeight(100)
	payload := []byte("identical payload across two escrows")
	proofA, schema := proveFor(t, testID(63), payload)

	// B shares the SAME schema as A (the replay target).
	f.createZk(t, testID(64), schema, coins(denom, 500))
	f.createZk(t, testID(63), schema, coins(denom, 700))

	// proof for A submitted to B → REJECT (instance-bound).
	_, err := f.ms().SubmitDeliveryProof(f.ctx, &types.MsgSubmitDeliveryProof{
		Signer: f.buyer.String(), EscrowId: testID(64), Proof: proofA,
	})
	require.ErrorIs(t, err, types.ErrInvalidProof, "a proof for escrow A must not release escrow B")
	require.Equal(t, types.ESCROW_STATE_ACTIVE, f.get(t, testID(64)).State)

	// the same proof DOES release its own escrow A.
	_, err = f.ms().SubmitDeliveryProof(f.ctx, &types.MsgSubmitDeliveryProof{
		Signer: f.buyer.String(), EscrowId: testID(63), Proof: proofA,
	})
	require.NoError(t, err)
	require.Equal(t, types.ESCROW_STATE_RELEASED, f.get(t, testID(63)).State)
}

// TestZk_ReplaySameInstance: re-submitting a spent proof against the same escrow
// after release REJECTS (terminal state) — no double-release.
func TestZk_ReplaySameInstance(t *testing.T) {
	f := setupEscrow(t)
	f.atHeight(100)
	proof, schema := proveFor(t, testID(65), []byte("payload"))
	f.createZk(t, testID(65), schema, coins(denom, 1000))

	_, err := f.ms().SubmitDeliveryProof(f.ctx, &types.MsgSubmitDeliveryProof{
		Signer: f.buyer.String(), EscrowId: testID(65), Proof: proof,
	})
	require.NoError(t, err)
	require.Equal(t, types.ESCROW_STATE_RELEASED, f.get(t, testID(65)).State)

	// replay the same valid proof → terminal state rejects.
	_, err = f.ms().SubmitDeliveryProof(f.ctx, &types.MsgSubmitDeliveryProof{
		Signer: f.buyer.String(), EscrowId: testID(65), Proof: proof,
	})
	require.ErrorIs(t, err, types.ErrTerminalState)
}

// TestZk_TimeoutRefund: with no proof, the EndBlock sweep refunds the buyer at
// the ZkDeliveryTimeoutBlocks deadline (ACTIVE→REFUNDED, no bond).
func TestZk_TimeoutRefund(t *testing.T) {
	f := setupEscrow(t)
	f.atHeight(100)
	_, schema := proveFor(t, testID(66), []byte("payload"))
	amt := coins(denom, 1000)
	f.createZk(t, testID(66), schema, amt)

	f.atHeight(int64(100 + types.ZkDeliveryTimeoutBlocks))
	require.NoError(t, f.k.EndBlocker(f.ctx))
	require.Equal(t, types.ESCROW_STATE_REFUNDED, f.get(t, testID(66)).State)
	require.Equal(t, amt, f.bank.get(f.buyer.String()), "buyer refunded on ZK timeout")
	require.True(t, f.bank.get(types.ModuleName).IsZero(), "escrow account drained")
}

// TestZk_UnknownVkAtCreate: an escrow naming an unknown circuit fails closed at
// creation (→ the conditioned CONCLUDE leg falls back; no funded-but-stuck
// escrow).
func TestZk_UnknownVkAtCreate(t *testing.T) {
	f := setupEscrow(t)
	f.atHeight(100)
	f.bank.fund(f.buyer.String(), coins(denom, 1000))
	esc := newZkEscrow(testID(67), zktest.SchemaCommitment([]byte("p")), "delivery/bogus", coins(denom, 1000))
	err := f.k.CreateEscrow(f.ctx, esc, f.buyer, f.selr, nil)
	require.ErrorIs(t, err, types.ErrUnknownVk)
}

// TestZk_BadSchemaAtCreate: an empty / oversized schema commitment is rejected
// at creation.
func TestZk_BadSchemaAtCreate(t *testing.T) {
	f := setupEscrow(t)
	f.atHeight(100)
	f.bank.fund(f.buyer.String(), coins(denom, 1000))
	for _, schema := range [][]byte{nil, make([]byte, 33)} {
		esc := newZkEscrow(testID(68), schema, zkdelivery.VKIDDeliveryV1, coins(denom, 1000))
		err := f.k.CreateEscrow(f.ctx, esc, f.buyer, f.selr, nil)
		require.ErrorIs(t, err, types.ErrInvalidEscrow)
	}
}

// TestZk_ReleaseEscrowWrongClass: MsgReleaseEscrow (the preimage path) does NOT
// release a ZK escrow — it must go through MsgSubmitDeliveryProof.
func TestZk_ReleaseEscrowWrongClass(t *testing.T) {
	f := setupEscrow(t)
	f.atHeight(100)
	_, schema := proveFor(t, testID(69), []byte("payload"))
	f.createZk(t, testID(69), schema, coins(denom, 1000))

	_, err := f.ms().ReleaseEscrow(f.ctx, &types.MsgReleaseEscrow{
		Signer: f.buyer.String(), EscrowId: testID(69), Preimage: []byte("anything"),
	})
	require.ErrorIs(t, err, types.ErrWrongClass)
	require.Equal(t, types.ESCROW_STATE_ACTIVE, f.get(t, testID(69)).State)
}

// TestZk_RaiseDisputeNoPath: a ZK escrow has no dispute path (like HASHLOCK).
func TestZk_RaiseDisputeNoPath(t *testing.T) {
	f := setupEscrow(t)
	f.atHeight(100)
	_, schema := proveFor(t, testID(70), []byte("payload"))
	f.createZk(t, testID(70), schema, coins(denom, 1000))

	_, err := f.ms().RaiseDispute(f.ctx, &types.MsgRaiseDispute{
		Signer: f.buyer.String(), EscrowId: testID(70),
	})
	require.ErrorIs(t, err, types.ErrNoDisputePath)
}
