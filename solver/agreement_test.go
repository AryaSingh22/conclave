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
			// LegacyDec prices the solver would submit). Both cases have a coin
			// numeraire, so exactly one candidate is produced.
			candidates := cycles.Find(tc.intents)
			require.Len(t, candidates, 1, "cycle finder must produce the candidate to judge")
			m := candidates[0]

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
