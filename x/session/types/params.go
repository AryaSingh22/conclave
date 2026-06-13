package types

import (
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Default parameter values (SPEC §6.4 params, D-007, D1/D3).
const (
	// DefaultBondDenom is the solver bond denom (SPEC §0: uclv). Read from
	// Params.SolverMinBond.Denom everywhere, never hardcoded.
	DefaultBondDenom = "uclv"

	DefaultSessionLengthBlocks    = 3      // SPEC §6.4 dev default; §14 D1
	DefaultSolutionWindowBlocks   = 2      // SPEC §6.4 dev default
	DefaultSolverFeeBps           = 500    // SPEC §14 D3
	DefaultMaxGraphLegs           = 256    // SPEC §6.4
	DefaultMaxSolutionsPerSession = 16     // SPEC §6.4
	DefaultSolverUnbondBlocks     = 86_400 // 1 day at the §12 dev cadence (1s blocks)
	DefaultInvalidSolutionSlashBp = 500    // SPEC §6.4 invalid solution = 5%; D-007
	DefaultEquivocationSlashBps   = 2000   // SPEC §6.4 equivocation = 20%; D-007
	DefaultSurplusSkimBps         = 50     // SPEC §6.4 "small skim (param)"; dev placeholder (D6)

	// bpsDenominator converts basis points to a fraction.
	bpsDenominator = 10_000
)

// DefaultSolverMinBond is the dev minimum solver bond: 100 CLV.
func DefaultSolverMinBond() sdk.Coin {
	return sdk.NewCoin(DefaultBondDenom, math.NewInt(100_000_000))
}

// DefaultParams returns the default x/session parameters.
func DefaultParams() Params {
	return Params{
		SessionLengthBlocks:     DefaultSessionLengthBlocks,
		SolutionWindowBlocks:    DefaultSolutionWindowBlocks,
		SolverMinBond:           DefaultSolverMinBond(),
		SolverFeeBps:            DefaultSolverFeeBps,
		MaxGraphLegs:            DefaultMaxGraphLegs,
		MaxSolutionsPerSession:  DefaultMaxSolutionsPerSession,
		SolverUnbondBlocks:      DefaultSolverUnbondBlocks,
		InvalidSolutionSlashBps: DefaultInvalidSolutionSlashBp,
		EquivocationSlashBps:    DefaultEquivocationSlashBps,
		SurplusSkimBps:          DefaultSurplusSkimBps,
	}
}

// Validate validates the parameter set.
func (p Params) Validate() error {
	if p.SessionLengthBlocks == 0 {
		return errorsmod.Wrap(ErrInvalidParams, "session_length_blocks must be positive")
	}
	if p.SolutionWindowBlocks == 0 {
		return errorsmod.Wrap(ErrInvalidParams, "solution_window_blocks must be positive")
	}
	if err := p.SolverMinBond.Validate(); err != nil {
		return errorsmod.Wrapf(ErrInvalidParams, "solver_min_bond: %s", err)
	}
	if !p.SolverMinBond.Amount.IsPositive() {
		return errorsmod.Wrap(ErrInvalidParams, "solver_min_bond must be positive")
	}
	if p.MaxGraphLegs == 0 {
		return errorsmod.Wrap(ErrInvalidParams, "max_graph_legs must be positive")
	}
	if p.MaxSolutionsPerSession == 0 {
		return errorsmod.Wrap(ErrInvalidParams, "max_solutions_per_session must be positive")
	}
	if p.SolverUnbondBlocks == 0 {
		return errorsmod.Wrap(ErrInvalidParams, "solver_unbond_blocks must be positive")
	}
	bpsFields := []struct {
		name string
		bps  uint32
	}{
		{"solver_fee_bps", p.SolverFeeBps},
		{"invalid_solution_slash_bps", p.InvalidSolutionSlashBps},
		{"equivocation_slash_bps", p.EquivocationSlashBps},
		{"surplus_skim_bps", p.SurplusSkimBps},
	}
	for _, f := range bpsFields {
		if f.bps > bpsDenominator {
			return errorsmod.Wrapf(ErrInvalidParams, "%s %d exceeds %d", f.name, f.bps, bpsDenominator)
		}
	}
	return nil
}
