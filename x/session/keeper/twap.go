package keeper

import (
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/session/types"
)

// The session TWAP (task 2.5) replaces the fixed §6.2 fee_conversion_rate
// with a windowed average of committed clearing prices. Observations enter
// ONLY from Conclude, inside the all-or-nothing settlement cache, so every
// sample is a consensus value every node recorded identically. The TWAP is
// recomputed at WRITE time and consumers read the stored value verbatim —
// the fee-abstraction ante path never iterates or averages (§11.6 and the
// CheckTx hot path stay O(1)).
//
// Staleness semantics (documented, tested): the stored TWAP reflects the
// window as of the LAST settlement that touched the pair. It is served while
// its newest observation is still inside the current window; once the whole
// window passes with no settlement, the pair goes cold and consumers fall
// back to their fixed rate. min_observations is evaluated against the
// write-time window (the pruned observation set).
//
// This price index has exactly ONE consumer — ExecuteFeeAbstraction via the
// mandate TwapSource seam. It is deliberately NOT offered to ceiling
// valuation: the D-044 ante PriceSource swap (AUDIT F-3) stays a separate
// task (Phase-3 3.4), which will reuse this index under its own review.

// GetTwapRecord loads the price index record for one pair; found=false when
// the pair has never been indexed (or its keys cannot form a store key).
func (k Keeper) GetTwapRecord(ctx sdk.Context, base, quote string) (types.TwapRecord, bool, error) {
	key, ok := types.TwapKey(base, quote)
	if !ok {
		return types.TwapRecord{}, false, nil
	}
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(key)
	if err != nil {
		return types.TwapRecord{}, false, err
	}
	if bz == nil {
		return types.TwapRecord{}, false, nil
	}
	var rec types.TwapRecord
	if err := k.cdc.Unmarshal(bz, &rec); err != nil {
		return types.TwapRecord{}, false, err
	}
	return rec, true, nil
}

// SetTwapRecord persists a pair record (settlement write path + genesis
// import). Records whose pair cannot form a store key are skipped
// deterministically.
func (k Keeper) SetTwapRecord(ctx sdk.Context, rec types.TwapRecord) error {
	key, ok := types.TwapKey(rec.Base, rec.Quote)
	if !ok {
		return nil
	}
	bz, err := k.cdc.Marshal(&rec)
	if err != nil {
		return err
	}
	return k.storeService.OpenKVStore(ctx).Set(key, bz)
}

// IterateTwapRecords walks every pair record in byte order of the store key
// (deterministic; genesis export).
func (k Keeper) IterateTwapRecords(ctx sdk.Context, fn func(types.TwapRecord) bool) error {
	store := k.storeService.OpenKVStore(ctx)
	it, err := store.Iterator(types.TwapKeyPrefix, types.PrefixEnd(types.TwapKeyPrefix))
	if err != nil {
		return err
	}
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var rec types.TwapRecord
		if err := k.cdc.Unmarshal(it.Value(), &rec); err != nil {
			return err
		}
		if fn(rec) {
			return nil
		}
	}
	return nil
}

// RecordClearingPrices ingests one settled match's clearing prices into the
// per-pair index: append at the settlement height, prune to the window,
// recompute the stored TWAP. Called inside the Conclude cache (with the
// receipts), so observations commit atomically with the settlement they
// price. Deterministic: prices arrive in the match's validated order;
// non-positive prices and self-pairs carry no information and are skipped.
func (k Keeper) RecordClearingPrices(ctx sdk.Context, prices []types.PricePoint) error {
	if len(prices) == 0 {
		return nil
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	if params.TwapWindowBlocks == 0 {
		return nil // index disabled: no window, nothing can ever qualify
	}
	height := uint64(ctx.BlockHeight())
	minHeight := windowFloor(height, params.TwapWindowBlocks)

	for i := range prices {
		pp := prices[i]
		if pp.Base == pp.Quote || pp.Price.IsNil() || !pp.Price.IsPositive() {
			continue
		}
		rec, found, err := k.GetTwapRecord(ctx, pp.Base, pp.Quote)
		if err != nil {
			return err
		}
		if !found {
			rec = types.TwapRecord{Base: pp.Base, Quote: pp.Quote}
		}
		rec.Observations = append(rec.Observations, types.PriceObservation{
			Height: height,
			Price:  pp.Price,
		})
		rec.Observations = pruneObservations(rec.Observations, minHeight)
		rec.Twap = computeTwap(rec.Observations)
		if err := k.SetTwapRecord(ctx, rec); err != nil {
			return err
		}
	}
	return nil
}

// TwapPrice implements the mandate TwapSource seam (task 2.5): the current
// TWAP price of base quoted in quote, and whether it is WARM — served only
// when the newest observation is still inside the window (staleness gate),
// the write-time observation count meets twap_min_observations, and the
// stored value is positive. Cold pairs return (zero, false) and the consumer
// uses its fixed fallback. Pure read: no writes, no iteration, O(1) on the
// ante path.
func (k Keeper) TwapPrice(ctx sdk.Context, base, quote string) (math.LegacyDec, bool) {
	params, err := k.GetParams(ctx)
	if err != nil || params.TwapWindowBlocks == 0 {
		return math.LegacyZeroDec(), false
	}
	rec, found, err := k.GetTwapRecord(ctx, base, quote)
	if err != nil || !found || len(rec.Observations) == 0 {
		return math.LegacyZeroDec(), false
	}
	minHeight := windowFloor(uint64(ctx.BlockHeight()), params.TwapWindowBlocks)
	newest := rec.Observations[len(rec.Observations)-1].Height
	if newest < minHeight {
		return math.LegacyZeroDec(), false // whole window passed with no settlement
	}
	if uint64(len(rec.Observations)) < params.TwapMinObservations {
		return math.LegacyZeroDec(), false // cold start: not enough history
	}
	if rec.Twap.IsNil() || !rec.Twap.IsPositive() {
		return math.LegacyZeroDec(), false
	}
	return rec.Twap, true
}

// windowFloor returns the lowest height still inside a window ending at
// height (inclusive; window >= 1).
func windowFloor(height, window uint64) uint64 {
	if height < window {
		return 0
	}
	return height - window + 1
}

// pruneObservations drops entries below the window floor. Observations are
// appended at non-decreasing heights, so the suffix from the first in-window
// entry is the in-window set.
func pruneObservations(obs []types.PriceObservation, minHeight uint64) []types.PriceObservation {
	for i := range obs {
		if obs[i].Height >= minHeight {
			return obs[i:]
		}
	}
	return nil
}

// computeTwap is the approved aggregation (task 2.5 plan decision B): mean
// per height first, then mean across heights — a block stuffed with many
// cleared sessions still counts as one height, damping intra-block spam
// skew. Pure LegacyDec; zero when there are no observations.
func computeTwap(obs []types.PriceObservation) math.LegacyDec {
	if len(obs) == 0 {
		return math.LegacyZeroDec()
	}
	heightSum := math.LegacyZeroDec() // sum of per-height means
	heights := math.LegacyZeroDec()   // number of distinct heights

	runSum := math.LegacyZeroDec() // current height's price sum
	runCount := int64(0)           // current height's observation count
	runHeight := obs[0].Height

	flush := func() {
		heightSum = heightSum.Add(runSum.QuoInt64(runCount))
		heights = heights.Add(math.LegacyOneDec())
	}
	for i := range obs {
		if obs[i].Height != runHeight {
			flush()
			runSum, runCount, runHeight = math.LegacyZeroDec(), 0, obs[i].Height
		}
		runSum = runSum.Add(obs[i].Price)
		runCount++
	}
	flush()
	return heightSum.Quo(heights)
}
