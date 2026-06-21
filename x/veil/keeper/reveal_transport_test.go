package keeper

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/crypto/reveal"
	"github.com/AryaSingh22/conclave/crypto/threshold"
	"github.com/AryaSingh22/conclave/x/veil/types"
)

// submitTagged submits an intent with a chosen epoch_id and ciphertext, an empty
// spend_commitment (=zero, min bond), and returns its id. The keeper fixture
// wires no epochKeeper, so the submit-time epoch-tag gate is skipped — letting a
// test seed intents tagged to arbitrary epoch ids.
func (f *fixture) submitTagged(t testingT, owner sdk.AccAddress, sigilID string, ttl, epochID uint64, ct []byte) string {
	t.Helper()
	env := types.IntentEnvelope{TtlHeight: ttl, EpochId: epochID}
	bz, err := env.Marshal()
	require.NoError(t, err)
	id, err := f.k.SubmitIntent(f.ctx, submitMsg(owner, sigilID, ct, bz, minBond()))
	require.NoError(t, err)
	return id
}

// TestEnumerateInFlightForReveal is the ordering↔share contract (PHASE1_DESIGN
// §2) and a load-bearing piece of the F-4 guarantee: a share can only be
// produced for a ciphertext that is in COMMITTED SEALED_IN_SESSION state and
// tagged to a DKG epoch. The enumeration therefore EXCLUDES a still-SUBMITTED
// intent (ordering not yet fixed → not decryptable) and an epoch_id==0 stub
// intent (StubScheme path → no shares), and is deterministic across calls.
func TestEnumerateInFlightForReveal(t *testing.T) {
	f := setupKeeper(t)
	owner := randAddr()
	sigilID := f.registerSigil(t, owner)
	f.bank.fund(owner.String(), sdk.NewCoins(sdk.NewInt64Coin(bondDenom, 1_000_000_000)))

	f.beginBlock(t, 3)
	// three intents that WILL be sealed: epoch 0 (stub), 7, 5.
	id0 := f.submitTagged(t, owner, sigilID, 100, 0, []byte("ct-stub"))
	id7 := f.submitTagged(t, owner, sigilID, 100, 7, []byte("ct-seven"))
	id5 := f.submitTagged(t, owner, sigilID, 100, 5, []byte("ct-five"))
	require.NoError(t, f.k.SealSubmitted(f.ctx, 3))
	// a fourth submitted AFTER the seal stays SUBMITTED (ordering not yet fixed).
	idSub := f.submitTagged(t, owner, sigilID, 100, 9, []byte("ct-submitted"))

	targets, err := f.k.EnumerateInFlightForReveal(f.ctx)
	require.NoError(t, err)

	got := make(map[string]uint64, len(targets))
	for _, tg := range targets {
		got[tg.IntentID] = tg.EpochID
	}
	require.Len(t, targets, 2, "only epoch_id!=0 SEALED intents are in-flight reveal targets")
	require.Equal(t, uint64(7), got[id7])
	require.Equal(t, uint64(5), got[id5])
	require.NotContains(t, got, id0, "epoch_id==0 (stub) intent is never a reveal-share target")
	require.NotContains(t, got, idSub, "a SUBMITTED intent (ordering not fixed) is not decryptable — F-4")

	// the ciphertext and session id travel with each target (what ExtendVote needs).
	for _, tg := range targets {
		require.NotEmpty(t, tg.Ciphertext)
		require.Equal(t, uint64(3), tg.SessionID)
	}

	// deterministic enumeration: identical order on a repeat call (§11.6 — the
	// contract that every validator's ExtendVote covers the same ordered set).
	again, err := f.k.EnumerateInFlightForReveal(f.ctx)
	require.NoError(t, err)
	require.Equal(t, targets, again)
}

// TestRevealShareSourceDefaultNil confirms the keeper tolerates no wired share
// source (stub/unit-test mode): SharesFor is simply never consulted because the
// live Reveal stays on StubScheme until the Phase-B cutover. The setter wires it.
func TestRevealShareSourceDefaultNil(t *testing.T) {
	f := setupKeeper(t)
	require.Nil(t, f.k.revealShares)
	f.k.SetRevealShareSource(stubShareSource{})
	require.NotNil(t, f.k.revealShares)
}

type stubShareSource struct{}

func (stubShareSource) SharesFor(int64, string) []reveal.RevealShare { return nil }

// --- B5 acceptance: the real threshold scheme over the live Reveal ---

// realEpochMock is a test EpochKeeper that publishes real DKG verification
// material for the Reveal cutover (Task 1.6).
type realEpochMock struct {
	decryptable map[uint64]bool
	commits     map[uint64][][]byte
	n           map[uint64]int
}

func (m *realEpochMock) IsDecryptableEpoch(_ sdk.Context, id uint64) bool { return m.decryptable[id] }
func (m *realEpochMock) GetEpochVerification(_ sdk.Context, id uint64) ([][]byte, int, bool) {
	c, ok := m.commits[id]
	if !ok {
		return nil, 0, false
	}
	return c, m.n[id], true
}

// mapShareSource is a test RevealShareSource keyed by (height, intent id).
type mapShareSource struct {
	byHeight map[int64]map[string][]reveal.RevealShare
}

func newMapShareSource() *mapShareSource {
	return &mapShareSource{byHeight: map[int64]map[string][]reveal.RevealShare{}}
}
func (s *mapShareSource) set(h int64, id string, shares []reveal.RevealShare) {
	if s.byHeight[h] == nil {
		s.byHeight[h] = map[string][]reveal.RevealShare{}
	}
	s.byHeight[h][id] = shares
}
func (s *mapShareSource) SharesFor(h int64, id string) []reveal.RevealShare {
	if s.byHeight[h] == nil {
		return nil
	}
	return s.byHeight[h][id]
}

// realEpochScenario wires a fixture with a real DKG epoch (n=4,t=3), an
// encrypted intent (give 50 usdc under a 100-usdc commitment, so no slash), and
// returns the intent id, the per-node reveal shares, and the share source.
func realEpochScenario(t *testing.T, f *fixture, owner sdk.AccAddress) (id string, shares []reveal.RevealShare, src *mapShareSource, plaintext []byte) {
	t.Helper()
	const n, thr = 4, 3
	sigilID := f.registerSigil(t, owner)
	f.bank.fund(owner.String(), sdk.NewCoins(sdk.NewInt64Coin(bondDenom, 1_000_000_000)))

	mats, err := threshold.RunDKGInProcess(n, thr)
	require.NoError(t, err)
	commitsBz, err := threshold.MarshalCommits(mats[0].Commits)
	require.NoError(t, err)
	const epochID = uint64(1)
	f.k.SetEpochKeeper(&realEpochMock{
		decryptable: map[uint64]bool{epochID: true},
		commits:     map[uint64][][]byte{epochID: commitsBz},
		n:           map[uint64]int{epochID: n},
	})
	src = newMapShareSource()
	f.k.SetRevealShareSource(src)

	client := threshold.NewClientScheme()
	give := sdk.NewInt64Coin(usdcDenom, 50_000_000)
	payload := types.IntentPayload{Give: []types.IntentLeg{{Asset: &types.IntentLeg_Coin{Coin: &give}}}}
	plaintext, err = payload.Marshal()
	require.NoError(t, err)
	ct, err := client.Encrypt(mats[0].EpochPub, plaintext)
	require.NoError(t, err)

	// submit at boundary height 3, commitment 100 usdc (>= give → no slash).
	f.beginBlock(t, 3)
	env := types.IntentEnvelope{
		TtlHeight:       100,
		EpochId:         epochID,
		SpendCommitment: sdk.NewCoins(sdk.NewInt64Coin(usdcDenom, 100_000_000)),
	}
	envBz, err := env.Marshal()
	require.NoError(t, err)
	id, err = f.k.SubmitIntent(f.ctx, submitMsg(owner, sigilID, ct, envBz, minBond()))
	require.NoError(t, err)
	f.endBlock(t) // boundary 3 → seal (session id 3)
	require.Equal(t, types.INTENT_STATE_SEALED_IN_SESSION, f.mustRecord(t, id).State)

	shares = make([]reveal.RevealShare, 0, thr)
	for i := 0; i < thr; i++ {
		sh, err := client.PartialReveal(mats[i].Share, ct)
		require.NoError(t, err)
		shares = append(shares, sh)
	}
	return id, shares, src, plaintext
}

// TestRevealRealSchemeEndToEnd is the 1.6 acceptance centerpiece: an encrypted
// intent submitted to a live DKG epoch reveals via the REAL threshold scheme
// from the vote-extension shares and clears end-to-end — and CANNOT reveal before
// the shares exist, even though its ordering is already fixed (F-4, live).
func TestRevealRealSchemeEndToEnd(t *testing.T) {
	f := setupKeeper(t)
	owner := randAddr()
	id, shares, src, _ := realEpochScenario(t, f, owner)

	// F-4 live: at B+1 (height 4) the shares do not exist yet → stays SEALED,
	// even though ordering was fixed at the boundary (session id 3 < height 4).
	f.beginBlock(t, 4)
	require.Equal(t, types.INTENT_STATE_SEALED_IN_SESSION, f.mustRecord(t, id).State,
		"no shares ⇒ no reveal even with ordering fixed (order-then-reveal, F-4)")

	// at B+2 (height 5) t valid shares are present → the real Reveal opens it.
	src.set(5, id, shares)
	f.beginBlock(t, 5)
	require.Equal(t, types.INTENT_STATE_OPEN, f.mustRecord(t, id).State, "t real shares ⇒ reveal opens")

	// the revealed plaintext is readable by x/session (settlement hand-off).
	_, payload, err := f.k.GetOpenIntent(f.ctx, 0, id)
	require.NoError(t, err)
	require.Len(t, payload.Give, 1)
	require.Equal(t, sdk.NewInt64Coin(usdcDenom, 50_000_000), *payload.Give[0].GetCoin())
}

// TestRevealRetryExhaustionCancels proves the §2(c) liveness path: a permanently
// share-starved session retries (from the B+2 floor) up to max_reveal_retries,
// then CANCELs — bonds refunded, the chain never halts.
func TestRevealRetryExhaustionCancels(t *testing.T) {
	f := setupKeeper(t)
	owner := randAddr()
	id, _, _, _ := realEpochScenario(t, f, owner) // shares NEVER provided
	// balance after the scenario (owner funded, bond escrowed at submit).
	balAfterSubmit := f.bank.balanceOf(owner.String())

	// default max_reveal_retries = 3. B+2 floor = session 3 + 2 = height 5:
	// height 4 (skip, < floor), 5 (attempt 1), 6 (attempt 2), 7 (attempt 3 → CANCEL).
	for h := int64(4); h <= 6; h++ {
		f.beginBlock(t, h)
		require.Equal(t, types.INTENT_STATE_SEALED_IN_SESSION, f.mustRecord(t, id).State,
			"still retrying at height %d", h)
	}
	f.beginBlock(t, 7) // BeginBlocker must NOT error — the chain never halts
	require.Equal(t, types.INTENT_STATE_CANCELLED, f.mustRecord(t, id).State,
		"retry exhaustion ⇒ session CANCEL")

	// the bond is refunded on CANCEL: the owner is whole again (escrow returned).
	require.True(t, f.bank.balanceOf(owner.String()).Equal(balAfterSubmit.Add(minBond())),
		"CANCEL refunds the escrowed bond")
}

// TestRevealRealSchemeDeterministic feeds IDENTICAL committed inputs (same epoch
// commits, same ciphertext, same shares, same owner) into two INDEPENDENT keepers
// and asserts the resulting reveal state is byte-identical — the new consensus
// Reveal path is a deterministic function of committed inputs (SPEC §11.6; any
// non-determinism here is a release blocker). The DKG/ciphertext/shares are
// generated ONCE and replayed into both keepers (the off-chain DKG randomness is
// isolated from the on-chain reveal, exactly as the determinism vector requires).
func TestRevealRealSchemeDeterministic(t *testing.T) {
	const n, thr = 4, 3
	mats, err := threshold.RunDKGInProcess(n, thr)
	require.NoError(t, err)
	commitsBz, err := threshold.MarshalCommits(mats[0].Commits)
	require.NoError(t, err)
	client := threshold.NewClientScheme()
	give := sdk.NewInt64Coin(usdcDenom, 50_000_000)
	payload := types.IntentPayload{Give: []types.IntentLeg{{Asset: &types.IntentLeg_Coin{Coin: &give}}}}
	pt, err := payload.Marshal()
	require.NoError(t, err)
	ct, err := client.Encrypt(mats[0].EpochPub, pt)
	require.NoError(t, err)
	shares := make([]reveal.RevealShare, 0, thr)
	for i := 0; i < thr; i++ {
		sh, err := client.PartialReveal(mats[i].Share, ct)
		require.NoError(t, err)
		shares = append(shares, sh)
	}
	// a fixed owner so the intent id/owner match across the two independent runs.
	owner := sdk.AccAddress(bytes.Repeat([]byte{7}, 20))

	run := func() []byte {
		f := setupKeeper(t)
		sigilID := f.registerSigil(t, owner)
		f.bank.fund(owner.String(), sdk.NewCoins(sdk.NewInt64Coin(bondDenom, 1_000_000_000)))
		const epochID = uint64(1)
		f.k.SetEpochKeeper(&realEpochMock{
			decryptable: map[uint64]bool{epochID: true},
			commits:     map[uint64][][]byte{epochID: commitsBz},
			n:           map[uint64]int{epochID: n},
		})
		src := newMapShareSource()
		f.k.SetRevealShareSource(src)

		f.beginBlock(t, 3)
		env := types.IntentEnvelope{TtlHeight: 100, EpochId: epochID, SpendCommitment: sdk.NewCoins(sdk.NewInt64Coin(usdcDenom, 100_000_000))}
		envBz, err := env.Marshal()
		require.NoError(t, err)
		id, err := f.k.SubmitIntent(f.ctx, submitMsg(owner, sigilID, ct, envBz, minBond()))
		require.NoError(t, err)
		f.endBlock(t)
		src.set(5, id, shares)
		f.beginBlock(t, 5)
		rec := f.mustRecord(t, id)
		require.Equal(t, types.INTENT_STATE_OPEN, rec.State)
		bz, err := rec.Marshal()
		require.NoError(t, err)
		return bz
	}

	require.Equal(t, run(), run(),
		"identical committed inputs must yield a byte-identical reveal across independent keepers")
}
