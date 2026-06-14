// Package gate is the reference solver's pre-submission self-check. It does NOT
// reimplement the chain's price-binding rule — it CALLS it: the exact, pure,
// stateless functions the chain runs in validateSolution
// (keeper.CheckConservation, keeper.CheckPriceBinding). This is the single
// source of truth for price representability (D-047/D-055): the rule deciding
// "which clearing prices are exactly representable in LegacyDec, hence which
// cycles/swaps clear" is identical on both sides BY CONSTRUCTION — the solver
// can never believe a price-binding-valid solution the chain would reject, or
// skip one the chain would accept.
//
// This is the only solver package that imports x/session/keeper, and it imports
// only the pure validation primitives — never the fallback matcher (the matching
// idea is reimplemented in solver/cycles and solver/naive). Stateful §6.4 rules
// (OPEN-in-session, no-reuse, counterparty class) are the chain's authority;
// this gate covers exactly the conservation + price-binding rules that are under
// the solver's control and would otherwise cost it a 5% slash.
package gate

import (
	sessionkeeper "github.com/AryaSingh22/conclave/x/session/keeper"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
)

// Accept returns nil iff match m passes the chain's pure §6.4 checks a solver
// can verify off-chain: rule 2 (conservation / structural leg validity) and the
// binding half of rules 3/4 (every leg clears at the stated prices, exact-zero
// residual). Equivalent to what validateSolution runs minus the ctx-dependent
// rules. Returns the chain's own error so callers can log the precise reason.
func Accept(m sessiontypes.Match) error {
	flows, err := sessionkeeper.CheckConservation([]sessiontypes.Match{m})
	if err != nil {
		return err
	}
	return sessionkeeper.CheckPriceBinding(m, flows)
}

// PriceBindingOK is the boolean form of the price-binding self-check: it reports
// whether m clears at its stated prices under the chain's exact-zero rule
// (keeper.CheckPriceBinding over keeper.CheckConservation flows). This is the
// function whose verdict the agreement test pins against the chain's
// CheckPriceBinding for the same Match (D-055).
func PriceBindingOK(m sessiontypes.Match) bool {
	flows, err := sessionkeeper.CheckConservation([]sessiontypes.Match{m})
	if err != nil {
		return false
	}
	return sessionkeeper.CheckPriceBinding(m, flows) == nil
}
