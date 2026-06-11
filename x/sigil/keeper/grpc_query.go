package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/AryaSingh22/conclave/x/sigil/types"
)

type queryServer struct {
	Keeper
}

// NewQueryServerImpl returns the x/sigil QueryServer implementation.
func NewQueryServerImpl(keeper Keeper) types.QueryServer {
	return &queryServer{Keeper: keeper}
}

var _ types.QueryServer = queryServer{}

// Sigil implements SPEC §6.1 sigil lookup.
func (q queryServer) Sigil(goCtx context.Context, req *types.QuerySigilRequest) (*types.QuerySigilResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	sigil, err := q.GetSigil(ctx, 0, req.Id)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	return &types.QuerySigilResponse{Sigil: sigil}, nil
}

// SigilByAddress implements the SPEC §6.1 reverse index lookup (key 0x04).
func (q queryServer) SigilByAddress(goCtx context.Context, req *types.QuerySigilByAddressRequest) (*types.QuerySigilByAddressResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	addr, err := sdk.AccAddressFromBech32(req.Address)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	sigilID, err := q.SigilIDByAddress(ctx, 0, addr)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	sigil, err := q.GetSigil(ctx, 0, sigilID)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	return &types.QuerySigilByAddressResponse{Sigil: sigil}, nil
}

// Credentials implements SPEC §6.1 credential listing.
func (q queryServer) Credentials(goCtx context.Context, req *types.QueryCredentialsRequest) (*types.QueryCredentialsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)

	var creds []types.CredentialAnchor
	if err := q.IterateCredentials(ctx, 0, req.SigilId, func(c types.CredentialAnchor) bool {
		creds = append(creds, c)
		return false
	}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// offset/limit pagination over the collected set; the per-sigil
	// credential count is small by construction
	offset, limit := uint64(0), uint64(query.DefaultLimit)
	if req.Pagination != nil {
		offset = req.Pagination.Offset
		if req.Pagination.Limit > 0 {
			limit = req.Pagination.Limit
		}
	}
	if offset > uint64(len(creds)) {
		offset = uint64(len(creds))
	}
	end := offset + limit
	if end > uint64(len(creds)) {
		end = uint64(len(creds))
	}
	return &types.QueryCredentialsResponse{
		Credentials: creds[offset:end],
		Pagination:  &query.PageResponse{Total: uint64(len(creds))},
	}, nil
}

// Reputation implements SPEC §6.1 reputation lookup (decay applied to the
// returned value, state untouched — D-013).
func (q queryServer) Reputation(goCtx context.Context, req *types.QueryReputationRequest) (*types.QueryReputationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	rep, err := q.GetReputation(ctx, 0, req.SigilId)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	return &types.QueryReputationResponse{Reputation: rep}, nil
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
