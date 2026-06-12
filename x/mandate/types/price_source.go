package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// PriceSource values intent give legs for spend-ceiling enforcement
// (SPEC §6.2 step 4: "valued via the session module's last clearing
// prices (conservative: if no price, treat limit as face value)").
//
// CONCLAVE-TODO(phase-0): task 0.8 provides the x/session implementation
// backed by last clearing prices / session TWAP; app wiring then swaps
// FaceValuePriceSource for it.
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
// CONCLAVE-TODO(phase-0): known Phase-0 exposure (D-020) — data/service give
// legs do not draw down spend ceilings until task 0.8 wires session clearing
// prices, which value svc/{schema} and data/{schema} assets (D-006).
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
