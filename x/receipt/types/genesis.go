package types

import "fmt"

// DefaultGenesis returns the default (empty) x/receipt genesis. x/receipt owns
// no state — the §6.6 receipts live in the x/session store (D-045) — so its
// genesis is always empty.
func DefaultGenesis() *GenesisState { return &GenesisState{} }

// Validate checks the genesis state. Because x/receipt owns no state, a
// non-empty receipt list is a misconfiguration: receipts are written at runtime
// by x/session and are never imported through this module (D-045/D-052).
func (gs GenesisState) Validate() error {
	if len(gs.Receipts) != 0 {
		return fmt.Errorf("x/receipt genesis must be empty: receipts live in the x/session store (D-045), got %d", len(gs.Receipts))
	}
	return nil
}
