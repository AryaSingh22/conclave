// Package simulation implements the SDK module simulation hooks for x/session
// (task 0.11): randomized genesis plus the solver lifecycle and the
// MsgProposeSolution operation. ProposeSolution REUSES the reference solver
// (solver/cycles + solver/naive, gated by solver/gate — D-054/D-055) to compute
// valid solutions over a live AUCTION session, so the sim drives the real
// scoring → Conclude path (not just the in-consensus fallback) — which is what
// makes "sessions actually CLOSED with solver solutions" real coverage.
package simulation

import (
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/AryaSingh22/conclave/x/session/types"
)

// RandomizedGenState writes default x/session genesis (default params, no
// pre-registered solvers — solvers register via the operation). Params stay at
// dev defaults (session_length=3, solution_window=2, etc.).
func RandomizedGenState(simState *module.SimulationState) {
	gs := types.DefaultGenesis()
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(gs)
}
