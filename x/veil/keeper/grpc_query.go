package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/store/prefix"

	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/AryaSingh22/conclave/x/veil/types"
)

type queryServer struct {
	Keeper
}

// NewQueryServerImpl returns the x/veil QueryServer implementation.
func NewQueryServerImpl(keeper Keeper) types.QueryServer {
	return &queryServer{Keeper: keeper}
}

var _ types.QueryServer = queryServer{}

// Intent implements the SPEC §6.3 intent lookup.
func (q queryServer) Intent(goCtx context.Context, req *types.QueryIntentRequest) (*types.QueryIntentResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	rec, err := q.GetIntentRecord(ctx, phase0Chamber, req.Id)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	return &types.QueryIntentResponse{Record: rec}, nil
}

// Intents implements the SPEC §6.3 state-filtered listing (solvers read
// the OPEN set of the current session through this).
func (q queryServer) Intents(goCtx context.Context, req *types.QueryIntentsRequest) (*types.QueryIntentsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)

	store := runtime.KVStoreAdapter(q.storeService.OpenKVStore(ctx))
	recordStore := prefix.NewStore(store, types.IntentKeyPrefix)

	var records []types.IntentRecord
	pageRes, err := query.FilteredPaginate(recordStore, req.Pagination,
		func(_ []byte, value []byte, accumulate bool) (bool, error) {
			var rec types.IntentRecord
			if err := q.cdc.Unmarshal(value, &rec); err != nil {
				return false, err
			}
			if req.State != types.INTENT_STATE_UNSPECIFIED && rec.State != req.State {
				return false, nil
			}
			if accumulate {
				records = append(records, rec)
			}
			return true, nil
		})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryIntentsResponse{Records: records, Pagination: pageRes}, nil
}

// Params implements the SPEC §6.3 params query.
func (q queryServer) Params(goCtx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	params, err := q.GetParams(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryParamsResponse{Params: params}, nil
}
