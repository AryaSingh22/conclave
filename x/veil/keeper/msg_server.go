package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/veil/types"
)

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns the x/veil MsgServer implementation. Routing
// these msg servers is what resolves D-024: MsgSubmitIntent transits real
// blocks from task 0.7 on.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

var _ types.MsgServer = msgServer{}

// SubmitIntent implements SPEC §6.3: "validates envelope, escrows bond,
// assigns id, stores SUBMITTED". Mandate enforcement happened in the ante
// handler before this executes (SPEC §6.2).
func (m msgServer) SubmitIntent(goCtx context.Context, msg *types.MsgSubmitIntent) (*types.MsgSubmitIntentResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	id, err := m.Keeper.SubmitIntent(ctx, msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgSubmitIntentResponse{IntentId: id}, nil
}

// CancelIntent implements SPEC §6.3 MsgCancelIntent (owner; only pre-OPEN
// states; bond refunded).
func (m msgServer) CancelIntent(goCtx context.Context, msg *types.MsgCancelIntent) (*types.MsgCancelIntentResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if _, err := sdk.AccAddressFromBech32(msg.Sender); err != nil {
		return nil, err
	}
	if err := m.Keeper.CancelIntent(ctx, msg.Sender, msg.IntentId); err != nil {
		return nil, err
	}
	return &types.MsgCancelIntentResponse{}, nil
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
