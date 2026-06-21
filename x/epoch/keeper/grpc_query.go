package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/epoch/types"
)

type queryServer struct {
	Keeper
}

// NewQueryServerImpl returns the x/epoch QueryServer implementation.
func NewQueryServerImpl(k *Keeper) types.QueryServer { return queryServer{Keeper: *k} }

var _ types.QueryServer = queryServer{}

// CurrentEpoch returns the ACTIVE epoch — the encryption target clients fetch to
// Encrypt (PHASE1_DESIGN §2 perf FLAG: EpochPubKey distribution). When no epoch
// is active yet (the 1.6 bootstrap seam) it returns an epoch with id 0.
func (q queryServer) CurrentEpoch(goCtx context.Context, _ *types.QueryCurrentEpochRequest) (*types.QueryCurrentEpochResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	id := q.CurrentEpochID(ctx)
	if id == 0 {
		return &types.QueryCurrentEpochResponse{Epoch: types.Epoch{}}, nil
	}
	e, ok := q.GetEpoch(ctx, id)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "current epoch %d not found", id)
	}
	return &types.QueryCurrentEpochResponse{Epoch: e}, nil
}

// Epoch returns an epoch record by id.
func (q queryServer) Epoch(goCtx context.Context, req *types.QueryEpochRequest) (*types.QueryEpochResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	e, ok := q.GetEpoch(ctx, req.Id)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "epoch %d not found", req.Id)
	}
	return &types.QueryEpochResponse{Epoch: e}, nil
}

// Params returns the module parameters.
func (q queryServer) Params(goCtx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	p, err := q.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	return &types.QueryParamsResponse{Params: p}, nil
}

// FormingEpoch returns the open FORMING epoch the off-chain DKG daemon drives
// (id 0 when none is forming) — Task 1.7, D-070.
func (q queryServer) FormingEpoch(goCtx context.Context, _ *types.QueryFormingEpochRequest) (*types.QueryFormingEpochResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	id := q.FormingEpochID(ctx)
	if id == 0 {
		return &types.QueryFormingEpochResponse{Epoch: types.Epoch{}}, nil
	}
	e, ok := q.GetEpoch(ctx, id)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "forming epoch %d not found", id)
	}
	return &types.QueryFormingEpochResponse{Epoch: e}, nil
}

// Registrations returns the long-term DKG keys registered for an epoch (sorted
// by valoper), so the daemon can build the DKG roster — Task 1.7 A2.
func (q queryServer) Registrations(goCtx context.Context, req *types.QueryRegistrationsRequest) (*types.QueryRegistrationsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	return &types.QueryRegistrationsResponse{Registrations: q.Keeper.Registrations(ctx, req.EpochId)}, nil
}

// DKGTranscript returns one round's collected opaque bundles (0=deal 1=response
// 2=justify) so the daemon can drive the next round — Task 1.7, D-070.
func (q queryServer) DKGTranscript(goCtx context.Context, req *types.QueryDKGTranscriptRequest) (*types.QueryDKGTranscriptResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	return &types.QueryDKGTranscriptResponse{Bundles: q.transcriptBundles(ctx, req.EpochId, req.Round)}, nil
}
