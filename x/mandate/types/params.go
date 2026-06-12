package types

import (
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Default parameter values (SPEC §6.2, D-007). The dev conversion rate is
// 1 uclv per uusdc; the fee pool float is 1000 CLV. Phase 2 replaces the
// fixed rate with a TWAP from session clearing prices (SPEC §10 task 2.5).
const (
	// DefaultFeePoolDenom is the native gas denom the fee pool floats
	// (SPEC §0: uclv). The module reads the native denom from
	// Params.FeePool.Denom everywhere, never from a hardcoded constant.
	DefaultFeePoolDenom = "uclv"

	// DefaultMaxMsgNestingDepth caps recursive nested-msg unwrapping in the
	// ante handler (SPEC §6.2 fuzz targets: "cap recursion at 8").
	DefaultMaxMsgNestingDepth = 8
)

// DefaultParams returns the default x/mandate parameters.
func DefaultParams() Params {
	return Params{
		FeeConversionRate:  math.LegacyOneDec(),                                          // 1 uclv per uusdc (dev)
		FeePool:            sdk.NewCoin(DefaultFeePoolDenom, math.NewInt(1_000_000_000)), // 1000 CLV float
		MaxMsgNestingDepth: DefaultMaxMsgNestingDepth,
	}
}

// Validate validates the parameter set.
func (p Params) Validate() error {
	if p.FeeConversionRate.IsNil() || !p.FeeConversionRate.IsPositive() {
		return errorsmod.Wrap(ErrInvalidParams, "fee_conversion_rate must be positive")
	}
	if err := p.FeePool.Validate(); err != nil {
		return errorsmod.Wrapf(ErrInvalidParams, "fee_pool: %s", err)
	}
	if p.MaxMsgNestingDepth == 0 {
		return errorsmod.Wrap(ErrInvalidParams, "max_msg_nesting_depth must be positive")
	}
	return nil
}
