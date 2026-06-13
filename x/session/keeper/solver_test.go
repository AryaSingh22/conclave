package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/session/types"
)

func TestRegisterSolver(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		f := setupKeeper(t)
		solver := randAddr()
		f.bank.fund(solver.String(), sdk.NewCoins(minSolverBond()))
		require.NoError(t, f.k.RegisterSolver(f.ctx, solver, sdk.NewCoins(minSolverBond())))

		s, err := f.k.GetSolver(f.ctx, 0, solver)
		require.NoError(t, err)
		require.Equal(t, types.SOLVER_STATUS_ACTIVE, s.Status)
		require.Equal(t, minSolverBond().Amount, s.Bond.AmountOf(bondDenom))
		// bond escrowed in the session module account.
		require.Equal(t, minSolverBond().Amount, f.bank.balanceOf(sessionModuleAddr).AmountOf(bondDenom))
		require.True(t, s.Bond.IsAllPositive())
	})

	t.Run("below minimum", func(t *testing.T) {
		f := setupKeeper(t)
		solver := randAddr()
		small := sdk.NewInt64Coin(bondDenom, 1)
		f.bank.fund(solver.String(), sdk.NewCoins(small))
		require.ErrorIs(t, f.k.RegisterSolver(f.ctx, solver, sdk.NewCoins(small)), types.ErrBondInsufficient)
	})

	t.Run("double register", func(t *testing.T) {
		f := setupKeeper(t)
		solver := randAddr()
		f.bank.fund(solver.String(), sdk.NewCoins(sdk.NewInt64Coin(bondDenom, 200_000_000)))
		require.NoError(t, f.k.RegisterSolver(f.ctx, solver, sdk.NewCoins(minSolverBond())))
		require.ErrorIs(t, f.k.RegisterSolver(f.ctx, solver, sdk.NewCoins(minSolverBond())), types.ErrSolverExists)
	})

	t.Run("insufficient funds", func(t *testing.T) {
		f := setupKeeper(t)
		solver := randAddr() // unfunded
		require.Error(t, f.k.RegisterSolver(f.ctx, solver, sdk.NewCoins(minSolverBond())))
	})
}

func TestUnbondSolver(t *testing.T) {
	f := setupKeeper(t)
	solver := randAddr()
	f.registerSolver(t, solver, minSolverBond())

	completion, err := f.k.UnbondSolver(f.ctx, solver)
	require.NoError(t, err)
	params := types.DefaultParams()
	require.Equal(t, uint64(f.ctx.BlockHeight())+params.SolverUnbondBlocks, completion)

	s, err := f.k.GetSolver(f.ctx, 0, solver)
	require.NoError(t, err)
	require.Equal(t, types.SOLVER_STATUS_UNBONDING, s.Status)
	require.Equal(t, completion, s.UnbondHeight)

	// unbonding a non-active solver fails.
	require.ErrorIs(t, func() error { _, e := f.k.UnbondSolver(f.ctx, solver); return e }(), types.ErrSolverNotActive)

	// sweep before completion: still held.
	f.ctx = f.ctx.WithBlockHeight(int64(completion) - 1)
	require.NoError(t, f.k.SweepUnbondedSolvers(f.ctx))
	_, err = f.k.GetSolver(f.ctx, 0, solver)
	require.NoError(t, err, "still unbonding before completion")

	// sweep at completion: bond returns, record deleted.
	f.ctx = f.ctx.WithBlockHeight(int64(completion))
	require.NoError(t, f.k.SweepUnbondedSolvers(f.ctx))
	_, err = f.k.GetSolver(f.ctx, 0, solver)
	require.ErrorIs(t, err, types.ErrSolverNotFound)
	require.Equal(t, minSolverBond().Amount, f.bank.balanceOf(solver.String()).AmountOf(bondDenom), "bond returned")
	require.True(t, f.bank.balanceOf(sessionModuleAddr).AmountOf(bondDenom).IsZero())
}

func TestUnbondUnknownSolver(t *testing.T) {
	f := setupKeeper(t)
	_, err := f.k.UnbondSolver(f.ctx, randAddr())
	require.ErrorIs(t, err, types.ErrSolverNotFound)
}
