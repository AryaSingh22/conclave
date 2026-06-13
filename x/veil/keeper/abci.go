package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BeginBlocker runs the Reveal (SPEC §6.3 ABCI hooks: "The Reveal runs at
// the START of the next block's BeginBlock"). Strictly ordered after the
// EndBlock that fixed ordering — the order-then-reveal invariant is real
// even in stub mode.
//
// CONCLAVE-TODO(phase-0): at task 0.8, x/session takes over downstream
// processing per §6.4's EndBlock order of operations; the Reveal itself
// stays here.
func (k Keeper) BeginBlocker(ctx sdk.Context) error {
	return k.RevealSealed(ctx)
}

// EndBlocker runs the veil lifecycle sweeps, standing in for x/session
// EndBlock until task 0.8 (§5.2 transitions happen "only in x/session
// EndBlock or via explicit Msgs"; D-038):
//
//  1. expiry sweep — OPEN intents whose ttl_height has arrived EXPIRE
//     (with refunds) before any new boundary work, mirroring §6.4's
//     "(6) expiry sweep" placement relative to intake;
//  2. session boundary — when the SessionKeeper reports a boundary,
//     SUBMITTED intents seal into the session, fixing ordering.
func (k Keeper) EndBlocker(ctx sdk.Context) error {
	if err := k.ExpireOpen(ctx); err != nil {
		return err
	}
	if isBoundary, sessionID := k.sessionKeeper.SessionBoundary(ctx); isBoundary {
		return k.SealSubmitted(ctx, sessionID)
	}
	return nil
}
