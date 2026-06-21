package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/veil/types"
)

// mockEpoch is a controllable decryptable-epoch oracle for the submit-time
// epoch-tag gate (D-068).
type mockEpoch struct{ decryptable map[uint64]bool }

func (m mockEpoch) IsDecryptableEpoch(_ sdk.Context, id uint64) bool { return m.decryptable[id] }

// GetEpochVerification satisfies the Task-1.6 EpochKeeper surface; the epoch-tag
// tests do not drive the Reveal, so it returns no material.
func (mockEpoch) GetEpochVerification(_ sdk.Context, _ uint64) ([][]byte, int, bool) {
	return nil, 0, false
}

func epochEnvelope(t testingT, ttl, epochID uint64) []byte {
	t.Helper()
	env := types.IntentEnvelope{
		TtlHeight:       ttl,
		EpochId:         epochID,
		SpendCommitment: sdk.NewCoins(sdk.NewInt64Coin(usdcDenom, 100_000_000)),
	}
	bz, err := env.Marshal()
	require.NoError(t, err)
	return bz
}

// TestSubmitIntentEpochTagBothEdges proves the submit-time epoch-tag gate
// (D-068): epoch_id 0 (stub/unset) is always accepted; a currently-decryptable
// epoch (ACTIVE/DRAINING) is accepted; and a non-decryptable epoch is rejected —
// covering BOTH edges (a RETIRED past epoch and a not-yet-published future epoch
// are both non-decryptable, so both reject through the same gate).
func TestSubmitIntentEpochTagBothEdges(t *testing.T) {
	f := setupKeeper(t)
	// epoch 7 is decryptable (ACTIVE or DRAINING); 8 (future, not-yet-published)
	// and 9 (past, RETIRED) are not — both edges fail the same gate.
	f.k.SetEpochKeeper(mockEpoch{decryptable: map[uint64]bool{7: true}})

	owner := randAddr()
	sigilID := f.registerSigil(t, owner)
	f.bank.fund(owner.String(), sdk.NewCoins(sdk.NewInt64Coin(bondDenom, 1_000_000_000)))
	payload := payloadBytes(t, sdk.NewInt64Coin(usdcDenom, 100_000_000))

	// epoch_id 0 — stub/unset path, always accepted.
	_, err := f.k.SubmitIntent(f.ctx, submitMsg(owner, sigilID, payload, epochEnvelope(t, 100, 0), minBond()))
	require.NoError(t, err)

	// epoch_id 7 — currently decryptable, accepted.
	_, err = f.k.SubmitIntent(f.ctx, submitMsg(owner, sigilID, payload, epochEnvelope(t, 100, 7), minBond()))
	require.NoError(t, err)

	// epoch_id 8 — future edge (not yet published): rejected.
	_, err = f.k.SubmitIntent(f.ctx, submitMsg(owner, sigilID, payload, epochEnvelope(t, 100, 8), minBond()))
	require.ErrorIs(t, err, types.ErrEpochNotDecryptable)

	// epoch_id 9 — past edge (RETIRED): rejected.
	_, err = f.k.SubmitIntent(f.ctx, submitMsg(owner, sigilID, payload, epochEnvelope(t, 100, 9), minBond()))
	require.ErrorIs(t, err, types.ErrEpochNotDecryptable)
}
