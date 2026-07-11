package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/AryaSingh22/conclave/x/mandate/types"
)

// stubTwap is a TwapSource with a fixed answer — the seam contract under
// test, not the session index (which has its own suite in x/session).
type stubTwap struct {
	price math.LegacyDec
	warm  bool
}

func (s stubTwap) TwapPrice(_ sdk.Context, _, _ string) (math.LegacyDec, bool) {
	return s.price, s.warm
}

// TestFeeAbstractionTwapSeam is the task 2.5 load-bearing seam proof:
// warm TWAP → the conversion is priced by the TWAP and the fixed param has
// NO effect (this test goes red if the seam still reads the param when
// warm); cold or unwired → the fixed fallback governs (red if the cold gate
// is bypassed and a zero/stale TWAP prices the fee).
func TestFeeAbstractionTwapSeam(t *testing.T) {
	f := setupKeeper(t)
	controller := randAddr()
	sigilID := f.registerSigil(t, controller)
	grantee := randAddr()

	_, err := f.k.IssueMandate(f.ctx, controller, validTerms(sigilID, grantee))
	require.NoError(t, err)
	m, _, err := f.k.ActiveMandateByGrantee(f.ctx, 0, grantee)
	require.NoError(t, err)

	poolAddr := authtypes.NewModuleAddress(types.ModuleName)
	collectorAddr := authtypes.NewModuleAddress(authtypes.FeeCollectorName)
	f.bank.fund(grantee.String(), sdk.NewCoins(sdk.NewInt64Coin("uusdc", 1_000_000)))
	f.bank.fund(poolAddr.String(), sdk.NewCoins(sdk.NewInt64Coin("uclv", 10_000_000)))
	fee := sdk.NewCoins(sdk.NewInt64Coin("uusdc", 1_000))

	collectorClv := func() math.Int { return f.bank.GetBalance(f.ctx, collectorAddr, "uclv").Amount }

	// (1) no TwapSource wired (nil): fixed fallback at the dev 1:1 rate
	require.NoError(t, f.k.ExecuteFeeAbstraction(f.ctx, m, fee))
	require.Equal(t, math.NewInt(1_000), collectorClv(), "nil source → fallback param rate")

	// (2) warm TWAP price 2.0 (fee-denom per uclv): converted = 1000/2 = 500
	f.k.SetTwapSource(stubTwap{price: math.LegacyNewDec(2), warm: true})
	require.NoError(t, f.k.ExecuteFeeAbstraction(f.ctx, m, fee))
	require.Equal(t, math.NewInt(1_500), collectorClv(), "warm TWAP → fee/price")

	// (3) THE seam proof: crank the fixed param to 7 — a warm conversion
	// must not move.
	p, err := f.k.GetParams(f.ctx)
	require.NoError(t, err)
	p.FeeConversionRate = math.LegacyNewDec(7)
	require.NoError(t, f.k.SetParams(f.ctx, p))
	require.NoError(t, f.k.ExecuteFeeAbstraction(f.ctx, m, fee))
	require.Equal(t, math.NewInt(2_000), collectorClv(), "param change must not affect a warm TWAP conversion")

	// (4) cold source: the (now 7x) fallback governs again
	f.k.SetTwapSource(stubTwap{price: math.LegacyZeroDec(), warm: false})
	require.NoError(t, f.k.ExecuteFeeAbstraction(f.ctx, m, fee))
	require.Equal(t, math.NewInt(9_000), collectorClv(), "cold → fixed fallback rate (7000 uclv)")

	// (5) pool-favoring truncation on the TWAP path: 1000/3 → 333
	f.k.SetTwapSource(stubTwap{price: math.LegacyNewDec(3), warm: true})
	require.NoError(t, f.k.ExecuteFeeAbstraction(f.ctx, m, fee))
	require.Equal(t, math.NewInt(9_333), collectorClv(), "truncation favors the pool")
}
