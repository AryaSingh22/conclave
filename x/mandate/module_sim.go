package mandate

import (
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	mandatesim "github.com/AryaSingh22/conclave/x/mandate/simulation"
	"github.com/AryaSingh22/conclave/x/mandate/types"
)

// GenerateGenesisState implements module.AppModuleSimulation (task 0.11).
func (am AppModule) GenerateGenesisState(simState *module.SimulationState) {
	mandatesim.RandomizedGenState(simState)
}

// RegisterStoreDecoder registers the x/mandate store decoder.
func (am AppModule) RegisterStoreDecoder(sdr simtypes.StoreDecoderRegistry) {
	sdr[types.StoreKey] = mandatesim.NewDecodeStore(am.cdc)
}

// WeightedOperations returns the x/mandate simulation operations.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	return mandatesim.WeightedOperations(simState.TxConfig, am.accountKeeper, am.keeper, am.sigilKeeper)
}
