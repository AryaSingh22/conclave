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
	// larger than deal+response+confirm.
	p := types.Params{
		EpochLengthBlocks:    100,
		DealWindowBlocks:     3,
		ResponseWindowBlocks: 3,
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

// runDKG opens a FORMING epoch at boundaryHeight with the current validator set,
// drives the on-chain transport (deals → responses → confirmations as opaque
// bytes through the keeper Collect* methods, exactly as the on-chain transport
// will), finalizes at the confirm deadline, and returns the epoch id plus every
// node's local EpochKeyMaterial. The DKG runs OFF-CHAIN (NewInProcessCommittee);
// only the public bundles transit chain state.
func (f *epochFixture) runDKG(t testing.TB, boundaryHeight uint64, n int, outcome dkgOutcome) (uint64, []*threshold.EpochKeyMaterial) {
	t.Helper()

	f.setHeight(boundaryHeight)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	epochID := f.k.FormingEpochID(f.ctx)
	require.NotZero(t, epochID, "boundary did not open a FORMING epoch")
	e, ok := f.k.GetEpoch(f.ctx, epochID)
	require.True(t, ok)
	require.Equal(t, types.EPOCH_STATUS_FORMING, e.Status)
	require.Len(t, e.Committee, n)

	thr := int(e.Threshold)
	parts, err := threshold.NewInProcessCommittee(n, thr)
	require.NoError(t, err)

	// Round 1 — deals (deal window).
	f.setHeight(boundaryHeight + 1)
	for i, p := range parts {
		d, err := p.Round1Deal()
		require.NoError(t, err)
		enc, err := threshold.MarshalDealBundle(d)
		require.NoError(t, err)
		require.NoError(t, f.k.CollectDeal(f.ctx, epochID, uint32(i), enc))
	}

	// Round 2 — responses (response window). Each node reads ALL committed deals.
	respDeadline := e.DealDeadline + 3
	f.setHeight(e.DealDeadline)
	deals := decodeDealWire(t, f.k.Deals(f.ctx, epochID))
	for i, p := range parts {
		r, err := p.Round2Response(deals)
		require.NoError(t, err)
		if r == nil {
			continue
		}
		enc, err := threshold.MarshalResponseBundle(r)
		require.NoError(t, err)
		require.NoError(t, f.k.CollectResponse(f.ctx, epochID, uint32(i), enc))
	}

	// Round 3 — confirmations (confirm window). Each node finalizes locally and
	// posts its computed (epoch_pub_key, commits).
	f.setHeight(respDeadline)
	resps := decodeRespWire(t, f.k.Responses(f.ctx, epochID))
	mats := make([]*threshold.EpochKeyMaterial, 0, n)
	confirmers := n
	if outcome == dkgConfirmShort {
		confirmers = thr - 1 // below threshold → FAILED
	}
	for i, p := range parts {
		mat, err := p.Finalize(resps)
		require.NoError(t, err)
		mats = append(mats, mat)
		if i >= confirmers {
			continue
		}
		commits, err := threshold.MarshalCommits(mat.Commits)
		require.NoError(t, err)
		require.NoError(t, f.k.CollectConfirm(f.ctx, epochID, uint32(i), mat.EpochPub, commits))
	}

	// Finalize at the confirm deadline.
	f.setHeight(e.ConfirmDeadline)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	return epochID, mats
}
