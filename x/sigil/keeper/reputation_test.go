package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/AryaSingh22/conclave/x/sigil/types"
)

func dec(s string) math.LegacyDec { return math.LegacyMustNewDecFromStr(s) }

// TestLog10Floor pins the deterministic log10 approximation (SPEC §6.1
// value_weight, D-012): exact at powers of 10, piecewise-linear within each
// decade, zero at and below 1.
func TestLog10Floor(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"0", "0"},
		{"0.5", "0"},
		{"1", "0"},
		{"10", "1"},
		{"100", "2"},
		{"1000", "3"},
		{"10000", "4"},
		{"1000000", "6"},
		// mid-decade linear interpolation: (v/10^d - 1)/9 + d
		{"50", "1.444444444444444444"}, // (5-1)/9 + 1
		{"2", "0.111111111111111111"},  // (2-1)/9
		{"5500", "3.5"},                // (5.5-1)/9 + 3
	}
	for _, tc := range cases {
		got := log10Floor(dec(tc.in))
		require.True(t, got.Equal(dec(tc.want)), "log10Floor(%s) = %s, want %s", tc.in, got, tc.want)
	}
}

// TestValueWeight pins SPEC §6.1: value_weight = min(1, log10(value_usdc)/4).
func TestValueWeight(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"0", "0"},
		{"0.99", "0"},
		{"1", "0"},
		{"10", "0.25"},
		{"100", "0.5"},
		{"1000", "0.75"},
		{"10000", "1"},   // 4/4 → exactly 1
		{"1000000", "1"}, // clamped
	}
	for _, tc := range cases {
		got := valueWeight(dec(tc.in))
		require.True(t, got.Equal(dec(tc.want)), "valueWeight(%s) = %s, want %s", tc.in, got, tc.want)
	}
}

func TestAddSettlementReputation(t *testing.T) {
	ctx, k := setupKeeper(t)
	id := mustRegister(t, ctx, k, randAddr(), nil, 0)

	// 100 USDC → weight 0.5 → score += 1.0 * 0.5
	require.NoError(t, k.AddSettlementReputation(ctx, 0, id, dec("100")))
	rep, err := k.GetReputation(ctx, 0, id)
	require.NoError(t, err)
	require.Equal(t, "0.500000000000000000", rep.Score.String())
	require.Equal(t, uint64(1), rep.ConcludedCount)

	// 10k USDC → weight 1 → +1.0
	require.NoError(t, k.AddSettlementReputation(ctx, 0, id, dec("10000")))
	rep, err = k.GetReputation(ctx, 0, id)
	require.NoError(t, err)
	require.Equal(t, "1.500000000000000000", rep.Score.String())
	require.Equal(t, uint64(2), rep.ConcludedCount)

	// unknown sigil errors
	err = k.AddSettlementReputation(ctx, 0, deriveID(0x01, 0, []byte("x"), 1), dec("100"))
	require.ErrorIs(t, err, types.ErrSigilNotFound)
}

func TestPenalizeLostDispute(t *testing.T) {
	ctx, k := setupKeeper(t)
	id := mustRegister(t, ctx, k, randAddr(), nil, 0)

	// seed a high score, then penalize: 12 - 10*1.0 = 2
	rep, err := k.GetReputation(ctx, 0, id)
	require.NoError(t, err)
	rep.Score = dec("12")
	require.NoError(t, k.SetReputation(ctx, rep))

	require.NoError(t, k.PenalizeLostDispute(ctx, 0, id))
	rep, err = k.GetReputation(ctx, 0, id)
	require.NoError(t, err)
	require.Equal(t, "2.000000000000000000", rep.Score.String())
	require.Equal(t, uint64(1), rep.DisputedCount)

	// second penalty floors at zero — reputation never negative (§6.1)
	require.NoError(t, k.PenalizeLostDispute(ctx, 0, id))
	rep, err = k.GetReputation(ctx, 0, id)
	require.NoError(t, err)
	require.True(t, rep.Score.IsZero())
}

// TestApplyDecay pins the lazy decay semantics (SPEC §6.1, D-013): integer
// periods, remainder carried via LastDecayHeight.
func TestApplyDecay(t *testing.T) {
	params := types.Params{
		BaseGain:          dec("1"),
		DecayFactor:       dec("0.5"),
		DecayPeriodBlocks: 100,
		PenaltyMultiplier: dec("10"),
	}

	t.Run("sub-period gap is a no-op", func(t *testing.T) {
		rep := types.Reputation{SigilId: "x", Score: dec("8"), LastDecayHeight: 0}
		applyDecay(&rep, 99, params)
		require.Equal(t, "8.000000000000000000", rep.Score.String())
		require.Equal(t, uint64(0), rep.LastDecayHeight)
	})

	t.Run("two and a half periods decay twice, remainder carries", func(t *testing.T) {
		rep := types.Reputation{SigilId: "x", Score: dec("8"), LastDecayHeight: 0}
		applyDecay(&rep, 250, params)
		require.Equal(t, "2.000000000000000000", rep.Score.String()) // 8 * 0.5^2
		require.Equal(t, uint64(200), rep.LastDecayHeight)           // remainder 50 carried

		// 50 more blocks completes the third period
		applyDecay(&rep, 300, params)
		require.Equal(t, "1.000000000000000000", rep.Score.String())
		require.Equal(t, uint64(300), rep.LastDecayHeight)
	})

	t.Run("idempotent at same height", func(t *testing.T) {
		rep := types.Reputation{SigilId: "x", Score: dec("8"), LastDecayHeight: 0}
		applyDecay(&rep, 250, params)
		applyDecay(&rep, 250, params)
		require.Equal(t, "2.000000000000000000", rep.Score.String())
	})

	t.Run("extreme gap underflows to zero without panic", func(t *testing.T) {
		rep := types.Reputation{SigilId: "x", Score: dec("1000000000"), LastDecayHeight: 0}
		realistic := types.DefaultParams() // factor 0.995, period 86400
		applyDecay(&rep, 1_000_000_000_000_000, realistic)
		require.False(t, rep.Score.IsNegative())
		require.True(t, rep.Score.LTE(dec("0.000000000000000001")))
	})
}
