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

	blocksdkabci "github.com/skip-mev/block-sdk/v2/abci"
	"github.com/skip-mev/block-sdk/v2/block"

	"github.com/AryaSingh22/conclave/app/unordered"
	unorderedtypes "github.com/AryaSingh22/conclave/app/unordered/types"
	escrowtypes "github.com/AryaSingh22/conclave/x/escrow/types"
	"github.com/AryaSingh22/conclave/x/mandate"
	mandatekeeper "github.com/AryaSingh22/conclave/x/mandate/keeper"
	mandatetypes "github.com/AryaSingh22/conclave/x/mandate/types"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
	"github.com/AryaSingh22/conclave/x/sigil"
	sigilkeeper "github.com/AryaSingh22/conclave/x/sigil/keeper"
	sigiltypes "github.com/AryaSingh22/conclave/x/sigil/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
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

	// Conclave keepers (wired manually, same pattern as IBC; modules land
	// in SPEC §10 task order)
	SigilKeeper   sigilkeeper.Keeper
	MandateKeeper mandatekeeper.Keeper

	// UnorderedManager owns the SPEC §6.7 dedup cache (app-level, not a
	// module)
	UnorderedManager unordered.Manager

	// LanedMempool is the Block SDK mempool behind block proposals
	// (SPEC §6.7 lanes; D-032 supersedes D-031). Exported so tests can
	// inspect the lane registry and per-lane contents.
	LanedMempool *block.LanedMempool

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

	// ---- Conclave modules (SPEC §10 build order) ----

	sigilKey := storetypes.NewKVStoreKey(sigiltypes.StoreKey)
	mandateKey := storetypes.NewKVStoreKey(mandatetypes.StoreKey)
	unorderedKey := storetypes.NewKVStoreKey(unorderedtypes.StoreKey)
	if err := app.RegisterStores(sigilKey, mandateKey, unorderedKey); err != nil {
		panic(err)
	}
	app.SigilKeeper = sigilkeeper.NewKeeper(app.appCodec, runtime.NewKVStoreService(sigilKey), govAuthority)
	app.MandateKeeper = mandatekeeper.NewKeeper(
		app.appCodec, runtime.NewKVStoreService(mandateKey),
		app.SigilKeeper, app.BankKeeper, app.AccountKeeper, govAuthority,
	)
	app.UnorderedManager = unordered.NewManager(runtime.NewKVStoreService(unorderedKey))

	if err := app.RegisterModules(
		sigil.NewAppModule(app.appCodec, app.SigilKeeper),
		mandate.NewAppModule(app.appCodec, app.MandateKeeper),
		unordered.NewAppModule(app.UnorderedManager),
	); err != nil {
		panic(err)
	}

	// Lane-class msg types whose modules land later (veil 0.7, session 0.8,
	// escrow Phase 2) are registered now: the SPEC §6.7 lanes route on these
	// TypeURLs, so the app's tx decoder must resolve them for lane
	// verification in ProcessProposal (D-033). No msg server is routed, so
	// baseapp keeps rejecting these msgs in execution (D-024 unchanged).
	veiltypes.RegisterInterfaces(app.interfaceRegistry)
	sessiontypes.RegisterInterfaces(app.interfaceRegistry)
	escrowtypes.RegisterInterfaces(app.interfaceRegistry)
	veiltypes.RegisterLegacyAminoCodec(app.legacyAmino)
	sessiontypes.RegisterLegacyAminoCodec(app.legacyAmino)
	escrowtypes.RegisterLegacyAminoCodec(app.legacyAmino)

	// ---- end Conclave modules ----

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

	anteHandler := app.setAnteHandler(app.txConfig)

	// The Block SDK laned mempool + proposal handlers replace the interim
	// NoOpMempool wholesale (SPEC §6.7 lanes; D-032 supersedes D-031).
	// Set AFTER Build so this overrides whatever mempool the server's
	// DefaultBaseappOptions selected — the SenderNonceMempool it defaults
	// to orders and gates same-sender txs by sequence BEFORE the ante
	// handler runs, serializing exactly the parallel unordered txs
	// SPEC §6.7 requires.
	app.setBlockSDKLanes(logger, anteHandler)

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

// setAnteHandler installs the Conclave ante pipeline (SPEC §2; see ante.go)
// and returns it for reuse by the lane definitions (SPEC §6.7): lanes verify
// proposal txs with the production ante chain, so proposal-time checks are
// exactly the CheckTx checks.
func (app *ConclaveApp) setAnteHandler(txConfig client.TxConfig) sdk.AnteHandler {
	anteHandler, err := NewAnteHandler(
		HandlerOptions{
			HandlerOptions: ante.HandlerOptions{
				AccountKeeper:   app.AccountKeeper,
				BankKeeper:      app.BankKeeper,
				SignModeHandler: txConfig.SignModeHandler(),
				FeegrantKeeper:  app.FeeGrantKeeper,
				SigGasConsumer:  ante.DefaultSigVerificationGasConsumer,
			},
			IBCKeeper:     app.IBCKeeper,
			MandateKeeper: app.MandateKeeper,
			// CONCLAVE-TODO(phase-0): task 0.8 swaps the face-value source
			// for the x/session clearing-price implementation (SPEC §6.2
			// step 4)
			PriceSource:      mandatetypes.FaceValuePriceSource{},
			UnorderedManager: app.UnorderedManager,
		},
	)
	if err != nil {
		panic(err)
	}

	app.SetAnteHandler(anteHandler)
	return anteHandler
}

// setBlockSDKLanes installs the SPEC §6.7 laned mempool and the Block SDK
// PrepareProposal/ProcessProposal handlers (lane structure validation:
// partition contiguity in priority order, intra-lane priority ordering,
// per-lane space limits, per-tx ante verification — see docs/LANES.md).
func (app *ConclaveApp) setBlockSDKLanes(logger log.Logger, anteHandler sdk.AnteHandler) {
	lanes, err := NewLanes(logger, app.txConfig, anteHandler)
	if err != nil {
		panic(err)
	}

	lanedMempool, err := block.NewLanedMempool(logger, lanes)
	if err != nil {
		panic(err)
	}

	app.LanedMempool = lanedMempool
	app.SetMempool(lanedMempool)

	// useCustomProcessProposal=true: the Block SDK's default handler wires
	// a NO-OP ProcessProposal; lane structure validation requires the
	// custom path (D-032).
	proposalHandler := blocksdkabci.New(
		logger,
		app.txConfig.TxDecoder(),
		app.txConfig.TxEncoder(),
		lanedMempool,
		true,
	)
	app.SetPrepareProposal(proposalHandler.PrepareProposalHandler())
	app.SetProcessProposal(proposalHandler.ProcessProposalHandler())
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
