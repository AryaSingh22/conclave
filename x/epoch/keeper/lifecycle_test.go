package keeper

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AryaSingh22/conclave/crypto/reveal"
	"github.com/AryaSingh22/conclave/crypto/threshold"
	"github.com/AryaSingh22/conclave/x/epoch/types"
)

// recoverUnder encrypts payload to encPub and recovers it with t shares drawn
// from mats under their scheme — the real Encrypt → PartialReveal → Combine path.
// Used to prove a chain-published epoch key is a usable threshold key and to
// prove cross-epoch isolation (decrypt an epoch-e ct under epoch-(e+1) material).
func recoverUnder(t testing.TB, encPub reveal.EpochPubKey, scheme *threshold.Scheme, mats []*threshold.EpochKeyMaterial, thr int, payload []byte) ([]byte, error) {
	t.Helper()
	client := threshold.NewClientScheme()
	ct, err := client.Encrypt(encPub, payload)
	require.NoError(t, err)
	shares := make([]reveal.RevealShare, 0, thr)
	for i := 0; i < thr; i++ {
		rs, err := scheme.PartialReveal(mats[i].Share, ct)
		require.NoError(t, err)
		shares = append(shares, rs)
	}
	return scheme.Combine(shares, ct)
}

// TestDKGLifecyclePublishesConsistentKey is the core Task-1.5 acceptance: the
// on-chain DKG transport carries a real multi-validator DKG to completion, every
// node finalizes the SAME EpochPubKey, the chain PUBLISHES that key to queryable
// ACTIVE-epoch state, and the published key is a usable threshold key.
func TestDKGLifecyclePublishesConsistentKey(t *testing.T) {
	f := setupEpoch(t)
	const n = 5
	f.staking.setValoper(valopers(n))

	id, mats := f.runDKG(t, 100, n, dkgSucceed)

	e, ok := f.k.GetEpoch(f.ctx, id)
	require.True(t, ok)
	require.Equal(t, types.EPOCH_STATUS_ACTIVE, e.Status, "DKG should publish an ACTIVE epoch")
	require.Equal(t, id, f.k.CurrentEpochID(f.ctx), "ACTIVE epoch is the encryption target")
	require.Len(t, e.Committee, n)
	require.NotEmpty(t, e.EpochPubKey)

	// every node's locally finalized key equals the chain-published key.
	for i, m := range mats {
		require.True(t, bytes.Equal(m.EpochPub, e.EpochPubKey),
			"node %d EpochPub differs from the chain-published key", i)
	}

	// the published key actually decrypts a t-of-n reveal.
	sch, err := threshold.NewScheme(mats[0].Commits, n)
	require.NoError(t, err)
	payload := []byte("intent encrypted to the chain-published epoch key")
	got, err := recoverUnder(t, e.EpochPubKey, sch, mats, int(e.Threshold), payload)
	require.NoError(t, err)
	require.Equal(t, payload, got)
}

// TestFailedDKGDegradesNoRotation proves graceful degradation: a DKG that does
// not reach t confirmations is marked FAILED, the chain never halts, NO rotation
// happens, and the prior ACTIVE epoch's key stays the encryption target — the
// natural form of the §2(b) grace-window fallback (D-068). A single faulty
// dealer / withholding minority can thus DENY rotation (bounded liveness-grief,
// closed by full justification handling in 1.6/1.7).
func TestFailedDKGDegradesNoRotation(t *testing.T) {
	f := setupEpoch(t)
	const n = 5
	f.staking.setValoper(valopers(n))

	id1, _ := f.runDKG(t, 100, n, dkgSucceed)
	require.Equal(t, id1, f.k.CurrentEpochID(f.ctx))

	// next epoch: too few confirmations → FAILED.
	id2, _ := f.runDKG(t, 200, n, dkgConfirmShort)

	e2, ok := f.k.GetEpoch(f.ctx, id2)
	require.True(t, ok)
	require.Equal(t, types.EPOCH_STATUS_FAILED, e2.Status)

	// no rotation: the prior epoch stays ACTIVE and remains the encryption target.
	require.Equal(t, id1, f.k.CurrentEpochID(f.ctx), "failed DKG must not rotate")
	e1, _ := f.k.GetEpoch(f.ctx, id1)
	require.Equal(t, types.EPOCH_STATUS_ACTIVE, e1.Status, "prior key must stay active")
}

// TestDrainBeforeRotateAndCancelReleases proves the chain-state half of seam (a):
// when a successor epoch goes ACTIVE, the prior epoch becomes DRAINING (NOT
// dropped) and is RETIRED only once it has no in-flight ciphertexts; an
// epoch-e ciphertext stays decryptable under epoch-e material across the
// valset-change rotation; and a stuck session reaching a terminal outcome
// (OPEN-cleared or CANCELLED — modeled by the drain oracle going false) releases
// the retirement.
func TestDrainBeforeRotateAndCancelReleases(t *testing.T) {
	f := setupEpoch(t)
	const n = 5
	f.staking.setValoper(valopers(n))

	id1, mats1 := f.runDKG(t, 100, n, dkgSucceed)

	// an epoch-1 ciphertext is in flight (sealed, not yet revealed).
	f.veil.inflight[id1] = true

	// rotate to epoch 2.
	id2, _ := f.runDKG(t, 200, n, dkgSucceed)
	require.Equal(t, id2, f.k.CurrentEpochID(f.ctx), "successor is the new encryption target")

	e1, _ := f.k.GetEpoch(f.ctx, id1)
	require.Equal(t, types.EPOCH_STATUS_DRAINING, e1.Status,
		"prior epoch with in-flight cts must DRAIN, not retire")

	// drain-before-rotate is REAL: the epoch-1 key still decrypts under epoch-1
	// material even though the chain has rotated to epoch 2.
	sch1, err := threshold.NewScheme(mats1[0].Commits, n)
	require.NoError(t, err)
	payload := []byte("in-flight epoch-1 intent must still drain")
	got, err := recoverUnder(t, e1.EpochPubKey, sch1, mats1, int(e1.Threshold), payload)
	require.NoError(t, err)
	require.Equal(t, payload, got)

	// the stuck session reaches a terminal outcome (CANCELLED / OPEN-cleared):
	// the drain oracle goes false → next block retires epoch 1 (rotation released).
	f.veil.inflight[id1] = false
	f.setHeight(260)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	e1, _ = f.k.GetEpoch(f.ctx, id1)
	require.Equal(t, types.EPOCH_STATUS_RETIRED, e1.Status,
		"a drained epoch must retire — CANCEL/OPEN releases the rotation")
}

// TestPartialOverlapResharing proves seam (c): a rotation whose committees
// PARTIALLY OVERLAP (some validators persist across the boundary — 1.4 only
// modeled full independence) still produces independent epoch keys, and the
// retained-share selection keys by EPOCH, not by validator identity: an
// epoch-e ciphertext decrypts under epoch-e material and CANNOT be decrypted
// under epoch-(e+1) material even though the committees share members.
func TestPartialOverlapResharing(t *testing.T) {
	f := setupEpoch(t)
	const n = 4

	base := valopers(n) // A B C D
	f.staking.setValoper(base)
	id1, mats1 := f.runDKG(t, 100, n, dkgSucceed)
	e1, _ := f.k.GetEpoch(f.ctx, id1)

	// epoch 2 committee shares the last two members with epoch 1 (C D E F).
	overlap := []string{base[2], base[3], valopers(2)[0], valopers(2)[1]}
	f.staking.setValoper(overlap)
	f.veil.inflight[id1] = true // keep epoch 1 alive across the rotation
	id2, mats2 := f.runDKG(t, 200, n, dkgSucceed)
	e2, _ := f.k.GetEpoch(f.ctx, id2)

	require.False(t, bytes.Equal(e1.EpochPubKey, e2.EpochPubKey),
		"partially-overlapping committees must still yield independent epoch keys")

	// epoch-1 ct decrypts under epoch-1 material.
	sch1, err := threshold.NewScheme(mats1[0].Commits, n)
	require.NoError(t, err)
	payload := []byte("epoch-1 ct under partial overlap")
	got, err := recoverUnder(t, e1.EpochPubKey, sch1, mats1, int(e1.Threshold), payload)
	require.NoError(t, err)
	require.Equal(t, payload, got)

	// cross-epoch isolation: epoch-2 material cannot recover the epoch-1 ct.
	sch2, err := threshold.NewScheme(mats2[0].Commits, n)
	require.NoError(t, err)
	bad, err := recoverUnder(t, e1.EpochPubKey, sch2, mats2, int(e2.Threshold), payload)
	if err == nil && bytes.Equal(bad, payload) {
		t.Fatal("epoch-2 material decrypted an epoch-1 ciphertext — epochs not isolated")
	}
}

// TestBoundaryDetectionDeterministic pins the height-based boundary and the
// degraded paths that keep the chain live: no epoch opens off-boundary, and a
// bonded set below min_committee_size opens nothing (prior key stays).
func TestBoundaryDetectionDeterministic(t *testing.T) {
	f := setupEpoch(t)
	f.staking.setValoper(valopers(5))

	// off-boundary heights open nothing.
	for _, h := range []uint64{1, 50, 99, 101, 150, 199} {
		f.setHeight(h)
		require.NoError(t, f.k.EndBlocker(f.ctx))
		require.Zero(t, f.k.FormingEpochID(f.ctx), "height %d is not a boundary", h)
	}

	// a boundary with enough validators opens a FORMING epoch.
	f.setHeight(100)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	require.NotZero(t, f.k.FormingEpochID(f.ctx))

	// too few validators: no epoch opens (degraded, prior key stays).
	f2 := setupEpoch(t)
	f2.staking.setValoper(valopers(3)) // < min_committee_size (4)
	f2.setHeight(100)
	require.NoError(t, f2.k.EndBlocker(f2.ctx))
	require.Zero(t, f2.k.FormingEpochID(f2.ctx), "below min_committee_size must not open an epoch")
}

// TestIsDecryptableEpochBothEdges pins the both-edge gate veil's submit
// validation rests on: only ACTIVE and DRAINING epochs are decryptable; a
// FORMING/FAILED epoch (no published key) and a RETIRED epoch (past) and an
// unknown id are all rejected.
func TestIsDecryptableEpochBothEdges(t *testing.T) {
	f := setupEpoch(t)
	cases := []struct {
		status types.EpochStatus
		want   bool
	}{
		{types.EPOCH_STATUS_FORMING, false},
		{types.EPOCH_STATUS_ACTIVE, true},
		{types.EPOCH_STATUS_DRAINING, true},
		{types.EPOCH_STATUS_RETIRED, false},
		{types.EPOCH_STATUS_FAILED, false},
	}
	for i, c := range cases {
		id := uint64(i + 1)
		f.k.SetEpoch(f.ctx, types.Epoch{Id: id, Status: c.status})
		require.Equal(t, c.want, f.k.IsDecryptableEpoch(f.ctx, id), "status %v", c.status)
	}
	require.False(t, f.k.IsDecryptableEpoch(f.ctx, 9999), "unknown epoch id is not decryptable")
}
