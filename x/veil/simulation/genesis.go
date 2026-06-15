// Package simulation implements the SDK module simulation hooks for x/veil
// (task 0.11): randomized genesis plus the operation set that is the coverage
// ENGINE of the whole 0.11 run — it submits coordinated matchable intent sets
// (2-party coin swaps that the in-consensus fallback clears, and 3-party
// data/svc cycles that only a solver's cycle finder can clear), window-charging
// intents from mandate-bound session keys, and malformed-payload intents that
// go INVALID at the Reveal. Without real intent flow the sim would be vacuous;
// this package makes the §6.4 clearing path actually fire.
package simulation

import (
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/AryaSingh22/conclave/x/veil/types"
)

// RandomizedGenState writes default x/veil genesis (default params, no intents —
// intents are created by the operations). Params stay at dev defaults.
func RandomizedGenState(simState *module.SimulationState) {
	gs := types.DefaultGenesis()
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(gs)
}
