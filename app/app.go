// Package app wires the Conclave application (SPEC §2 System Architecture).
//
// Wiring style (SPEC §3): Cosmos SDK v0.50 depinject for all SDK modules
// (app_config.go) plus manual registration for the IBC stack, which has no
// depinject support in ibc-go v8.
package app

import (
	"io"

	dbm "github.com/cosmos/cosmos-db"

	clienthelpers "cosmossdk.io/client/v2/helpers"
	"cosmossdk.io/depinject"
	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	evidencekeeper "cosmossdk.io/x/evidence/keeper"
	feegrantkeeper "cosmossdk.io/x/feegrant/keeper"
	upgradekeeper "cosmossdk.io/x/upgrade/keeper"

	abci "github.com/cometbft/cometbft/abci/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/api"
	"github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/auth"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authsims "github.com/cosmos/cosmos-sdk/x/auth/simulation"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	consensuskeeper "github.com/cosmos/cosmos-sdk/x/consensus/keeper"
	crisiskeeper "github.com/cosmos/cosmos-sdk/x/crisis/keeper"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	mintkeeper "github.com/cosmos/cosmos-sdk/x/mint/keeper"
	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
	slashingkeeper "github.com/cosmos/cosmos-sdk/x/slashing/keeper"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"

	"github.com/cosmos/ibc-go/modules/capability"
	capabilitykeeper "github.com/cosmos/ibc-go/modules/capability/keeper"
	capabilitytypes "github.com/cosmos/ibc-go/modules/capability/types"
	"github.com/cosmos/ibc-go/v8/modules/apps/transfer"
	ibctransferkeeper "github.com/cosmos/ibc-go/v8/modules/apps/transfer/keeper"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	ibc "github.com/cosmos/ibc-go/v8/modules/core"
	porttypes "github.com/cosmos/ibc-go/v8/modules/core/05-port/types"
	ibcexported "github.com/cosmos/ibc-go/v8/modules/core/exported"
	ibckeeper "github.com/cosmos/ibc-go/v8/modules/core/keeper"
	solomachine "github.com/cosmos/ibc-go/v8/modules/light-clients/06-solomachine"
	ibctm "github.com/cosmos/ibc-go/v8/modules/light-clients/07-tendermint"
)

// DefaultNodeHome is the default home directory for the application daemon.
var DefaultNodeHome string

var (
	_ runtime.AppI            = (*ConclaveApp)(nil)
	_ servertypes.Application = (*ConclaveApp)(nil)
)

// ConclaveApp extends runtime.App (depinject) with the manually wired IBC
// stack. Keepers are exported for use in tests and module wiring.
type ConclaveApp struct {
	*runtime.App
	legacyAmino       *codec.LegacyAmino
	appCodec          codec.Codec
	txConfig          client.TxConfig
	interfaceRegistry codectypes.InterfaceRegistry

	// SDK keepers (provided by depinject from app_config.go)
	AccountKeeper         authkeeper.AccountKeeper
	BankKeeper            bankkeeper.Keeper
	StakingKeeper         *stakingkeeper.Keeper
	SlashingKeeper        slashingkeeper.Keeper
	MintKeeper            mintkeeper.Keeper
	DistrKeeper           distrkeeper.Keeper
	GovKeeper             *govkeeper.Keeper
	CrisisKeeper          *crisiskeeper.Keeper
	UpgradeKeeper         *upgradekeeper.Keeper
	ParamsKeeper          paramskeeper.Keeper
	AuthzKeeper           authzkeeper.Keeper
	EvidenceKeeper        evidencekeeper.Keeper
	FeeGrantKeeper        feegrantkeeper.Keeper
	ConsensusParamsKeeper consensuskeeper.Keeper

	// IBC keepers (wired manually; ibc-go v8 has no depinject support)
	CapabilityKeeper *capabilitykeeper.Keeper
	IBCKeeper        *ibckeeper.Keeper // must be a pointer so SetRouter works
	TransferKeeper   ibctransferkeeper.Keeper

	ScopedIBCKeeper      capabilitykeeper.ScopedKeeper
	ScopedTransferKeeper capabilitykeeper.ScopedKeeper

	// simulation manager
	sm *module.SimulationManager
}

func init() {
	// The bech32 prefixes and node home are process-wide; setting them in the
	// app package init guarantees every binary and test importing app gets the
	// Conclave identity (SPEC §0) before any address is parsed or rendered.
	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount(Bech32Prefix, Bech32Prefix+sdk.PrefixPublic)
	cfg.SetBech32PrefixForValidator(Bech32PrefixValidator, Bech32PrefixValidator+sdk.PrefixPublic)
	cfg.SetBech32PrefixForConsensusNode(Bech32PrefixConsensus, Bech32PrefixConsensus+sdk.PrefixPublic)
	cfg.Seal()

	var err error
	DefaultNodeHome, err = clienthelpers.GetNodeHomeDirectory(".conclaved")
	if err != nil {
		panic(err)
	}
}

// NewConclaveApp returns a reference to an initialized ConclaveApp.
func NewConclaveApp(
	logger log.Logger,
	db dbm.DB,
	traceStore io.Writer,
	loadLatest bool,
	appOpts servertypes.AppOptions,
	baseAppOptions ...func(*baseapp.BaseApp),
) *ConclaveApp {
	var (
		app        = &ConclaveApp{}
		appBuilder *runtime.AppBuilder

		appConfig = depinject.Configs(
			AppConfig,
			depinject.Supply(
				appOpts,
				logger,
			),
		)
	)

	if err := depinject.Inject(appConfig,
		&appBuilder,
		&app.appCodec,
		&app.legacyAmino,
		&app.txConfig,
		&app.interfaceRegistry,
		&app.AccountKeeper,
		&app.BankKeeper,
		&app.StakingKeeper,
		&app.SlashingKeeper,
		&app.MintKeeper,
		&app.DistrKeeper,
		&app.GovKeeper,
		&app.CrisisKeeper,
		&app.UpgradeKeeper,
		&app.ParamsKeeper,
		&app.AuthzKeeper,
		&app.EvidenceKeeper,
		&app.FeeGrantKeeper,
		&app.ConsensusParamsKeeper,
	); err != nil {
		panic(err)
	}

	app.App = appBuilder.Build(db, traceStore, baseAppOptions...)

	// ---- manual IBC wiring (no depinject support in ibc-go v8) ----

	ibcKey := storetypes.NewKVStoreKey(ibcexported.StoreKey)
	transferKey := storetypes.NewKVStoreKey(ibctransfertypes.StoreKey)
	capabilityKey := storetypes.NewKVStoreKey(capabilitytypes.StoreKey)
	capabilityMemKey := storetypes.NewMemoryStoreKey(capabilitytypes.MemStoreKey)
	if err := app.RegisterStores(ibcKey, transferKey, capabilityKey, capabilityMemKey); err != nil {
		panic(err)
	}

	govAuthority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	// legacy param subspaces are still required by ibc-go v8 for param migrations
	ibcSubspace := app.ParamsKeeper.Subspace(ibcexported.ModuleName)
	transferSubspace := app.ParamsKeeper.Subspace(ibctransfertypes.ModuleName)

	app.CapabilityKeeper = capabilitykeeper.NewKeeper(app.appCodec, capabilityKey, capabilityMemKey)
	app.ScopedIBCKeeper = app.CapabilityKeeper.ScopeToModule(ibcexported.ModuleName)
	app.ScopedTransferKeeper = app.CapabilityKeeper.ScopeToModule(ibctransfertypes.ModuleName)
	// seal the capability keeper: all scoped keepers must be created above
	app.CapabilityKeeper.Seal()

	app.IBCKeeper = ibckeeper.NewKeeper(
		app.appCodec, ibcKey, ibcSubspace, app.StakingKeeper, app.UpgradeKeeper,
		app.ScopedIBCKeeper, govAuthority,
	)

	app.TransferKeeper = ibctransferkeeper.NewKeeper(
		app.appCodec, transferKey, transferSubspace,
		app.IBCKeeper.ChannelKeeper, app.IBCKeeper.ChannelKeeper, app.IBCKeeper.PortKeeper,
		app.AccountKeeper, app.BankKeeper, app.ScopedTransferKeeper, govAuthority,
	)

	ibcRouter := porttypes.NewRouter()
	ibcRouter.AddRoute(ibctransfertypes.ModuleName, transfer.NewIBCModule(app.TransferKeeper))
	app.IBCKeeper.SetRouter(ibcRouter)

	if err := app.RegisterModules(
		capability.NewAppModule(app.appCodec, *app.CapabilityKeeper, false),
		ibc.NewAppModule(app.IBCKeeper),
		transfer.NewAppModule(app.TransferKeeper),
		ibctm.NewAppModule(),
		solomachine.NewAppModule(),
	); err != nil {
		panic(err)
	}

	// ---- end manual IBC wiring ----

	// register streaming services
	if err := app.RegisterStreamingServices(appOpts, app.kvStoreKeys()); err != nil {
		panic(err)
	}

	// register module invariants with the crisis module (SPEC §6.1 requires
	// Conclave module invariants to be registered here as they land)
	app.ModuleManager.RegisterInvariants(app.CrisisKeeper)

	// simulation manager (SDK simulation integration is task 0.11)
	overrideModules := map[string]module.AppModuleSimulation{
		authtypes.ModuleName: auth.NewAppModule(app.appCodec, app.AccountKeeper, authsims.RandomGenesisAccounts, app.GetSubspace(authtypes.ModuleName)),
	}
	app.sm = module.NewSimulationManagerFromAppModules(app.ModuleManager.Modules, overrideModules)
	app.sm.RegisterStoreDecoders()

	app.setAnteHandler(app.txConfig)

	// Modules registered outside depinject (the IBC stack) are unknown to the
	// upgrade module's automatic version-map wiring, so the full version map
	// must be set in InitChain explicitly (the upgrade module de-duplicates).
	app.SetInitChainer(func(ctx sdk.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
		if err := app.UpgradeKeeper.SetModuleVersionMap(ctx, app.ModuleManager.GetVersionMap()); err != nil {
			return nil, err
		}
		return app.App.InitChainer(ctx, req)
	})

	if err := app.Load(loadLatest); err != nil {
		panic(err)
	}

	return app
}

// setAnteHandler installs the Conclave ante pipeline (SPEC §2; see ante.go).
func (app *ConclaveApp) setAnteHandler(txConfig client.TxConfig) {
	anteHandler, err := NewAnteHandler(
		HandlerOptions{
			HandlerOptions: ante.HandlerOptions{
				AccountKeeper:   app.AccountKeeper,
				BankKeeper:      app.BankKeeper,
				SignModeHandler: txConfig.SignModeHandler(),
				FeegrantKeeper:  app.FeeGrantKeeper,
				SigGasConsumer:  ante.DefaultSigVerificationGasConsumer,
			},
			IBCKeeper: app.IBCKeeper,
		},
	)
	if err != nil {
		panic(err)
	}

	app.SetAnteHandler(anteHandler)
}

// LegacyAmino returns the app's amino codec.
func (app *ConclaveApp) LegacyAmino() *codec.LegacyAmino {
	return app.legacyAmino
}

// AppCodec returns the app codec.
func (app *ConclaveApp) AppCodec() codec.Codec {
	return app.appCodec
}

// InterfaceRegistry returns the app's InterfaceRegistry.
func (app *ConclaveApp) InterfaceRegistry() codectypes.InterfaceRegistry {
	return app.interfaceRegistry
}

// TxConfig returns the app's TxConfig.
func (app *ConclaveApp) TxConfig() client.TxConfig {
	return app.txConfig
}

// GetKey returns the KVStoreKey for the provided store key.
//
// NOTE: This is solely to be used for testing purposes.
func (app *ConclaveApp) GetKey(storeKey string) *storetypes.KVStoreKey {
	sk := app.UnsafeFindStoreKey(storeKey)
	kvStoreKey, ok := sk.(*storetypes.KVStoreKey)
	if !ok {
		return nil
	}
	return kvStoreKey
}

func (app *ConclaveApp) kvStoreKeys() map[string]*storetypes.KVStoreKey {
	keys := make(map[string]*storetypes.KVStoreKey)
	for _, k := range app.GetStoreKeys() {
		if kv, ok := k.(*storetypes.KVStoreKey); ok {
			keys[kv.Name()] = kv
		}
	}

	return keys
}

// GetSubspace returns a param subspace for a given module name.
func (app *ConclaveApp) GetSubspace(moduleName string) paramstypes.Subspace {
	subspace, _ := app.ParamsKeeper.GetSubspace(moduleName)
	return subspace
}

// SimulationManager implements the SimulationApp interface.
func (app *ConclaveApp) SimulationManager() *module.SimulationManager {
	return app.sm
}

// RegisterAPIRoutes registers all application module routes with the provided
// API server.
func (app *ConclaveApp) RegisterAPIRoutes(apiSvr *api.Server, apiConfig config.APIConfig) {
	app.App.RegisterAPIRoutes(apiSvr, apiConfig)
	// register swagger API in app.go so that other applications can override easily
	if err := server.RegisterSwaggerAPI(apiSvr.ClientCtx, apiSvr.Router, apiConfig.Swagger); err != nil {
		panic(err)
	}
}

// GetMaccPerms returns a copy of the module account permissions.
func GetMaccPerms() map[string][]string {
	dup := make(map[string][]string)
	for _, perms := range moduleAccPerms {
		dup[perms.Account] = perms.Permissions
	}

	return dup
}

// BlockedAddresses returns all the app's blocked account addresses.
func BlockedAddresses() map[string]bool {
	result := make(map[string]bool)

	if len(blockAccAddrs) > 0 {
		for _, addr := range blockAccAddrs {
			result[addr] = true
		}
	} else {
		for addr := range GetMaccPerms() {
			result[addr] = true
		}
	}

	return result
}
