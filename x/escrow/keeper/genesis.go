package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/escrow/types"
)

// InitGenesis writes the x/escrow store from genesis. Records are written and
// their participant + sweep indexes rebuilt (the sweep index for any
// non-terminal escrow on a swept condition class). NOTE: the OPTIMISTIC
// committed content hash side-record (0x04) is NOT carried in the genesis proto
// (D-073), so a mid-flight OPTIMISTIC escrow does not round-trip its
// dispute-reveal commitment — operators should drain disputes before an
// export/import (the drain-before-rotate discipline). DefaultGenesis is empty;
// escrows are created at runtime by CONCLUDE.
func (k Keeper) InitGenesis(ctx sdk.Context, gs types.GenesisState) {
	store := k.storeService.OpenKVStore(ctx)
	for i := range gs.Escrows {
		esc := gs.Escrows[i]
		idBz, err := types.IDBytes(esc.Id)
		if err != nil {
			panic(err)
		}
		if err := k.setEscrow(ctx, esc); err != nil {
			panic(err)
		}
		if err := store.Set(types.SigilIdxKey(esc.PayerSigil, idBz), []byte{}); err != nil {
			panic(err)
		}
		if err := store.Set(types.SigilIdxKey(esc.PayeeSigil, idBz), []byte{}); err != nil {
			panic(err)
		}
		nonTerminal := esc.State == types.ESCROW_STATE_ACTIVE || esc.State == types.ESCROW_STATE_DISPUTED
		if _, swept, _ := conditionWindow(esc); swept && nonTerminal && esc.TimeoutHeight != 0 {
			if err := store.Set(types.SweepIdxKey(esc.TimeoutHeight, idBz), []byte{}); err != nil {
				panic(err)
			}
		}
	}
}

// ExportGenesis dumps every escrow record in deterministic key order. The
// OPTIMISTIC commit side-record is not exported (see InitGenesis).
func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	gs := &types.GenesisState{}
	if err := k.IterateEscrows(ctx, func(esc types.Escrow) bool {
		gs.Escrows = append(gs.Escrows, esc)
		return false
	}); err != nil {
		panic(err)
	}
	return gs
}
