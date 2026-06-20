package types

import (
	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.Msg = (*MsgSubmitDeal)(nil)
	_ sdk.Msg = (*MsgSubmitResponse)(nil)
	_ sdk.Msg = (*MsgConfirmEpochKey)(nil)
	_ sdk.Msg = (*MsgUpdateParams)(nil)
)

// ValidateBasic does stateless validation of a deal submission. The bundle is
// opaque to the chain, so only structural checks apply here; committee/window
// membership is stateful and checked in the keeper.
func (m *MsgSubmitDeal) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Sender); err != nil {
		return errorsmod.Wrap(ErrUnauthorized, "invalid sender")
	}
	if len(m.Bundle) == 0 {
		return errorsmod.Wrap(ErrInvalidBundle, "empty deal bundle")
	}
	return nil
}

// ValidateBasic does stateless validation of a response submission.
func (m *MsgSubmitResponse) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Sender); err != nil {
		return errorsmod.Wrap(ErrUnauthorized, "invalid sender")
	}
	if len(m.Bundle) == 0 {
		return errorsmod.Wrap(ErrInvalidBundle, "empty response bundle")
	}
	return nil
}

// ValidateBasic does stateless validation of a key confirmation.
func (m *MsgConfirmEpochKey) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Sender); err != nil {
		return errorsmod.Wrap(ErrUnauthorized, "invalid sender")
	}
	if len(m.EpochPubKey) == 0 {
		return errorsmod.Wrap(ErrInvalidBundle, "empty epoch_pub_key")
	}
	if len(m.Commits) == 0 {
		return errorsmod.Wrap(ErrInvalidBundle, "empty commits")
	}
	return nil
}

// ValidateBasic does stateless validation of a params update.
func (m *MsgUpdateParams) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return errorsmod.Wrap(ErrUnauthorized, "invalid authority")
	}
	return m.Params.Validate()
}
