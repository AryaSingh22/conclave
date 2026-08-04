package ante

import (
	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/mandate/keeper"
	"github.com/AryaSingh22/conclave/x/mandate/types"
)

// FeeAbstractionDecorator implements SPEC §6.2 "Fee abstraction": it runs
// after MandateDecorator, converts a permitted non-native fee denom at the
// fixed param rate, and settles it through the genesis-funded CLV fee pool
// (no mint, no burn). The Keeper.FeeAbstractionMandate predicate it shares
// with ConditionalDeductFeeDecorator guarantees exactly one of the two fee
// paths handles any tx.
type FeeAbstractionDecorator struct {
	keeper keeper.Keeper
}

// NewFeeAbstractionDecorator creates the SPEC §6.2 fee abstraction decorator.
func NewFeeAbstractionDecorator(k keeper.Keeper) FeeAbstractionDecorator {
	return FeeAbstractionDecorator{keeper: k}
}

var _ sdk.AnteDecorator = FeeAbstractionDecorator{}

// AnteHandle converts and settles the fee when abstraction applies, and
// polices the mandate's fee_denoms otherwise: a mandate-bound fee payer may
// pay gas only in the native denom or a single coin of a listed fee denom
// (D-022 deny-by-default, consistent with the spend-ceiling posture).
func (d FeeAbstractionDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return next(ctx, tx, simulate)
	}
	fee := feeTx.GetFee()
	payer := sdk.AccAddress(feeTx.FeePayer())

	if m, applies := d.keeper.FeeAbstractionMandate(ctx, payer, fee); applies {
		if err := d.keeper.ExecuteFeeAbstraction(ctx, m, fee); err != nil {
			return ctx, err
		}
		return next(ctx, tx, simulate)
	}

	if !fee.IsZero() {
		if _, found, err := d.keeper.ActiveMandateByGrantee(ctx, keeper.Phase0Chamber(), payer); err != nil {
			return ctx, err
		} else if found {
			// the payer is mandate-bound and abstraction did not apply:
			// only an all-native fee is acceptable on the standard path
			params, err := d.keeper.GetParams(ctx)
			if err != nil {
				return ctx, err
			}
			for _, coin := range fee {
				if coin.Denom != params.FeePool.Denom {
					return ctx, errorsmod.Wrapf(types.ErrFeeDenomNotPermitted,
						"mandate-bound fee payer %s cannot pay gas in %s", payer, coin.Denom)
				}
			}
		}
	}

	return next(ctx, tx, simulate)
}

// ConditionalDeductFeeDecorator wraps the standard DeductFeeDecorator and
// skips it when the shared SPEC §6.2 predicate says fee abstraction will
// handle the fee after MandateDecorator. Both decorators consult
// Keeper.FeeAbstractionMandate, so no tx is double-charged and none
// escapes fee payment.
//
// CONCLAVE-TODO(phase-3): skipped txs carry the default 0 mempool priority;
// per-lane fee/priority handling (SPEC §6.7) still does not replace it. Owned
// by task 3.3b (SDK-family migration), because 3.3b's block-sdk disposition —
// fork-and-maintain vs an app-side PrepareProposal lane implementation — is
// what decides who owns lane priority at all, and settling it before that
// settles it twice. RE-POINTED from "task 3.3" at task 3.3a (D-099): D-096
// SPLIT 3.3 into 3.3a/3.3b, and a split leaves the OLD referent behind — this
// marker named a task that had stopped existing, which is Rule 6 sub-rule 3's
// worst case (reads as pending, owned by nobody). Found by the F-13 marker
// check the same task built. RE-TAGGED from phase-0 at PHASE2_DESIGN Rev 3 (D-090):
// the marker deferred this to task 0.6, which completed in Phase 0, so it was
// an unowned debt rather than a pending one (AUDIT F-8's class). Mempool
// ordering only — no consensus or authorization property depends on it.
type ConditionalDeductFeeDecorator struct {
	keeper keeper.Keeper
	inner  sdk.AnteDecorator
}

// NewConditionalDeductFeeDecorator wraps the standard fee deduction
// decorator with the fee-abstraction skip.
func NewConditionalDeductFeeDecorator(k keeper.Keeper, inner sdk.AnteDecorator) ConditionalDeductFeeDecorator {
	return ConditionalDeductFeeDecorator{keeper: k, inner: inner}
}

var _ sdk.AnteDecorator = ConditionalDeductFeeDecorator{}

// AnteHandle defers to FeeAbstractionDecorator when the shared predicate
// applies, otherwise runs the standard DeductFeeDecorator.
func (d ConditionalDeductFeeDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if feeTx, ok := tx.(sdk.FeeTx); ok {
		payer := sdk.AccAddress(feeTx.FeePayer())
		if _, applies := d.keeper.FeeAbstractionMandate(ctx, payer, feeTx.GetFee()); applies {
			return next(ctx, tx, simulate)
		}
	}
	return d.inner.AnteHandle(ctx, tx, simulate, next)
}
