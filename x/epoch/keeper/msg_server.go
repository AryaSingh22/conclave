package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/epoch/types"
)

type msgServer struct {
	*Keeper
}

// NewMsgServerImpl returns the x/epoch MsgServer implementation.
func NewMsgServerImpl(k *Keeper) types.MsgServer { return &msgServer{Keeper: k} }

var _ types.MsgServer = msgServer{}

// authCommitteeMember verifies the signer is the FORMING-epoch committee member
// at dkgIndex: the bonded validator's operator address shares its 20-byte
// payload with an account address, so committee[dkgIndex].valoper recast as an
// account must equal the signer. This binds a DKG bundle to the validator whose
// index it claims (the on-chain transport's sender authorization).
func (k Keeper) authCommitteeMember(ctx sdk.Context, e types.Epoch, dkgIndex uint32, sender string) error {
	if int(dkgIndex) >= len(e.Committee) {
		return errorsmod.Wrapf(types.ErrNotCommittee, "dkg_index %d out of range", dkgIndex)
	}
	valAddr, err := sdk.ValAddressFromBech32(e.Committee[dkgIndex].Valoper)
	if err != nil {
		return err
	}
	signer, err := sdk.AccAddressFromBech32(sender)
	if err != nil {
		return err
	}
	if !sdk.AccAddress(valAddr).Equals(signer) {
		return errorsmod.Wrapf(types.ErrNotCommittee,
			"sender %s is not committee member %d (%s)", sender, dkgIndex, e.Committee[dkgIndex].Valoper)
	}
	return nil
}

func (m msgServer) SubmitDeal(goCtx context.Context, msg *types.MsgSubmitDeal) (*types.MsgSubmitDealResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	e, ok := m.GetEpoch(ctx, msg.EpochId)
	if !ok {
		return nil, types.ErrEpochNotFound
	}
	if err := m.authCommitteeMember(ctx, e, msg.DkgIndex, msg.Sender); err != nil {
		return nil, err
	}
	if err := m.CollectDeal(ctx, msg.EpochId, msg.DkgIndex, msg.Bundle); err != nil {
		return nil, err
	}
	return &types.MsgSubmitDealResponse{}, nil
}

func (m msgServer) SubmitResponse(goCtx context.Context, msg *types.MsgSubmitResponse) (*types.MsgSubmitResponseResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	e, ok := m.GetEpoch(ctx, msg.EpochId)
	if !ok {
		return nil, types.ErrEpochNotFound
	}
	if err := m.authCommitteeMember(ctx, e, msg.DkgIndex, msg.Sender); err != nil {
		return nil, err
	}
	if err := m.CollectResponse(ctx, msg.EpochId, msg.DkgIndex, msg.Bundle); err != nil {
		return nil, err
	}
	return &types.MsgSubmitResponseResponse{}, nil
}

func (m msgServer) ConfirmEpochKey(goCtx context.Context, msg *types.MsgConfirmEpochKey) (*types.MsgConfirmEpochKeyResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	e, ok := m.GetEpoch(ctx, msg.EpochId)
	if !ok {
		return nil, types.ErrEpochNotFound
	}
	if err := m.authCommitteeMember(ctx, e, msg.DkgIndex, msg.Sender); err != nil {
		return nil, err
	}
	if err := m.CollectConfirm(ctx, msg.EpochId, msg.DkgIndex, msg.EpochPubKey, msg.Commits); err != nil {
		return nil, err
	}
	return &types.MsgConfirmEpochKeyResponse{}, nil
}

func (m msgServer) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if msg.Authority != m.GetAuthority() {
		return nil, errorsmod.Wrapf(types.ErrUnauthorized,
			"expected authority %s, got %s", m.GetAuthority(), msg.Authority)
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	if err := m.SetParams(ctx, msg.Params); err != nil {
		return nil, err
	}
	return &types.MsgUpdateParamsResponse{}, nil
}
