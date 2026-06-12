package unordered

import (
	"context"

	gwruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"

	"cosmossdk.io/core/appmodule"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/AryaSingh22/conclave/app/unordered/types"
)

// ConsensusVersion defines the shim's consensus-breaking version.
const ConsensusVersion = 1

var (
	_ module.AppModuleBasic   = AppModule{}
	_ appmodule.AppModule     = AppModule{}
	_ appmodule.HasEndBlocker = AppModule{}
)

// AppModule is the minimal EndBlocker shim carrying the SPEC §6.7 dedup
// sweep. It is deliberately NOT a user-facing module — no msgs, no
// queries, no genesis — preserving §6.7's "app-level, not a module"
// (D-030). Its only jobs: register the tx extension type and run Sweep
// every EndBlock.
type AppModule struct {
	manager Manager
}

// NewAppModule creates the unordered EndBlocker shim.
func NewAppModule(manager Manager) AppModule {
	return AppModule{manager: manager}
}

// IsOnePerModuleType implements appmodule.AppModule.
func (AppModule) IsOnePerModuleType() {}

// IsAppModule implements appmodule.AppModule.
func (AppModule) IsAppModule() {}

// Name returns the shim name (also the dedup store key).
func (AppModule) Name() string { return types.ModuleName }

// RegisterLegacyAminoCodec is a no-op: the extension is proto-only.
func (AppModule) RegisterLegacyAminoCodec(*codec.LegacyAmino) {}

// RegisterInterfaces registers UnorderedTxExtension as a TxBody extension
// option so tx decoding resolves it.
func (AppModule) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// RegisterGRPCGatewayRoutes is a no-op: no query surface (SPEC §6.7).
func (AppModule) RegisterGRPCGatewayRoutes(client.Context, *gwruntime.ServeMux) {}

// EndBlock implements the SPEC §6.7 dedup-cache expiry sweep.
func (am AppModule) EndBlock(goCtx context.Context) error {
	ctx := sdk.UnwrapSDKContext(goCtx)
	swept, err := am.manager.Sweep(ctx)
	if err != nil {
		return err
	}
	if swept > 0 {
		ctx.Logger().Debug("swept expired unordered dedup entries", "count", swept)
	}
	return nil
}

// ConsensusVersion implements module.HasConsensusVersion.
func (AppModule) ConsensusVersion() uint64 { return ConsensusVersion }
