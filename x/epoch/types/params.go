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

	// DKG transport window lengths (blocks). The three rounds run in sequence
	// inside the FORMING epoch; their sum must stay below the epoch length so a
	// DKG completes before the next boundary.
	DefaultDealWindowBlocks     = 10
	DefaultResponseWindowBlocks = 10
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
		DealWindowBlocks:     DefaultDealWindowBlocks,
		ResponseWindowBlocks: DefaultResponseWindowBlocks,
		ConfirmWindowBlocks:  DefaultConfirmWindowBlocks,
		MinCommitteeSize:     DefaultMinCommitteeSize,
		MaxBundleBytes:       DefaultMaxBundleBytes,
	}
}

// Validate validates the parameter set.
func (p Params) Validate() error {
	if p.EpochLengthBlocks == 0 {
		return errorsmod.Wrap(ErrInvalidParams, "epoch_length_blocks must be positive")
	}
	if p.DealWindowBlocks == 0 || p.ResponseWindowBlocks == 0 || p.ConfirmWindowBlocks == 0 {
		return errorsmod.Wrap(ErrInvalidParams, "DKG window lengths must be positive")
	}
	// the full DKG (deal + response + confirm) must complete within one epoch,
	// strictly before the next boundary.
	if p.DealWindowBlocks+p.ResponseWindowBlocks+p.ConfirmWindowBlocks >= p.EpochLengthBlocks {
		return errorsmod.Wrapf(ErrInvalidParams,
			"DKG windows (%d+%d+%d) must fit within epoch_length_blocks %d",
			p.DealWindowBlocks, p.ResponseWindowBlocks, p.ConfirmWindowBlocks, p.EpochLengthBlocks)
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
