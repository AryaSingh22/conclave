package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers the x/escrow msg types on the legacy
// amino codec (ledger / legacy sign-mode support).
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgReleaseEscrow{}, "conclave/escrow/MsgReleaseEscrow", nil)
	cdc.RegisterConcrete(&MsgRaiseDispute{}, "conclave/escrow/MsgRaiseDispute", nil)
	cdc.RegisterConcrete(&MsgSubmitDeliveryProof{}, "conclave/escrow/MsgSubmitDeliveryProof", nil)
}

// RegisterInterfaces registers the x/escrow msg implementations.
//
// Registered at task 0.6 (before the Phase-2 msg server) because the
// commerce lane routes on these TypeURLs (SPEC §6.7): the app's tx decoder
// must resolve them for lane verification in ProcessProposal. With no msg
// server routed, baseapp keeps rejecting these msgs in execution (D-024
// unchanged).
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgReleaseEscrow{},
		&MsgRaiseDispute{},
		&MsgSubmitDeliveryProof{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
