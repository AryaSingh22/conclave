package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers the x/mandate msg types on the legacy
// amino codec (ledger / legacy sign-mode support).
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgIssueMandate{}, "conclave/mandate/MsgIssueMandate", nil)
	cdc.RegisterConcrete(&MsgRevokeMandate{}, "conclave/mandate/MsgRevokeMandate", nil)
	cdc.RegisterConcrete(&MsgAmendMandate{}, "conclave/mandate/MsgAmendMandate", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "conclave/mandate/MsgUpdateParams", nil)
}

// RegisterInterfaces registers the x/mandate msg implementations.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgIssueMandate{},
		&MsgRevokeMandate{},
		&MsgAmendMandate{},
		&MsgUpdateParams{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
