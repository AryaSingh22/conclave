package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/epoch/types"
)

// InitGenesis initializes the x/epoch store from genesis state. Normally only
// params + the id counters are set (no ACTIVE epoch — the 1.6 bootstrap seam,
// D-068); imported epoch records exist for test/pre-provisioned networks.
func (k Keeper) InitGenesis(ctx sdk.Context, gs types.GenesisState) {
	if err := k.SetParams(ctx, gs.Params); err != nil {
		panic(err)
	}
	for i := range gs.Epochs {
		k.SetEpoch(ctx, gs.Epochs[i])
	}
	k.setCurrentEpochID(ctx, gs.CurrentEpochId)
	next := gs.NextEpochId
	if next == 0 {
		next = 1
	}
	k.setNextEpochID(ctx, next)
}

// ExportGenesis exports the x/epoch store to genesis state. Transient DKG
// transport (deal/response/confirm bundles) is NOT exported — it is per-block
// in-flight state, not lasting epoch state.
func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	params, err := k.GetParams(ctx)
	if err != nil {
		panic(err)
	}
	gs := &types.GenesisState{
		Params:         params,
		CurrentEpochId: k.CurrentEpochID(ctx),
		NextEpochId:    k.NextEpochID(ctx),
	}
	k.IterateEpochs(ctx, func(e types.Epoch) bool {
		gs.Epochs = append(gs.Epochs, e)
		return false
	})
	return gs
}
