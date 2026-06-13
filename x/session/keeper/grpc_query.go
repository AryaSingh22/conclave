package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/session/types"
)

type queryServer struct {
	*Keeper
}

// NewQueryServerImpl returns the x/session QueryServer implementation.
func NewQueryServerImpl(keeper *Keeper) types.QueryServer {
	return &queryServer{Keeper: keeper}
}

var _ types.QueryServer = queryServer{}

// Session returns a session by id (SPEC §6.4 / §7: solvers read open sessions).
func (q queryServer) Session(goCtx context.Context, req *types.QuerySessionRequest) (*types.QuerySessionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	s, err := q.GetSession(ctx, phase0Chamber, req.Id)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	return &types.QuerySessionResponse{Session: s}, nil
}

// CurrentSession returns the active session of a chamber.
func (q queryServer) CurrentSession(goCtx context.Context, req *types.QueryCurrentSessionRequest) (*types.QueryCurrentSessionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	s, err := q.Keeper.CurrentSession(ctx, req.ChamberId)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	return &types.QueryCurrentSessionResponse{Session: s}, nil
}

// Solver returns a solver registry entry.
func (q queryServer) Solver(goCtx context.Context, req *types.QuerySolverRequest) (*types.QuerySolverResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	addr, err := sdk.AccAddressFromBech32(req.Addr)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	s, err := q.GetSolver(ctx, phase0Chamber, addr)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	return &types.QuerySolverResponse{Solver: s}, nil
}

// Solvers returns all registered solvers.
func (q queryServer) Solvers(goCtx context.Context, req *types.QuerySolversRequest) (*types.QuerySolversResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	var solvers []types.Solver
	if err := q.IterateSolvers(ctx, func(s types.Solver) bool {
		solvers = append(solvers, s)
		return false
	}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QuerySolversResponse{Solvers: solvers}, nil
}

// Solutions returns the stored solutions of a session.
func (q queryServer) Solutions(goCtx context.Context, req *types.QuerySolutionsRequest) (*types.QuerySolutionsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	sols, err := q.SolutionsForSession(ctx, req.SessionId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QuerySolutionsResponse{Solutions: sols}, nil
}

// Params returns the module parameters.
func (q queryServer) Params(goCtx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	params, err := q.GetParams(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryParamsResponse{Params: params}, nil
}
