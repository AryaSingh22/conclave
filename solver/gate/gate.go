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
// It imports only the pure validation primitives — never the fallback matcher
// (the matching idea is reimplemented in solver/cycles and solver/naive).
// Stateful §6.4 rules (OPEN-in-session, no-reuse, counterparty class) are the
// chain's authority; this gate covers exactly the rules that are under the
// solver's control and would otherwise cost it a 5% slash: conservation,
// price-binding, and — since task 2.14 (D-097) — the §6.5 delivery-condition
// consent binding. (solver/condition is the other package that imports
// x/session/keeper, for the same no-drift reason.)
package gate

import (
	sessionkeeper "github.com/AryaSingh22/conclave/x/session/keeper"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
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

// AcceptConditionShape returns nil iff a conditioned match has the D-074 shape
// the escrow funding can serve (keeper.CheckConditionedMatchShape — the same
// function validateSolution runs at the rule-2 position). An unconditioned
// match always passes.
//
// It is a SEPARATE self-check from AcceptConsent because it is a separate rule
// with its own error (ErrConditionUnsupportedMatch, 1426): a condition BOTH
// parties consented to is still invalid inside an n-party cycle, since the
// payment-into-escrow funding needs one unambiguous coin payment against one
// deliverable. A solver that checked only consent would propose that match and
// be slashed for it.
func AcceptConditionShape(m sessiontypes.Match) error {
	return sessionkeeper.CheckConditionedMatchShape(m)
}

// AcceptConsent returns nil iff every DataGrant leg of m satisfies what BOTH
// its parties signed under SPEC §6.5 — the delivery-condition consent binding,
// in both directions: nothing attached that a party did not sign
// (ErrConditionNotConsented, 1427) and nothing dropped that a party did sign
// (ErrConditionNotHonored, 1429).
//
// IT IS THE CHAIN'S OWN PREDICATE, not a copy of it (keeper.CheckConditionBinding,
// the same function validateSolution runs). That matters more here than anywhere
// else in this package: the rule is a per-class relation whose two halves point
// in OPPOSITE directions — a HASHLOCK timeout the buyer signed is a MAXIMUM, an
// OPTIMISTIC dispute window is a MINIMUM (D-094 R3) — and it resolves the
// per-leg IntentLeg.condition refinement before the payload-level field. A
// mirrored implementation that inverted one inequality would look correct, pass
// its own tests, and lose 5% of the bond on the first intent that used the other
// class.
//
// THIS IS THE INVARIANT BEHIND "never propose a downgrade", and it is why
// attachment (solver/condition) and this check are separate: a candidate whose
// condition was mis-attached, half-attached, or not attached at all is DROPPED
// here rather than proposed, so the guarantee does not rest on the attacher
// having covered every shape.
func AcceptConsent(m sessiontypes.Match, payloadFor func(sigil string) (veiltypes.IntentPayload, bool)) error {
	return sessionkeeper.CheckConditionBinding(m, payloadFor)
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
