package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/crypto/reveal"
	"github.com/AryaSingh22/conclave/crypto/threshold"
	"github.com/AryaSingh22/conclave/x/epoch/types"
)

// register_justify_test.go proves the Task-1.7 / D-070 on-chain behaviors of the
// A2 registration round and the B2 justification round at the keeper level:
//   - the DKG-key↔validator binding is protocol-enforced (build condition 2);
//   - a faulty dealer is excluded and the epoch STILL reaches ACTIVE on chain
//     (build condition 1, the network-real counterpart of the crypto-layer
//     TestJustificationExcludesFaultyDealerAndCompletes);
//   - a non-registering candidate cannot veto the epoch (the property the
//     registration round must preserve to not undo B2);
//   - too few registrations FAILEDs the epoch rather than silently producing an
//     insecure too-small committee (build condition 3).

// openForming opens a FORMING epoch at a boundary with the given candidate set.
func (f *epochFixture) openForming(t testing.TB, boundary uint64, vals []string) uint64 {
	t.Helper()
	f.staking.setValoper(vals)
	f.setHeight(boundary)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	id := f.k.FormingEpochID(f.ctx)
	require.NotZero(t, id)
	return id
}

// TestRegisterBindingRejectsNonCandidate proves BUILD CONDITION 2: a DKG-key
// registration is bound to the registrant's validator identity. A registration
// whose valoper is NOT a bonded candidate of the FORMING epoch is REJECTED — a
// key cannot be planted in an honest validator's slot (the analog of 1.6's
// forged-vote-extension test). Because registration is keyed by the SIGNER's own
// valoper (msg_server derives it from the signer account), a validator can only
// ever register its own slot.
func TestRegisterBindingRejectsNonCandidate(t *testing.T) {
	f := setupEpoch(t)
	vals := valopers(4)
	id := f.openForming(t, 100, vals)
	f.setHeight(101)

	_, pub, err := threshold.GenerateLongTermKey()
	require.NoError(t, err)

	// an outsider valoper (not in the candidate set) is rejected.
	outsider := valopers(1)[0]
	err = f.k.CollectRegistration(f.ctx, id, outsider, pub)
	require.ErrorIs(t, err, types.ErrNotCandidate)

	// a real candidate registers successfully...
	require.NoError(t, f.k.CollectRegistration(f.ctx, id, vals[0], pub))
	// ...and cannot register twice.
	err = f.k.CollectRegistration(f.ctx, id, vals[0], pub)
	require.ErrorIs(t, err, types.ErrDuplicateRegister)
}

// TestRegisterBindingViaMsgServer proves the binding through the actual msg
// server: the registration valoper is DERIVED from the signer account, so a
// non-validator signer (its valoper not a candidate) is rejected, and a candidate
// signer registers its own slot.
func TestRegisterBindingViaMsgServer(t *testing.T) {
	f := setupEpoch(t)
	// candidates derived from real account addresses so the signer→valoper
	// derivation lands on a candidate.
	acc0 := sdk.AccAddress([]byte("validator-account-00"))
	val0 := sdk.ValAddress(acc0).String()
	vals := append([]string{val0}, valopers(3)...)
	id := f.openForming(t, 100, vals)
	f.setHeight(101)
	srv := NewMsgServerImpl(f.k)

	_, pub, err := threshold.GenerateLongTermKey()
	require.NoError(t, err)

	// outsider signer → its valoper is not a candidate → rejected.
	outsider := sdk.AccAddress([]byte("not-a-validator-acct"))
	_, err = srv.RegisterDKGKey(f.ctx, &types.MsgRegisterDKGKey{
		Sender: outsider.String(), EpochId: id, DkgPubKey: pub,
	})
	require.ErrorIs(t, err, types.ErrNotCandidate)

	// candidate signer registers its own slot.
	_, err = srv.RegisterDKGKey(f.ctx, &types.MsgRegisterDKGKey{
		Sender: acc0.String(), EpochId: id, DkgPubKey: pub,
	})
	require.NoError(t, err)
}

// TestFaultyDealerExcludedEpochActivates proves BUILD CONDITION 1 on chain: with
// one faulty dealer in an n=4 / t=3 committee, the justification round excludes it
// and the epoch reaches ACTIVE (the published key decrypts a t-of-n reveal from
// the honest shares) — NOT FAILED. The single-point veto is closed end to end
// through the on-chain transport.
func TestFaultyDealerExcludedEpochActivates(t *testing.T) {
	f := setupEpoch(t)
	f.staking.setValoper(valopers(4))
	id, mats := f.runDKGFaulty(t, 100, 4, dkgSucceed, map[int]bool{1: true})

	e, ok := f.k.GetEpoch(f.ctx, id)
	require.True(t, ok)
	require.Equal(t, types.EPOCH_STATUS_ACTIVE, e.Status,
		"a single faulty dealer must NOT veto the epoch — justification excludes it and the DKG completes")
	require.NotEmpty(t, e.EpochPubKey)
	require.Len(t, mats, 3, "only the 3 honest nodes contribute material")

	// the published key works: encrypt to it, reveal with the 3 honest shares.
	client := threshold.NewClientScheme()
	payload := []byte("post-justification on-chain key")
	ct, err := client.Encrypt(e.EpochPubKey, payload)
	require.NoError(t, err)
	sch, err := threshold.NewSchemeFromCommitBytes(e.Commits, len(e.Committee))
	require.NoError(t, err)
	shares := make([]reveal.RevealShare, 0, 3)
	for _, m := range mats {
		rs, err := sch.PartialReveal(m.Share, ct)
		require.NoError(t, err)
		shares = append(shares, rs)
	}
	got, err := sch.Combine(shares, ct)
	require.NoError(t, err)
	require.Equal(t, payload, got)
}

// TestNonRegisteringCandidateDoesNotVeto proves the registration round preserves
// the no-single-point-veto property (consistent with B2): with min_committee_size
// = 3, three of four candidates register, the committee finalizes to those three,
// and the DKG completes ACTIVE. The fourth candidate's silence cannot block it.
func TestNonRegisteringCandidateDoesNotVeto(t *testing.T) {
	f := setupEpoch(t)
	p, err := f.k.GetParams(f.ctx)
	require.NoError(t, err)
	p.MinCommitteeSize = 3
	require.NoError(t, f.k.SetParams(f.ctx, p))

	vals := valopers(4)
	id := f.openForming(t, 100, vals)

	// only 3 of 4 candidates register.
	f.setHeight(101)
	registered := vals[:3]
	secrets := map[string][]byte{}
	for _, v := range registered {
		sec, pub, err := threshold.GenerateLongTermKey()
		require.NoError(t, err)
		secrets[v] = sec
		require.NoError(t, f.k.CollectRegistration(f.ctx, id, v, pub))
	}

	// register-window close — committee finalizes to the 3 registrants.
	f.setHeight(100 + p.RegisterWindowBlocks)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	e, ok := f.k.GetEpoch(f.ctx, id)
	require.True(t, ok)
	require.Equal(t, types.EPOCH_STATUS_FORMING, e.Status, "epoch must proceed, not be vetoed")
	require.Len(t, e.Committee, 3, "committee = the 3 registrants")

	// drive the remaining rounds over the 3-member roster → ACTIVE.
	thr := int(e.Threshold)
	regs := f.k.Registrations(f.ctx, id)
	roster := make([][]byte, len(regs))
	for i, r := range regs {
		roster[i] = r.DkgPubKey
	}
	parts := make([]*threshold.DKGParticipant, len(regs))
	for i, r := range regs {
		pp, err := threshold.NewParticipantFromRoster(id, i, secrets[r.Valoper], roster, thr)
		require.NoError(t, err)
		parts[i] = pp
	}
	f.setHeight(e.DealDeadline - 1)
	for i, pp := range parts {
		d, err := pp.Round1Deal()
		require.NoError(t, err)
		enc, err := threshold.MarshalDealBundle(d)
		require.NoError(t, err)
		require.NoError(t, f.k.CollectDeal(f.ctx, id, uint32(i), enc))
	}
	f.setHeight(e.DealDeadline)
	deals := decodeDealWire(t, f.k.Deals(f.ctx, id))
	for i, pp := range parts {
		r, err := pp.Round2Response(deals)
		require.NoError(t, err)
		if r == nil {
			continue
		}
		enc, err := threshold.MarshalResponseBundle(r)
		require.NoError(t, err)
		require.NoError(t, f.k.CollectResponse(f.ctx, id, uint32(i), enc))
	}
	f.setHeight(e.DealDeadline + p.ResponseWindowBlocks + p.JustifyWindowBlocks)
	resps := decodeRespWire(t, f.k.Responses(f.ctx, id))
	for i, pp := range parts {
		mat, jb, err := pp.ProcessResponses(resps)
		require.NoError(t, err)
		require.Nil(t, jb)
		require.NotNil(t, mat)
		com, err := threshold.MarshalCommits(mat.Commits)
		require.NoError(t, err)
		require.NoError(t, f.k.CollectConfirm(f.ctx, id, uint32(i), mat.EpochPub, com))
	}
	f.setHeight(e.ConfirmDeadline)
	require.NoError(t, f.k.EndBlocker(f.ctx))

	e, _ = f.k.GetEpoch(f.ctx, id)
	require.Equal(t, types.EPOCH_STATUS_ACTIVE, e.Status)
}

// TestTooFewRegistrationsFailsEpoch proves BUILD CONDITION 3 / the security
// floor: when fewer than min_committee_size validators register, the epoch
// FAILEDs (the prior key stays) rather than completing with an insecure too-small
// committee. min_committee_size defaults to 4; only 3 of 4 register here.
func TestTooFewRegistrationsFailsEpoch(t *testing.T) {
	f := setupEpoch(t) // MinCommitteeSize = 4
	vals := valopers(4)
	id := f.openForming(t, 100, vals)

	f.setHeight(101)
	for _, v := range vals[:3] { // only 3 register, below the floor of 4
		_, pub, err := threshold.GenerateLongTermKey()
		require.NoError(t, err)
		require.NoError(t, f.k.CollectRegistration(f.ctx, id, v, pub))
	}

	p, _ := f.k.GetParams(f.ctx)
	f.setHeight(100 + p.RegisterWindowBlocks)
	require.NoError(t, f.k.EndBlocker(f.ctx))

	e, ok := f.k.GetEpoch(f.ctx, id)
	require.True(t, ok)
	require.Equal(t, types.EPOCH_STATUS_FAILED, e.Status)
	require.Zero(t, f.k.FormingEpochID(f.ctx), "FAILED epoch releases the forming slot")
	require.Zero(t, f.k.CurrentEpochID(f.ctx), "no rotation on a FAILED DKG")
}
