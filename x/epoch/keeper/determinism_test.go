package keeper

import (
	"crypto/sha256"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AryaSingh22/conclave/crypto/threshold"
	"github.com/AryaSingh22/conclave/x/epoch/types"
)

// Determinism coverage for the x/epoch chain-state surface (D-068, §11.6).
//
// The 500-block app determinism vector (D-063) does NOT reach an epoch boundary
// (epoch_length_blocks defaults to ~1 day = 17280, far beyond 500), so x/epoch is
// INERT there. This test closes that gap directly: it REPLAYS one fixed DKG
// transcript (identical committee + identical opaque deal/response/confirmation
// bytes) into two INDEPENDENT keeper instances through the identical block
// sequence — boundary detection, committee snapshot, transport collection,
// confirmation tally, publication, rotation, drain — and asserts the resulting
// chain state is BYTE-IDENTICAL. That isolates the deterministic chain-state
// transitions from the off-chain DKG randomness (which legitimately differs run
// to run; only the agreed, committed bytes drive consensus state).
//
// Cross-NODE agreement (every validator publishes the same key) is covered
// separately by TestDKGLifecyclePublishesConsistentKey. NAMED FORWARD GAP: no
// test exercises a real epoch boundary inside the full multi-validator app sim;
// Task 1.7's multi-node adversarial localnet must drive a real boundary +
// rotation end-to-end (recorded in D-068).

// bundleMsg is one index-tagged opaque transport bundle (deal or response).
type bundleMsg struct {
	idx   uint32
	bytes []byte
}

// confirmMsg is one index-tagged key confirmation.
type confirmMsg struct {
	idx uint32
	pub []byte
	com [][]byte
}

// dkgTranscript is a fixed, replayable DKG transport transcript.
type dkgTranscript struct {
	vals  []string
	deals []bundleMsg
	resps []bundleMsg
	confs []confirmMsg
	n     int
	thr   int
}

// buildTranscript runs ONE off-chain DKG and captures its committee + every
// round's wire bytes, so two independent chain replays can be fed byte-identical
// transport inputs.
func buildTranscript(t testing.TB, n int) dkgTranscript {
	t.Helper()
	thr := types.Threshold(n)
	parts, err := threshold.NewInProcessCommittee(n, thr)
	require.NoError(t, err)

	tr := dkgTranscript{vals: valopers(n), n: n, thr: thr}

	dealWire := make([][]byte, 0, n)
	for i, p := range parts {
		d, err := p.Round1Deal()
		require.NoError(t, err)
		enc, err := threshold.MarshalDealBundle(d)
		require.NoError(t, err)
		dealWire = append(dealWire, enc)
		tr.deals = append(tr.deals, bundleMsg{uint32(i), enc})
	}

	deals := decodeDealWire(t, dealWire)
	respWire := make([][]byte, 0, n)
	for i, p := range parts {
		r, err := p.Round2Response(deals)
		require.NoError(t, err)
		if r == nil {
			continue
		}
		enc, err := threshold.MarshalResponseBundle(r)
		require.NoError(t, err)
		respWire = append(respWire, enc)
		tr.resps = append(tr.resps, bundleMsg{uint32(i), enc})
	}

	resps := decodeRespWire(t, respWire)
	for i, p := range parts {
		mat, err := p.Finalize(resps)
		require.NoError(t, err)
		com, err := threshold.MarshalCommits(mat.Commits)
		require.NoError(t, err)
		tr.confs = append(tr.confs, confirmMsg{uint32(i), mat.EpochPub, com})
	}
	return tr
}

// replayLifecycle drives a fresh keeper through two epoch boundaries with the
// fixed transcript (epoch 1 forms+activates, then epoch 2 forms+activates and
// rotates epoch 1 to DRAINING while it holds an in-flight ct), and returns a
// digest of the full resulting epoch chain state.
func replayLifecycle(t testing.TB, tr dkgTranscript) []byte {
	t.Helper()
	f := setupEpoch(t)
	f.staking.setValoper(tr.vals)

	replayEpoch := func(boundary uint64) uint64 {
		f.setHeight(boundary)
		require.NoError(t, f.k.EndBlocker(f.ctx))
		id := f.k.FormingEpochID(f.ctx)
		require.NotZero(t, id)
		e, _ := f.k.GetEpoch(f.ctx, id)

		f.setHeight(boundary + 1)
		for _, d := range tr.deals {
			require.NoError(t, f.k.CollectDeal(f.ctx, id, d.idx, d.bytes))
		}
		f.setHeight(e.DealDeadline)
		for _, r := range tr.resps {
			require.NoError(t, f.k.CollectResponse(f.ctx, id, r.idx, r.bytes))
		}
		f.setHeight(e.DealDeadline + 3)
		for _, c := range tr.confs {
			require.NoError(t, f.k.CollectConfirm(f.ctx, id, c.idx, c.pub, c.com))
		}
		f.setHeight(e.ConfirmDeadline)
		require.NoError(t, f.k.EndBlocker(f.ctx))
		return id
	}

	id1 := replayEpoch(100)
	f.veil.inflight[id1] = true // keep epoch 1 DRAINING across the rotation
	replayEpoch(200)

	return f.stateDigest(t)
}

// stateDigest hashes the full x/epoch chain state in a deterministic order
// (scalar ids, then every epoch record in ascending id order — IterateEpochs is
// key-ordered).
func (f *epochFixture) stateDigest(t testing.TB) []byte {
	t.Helper()
	h := sha256.New()
	p, err := f.k.GetParams(f.ctx)
	require.NoError(t, err)
	pb, err := p.Marshal()
	require.NoError(t, err)
	h.Write(pb)
	for _, v := range []uint64{f.k.CurrentEpochID(f.ctx), f.k.NextEpochID(f.ctx), f.k.FormingEpochID(f.ctx)} {
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], v)
		h.Write(b[:])
	}
	f.k.IterateEpochs(f.ctx, func(e types.Epoch) bool {
		eb, err := e.Marshal()
		require.NoError(t, err)
		h.Write(eb)
		return false
	})
	return h.Sum(nil)
}

// TestEpochChainStateDeterministic proves the new x/epoch chain-state transitions
// are byte-identical across two independent runs on identical committed inputs —
// the §11.6 guarantee for the surface the 500-block app sim does not reach.
func TestEpochChainStateDeterministic(t *testing.T) {
	const n = 5
	tr := buildTranscript(t, n)
	digestA := replayLifecycle(t, tr)
	digestB := replayLifecycle(t, tr)
	require.Equal(t, digestA, digestB,
		"x/epoch chain-state transitions must be byte-identical across independent runs on identical inputs")
}
