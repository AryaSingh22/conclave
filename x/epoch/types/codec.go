package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers the x/epoch msg types on the legacy amino
// codec.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgRegisterDKGKey{}, "conclave/epoch/MsgRegisterDKGKey", nil)
	cdc.RegisterConcrete(&MsgSubmitDeal{}, "conclave/epoch/MsgSubmitDeal", nil)
	cdc.RegisterConcrete(&MsgSubmitResponse{}, "conclave/epoch/MsgSubmitResponse", nil)
	cdc.RegisterConcrete(&MsgSubmitJustification{}, "conclave/epoch/MsgSubmitJustification", nil)
	cdc.RegisterConcrete(&MsgConfirmEpochKey{}, "conclave/epoch/MsgConfirmEpochKey", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "conclave/epoch/MsgUpdateParams", nil)
}

// RegisterInterfaces registers the x/epoch msg implementations.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterDKGKey{},
		&MsgSubmitDeal{},
		&MsgSubmitResponse{},
		&MsgSubmitJustification{},
		&MsgConfirmEpochKey{},
		&MsgUpdateParams{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
