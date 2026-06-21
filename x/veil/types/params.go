package types

import (
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Default parameter values (SPEC §6.3 bond rules, D-007).
const (
	// DefaultBondDenom is the anti-spam bond denom (SPEC §0: uclv). The
	// module reads the bond denom from Params.MinBond.Denom everywhere,
	// never from a hardcoded constant.
	DefaultBondDenom = "uclv"

	// DefaultBondBps is the bond as basis points of intent face value
	// (SPEC §6.3: bond = max(min_bond, bond_bps * face_value); D-041 defines
	// the Phase-0 face-value scalar).
	DefaultBondBps = 10

	// DefaultInvalidSlashBps is the bond fraction slashed to the community
	// pool on INVALID (SPEC §6.3 "50%" = 5000 bps, D-007).
	DefaultInvalidSlashBps = 5000

	// DefaultMaxIntentsPerSession bounds how many intents seal into one session
	// (Task 1.6, PHASE1_DESIGN §2 perf FLAG). 0 = unbounded: the Phase-0/stub
	// default, safe because epoch_id==0 (stub) intents produce no vote-extension
	// shares, so there is nothing to bound until a DKG epoch is ACTIVE. A live
	// network with encrypted intents sets a finite cap via governance.
	DefaultMaxIntentsPerSession = 0

	// DefaultMaxRevealRetries is how many blocks a session may fall short of t
	// reveal shares before CANCEL (SPEC §8(c), PHASE1_DESIGN §2(c): 3).
	DefaultMaxRevealRetries = 3

	// bpsDenominator converts basis points to a fraction.
	bpsDenominator = 10_000
)

// DefaultParams returns the default x/veil parameters.
func DefaultParams() Params {
	return Params{
		MinBond:              sdk.NewCoin(DefaultBondDenom, math.NewInt(1_000_000)), // 1 CLV
		BondBps:              DefaultBondBps,
		InvalidSlashBps:      DefaultInvalidSlashBps,
		MaxIntentsPerSession: DefaultMaxIntentsPerSession,
		MaxRevealRetries:     DefaultMaxRevealRetries,
	}
}

// Validate validates the parameter set.
func (p Params) Validate() error {
	if err := p.MinBond.Validate(); err != nil {
		return errorsmod.Wrapf(ErrInvalidParams, "min_bond: %s", err)
	}
	if !p.MinBond.Amount.IsPositive() {
		return errorsmod.Wrap(ErrInvalidParams, "min_bond must be positive")
	}
	if p.BondBps > bpsDenominator {
		return errorsmod.Wrapf(ErrInvalidParams, "bond_bps %d exceeds %d", p.BondBps, bpsDenominator)
	}
	if p.InvalidSlashBps > bpsDenominator {
		return errorsmod.Wrapf(ErrInvalidParams, "invalid_slash_bps %d exceeds %d", p.InvalidSlashBps, bpsDenominator)
	}
	return nil
}

// RequiredBond implements the SPEC §6.3 bond formula
// max(min_bond, bond_bps * face_value) for the D-041 Phase-0 face-value
// scalar (base units, denom-blind).
func (p Params) RequiredBond(faceValue math.Int) sdk.Coin {
	required := p.MinBond
	bps := faceValue.MulRaw(int64(p.BondBps)).QuoRaw(bpsDenominator)
	if bps.GT(required.Amount) {
		required = sdk.NewCoin(p.MinBond.Denom, bps)
	}
	return required
}
