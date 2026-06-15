package app

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"

	mandatekeeper "github.com/AryaSingh22/conclave/x/mandate/keeper"
	sessionkeeper "github.com/AryaSingh22/conclave/x/session/keeper"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
	veilkeeper "github.com/AryaSingh22/conclave/x/veil/keeper"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// RegisterConservationInvariants registers the cross-module accounting
// invariants with the crisis module (task 0.11). These are the resting-state
// accounting properties that the §10-row-0.11 simulation checks every block.
// They are registered HERE, at the app level, rather than inside any single
// module's RegisterInvariants because each needs bank balance access and/or
// reads more than one custom module's store — keeping the module keepers free
// of an artificial cross-module dependency (the D-041 invariant in particular
// spans x/mandate window_spent and x/veil charged_spend, with no mandate→veil
// keeper edge anywhere else in the build).
//
// What CANNOT be a crisis invariant, and why (stated, not hidden):
//   - Conservation of value within a CONCLUDE (SPEC §6.4 rule 2) is a
//     *transition* property ("this settlement created/destroyed nothing"),
//     not observable from a single committed-state snapshot. It is enforced at
//     solution validation (keeper.CheckConservation) and proven by the
//     conservation fuzz; its resting consequences are covered here by the bank
//     total-supply invariant (registered by x/bank) plus the two custody
//     invariants below.
//   - Limit satisfaction / uniform price / counterparty class (rules 3/4/5) are
//     per-solution transition properties with no resting-state form.
//   - Fallback/scoring determinism (§11.3) is a cross-run property, not a state
//     predicate; it is covered by the same-seed determinism test, not crisis.
func RegisterConservationInvariants(
	ir sdk.InvariantRegistry,
	bankKeeper bankkeeper.Keeper,
	mandateKeeper mandatekeeper.Keeper,
	veilKeeper veilkeeper.Keeper,
	sessionKeeper *sessionkeeper.Keeper,
) {
	ir.RegisterRoute("conservation", "veil-bond-custody",
		VeilBondCustodyInvariant(bankKeeper, veilKeeper))
	ir.RegisterRoute("conservation", "solver-bond-custody",
		SolverBondCustodyInvariant(bankKeeper, sessionKeeper))
	ir.RegisterRoute("conservation", "mandate-commitment-conservation",
		CommitmentConservationInvariant(mandateKeeper, veilKeeper))
}

// veilLiveStates are the x/veil intent states in which the anti-spam bond is
// still escrowed in the veil module account (SPEC §5.2; D-041 refund matrix):
// the bond is refunded/slashed out only on CONCLUDED/EXPIRED/CANCELLED/INVALID.
// ROLLED is transient (it re-opens as OPEN within the same EndBlock), so it
// never rests; it is included for completeness.
func veilIsLive(s veiltypes.IntentState) bool {
	switch s {
	case veiltypes.INTENT_STATE_SUBMITTED,
		veiltypes.INTENT_STATE_SEALED_IN_SESSION,
		veiltypes.INTENT_STATE_OPEN,
		veiltypes.INTENT_STATE_ROLLED:
		return true
	default:
		return false
	}
}

// VeilBondCustodyInvariant asserts the resting consequence of the §6.3 bond
// escrow: the veil module account's balance, per denom, equals the sum of the
// bonds of all intents still in a live (bond-escrowed) state. This is the
// registerable proxy for "no bond is created or destroyed" — the part of value
// conservation that IS a resting-state predicate.
func VeilBondCustodyInvariant(bankKeeper bankkeeper.Keeper, veilKeeper veilkeeper.Keeper) sdk.Invariant {
	moduleAddr := authtypes.NewModuleAddress(veiltypes.ModuleName)
	return func(ctx sdk.Context) (string, bool) {
		expected := sdk.NewCoins()
		if err := veilKeeper.IterateIntentRecords(ctx, func(rec veiltypes.IntentRecord) bool {
			if veilIsLive(rec.State) && rec.Intent.Bond.IsValid() && rec.Intent.Bond.IsPositive() {
				expected = expected.Add(rec.Intent.Bond)
			}
			return false
		}); err != nil {
			return sdk.FormatInvariant("conservation", "veil-bond-custody", err.Error()), true
		}

		var msg string
		broken := false
		// every denom that should be escrowed must be present in the module
		// account at exactly the summed amount; check the bond denom(s) seen.
		for _, c := range expected {
			held := bankKeeper.GetBalance(ctx, moduleAddr, c.Denom)
			if !held.Amount.Equal(c.Amount) {
				broken = true
				msg += fmt.Sprintf("veil module holds %s but live bonds sum to %s for denom %s\n",
					held, c, c.Denom)
			}
		}
		return sdk.FormatInvariant("conservation", "veil-bond-custody", msg), broken
	}
}

// SolverBondCustodyInvariant asserts the §6.4 solver-registry custody property:
// the session module account holds at least the sum of every registered
// solver's bond. The relation is ≥, not ==, because the same module account is
// also the settlement escrow account and the protocol fee pool (it transiently
// routes coin legs and accrues skim), so its balance is bounded below — never
// above — by the solver bonds it custodies.
func SolverBondCustodyInvariant(bankKeeper bankkeeper.Keeper, sessionKeeper *sessionkeeper.Keeper) sdk.Invariant {
	moduleAddr := authtypes.NewModuleAddress(sessiontypes.ModuleName)
	return func(ctx sdk.Context) (string, bool) {
		bonded := sdk.NewCoins()
		if err := sessionKeeper.IterateSolvers(ctx, func(s sessiontypes.Solver) bool {
			// ACTIVE and UNBONDING solvers both still have their bond escrowed
			// (it is returned only by the EndBlock unbond sweep at maturity).
			if s.Status != sessiontypes.SOLVER_STATUS_UNSPECIFIED {
				bonded = bonded.Add(s.Bond...)
			}
			return false
		}); err != nil {
			return sdk.FormatInvariant("conservation", "solver-bond-custody", err.Error()), true
		}

		var msg string
		broken := false
		for _, c := range bonded {
			held := bankKeeper.GetBalance(ctx, moduleAddr, c.Denom)
			if held.Amount.LT(c.Amount) {
				broken = true
				msg += fmt.Sprintf("session module holds %s < solver bonds %s for denom %s\n",
					held, c, c.Denom)
			}
		}
		return sdk.FormatInvariant("conservation", "solver-bond-custody", msg), broken
	}
}

// CommitmentConservationInvariant promotes the D-041 window/bond conservation
// property — proven across tasks 0.4/0.7/0.8 only as a rapid fuzz property — to
// a per-block crisis invariant (task 0.11). After the D-059 split, the
// conserved quantity is the mandate's LIFETIME outstanding_commitment (NOT the
// per-period window_spent, which conflated a lifetime balance with a per-period
// consumption — the task-0.11 finding). For every grantee holding an active
// mandate:
//
//	outstanding_commitment == Σ charged_spend over that grantee's intents not in
//	                          {EXPIRED, CANCELLED, CONCLUDED}
//
// This holds GLOBALLY by construction across every window rollover, amendment,
// and reissue, because outstanding_commitment moves only on the intent charge
// (ChargeWindow) and the intent's terminate-time release (DecommitIntent), and
// is carried — never zeroed — by a reset/amend/reissue (D-059).
//
// It IS expressible as a crisis invariant because crisis runs FIRST in the
// EndBlock order (app_config.go: crisis → … → session → veil): at check time an
// intent about to expire/conclude is still counted AND still committed, and
// after the session/veil sweep it is uncounted AND decommitted — equality holds
// at the crisis-check boundary for the expire/conclude path.
//
// Intents owned by root keys (no mandate) are excluded: their charged_spend is
// persisted (D-041) but never entered a mandate, so there is nothing to
// reconcile — only mandate-bound grantees are checked.
func CommitmentConservationInvariant(mandateKeeper mandatekeeper.Keeper, veilKeeper veilkeeper.Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		// owner (bech32) → Σ charged_spend over live (non-terminated) intents
		charged := make(map[string]sdk.Coins)
		if err := veilKeeper.IterateIntentRecords(ctx, func(rec veiltypes.IntentRecord) bool {
			switch rec.State {
			case veiltypes.INTENT_STATE_EXPIRED,
				veiltypes.INTENT_STATE_CANCELLED,
				veiltypes.INTENT_STATE_CONCLUDED:
				return false
			}
			charged[rec.Owner] = charged[rec.Owner].Add(rec.ChargedSpend...)
			return false
		}); err != nil {
			return sdk.FormatInvariant("conservation", "mandate-commitment-conservation", err.Error()), true
		}

		var msg string
		broken := false
		// Walk every active mandate (grantee index) and reconcile its
		// outstanding_commitment against the charged map. A grantee with an
		// active mandate but no live intents must show outstanding_commitment==0.
		if err := mandateKeeper.IterateGranteeIndex(ctx, func(chamberID uint32, grantee sdk.AccAddress, _ []byte) bool {
			m, found, err := mandateKeeper.ActiveMandateByGrantee(ctx, chamberID, grantee)
			if err != nil || !found {
				return false
			}
			want := charged[grantee.String()]
			if want == nil {
				want = sdk.NewCoins()
			}
			if !m.OutstandingCommitment.Equal(want) {
				broken = true
				msg += fmt.Sprintf("grantee %s: outstanding_commitment=%s but Σ charged_spend(live)=%s (mandate %s)\n",
					grantee, m.OutstandingCommitment, want, m.Id)
			}
			return false
		}); err != nil {
			return sdk.FormatInvariant("conservation", "mandate-commitment-conservation", err.Error()), true
		}
		return sdk.FormatInvariant("conservation", "mandate-commitment-conservation", msg), broken
	}
}
