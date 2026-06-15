package types

import (
	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultGenesis returns the default x/mandate genesis state.
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

	forbidden := make(map[string]bool, 6)
	for _, url := range ForbiddenMsgTypes() {
		forbidden[url] = true
	}

	ids := make(map[string]bool, len(gs.Mandates))
	grantees := make(map[string]bool, len(gs.Mandates))
	for _, m := range gs.Mandates {
		if _, err := MandateIDBytes(m.Id); err != nil {
			return err
		}
		if ids[m.Id] {
			return errorsmod.Wrapf(ErrInvalidMandateID, "duplicate mandate id %s", m.Id)
		}
		ids[m.Id] = true

		if _, err := sdk.AccAddressFromBech32(m.GranteeAddr); err != nil {
			return errorsmod.Wrapf(ErrInvalidTerms, "mandate %s grantee: %s", m.Id, err)
		}
		// one active mandate per grantee key per chamber (SPEC §6.2, D2)
		gk := string(chamberBytes(m.ChamberId)) + m.GranteeAddr
		if grantees[gk] {
			return errorsmod.Wrapf(ErrGranteeConflict, "grantee %s has multiple mandates", m.GranteeAddr)
		}
		grantees[gk] = true

		if len(m.SigilId) != 2*IDLength {
			return errorsmod.Wrapf(ErrInvalidTerms, "mandate %s sigil_id %q is not a 64-char hex id", m.Id, m.SigilId)
		}
		for _, url := range m.AllowedMsgTypes {
			if forbidden[url] {
				return errorsmod.Wrapf(ErrPrivilegeEscalation, "mandate %s allows %s", m.Id, url)
			}
		}
		if err := m.SpendCeilingPerTx.Validate(); err != nil {
			return errorsmod.Wrapf(ErrInvalidTerms, "mandate %s spend_ceiling_per_tx: %s", m.Id, err)
		}
		if err := m.SpendCeilingWindow.Validate(); err != nil {
			return errorsmod.Wrapf(ErrInvalidTerms, "mandate %s spend_ceiling_window: %s", m.Id, err)
		}
		if err := m.WindowSpent.Validate(); err != nil {
			return errorsmod.Wrapf(ErrInvalidTerms, "mandate %s window_spent: %s", m.Id, err)
		}
		if err := m.OutstandingCommitment.Validate(); err != nil {
			return errorsmod.Wrapf(ErrInvalidTerms, "mandate %s outstanding_commitment: %s", m.Id, err)
		}
		if m.FeeSponsor != "" {
			if _, err := sdk.AccAddressFromBech32(m.FeeSponsor); err != nil {
				return errorsmod.Wrapf(ErrInvalidTerms, "mandate %s fee_sponsor: %s", m.Id, err)
			}
		}
		if m.ExpiryHeight == 0 {
			return errorsmod.Wrapf(ErrInvalidTerms, "mandate %s expiry_height must be positive", m.Id)
		}
	}
	return nil
}
