package session

import (
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	sessionsim "github.com/AryaSingh22/conclave/x/session/simulation"
	"github.com/AryaSingh22/conclave/x/session/types"
)

// GenerateGenesisState implements module.AppModuleSimulation (task 0.11).
func (am AppModule) GenerateGenesisState(simState *module.SimulationState) {
	sessionsim.RandomizedGenState(simState)
}

// RegisterStoreDecoder registers the x/session store decoder.
func (am AppModule) RegisterStoreDecoder(sdr simtypes.StoreDecoderRegistry) {
	sdr[types.StoreKey] = sessionsim.NewDecodeStore(am.cdc)
}

// WeightedOperations returns the x/session simulation operations (solver
// lifecycle + reference-solver-driven ProposeSolution).
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	return sessionsim.WeightedOperations(simState.TxConfig, am.accountKeeper, am.keeper, am.veilKeeper)
}
