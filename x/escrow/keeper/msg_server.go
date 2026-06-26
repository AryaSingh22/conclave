package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/escrow/types"
)

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns the x/escrow MsgServer implementation.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

var _ types.MsgServer = msgServer{}

// ReleaseEscrow implements SPEC §6.5 MsgReleaseEscrow. It dispatches on the
// escrow's condition class: HASHLOCK releases on a valid pre-timeout preimage
// (ReleaseHashlock); OPTIMISTIC releases a DISPUTED escrow when the seller
// reveals the content preimage (RevealOptimistic). Release is knowledge-gated
// (any signer with a valid preimage authorizes it — standard HTLC, D-073), so
// the signer need only be well-formed. ZK_DELIVERY does NOT release via this
// preimage path — it releases only via MsgSubmitDeliveryProof — so a
// MsgReleaseEscrow against a ZK escrow is a wrong-class reject.
func (m msgServer) ReleaseEscrow(goCtx context.Context, msg *types.MsgReleaseEscrow) (*types.MsgReleaseEscrowResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return nil, err
	}
	esc, err := m.Keeper.GetEscrow(ctx, phase0Chamber, msg.EscrowId)
	if err != nil {
		return nil, err
	}
	switch classOf(esc) {
	case classHashlock:
		if err := m.Keeper.ReleaseHashlock(ctx, esc, msg.Preimage); err != nil {
			return nil, err
		}
	case classOptimistic:
		if err := m.Keeper.RevealOptimistic(ctx, esc, msg.Preimage); err != nil {
			return nil, err
		}
	default:
		// classZk (releases via MsgSubmitDeliveryProof) and classNone both reject.
		return nil, types.ErrWrongClass
	}
	return &types.MsgReleaseEscrowResponse{}, nil
}

// RaiseDispute implements SPEC §6.5 MsgRaiseDispute (OPTIMISTIC only). Only the
// buyer (payer) may dispute — a dispute is a claim of non-delivery by the party
// who paid — so the signer must be the payer's controller address.
func (m msgServer) RaiseDispute(goCtx context.Context, msg *types.MsgRaiseDispute) (*types.MsgRaiseDisputeResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	signer, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		return nil, err
	}
	esc, err := m.Keeper.GetEscrow(ctx, phase0Chamber, msg.EscrowId)
	if err != nil {
		return nil, err
	}
	if classOf(esc) != classOptimistic {
		return nil, types.ErrNoDisputePath
	}
	payer, err := m.Keeper.sigilKeeper.ControllerAddress(ctx, esc.ChamberId, esc.PayerSigil)
	if err != nil {
		return nil, err
	}
	if !payer.Equals(signer) {
		return nil, types.ErrUnauthorized
	}
	if err := m.Keeper.RaiseDispute(ctx, esc); err != nil {
		return nil, err
	}
	return &types.MsgRaiseDisputeResponse{}, nil
}

// SubmitDeliveryProof implements SPEC §6.5 MsgSubmitDeliveryProof
// (ZK_DELIVERY, task 2.1b). It releases a ZK escrow when the submitted groth16
// proof verifies under the escrow's signed vk_id over its schema_commitment and
// escrow id (the instance binding — ReleaseZk reconstructs the public witness
// from the record). Release is knowledge-gated like HASHLOCK: any well-formed
// signer presenting a verifying proof authorizes it. A malformed/invalid/
// replayed proof rejects (ErrInvalidProof) and never panics; a non-ZK escrow is
// a wrong-class reject.
func (m msgServer) SubmitDeliveryProof(goCtx context.Context, msg *types.MsgSubmitDeliveryProof) (*types.MsgSubmitDeliveryProofResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return nil, err
	}
	esc, err := m.Keeper.GetEscrow(ctx, phase0Chamber, msg.EscrowId)
	if err != nil {
		return nil, err
	}
	if classOf(esc) != classZk {
		return nil, types.ErrWrongClass
	}
	if err := m.Keeper.ReleaseZk(ctx, esc, msg.Proof); err != nil {
		return nil, err
	}
	return &types.MsgSubmitDeliveryProofResponse{}, nil
}
