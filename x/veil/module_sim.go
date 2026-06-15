package veil

import (
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	veilsim "github.com/AryaSingh22/conclave/x/veil/simulation"
	"github.com/AryaSingh22/conclave/x/veil/types"
)

// GenerateGenesisState implements module.AppModuleSimulation (task 0.11).
func (am AppModule) GenerateGenesisState(simState *module.SimulationState) {
	veilsim.RandomizedGenState(simState)
}

// RegisterStoreDecoder registers the x/veil store decoder.
func (am AppModule) RegisterStoreDecoder(sdr simtypes.StoreDecoderRegistry) {
	sdr[types.StoreKey] = veilsim.NewDecodeStore(am.cdc)
}

// WeightedOperations returns the x/veil simulation operations — the coverage
// engine that drives real intent flow and §6.4 clearing.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	return veilsim.WeightedOperations(simState.TxConfig, am.accountKeeper, am.bankKeeper, am.keeper, am.sigilKeeper, am.mandateKeeper)
}
