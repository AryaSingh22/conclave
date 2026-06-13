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

// RegisterInterfaces registers the x/veil msg implementations. Originally
// registered manually at task 0.6 for lane routing (D-033); since task 0.7
// the x/veil AppModule registers them and routes the msg server, resolving
// D-024.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSubmitIntent{},
		&MsgCancelIntent{},
		&MsgUpdateParams{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
