package sigil

import (
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	sigilsim "github.com/AryaSingh22/conclave/x/sigil/simulation"
	"github.com/AryaSingh22/conclave/x/sigil/types"
)

// GenerateGenesisState implements module.AppModuleSimulation (task 0.11):
// randomized x/sigil genesis (pre-registered identities + default params).
func (am AppModule) GenerateGenesisState(simState *module.SimulationState) {
	sigilsim.RandomizedGenState(simState)
}

// RegisterStoreDecoder registers the x/sigil store decoder for the sim diff.
func (am AppModule) RegisterStoreDecoder(sdr simtypes.StoreDecoderRegistry) {
	sdr[types.StoreKey] = sigilsim.NewDecodeStore(am.cdc)
}

// WeightedOperations returns the x/sigil simulation operations.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	return sigilsim.WeightedOperations(simState.TxConfig, am.accountKeeper, am.keeper)
}
