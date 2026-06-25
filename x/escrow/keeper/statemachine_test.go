package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/escrow/types"
)

const denom = "uusdc"

// ---- fixture helpers ------------------------------------------------------

func (f *escrowFixture) atHeight(h int64) { f.ctx = f.ctx.WithBlockHeight(h) }

func (f *escrowFixture) get(t *testing.T, id string) types.Escrow {
	esc, err := f.k.GetEscrow(f.ctx, 0, id)
	require.NoError(t, err)
	return esc
}

func (f *escrowFixture) createHashlock(t *testing.T, id string, preimage []byte, timeoutBlocks uint64, amount sdk.Coins) {
	f.bank.fund(f.buyer.String(), amount)
	esc := newHashlockEscrow(id, sha256Of(preimage), timeoutBlocks, amount)
	require.NoError(t, f.k.CreateEscrow(f.ctx, esc, f.buyer, f.selr, nil))
	require.Equal(t, types.ESCROW_STATE_ACTIVE, f.get(t, id).State)
}

func (f *escrowFixture) createOptimistic(t *testing.T, id string, contentHash []byte, windowBlocks uint64, amount sdk.Coins) {
	f.bank.fund(f.buyer.String(), amount)
	f.bank.fund(f.selr.String(), bondOf(amount))
	esc := newOptimisticEscrow(id, windowBlocks, amount)
	require.NoError(t, f.k.CreateEscrow(f.ctx, esc, f.buyer, f.selr, contentHash))
	require.Equal(t, types.ESCROW_STATE_ACTIVE, f.get(t, id).State)
}

func (f *escrowFixture) ms() types.MsgServer { return NewMsgServerImpl(f.k) }

// ===========================================================================
// HASHLOCK
// ===========================================================================

func TestHashlock_ReleaseHappy(t *testing.T) {
	f := setupEscrow(t)
	pre := []byte("receipt-of-access-token")
	amt := coins(denom, 1000)
	f.atHeight(100)
	f.createHashlock(t, testID(1), pre, 10, amt)

	f.atHeight(105) // pre-timeout
	_, err := f.ms().ReleaseEscrow(f.ctx, &types.MsgReleaseEscrow{Signer: f.buyer.String(), EscrowId: testID(1), Preimage: pre})
	require.NoError(t, err)

	require.Equal(t, types.ESCROW_STATE_RELEASED, f.get(t, testID(1)).State)
	require.Equal(t, amt, f.bank.get(f.selr.String()), "payment paid to the seller on release")
	require.True(t, f.bank.get(types.ModuleName).IsZero(), "escrow account drained")
}

// load-bearing: a wrong preimage must REJECT and leave funds escrowed. If the
// H(preimage)==payload_hash guard were removed this would release.
func TestHashlock_WrongPreimage(t *testing.T) {
	f := setupEscrow(t)
	amt := coins(denom, 1000)
	f.atHeight(100)
	f.createHashlock(t, testID(2), []byte("correct"), 10, amt)

	f.atHeight(105)
	_, err := f.ms().ReleaseEscrow(f.ctx, &types.MsgReleaseEscrow{Signer: f.buyer.String(), EscrowId: testID(2), Preimage: []byte("WRONG")})
	require.ErrorIs(t, err, types.ErrPreimageMismatch)
	require.Equal(t, types.ESCROW_STATE_ACTIVE, f.get(t, testID(2)).State)
	require.Equal(t, amt, f.bank.get(types.ModuleName), "funds stay escrowed on a bad preimage")
}

// load-bearing: release at/after timeout must REJECT. If the height<timeout
// guard were removed this would release post-timeout.
func TestHashlock_PostTimeoutRelease(t *testing.T) {
	f := setupEscrow(t)
	pre := []byte("token")
	f.atHeight(100)
	f.createHashlock(t, testID(3), pre, 10, coins(denom, 1000)) // timeout at 110

	f.atHeight(110)
	_, err := f.ms().ReleaseEscrow(f.ctx, &types.MsgReleaseEscrow{Signer: f.buyer.String(), EscrowId: testID(3), Preimage: pre})
	require.ErrorIs(t, err, types.ErrTimedOut)
	require.Equal(t, types.ESCROW_STATE_ACTIVE, f.get(t, testID(3)).State)
}

func TestHashlock_TimeoutSweepRefunds(t *testing.T) {
	f := setupEscrow(t)
	amt := coins(denom, 1000)
	f.atHeight(100)
	f.createHashlock(t, testID(4), []byte("token"), 10, amt) // timeout at 110

	f.atHeight(110)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	require.Equal(t, types.ESCROW_STATE_REFUNDED, f.get(t, testID(4)).State)
	require.Equal(t, amt, f.bank.get(f.buyer.String()), "timeout refunds the buyer")
}

// load-bearing: re-releasing a terminal escrow must REJECT (double-release).
func TestHashlock_DoubleReleaseRejected(t *testing.T) {
	f := setupEscrow(t)
	pre := []byte("token")
	f.atHeight(100)
	f.createHashlock(t, testID(5), pre, 10, coins(denom, 1000))

	f.atHeight(105)
	_, err := f.ms().ReleaseEscrow(f.ctx, &types.MsgReleaseEscrow{Signer: f.buyer.String(), EscrowId: testID(5), Preimage: pre})
	require.NoError(t, err)
	_, err = f.ms().ReleaseEscrow(f.ctx, &types.MsgReleaseEscrow{Signer: f.buyer.String(), EscrowId: testID(5), Preimage: pre})
	require.ErrorIs(t, err, types.ErrTerminalState)
}

// load-bearing: a HASHLOCK has no dispute path.
func TestHashlock_DisputeRejected(t *testing.T) {
	f := setupEscrow(t)
	f.atHeight(100)
	f.createHashlock(t, testID(6), []byte("token"), 10, coins(denom, 1000))

	_, err := f.ms().RaiseDispute(f.ctx, &types.MsgRaiseDispute{Signer: f.buyer.String(), EscrowId: testID(6)})
	require.ErrorIs(t, err, types.ErrNoDisputePath)
	require.Equal(t, types.ESCROW_STATE_ACTIVE, f.get(t, testID(6)).State)
}

// after a HASHLOCK timeout sweep, the entry has no sweep index left — a second
// sweep is a no-op (the terminal escrow is never re-refunded).
func TestHashlock_SweepIsIdempotent(t *testing.T) {
	f := setupEscrow(t)
	amt := coins(denom, 1000)
	f.atHeight(100)
	f.createHashlock(t, testID(7), []byte("token"), 10, amt)
	f.atHeight(110)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	f.atHeight(111)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	require.Equal(t, amt, f.bank.get(f.buyer.String()), "buyer refunded exactly once")
}

// ===========================================================================
// OPTIMISTIC
// ===========================================================================

func TestOptimistic_AutoReleaseSweep(t *testing.T) {
	f := setupEscrow(t)
	amt := coins(denom, 1000)
	f.atHeight(100)
	f.createOptimistic(t, testID(10), []byte("content"), 10, amt) // window end 110

	f.atHeight(110)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	require.Equal(t, types.ESCROW_STATE_RELEASED, f.get(t, testID(10)).State)
	// seller receives payment + their returned bond.
	require.Equal(t, amt.Add(bondOf(amt)...), f.bank.get(f.selr.String()))
	require.Empty(t, f.sigil.penalized, "no dispute → no reputation penalty")
}

func TestOptimistic_DisputeThenResolveRefundsAndPenalizes(t *testing.T) {
	f := setupEscrow(t)
	amt := coins(denom, 1000)
	content := []byte("the-promised-bytes")
	f.atHeight(100)
	f.createOptimistic(t, testID(11), sha256Of(content), 10, amt) // window end 110

	f.atHeight(105) // in-window dispute
	_, err := f.ms().RaiseDispute(f.ctx, &types.MsgRaiseDispute{Signer: f.buyer.String(), EscrowId: testID(11)})
	require.NoError(t, err)
	require.Equal(t, types.ESCROW_STATE_DISPUTED, f.get(t, testID(11)).State)

	// seller never reveals → resolution deadline 115 → refund + slash + penalty.
	f.atHeight(115)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	require.Equal(t, types.ESCROW_STATE_REFUNDED, f.get(t, testID(11)).State)
	require.Equal(t, amt.Add(bondOf(amt)...), f.bank.get(f.buyer.String()), "buyer refunded payment + slashed bond")
	require.Equal(t, 1, f.sigil.penalizedCount(sellerSigil), "PenalizeLostDispute fired on the seller")
}

func TestOptimistic_DisputeThenSellerReveals(t *testing.T) {
	f := setupEscrow(t)
	amt := coins(denom, 1000)
	content := []byte("the-promised-bytes")
	f.atHeight(100)
	f.createOptimistic(t, testID(12), sha256Of(content), 10, amt)

	f.atHeight(105)
	_, err := f.ms().RaiseDispute(f.ctx, &types.MsgRaiseDispute{Signer: f.buyer.String(), EscrowId: testID(12)})
	require.NoError(t, err)

	// seller proves delivery by revealing the content preimage → RELEASED.
	f.atHeight(108)
	_, err = f.ms().ReleaseEscrow(f.ctx, &types.MsgReleaseEscrow{Signer: f.selr.String(), EscrowId: testID(12), Preimage: content})
	require.NoError(t, err)
	require.Equal(t, types.ESCROW_STATE_RELEASED, f.get(t, testID(12)).State)
	require.Equal(t, amt.Add(bondOf(amt)...), f.bank.get(f.selr.String()), "seller paid + bond returned")
	require.Empty(t, f.sigil.penalized, "successful defense → no penalty")
}

// load-bearing: wrong reveal preimage under dispute must REJECT and stay
// DISPUTED (the seller hasn't proven delivery).
func TestOptimistic_WrongRevealKeepsDisputed(t *testing.T) {
	f := setupEscrow(t)
	f.atHeight(100)
	f.createOptimistic(t, testID(13), sha256Of([]byte("real")), 10, coins(denom, 1000))
	f.atHeight(105)
	_, err := f.ms().RaiseDispute(f.ctx, &types.MsgRaiseDispute{Signer: f.buyer.String(), EscrowId: testID(13)})
	require.NoError(t, err)

	_, err = f.ms().ReleaseEscrow(f.ctx, &types.MsgReleaseEscrow{Signer: f.selr.String(), EscrowId: testID(13), Preimage: []byte("FAKE")})
	require.ErrorIs(t, err, types.ErrPreimageMismatch)
	require.Equal(t, types.ESCROW_STATE_DISPUTED, f.get(t, testID(13)).State)
}

// load-bearing: a dispute after the window must REJECT.
func TestOptimistic_DisputePostWindowRejected(t *testing.T) {
	f := setupEscrow(t)
	f.atHeight(100)
	f.createOptimistic(t, testID(14), []byte("c"), 10, coins(denom, 1000)) // window end 110
	f.atHeight(110)
	_, err := f.ms().RaiseDispute(f.ctx, &types.MsgRaiseDispute{Signer: f.buyer.String(), EscrowId: testID(14)})
	require.ErrorIs(t, err, types.ErrDisputeWindow)
}

// load-bearing: MsgReleaseEscrow on an undisputed OPTIMISTIC escrow must REJECT
// (release happens only via the sweep or a dispute reveal — never directly).
func TestOptimistic_ReleaseWhileActiveRejected(t *testing.T) {
	f := setupEscrow(t)
	content := []byte("c")
	f.atHeight(100)
	f.createOptimistic(t, testID(15), sha256Of(content), 10, coins(denom, 1000))
	f.atHeight(105)
	_, err := f.ms().ReleaseEscrow(f.ctx, &types.MsgReleaseEscrow{Signer: f.selr.String(), EscrowId: testID(15), Preimage: content})
	require.ErrorIs(t, err, types.ErrNotDisputed)
	require.Equal(t, types.ESCROW_STATE_ACTIVE, f.get(t, testID(15)).State)
}

// load-bearing: the auto-release sweep must NOT release a DISPUTED escrow. After
// a dispute the window-end sweep entry is gone, so the original window height
// sweeps nothing; the escrow stays DISPUTED until its resolution deadline.
func TestOptimistic_SweepSkipsDisputed(t *testing.T) {
	f := setupEscrow(t)
	f.atHeight(100)
	f.createOptimistic(t, testID(16), sha256Of([]byte("c")), 10, coins(denom, 1000)) // window end 110
	f.atHeight(105)
	_, err := f.ms().RaiseDispute(f.ctx, &types.MsgRaiseDispute{Signer: f.buyer.String(), EscrowId: testID(16)})
	require.NoError(t, err)

	f.atHeight(110) // original window end — must NOT auto-release
	require.NoError(t, f.k.EndBlocker(f.ctx))
	require.Equal(t, types.ESCROW_STATE_DISPUTED, f.get(t, testID(16)).State, "disputed escrow not auto-released")
}

// load-bearing: a second dispute on a DISPUTED escrow must REJECT.
func TestOptimistic_DoubleDisputeRejected(t *testing.T) {
	f := setupEscrow(t)
	f.atHeight(100)
	f.createOptimistic(t, testID(17), sha256Of([]byte("c")), 10, coins(denom, 1000))
	f.atHeight(105)
	_, err := f.ms().RaiseDispute(f.ctx, &types.MsgRaiseDispute{Signer: f.buyer.String(), EscrowId: testID(17)})
	require.NoError(t, err)
	_, err = f.ms().RaiseDispute(f.ctx, &types.MsgRaiseDispute{Signer: f.buyer.String(), EscrowId: testID(17)})
	require.ErrorIs(t, err, types.ErrAlreadyDisputed)
}

// load-bearing: only the buyer (payer) may dispute.
func TestOptimistic_DisputeUnauthorizedRejected(t *testing.T) {
	f := setupEscrow(t)
	f.atHeight(100)
	f.createOptimistic(t, testID(18), sha256Of([]byte("c")), 10, coins(denom, 1000))
	f.atHeight(105)
	// the seller is not the payer — must be rejected.
	_, err := f.ms().RaiseDispute(f.ctx, &types.MsgRaiseDispute{Signer: f.selr.String(), EscrowId: testID(18)})
	require.ErrorIs(t, err, types.ErrUnauthorized)
	require.Equal(t, types.ESCROW_STATE_ACTIVE, f.get(t, testID(18)).State)
}

func TestOptimistic_InsufficientSellerBondRejected(t *testing.T) {
	f := setupEscrow(t)
	amt := coins(denom, 1000)
	f.bank.fund(f.buyer.String(), amt) // buyer can pay, seller cannot bond
	esc := newOptimisticEscrow(testID(19), 10, amt)
	err := f.k.CreateEscrow(f.ctx, esc, f.buyer, f.selr, sha256Of([]byte("c")))
	require.ErrorIs(t, err, types.ErrInsufficientBond)
}

// ===========================================================================
// Sweep determinism
// ===========================================================================

// the sweep fires only entries due at/under the current height, deterministically
// (sorted index). Entries timing out later are untouched.
func TestSweep_OnlyDueEntries(t *testing.T) {
	f := setupEscrow(t)
	f.atHeight(100)
	f.createHashlock(t, testID(20), []byte("a"), 10, coins(denom, 100)) // due 110
	f.createHashlock(t, testID(21), []byte("b"), 20, coins(denom, 100)) // due 120

	f.atHeight(110)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	require.Equal(t, types.ESCROW_STATE_REFUNDED, f.get(t, testID(20)).State)
	require.Equal(t, types.ESCROW_STATE_ACTIVE, f.get(t, testID(21)).State, "later timeout untouched")

	f.atHeight(120)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	require.Equal(t, types.ESCROW_STATE_REFUNDED, f.get(t, testID(21)).State)
}

// many escrows due at the same height all sweep in one EndBlock, terminally and
// completely (the sorted-index iteration is order-stable across nodes).
func TestSweep_BatchSameHeight(t *testing.T) {
	f := setupEscrow(t)
	f.atHeight(100)
	ids := []string{testID(30), testID(31), testID(32), testID(33)}
	for _, id := range ids {
		f.createHashlock(t, id, []byte(id), 10, coins(denom, 100)) // all due 110
	}
	f.atHeight(110)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	for _, id := range ids {
		require.Equal(t, types.ESCROW_STATE_REFUNDED, f.get(t, id).State, "all due escrows swept")
	}
	require.True(t, f.bank.get(types.ModuleName).IsZero(), "escrow account fully drained")
}

// ===========================================================================
// ZK_DELIVERY is not enabled until 2.1b
// ===========================================================================

func TestZk_CreateRejected(t *testing.T) {
	f := setupEscrow(t)
	esc := types.Escrow{
		Id: testID(40), PayerSigil: buyerSigil, PayeeSigil: sellerSigil,
		Condition: types.DeliveryCondition{Condition: &types.DeliveryCondition_ZkDelivery{
			ZkDelivery: &types.ZkDeliveryCondition{},
		}},
	}
	err := f.k.CreateEscrow(f.ctx, esc, f.buyer, f.selr, nil)
	require.ErrorIs(t, err, types.ErrZkNotEnabled)
}

func TestZk_SubmitProofRejected(t *testing.T) {
	f := setupEscrow(t)
	_, err := f.ms().SubmitDeliveryProof(f.ctx, &types.MsgSubmitDeliveryProof{Signer: f.buyer.String(), EscrowId: testID(40), Proof: []byte("p")})
	require.ErrorIs(t, err, types.ErrZkNotEnabled)
}

// ===========================================================================
// NONE-condition record-only escrow (the Phase-0 DataGrant stub, post-handoff)
// ===========================================================================

func TestNone_RecordOnlyNoSweepNoRelease(t *testing.T) {
	f := setupEscrow(t)
	f.atHeight(100)
	esc := types.Escrow{Id: testID(50), PayerSigil: buyerSigil, PayeeSigil: sellerSigil}
	require.NoError(t, f.k.CreateEscrow(f.ctx, esc, f.buyer, f.selr, nil))
	require.Equal(t, types.ESCROW_STATE_ACTIVE, f.get(t, testID(50)).State)

	// no funds moved, no sweep entry → EndBlock never touches it.
	f.atHeight(100000)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	require.Equal(t, types.ESCROW_STATE_ACTIVE, f.get(t, testID(50)).State, "NONE stub is record-only")

	// it has no release path (wrong class).
	_, err := f.ms().ReleaseEscrow(f.ctx, &types.MsgReleaseEscrow{Signer: f.buyer.String(), EscrowId: testID(50)})
	require.ErrorIs(t, err, types.ErrWrongClass)
}
