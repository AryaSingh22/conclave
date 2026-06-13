package keeper

import (
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/session/types"
)

// matchNumeraire returns a match's price numeraire (the common quote; dev:
// uusdc) and the per-asset price map valued in it, mirroring the index
// checkPriceBinding builds (the numeraire self-prices at 1). It is the single
// source of the price lens that scoring AND the Half-B skim measure surplus
// through, so both read the same prices Half-A's checkPriceBinding bound the
// legs to (D-047). Pure function of the match; never iterated into a write.
func matchNumeraire(m types.Match) (string, map[string]math.LegacyDec, error) {
	numeraire := ""
	priceOf := make(map[string]math.LegacyDec, len(m.ClearingPrices))
	for i := range m.ClearingPrices {
		pp := m.ClearingPrices[i]
		if numeraire == "" {
			numeraire = pp.Quote
		} else if pp.Quote != numeraire {
			return "", nil, errorsmod.Wrapf(types.ErrPriceMismatch, "mixed price quotes %s and %s", numeraire, pp.Quote)
		}
		priceOf[pp.Base] = pp.Price
	}
	if numeraire != "" {
		if p, ok := priceOf[numeraire]; ok && !p.Equal(math.LegacyOneDec()) {
			return "", nil, errorsmod.Wrapf(types.ErrPriceMismatch, "numeraire %s self-price %s != 1", numeraire, p)
		}
		priceOf[numeraire] = math.LegacyOneDec()
	}
	return numeraire, priceOf, nil
}

// flowOf reads a party's net flow for one asset, returning a valid ZeroInt for
// an absent key (a bare math.Int zero value has a nil big.Int and panics on use).
func flowOf(row map[string]math.Int, key string) math.Int {
	if v, ok := row[key]; ok {
		return v
	}
	return math.ZeroInt()
}

// negPart returns max(0, -n) — the amount a party GAVE of an asset (net flow is
// negative for a giver). posPart returns max(0, n) — the amount RECEIVED.
func negPart(n math.Int) math.Int {
	if n.IsNegative() {
		return n.Neg()
	}
	return math.ZeroInt()
}

func posPart(n math.Int) math.Int {
	if n.IsPositive() {
		return n
	}
	return math.ZeroInt()
}

// intentSurplus is the SPEC §6.4 per-intent surplus: the margin between the
// clearing terms and the party's own decrypted limit, valued in the match
// numeraire. It is the sum of the give-side saving (cap − given) and the
// receive-side excess (recv − floor), each priced at the bound clearing prices
// and each ≥ 0 because Half-A rule 3 guarantees given ≤ cap and recv ≥ floor.
// LegacyDec throughout; no floats (§11.6). Terms are clamped at zero so a
// solution that somehow slipped a cap/floor (e.g. a direct Conclude that never
// ran validateSolution) can never produce a negative surplus.
func intentSurplus(mi matchIntent, flow map[string]math.Int, priceOf map[string]math.LegacyDec) (math.LegacyDec, error) {
	surplus := math.LegacyZeroDec()

	for i := range mi.payload.Give {
		key, cap, err := intentLegKeyAmount(mi.payload.Give[i])
		if err != nil {
			return math.LegacyDec{}, err
		}
		p, ok := priceOf[key]
		if !ok {
			return math.LegacyDec{}, errorsmod.Wrapf(types.ErrPriceMismatch, "surplus: give asset %s unpriced", key)
		}
		given := negPart(flowOf(flow, key))
		if saved := cap.Sub(given); saved.IsPositive() {
			surplus = surplus.Add(p.MulInt(saved))
		}
	}

	for i := range mi.payload.Want {
		key, floor, err := intentLegKeyAmount(mi.payload.Want[i])
		if err != nil {
			return math.LegacyDec{}, err
		}
		p, ok := priceOf[key]
		if !ok {
			return math.LegacyDec{}, errorsmod.Wrapf(types.ErrPriceMismatch, "surplus: want asset %s unpriced", key)
		}
		recv := posPart(flowOf(flow, key))
		if excess := recv.Sub(floor); excess.IsPositive() {
			surplus = surplus.Add(p.MulInt(excess))
		}
	}

	return surplus, nil
}

// scoreSolution computes a solution's SPEC §6.4 score: Σ over every matched
// intent of intentSurplus, normalized to the match numeraire via the bound
// clearing prices. It re-derives the net flows with the same CheckConservation
// the validator used, so scoring measures exactly the graph that will settle.
// Deterministic and pure-read — the winner-selection caller persists the score.
func (k Keeper) scoreSolution(ctx sdk.Context, sol types.Solution) (math.LegacyDec, error) {
	total := math.LegacyZeroDec()
	for mi := range sol.Matches {
		m := sol.Matches[mi]
		flows, err := CheckConservation([]types.Match{m})
		if err != nil {
			return math.LegacyDec{}, err
		}
		_, priceOf, err := matchNumeraire(m)
		if err != nil {
			return math.LegacyDec{}, err
		}
		for _, id := range m.IntentIds {
			rec, payload, err := k.veil().GetOpenIntent(ctx, phase0Chamber, id)
			if err != nil {
				return math.LegacyDec{}, err
			}
			mi := matchIntent{rec: rec, payload: payload, sigil: rec.Intent.SenderSigil}
			s, err := intentSurplus(mi, flows[mi.sigil], priceOf)
			if err != nil {
				return math.LegacyDec{}, err
			}
			total = total.Add(s)
		}
	}
	return total, nil
}

// selectWinner implements SPEC §6.4 deterministic winner selection: the stored
// solution with the highest score wins; ties break to the LOWER solution id
// (lexicographic, §6.4 "lower solution tx hash"). SolutionsForSession returns
// solutions in ascending id order, so a strict-greater comparison keeps the
// first (lowest-id) solution on a tie. The computed score is written back onto
// each solution. Returns ok=false when the session has no stored solutions.
func (k Keeper) selectWinner(ctx sdk.Context, sessionID uint64) (types.Solution, bool, error) {
	sols, err := k.SolutionsForSession(ctx, sessionID)
	if err != nil {
		return types.Solution{}, false, err
	}
	if len(sols) == 0 {
		return types.Solution{}, false, nil
	}

	bestIdx := -1
	best := math.LegacyZeroDec()
	for i := range sols {
		score, err := k.scoreSolution(ctx, sols[i])
		if err != nil {
			// a stored solution that no longer scores (e.g. an intent vanished)
			// is skipped, never a chain halt; it simply cannot win.
			continue
		}
		sols[i].Score = score
		if err := k.setSolution(ctx, sols[i]); err != nil {
			return types.Solution{}, false, err
		}
		if bestIdx == -1 || score.GT(best) {
			bestIdx = i
			best = score
		}
	}
	if bestIdx == -1 {
		return types.Solution{}, false, nil
	}
	return sols[bestIdx], true, nil
}
