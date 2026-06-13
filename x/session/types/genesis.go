package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultGenesis returns the default x/session genesis state.
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

	seenSolver := make(map[string]bool, len(gs.Solvers))
	for _, s := range gs.Solvers {
		if _, err := sdk.AccAddressFromBech32(s.Addr); err != nil {
			return fmt.Errorf("solver %s: %w", s.Addr, err)
		}
		if seenSolver[s.Addr] {
			return fmt.Errorf("duplicate solver %s", s.Addr)
		}
		seenSolver[s.Addr] = true
		if err := s.Bond.Validate(); err != nil {
			return fmt.Errorf("solver %s bond: %w", s.Addr, err)
		}
		if s.Status == SOLVER_STATUS_UNSPECIFIED {
			return fmt.Errorf("solver %s has unspecified status", s.Addr)
		}
	}

	seenSession := make(map[uint64]bool, len(gs.Sessions))
	for _, ses := range gs.Sessions {
		if seenSession[ses.Id] {
			return fmt.Errorf("duplicate session %d", ses.Id)
		}
		seenSession[ses.Id] = true
		if ses.State == SESSION_STATE_UNSPECIFIED {
			return fmt.Errorf("session %d has unspecified state", ses.Id)
		}
	}
	return nil
}
