package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// EndBlocker runs the x/session EndBlock work: the full SPEC §6.4 order of
// operations — (1) advance session states, (3) scoring, (4) conclude winner or
// fallback, (5) rollover sweep — plus the solver unbonding sweep (RunEndBlock).
//
// The module is registered in the EndBlocker chain immediately BEFORE x/veil
// (app_config.go) so x/session steps 1–5 precede x/veil's expiry sweep +
// boundary seal (steps 6–7), exactly the §6.4 order pinned by D-038. The
// Reveal hand-off intake (step 2) stays in x/veil's BeginBlock, feeding
// IntakeRevealed.
func (k Keeper) EndBlocker(ctx sdk.Context) error {
	return k.RunEndBlock(ctx)
}
