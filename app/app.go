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
	corestoretypes "cosmossdk.io/core/store"
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

	"github.com/CosmWasm/wasmd/x/wasm"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"

	blocksdkabci "github.com/skip-mev/block-sdk/v2/abci"
	"github.com/skip-mev/block-sdk/v2/block"

	"github.com/AryaSingh22/conclave/app/cvm"
	"github.com/AryaSingh22/conclave/app/unordered"
	unorderedtypes "github.com/AryaSingh22/conclave/app/unordered/types"
	"github.com/AryaSingh22/conclave/crypto/reveal"
	"github.com/AryaSingh22/conclave/x/epoch"
	epochkeeper "github.com/AryaSingh22/conclave/x/epoch/keeper"
	epochtypes "github.com/AryaSingh22/conclave/x/epoch/types"
	"github.com/AryaSingh22/conclave/x/escrow"
	escrowkeeper "github.com/AryaSingh22/conclave/x/escrow/keeper"
	escrowtypes "github.com/AryaSingh22/conclave/x/escrow/types"
	"github.com/AryaSingh22/conclave/x/mandate"
	mandatekeeper "github.com/AryaSingh22/conclave/x/mandate/keeper"
	mandatetypes "github.com/AryaSingh22/conclave/x/mandate/types"
	"github.com/AryaSingh22/conclave/x/receipt"
	receiptkeeper "github.com/AryaSingh22/conclave/x/receipt/keeper"
	receipttypes "github.com/AryaSingh22/conclave/x/receipt/types"
	"github.com/AryaSingh22/conclave/x/session"
	sessionkeeper "github.com/AryaSingh22/conclave/x/session/keeper"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
	"github.com/AryaSingh22/conclave/x/sigil"
	sigilkeeper "github.com/AryaSingh22/conclave/x/sigil/keeper"
	sigiltypes "github.com/AryaSingh22/conclave/x/sigil/types"
	"github.com/AryaSingh22/conclave/x/veil"
	veilkeeper "github.com/AryaSingh22/conclave/x/veil/keeper"
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

	// WasmKeeper runs the SPEC §0 CVM contract runtime (CosmWasm, task 2.2,
	// D-078) on the D-077 v0.50 dependency line. Contracts reach Conclave
	// modules ONLY through the app/cvm bindings wired at construction.
	WasmKeeper wasmkeeper.Keeper

	// Conclave keepers (wired manually, same pattern as IBC; modules land
	// in SPEC §10 task order)
	SigilKeeper   sigilkeeper.Keeper
	MandateKeeper mandatekeeper.Keeper
	VeilKeeper    veilkeeper.Keeper
	// EscrowKeeper owns the SPEC §6.5 conditioned-delivery escrows (D-073
	// ownership handoff from x/session). Built before x/session so the
	// conclude.go DataGrant handoff can write through it.
	EscrowKeeper escrowkeeper.Keeper
	// SessionKeeper is a pointer so SetVeilKeeper can wire the veil↔session
	// dependency cycle in place after both keepers exist (Guard 1).
	SessionKeeper *sessionkeeper.Keeper
	// EpochKeeper owns the DKG epoch key lifecycle (PHASE1_DESIGN §2(b), D-068).
	// A pointer so SetVeilKeeper can wire the veil↔epoch cycle in place. Since
	// task 1.6 (D-069) the live Reveal dispatches PER INTENT on the envelope's
	// epoch_id — StubScheme for 0, real threshold decryption otherwise — so this
	// keeper's published epoch key is on the live path, not a future one.
	EpochKeeper *epochkeeper.Keeper
	// ReceiptKeeper is the SPEC §6.6 query/proof surface over the receipts
	// x/session writes (D-045). It owns no store (D-052).
	ReceiptKeeper receiptkeeper.Keeper

	// UnorderedManager owns the SPEC §6.7 dedup cache (app-level, not a
	// module)
	UnorderedManager unordered.Manager

	// LanedMempool is the Block SDK mempool behind block proposals
	// (SPEC §6.7 lanes; D-032 supersedes D-031). Exported so tests can
	// inspect the lane registry and per-lane contents.
	LanedMempool *block.LanedMempool

	// Task-1.6 vote-extension share transport (PHASE1_DESIGN §2, AUDIT F-4).
	// RevealShareCache is the per-block supply of collected reveal shares wired
	// into x/veil as its RevealShareSource; ShareProvider is the node-local DKG
	// secret-share store ExtendVote reads (a MapShareProvider by default — tests
	// and the Task-1.7 DKG daemon seed it via the same pointer the handler holds);
	// RevealVoteExt holds the ExtendVote/Verify/PrepareProposal/ProcessProposal/
	// PreBlocker handlers. Exported for the in-process transport harness.
	RevealShareCache *RevealShareCache
	ShareProvider    ShareProvider
	RevealVoteExt    *RevealVoteExtensions

	ScopedIBCKeeper      capabilitykeeper.ScopedKeeper
	ScopedTransferKeeper capabilitykeeper.ScopedKeeper
	ScopedWasmKeeper     capabilitykeeper.ScopedKeeper

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
	app.ScopedWasmKeeper = app.CapabilityKeeper.ScopeToModule(wasmtypes.ModuleName)
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
	// SetRouter is deferred to the CosmWasm section below: the router seals on
	// set, and the wasm IBC route can only be added once the wasm keeper exists
	// (which in turn needs the Conclave keepers for its CVM bindings).

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
	veilKey := storetypes.NewKVStoreKey(veiltypes.StoreKey)
	sessionKey := storetypes.NewKVStoreKey(sessiontypes.StoreKey)
	escrowKey := storetypes.NewKVStoreKey(escrowtypes.StoreKey)
	epochKey := storetypes.NewKVStoreKey(epochtypes.StoreKey)
	unorderedKey := storetypes.NewKVStoreKey(unorderedtypes.StoreKey)
	if err := app.RegisterStores(sigilKey, mandateKey, veilKey, sessionKey, escrowKey, epochKey, unorderedKey); err != nil {
		panic(err)
	}
	app.SigilKeeper = sigilkeeper.NewKeeper(app.appCodec, runtime.NewKVStoreService(sigilKey), govAuthority)
	app.MandateKeeper = mandatekeeper.NewKeeper(
		app.appCodec, runtime.NewKVStoreService(mandateKey),
		app.SigilKeeper, app.BankKeeper, app.AccountKeeper, govAuthority,
	)
	// x/escrow owns the SPEC §6.5 conditioned-delivery escrows (D-073). Built
	// BEFORE x/session so the conclude.go DataGrant ownership handoff writes
	// escrow records through this keeper rather than into the session store.
	app.EscrowKeeper = escrowkeeper.NewKeeper(
		app.appCodec, runtime.NewKVStoreService(escrowKey),
		app.BankKeeper, app.SigilKeeper,
	)
	// x/session is built BEFORE x/veil so it can be passed in as veil's real
	// SessionKeeper (SessionBoundary/IntakeRevealed), replacing the D-038 stub.
	// Its own dependency on x/veil (CONCLUDED/refund hooks) is wired below via
	// SetVeilKeeper once VeilKeeper exists (Guard 1).
	app.SessionKeeper = sessionkeeper.NewKeeper(
		app.appCodec, runtime.NewKVStoreService(sessionKey),
		app.BankKeeper, app.DistrKeeper, app.AccountKeeper, app.SigilKeeper,
		app.EscrowKeeper,
		govAuthority,
	)
	// x/epoch owns the Phase-1 DKG epoch key lifecycle (PHASE1_DESIGN §2(b),
	// D-068). Built BEFORE x/veil so it can be wired in as veil's EpochKeeper
	// (submit-time epoch-tag validation); its own dependency on x/veil
	// (HasInFlightForEpoch, for drain-before-rotate) is wired below via
	// SetVeilKeeper once VeilKeeper exists. It reads the staking-finalized bonded
	// set to snapshot each epoch's DKG committee.
	app.EpochKeeper = epochkeeper.NewKeeper(
		app.appCodec, runtime.NewKVStoreService(epochKey),
		app.StakingKeeper, govAuthority,
	)
	app.VeilKeeper = veilkeeper.NewKeeper(
		app.appCodec, runtime.NewKVStoreService(veilKey),
		// The BASE scheme, not the only one: SPEC §6.3 STUB mode (identity
		// Reveal) behind the real ThresholdScheme interface. Since task 1.6
		// (D-069) RevealSealed selects per intent on the envelope's epoch_id —
		// this scheme serves epoch_id == 0 (the epoch-0 bootstrap window, L1),
		// while a non-zero epoch_id builds a real threshold scheme from the
		// epoch's published commits. See crypto/reveal/stub.go.
		reveal.StubScheme{},
		app.BankKeeper, app.DistrKeeper, app.SigilKeeper, app.MandateKeeper,
		// the real x/session keeper now supplies the session-boundary hand-off
		// (SessionBoundary/IntakeRevealed), replacing the D-038 stub.
		app.SessionKeeper,
		// the ante/veil PriceSource swap (D-020) is deferred to a dedicated
		// post-0.8 task (D-044): price-dependent ante is a new CheckTx/DeliverTx
		// consistency surface and has zero 0.8 acceptance dependency.
		mandatetypes.FaceValuePriceSource{},
		govAuthority,
	)
	// wire the veil↔session cycle now that both keepers exist (Guard 1): must
	// run before any block, which app construction guarantees.
	app.SessionKeeper.SetVeilKeeper(app.VeilKeeper)
	// wire the veil↔mandate cycle the same way (D-064): IssueMandate adopts a
	// root->mandated key's live intent exposure at FRESH issuance via this x/veil
	// scan, so the §1.1-goal-3 bound attaches from issuance. Set BEFORE the
	// keeper value is captured by RegisterModules/setAnteHandler below, so the
	// mandate msg-server and ante copies both observe it.
	app.MandateKeeper.SetLiveExposureFn(app.VeilKeeper.SumLiveChargedSpendByOwner)
	// wire the session TWAP behind §6.2 fee conversion (task 2.5): the fee
	// seam consumes the clearing-price index instead of the fixed rate once
	// warm. Same set-before-capture contract as SetLiveExposureFn above. The
	// index's ONLY consumer — ceiling valuation stays on FaceValuePriceSource
	// (F-3 boundary; the D-044 swap is Phase-3 3.4).
	app.MandateKeeper.SetTwapSource(app.SessionKeeper)
	// wire the veil↔epoch cycle (D-068): veil's submit-time epoch-tag check needs
	// IsDecryptableEpoch; epoch's drain-before-rotate needs HasInFlightForEpoch.
	// SetEpochKeeper must run BEFORE the veil keeper value is captured by
	// RegisterModules below (so the veil msg-server copy observes it), mirroring
	// the mandate.SetLiveExposureFn pattern above.
	app.VeilKeeper.SetEpochKeeper(app.EpochKeeper)
	app.EpochKeeper.SetVeilKeeper(app.VeilKeeper)
	// wire the Task-1.6 vote-extension share transport (PHASE1_DESIGN §2): the
	// per-block reveal-share cache is x/veil's RevealShareSource, and the
	// node-local DKG secret-share store ExtendVote reads. Both are created here —
	// BEFORE RegisterModules captures the veil keeper copy, mirroring SetEpochKeeper
	// — so the BeginBlocker copy observes the wired source. The ABCI handlers
	// themselves are built in setBlockSDKLanes (they wrap the Block-SDK proposal
	// handlers). The cache is populated by the PreBlocker AND CONSUMED by
	// BeginBlock's Reveal — the Phase-B cutover shipped in task 1.6 (D-069), so
	// for any intent carrying a non-zero epoch_id this transport is the live
	// path, not a staged one. (Comment corrected at Rev 3, D-090: it still read
	// "populated but not yet consumed", which stopped being true at 1.6.)
	app.RevealShareCache = NewRevealShareCache()
	// Node-local DKG secret-share store ExtendVote reads (D-062). On a real node
	// (home dir set in appOpts) it is FILE-backed under <home>/data/dkg_shares —
	// the off-chain DKG daemon (Task 1.7, D-070) writes finalized shares there and
	// this provider serves them to ExtendVote; the share never enters consensus
	// state or p2p. Unit tests / the in-process harness pass no home and fall back
	// to the in-memory MapShareProvider.
	app.ShareProvider = newNodeShareProvider(logger, appOpts)
	app.VeilKeeper.SetRevealShareSource(app.RevealShareCache)
	// x/receipt is the SPEC §6.6 query/proof surface: it READS the receipts
	// x/session writes (the SessionKeeper, via the SessionReceiptReader
	// boundary; D-045/D-052) and builds ICS-23 proofs against the committed
	// app root (the CommitMultiStore is a *rootmulti.Store, which implements the
	// ProofQuerier Query method; D-053). It is given NO store key — by
	// construction it cannot write consensus state.
	app.ReceiptKeeper = receiptkeeper.NewKeeper(
		app.appCodec,
		app.SessionKeeper,
		app.CommitMultiStore().(receipttypes.ProofQuerier),
	)
	app.UnorderedManager = unordered.NewManager(runtime.NewKVStoreService(unorderedKey))

	if err := app.RegisterModules(
		sigil.NewAppModule(app.appCodec, app.SigilKeeper, app.AccountKeeper),
		mandate.NewAppModule(app.appCodec, app.MandateKeeper, app.AccountKeeper, app.SigilKeeper),
		veil.NewAppModule(app.appCodec, app.VeilKeeper, app.AccountKeeper, app.BankKeeper, app.SigilKeeper, app.MandateKeeper),
		session.NewAppModule(app.appCodec, app.SessionKeeper, app.AccountKeeper, app.VeilKeeper),
		escrow.NewAppModule(app.appCodec, app.EscrowKeeper),
		epoch.NewAppModule(app.appCodec, app.EpochKeeper),
		receipt.NewAppModule(app.appCodec, app.ReceiptKeeper),
		unordered.NewAppModule(app.UnorderedManager),
	); err != nil {
		panic(err)
	}

	// x/escrow now has a real module (Phase 2, D-073): it registers its own msg
	// interfaces + amino codec via RegisterModules above, so the D-033 app-level
	// shim that pre-registered the escrow types for the §6.7 commerce lane is
	// removed (it would double-register and conflict the proto interface table).

	// ---- end Conclave modules ----

	// ---- CosmWasm / CVM (task 2.2, D-077/D-078) ----
	//
	// wasmd v0.54.x on the D-077 v0.50 line. The app/cvm bindings wired here
	// are the ONLY write surface a contract has into Conclave modules: a
	// curated escrow-msg allowlist with the calling contract's own address
	// stamped as signer, and Any/Stargate passthrough disabled. MsgConclude is
	// structurally unreachable from a contract — settlement authorship stays
	// module-authority-only (§6.4); CONCLUDE is read-only to contracts through
	// the CVM query plugins.

	wasmKey := storetypes.NewKVStoreKey(wasmtypes.StoreKey)
	if err := app.RegisterStores(wasmKey); err != nil {
		panic(err)
	}
	// legacy param subspace, required by wasmd for param migrations (the live
	// params are in the wasm store; genesis default is the PERMISSIONED
	// posture — see permissionedWasmModule in wasm.go)
	wasmSubspace := app.ParamsKeeper.Subspace(wasmtypes.ModuleName)

	wasmNodeConfig, err := wasm.ReadNodeConfig(appOpts)
	if err != nil {
		panic("reading wasm node config: " + err.Error())
	}
	// the wasm store key doubles as the ante TX-counter store, wasmd's own
	// pattern (the counter feeds contract env metadata, not consensus msgs)
	wasmCounterStore := runtime.NewKVStoreService(wasmKey)

	app.WasmKeeper = wasmkeeper.NewKeeper(
		app.appCodec,
		runtime.NewKVStoreService(wasmKey),
		app.AccountKeeper,
		app.BankKeeper,
		app.StakingKeeper,
		distrkeeper.NewQuerier(app.DistrKeeper),
		app.IBCKeeper.ChannelKeeper, // ICS4Wrapper: no fee middleware in this stack
		app.IBCKeeper.ChannelKeeper,
		app.IBCKeeper.PortKeeper,
		app.ScopedWasmKeeper,
		app.TransferKeeper,
		app.MsgServiceRouter(),
		app.GRPCQueryRouter(),
		wasmVMDir(appOpts),
		wasmNodeConfig,
		wasmtypes.VMConfig{},
		wasmkeeper.BuiltInCapabilities(),
		govAuthority,
		// the CVM binding surface (task 2.2): both plugin sets are allowlists
		wasmkeeper.WithMessageEncoders(cvm.MessageEncoders()),
		wasmkeeper.WithQueryPlugins(cvm.QueryPlugins(app.appCodec, app.SessionKeeper, app.EscrowKeeper)),
	)

	ibcRouter.AddRoute(wasmtypes.ModuleName, wasm.NewIBCHandler(app.WasmKeeper, app.IBCKeeper.ChannelKeeper, app.IBCKeeper.ChannelKeeper))
	app.IBCKeeper.SetRouter(ibcRouter)

	if err := app.RegisterModules(
		permissionedWasmModule{wasm.NewAppModule(app.appCodec, &app.WasmKeeper, app.StakingKeeper, app.AccountKeeper, app.BankKeeper, app.MsgServiceRouter(), wasmSubspace)},
	); err != nil {
		panic(err)
	}

	// wasm state-sync snapshot extension: compiled code blobs live outside the
	// IAVL tree and need their own snapshot items. The manager is nil in unit
	// tests (no snapshot store configured).
	if manager := app.SnapshotManager(); manager != nil {
		if err := manager.RegisterExtensions(
			wasmkeeper.NewWasmSnapshotter(app.CommitMultiStore(), &app.WasmKeeper),
		); err != nil {
			panic(err)
		}
	}

	// ---- end CosmWasm / CVM ----

	// register streaming services
	if err := app.RegisterStreamingServices(appOpts, app.kvStoreKeys()); err != nil {
		panic(err)
	}

	// register module invariants with the crisis module (SPEC §6.1 requires
	// Conclave module invariants to be registered here as they land)
	app.ModuleManager.RegisterInvariants(app.CrisisKeeper)

	// register the cross-module accounting invariants (task 0.11): veil/solver
	// bond custody and the D-041 window/bond conservation property promoted to a
	// per-block crisis invariant. Registered at app level because they need bank
	// balance access and span more than one custom module's store.
	RegisterConservationInvariants(app.CrisisKeeper, app.BankKeeper, app.MandateKeeper, app.VeilKeeper, app.SessionKeeper)

	// simulation manager (SDK simulation integration is task 0.11)
	overrideModules := map[string]module.AppModuleSimulation{
		authtypes.ModuleName: auth.NewAppModule(app.appCodec, app.AccountKeeper, authsims.RandomGenesisAccounts, app.GetSubspace(authtypes.ModuleName)),
	}
	// x/wasm ships sim operations (random contract stores/executes) and a
	// randomized sim genesis. The determinism vector is custom-only-ops
	// (D-057/D-063) and contracts are deliberately sim-inert, so wasm is
	// EXCLUDED from the simulation manager; its sim genesis stays the app
	// default (the permissioned posture, wasm.go).
	simModules := make(map[string]interface{}, len(app.ModuleManager.Modules))
	for name, mod := range app.ModuleManager.Modules {
		if name == wasmtypes.ModuleName {
			continue
		}
		simModules[name] = mod
	}
	app.sm = module.NewSimulationManagerFromAppModules(simModules, overrideModules)
	app.sm.RegisterStoreDecoders()

	anteHandler := app.setAnteHandler(app.txConfig, wasmNodeConfig, wasmCounterStore)

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
func (app *ConclaveApp) setAnteHandler(txConfig client.TxConfig, wasmNodeConfig wasmtypes.NodeConfig, wasmCounterStore corestoretypes.KVStoreService) sdk.AnteHandler {
	anteHandler, err := NewAnteHandler(
		HandlerOptions{
			HandlerOptions: ante.HandlerOptions{
				AccountKeeper:   app.AccountKeeper,
				BankKeeper:      app.BankKeeper,
				SignModeHandler: txConfig.SignModeHandler(),
				FeegrantKeeper:  app.FeeGrantKeeper,
				SigGasConsumer:  ante.DefaultSigVerificationGasConsumer,
			},
			IBCKeeper:             app.IBCKeeper,
			MandateKeeper:         app.MandateKeeper,
			VeilKeeper:            app.VeilKeeper,
			SessionKeeper:         app.SessionKeeper,
			UnorderedManager:      app.UnorderedManager,
			WasmKeeper:            &app.WasmKeeper,
			WasmNodeConfig:        wasmNodeConfig,
			TXCounterStoreService: wasmCounterStore,
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

	// Task-1.6 vote-extension share transport (PHASE1_DESIGN §2, AUDIT F-4). The
	// Block SDK owns PrepareProposal/ProcessProposal (lane structure), so the
	// reveal transport WRAPS them: PrepareProposal prepends the prior block's
	// ExtendedCommitInfo at Txs[0], ProcessProposal validates + strips it before
	// delegating to the lane handler. ExtendVote/VerifyVoteExtension produce/check
	// the per-block reveal shares, and the PreBlocker collects them into the
	// share cache BEFORE BeginBlock. block-sdk ships no vote-extension handling,
	// so this is the only VE surface. The Phase-B cutover shipped (task 1.6 STOP
	// #2, D-069): StubScheme now serves only epoch_id == 0, and a non-zero
	// epoch_id reveals through these shares. Timing is B+2, not B+1 — ExtendVote(h)
	// runs BEFORE FinalizeBlock(h) and reads committed state through h-1, which
	// STRENGTHENS order-then-reveal: a share needs the seal COMMITTED, not merely
	// decided. (Comment corrected at Rev 3, D-090.)
	ve := NewRevealVoteExtensions(
		logger, app.VeilKeeper, app.EpochKeeper, app.StakingKeeper,
		app.ShareProvider, app.RevealShareCache,
	)
	app.RevealVoteExt = ve
	app.SetPrepareProposal(ve.PrepareProposalHandler(proposalHandler.PrepareProposalHandler()))
	app.SetProcessProposal(ve.ProcessProposalHandler(proposalHandler.ProcessProposalHandler()))
	app.SetExtendVoteHandler(ve.ExtendVoteHandler())
	app.SetVerifyVoteExtensionHandler(ve.VerifyVoteExtensionHandler())
	// chain the share-collection PreBlocker in front of the module-manager
	// PreBlock (the runtime default, which runs the upgrade module): collect the
	// injected reveal shares, then run the normal preblock.
	app.SetPreBlocker(ve.PreBlocker(app.App.PreBlocker))
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
