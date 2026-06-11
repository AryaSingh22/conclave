package keeper

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/sigil/types"
)

// TestFuzzRotationConcurrentRecovery is the SPEC §6.1 fuzz target "key
// rotation under concurrent recovery attempts": random sequences of
// competing rotation attempts (controller-signed, recovery subsets,
// strangers, stale ex-controllers) must authorize exactly per the §6.1 rule
// and always leave the reverse index consistent.
func TestFuzzRotationConcurrentRecovery(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// fixture creation uses the outer *testing.T (rapid.T lacks Cleanup);
		// property assertions use rt so rapid can shrink failures
		ctx, k := setupKeeper(t)

		recoverySize := rapid.IntRange(1, 4).Draw(rt, "recovery_size")
		threshold := uint32(rapid.IntRange(0, recoverySize).Draw(rt, "threshold"))

		controller := randAddr()
		recovery := make([]sdk.AccAddress, recoverySize)
		recoveryStr := make([]string, recoverySize)
		for i := range recovery {
			recovery[i] = randAddr()
			recoveryStr[i] = recovery[i].String()
		}

		id, err := k.RegisterSigil(ctx, controller, validDIDHash(), "", recoveryStr, threshold, 0)
		require.NoError(rt, err)

		current := controller
		exControllers := []sdk.AccAddress{}

		attempts := rapid.IntRange(1, 8).Draw(rt, "attempts")
		for i := 0; i < attempts; i++ {
			// build a random signer set from recovery members, the current
			// controller, stale ex-controllers, and strangers
			var signers []sdk.AccAddress
			distinctRecovery := map[string]bool{}
			includesCurrent := false

			for j, r := range recovery {
				n := rapid.IntRange(0, 2).Draw(rt, "rec_count")
				for c := 0; c < n; c++ { // duplicates must not double count
					signers = append(signers, r)
				}
				if n > 0 {
					distinctRecovery[recoveryStr[j]] = true
				}
			}
			if rapid.Bool().Draw(rt, "include_current") {
				signers = append(signers, current)
				includesCurrent = true
			}
			if len(exControllers) > 0 && rapid.Bool().Draw(rt, "include_ex") {
				signers = append(signers, exControllers[0]) // stale key: must carry no authority
			}
			if rapid.Bool().Draw(rt, "include_stranger") {
				signers = append(signers, randAddr())
			}
			if len(signers) == 0 {
				continue
			}

			authorized := includesCurrent ||
				(threshold > 0 && uint32(len(distinctRecovery)) >= threshold)

			target := randAddr()
			err := k.RotateController(ctx, 0, id, signers, target)

			if authorized {
				require.NoError(rt, err, "attempt %d should be authorized", i)
				exControllers = append([]sdk.AccAddress{current}, exControllers...)
				current = target
			} else {
				require.ErrorIs(rt, err, types.ErrUnauthorized, "attempt %d should be rejected", i)
			}

			// the reverse index must stay consistent after every attempt
			_, broken := AddressIndexInvariant(k)(ctx)
			require.False(rt, broken, "address index broken after attempt %d", i)
		}

		// final state: current controller resolves to the sigil, every
		// ex-controller resolves to nothing
		gotID, err := k.SigilIDByAddress(ctx, 0, current)
		require.NoError(rt, err)
		require.Equal(rt, id, gotID)
		for _, ex := range exControllers {
			_, err := k.SigilIDByAddress(ctx, 0, ex)
			require.ErrorIs(rt, err, types.ErrSigilNotFound)
		}
	})
}

// TestFuzzDecayExtremeGaps is the SPEC §6.1 fuzz target "decay arithmetic
// over extreme block gaps": arbitrary factors, periods, and height jumps up
// to 10^12 blocks must never panic, overflow, underflow below zero, or
// increase the score.
func TestFuzzDecayExtremeGaps(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		params := types.Params{
			BaseGain: math.LegacyOneDec(),
			DecayFactor: math.LegacyNewDecWithPrec(
				int64(rapid.IntRange(0, 1000).Draw(rt, "factor_milli")), 3),
			DecayPeriodBlocks: rapid.Uint64Range(1, 1_000_000).Draw(rt, "period"),
			PenaltyMultiplier: math.LegacyNewDec(10),
		}

		score := math.LegacyNewDec(int64(rapid.Uint64Range(0, 1_000_000_000).Draw(rt, "score")))
		rep := types.Reputation{SigilId: "fuzz", Score: score, LastDecayHeight: 0}

		height := uint64(0)
		jumps := rapid.IntRange(1, 6).Draw(rt, "jumps")
		for i := 0; i < jumps; i++ {
			height += rapid.Uint64Range(0, 1_000_000_000_000).Draw(rt, "jump")

			before := rep.Score
			applyDecay(&rep, height, params)

			require.False(rt, rep.Score.IsNegative(), "score went negative")
			require.True(rt, rep.Score.LTE(before), "pure decay increased the score")
			require.True(rt, rep.LastDecayHeight <= height, "decay height ran ahead of chain height")

			// idempotence: re-applying at the same height is a no-op
			again := rep
			applyDecay(&again, height, params)
			require.True(rt, again.Score.Equal(rep.Score), "decay not idempotent at fixed height")
			require.Equal(rt, rep.LastDecayHeight, again.LastDecayHeight)
		}
	})
}
