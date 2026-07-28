package keeper

import (
	"sort"
	"strings"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// fbIntent is an OPEN intent reduced to its single give/want legs for the
// fallback matcher (Phase-0 intents express one give and one want; richer
// shapes are left to solvers), plus the signed allow_partial flag the 2.6
// floor-fill candidate needs.
type fbIntent struct {
	id           string
	sigil        string
	give         veiltypes.IntentLeg
	want         veiltypes.IntentLeg
	allowPartial bool
}

// buildFallbackMatches is the SPEC §6.4 in-consensus naive matcher: greedy
// pairwise by a FULLY deterministic order (intents sorted by id, scanned in
// nested order). Every node computes it identically — sorted iteration, no map
// drives a state write, LegacyDec only, no wall-clock (§11.6). Each candidate
// match is held to the SAME validateSolution bar as a solver's solution, so the
// fallback can never produce an invalid graph; pairs that clear at no
// representable price, miss a floor, or breach a signed §5.1 rate are simply
// left unmatched (degraded liveness — those intents roll or expire).
//
// Task 2.6 adds PARTIAL FILLS for 2-party swaps (see tryFallbackPair); the
// cycle-aware fallback remains out of consensus and is task 2.10.
func (k Keeper) buildFallbackMatches(ctx sdk.Context, session types.Session, params types.Params) ([]types.Match, error) {
	intents := make([]fbIntent, 0, len(session.IntentIds))
	for _, id := range session.IntentIds {
		rec, payload, err := k.veil().GetOpenIntent(ctx, phase0Chamber, id)
		if err != nil {
			continue // not OPEN (already concluded/rolled) — skip
		}
		if len(payload.Give) != 1 || len(payload.Want) != 1 {
			continue // Phase-0 fallback handles single give/want only
		}
		intents = append(intents, fbIntent{
			id: id, sigil: rec.Intent.SenderSigil,
			give: payload.Give[0], want: payload.Want[0],
			allowPartial: intentAllowsPartial(rec),
		})
	}
	// deterministic order: by intent id (the ids are content-derived, D-009).
	sort.Slice(intents, func(i, j int) bool { return intents[i].id < intents[j].id })

	matched := make(map[string]bool, len(intents))
	var matches []types.Match
	for i := range intents {
		if matched[intents[i].id] {
			continue
		}
		for j := i + 1; j < len(intents); j++ {
			if matched[intents[j].id] {
				continue
			}
			m, ok := k.tryFallbackPair(ctx, session, intents[i], intents[j], params)
			if ok {
				matches = append(matches, m)
				matched[intents[i].id] = true
				matched[intents[j].id] = true
				break
			}
		}
	}
	return matches, nil
}

// tryFallbackPair builds candidate swap terms for two mutually-fillable intents
// and returns the FIRST that passes the full §6.4 validateSolution (all six
// rules, incl. price binding, the §5.1 limit rates, and counterparty class). The
// swap clears in the numeraire coin, and the non-numeraire asset is priced so
// each party's value nets exactly zero; a non-terminating price (e.g. 100/3)
// makes price binding fail and the candidate is skipped — the matcher never
// rounds.
//
// Two candidates are tried, in a FIXED order:
//
//  1. FULL-GIVE (the pre-2.6 degraded terms) — each party hands over its whole
//     give leg. Tried FIRST, so every pair that cleared before task 2.6 still
//     clears at byte-identical terms: the enrichment is strictly additive.
//  2. FLOOR-FILL (task 2.6) — a party that signed `allow_partial` hands over
//     only what its counterparty's want-floor needs and KEEPS THE REMAINDER. A
//     fill-or-kill party is never shrunk, so the §5.1 all-or-nothing guarantee
//     is intact; the remainder is simply never moved (no leg, no mint, no
//     burn), so the D-048 coin value-conservation invariant is untouched.
//
// The second candidate earns its keep because the two terms differ in PRICE and
// in RATE: a pair whose full-give ratio is not exactly representable (D-047), or
// whose full-give rate breaches a signed §5.1 limit (task 2.6 item 8), can still
// clear exactly at the floor terms. Both candidates face the same
// validateSolution bar, so neither can produce an invalid graph.
//
// A partially filled intent is CONCLUDED like any other (applyMatches): its
// remainder is NOT re-offered into a later session, so no unit is ever counted
// twice — the intent settles once, for less than it offered.
func (k Keeper) tryFallbackPair(ctx sdk.Context, session types.Session, a, b fbIntent, params types.Params) (types.Match, bool) {
	aGiveKey, aGiveAmt, err := intentLegKeyAmount(a.give)
	if err != nil {
		return types.Match{}, false
	}
	bGiveKey, bGiveAmt, err := intentLegKeyAmount(b.give)
	if err != nil {
		return types.Match{}, false
	}
	aWantKey, aWantAmt, err := intentLegKeyAmount(a.want)
	if err != nil {
		return types.Match{}, false
	}
	bWantKey, bWantAmt, err := intentLegKeyAmount(b.want)
	if err != nil {
		return types.Match{}, false
	}
	// mutual fillability: each side's give is the other's want.
	if aGiveKey != bWantKey || bGiveKey != aWantKey {
		return types.Match{}, false
	}

	// pick the numeraire: the plain-coin give side (no svc//data/ prefix). If
	// both are coins, the lexicographically smaller for determinism; if neither
	// is, the swap cannot price in a coin numeraire — skip.
	numeraire, ok := pickNumeraire(aGiveKey, bGiveKey)
	if !ok {
		return types.Match{}, false
	}

	for _, terms := range fallbackTerms(a, aGiveAmt, aWantAmt, b, bGiveAmt, bWantAmt) {
		m, built := buildFallbackMatch(a, b, aGiveKey, bGiveKey, numeraire, terms)
		if !built {
			continue
		}
		if err := k.validateSolution(ctx, session, []types.Match{m}, params); err == nil {
			return m, true
		}
	}
	return types.Match{}, false
}

// fallbackTerms lists the candidate (a-gives, b-gives) quantities for a pair in
// the fixed order the matcher tries them: the FULL-GIVE terms first (pre-2.6
// behaviour, so nothing that cleared before can change), then the FLOOR-FILL
// terms when they actually differ. A side shrinks to its counterparty's want
// floor only if its own intent signed allow_partial, and never for a DataGrant,
// whose conservation amount is the indivisible bookkeeping 1 (D-048).
//
// That allow_partial condition is an EFFICIENCY guard, NOT the fill-or-kill
// guarantee: removing it leaves the guarantee intact, because §6.4 rule 3
// (checkLimits) rejects an under-filled give on its own — watched both ways in
// D-089. It is kept so the matcher never builds a candidate that must fail.
//
// Pure function of the two intents, so every node builds the identical candidate
// list (§11.6).
func fallbackTerms(a fbIntent, aGiveAmt, aWantAmt math.Int, b fbIntent, bGiveAmt, bWantAmt math.Int) [][2]math.Int {
	terms := [][2]math.Int{{aGiveAmt, bGiveAmt}}

	x, y := aGiveAmt, bGiveAmt
	if a.allowPartial && a.give.GetData() == nil && bWantAmt.LT(aGiveAmt) {
		x = bWantAmt
	}
	if b.allowPartial && b.give.GetData() == nil && aWantAmt.LT(bGiveAmt) {
		y = aWantAmt
	}
	if !x.Equal(aGiveAmt) || !y.Equal(bGiveAmt) {
		terms = append(terms, [2]math.Int{x, y})
	}
	return terms
}

// buildFallbackMatch assembles the two-leg swap for one candidate's quantities,
// pricing the non-numeraire asset so each party's value nets EXACTLY zero at the
// stated prices.
func buildFallbackMatch(a, b fbIntent, aGiveKey, bGiveKey, numeraire string, terms [2]math.Int) (types.Match, bool) {
	x, y := terms[0], terms[1]
	if !x.IsPositive() || !y.IsPositive() {
		return types.Match{}, false
	}
	aLeg, ok := intentLegToSettle(a.sigil, b.sigil, a.give, x)
	if !ok {
		return types.Match{}, false
	}
	bLeg, ok := intentLegToSettle(b.sigil, a.sigil, b.give, y)
	if !ok {
		return types.Match{}, false
	}

	prices := []types.PricePoint{{Base: numeraire, Quote: numeraire, Price: math.LegacyOneDec()}}
	if aGiveKey == numeraire {
		prices = append(prices, types.PricePoint{
			Base: bGiveKey, Quote: numeraire,
			Price: math.LegacyNewDecFromInt(x).Quo(math.LegacyNewDecFromInt(y)),
		})
	} else {
		prices = append(prices, types.PricePoint{
			Base: aGiveKey, Quote: numeraire,
			Price: math.LegacyNewDecFromInt(y).Quo(math.LegacyNewDecFromInt(x)),
		})
	}

	return types.Match{
		IntentIds:      []string{a.id, b.id},
		Legs:           []types.SettleLeg{aLeg, bLeg},
		ClearingPrices: prices,
	}, true
}

// pickNumeraire returns the coin denom to price a swap in: the plain-coin side
// (svc//data/ keys are not coin numeraires). Deterministic tie-break to the
// lexicographically smaller denom when both are coins.
func pickNumeraire(keyA, keyB string) (string, bool) {
	aCoin := isCoinDenom(keyA)
	bCoin := isCoinDenom(keyB)
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

func isCoinDenom(key string) bool {
	return !strings.HasPrefix(key, "svc/") && !strings.HasPrefix(key, "data/")
}

// intentLegToSettle converts an intent give leg into a directed settlement leg
// (same asset) from giver to receiver, carrying `amount` of it — the whole offer
// on a full-give clearing, the counterparty's floor on a 2.6 floor fill. A
// DataGrant has no divisible quantity (its conservation amount is the
// bookkeeping 1, D-048), so it is only ever emitted whole; ok is false if a
// different amount is asked of one, or if the leg names no asset.
func intentLegToSettle(from, to string, leg veiltypes.IntentLeg, amount math.Int) (types.SettleLeg, bool) {
	out := types.SettleLeg{FromSigil: from, ToSigil: to}
	switch {
	case leg.GetCoin() != nil:
		c := *leg.GetCoin()
		c.Amount = amount
		out.Asset = &types.SettleLeg_Coin{Coin: &c}
	case leg.GetSvc() != nil:
		v := *leg.GetSvc()
		v.Units = amount
		out.Asset = &types.SettleLeg_Svc{Svc: &v}
	case leg.GetData() != nil:
		if !amount.Equal(math.OneInt()) {
			return types.SettleLeg{}, false
		}
		d := *leg.GetData()
		out.Asset = &types.SettleLeg_Data{Data: &d}
	default:
		return types.SettleLeg{}, false
	}
	return out, true
}
