package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers the x/session msg types on the legacy
// amino codec (ledger / legacy sign-mode support).
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgRegisterSolver{}, "conclave/session/MsgRegisterSolver", nil)
	cdc.RegisterConcrete(&MsgUnbondSolver{}, "conclave/session/MsgUnbondSolver", nil)
	cdc.RegisterConcrete(&MsgProposeSolution{}, "conclave/session/MsgProposeSolution", nil)
	cdc.RegisterConcrete(&MsgConclude{}, "conclave/session/MsgConclude", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "conclave/session/MsgUpdateParams", nil)
}

// RegisterInterfaces registers the x/session msg implementations.
//
// Registered at task 0.6 (before the 0.8 msg server) because the session
// lane routes on these TypeURLs (SPEC §6.7): the app's tx decoder must
// resolve them for lane verification in ProcessProposal. With no msg server
// routed, baseapp keeps rejecting these msgs in execution (D-024 unchanged).
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterSolver{},
		&MsgUnbondSolver{},
		&MsgProposeSolution{},
		&MsgConclude{},
		&MsgUpdateParams{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
