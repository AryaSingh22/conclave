// Package cycles is the reference solver's circular-trade finder (SPEC §4
// solver/cycles/). It detects directed trade cycles A→B→C→A that the pairwise
// matcher (solver/naive) structurally cannot clear, and emits an n-party Match
// for each — the part of the solver that earns real surplus and proves the
// N-party value proposition (SPEC §1.1 goal 2, §1.3).
//
// It is PURE (proto types + math only; no x/session/keeper): the matching idea
// is reimplemented off-chain, separate from the in-consensus fallback. The one
// rule that must not drift — whether the constructed clearing prices are exactly
// representable in LegacyDec (D-047) — is NOT decided here: cycles proposes
// candidate Matches with numeraire-anchored prices, and the shared chain
// validator (solver/gate → keeper.CheckPriceBinding) is the sole arbiter of
// acceptance, so the skip/accept decision is identical to the chain's by
// construction (D-055).
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

// canFill reports whether this node's give leg can deliver `amt` of its give
// asset to a consumer: EXACTLY (Phase-0 fill-or-kill), or with a remainder the
// giver keeps — a PARTIAL fill, which §6.4 rule 3 accepts only when the intent
// signed `allow_partial` (task 2.6). A DataGrant has no divisible quantity (its
// conservation amount is the bookkeeping 1, D-048), so a data give leg is
// exact-fill only whatever the flag says.
func (n node) canFill(amt math.Int) bool {
	if !amt.IsPositive() {
		return false
	}
	if n.giveAmt.Equal(amt) {
		return true
	}
	return n.oi.AllowPartial && n.give.GetData() == nil && n.giveAmt.GT(amt)
}

// Find returns one candidate GROUP per disjoint trade cycle it discovers,
// greedily over intents in id order (deterministic). A group holds the SAME
// cycle priced under each of its coin give-assets in turn — the ALTERNATE
// NUMERAIRE ANCHORINGS (task 2.6) — in sorted anchor order, the first being the
// pre-2.6 default. Anchoring changes which clearing prices are exactly
// representable, so a cycle its default anchor cannot price may still clear
// under another; cycles does NOT decide which (D-055: representability is the
// chain gate's call and is never reimplemented here). The caller admits the
// FIRST alternative its arbiter accepts and skips the rest of that group.
// Intents are used at most once across the returned groups.
func Find(intents []match.OpenIntent) [][]sessiontypes.Match {
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
	var groups [][]sessiontypes.Match
	for s := range nodes {
		if used[nodes[s].oi.ID] {
			continue
		}
		cyc := findCycleFrom(nodes, s, used)
		if len(cyc) < 2 {
			continue
		}
		alts := buildCycleAlternatives(nodes, cyc)
		if len(alts) == 0 {
			continue
		}
		groups = append(groups, alts)
		for _, ni := range cyc {
			used[nodes[ni].oi.ID] = true
		}
	}
	return groups
}

// findCycleFrom does a bounded DFS over the provider graph starting at node s
// and returns the node indices of the first simple cycle through s (in
// give-flow order), or nil. The provider of intent A is any unused intent B
// whose give asset is A's want asset and whose give leg can FILL A's wanted
// quantity — exactly, or partially when B signed allow_partial (node.canFill).
func findCycleFrom(nodes []node, s int, used map[string]bool) []int {
	// provider candidates of node i, in id order for determinism.
	providers := func(i int) []int {
		var out []int
		for j := range nodes {
			if j == i || used[nodes[j].oi.ID] {
				continue
			}
			if nodes[j].giveKey == nodes[i].wantKey && nodes[j].canFill(nodes[i].wantAmt) {
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

// buildCycleAlternatives prices one cycle under every coin give-asset it
// contains, in sorted anchor order (deterministic), dropping any anchoring the
// construction cannot complete. A cycle with no coin give-asset yields nothing —
// it cannot be priced in a coin numeraire at all (mirrors the fallback's
// pickNumeraire failing).
func buildCycleAlternatives(nodes []node, cyc []int) []sessiontypes.Match {
	anchors := make([]string, 0, len(cyc))
	seen := make(map[string]bool, len(cyc))
	for _, ni := range cyc {
		gk := nodes[ni].giveKey
		if match.IsCoinDenom(gk) && !seen[gk] {
			seen[gk] = true
			anchors = append(anchors, gk)
		}
	}
	sort.Strings(anchors)

	alts := make([]sessiontypes.Match, 0, len(anchors))
	for _, numeraire := range anchors {
		if m, ok := buildCycleMatch(nodes, cyc, numeraire); ok {
			alts = append(alts, m)
		}
	}
	return alts
}

// buildCycleMatch constructs the settlement Match for a provider-chain cycle
// cyc = [a_0, a_1, ...] where a_{k+1} provides for a_k (a_{k+1} gives to a_k)
// and a_0 provides for the last, priced against `numeraire` (price 1).
//
// Each party DELIVERS exactly what its consumer wants: an exact fill hands over
// the whole give leg, a partial fill keeps the remainder (node.canFill already
// established the intent permits it). Prices are propagated around the cycle as
// exact LegacyDecs from the anchor; ok is false if the propagation cannot be
// completed. Representability is not judged here (D-055).
func buildCycleMatch(nodes []node, cyc []int, numeraire string) (sessiontypes.Match, bool) {
	m := len(cyc)

	// delivered[j] is what party a_j actually hands over: exactly the amount its
	// consumer a_{j-1} wants. On an exact fill this equals a_j's whole give.
	delivered := make([]math.Int, m)
	for j := 0; j < m; j++ {
		delivered[j] = nodes[cyc[(j-1+m)%m]].wantAmt
	}

	// legs: provider a_{k+1} gives to consumer a_k, in the delivered quantity.
	legs := make([]sessiontypes.SettleLeg, 0, m)
	intentIDs := make([]string, 0, m)
	for k := 0; k < m; k++ {
		consumer := nodes[cyc[k]]
		provider := nodes[cyc[(k+1)%m]]
		leg, ok := match.LegToSettleAmount(provider.oi.Sigil, consumer.oi.Sigil, provider.give, consumer.wantAmt)
		if !ok {
			return sessiontypes.Match{}, false
		}
		legs = append(legs, leg)
		intentIDs = append(intentIDs, consumer.oi.ID)
	}

	// propagate give-asset prices around the cycle from the numeraire anchor.
	// Party a_k hands over delivered_k of G_k and receives wantAmt_k of G_{k+1};
	// equal value at the clearing prices ⇒ price[G_{k+1}] = price[G_k] ×
	// delivered_k / wantAmt_k — exact LegacyDec (any non-terminating quotient is
	// caught by the chain gate downstream). With exact fills delivered_k is the
	// whole give, so this reduces to the pre-2.6 formula.
	priceOf := map[string]math.LegacyDec{numeraire: math.LegacyOneDec()}
	start := -1
	for k := 0; k < m; k++ {
		if nodes[cyc[k]].giveKey == numeraire {
			start = k
			break
		}
	}
	if start < 0 {
		return sessiontypes.Match{}, false
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
		priceOf[nextKey] = base.MulInt(delivered[k]).Quo(math.LegacyNewDecFromInt(cur.wantAmt))
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
