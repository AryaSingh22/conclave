package ante

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/mandate/types"
)

// TestFuzzConcurrentTxsNeverExceedWindow is the SPEC §6.2 fuzz target
// "concurrent unordered txs draining a window in one block (must never
// exceed ceiling)": random sequences of txs with random spend intents,
// random CheckTx/DeliverTx mode, and random height advances are run through
// the MandateDecorator against shared state — exactly how N unordered txs
// from one key land in one block. Property: the sum of accepted spends
// within any single window NEVER exceeds the window ceiling, regardless of
// interleaving, because every accepted tx charged the window optimistically
// before the next one was checked.
func TestFuzzConcurrentTxsNeverExceedWindow(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		f := setup(t)

		windowBlocks := uint64(rapid.IntRange(1, 50).Draw(rt, "window_blocks"))
		windowCeiling := int64(rapid.IntRange(1, 1_000).Draw(rt, "window_ceiling"))
		perTxCeiling := int64(rapid.IntRange(1, 1_000).Draw(rt, "per_tx_ceiling"))
		f.issue(t, []string{sendURL}, func(terms *types.MandateTerms) {
			terms.SpendCeilingPerTx = usdc(perTxCeiling)
			terms.SpendCeilingWindow = usdc(windowCeiling)
			terms.WindowBlocks = windowBlocks
		})

		// reference model mirroring ChargeWindow exactly: a reset is computed
		// per charge attempt and persists only with a successful charge
		modelStart := uint64(f.ctx.BlockHeight())
		modelSpent := int64(0)

		numTxs := rapid.IntRange(1, 60).Draw(rt, "num_txs")
		for i := 0; i < numTxs; i++ {
			// most txs land in the same block (the unordered-tx scenario);
			// some advance the height, occasionally across the boundary
			if rapid.IntRange(0, 4).Draw(rt, "advance") == 0 {
				jump := int64(rapid.IntRange(1, int(windowBlocks)+3).Draw(rt, "jump"))
				f.ctx = f.ctx.WithBlockHeight(f.ctx.BlockHeight() + jump)
			}
			f.ctx = f.ctx.WithIsCheckTx(rapid.Bool().Draw(rt, "check_tx"))

			height := uint64(f.ctx.BlockHeight())
			effStart, effSpent := modelStart, modelSpent
			if height >= modelStart+windowBlocks {
				effStart, effSpent = height, 0
			}

			amt := int64(rapid.IntRange(1, 1_200).Draw(rt, "spend"))
			err := f.runMandateDecorator(t, f.buildTx(t, nil, bankSend(f.grantee, usdc(amt))))

			if amt <= perTxCeiling && effSpent+amt <= windowCeiling {
				require.NoError(rt, err)
				modelStart, modelSpent = effStart, effSpent+amt
				// THE property: accepted spend in any window never exceeds
				// the ceiling, and the persisted accounting agrees exactly
				require.LessOrEqual(rt, modelSpent, windowCeiling,
					"accepted spend exceeded the window ceiling")
				require.Equal(rt, usdc(modelSpent).String(), f.windowSpent(t).String())
			} else {
				require.ErrorIs(rt, err, types.ErrMandateExceeded)
				// a rejected tx persists nothing: store still matches the
				// pre-tx model state
				require.Equal(rt, usdc(modelSpent).String(), f.windowSpent(t).String())
			}
		}
	})
}

// TestFuzzWindowResetBoundary is the SPEC §6.2 fuzz target "window reset
// boundary off-by-ones": for random window geometry, a charge that
// overflows the window is rejected at every height strictly inside the
// window (including start+window−1) and accepted at exactly start+window.
func TestFuzzWindowResetBoundary(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		f := setup(t)

		windowBlocks := uint64(rapid.IntRange(1, 10_000).Draw(rt, "window_blocks"))
		ceiling := int64(rapid.IntRange(2, 1_000_000).Draw(rt, "ceiling"))
		f.issue(t, []string{sendURL}, func(terms *types.MandateTerms) {
			terms.SpendCeilingPerTx = usdc(ceiling)
			terms.SpendCeilingWindow = usdc(ceiling)
			terms.WindowBlocks = windowBlocks
		})
		start := uint64(f.ctx.BlockHeight())

		// exhaust the window at its first block
		require.NoError(rt, f.runMandateDecorator(t, f.buildTx(t, nil, bankSend(f.grantee, usdc(ceiling)))))

		// inside the window (any height in [start, start+window)): rejected
		inside := start + uint64(rapid.IntRange(0, int(windowBlocks)-1).Draw(rt, "inside_offset"))
		f.ctx = f.ctx.WithBlockHeight(int64(inside))
		require.ErrorIs(rt,
			f.runMandateDecorator(t, f.buildTx(t, nil, bankSend(f.grantee, usdc(1)))),
			types.ErrMandateExceeded)

		// the exact boundary start+window: reset, full ceiling available
		f.ctx = f.ctx.WithBlockHeight(int64(start + windowBlocks))
		require.NoError(rt, f.runMandateDecorator(t, f.buildTx(t, nil, bankSend(f.grantee, usdc(ceiling)))))
	})
}

// TestFuzzNestedUnwrapDepth is the SPEC §6.2 fuzz target "nested-msg unwrap
// depth (cap recursion at 8, reject deeper → ErrMsgTooNested)": random
// wrapper chains at and beyond the cap, with the innermost spend always
// accounted when accepted.
func TestFuzzNestedUnwrapDepth(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		f := setup(t)
		f.issue(t, []string{execURL, sendURL})

		depth := rapid.IntRange(0, 2*types.DefaultMaxMsgNestingDepth).Draw(rt, "depth")
		amt := int64(rapid.IntRange(1, 500).Draw(rt, "amt"))

		var msg sdk.Msg = bankSend(f.grantee, usdc(amt))
		for i := 0; i < depth; i++ {
			msg = wrapExec(t, f.grantee, msg)
		}

		err := f.runMandateDecorator(t, f.buildTx(t, nil, msg))
		if depth > types.DefaultMaxMsgNestingDepth {
			require.ErrorIs(rt, err, types.ErrMsgTooNested)
			require.True(rt, f.windowSpent(t).IsZero(), "rejected txs charge nothing")
		} else {
			require.NoError(rt, err)
			require.Equal(rt, usdc(amt).String(), f.windowSpent(t).String(),
				"nested spend must draw down the window")
		}
	})
}
