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
// shapes are left to solvers).
type fbIntent struct {
	id    string
	sigil string
	give  veiltypes.IntentLeg
	want  veiltypes.IntentLeg
}

// buildFallbackMatches is the SPEC §6.4 in-consensus naive matcher: greedy
// pairwise by a FULLY deterministic order (intents sorted by id, scanned in
// nested order), clearing each mutually-fillable 2-party swap at full-give
// "degraded" terms. Every node computes it identically — sorted iteration, no
// map drives a state write, LegacyDec only, no wall-clock (§11.6). Each
// candidate match is held to the SAME validateSolution bar as a solver's
// solution, so the fallback can never produce an invalid graph; pairs that do
// not clear exactly at representable prices, or miss a floor, are simply left
// unmatched (degraded liveness — those intents roll or expire).
//
// CONCLAVE-TODO(phase-2): partial-fill and cycle-aware fallback; Phase-0 clears
// only mutually-fillable 2-party swaps with one numeraire-coin side.
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

// tryFallbackPair builds a candidate full-give swap between two intents and
// returns it only if it passes the full §6.4 validateSolution (all six rules,
// incl. price binding and counterparty class). The swap clears in the
// numeraire coin: the party that gives the numeraire pays its full give, the
// other delivers its full give, and the non-numeraire asset is priced so each
// party's value nets exactly zero. A non-terminating price (e.g. 100/3) makes
// price binding fail and the pair is skipped — the matcher never rounds.
func (k Keeper) tryFallbackPair(ctx sdk.Context, session types.Session, a, b fbIntent, params types.Params) (types.Match, bool) {
	aGiveKey, aGiveAmt, err := intentLegKeyAmount(a.give)
	if err != nil {
		return types.Match{}, false
	}
	bGiveKey, bGiveAmt, err := intentLegKeyAmount(b.give)
	if err != nil {
		return types.Match{}, false
	}
	aWantKey, _, err := intentLegKeyAmount(a.want)
	if err != nil {
		return types.Match{}, false
	}
	bWantKey, _, err := intentLegKeyAmount(b.want)
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

	// price the non-numeraire asset so the numeraire-giver's value nets zero.
	var prices []types.PricePoint
	if aGiveKey == numeraire {
		prices = []types.PricePoint{
			{Base: numeraire, Quote: numeraire, Price: math.LegacyOneDec()},
			{Base: bGiveKey, Quote: numeraire, Price: math.LegacyNewDecFromInt(aGiveAmt).Quo(math.LegacyNewDecFromInt(bGiveAmt))},
		}
	} else {
		prices = []types.PricePoint{
			{Base: numeraire, Quote: numeraire, Price: math.LegacyOneDec()},
			{Base: aGiveKey, Quote: numeraire, Price: math.LegacyNewDecFromInt(bGiveAmt).Quo(math.LegacyNewDecFromInt(aGiveAmt))},
		}
	}

	m := types.Match{
		IntentIds: []string{a.id, b.id},
		Legs: []types.SettleLeg{
			intentLegToSettle(a.sigil, b.sigil, a.give),
			intentLegToSettle(b.sigil, a.sigil, b.give),
		},
		ClearingPrices: prices,
	}
	if err := k.validateSolution(ctx, session, []types.Match{m}, params); err != nil {
		return types.Match{}, false
	}
	return m, true
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

// intentLegToSettle converts an intent give/want leg into a directed settlement
// leg (same asset), from giver sigil to receiver sigil.
func intentLegToSettle(from, to string, leg veiltypes.IntentLeg) types.SettleLeg {
	out := types.SettleLeg{FromSigil: from, ToSigil: to}
	switch {
	case leg.GetCoin() != nil:
		c := *leg.GetCoin()
		out.Asset = &types.SettleLeg_Coin{Coin: &c}
	case leg.GetSvc() != nil:
		v := *leg.GetSvc()
		out.Asset = &types.SettleLeg_Svc{Svc: &v}
	case leg.GetData() != nil:
		d := *leg.GetData()
		out.Asset = &types.SettleLeg_Data{Data: &d}
	}
	return out
}
