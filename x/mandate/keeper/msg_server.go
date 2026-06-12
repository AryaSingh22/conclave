package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/AryaSingh22/conclave/x/mandate/types"
)

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns the x/mandate MsgServer implementation (SPEC §6.2).
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

var _ types.MsgServer = msgServer{}

// IssueMandate implements SPEC §6.2 MsgIssueMandate.
func (m msgServer) IssueMandate(goCtx context.Context, msg *types.MsgIssueMandate) (*types.MsgIssueMandateResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	controller, err := sdk.AccAddressFromBech32(msg.Controller)
	if err != nil {
		return nil, errorsmod.Wrap(err, "controller")
	}
	id, err := m.Keeper.IssueMandate(ctx, controller, msg.Terms)
	if err != nil {
		return nil, err
	}
	return &types.MsgIssueMandateResponse{MandateId: id}, nil
}

// RevokeMandate implements SPEC §6.2 MsgRevokeMandate.
func (m msgServer) RevokeMandate(goCtx context.Context, msg *types.MsgRevokeMandate) (*types.MsgRevokeMandateResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	controller, err := sdk.AccAddressFromBech32(msg.Controller)
	if err != nil {
		return nil, errorsmod.Wrap(err, "controller")
	}
	if err := m.Keeper.RevokeMandate(ctx, controller, msg.MandateId); err != nil {
		return nil, err
	}
	return &types.MsgRevokeMandateResponse{}, nil
}

// AmendMandate implements SPEC §6.2 MsgAmendMandate.
func (m msgServer) AmendMandate(goCtx context.Context, msg *types.MsgAmendMandate) (*types.MsgAmendMandateResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	controller, err := sdk.AccAddressFromBech32(msg.Controller)
	if err != nil {
		return nil, errorsmod.Wrap(err, "controller")
	}
	if err := m.Keeper.AmendMandate(ctx, controller, msg.MandateId, msg.Terms); err != nil {
		return nil, err
	}
	return &types.MsgAmendMandateResponse{}, nil
}

// UpdateParams implements the governance param update.
func (m msgServer) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if m.GetAuthority() != msg.Authority {
		return nil, errorsmod.Wrapf(govtypes.ErrInvalidSigner,
			"invalid authority: expected %s, got %s", m.GetAuthority(), msg.Authority)
	}
	if err := m.SetParams(ctx, msg.Params); err != nil {
		return nil, err
	}
	return &types.MsgUpdateParamsResponse{}, nil
}
