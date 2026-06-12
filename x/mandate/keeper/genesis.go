package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/mandate/types"
)

// InitGenesis initializes the x/mandate store from genesis state and
// materializes the fee pool module account (SPEC §6.2: "a CLV float funded
// at genesis") so the genesis bank balance lands on a proper module account.
func (k Keeper) InitGenesis(ctx sdk.Context, gs types.GenesisState) {
	if err := k.SetParams(ctx, gs.Params); err != nil {
		panic(err)
	}

	// touching the module account creates it under the auth keeper before
	// any fee abstraction runs
	k.accountKeeper.GetModuleAccount(ctx, types.ModuleName)

	for _, m := range gs.Mandates {
		grantee := sdk.MustAccAddressFromBech32(m.GranteeAddr)
		if err := k.SetMandate(ctx, m); err != nil {
			panic(err)
		}
		if err := k.setIndexes(ctx, m, grantee); err != nil {
			panic(err)
		}
		// delegate keys enter the sigil reverse index exactly as on the
		// MsgIssueMandate path (SPEC §6.1 key 0x04)
		if err := k.sigilKeeper.SetAddressIndex(ctx, m.ChamberId, grantee, m.SigilId); err != nil {
			panic(err)
		}
	}
}

// ExportGenesis exports the x/mandate store to genesis state.
func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	params, err := k.GetParams(ctx)
	if err != nil {
		panic(err)
	}
	gs := &types.GenesisState{Params: params}

	if err := k.IterateMandates(ctx, func(m types.Mandate) bool {
		gs.Mandates = append(gs.Mandates, m)
		return false
	}); err != nil {
		panic(err)
	}
	return gs
}
