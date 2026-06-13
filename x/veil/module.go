// Package veil implements the Conclave encrypted intent mempool — Sealed
// Intents, bonds, and the Reveal (SPEC §6.3, Phase 0 STUB mode).
package veil

import (
	"context"
	"encoding/json"
	"fmt"

	gwruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"

	"cosmossdk.io/core/appmodule"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/AryaSingh22/conclave/x/veil/keeper"
	"github.com/AryaSingh22/conclave/x/veil/types"
)

// ConsensusVersion defines the module's consensus-breaking version.
const ConsensusVersion = 1

var (
	_ module.AppModuleBasic     = AppModule{}
	_ module.HasGenesis         = AppModule{}
	_ module.HasServices        = AppModule{}
	_ appmodule.AppModule       = AppModule{}
	_ appmodule.HasBeginBlocker = AppModule{}
	_ appmodule.HasEndBlocker   = AppModule{}
)

// AppModule implements the x/veil module (SPEC §6.3).
type AppModule struct {
	cdc    codec.Codec
	keeper keeper.Keeper
}

// NewAppModule creates the x/veil AppModule.
func NewAppModule(cdc codec.Codec, k keeper.Keeper) AppModule {
	return AppModule{cdc: cdc, keeper: k}
}

// IsOnePerModuleType implements appmodule.AppModule.
func (AppModule) IsOnePerModuleType() {}

// IsAppModule implements appmodule.AppModule.
func (AppModule) IsAppModule() {}

// Name returns the module name.
func (AppModule) Name() string { return types.ModuleName }

// RegisterLegacyAminoCodec registers the module's amino types.
func (AppModule) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

// RegisterInterfaces registers the module's interface types.
func (AppModule) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// RegisterGRPCGatewayRoutes registers the module's REST routes.
func (AppModule) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *gwruntime.ServeMux) {
	if err := types.RegisterQueryHandlerClient(context.Background(), mux, types.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
}

// GetTxCmd returns nil: tx commands come from autocli.
func (AppModule) GetTxCmd() *cobra.Command { return nil }

// GetQueryCmd returns nil: query commands come from autocli.
func (AppModule) GetQueryCmd() *cobra.Command { return nil }

// RegisterServices registers the Msg and Query services. Routing the msg
// server is what resolves D-024: veil msgs execute in blocks from 0.7 on.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.keeper))
	types.RegisterQueryServer(cfg.QueryServer(), keeper.NewQueryServerImpl(am.keeper))
}

// DefaultGenesis returns the module's default genesis state.
func (am AppModule) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(types.DefaultGenesis())
}

// ValidateGenesis validates the module's genesis state.
func (AppModule) ValidateGenesis(cdc codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var gs types.GenesisState
	if err := cdc.UnmarshalJSON(bz, &gs); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
	}
	return gs.Validate()
}

// InitGenesis initializes the module state from genesis.
func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, bz json.RawMessage) {
	var gs types.GenesisState
	cdc.MustUnmarshalJSON(bz, &gs)
	am.keeper.InitGenesis(ctx, gs)
}

// ExportGenesis exports the module state to genesis.
func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(am.keeper.ExportGenesis(ctx))
}

// BeginBlock runs the Reveal — strictly the block after ordering fixed
// (SPEC §6.3 ABCI hooks).
func (am AppModule) BeginBlock(goCtx context.Context) error {
	return am.keeper.BeginBlocker(sdk.UnwrapSDKContext(goCtx))
}

// EndBlock runs the expiry sweep and the session-boundary seal
// (SPEC §6.3 ABCI hooks; D-038 interim ownership until task 0.8).
func (am AppModule) EndBlock(goCtx context.Context) error {
	return am.keeper.EndBlocker(sdk.UnwrapSDKContext(goCtx))
}

// ConsensusVersion implements module.HasConsensusVersion.
func (AppModule) ConsensusVersion() uint64 { return ConsensusVersion }
