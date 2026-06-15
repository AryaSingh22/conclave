package simulation

import (
	"math/rand"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"github.com/AryaSingh22/conclave/simcoverage"
	"github.com/AryaSingh22/conclave/simutil"
	refsolver "github.com/AryaSingh22/conclave/solver"
	"github.com/AryaSingh22/conclave/solver/match"
	"github.com/AryaSingh22/conclave/x/session/keeper"
	"github.com/AryaSingh22/conclave/x/session/types"
	veilkeeper "github.com/AryaSingh22/conclave/x/veil/keeper"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

const (
	weightRegisterSolver  = 12
	weightUnbondSolver    = 4
	weightProposeSolution = 40
)

// WeightedOperations returns the x/session simulation operations.
func WeightedOperations(txGen client.TxConfig, ak simutil.AccountKeeper, k *keeper.Keeper, vk veilkeeper.Keeper) simulation.WeightedOperations {
	return simulation.WeightedOperations{
		simulation.NewWeightedOperation(weightRegisterSolver, SimulateMsgRegisterSolver(txGen, ak, k)),
		simulation.NewWeightedOperation(weightUnbondSolver, SimulateMsgUnbondSolver(txGen, ak, k)),
		simulation.NewWeightedOperation(weightProposeSolution, SimulateMsgProposeSolution(txGen, ak, k, vk)),
	}
}

// SimulateMsgRegisterSolver registers a fresh sim account as a bonded solver.
func SimulateMsgRegisterSolver(txGen client.TxConfig, ak simutil.AccountKeeper, k *keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		acc, _ := simtypes.RandomAcc(r, accs)
		if _, err := k.GetSolver(ctx, 0, acc.Address); err == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterSolver{}), "already a solver"), nil, nil
		}
		params, err := k.GetParams(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterSolver{}), "params"), nil, err
		}
		// bond comfortably above the minimum, in the min-bond denom.
		bond := sdk.NewCoins(sdk.NewCoin(params.SolverMinBond.Denom,
			params.SolverMinBond.Amount.MulRaw(2).Add(math.NewInt(int64(r.Intn(1_000_000))))))
		msg := &types.MsgRegisterSolver{Solver: acc.Address.String(), Bond: bond}
		ok, err := simutil.Deliver(r, app, txGen, ctx, ak, msg, acc)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "deliver"), nil, err
		}
		if ok {
			simcoverage.Inc("session/register_solver")
		}
		return simtypes.NewOperationMsg(msg, ok, ""), nil, nil
	}
}

type controlledSolver struct {
	addr sdk.AccAddress
	acc  simtypes.Account
}

func activeSolvers(ctx sdk.Context, k *keeper.Keeper, accs []simtypes.Account) []controlledSolver {
	var out []controlledSolver
	_ = k.IterateSolvers(ctx, func(s types.Solver) bool {
		if s.Status != types.SOLVER_STATUS_ACTIVE {
			return false
		}
		addr, err := sdk.AccAddressFromBech32(s.Addr)
		if err != nil {
			return false
		}
		if acc, ok := simutil.FindAccount(accs, addr); ok {
			out = append(out, controlledSolver{addr: addr, acc: acc})
		}
		return false
	})
	return out
}

// SimulateMsgUnbondSolver starts unbonding for a random active solver.
func SimulateMsgUnbondSolver(txGen client.TxConfig, ak simutil.AccountKeeper, k *keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		solvers := activeSolvers(ctx, k, accs)
		if len(solvers) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnbondSolver{}), "no active solvers"), nil, nil
		}
		s := solvers[r.Intn(len(solvers))]
		msg := &types.MsgUnbondSolver{Solver: s.addr.String()}
		ok, err := simutil.Deliver(r, app, txGen, ctx, ak, msg, s.acc)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "deliver"), nil, err
		}
		if ok {
			simcoverage.Inc("session/unbond_solver")
		}
		return simtypes.NewOperationMsg(msg, ok, ""), nil, nil
	}
}

// SimulateMsgProposeSolution finds a live AUCTION session, decodes its OPEN
// intents, and runs the reference solver to compute valid matches; a bonded
// active solver then submits them. This is the path that clears 3-party cycles
// (the fallback cannot) and exercises scoring → Conclude → receipts → the
// reputation hook.
func SimulateMsgProposeSolution(txGen client.TxConfig, ak simutil.AccountKeeper, k *keeper.Keeper, vk veilkeeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		solvers := activeSolvers(ctx, k, accs)
		if len(solvers) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgProposeSolution{}), "no active solvers"), nil, nil
		}

		// collect AUCTION sessions
		var auctions []types.Session
		_ = k.IterateSessions(ctx, func(s types.Session) bool {
			if s.State == types.SESSION_STATE_AUCTION {
				auctions = append(auctions, s)
			}
			return false
		})
		if len(auctions) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgProposeSolution{}), "no auction session"), nil, nil
		}
		session := auctions[r.Intn(len(auctions))]

		// decode the session's OPEN intents into solver inputs
		var open []match.OpenIntent
		for _, id := range session.IntentIds {
			rec, err := vk.GetIntentRecord(ctx, 0, id)
			if err != nil || rec.State != veiltypes.INTENT_STATE_OPEN {
				continue
			}
			if oi, ok := match.DecodeOpenIntent(rec); ok {
				open = append(open, oi)
			}
		}
		if len(open) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgProposeSolution{}), "no open intents"), nil, nil
		}

		matches := refsolver.Solve(open)
		if len(matches) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgProposeSolution{}), "solver found nothing"), nil, nil
		}

		solver := solvers[r.Intn(len(solvers))]
		msg := &types.MsgProposeSolution{
			Solver:    solver.addr.String(),
			SessionId: session.Id,
			Matches:   matches,
		}
		ok, err := simutil.Deliver(r, app, txGen, ctx, ak, msg, solver.acc)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "deliver"), nil, err
		}
		if ok {
			simcoverage.Inc("session/propose_solution")
		}
		return simtypes.NewOperationMsg(msg, ok, ""), nil, nil
	}
}
