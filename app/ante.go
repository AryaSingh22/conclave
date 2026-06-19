package app

import (
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"

	ibcante "github.com/cosmos/ibc-go/v8/modules/core/ante"
	ibckeeper "github.com/cosmos/ibc-go/v8/modules/core/keeper"

	"github.com/AryaSingh22/conclave/app/unordered"
	unorderedtypes "github.com/AryaSingh22/conclave/app/unordered/types"
	mandateante "github.com/AryaSingh22/conclave/x/mandate/ante"
	mandatekeeper "github.com/AryaSingh22/conclave/x/mandate/keeper"
)

// HandlerOptions extends the SDK's ante HandlerOptions with Conclave-specific
// keepers required by the ante pipeline (SPEC §2 "Ante pipeline").
type HandlerOptions struct {
	ante.HandlerOptions

	IBCKeeper        *ibckeeper.Keeper
	MandateKeeper    mandatekeeper.Keeper
	UnorderedManager unordered.Manager
}

// NewAnteHandler constructs the ordered Conclave ante decorator chain.
//
// Pipeline per SPEC §2:
//
//	standard SDK decorators
//	  → UnorderedTxDecorator   (ADR-070 pattern, SPEC §6.7)  — dedup + TTL
//	  → MandateDecorator       (SPEC §6.2)                   — the agent-safety chokepoint
//	  → FeeAbstractionDecorator (SPEC §6.2)                  — pay gas in USDC via conversion
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

	anteDecorators := []sdk.AnteDecorator{
		ante.NewSetUpContextDecorator(),
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
		mandateante.NewMandateDecorator(options.MandateKeeper),
		mandateante.NewFeeAbstractionDecorator(options.MandateKeeper),
	}

	return sdk.ChainAnteDecorators(anteDecorators...), nil
}
