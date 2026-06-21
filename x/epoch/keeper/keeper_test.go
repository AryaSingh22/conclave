package keeper

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/drand/kyber/share/dkg"

	"github.com/AryaSingh22/conclave/crypto/threshold"
	"github.com/AryaSingh22/conclave/x/epoch/types"
)

// mockStaking returns a controllable bonded validator set (so a test can change
// committee membership across epoch boundaries, e.g. partial overlap).
type mockStaking struct {
	vals []stakingtypes.Validator
}

func (m *mockStaking) GetBondedValidatorsByPower(_ context.Context) ([]stakingtypes.Validator, error) {
	return m.vals, nil
}

// setValoper sets n validators with distinct operator addresses.
func (m *mockStaking) setValoper(valopers []string) {
	m.vals = make([]stakingtypes.Validator, len(valopers))
	for i, v := range valopers {
		m.vals[i] = stakingtypes.Validator{OperatorAddress: v}
	}
}

// mockVeil is a controllable drain oracle: a test sets which epochs still have
// in-flight (sealed-but-unrevealed) ciphertexts.
type mockVeil struct {
	inflight map[uint64]bool
}

func newMockVeil() *mockVeil { return &mockVeil{inflight: map[uint64]bool{}} }

func (m *mockVeil) HasInFlightForEpoch(_ sdk.Context, epochID uint64) bool {
	return m.inflight[epochID]
}

type epochFixture struct {
	ctx     sdk.Context
	k       *Keeper
	staking *mockStaking
	veil    *mockVeil
}

func setupEpoch(t testing.TB) *epochFixture {
	t.Helper()
	epochKey := storetypes.NewKVStoreKey(types.StoreKey)
	ctx := testutil.DefaultContextWithKeys(
		map[string]*storetypes.KVStoreKey{types.StoreKey: epochKey}, nil, nil,
	)
	encCfg := moduletestutil.MakeTestEncodingConfig()
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	staking := &mockStaking{}
	k := NewKeeper(encCfg.Codec, runtime.NewKVStoreService(epochKey), staking, authority)
	veil := newMockVeil()
	k.SetVeilKeeper(veil)

	ctx = ctx.WithBlockHeight(1)
	// short windows so a full DKG fits in a few blocks; epoch_length comfortably
	// larger than the five DKG windows.
	p := types.Params{
		EpochLengthBlocks:    100,
		RegisterWindowBlocks: 3,
		DealWindowBlocks:     3,
		ResponseWindowBlocks: 3,
		JustifyWindowBlocks:  3,
		ConfirmWindowBlocks:  3,
		MinCommitteeSize:     4,
		MaxBundleBytes:       1 << 20,
	}
	require.NoError(t, k.SetParams(ctx, p))
	k.setNextEpochID(ctx, 1)
	return &epochFixture{ctx: ctx, k: k, staking: staking, veil: veil}
}

func (f *epochFixture) setHeight(h uint64) {
	f.ctx = f.ctx.WithBlockHeight(int64(h))
}

func valopers(n int) []string {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = sdk.ValAddress(secp256k1.GenPrivKey().PubKey().Address()).String()
	}
	return out
}

func decodeDealWire(t testing.TB, wire [][]byte) []*dkg.DealBundle {
	t.Helper()
	out := make([]*dkg.DealBundle, 0, len(wire))
	for _, w := range wire {
		d, err := threshold.UnmarshalDealBundle(w)
		require.NoError(t, err)
		out = append(out, d)
	}
	return out
}

func decodeRespWire(t testing.TB, wire [][]byte) []*dkg.ResponseBundle {
	t.Helper()
	out := make([]*dkg.ResponseBundle, 0, len(wire))
	for _, w := range wire {
		r, err := threshold.UnmarshalResponseBundle(w)
		require.NoError(t, err)
		out = append(out, r)
	}
	return out
}

// dkgOutcome controls how the harness drives a DKG: a full honest run, or a
// degraded one (too few confirmations) to exercise the FAILED path.
type dkgOutcome int

const (
	dkgSucceed dkgOutcome = iota
	dkgConfirmShort
)

// runDKG drives a full five-window on-chain DKG (Task 1.7, D-070), exactly as
// the off-chain daemon will: open FORMING at boundaryHeight, REGISTER each
// candidate's long-term key (register window), FINALIZE the committee from the
// registrants, then deal → response → (justify, empty on the honest path) →
// confirm, all as opaque bytes through the keeper Collect* methods, and finalize
// at the confirm deadline. The DKG runs OFF-CHAIN over the registered roster
// (NewParticipantFromRoster + DeriveNonce — the real daemon construction); only
// public bundles transit chain state. Returns the epoch id and every node's local
// EpochKeyMaterial (honest path).
func (f *epochFixture) runDKG(t testing.TB, boundaryHeight uint64, n int, outcome dkgOutcome) (uint64, []*threshold.EpochKeyMaterial) {
	t.Helper()
	return f.runDKGFaulty(t, boundaryHeight, n, outcome, nil)
}

// runDKGFaulty is runDKG with an optional set of faulty dealer committee indices
// (by their FINALIZED dkg_index) that corrupt their deals and refuse to justify —
// the justification round excludes them. With outcome==dkgConfirmShort only t-1
// nodes confirm (FAILED path).
func (f *epochFixture) runDKGFaulty(t testing.TB, boundaryHeight uint64, n int, outcome dkgOutcome, faulty map[int]bool) (uint64, []*threshold.EpochKeyMaterial) {
	t.Helper()

	// Boundary — open FORMING with the candidate (bonded) set; committee not yet fixed.
	f.setHeight(boundaryHeight)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	epochID := f.k.FormingEpochID(f.ctx)
	require.NotZero(t, epochID, "boundary did not open a FORMING epoch")
	e, ok := f.k.GetEpoch(f.ctx, epochID)
	require.True(t, ok)
	require.Equal(t, types.EPOCH_STATUS_FORMING, e.Status)
	require.Empty(t, e.Committee, "committee must not be fixed before the register window closes")

	params, err := f.k.GetParams(f.ctx)
	require.NoError(t, err)

	// Register window — every candidate registers a long-term DKG key. Keyed by
	// valoper; the committee dkg_index is assigned from the sorted registrants.
	secrets := map[string]([]byte){}
	f.setHeight(boundaryHeight + 1)
	for _, v := range f.staking.vals {
		valoper := v.OperatorAddress
		sec, pub, err := threshold.GenerateLongTermKey()
		require.NoError(t, err)
		secrets[valoper] = sec
		require.NoError(t, f.k.CollectRegistration(f.ctx, epochID, valoper, pub))
	}

	// Register-window close — finalize the committee from the registrants.
	f.setHeight(boundaryHeight + params.RegisterWindowBlocks)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	e, ok = f.k.GetEpoch(f.ctx, epochID)
	require.True(t, ok)
	require.Len(t, e.Committee, n, "committee = registered set")
	thr := int(e.Threshold)

	// Build the off-chain participants from the registered roster (real path).
	regs := f.k.Registrations(f.ctx, epochID)
	roster := make([][]byte, len(regs))
	for i, r := range regs {
		roster[i] = r.DkgPubKey
	}
	parts := make([]*threshold.DKGParticipant, n)
	for i, r := range regs {
		p, err := threshold.NewParticipantFromRoster(epochID, i, secrets[r.Valoper], roster, thr)
		require.NoError(t, err)
		parts[i] = p
	}

	// Deal window.
	f.setHeight(e.DealDeadline - 1)
	for i, p := range parts {
		d, err := p.Round1Deal()
		require.NoError(t, err)
		if faulty[i] {
			d2 := *d
			threshold.CorruptDealForTest(p, &d2)
			d = &d2
		}
		enc, err := threshold.MarshalDealBundle(d)
		require.NoError(t, err)
		require.NoError(t, f.k.CollectDeal(f.ctx, epochID, uint32(i), enc))
	}

	// Response window — each node reads ALL committed deals; honest nodes complain
	// about a faulty dealer.
	f.setHeight(e.DealDeadline)
	deals := decodeDealWire(t, f.k.Deals(f.ctx, epochID))
	for i, p := range parts {
		if faulty[i] {
			_, _ = p.Round2Response(deals)
			continue
		}
		r, err := p.Round2Response(deals)
		require.NoError(t, err)
		if r == nil {
			continue
		}
		enc, err := threshold.MarshalResponseBundle(r)
		require.NoError(t, err)
		require.NoError(t, f.k.CollectResponse(f.ctx, epochID, uint32(i), enc))
	}

	// Justify window — process responses; honest dealers justify (faulty refuse).
	respDeadline := e.DealDeadline + params.ResponseWindowBlocks
	f.setHeight(respDeadline)
	resps := decodeRespWire(t, f.k.Responses(f.ctx, epochID))
	type pend struct {
		p   *threshold.DKGParticipant
		mat *threshold.EpochKeyMaterial
	}
	var pending []pend
	for i, p := range parts {
		if faulty[i] {
			_, _, _ = p.ProcessResponses(resps)
			continue
		}
		mat, jb, err := p.ProcessResponses(resps)
		require.NoError(t, err)
		if jb != nil {
			enc, err := threshold.MarshalJustificationBundle(jb)
			require.NoError(t, err)
			require.NoError(t, f.k.CollectJustification(f.ctx, epochID, uint32(i), enc))
		}
		pending = append(pending, pend{p: p, mat: mat})
	}

	// Confirm window — finalize locally (over justifications if any) and confirm.
	justDeadline := respDeadline + params.JustifyWindowBlocks
	f.setHeight(justDeadline)
	justs := decodeJustWire(t, f.k.Justifications(f.ctx, epochID))
	mats := make([]*threshold.EpochKeyMaterial, 0, n)
	for _, pe := range pending {
		mat := pe.mat
		if mat == nil {
			m, err := pe.p.Finalize(justs)
			require.NoError(t, err)
			mat = m
		}
		mats = append(mats, mat)
	}
	confirmers := len(mats)
	if outcome == dkgConfirmShort {
		confirmers = thr - 1 // below threshold → FAILED
	}
	for i, mat := range mats {
		if i >= confirmers {
			continue
		}
		commits, err := threshold.MarshalCommits(mat.Commits)
		require.NoError(t, err)
		require.NoError(t, f.k.CollectConfirm(f.ctx, epochID, uint32(mat.Index), mat.EpochPub, commits))
	}

	// Finalize at the confirm deadline.
	f.setHeight(e.ConfirmDeadline)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	return epochID, mats
}

func decodeJustWire(t testing.TB, wire [][]byte) []*dkg.JustificationBundle {
	t.Helper()
	out := make([]*dkg.JustificationBundle, 0, len(wire))
	for _, w := range wire {
		j, err := threshold.UnmarshalJustificationBundle(w)
		require.NoError(t, err)
		out = append(out, j)
	}
	return out
}
