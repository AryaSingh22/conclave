package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/AryaSingh22/conclave/x/mandate/types"
)

type queryServer struct {
	Keeper
}

// NewQueryServerImpl returns the x/mandate QueryServer implementation.
func NewQueryServerImpl(keeper Keeper) types.QueryServer {
	return &queryServer{Keeper: keeper}
}

var _ types.QueryServer = queryServer{}

// Mandate implements the SPEC §6.2 mandate-by-id lookup.
func (q queryServer) Mandate(goCtx context.Context, req *types.QueryMandateRequest) (*types.QueryMandateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	m, err := q.MandateByID(ctx, req.Id)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	return &types.QueryMandateResponse{Mandate: m}, nil
}

// MandateByGrantee implements the SPEC §6.2 O(1) ante lookup index (0x02).
func (q queryServer) MandateByGrantee(goCtx context.Context, req *types.QueryMandateByGranteeRequest) (*types.QueryMandateByGranteeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	grantee, err := sdk.AccAddressFromBech32(req.Grantee)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	m, found, err := q.ActiveMandateByGrantee(ctx, phase0Chamber, grantee)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !found {
		return nil, status.Errorf(codes.NotFound, "no mandate for grantee %s", req.Grantee)
	}
	return &types.QueryMandateByGranteeResponse{Mandate: m}, nil
}

// Mandates implements the SPEC §6.2 per-sigil mandate listing (0x03 index).
func (q queryServer) Mandates(goCtx context.Context, req *types.QueryMandatesRequest) (*types.QueryMandatesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)

	mandates, err := q.MandatesBySigil(ctx, phase0Chamber, req.SigilId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// offset/limit pagination over the collected set; the per-sigil mandate
	// count is small by construction (one per delegate key)
	offset, limit := uint64(0), uint64(query.DefaultLimit)
	if req.Pagination != nil {
		offset = req.Pagination.Offset
		if req.Pagination.Limit > 0 {
			limit = req.Pagination.Limit
		}
	}
	if offset > uint64(len(mandates)) {
		offset = uint64(len(mandates))
	}
	end := offset + limit
	if end > uint64(len(mandates)) {
		end = uint64(len(mandates))
	}
	return &types.QueryMandatesResponse{
		Mandates:   mandates[offset:end],
		Pagination: &query.PageResponse{Total: uint64(len(mandates))},
	}, nil
}

// Params returns the module parameters.
func (q queryServer) Params(goCtx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	params, err := q.GetParams(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryParamsResponse{Params: params}, nil
}
