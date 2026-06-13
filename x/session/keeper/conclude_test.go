package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// TestThreePartyCycleClears is the §10 row-0.8 acceptance core: a 3-party cycle
// (alice 100 uusdc → dataset D; bob D → 10 CMPT; carol 10 CMPT → 100 uusdc)
// clears atomically in one Conclude. Coins route through the module (it nets
// zero), the CMPT voucher mints to bob, the DataGrant opens an escrow stub, and
// every intent reaches CONCLUDED with its bond refunded.
func TestThreePartyCycleClears(t *testing.T) {
	f := setupKeeper(t)
	c := f.setupCycle(t)

	// pre-state: each owner's bond is escrowed in x/veil; alice holds the
	// 100 uusdc she will give.
	require.Equal(t, math.NewInt(3_000_000), f.bank.balanceOf(veilModuleAddr).AmountOf(bondDenom))
	require.Equal(t, math.NewInt(100_000_000), f.bank.balanceOf(c.alice.String()).AmountOf(usdcDenom))
	usdcSupplyBefore := f.bank.total(usdcDenom)

	require.NoError(t, f.k.Conclude(f.ctx, c.sessionID, c.matches()))

	// every intent CONCLUDED (driven through x/veil inside the cache).
	for _, id := range []string{c.aliceID, c.bobID, c.carolID} {
		rec, err := f.veil.GetIntentRecord(f.ctx, 0, id)
		require.NoError(t, err)
		require.Equal(t, veiltypes.INTENT_STATE_CONCLUDED, rec.State, "intent %s", id)
	}

	// coin leg: alice -100 uusdc, carol +100 uusdc; supply unchanged; the
	// settlement module account nets exactly zero.
	require.True(t, f.bank.balanceOf(c.alice.String()).AmountOf(usdcDenom).IsZero())
	require.Equal(t, math.NewInt(100_000_000), f.bank.balanceOf(c.carol.String()).AmountOf(usdcDenom))
	require.Equal(t, usdcSupplyBefore, f.bank.total(usdcDenom), "no uusdc created or destroyed")
	require.True(t, f.bank.balanceOf(sessionModuleAddr).AmountOf(usdcDenom).IsZero(), "module nets zero")

	// svc voucher minted to bob.
	require.Equal(t, math.NewInt(10), f.bank.balanceOf(c.bob.String()).AmountOf(svcDenom))

	// bonds refunded to every owner (CONCLUDED, D-041); veil module emptied.
	for _, addr := range []sdk.AccAddress{c.alice, c.bob, c.carol} {
		require.Equal(t, minBond().Amount, f.bank.balanceOf(addr.String()).AmountOf(bondDenom), "bond refunded to %s", addr)
	}
	require.True(t, f.bank.balanceOf(veilModuleAddr).AmountOf(bondDenom).IsZero())

	// DataGrant leg opened an escrow stub.
	require.True(t, hasEvent(f.ctx, types.EventTypeEscrowOpened), "DataGrant leg opens an escrow stub")
}

// TestConcludeAtomicityOnLegFailure proves the all-or-nothing guarantee plus
// Guard 3: when a mid-graph leg fails, the cache is discarded so NO leg, bond
// refund, or intent state change persists — yet the solver slash and the
// FALLBACK transition (applied on the parent ctx) DO persist.
func TestConcludeAtomicityOnLegFailure(t *testing.T) {
	f := setupKeeper(t)
	c := f.setupCycle(t)

	// drain alice so the alice→carol 100 uusdc leg fails mid-graph.
	require.NoError(t, f.bank.send(c.alice.String(), randAddr().String(), sdk.NewCoins(sdk.NewInt64Coin(usdcDenom, 100_000_000))))

	solverAddr := randAddr()
	f.registerSolver(t, solverAddr, minSolverBond())
	bondBefore := f.bank.balanceOf(sessionModuleAddr).AmountOf(bondDenom)
	require.Equal(t, minSolverBond().Amount, bondBefore)

	session, err := f.k.GetSession(f.ctx, 0, c.sessionID)
	require.NoError(t, err)
	sol := types.Solution{
		Id:        types.IDHex(contentHash("losing-solution")),
		SessionId: c.sessionID,
		Solver:    solverAddr.String(),
		Matches:   c.matches(),
		Score:     math.LegacyZeroDec(),
	}

	veilBondBefore := f.bank.balanceOf(veilModuleAddr).AmountOf(bondDenom)

	err = f.k.concludeSolution(f.ctx, session, sol)
	require.ErrorIs(t, err, types.ErrConcludeFailed)

	// ZERO leg / refund / intent state change: the cache was discarded.
	for _, id := range []string{c.aliceID, c.bobID, c.carolID} {
		rec, gErr := f.veil.GetIntentRecord(f.ctx, 0, id)
		require.NoError(t, gErr)
		require.Equal(t, veiltypes.INTENT_STATE_OPEN, rec.State, "intent %s untouched", id)
	}
	require.True(t, f.bank.balanceOf(c.carol.String()).AmountOf(usdcDenom).IsZero(), "no coin moved")
	require.True(t, f.bank.balanceOf(c.bob.String()).AmountOf(svcDenom).IsZero(), "no svc minted")
	require.Equal(t, veilBondBefore, f.bank.balanceOf(veilModuleAddr).AmountOf(bondDenom), "no bond refunded")

	// the slash PERSISTS (parent ctx, outside the cache): 5% of the solver bond.
	solver, sErr := f.k.GetSolver(f.ctx, 0, solverAddr)
	require.NoError(t, sErr)
	expectSlash := minSolverBond().Amount.MulRaw(500).QuoRaw(10_000)
	require.Equal(t, minSolverBond().Amount.Sub(expectSlash), solver.Bond.AmountOf(bondDenom), "solver bond slashed 5%")
	require.Equal(t, uint64(1), solver.SlashedCount)
	require.Equal(t, expectSlash, f.bank.balanceOf(distrModuleAddr).AmountOf(bondDenom), "slash funded community pool")

	// the session fell through to FALLBACK (persists).
	session, err = f.k.GetSession(f.ctx, 0, c.sessionID)
	require.NoError(t, err)
	require.Equal(t, types.SESSION_STATE_FALLBACK, session.State)
}

// TestMsgConcludeAuthorityOnly proves MsgConclude is restricted to the module
// authority (CVM/testing entry point) and otherwise settles atomically.
func TestMsgConcludeAuthorityOnly(t *testing.T) {
	f := setupKeeper(t)
	c := f.setupCycle(t)
	srv := NewMsgServerImpl(f.k)

	_, err := srv.Conclude(f.ctx, &types.MsgConclude{
		Authority: randAddr().String(), SessionId: c.sessionID, Matches: c.matches(),
	})
	require.ErrorIs(t, err, types.ErrUnauthorized)

	_, err = srv.Conclude(f.ctx, &types.MsgConclude{
		Authority: f.k.GetAuthority(), SessionId: c.sessionID, Matches: c.matches(),
	})
	require.NoError(t, err)
	rec, err := f.veil.GetIntentRecord(f.ctx, 0, c.aliceID)
	require.NoError(t, err)
	require.Equal(t, veiltypes.INTENT_STATE_CONCLUDED, rec.State)
}

// hasEvent reports whether an event of the given type was emitted on ctx.
func hasEvent(ctx sdk.Context, eventType string) bool {
	for _, ev := range ctx.EventManager().Events() {
		if ev.Type == eventType {
			return true
		}
	}
	return false
}
