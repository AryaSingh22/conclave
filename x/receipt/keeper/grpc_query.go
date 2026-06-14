package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	storetypes "cosmossdk.io/store/types"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/receipt/types"
)

type queryServer struct {
	Keeper
}

// NewQueryServerImpl returns the x/receipt QueryServer (SPEC §6.6).
func NewQueryServerImpl(k Keeper) types.QueryServer { return queryServer{Keeper: k} }

var _ types.QueryServer = queryServer{}

// Receipts returns a sigil's receipts over the §6.6 per-agent chronological
// index (0x07), paginated. Reads cross-module from the x/session store via the
// SessionReceiptReader boundary (D-045/D-052).
func (q queryServer) Receipts(goCtx context.Context, req *types.QueryReceiptsRequest) (*types.QueryReceiptsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if req.SigilId == "" {
		return nil, status.Error(codes.InvalidArgument, "sigil_id is required")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	receipts, pageRes, err := q.session.GetReceiptsBySigil(ctx, req.SigilId, req.Pagination)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryReceiptsResponse{Receipts: receipts, Pagination: pageRes}, nil
}

// Receipt returns a single receipt by id over the §6.6 secondary index (0x08).
func (q queryServer) Receipt(goCtx context.Context, req *types.QueryReceiptRequest) (*types.QueryReceiptResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	rec, found, err := q.session.GetReceiptByID(ctx, req.Id)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if !found {
		return nil, status.Errorf(codes.NotFound, "receipt %q not found", req.Id)
	}
	return &types.QueryReceiptResponse{Receipt: rec}, nil
}

// ReceiptProof returns a receipt with an ICS-23 existence proof of its 0x08 key
// against the session store's IAVL root, chained to the app hash (SPEC §6.6,
// D-053). The proof is bound to ProofHeight: a verifier MUST check it against
// the app root at that height. The committed multistore's Query appends the
// store-level commit op, so the returned proof verifies value → session IAVL
// root → app hash via ibc ConvertProofs / VerifyMembership (GetSDKSpecs).
func (q queryServer) ReceiptProof(goCtx context.Context, req *types.QueryReceiptProofRequest) (*types.QueryReceiptProofResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)

	rec, found, err := q.session.GetReceiptByID(ctx, req.Id)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if !found {
		return nil, status.Errorf(codes.NotFound, "receipt %q not found", req.Id)
	}

	key, err := q.session.ReceiptByIDStoreKey(req.Id)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Prove the 0x08 secondary key against the committed session IAVL root. The
	// "/key" subpath is the iavl store's proof convention; rootmulti appends the
	// multistore commit op so ProofOps chains to the app hash.
	res, err := q.proofs.Query(&storetypes.RequestQuery{
		Path:  "/" + q.session.ReceiptStoreName() + "/key",
		Data:  key,
		Prove: true,
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if res.ProofOps == nil || len(res.ProofOps.Ops) == 0 {
		return nil, status.Error(codes.Internal, "store returned no proof (height may be pruned)")
	}

	proofBz, err := res.ProofOps.Marshal()
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryReceiptProofResponse{
		Receipt:     rec,
		Proof:       proofBz,
		ProofHeight: uint64(res.Height),
	}, nil
}
