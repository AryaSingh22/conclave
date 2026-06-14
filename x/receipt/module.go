// Package receipt implements the SPEC §6.6 provable settlement-record surface:
// gRPC queries and ICS-23 proofs over the receipts x/session writes (D-045).
// It is a pure query/proof module — no Msgs, and no KVStoreKey, so it cannot
// write consensus state (D-052).
package receipt

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

	"github.com/AryaSingh22/conclave/x/receipt/keeper"
	"github.com/AryaSingh22/conclave/x/receipt/types"
)

// ConsensusVersion defines the module's consensus-breaking version.
const ConsensusVersion = 1

var (
	_ module.AppModuleBasic = AppModule{}
	_ module.HasGenesis     = AppModule{}
	_ module.HasServices    = AppModule{}
	_ appmodule.AppModule   = AppModule{}
)

// AppModule implements the x/receipt query/proof module (SPEC §6.6). It carries
// no EndBlocker/BeginBlocker and no store — it is a read-only surface.
type AppModule struct {
	cdc    codec.Codec
	keeper keeper.Keeper
}

// NewAppModule creates the x/receipt AppModule.
func NewAppModule(cdc codec.Codec, k keeper.Keeper) AppModule {
	return AppModule{cdc: cdc, keeper: k}
}

// IsOnePerModuleType implements appmodule.AppModule.
func (AppModule) IsOnePerModuleType() {}

// IsAppModule implements appmodule.AppModule.
func (AppModule) IsAppModule() {}

// Name returns the module name.
func (AppModule) Name() string { return types.ModuleName }

// RegisterLegacyAminoCodec registers the module's amino types (none — query/
// proof only).
func (AppModule) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

// RegisterInterfaces registers the module's interface types (none — query/proof
// only).
func (AppModule) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// RegisterGRPCGatewayRoutes registers the module's REST routes.
func (AppModule) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *gwruntime.ServeMux) {
	if err := types.RegisterQueryHandlerClient(context.Background(), mux, types.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
}

// GetTxCmd returns nil: x/receipt has no transactions (SPEC §6.6).
func (AppModule) GetTxCmd() *cobra.Command { return nil }

// GetQueryCmd returns nil: query commands come from autocli.
func (AppModule) GetQueryCmd() *cobra.Command { return nil }

// RegisterServices registers only the Query service — x/receipt has no Msgs.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterQueryServer(cfg.QueryServer(), keeper.NewQueryServerImpl(am.keeper))
}

// DefaultGenesis returns the module's default (empty) genesis state.
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

// InitGenesis is a no-op (x/receipt owns no state; D-045/D-052).
func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, bz json.RawMessage) {
	var gs types.GenesisState
	cdc.MustUnmarshalJSON(bz, &gs)
	am.keeper.InitGenesis(ctx, gs)
}

// ExportGenesis exports the empty genesis state.
func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(am.keeper.ExportGenesis(ctx))
}

// ConsensusVersion implements module.HasConsensusVersion.
func (AppModule) ConsensusVersion() uint64 { return ConsensusVersion }
