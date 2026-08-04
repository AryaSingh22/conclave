package app

import (
	"errors"

	corestoretypes "cosmossdk.io/core/store"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"

	ibcante "github.com/cosmos/ibc-go/v8/modules/core/ante"
	ibckeeper "github.com/cosmos/ibc-go/v8/modules/core/keeper"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"

	"github.com/AryaSingh22/conclave/app/unordered"
	unorderedtypes "github.com/AryaSingh22/conclave/app/unordered/types"
	mandateante "github.com/AryaSingh22/conclave/x/mandate/ante"
	mandatekeeper "github.com/AryaSingh22/conclave/x/mandate/keeper"
	sessionkeeper "github.com/AryaSingh22/conclave/x/session/keeper"
	veilkeeper "github.com/AryaSingh22/conclave/x/veil/keeper"
)

// HandlerOptions extends the SDK's ante HandlerOptions with Conclave-specific
// keepers required by the ante pipeline (SPEC §2 "Ante pipeline").
type HandlerOptions struct {
	ante.HandlerOptions

	IBCKeeper        *ibckeeper.Keeper
	MandateKeeper    mandatekeeper.Keeper
	VeilKeeper       veilkeeper.Keeper
	SessionKeeper    *sessionkeeper.Keeper
	UnorderedManager unordered.Manager

	// CosmWasm (task 2.2): the wasm ante decorators need the keeper's gas
	// register, the node's wasm config (simulation gas cap), and the
	// TX-counter store service.
	WasmKeeper            *wasmkeeper.Keeper
	WasmNodeConfig        wasmtypes.NodeConfig
	TXCounterStoreService corestoretypes.KVStoreService
}

// NewAnteHandler constructs the ordered Conclave ante decorator chain.
//
// Pipeline per SPEC §2:
//
//	standard SDK decorators
//	  → UnorderedTxDecorator              (ADR-070, SPEC §6.7) — dedup + TTL
//	  → SubmitIntentPrevalidationDecorator (D-085/G2)          — reject a submit that would fail the veil msg server, before the charge
//	  → MandateDecorator                  (SPEC §6.2)          — the agent-safety chokepoint
//	  → FeeAbstractionDecorator           (SPEC §6.2)          — pay gas in USDC via conversion
//	  → ProposeSolutionSlashDecorator     (SPEC §6.4, D-097)   — the proposal-time bond slash, in the only branch that survives its own message
//
// Unordered-tx integration (SPEC §6.7, task 0.5):
//   - ExtensionOptionsDecorator accepts exactly UnorderedTxExtension;
//   - SigVerificationDecorator is the unordered-aware fork (provenance
//     pin in app/unordered/sigverify.go) — sequence checks bypassed iff
//     unordered, ordered txs byte-identical to upstream;
//   - IncrementSequenceDecorator is skipped for unordered txs;
//   - UnorderedTxDecorator runs after signature verification (dedup
//     entries only for verified txs) and before MandateDecorator, so
//     unordered txs hit window charging with zero special-casing.
//
// The standard DeductFeeDecorator is wrapped by
// ConditionalDeductFeeDecorator: when the shared SPEC §6.2 predicate says
// fee abstraction applies, fee handling defers to FeeAbstractionDecorator
// after the mandate check, so exactly one fee path runs per tx.
//
// Mandate enforcement MUST stay before any message executes (SPEC §1.1
// goal 3).
func NewAnteHandler(options HandlerOptions) (sdk.AnteHandler, error) {
	if options.AccountKeeper == nil {
		return nil, errors.New("account keeper is required for ante builder")
	}
	if options.BankKeeper == nil {
		return nil, errors.New("bank keeper is required for ante builder")
	}
	if options.SignModeHandler == nil {
		return nil, errors.New("sign mode handler is required for ante builder")
	}
	if options.IBCKeeper == nil {
		return nil, errors.New("IBC keeper is required for ante builder")
	}
	if !options.UnorderedManager.Initialized() {
		return nil, errors.New("unordered manager is required for ante builder")
	}
	// Task 2.17: without it the SPEC §6.4 proposal-time slash is silently
	// absent — the exact failure mode the task exists to close, so it fails
	// construction rather than nil-derefing at the first proposal.
	if options.SessionKeeper == nil {
		return nil, errors.New("session keeper is required for ante builder")
	}
	if options.WasmKeeper == nil {
		return nil, errors.New("wasm keeper is required for ante builder")
	}
	if options.TXCounterStoreService == nil {
		return nil, errors.New("wasm tx counter store service is required for ante builder")
	}

	anteDecorators := []sdk.AnteDecorator{
		ante.NewSetUpContextDecorator(),
		// wasm decorators directly after context setup (wasmd's own ante
		// order, task 2.2): cap simulation gas, count txs per block for the
		// contract env, expose the gas register, track contract call frames
		wasmkeeper.NewLimitSimulationGasDecorator(options.WasmNodeConfig.SimulationGasLimit),
		wasmkeeper.NewCountTXDecorator(options.TXCounterStoreService),
		wasmkeeper.NewGasRegisterDecorator(options.WasmKeeper.GetGasRegister()),
		wasmkeeper.NewTxContractsDecorator(),
		// the only accepted TxBody extension option is UnorderedTxExtension
		// (SPEC §6.7, D-026); everything else stays rejected
		ante.NewExtensionOptionsDecorator(unorderedtypes.ExtensionOptionChecker),
		ante.NewValidateBasicDecorator(),
		ante.NewTxTimeoutHeightDecorator(),
		ante.NewValidateMemoDecorator(options.AccountKeeper),
		ante.NewConsumeGasForTxSizeDecorator(options.AccountKeeper),
		mandateante.NewConditionalDeductFeeDecorator(
			options.MandateKeeper,
			ante.NewDeductFeeDecorator(options.AccountKeeper, options.BankKeeper, options.FeegrantKeeper, options.TxFeeChecker),
		),
		ante.NewSetPubKeyDecorator(options.AccountKeeper),
		ante.NewValidateSigCountDecorator(options.AccountKeeper),
		ante.NewSigGasConsumeDecorator(options.AccountKeeper, options.SigGasConsumer),
		unordered.NewSigVerificationDecorator(options.AccountKeeper, options.SignModeHandler),
		unordered.NewIncrementSequenceDecorator(ante.NewIncrementSequenceDecorator(options.AccountKeeper)),
		ibcante.NewRedundantRelayDecorator(options.IBCKeeper),
		unordered.NewUnorderedTxDecorator(options.UnorderedManager),
		// D-085/G2: reject a MsgSubmitIntent that would fail the veil msg
		// server BEFORE MandateDecorator charges the window, so no ante-passing
		// submit can strand its optimistic charge (breaking the D-059/D-064
		// conservation invariant). Runs the SAME preconditions the msg server
		// runs (one source of truth), covering nested submits like the mandate
		// walk does.
		mandateante.NewSubmitIntentPrevalidationDecorator(options.MandateKeeper, options.VeilKeeper),
		mandateante.NewMandateDecorator(options.MandateKeeper),
		mandateante.NewFeeAbstractionDecorator(options.MandateKeeper),
		// Task 2.17 (D-097 F-1): the SPEC §6.4 proposal-time bond slashes.
		// LAST on purpose. The ante's store branch is the only one in v0.50
		// that survives a failing message, so this is where a penalty for a
		// rejected proposal has to be written — and being last means no
		// decorator after it can fail and roll that write back. It never
		// rejects a slashable proposal; the msg server still does, keeping
		// 1412/1410 on the wire (D-093).
		mandateante.NewProposeSolutionSlashDecorator(options.MandateKeeper, options.SessionKeeper),
	}

	return sdk.ChainAnteDecorators(anteDecorators...), nil
}
