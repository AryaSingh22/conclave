package types

import (
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
)

// Default parameter values (SPEC §6.1: base_gain=1.0, decay_factor=0.995,
// decay_period=daily blocks; penalty = 10× base_gain).
// DefaultDecayPeriodBlocks assumes the dev 1s block cadence (SPEC §12).
const DefaultDecayPeriodBlocks = 86_400

// DefaultParams returns the default x/sigil parameters.
func DefaultParams() Params {
	return Params{
		BaseGain:          math.LegacyOneDec(),               // 1.0
		DecayFactor:       math.LegacyNewDecWithPrec(995, 3), // 0.995
		DecayPeriodBlocks: DefaultDecayPeriodBlocks,          // daily blocks at 1s
		PenaltyMultiplier: math.LegacyNewDec(10),             // 10× base_gain
	}
}

// Validate validates the parameter set.
func (p Params) Validate() error {
	if p.BaseGain.IsNil() || p.BaseGain.IsNegative() {
		return errorsmod.Wrap(ErrInvalidParams, "base_gain must be non-negative")
	}
	if p.DecayFactor.IsNil() || p.DecayFactor.IsNegative() || p.DecayFactor.GT(math.LegacyOneDec()) {
		return errorsmod.Wrap(ErrInvalidParams, "decay_factor must be in [0, 1]")
	}
	if p.DecayPeriodBlocks == 0 {
		return errorsmod.Wrap(ErrInvalidParams, "decay_period_blocks must be positive")
	}
	if p.PenaltyMultiplier.IsNil() || p.PenaltyMultiplier.IsNegative() {
		return errorsmod.Wrap(ErrInvalidParams, "penalty_multiplier must be non-negative")
	}
	return nil
}
