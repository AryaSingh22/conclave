package types

import "fmt"

// DefaultGenesis returns the default x/escrow genesis state: no escrows.
func DefaultGenesis() *GenesisState {
	return &GenesisState{}
}

// Validate performs basic genesis state validation: escrow ids are well-formed
// 32-byte hex, unique, in a known non-UNSPECIFIED state, and coin amounts are
// valid.
func (gs GenesisState) Validate() error {
	seen := make(map[string]bool, len(gs.Escrows))
	for i := range gs.Escrows {
		esc := gs.Escrows[i]
		if _, err := IDBytes(esc.Id); err != nil {
			return fmt.Errorf("escrow %d: %w", i, err)
		}
		if seen[esc.Id] {
			return fmt.Errorf("duplicate escrow %s", esc.Id)
		}
		seen[esc.Id] = true
		if esc.State == ESCROW_STATE_UNSPECIFIED {
			return fmt.Errorf("escrow %s has unspecified state", esc.Id)
		}
		if err := esc.Amount.Validate(); err != nil {
			return fmt.Errorf("escrow %s amount: %w", esc.Id, err)
		}
	}
	return nil
}
