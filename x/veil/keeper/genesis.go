package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/veil/types"
)

// InitGenesis initializes the x/veil store from genesis state. Records are
// written through setIntentRecord so the (state, ttl_height) index is
// rebuilt rather than imported (SPEC §5.2 index).
func (k Keeper) InitGenesis(ctx sdk.Context, gs types.GenesisState) {
	if err := k.SetParams(ctx, gs.Params); err != nil {
		panic(err)
	}
	for _, rec := range gs.Intents {
		if err := k.setIntentRecord(ctx, rec, types.INTENT_STATE_UNSPECIFIED); err != nil {
			panic(err)
		}
	}
}

// ExportGenesis exports the x/veil store to genesis state.
func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	params, err := k.GetParams(ctx)
	if err != nil {
		panic(err)
	}
	gs := &types.GenesisState{Params: params}
	if err := k.IterateIntentRecords(ctx, func(rec types.IntentRecord) bool {
		gs.Intents = append(gs.Intents, rec)
		return false
	}); err != nil {
		panic(err)
	}
	return gs
}
