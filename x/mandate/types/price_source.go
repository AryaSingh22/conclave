package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// PriceSource values intent give legs for spend-ceiling enforcement
// (SPEC §6.2 step 4: "valued via the session module's last clearing
// prices (conservative: if no price, treat limit as face value)").
//
// CONCLAVE-TODO(phase-3): swap FaceValuePriceSource for an x/session-backed
// implementation reading clearing prices — the D-044 PriceSource swap, owned
// by task 3.4 (mandate-bound completeness; AUDIT F-3). RE-TAGGED from phase-0
// at PHASE2_DESIGN Rev 3 (D-090): the marker pointed at task 0.8, which
// completed in Phase 0, and the debt then floated unowned across two phases —
// which is precisely how it resurfaced as external audit finding F-3
// (METHODOLOGY Rule 6's own counterexample).
//
// The price index this swap will read now EXISTS: task 2.5 (D-082) built the
// x/session per-pair TWAP (store 0x0B, gRPC `Twap`). It is deliberately NOT
// wired here — D-082's containment argument is that the TWAP has exactly ONE
// consumer (the §6.2 fee-conversion seam), so a manipulated rate can mis-price
// gas against a genesis-funded pool but CANNOT move mandate-ceiling value.
// Task 3.4 re-reviews that exposure on its own terms before reusing the index.
type PriceSource interface {
	// GiveLegValue returns the conservative spend value of a single give leg.
	GiveLegValue(ctx sdk.Context, leg *veiltypes.IntentLeg) sdk.Coins
}

// FaceValuePriceSource is the Phase-0 PriceSource: no clearing prices exist
// yet, so every coin leg is valued at its face amount in its own denom.
type FaceValuePriceSource struct{}

var _ PriceSource = FaceValuePriceSource{}

// GiveLegValue implements SPEC §6.2 step 4's conservative face-value
// fallback. Non-coin legs (DataGrant, ServiceVoucher) value at zero under
// face-value pricing: they have no denom a Coins ceiling could bound.
//
// CONCLAVE-TODO(phase-3): known exposure (D-020, AUDIT F-3) — data/service
// give legs do not draw down spend ceilings, because face-value pricing has no
// denom for them. Owned by task 3.4, together with the swap above; clearing
// prices value svc/{schema} and data/{schema} assets (D-006). RE-TAGGED from
// phase-0 at Rev 3 (D-090) — it named task 0.8, long complete.
//
// The boundary is PINNED, not drifting: `TestSpendIntentF2BoundaryPin` (AUDIT
// F-2's sibling) asserts the zero-valuation BY NAME, so 3.4 flips it via a
// deliberate red. D-065's Reveal cross-check bounds coin legs only, for the
// same reason.
func (FaceValuePriceSource) GiveLegValue(_ sdk.Context, leg *veiltypes.IntentLeg) sdk.Coins {
	if leg == nil {
		return nil
	}
	if coin := leg.GetCoin(); coin != nil {
		if err := coin.Validate(); err != nil || coin.IsZero() {
			return nil
		}
		return sdk.NewCoins(*coin)
	}
	return nil
}
