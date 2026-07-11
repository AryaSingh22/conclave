package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/session/types"
)

// Task 2.5 session-TWAP tests. The determinism story: pure LegacyDec, height
// windows, observations only from committed clearing prices; the golden
// values below pin the exact Dec output so a silent rounding change fails by
// name (the same discipline as the kyber selection-rule probe, D-067).

func dec(s string) math.LegacyDec { return math.LegacyMustNewDecFromStr(s) }

func pp(base, quote, price string) types.PricePoint {
	return types.PricePoint{Base: base, Quote: quote, Price: dec(price)}
}

// twapParams gives the fixture a small window so boundary tests are exact.
func twapParams(t testingT, f *fixture, window, minObs uint64) {
	t.Helper()
	p, err := f.k.GetParams(f.ctx)
	require.NoError(t, err)
	p.TwapWindowBlocks = window
	p.TwapMinObservations = minObs
	require.NoError(t, f.k.SetParams(f.ctx, p))
}

func (f *fixture) at(height int64) sdk.Context { return f.ctx.WithBlockHeight(height) }

func TestComputeTwapPerHeightMeanThenAcrossHeights(t *testing.T) {
	// plan decision (B): one block of many sessions counts as ONE height.
	obs := []types.PriceObservation{
		{Height: 5, Price: dec("1.0")},
		{Height: 5, Price: dec("3.0")}, // height 5 mean = 2.0
		{Height: 8, Price: dec("4.0")}, // height 8 mean = 4.0
	}
	require.Equal(t, dec("3.0"), computeTwap(obs), "mean of per-height means, not of raw observations (raw mean would be 8/3)")

	// golden rounding pin: (1 + 1 + 2)/3 heights = 4/3 at 18 decimals
	obs = []types.PriceObservation{
		{Height: 1, Price: dec("1.0")},
		{Height: 2, Price: dec("1.0")},
		{Height: 3, Price: dec("2.0")},
	}
	require.Equal(t, "1.333333333333333333", computeTwap(obs).String(), "exact Dec division output is part of consensus — pin it")

	require.True(t, computeTwap(nil).IsZero())
}

func TestRecordClearingPricesSkipsAndAggregates(t *testing.T) {
	f := setupKeeper(t)
	twapParams(t, f, 100, 1)
	ctx := f.at(10)

	require.NoError(t, f.k.RecordClearingPrices(ctx, []types.PricePoint{
		pp(bondDenom, usdcDenom, "2.0"),
		pp(usdcDenom, usdcDenom, "1.0"),                                // self-pair: no information, skipped
		{Base: bondDenom, Quote: "other", Price: math.LegacyZeroDec()}, // non-positive: skipped
		pp(bondDenom, usdcDenom, "4.0"),
	}))

	rec, found, err := f.k.GetTwapRecord(ctx, bondDenom, usdcDenom)
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, rec.Observations, 2)
	require.Equal(t, dec("3.0"), rec.Twap, "same-height mean")

	_, found, err = f.k.GetTwapRecord(ctx, usdcDenom, usdcDenom)
	require.NoError(t, err)
	require.False(t, found, "self-pair never indexed")
	_, found, err = f.k.GetTwapRecord(ctx, bondDenom, "other")
	require.NoError(t, err)
	require.False(t, found, "non-positive price never indexed")
}

func TestTwapColdStartMinObservations(t *testing.T) {
	f := setupKeeper(t)
	twapParams(t, f, 100, 3)

	// 2 of 3 required observations: cold — the fee seam must keep the fixed rate
	require.NoError(t, f.k.RecordClearingPrices(f.at(10), []types.PricePoint{pp(bondDenom, usdcDenom, "2.0")}))
	require.NoError(t, f.k.RecordClearingPrices(f.at(11), []types.PricePoint{pp(bondDenom, usdcDenom, "2.0")}))
	_, warm := f.k.TwapPrice(f.at(11), bondDenom, usdcDenom)
	require.False(t, warm, "below twap_min_observations the TWAP is withheld (cold start)")

	// 3rd observation: warm
	require.NoError(t, f.k.RecordClearingPrices(f.at(12), []types.PricePoint{pp(bondDenom, usdcDenom, "2.0")}))
	price, warm := f.k.TwapPrice(f.at(12), bondDenom, usdcDenom)
	require.True(t, warm)
	require.Equal(t, dec("2.0"), price)

	// never-indexed pair: cold, not an error
	_, warm = f.k.TwapPrice(f.at(12), "nope", usdcDenom)
	require.False(t, warm)
}

func TestTwapStalenessBoundaryExact(t *testing.T) {
	f := setupKeeper(t)
	const window = 5
	twapParams(t, f, window, 1)

	require.NoError(t, f.k.RecordClearingPrices(f.at(10), []types.PricePoint{pp(bondDenom, usdcDenom, "2.0")}))

	// newest obs at h=10, window 5: in-window while h-window+1 <= 10, i.e. h <= 14
	_, warm := f.k.TwapPrice(f.at(14), bondDenom, usdcDenom)
	require.True(t, warm, "last in-window height")
	_, warm = f.k.TwapPrice(f.at(15), bondDenom, usdcDenom)
	require.False(t, warm, "the whole window passed with no settlement → cold (re-cold), fee reverts to fallback")
}

func TestTwapPruneOnWriteBoundaryExact(t *testing.T) {
	f := setupKeeper(t)
	const window = 5
	twapParams(t, f, window, 1)

	require.NoError(t, f.k.RecordClearingPrices(f.at(10), []types.PricePoint{pp(bondDenom, usdcDenom, "2.0")}))
	// writing at h=14: floor = 10 → the h=10 obs survives
	require.NoError(t, f.k.RecordClearingPrices(f.at(14), []types.PricePoint{pp(bondDenom, usdcDenom, "4.0")}))
	rec, _, err := f.k.GetTwapRecord(f.at(14), bondDenom, usdcDenom)
	require.NoError(t, err)
	require.Len(t, rec.Observations, 2)
	require.Equal(t, dec("3.0"), rec.Twap)

	// writing at h=16: floor = 12 → the h=10 obs is pruned; only 14 and 16 remain
	require.NoError(t, f.k.RecordClearingPrices(f.at(16), []types.PricePoint{pp(bondDenom, usdcDenom, "6.0")}))
	rec, _, err = f.k.GetTwapRecord(f.at(16), bondDenom, usdcDenom)
	require.NoError(t, err)
	require.Len(t, rec.Observations, 2)
	require.Equal(t, uint64(14), rec.Observations[0].Height)
	require.Equal(t, dec("5.0"), rec.Twap, "pruned window: (4+6)/2")
}

func TestTwapIbcVoucherDenomPair(t *testing.T) {
	// the ibc/<hash> voucher is an ordinary denom to the index — the 2.5
	// real-USDC pair works through the same generic path as uusdc.
	f := setupKeeper(t)
	twapParams(t, f, 100, 1)
	const voucher = "ibc/27394FB092D2ECCD56123C74F36E4C1F926001CEADA9CA97EA622B25F41E5EB2"

	require.NoError(t, f.k.RecordClearingPrices(f.at(10), []types.PricePoint{pp(bondDenom, voucher, "2.5")}))
	price, warm := f.k.TwapPrice(f.at(10), bondDenom, voucher)
	require.True(t, warm)
	require.Equal(t, dec("2.5"), price)
}

func TestTwapGenesisRoundTrip(t *testing.T) {
	f := setupKeeper(t)
	twapParams(t, f, 100, 1)
	require.NoError(t, f.k.RecordClearingPrices(f.at(10), []types.PricePoint{
		pp(bondDenom, usdcDenom, "2.0"),
		pp(svcDenom, usdcDenom, "7.0"),
	}))

	gs := f.k.ExportGenesis(f.ctx)
	require.Len(t, gs.TwapRecords, 2)
	require.NoError(t, gs.Validate())

	f2 := setupKeeper(t)
	f2.k.InitGenesis(f2.ctx, *gs)
	rec, found, err := f2.k.GetTwapRecord(f2.at(10), bondDenom, usdcDenom)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, dec("2.0"), rec.Twap)
	require.Len(t, rec.Observations, 1)
}

func TestTwapWindowZeroDisablesIndex(t *testing.T) {
	f := setupKeeper(t)
	twapParams(t, f, 0, 0)
	require.NoError(t, f.k.RecordClearingPrices(f.at(10), []types.PricePoint{pp(bondDenom, usdcDenom, "2.0")}))
	_, found, err := f.k.GetTwapRecord(f.at(10), bondDenom, usdcDenom)
	require.NoError(t, err)
	require.False(t, found, "window 0 disables recording")
	_, warm := f.k.TwapPrice(f.at(10), bondDenom, usdcDenom)
	require.False(t, warm)
}
