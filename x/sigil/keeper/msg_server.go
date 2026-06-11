package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/AryaSingh22/conclave/x/sigil/types"
)

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns the x/sigil MsgServer implementation (SPEC §6.1).
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

var _ types.MsgServer = msgServer{}

// RegisterSigil implements SPEC §6.1 MsgRegisterSigil.
func (m msgServer) RegisterSigil(goCtx context.Context, msg *types.MsgRegisterSigil) (*types.MsgRegisterSigilResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	controller, err := sdk.AccAddressFromBech32(msg.Controller)
	if err != nil {
		return nil, errorsmod.Wrap(err, "controller")
	}

	id, err := m.Keeper.RegisterSigil(ctx, controller, msg.DidDocHash, msg.MetadataUri,
		msg.RecoverySet, msg.RecoveryThreshold, msg.ChamberId)
	if err != nil {
		return nil, err
	}
	return &types.MsgRegisterSigilResponse{SigilId: id}, nil
}

// RotateController implements SPEC §6.1 MsgRotateController.
func (m msgServer) RotateController(goCtx context.Context, msg *types.MsgRotateController) (*types.MsgRotateControllerResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if len(msg.Signers) == 0 {
		return nil, errorsmod.Wrap(types.ErrUnauthorized, "no signers")
	}
	signers := make([]sdk.AccAddress, len(msg.Signers))
	for i, s := range msg.Signers {
		addr, err := sdk.AccAddressFromBech32(s)
		if err != nil {
			return nil, errorsmod.Wrapf(err, "signer %d", i)
		}
		signers[i] = addr
	}
	newController, err := sdk.AccAddressFromBech32(msg.NewController)
	if err != nil {
		return nil, errorsmod.Wrap(err, "new_controller")
	}

	// chamber is resolved from the sigil itself; chamber 0 in Phase 0
	sigil, err := m.findSigilAnyChamber(ctx, msg.SigilId)
	if err != nil {
		return nil, err
	}

	if err := m.Keeper.RotateController(ctx, sigil.ChamberId, msg.SigilId, signers, newController); err != nil {
		return nil, err
	}
	return &types.MsgRotateControllerResponse{}, nil
}

// AnchorCredential implements SPEC §6.1 MsgAnchorCredential.
func (m msgServer) AnchorCredential(goCtx context.Context, msg *types.MsgAnchorCredential) (*types.MsgAnchorCredentialResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	issuer, err := sdk.AccAddressFromBech32(msg.Issuer)
	if err != nil {
		return nil, errorsmod.Wrap(err, "issuer")
	}

	issuerSigil, err := m.findSigilAnyChamber(ctx, msg.IssuerSigil)
	if err != nil {
		return nil, err
	}

	id, err := m.Keeper.AnchorCredential(ctx, issuer, msg.IssuerSigil, msg.SigilId,
		msg.SchemaId, msg.ClaimHash, msg.ExpiryHeight, issuerSigil.ChamberId)
	if err != nil {
		return nil, err
	}
	return &types.MsgAnchorCredentialResponse{CredentialId: id}, nil
}

// RevokeCredential implements SPEC §6.1 MsgRevokeCredential (issuer only).
func (m msgServer) RevokeCredential(goCtx context.Context, msg *types.MsgRevokeCredential) (*types.MsgRevokeCredentialResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	issuer, err := sdk.AccAddressFromBech32(msg.Issuer)
	if err != nil {
		return nil, errorsmod.Wrap(err, "issuer")
	}

	subject, err := m.findSigilAnyChamber(ctx, msg.SigilId)
	if err != nil {
		return nil, err
	}

	if err := m.Keeper.RevokeCredential(ctx, issuer, subject.ChamberId, msg.SigilId, msg.CredentialId); err != nil {
		return nil, err
	}
	return &types.MsgRevokeCredentialResponse{}, nil
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

// findSigilAnyChamber resolves a sigil id; Phase 0 has only chamber 0.
// CONCLAVE-TODO(phase-3): chamber routing once chamber_id > 0 activates.
func (m msgServer) findSigilAnyChamber(ctx sdk.Context, sigilID string) (types.Sigil, error) {
	return m.GetSigil(ctx, 0, sigilID)
}
