// Package naive is the reference solver's greedy pairwise matcher (SPEC §4
// solver/naive/) — the off-chain mirror of the in-consensus fallback's matching
// idea (x/session/keeper fallback.go), implemented separately so the binary is
// not coupled to consensus code. It clears mutually-fillable 2-party swaps at
// full-give "degraded" terms and CANNOT clear an n-party cycle (no two parties
// are mutually fillable in a cycle) — which is exactly why solver/cycles exists.
//
// PURE (proto types + math only). Like cycles, it constructs candidate Matches
// with numeraire-anchored prices; the shared chain gate (solver/gate) is the
// sole arbiter of price representability and full §6.4 validity (D-055).
package naive

import (
	"sort"

	"cosmossdk.io/math"

	"github.com/AryaSingh22/conclave/solver/match"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

type pairNode struct {
	oi      match.OpenIntent
	give    veiltypes.IntentLeg
	giveKey string
	giveAmt math.Int
	wantKey string
	wantAmt math.Int
}

// Find returns one candidate Match per mutually-fillable 2-party swap, greedy
// over intents in id order (deterministic, mirroring buildFallbackMatches).
// Each intent is used at most once. Already-matched intents (e.g. by the cycle
// finder) should be excluded by the caller before calling Find.
func Find(intents []match.OpenIntent) []sessiontypes.Match {
	nodes := make([]pairNode, 0, len(intents))
	for _, oi := range intents {
		give, want, ok := oi.SingleGiveWant()
		if !ok {
			continue
		}
		gKey, gAmt, ok := match.LegKeyAmount(give)
		if !ok {
			continue
		}
		wKey, wAmt, ok := match.LegKeyAmount(want)
		if !ok {
			continue
		}
		nodes = append(nodes, pairNode{oi: oi, give: give, giveKey: gKey, giveAmt: gAmt, wantKey: wKey, wantAmt: wAmt})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].oi.ID < nodes[j].oi.ID })

	matched := make(map[string]bool, len(nodes))
	var matches []sessiontypes.Match
	for i := range nodes {
		if matched[nodes[i].oi.ID] {
			continue
		}
		for j := i + 1; j < len(nodes); j++ {
			if matched[nodes[j].oi.ID] {
				continue
			}
			m, ok := buildPair(nodes[i], nodes[j])
			if ok {
				matches = append(matches, m)
				matched[nodes[i].oi.ID] = true
				matched[nodes[j].oi.ID] = true
				break
			}
		}
	}
	return matches
}

// buildPair constructs a full-give swap between a and b, priced so each party's
// value nets exactly zero at the numeraire. It returns ok=false unless the two
// are mutually fillable (each side's give is the other's want, exact amounts)
// and at least one give side is a coin to price in. Representability and §6.4
// validity are decided by the chain gate downstream.
func buildPair(a, b pairNode) (sessiontypes.Match, bool) {
	// mutual fillability, fill-or-kill: each side's give exactly fills the
	// other's want (asset and amount).
	if a.giveKey != b.wantKey || b.giveKey != a.wantKey {
		return sessiontypes.Match{}, false
	}
	if !a.giveAmt.Equal(b.wantAmt) || !b.giveAmt.Equal(a.wantAmt) {
		return sessiontypes.Match{}, false
	}

	numeraire, ok := pickNumeraire(a.giveKey, b.giveKey)
	if !ok {
		return sessiontypes.Match{}, false
	}

	// price the non-numeraire side so the numeraire-giver's value nets zero
	// (mirrors tryFallbackPair). Exact LegacyDec; non-terminating quotients are
	// rejected by the chain gate.
	prices := []sessiontypes.PricePoint{{Base: numeraire, Quote: numeraire, Price: math.LegacyOneDec()}}
	if a.giveKey == numeraire {
		prices = append(prices, sessiontypes.PricePoint{
			Base: b.giveKey, Quote: numeraire,
			Price: math.LegacyNewDecFromInt(a.giveAmt).Quo(math.LegacyNewDecFromInt(b.giveAmt)),
		})
	} else {
		prices = append(prices, sessiontypes.PricePoint{
			Base: a.giveKey, Quote: numeraire,
			Price: math.LegacyNewDecFromInt(b.giveAmt).Quo(math.LegacyNewDecFromInt(a.giveAmt)),
		})
	}

	return sessiontypes.Match{
		IntentIds: []string{a.oi.ID, b.oi.ID},
		Legs: []sessiontypes.SettleLeg{
			match.LegToSettle(a.oi.Sigil, b.oi.Sigil, a.give),
			match.LegToSettle(b.oi.Sigil, a.oi.Sigil, b.give),
		},
		ClearingPrices: prices,
	}, true
}

// pickNumeraire returns the coin denom to price a swap in: the plain-coin side,
// lexicographically smaller when both are coins (deterministic). Mirrors the
// fallback's pickNumeraire.
func pickNumeraire(keyA, keyB string) (string, bool) {
	aCoin := match.IsCoinDenom(keyA)
	bCoin := match.IsCoinDenom(keyB)
	switch {
	case aCoin && bCoin:
		if keyA <= keyB {
			return keyA, true
		}
		return keyB, true
	case aCoin:
		return keyA, true
	case bCoin:
		return keyB, true
	default:
		return "", false
	}
}
