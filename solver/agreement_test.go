package solver_test

import (
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/solver"
	"github.com/AryaSingh22/conclave/solver/cycles"
	"github.com/AryaSingh22/conclave/solver/gate"
	"github.com/AryaSingh22/conclave/solver/match"
	"github.com/AryaSingh22/conclave/solver/naive"
	sessionkeeper "github.com/AryaSingh22/conclave/x/session/keeper"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

func coinLeg(denom string, amt int64) veiltypes.IntentLeg {
	c := sdk.NewInt64Coin(denom, amt)
	return veiltypes.IntentLeg{Asset: &veiltypes.IntentLeg_Coin{Coin: &c}}
}

func svcLeg(schema string, units int64) veiltypes.IntentLeg {
	v := veiltypes.ServiceVoucher{Schema: schema, Units: math.NewInt(units)}
	return veiltypes.IntentLeg{Asset: &veiltypes.IntentLeg_Svc{Svc: &v}}
}

func dataLeg(tag string) veiltypes.IntentLeg {
	h := sha256.Sum256([]byte(tag))
	return veiltypes.IntentLeg{Asset: &veiltypes.IntentLeg_Data{Data: &veiltypes.DataGrant{ContentHash: h[:]}}}
}

func oi(id, sigil string, give, want veiltypes.IntentLeg) match.OpenIntent {
	return match.OpenIntent{ID: id, Sigil: sigil, Give: []veiltypes.IntentLeg{give}, Want: []veiltypes.IntentLeg{want}}
}

// chainVerdict is the chain's authoritative price-binding decision for a match:
// keeper.CheckPriceBinding over keeper.CheckConservation flows — the exact code
// validateSolution runs.
func chainVerdict(t *testing.T, m sessiontypes.Match) bool {
	t.Helper()
	flows, err := sessionkeeper.CheckConservation([]sessiontypes.Match{m})
	if err != nil {
		return false
	}
	return sessionkeeper.CheckPriceBinding(m, flows) == nil
}

// representableCycle is the §1.3 cycle: alice 100 uusdc → dataset D; bob D → 10
// cmpt; carol 10 cmpt → 100 uusdc. Every clearing price is an exact integer.
func representableCycle() []match.OpenIntent {
	return []match.OpenIntent{
		oi("alice", "sigil-alice", coinLeg("uusdc", 100), dataLeg("dataset-D")),
		oi("bob", "sigil-bob", dataLeg("dataset-D"), svcLeg("cmpt", 10)),
		oi("carol", "sigil-carol", svcLeg("cmpt", 10), coinLeg("uusdc", 100)),
	}
}

// nonRepresentableCycle implies an intermediate price of 100/3 (alice wants 3
// svc/a for 100 uusdc), which is not exactly representable in LegacyDec.
func nonRepresentableCycle() []match.OpenIntent {
	return []match.OpenIntent{
		oi("alice", "sigil-alice", coinLeg("uusdc", 100), svcLeg("a", 3)),
		oi("bob", "sigil-bob", svcLeg("a", 3), svcLeg("b", 7)),
		oi("carol", "sigil-carol", svcLeg("b", 7), coinLeg("uusdc", 100)),
	}
}

// TestPriceBindingSelfCheckAgreesWithChain is the D-055 acceptance: the solver's
// price-binding self-check (gate.PriceBindingOK) and the chain's
// CheckPriceBinding return the IDENTICAL accept/reject verdict for the same
// Match — including a cycle with a non-terminating intermediate price (100/3).
// Because the gate calls the chain's exact function (single source of truth),
// they cannot diverge; this test pins that and guards against any future split.
func TestPriceBindingSelfCheckAgreesWithChain(t *testing.T) {
	cases := []struct {
		name    string
		intents []match.OpenIntent
		want    bool // expected verdict (both sides must agree on this)
	}{
		{"representable cycle clears", representableCycle(), true},
		{"non-representable 100/3 cycle rejected", nonRepresentableCycle(), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// the cycle finder constructs the candidate Match (with the exact
			// LegacyDec prices the solver would submit). Both cases have exactly
			// ONE coin give-asset, so each cycle group holds a single anchoring.
			groups := cycles.Find(tc.intents)
			require.Len(t, groups, 1, "cycle finder must produce the candidate to judge")
			require.Len(t, groups[0], 1, "one coin give-asset ⇒ one numeraire anchoring")
			m := groups[0][0]

			solverVerdict := gate.PriceBindingOK(m)
			chainV := chainVerdict(t, m)

			require.Equal(t, chainV, solverVerdict,
				"solver self-check and chain CheckPriceBinding must agree on the same Match")
			require.Equal(t, tc.want, solverVerdict, "verdict")
		})
	}
}

// TestSolveClearsTheCycleOnlyWhenRepresentable proves the full pipeline: the
// representable §1.3 cycle clears as one 3-leg match the chain accepts, while
// the 100/3 cycle clears nothing (the gate drops it and there is no pairwise
// fallback for a cycle).
func TestSolveClearsTheCycleOnlyWhenRepresentable(t *testing.T) {
	got := solver.Solve(representableCycle())
	require.Len(t, got, 1, "representable cycle must clear as one match")
	require.Len(t, got[0].IntentIds, 3, "all three intents in the cycle")
	require.Len(t, got[0].Legs, 3, "three settlement legs")
	require.NoError(t, gate.Accept(got[0]), "the cleared match must pass the chain gate")

	require.Empty(t, solver.Solve(nonRepresentableCycle()),
		"a non-representable cycle clears nothing")
}

// TestNaiveCannotClearCycle is the core proof that solver/cycles earns its place
// (SPEC §1.3, build plan): the greedy pairwise matcher CANNOT clear a 3-party
// cycle — no two parties are mutually fillable — while the cycle finder can.
func TestNaiveCannotClearCycle(t *testing.T) {
	cyc := representableCycle()
	require.Empty(t, naive.Find(cyc), "pairwise matcher cannot clear a 3-party cycle")
	require.Len(t, cycles.Find(cyc), 1, "the cycle finder clears it")
}

// --- task 2.6 item 7: partial-fill cycles + alternate numeraire anchoring ---

// altAnchorCycle is the item-7 fixture for ALTERNATE NUMERAIRE ANCHORING. Two
// coin give-assets, chosen so the lexicographically-first anchor (the pre-2.6
// default) cannot price the cycle exactly while the other can:
//
//	alice 1 uaaa → 3 svc/x ; bob 3 svc/x → 3 uzzz ; carol 3 uzzz → 1 uaaa
//
// Anchored at "uaaa": p[svc/x] = 1/3 — not representable, the chain gate rejects.
// Anchored at "uzzz": p[uaaa] = 3, p[svc/x] = 1 — exact, every party nets zero.
func altAnchorCycle() []match.OpenIntent {
	return []match.OpenIntent{
		oi("alice", "sigil-alice", coinLeg("uaaa", 1), svcLeg("x", 3)),
		oi("bob", "sigil-bob", svcLeg("x", 3), coinLeg("uzzz", 3)),
		oi("carol", "sigil-carol", coinLeg("uzzz", 3), coinLeg("uaaa", 1)),
	}
}

// TestCyclesAlternateNumeraireAnchoring is the item-7 acceptance: the finder now
// offers BOTH anchorings of the cycle, in sorted anchor order, and the pipeline
// clears it through the second — a cycle that was silently dropped before 2.6.
// The verdict on each anchoring is still the CHAIN's (D-055), asserted here
// against keeper.CheckPriceBinding itself, not a solver-local rule.
func TestCyclesAlternateNumeraireAnchoring(t *testing.T) {
	groups := cycles.Find(altAnchorCycle())
	require.Len(t, groups, 1, "one cycle")
	require.Len(t, groups[0], 2, "two coin give-assets ⇒ two anchorings")

	// the anchorings are ordered by anchor key: "uaaa" first (the pre-2.6
	// default), then "uzzz".
	require.Equal(t, "uaaa", groups[0][0].ClearingPrices[0].Quote)
	require.Equal(t, "uzzz", groups[0][1].ClearingPrices[0].Quote)

	require.False(t, chainVerdict(t, groups[0][0]),
		"the default anchoring is not exactly representable — the chain rejects it")
	require.True(t, chainVerdict(t, groups[0][1]),
		"the alternate anchoring clears exactly")

	got := solver.Solve(altAnchorCycle())
	require.Len(t, got, 1, "the pipeline clears the cycle via the alternate anchor")
	require.Len(t, got[0].IntentIds, 3)
	require.NoError(t, gate.Accept(got[0]), "the cleared match passes the chain gate")
	require.Equal(t, "uzzz", got[0].ClearingPrices[0].Quote, "cleared at the alternate anchor")
}

// partialFillCycle is the item-7 fixture for PARTIAL-FILL cycles: bob offers 20
// svc/x but alice only wants 10, and bob signed allow_partial — so bob delivers
// 10 and KEEPS 10. The full-give terms could not clear this pair at all under
// the pre-2.6 exact-fill provider rule (20 ≠ 10).
func partialFillCycle(bobAllowsPartial bool) []match.OpenIntent {
	alice := oi("alice", "sigil-alice", coinLeg("uusdc", 100), svcLeg("x", 10))
	bob := oi("bob", "sigil-bob", svcLeg("x", 20), coinLeg("uusdc", 100))
	bob.AllowPartial = bobAllowsPartial
	return []match.OpenIntent{alice, bob}
}

// TestCyclesPartialFill: with allow_partial signed, the over-offering provider
// fills its consumer's floor and the settlement leg carries the FILLED quantity
// (10), never the whole offer (20) — the remainder is simply not moved.
func TestCyclesPartialFill(t *testing.T) {
	groups := cycles.Find(partialFillCycle(true))
	require.Len(t, groups, 1, "the partial fill closes the 2-cycle")
	m := groups[0][0]
	require.True(t, chainVerdict(t, m), "the partial clearing nets zero at its prices")

	var svcLegAmt math.Int
	for _, leg := range m.Legs {
		if v := leg.GetSvc(); v != nil {
			svcLegAmt = v.Units
		}
	}
	require.False(t, svcLegAmt.IsNil(), "the svc leg must be present")
	require.Equal(t, math.NewInt(10).String(), svcLegAmt.String(),
		"the leg carries the FILLED amount, not the 20 offered")

	require.Len(t, solver.Solve(partialFillCycle(true)), 1, "the pipeline clears it")
}

// TestCyclesPartialFillRequiresAllowPartial is item 7's load-bearing guard: an
// intent that did NOT sign allow_partial is fill-or-kill, so the finder must not
// build a cycle that shrinks its give. Remove the allow_partial condition in
// node.canFill and this goes red (the fill-or-kill offer gets split).
func TestCyclesPartialFillRequiresAllowPartial(t *testing.T) {
	require.Empty(t, cycles.Find(partialFillCycle(false)),
		"a fill-or-kill intent must never be partially filled")
	require.Empty(t, solver.Solve(partialFillCycle(false)))
}

// TestNaiveClearsPairwiseSwap confirms the pairwise matcher does clear a
// mutually-fillable 2-party swap (the fallback-equivalent liveness path).
func TestNaiveClearsPairwiseSwap(t *testing.T) {
	swap := []match.OpenIntent{
		oi("x", "sigil-x", coinLeg("uusdc", 50), svcLeg("cmpt", 5)),
		oi("y", "sigil-y", svcLeg("cmpt", 5), coinLeg("uusdc", 50)),
	}
	got := naive.Find(swap)
	require.Len(t, got, 1, "mutually-fillable swap clears")
	require.NoError(t, gate.Accept(got[0]), "the swap passes the chain gate")
}
