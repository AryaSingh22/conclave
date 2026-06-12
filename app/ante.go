package app

import (
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"

	ibcante "github.com/cosmos/ibc-go/v8/modules/core/ante"
	ibckeeper "github.com/cosmos/ibc-go/v8/modules/core/keeper"

	mandateante "github.com/AryaSingh22/conclave/x/mandate/ante"
	mandatekeeper "github.com/AryaSingh22/conclave/x/mandate/keeper"
	mandatetypes "github.com/AryaSingh22/conclave/x/mandate/types"
)

// HandlerOptions extends the SDK's ante HandlerOptions with Conclave-specific
// keepers required by the ante pipeline (SPEC §2 "Ante pipeline").
type HandlerOptions struct {
	ante.HandlerOptions

	IBCKeeper     *ibckeeper.Keeper
	MandateKeeper mandatekeeper.Keeper
	PriceSource   mandatetypes.PriceSource
}

// NewAnteHandler constructs the ordered Conclave ante decorator chain.
//
// Pipeline per SPEC §2:
//
//	standard SDK decorators
//	  → UnorderedTxDecorator   (ADR-070 pattern, SPEC §6.7)  — task 0.5
//	  → MandateDecorator       (SPEC §6.2)                   — the agent-safety chokepoint
//	  → FeeAbstractionDecorator (SPEC §6.2)                  — pay gas in USDC via conversion
//
// The standard DeductFeeDecorator is wrapped by
// ConditionalDeductFeeDecorator: when the shared SPEC §6.2 predicate says
// fee abstraction applies, fee handling defers to FeeAbstractionDecorator
// after the mandate check, so exactly one fee path runs per tx.
//
// CONCLAVE-TODO(phase-0): task 0.5 inserts the hardened UnorderedTxDecorator
// before MandateDecorator. Mandate enforcement MUST stay before any message
// executes (SPEC §1.1 goal 3).
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
	if options.PriceSource == nil {
		return nil, errors.New("price source is required for ante builder")
	}

	anteDecorators := []sdk.AnteDecorator{
		ante.NewSetUpContextDecorator(),
		ante.NewExtensionOptionsDecorator(options.ExtensionOptionChecker),
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
		ante.NewSigVerificationDecorator(options.AccountKeeper, options.SignModeHandler),
		ante.NewIncrementSequenceDecorator(options.AccountKeeper),
		ibcante.NewRedundantRelayDecorator(options.IBCKeeper),
		mandateante.NewMandateDecorator(options.MandateKeeper, options.PriceSource),
		mandateante.NewFeeAbstractionDecorator(options.MandateKeeper),
	}

	return sdk.ChainAnteDecorators(anteDecorators...), nil
}
