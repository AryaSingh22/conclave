package types

import "fmt"

// DefaultGenesis returns the default x/epoch genesis state. No ACTIVE epoch and
// no key ship at genesis: the live encryption path is StubScheme until Task 1.6,
// so epoch-0 bootstrap (a genesis-provisioned key vs. first-successful DKG) is a
// RECORDED FORWARD SEAM to 1.6 (D-068).
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:         DefaultParams(),
		CurrentEpochId: 0,
		NextEpochId:    1,
	}
}

// Validate performs basic genesis state validation.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	seen := make(map[uint64]bool, len(gs.Epochs))
	var maxID uint64
	for i := range gs.Epochs {
		e := gs.Epochs[i]
		if seen[e.Id] {
			return fmt.Errorf("%w: duplicate epoch id %d", ErrInvalidGenesis, e.Id)
		}
		seen[e.Id] = true
		if e.Status == EPOCH_STATUS_UNSPECIFIED {
			return fmt.Errorf("%w: epoch %d has unspecified status", ErrInvalidGenesis, e.Id)
		}
		if e.Id > maxID {
			maxID = e.Id
		}
	}
	// next_epoch_id must be strictly past every imported epoch so a fresh DKG
	// never reuses an id.
	if gs.NextEpochId <= maxID {
		return fmt.Errorf("%w: next_epoch_id %d must exceed max imported epoch id %d",
			ErrInvalidGenesis, gs.NextEpochId, maxID)
	}
	if gs.CurrentEpochId != 0 && !seen[gs.CurrentEpochId] {
		return fmt.Errorf("%w: current_epoch_id %d has no imported epoch record",
			ErrInvalidGenesis, gs.CurrentEpochId)
	}
	return nil
}
