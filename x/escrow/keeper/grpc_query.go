package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/escrow/types"
)

type queryServer struct {
	Keeper
}

// NewQueryServerImpl returns the x/escrow QueryServer implementation.
func NewQueryServerImpl(keeper Keeper) types.QueryServer {
	return &queryServer{Keeper: keeper}
}

var _ types.QueryServer = queryServer{}

// Escrow returns an escrow by id (SPEC §6.5).
func (q queryServer) Escrow(goCtx context.Context, req *types.QueryEscrowRequest) (*types.QueryEscrowResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	esc, err := q.Keeper.GetEscrow(ctx, phase0Chamber, req.Id)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	return &types.QueryEscrowResponse{Escrow: esc}, nil
}

// Escrows returns the escrows a sigil participates in (as payer or payee),
// read off the 0x02 participant index (SPEC §6.5).
func (q queryServer) Escrows(goCtx context.Context, req *types.QueryEscrowsRequest) (*types.QueryEscrowsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if req.SigilId == "" {
		return nil, status.Error(codes.InvalidArgument, "empty sigil_id")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	prefix := types.SigilIdxPrefixKey(req.SigilId)
	it, err := q.storeService.OpenKVStore(ctx).Iterator(prefix, storePrefixEnd(prefix))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer it.Close()

	var escrows []types.Escrow
	for ; it.Valid(); it.Next() {
		key := it.Key()
		idBz := key[len(prefix):]
		esc, err := q.Keeper.GetEscrow(ctx, phase0Chamber, types.IDHex(idBz))
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		escrows = append(escrows, esc)
	}
	return &types.QueryEscrowsResponse{Escrows: escrows}, nil
}
