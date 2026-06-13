package keeper

import (
	"context"
	"crypto/sha256"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/session/types"
)

type msgServer struct {
	*Keeper
}

// NewMsgServerImpl returns the x/session MsgServer implementation.
func NewMsgServerImpl(keeper *Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

var _ types.MsgServer = msgServer{}

// RegisterSolver implements SPEC §6.4 MsgRegisterSolver.
func (m msgServer) RegisterSolver(goCtx context.Context, msg *types.MsgRegisterSolver) (*types.MsgRegisterSolverResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	addr, err := sdk.AccAddressFromBech32(msg.Solver)
	if err != nil {
		return nil, err
	}
	if err := m.Keeper.RegisterSolver(ctx, addr, msg.Bond); err != nil {
		return nil, err
	}
	return &types.MsgRegisterSolverResponse{}, nil
}

// UnbondSolver implements SPEC §6.4 MsgUnbondSolver.
func (m msgServer) UnbondSolver(goCtx context.Context, msg *types.MsgUnbondSolver) (*types.MsgUnbondSolverResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	addr, err := sdk.AccAddressFromBech32(msg.Solver)
	if err != nil {
		return nil, err
	}
	completion, err := m.Keeper.UnbondSolver(ctx, addr)
	if err != nil {
		return nil, err
	}
	return &types.MsgUnbondSolverResponse{CompletionHeight: completion}, nil
}

// ProposeSolution implements SPEC §6.4 MsgProposeSolution. The solution id is
// the tx hash (ties in scoring break by lower id — SPEC §6.4). An invalid
// solution or equivocation slashes the solver and returns an error; the slash
// commits with the failing tx.
func (m msgServer) ProposeSolution(goCtx context.Context, msg *types.MsgProposeSolution) (*types.MsgProposeSolutionResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	addr, err := sdk.AccAddressFromBech32(msg.Solver)
	if err != nil {
		return nil, err
	}
	txHash := sha256.Sum256(ctx.TxBytes())
	solutionID := types.IDHex(txHash[:])
	if err := m.Keeper.ProposeSolution(ctx, addr, msg.SessionId, msg.Matches, solutionID); err != nil {
		return nil, err
	}
	return &types.MsgProposeSolutionResponse{SolutionId: solutionID}, nil
}

// Conclude implements SPEC §6.4 MsgConclude: the authority-restricted atomic
// settlement entry point (for CVM bindings and testing). Normal settlement
// runs internally in EndBlock (Half B); this path applies a graph directly.
func (m msgServer) Conclude(goCtx context.Context, msg *types.MsgConclude) (*types.MsgConcludeResponse, error) {
	if msg.Authority != m.authority {
		return nil, errorsmod.Wrapf(types.ErrUnauthorized,
			"expected authority %s, got %s", m.authority, msg.Authority)
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	if err := m.Keeper.Conclude(ctx, msg.SessionId, msg.Matches); err != nil {
		return nil, err
	}
	return &types.MsgConcludeResponse{}, nil
}

// UpdateParams implements the gov-authority param update (D-008).
func (m msgServer) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if msg.Authority != m.authority {
		return nil, errorsmod.Wrapf(types.ErrUnauthorized,
			"expected authority %s, got %s", m.authority, msg.Authority)
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	if err := m.SetParams(ctx, msg.Params); err != nil {
		return nil, err
	}
	return &types.MsgUpdateParamsResponse{}, nil
}
