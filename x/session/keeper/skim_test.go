package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/AryaSingh22/conclave/x/session/types"
)

// TestCheckSkimBinding is the STOP #1 build-condition #2 guard: checkSkimBinding
// never fires on the happy path, so — like 0.7's Guard 1 — it must be proven
// load-bearing by tests that go RED if the bound is removed. (a) a skim
// extracting MORE than a party's surplus (into principal) and (b) a skim routed
// to a non-pool party are both rejected; the in-bounds skim passes. Each case
// builds the skim-inclusive net flows directly and asserts the residual==-skim_p
// invariant exactly as applyEconomics runs it on CheckConservation output.
//
// VERIFIED load-bearing: deleting either guard in checkSkimBinding (the
// skimVal.GT(surplus) check, or the residual.IsPositive() non-pool check) makes
// the corresponding sub-test fail — checkSkimBinding returns nil where the test
// requires ErrSkimViolation.
func TestCheckSkimBinding(t *testing.T) {
	uusdc := "uusdc"
	priceOf := map[string]math.LegacyDec{uusdc: math.LegacyOneDec()}

	t.Run("happy: skim within surplus to the fee pool", func(t *testing.T) {
		flows := NetFlows{
			"alice":      {uusdc: math.NewInt(-50)},
			feePoolSigil: {uusdc: math.NewInt(50)},
		}
		surplus := map[string]math.LegacyDec{"alice": math.LegacyNewDec(100)}
		require.NoError(t, checkSkimBinding(flows, priceOf, surplus))
	})

	t.Run("reject: skim exceeds surplus (touches principal)", func(t *testing.T) {
		flows := NetFlows{
			"alice":      {uusdc: math.NewInt(-100)},
			feePoolSigil: {uusdc: math.NewInt(100)},
		}
		// alice's surplus is only 50, but the skim takes 100 → into principal.
		surplus := map[string]math.LegacyDec{"alice": math.LegacyNewDec(50)}
		require.ErrorIs(t, checkSkimBinding(flows, priceOf, surplus), types.ErrSkimViolation)
	})

	t.Run("reject: skim routed to a non-pool party", func(t *testing.T) {
		flows := NetFlows{
			"alice":   {uusdc: math.NewInt(-100)},
			"mallory": {uusdc: math.NewInt(100)}, // a real sigil, NOT the fee pool
		}
		surplus := map[string]math.LegacyDec{"alice": math.LegacyNewDec(1000)}
		require.ErrorIs(t, checkSkimBinding(flows, priceOf, surplus), types.ErrSkimViolation)
	})

	t.Run("reject: pool residual != total skim taken", func(t *testing.T) {
		flows := NetFlows{
			"alice":      {uusdc: math.NewInt(-100)},
			feePoolSigil: {uusdc: math.NewInt(60)}, // 40 vanished
		}
		surplus := map[string]math.LegacyDec{"alice": math.LegacyNewDec(1000)}
		require.ErrorIs(t, checkSkimBinding(flows, priceOf, surplus), types.ErrSkimViolation)
	})
}
