package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultGenesis returns the default x/veil genesis state.
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
	seen := make(map[string]bool, len(gs.Intents))
	for _, rec := range gs.Intents {
		if _, err := IntentIDBytes(rec.Intent.Id); err != nil {
			return err
		}
		if seen[rec.Intent.Id] {
			return fmt.Errorf("duplicate intent id %s", rec.Intent.Id)
		}
		seen[rec.Intent.Id] = true
		if rec.State == INTENT_STATE_UNSPECIFIED {
			return fmt.Errorf("intent %s has unspecified state", rec.Intent.Id)
		}
		if _, err := sdk.AccAddressFromBech32(rec.Owner); err != nil {
			return fmt.Errorf("intent %s owner: %w", rec.Intent.Id, err)
		}
		if err := rec.Intent.Bond.Validate(); err != nil {
			return fmt.Errorf("intent %s bond: %w", rec.Intent.Id, err)
		}
		if err := rec.ChargedSpend.Validate(); err != nil {
			return fmt.Errorf("intent %s charged_spend: %w", rec.Intent.Id, err)
		}
		// the envelope must be decodable with a sane ttl: the state index is
		// keyed by ttl_height, so InitGenesis cannot index a record whose
		// envelope does not parse
		if _, err := ValidateEnvelope(rec.Intent.Envelope, rec.Intent.SubmitHeight); err != nil {
			return fmt.Errorf("intent %s envelope: %w", rec.Intent.Id, err)
		}
	}
	return nil
}
