package types

import (
	errorsmod "cosmossdk.io/errors"
)

// Default parameter values (PHASE1_DESIGN §2(b), D-068).
const (
	// DefaultEpochLengthBlocks is the epoch cadence: ~1 day at a ~5s block time
	// (§2(b) "epochs are short, ~1 day"). Deliberately far larger than the
	// 500-block determinism sim so no boundary fires there (x/epoch stays
	// dormant in the sim — see D-068).
	DefaultEpochLengthBlocks = 17_280

	// DKG transport window lengths (blocks). The five rounds (register → deal →
	// response → justify → confirm) run in sequence inside the FORMING epoch;
	// their sum must stay below the epoch length so a DKG completes before the
	// next boundary.
	DefaultRegisterWindowBlocks = 10
	DefaultDealWindowBlocks     = 10
	DefaultResponseWindowBlocks = 10
	DefaultJustifyWindowBlocks  = 10
	DefaultConfirmWindowBlocks  = 10

	// DefaultMinCommitteeSize is the smallest bonded set that opens a DKG. n=4
	// gives t=floor(2*4/3)+1=3 and tolerates n-t=1 fault — the smallest set with
	// any fault tolerance. Below it the boundary keeps the prior key (degraded).
	DefaultMinCommitteeSize = 4

	// DefaultMaxBundleBytes caps one opaque DKG transport message (DoS bound).
	DefaultMaxBundleBytes = 1 << 20 // 1 MiB
)

// DefaultParams returns the default x/epoch parameters.
func DefaultParams() Params {
	return Params{
		EpochLengthBlocks:    DefaultEpochLengthBlocks,
		RegisterWindowBlocks: DefaultRegisterWindowBlocks,
		DealWindowBlocks:     DefaultDealWindowBlocks,
		ResponseWindowBlocks: DefaultResponseWindowBlocks,
		JustifyWindowBlocks:  DefaultJustifyWindowBlocks,
		ConfirmWindowBlocks:  DefaultConfirmWindowBlocks,
		MinCommitteeSize:     DefaultMinCommitteeSize,
		MaxBundleBytes:       DefaultMaxBundleBytes,
	}
}

// TotalDKGWindowBlocks is the sum of all five DKG window lengths — the number of
// blocks a full DKG occupies inside one FORMING epoch.
func (p Params) TotalDKGWindowBlocks() uint64 {
	return p.RegisterWindowBlocks + p.DealWindowBlocks + p.ResponseWindowBlocks +
		p.JustifyWindowBlocks + p.ConfirmWindowBlocks
}

// Validate validates the parameter set.
func (p Params) Validate() error {
	if p.EpochLengthBlocks == 0 {
		return errorsmod.Wrap(ErrInvalidParams, "epoch_length_blocks must be positive")
	}
	if p.RegisterWindowBlocks == 0 || p.DealWindowBlocks == 0 || p.ResponseWindowBlocks == 0 ||
		p.JustifyWindowBlocks == 0 || p.ConfirmWindowBlocks == 0 {
		return errorsmod.Wrap(ErrInvalidParams, "DKG window lengths must be positive")
	}
	// the full DKG (register + deal + response + justify + confirm) must complete
	// within one epoch, strictly before the next boundary.
	if p.TotalDKGWindowBlocks() >= p.EpochLengthBlocks {
		return errorsmod.Wrapf(ErrInvalidParams,
			"DKG windows (%d total) must fit within epoch_length_blocks %d",
			p.TotalDKGWindowBlocks(), p.EpochLengthBlocks)
	}
	if p.MinCommitteeSize == 0 {
		return errorsmod.Wrap(ErrInvalidParams, "min_committee_size must be positive")
	}
	if p.MaxBundleBytes == 0 {
		return errorsmod.Wrap(ErrInvalidParams, "max_bundle_bytes must be positive")
	}
	return nil
}

// Threshold implements the SPEC §6.3 t-of-n rule: t = floor(2n/3) + 1, matching
// crypto/threshold and reveal.StubScheme so the chain-side committee threshold
// agrees with the off-chain DKG threshold.
func Threshold(n int) int { return 2*n/3 + 1 }
