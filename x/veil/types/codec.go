package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers the x/veil msg types on the legacy
// amino codec (ledger / legacy sign-mode support).
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgSubmitIntent{}, "conclave/veil/MsgSubmitIntent", nil)
	cdc.RegisterConcrete(&MsgCancelIntent{}, "conclave/veil/MsgCancelIntent", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "conclave/veil/MsgUpdateParams", nil)
}

// RegisterInterfaces registers the x/veil msg implementations.
//
// Registered at task 0.6 (before the 0.7 msg server) because the veil lane
// routes on these TypeURLs (SPEC §6.7): the app's tx decoder must resolve
// them for lane verification in ProcessProposal. With no msg server routed,
// baseapp keeps rejecting these msgs in execution (D-024 unchanged).
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSubmitIntent{},
		&MsgCancelIntent{},
		&MsgUpdateParams{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
