// Package cycles is the reference solver's circular-trade finder (SPEC §4
// solver/cycles/). It detects directed trade cycles A→B→C→A that the pairwise
// matcher (solver/naive) structurally cannot clear, and emits an n-party Match
// for each — the part of the solver that earns real surplus and proves the
// N-party value proposition (SPEC §1.1 goal 2, §1.3).
//
// It is PURE (proto types + math only; no x/session/keeper): the matching idea
// is reimplemented off-chain, separate from the in-consensus fallback. The one
// rule that must not drift — whether the constructed clearing prices are exactly
// representable in LegacyDec (D-047) — is NOT decided here: cycles proposes a
// candidate Match with numeraire-anchored prices, and the shared chain validator
// (solver/gate → keeper.CheckPriceBinding) is the sole arbiter of acceptance, so
// the skip/accept decision is identical to the chain's by construction (D-055).
package cycles

import (
	"sort"

	"cosmossdk.io/math"

	"github.com/AryaSingh22/conclave/solver/match"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// node is an OPEN intent reduced to its single give/want legs and their
// asset-key/amounts (Phase-0 single give/want, mirroring the fallback matcher).
type node struct {
	oi      match.OpenIntent
	give    veiltypes.IntentLeg
	giveKey string
	giveAmt math.Int
	wantKey string
	wantAmt math.Int
}

// Find returns one candidate Match per disjoint trade cycle it discovers,
// greedily over intents in id order (deterministic). Each candidate's legs and
// numeraire-anchored prices are constructed exactly; representability and full
// §6.4 validity are decided downstream by the shared chain gate. Intents are
// used at most once across the returned matches.
func Find(intents []match.OpenIntent) []sessiontypes.Match {
	nodes := make([]node, 0, len(intents))
	for _, oi := range intents {
		give, want, ok := oi.SingleGiveWant()
		if !ok {
			continue // Phase-0 cycle finding handles single give/want only
		}
		gKey, gAmt, ok := match.LegKeyAmount(give)
		if !ok {
			continue
		}
		wKey, wAmt, ok := match.LegKeyAmount(want)
		if !ok {
			continue
		}
		nodes = append(nodes, node{oi: oi, give: give, giveKey: gKey, giveAmt: gAmt, wantKey: wKey, wantAmt: wAmt})
	}
	// deterministic order: by intent id (content-derived, D-009).
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].oi.ID < nodes[j].oi.ID })

	used := make(map[string]bool, len(nodes))
	var matches []sessiontypes.Match
	for s := range nodes {
		if used[nodes[s].oi.ID] {
			continue
		}
		cyc := findCycleFrom(nodes, s, used)
		if len(cyc) < 2 {
			continue
		}
		m, ok := buildCycleMatch(nodes, cyc)
		if !ok {
			continue
		}
		matches = append(matches, m)
		for _, ni := range cyc {
			used[nodes[ni].oi.ID] = true
		}
	}
	return matches
}

// findCycleFrom does a bounded DFS over the provider graph starting at node s
// and returns the node indices of the first simple cycle through s (in
// give-flow order), or nil. The provider of intent A is any unused intent B
// whose give exactly fills A's want (B.giveKey == A.wantKey, B.giveAmt ==
// A.wantAmt) — Phase-0 fill-or-kill, mirroring the fallback's full-give terms.
// CONCLAVE-TODO(phase-2): partial-fill cycles (B.giveAmt >= A.wantAmt) and
// alternate numeraire anchoring when one anchoring is non-representable.
func findCycleFrom(nodes []node, s int, used map[string]bool) []int {
	// provider candidates of node i, in id order for determinism.
	providers := func(i int) []int {
		var out []int
		for j := range nodes {
			if j == i || used[nodes[j].oi.ID] {
				continue
			}
			if nodes[j].giveKey == nodes[i].wantKey && nodes[j].giveAmt.Equal(nodes[i].wantAmt) {
				out = append(out, j)
			}
		}
		return out
	}

	path := []int{s}
	onPath := map[int]bool{s: true}
	var dfs func(cur int) []int
	dfs = func(cur int) []int {
		for _, p := range providers(cur) {
			if p == s && len(path) >= 2 {
				return append([]int{}, path...) // closed a simple cycle back to s
			}
			if onPath[p] {
				continue
			}
			path = append(path, p)
			onPath[p] = true
			if cyc := dfs(p); cyc != nil {
				return cyc
			}
			path = path[:len(path)-1]
			delete(onPath, p)
		}
		return nil
	}
	return dfs(s)
}

// buildCycleMatch constructs the settlement Match for a provider-chain cycle
// cyc = [a_0, a_1, ...] where a_{k+1} provides for a_k (a_{k+1} gives to a_k)
// and a_0 provides for the last. Each intent gives exactly once. Prices are
// anchored at a coin numeraire (price 1) and propagated around the cycle as
// exact LegacyDecs; ok is false if the cycle has no coin give-asset to price in.
func buildCycleMatch(nodes []node, cyc []int) (sessiontypes.Match, bool) {
	m := len(cyc)

	// legs: provider a_{k+1} gives to consumer a_k (indices mod m).
	legs := make([]sessiontypes.SettleLeg, 0, m)
	intentIDs := make([]string, 0, m)
	for k := 0; k < m; k++ {
		consumer := nodes[cyc[k]]
		provider := nodes[cyc[(k+1)%m]]
		legs = append(legs, match.LegToSettle(provider.oi.Sigil, consumer.oi.Sigil, provider.give))
		intentIDs = append(intentIDs, nodes[cyc[k]].oi.ID)
	}

	// pick the numeraire: the lexicographically smallest coin give-asset in the
	// cycle (deterministic). Without a coin asset the cycle cannot price in a
	// coin numeraire — skip (mirrors the fallback's pickNumeraire failing).
	numeraire := ""
	for k := 0; k < m; k++ {
		gk := nodes[cyc[k]].giveKey
		if match.IsCoinDenom(gk) && (numeraire == "" || gk < numeraire) {
			numeraire = gk
		}
	}
	if numeraire == "" {
		return sessiontypes.Match{}, false
	}

	// propagate give-asset prices around the cycle from the numeraire anchor.
	// For each consumer a_k: price[G_k]*giveAmt_k == price[G_{k+1}]*wantAmt_k
	// (its want is filled by a_{k+1}'s give of the same asset/amount), so
	// price[G_{k+1}] = price[G_k] * giveAmt_k / wantAmt_k — exact LegacyDec
	// (any non-terminating quotient is caught by the chain gate downstream).
	priceOf := map[string]math.LegacyDec{numeraire: math.LegacyOneDec()}
	// find a starting index whose give-asset is the numeraire.
	start := -1
	for k := 0; k < m; k++ {
		if nodes[cyc[k]].giveKey == numeraire {
			start = k
			break
		}
	}
	for step := 0; step < m; step++ {
		k := (start + step) % m
		cur := nodes[cyc[k]]
		nextKey := nodes[cyc[(k+1)%m]].giveKey
		if _, ok := priceOf[nextKey]; ok {
			continue // already priced (closes the loop)
		}
		base, ok := priceOf[cur.giveKey]
		if !ok || cur.wantAmt.IsZero() {
			return sessiontypes.Match{}, false
		}
		priceOf[nextKey] = base.MulInt(cur.giveAmt).Quo(math.LegacyNewDecFromInt(cur.wantAmt))
	}

	// emit one PricePoint per distinct give-asset, in base-key order
	// (deterministic); the numeraire carries its explicit self-price 1.
	bases := make([]string, 0, len(priceOf))
	for b := range priceOf {
		bases = append(bases, b)
	}
	sort.Strings(bases)
	prices := make([]sessiontypes.PricePoint, 0, len(bases))
	for _, b := range bases {
		prices = append(prices, sessiontypes.PricePoint{Base: b, Quote: numeraire, Price: priceOf[b]})
	}

	return sessiontypes.Match{IntentIds: intentIDs, Legs: legs, ClearingPrices: prices}, true
}
