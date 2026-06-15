// Package simulation implements the SDK module simulation hooks for x/mandate
// (task 0.11): randomized genesis and weighted operations that issue, amend, and
// revoke mandates — including, deliberately, amend/re-issue against grantees that
// hold live intents, the interaction that stresses the D-041 window/bond
// conservation invariant (the headline 0.11 cross-module check).
package simulation

import (
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/AryaSingh22/conclave/x/mandate/types"
)

// RandomizedGenState writes default x/mandate genesis (default params, no
// pre-issued mandates — the IssueMandate operation creates them during the run).
// Params stay at dev defaults (task 0.11 fuzzes interactions, not params).
func RandomizedGenState(simState *module.SimulationState) {
	gs := types.DefaultGenesis()
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(gs)
}
