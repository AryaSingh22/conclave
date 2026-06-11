package types

import (
	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultGenesis returns the default x/sigil genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params: DefaultParams(),
	}
}

// Validate performs basic genesis state validation.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}

	sigilIDs := make(map[string]bool, len(gs.Sigils))
	controllers := make(map[string]bool, len(gs.Sigils))
	for _, s := range gs.Sigils {
		if _, err := SigilIDBytes(s.Id); err != nil {
			return err
		}
		if sigilIDs[s.Id] {
			return errorsmod.Wrapf(ErrInvalidSigilID, "duplicate sigil id %s", s.Id)
		}
		sigilIDs[s.Id] = true

		if _, err := sdk.AccAddressFromBech32(s.ControllerAddr); err != nil {
			return errorsmod.Wrapf(err, "sigil %s controller", s.Id)
		}
		// one sigil per controller address per chamber (SPEC §6.1 invariant:
		// every key resolves to exactly one sigil)
		ck := string(chamberBytes(s.ChamberId)) + s.ControllerAddr
		if controllers[ck] {
			return errorsmod.Wrapf(ErrSigilExists, "controller %s has multiple sigils", s.ControllerAddr)
		}
		controllers[ck] = true

		if int(s.RecoveryThreshold) > len(s.RecoverySet) {
			return errorsmod.Wrapf(ErrInvalidRecoverySet,
				"sigil %s: threshold %d exceeds recovery set size %d", s.Id, s.RecoveryThreshold, len(s.RecoverySet))
		}
	}

	for _, c := range gs.Credentials {
		if _, err := SigilIDBytes(c.Id); err != nil {
			return err
		}
		if !sigilIDs[c.SigilId] {
			return errorsmod.Wrapf(ErrSigilNotFound, "credential %s subject %s", c.Id, c.SigilId)
		}
		if !sigilIDs[c.IssuerSigil] {
			return errorsmod.Wrapf(ErrSigilNotFound, "credential %s issuer %s", c.Id, c.IssuerSigil)
		}
	}

	for _, r := range gs.Reputations {
		if !sigilIDs[r.SigilId] {
			return errorsmod.Wrapf(ErrSigilNotFound, "reputation for %s", r.SigilId)
		}
		if r.Score.IsNil() || r.Score.IsNegative() {
			return errorsmod.Wrapf(ErrInvalidParams, "reputation for %s is negative", r.SigilId)
		}
	}

	return nil
}
