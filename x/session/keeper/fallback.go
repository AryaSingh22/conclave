package keeper

import (
	"sort"
	"strings"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	escrowtypes "github.com/AryaSingh22/conclave/x/escrow/types"
	"github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// fbIntent is an OPEN intent reduced to its single give/want legs for the
// fallback matcher (Phase-0 intents express one give and one want; richer
// shapes are left to solvers), plus the signed allow_partial flag the 2.6
// floor-fill candidate needs and the full signed payload the 2.7 condition
// resolver reads.
type fbIntent struct {
	id           string
	sigil        string
	give         veiltypes.IntentLeg
	want         veiltypes.IntentLeg
	allowPartial bool
	payload      veiltypes.IntentPayload
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
//
// Task 2.7 makes it an ATTACHER of delivery conditions — the first real one in
// the tree. That is not an enrichment but a consequence of enforcing
// `signed ⇒ attached`: this matcher is the DEFAULT clearing path for every
// session no solver wins, so a fallback that could not attach would mean a
// condition-signing intent could never clear at all. It attaches the giver's
// signed condition to a DataGrant leg and then faces the unchanged
// validateSolution bar, so a candidate the counterparty has not consented to is
// left unmatched rather than downgraded. Neither branch settles a silent
// downgrade, which is the whole point.
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
			payload:      payload,
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
		if err := k.validateSolution(ctx, session, []types.Match{m}, params); err != nil {
			continue
		}
		if !k.fallbackCanFund(ctx, m) {
			continue
		}
		return m, true
	}
	return types.Match{}, false
}

// fallbackCanFund is the candidate-time AFFORDABILITY pre-check the 2.7
// attacher needs. concludeInner wraps EVERY fallback match of a session in ONE
// cache context, so a conditioned match whose seller cannot post its OPTIMISTIC
// dispute bond — or whose buyer cannot cover the payment being escrowed — would
// void every OTHER pair's clearing in that session. That blast radius is a cost
// the fallback takes on by attaching conditions at all, so it is paid down here
// rather than discovered in production.
//
// It sizes the bill from escrowtypes.FundingRequirement over the payment
// conditionedLegPayment derives — the same two functions the settlement
// boundary and CreateEscrow use — so a candidate can never be admitted on a
// different notion of what is owed than the one later charged. That shared
// definition is the point: an independent second computation here would be the
// ante-vs-msg-server divergence (D-085) rebuilt in a new place.
//
// It is a NECESSARY condition and NOT a sufficient one, stated plainly rather
// than glossed: GetBalance cannot see vesting locks or blocked-address rules, so
// CreateEscrow's bank sends remain the authoritative guard. A false positive
// here degrades to exactly the pre-2.7 behaviour — the batch's cache is
// discarded and the session closes with its intents rolling (runFallback never
// stalls the chain) — never to a funded escrow that should not exist. An
// unconditioned match reaches no escrow funding at all and returns true
// immediately, so nothing that cleared before 2.7 acquires a new way to fail.
func (k Keeper) fallbackCanFund(ctx sdk.Context, m types.Match) bool {
	for li := range m.Legs {
		leg := m.Legs[li]
		if leg.Condition == nil || leg.Condition.Condition == nil {
			continue
		}
		esc := escrowtypes.Escrow{Amount: conditionedLegPayment(m, li), Condition: *leg.Condition}
		fromPayer, fromPayee := escrowtypes.FundingRequirement(esc)
		if !k.sigilCanPay(ctx, leg.ToSigil, fromPayer) || !k.sigilCanPay(ctx, leg.FromSigil, fromPayee) {
			return false
		}
	}
	return true
}

// sigilCanPay reports whether a sigil's controller currently holds `amount`.
// Balances are read per denom over sorted sdk.Coins, so the verdict is
// deterministic on every node (§11.6); an unresolvable sigil fails CLOSED (the
// candidate is skipped, the pair left unmatched) rather than being assumed
// solvent.
func (k Keeper) sigilCanPay(ctx sdk.Context, sigil string, amount sdk.Coins) bool {
	if amount.IsZero() {
		return true
	}
	addr, err := k.sigilKeeper.ControllerAddress(ctx, phase0Chamber, sigil)
	if err != nil {
		return false
	}
	for _, c := range amount {
		if k.bankKeeper.GetBalance(ctx, addr, c.Denom).Amount.LT(c.Amount) {
			return false
		}
	}
	return true
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
//
// Since task 2.7 it ATTACHES each giver's signed delivery condition to its own
// DataGrant leg. The pair it builds is already exactly the D-074 conditioned
// shape — two distinct sigils, one DataGrant seller→buyer, one coin payment
// buyer→seller (pickNumeraire has already rejected data-for-data and
// data-for-svc, so a conditioned fallback match is always data-for-coin) — so
// attaching needs no new shape reasoning. What the counterparty signed is NOT
// consulted here: the candidate goes to the unchanged validateSolution bar,
// which runs the same binding predicate as every other clearing path and drops
// the candidate if the buyer never consented. Keeping the decision there rather
// than duplicating it is what stops the fallback from becoming a second, weaker
// definition of consent.
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
	attachFallbackCondition(&aLeg, a, aGiveKey)
	attachFallbackCondition(&bLeg, b, bGiveKey)

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

// attachFallbackCondition carries the giver's signed delivery condition onto its
// own settlement leg (task 2.7). Only a DataGrant leg can bear one (SPEC §6.5),
// and the giver is that leg's seller, so the condition resolved is the seller's
// — through the same effectiveCondition resolver the two consent chokepoints
// use, which means the per-leg IntentLeg.condition refinement (FW-2) is honored
// on the fallback path from the moment it exists rather than only on the solver
// path.
//
// A giver that signed nothing attaches nothing, so an unconditioned pair builds
// the byte-identical match it built before 2.7.
func attachFallbackCondition(leg *types.SettleLeg, giver fbIntent, giveKey string) {
	if leg.GetData() == nil {
		return
	}
	if cond := effectiveCondition(giver.payload, giveKey, roleSeller); cond != nil && cond.Condition != nil {
		leg.Condition = cond
	}
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
