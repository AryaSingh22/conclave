package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers the x/sigil msg types on the legacy
// amino codec (ledger / legacy sign-mode support).
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgRegisterSigil{}, "conclave/sigil/MsgRegisterSigil", nil)
	cdc.RegisterConcrete(&MsgRotateController{}, "conclave/sigil/MsgRotateController", nil)
	cdc.RegisterConcrete(&MsgAnchorCredential{}, "conclave/sigil/MsgAnchorCredential", nil)
	cdc.RegisterConcrete(&MsgRevokeCredential{}, "conclave/sigil/MsgRevokeCredential", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "conclave/sigil/MsgUpdateParams", nil)
}

// RegisterInterfaces registers the x/sigil msg implementations.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterSigil{},
		&MsgRotateController{},
		&MsgAnchorCredential{},
		&MsgRevokeCredential{},
		&MsgUpdateParams{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
