package keeper

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/crypto/zkdelivery"
	"github.com/AryaSingh22/conclave/x/escrow/types"
)

// Task 2.9 (D-100) path enumeration for the escrow→receipt write-back.
//
// The hook itself hangs off commitTerminal, which is class-agnostic — so the
// question Rule 4 step 5 asks is not "does the hook work?" (app-level tests
// answer that end to end, through the ICS-23 proof surface) but "HOW MANY WAYS
// are there for an escrow to change a state a receipt reports, and does each
// one report it?" This file answers it exhaustively: all EIGHT transition
// events in transition.go, across all three condition classes, plus the NONE
// class that must never report at all.
//
// It is also where ZK_DELIVERY's two paths are covered. The app-level gate
// exercises HASHLOCK and OPTIMISTIC (they resolve in a handful of blocks);
// ZK_DELIVERY's refund is 14,400 blocks out (ZkDeliveryTimeoutBlocks) and its
// release needs a real groth16 proof, both of which belong here, at the level
// where a height can simply be set. The live ZK release is additionally
// demonstrated end to end by the §6 exit demo, whose settlement IS a ZK buy.

type receiptOutcomeCall struct {
	escrowID string
	state    types.EscrowState
}

// mockReceiptOutcome records every write-back call, and can fail on demand.
type mockReceiptOutcome struct {
	calls []receiptOutcomeCall
	err   error
}

func (m *mockReceiptOutcome) SetReceiptOutcomeForEscrow(_ sdk.Context, id string, s types.EscrowState) error {
	m.calls = append(m.calls, receiptOutcomeCall{escrowID: id, state: s})
	return m.err
}

func (f *escrowFixture) withReceiptOutcome() *mockReceiptOutcome {
	m := &mockReceiptOutcome{}
	f.k.SetReceiptOutcomeWriter(m)
	return m
}

func (f *escrowFixture) createZkAt(t *testing.T, id string, payload []byte, amount sdk.Coins) []byte {
	t.Helper()
	proof, schema := proveFor(t, id, payload)
	f.bank.fund(f.buyer.String(), amount)
	require.NoError(t, f.k.CreateEscrow(f.ctx,
		newZkEscrow(id, schema, zkdelivery.VKIDDeliveryV1, amount), f.buyer, f.selr, nil))
	require.Equal(t, types.ESCROW_STATE_ACTIVE, f.get(t, id).State)
	return proof
}

// TestReceiptOutcomeHook_EveryTransitionReports drives each of the eight
// transition events and asserts the receipts are told, with the right state.
// A class or event added later without a write-back fails here.
func TestReceiptOutcomeHook_EveryTransitionReports(t *testing.T) {
	amount := coins(denom, 1_000)

	cases := []struct {
		name string
		want []types.EscrowState
		run  func(t *testing.T, f *escrowFixture, id string)
	}{
		{
			name: "HASHLOCK release (preimage)",
			want: []types.EscrowState{types.ESCROW_STATE_RELEASED},
			run: func(t *testing.T, f *escrowFixture, id string) {
				f.createHashlock(t, id, []byte("pre"), 50, amount)
				require.NoError(t, f.k.ReleaseHashlock(f.ctx, f.get(t, id), []byte("pre")))
			},
		},
		{
			name: "HASHLOCK timeout sweep",
			want: []types.EscrowState{types.ESCROW_STATE_REFUNDED},
			run: func(t *testing.T, f *escrowFixture, id string) {
				f.createHashlock(t, id, []byte("pre"), 50, amount)
				f.atHeight(200)
				require.NoError(t, f.k.EndBlocker(f.ctx))
			},
		},
		{
			name: "OPTIMISTIC auto-release sweep",
			want: []types.EscrowState{types.ESCROW_STATE_RELEASED},
			run: func(t *testing.T, f *escrowFixture, id string) {
				f.createOptimistic(t, id, sha256Of([]byte("content")), 50, amount)
				f.atHeight(200)
				require.NoError(t, f.k.EndBlocker(f.ctx))
			},
		},
		{
			name: "OPTIMISTIC dispute raised (NOT terminal)",
			want: []types.EscrowState{types.ESCROW_STATE_DISPUTED},
			run: func(t *testing.T, f *escrowFixture, id string) {
				f.createOptimistic(t, id, sha256Of([]byte("content")), 50, amount)
				require.NoError(t, f.k.RaiseDispute(f.ctx, f.get(t, id)))
			},
		},
		{
			name: "OPTIMISTIC seller reveals under dispute",
			want: []types.EscrowState{types.ESCROW_STATE_DISPUTED, types.ESCROW_STATE_RELEASED},
			run: func(t *testing.T, f *escrowFixture, id string) {
				f.createOptimistic(t, id, sha256Of([]byte("content")), 50, amount)
				require.NoError(t, f.k.RaiseDispute(f.ctx, f.get(t, id)))
				require.NoError(t, f.k.RevealOptimistic(f.ctx, f.get(t, id), []byte("content")))
			},
		},
		{
			name: "OPTIMISTIC dispute-resolution sweep (seller never proved)",
			want: []types.EscrowState{types.ESCROW_STATE_DISPUTED, types.ESCROW_STATE_REFUNDED},
			run: func(t *testing.T, f *escrowFixture, id string) {
				f.createOptimistic(t, id, sha256Of([]byte("content")), 50, amount)
				require.NoError(t, f.k.RaiseDispute(f.ctx, f.get(t, id)))
				f.atHeight(400)
				require.NoError(t, f.k.EndBlocker(f.ctx))
			},
		},
		{
			name: "ZK_DELIVERY release (verifying proof)",
			want: []types.EscrowState{types.ESCROW_STATE_RELEASED},
			run: func(t *testing.T, f *escrowFixture, id string) {
				proof := f.createZkAt(t, id, []byte("zk-payload"), amount)
				require.NoError(t, f.k.ReleaseZk(f.ctx, f.get(t, id), proof))
			},
		},
		{
			name: "ZK_DELIVERY timeout sweep (no proof arrived)",
			want: []types.EscrowState{types.ESCROW_STATE_REFUNDED},
			run: func(t *testing.T, f *escrowFixture, id string) {
				f.createZkAt(t, id, []byte("zk-payload"), amount)
				f.atHeight(int64(100 + types.ZkDeliveryTimeoutBlocks + 1))
				require.NoError(t, f.k.EndBlocker(f.ctx))
			},
		},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := setupEscrow(t)
			rec := f.withReceiptOutcome()
			id := testID(byte(0xA0 + i))
			tc.run(t, f, id)

			require.Len(t, rec.calls, len(tc.want),
				"every state a receipt reports must be reported exactly once")
			for j := range tc.want {
				require.Equal(t, id, rec.calls[j].escrowID)
				require.Equal(t, tc.want[j], rec.calls[j].state)
			}
		})
	}
}

// TestReceiptOutcomeHook_NoneClassNeverReports: a record-only DataGrant escrow
// has no transitions at all, so it must never touch a receipt — which is also
// why CONCLUDE does not index it (x/session keys.go 0x0C).
func TestReceiptOutcomeHook_NoneClassNeverReports(t *testing.T) {
	f := setupEscrow(t)
	rec := f.withReceiptOutcome()
	id := testID(0xB1)

	esc := types.Escrow{
		Id: id, PayerSigil: buyerSigil, PayeeSigil: sellerSigil,
		Amount: sdk.NewCoins(), ChamberId: 0,
	}
	require.NoError(t, f.k.CreateEscrow(f.ctx, esc, f.buyer, f.selr, nil))
	f.atHeight(10_000)
	require.NoError(t, f.k.EndBlocker(f.ctx))

	require.Empty(t, rec.calls, "a NONE escrow never resolves, so it never reports")
}

// TestReceiptOutcomeHook_WriteBackFailureAbortsTheTransition: the receipt and
// the escrow move together or not at all. A resolution that paid out while
// failing to update its receipt would reintroduce exactly the inconsistency
// task 2.9 closes, so the error is propagated rather than swallowed.
func TestReceiptOutcomeHook_WriteBackFailureAbortsTheTransition(t *testing.T) {
	f := setupEscrow(t)
	rec := f.withReceiptOutcome()
	rec.err = errors.New("boom")
	id := testID(0xB2)

	f.createHashlock(t, id, []byte("pre"), 50, coins(denom, 1_000))
	err := f.k.ReleaseHashlock(f.ctx, f.get(t, id), []byte("pre"))
	require.ErrorContains(t, err, "boom")
}

// TestReceiptOutcomeHook_UnwiredKeeperStillResolves pins the nil-tolerance the
// seam needs: x/escrow must work with no receipt writer at all (every keeper
// fixture in this package runs that way, and so does a genesis-imported chain
// with no receipts). The app-level gate is what proves the production wiring is
// present — this only proves its absence is not a crash.
func TestReceiptOutcomeHook_UnwiredKeeperStillResolves(t *testing.T) {
	f := setupEscrow(t) // no withReceiptOutcome()
	id := testID(0xB3)
	f.createHashlock(t, id, []byte("pre"), 50, coins(denom, 1_000))
	require.NoError(t, f.k.ReleaseHashlock(f.ctx, f.get(t, id), []byte("pre")))
	require.Equal(t, types.ESCROW_STATE_RELEASED, f.get(t, id).State)
}
