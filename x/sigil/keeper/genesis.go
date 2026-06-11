package keeper

import (
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/sigil/types"
)

// InitGenesis initializes the x/sigil store from genesis state.
func (k Keeper) InitGenesis(ctx sdk.Context, gs types.GenesisState) {
	if err := k.SetParams(ctx, gs.Params); err != nil {
		panic(err)
	}
	for _, sigil := range gs.Sigils {
		if err := k.SetSigil(ctx, sigil); err != nil {
			panic(err)
		}
		controller := sdk.MustAccAddressFromBech32(sigil.ControllerAddr)
		if err := k.SetAddressIndex(ctx, sigil.ChamberId, controller, sigil.Id); err != nil {
			panic(err)
		}
	}
	for _, cred := range gs.Credentials {
		if err := k.SetCredential(ctx, cred); err != nil {
			panic(err)
		}
	}
	// reputations explicitly listed in genesis win; otherwise zero records
	// are created for every sigil
	seeded := make(map[string]bool, len(gs.Reputations))
	for _, rep := range gs.Reputations {
		if err := k.SetReputation(ctx, rep); err != nil {
			panic(err)
		}
		seeded[rep.SigilId] = true
	}
	for _, sigil := range gs.Sigils {
		if seeded[sigil.Id] {
			continue
		}
		if err := k.SetReputation(ctx, types.Reputation{
			SigilId:         sigil.Id,
			Score:           math.LegacyZeroDec(),
			LastDecayHeight: sigil.CreatedHeight,
			ChamberId:       sigil.ChamberId,
		}); err != nil {
			panic(err)
		}
	}
}

// ExportGenesis exports the x/sigil store to genesis state.
func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	params, err := k.GetParams(ctx)
	if err != nil {
		panic(err)
	}
	gs := &types.GenesisState{Params: params}

	if err := k.IterateSigils(ctx, func(s types.Sigil) bool {
		gs.Sigils = append(gs.Sigils, s)
		return false
	}); err != nil {
		panic(err)
	}
	if err := k.IterateAllCredentials(ctx, func(c types.CredentialAnchor) bool {
		gs.Credentials = append(gs.Credentials, c)
		return false
	}); err != nil {
		panic(err)
	}
	if err := k.IterateReputations(ctx, func(r types.Reputation) bool {
		gs.Reputations = append(gs.Reputations, r)
		return false
	}); err != nil {
		panic(err)
	}
	return gs
}
