package keeper

import (
	"encoding/binary"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/session/types"
)

// InitGenesis initializes the x/session store from genesis state. Solver and
// session records are written through the keeper setters so the unbonding and
// state indexes are rebuilt rather than imported.
func (k Keeper) InitGenesis(ctx sdk.Context, gs types.GenesisState) {
	if err := k.SetParams(ctx, gs.Params); err != nil {
		panic(err)
	}
	for i := range gs.Solvers {
		s := gs.Solvers[i]
		if err := k.setSolver(ctx, s); err != nil {
			panic(err)
		}
		if s.Status == types.SOLVER_STATUS_UNBONDING {
			addr, err := sdk.AccAddressFromBech32(s.Addr)
			if err != nil {
				panic(err)
			}
			if err := k.storeService.OpenKVStore(ctx).Set(types.SolverUnbondIdxKey(s.UnbondHeight, addr.Bytes()), []byte{}); err != nil {
				panic(err)
			}
		}
	}
	for i := range gs.Sessions {
		if err := k.setSession(ctx, gs.Sessions[i], types.SESSION_STATE_UNSPECIFIED); err != nil {
			panic(err)
		}
	}
	if gs.NextSessionId != 0 {
		bz := make([]byte, 8)
		binary.BigEndian.PutUint64(bz, gs.NextSessionId)
		if err := k.storeService.OpenKVStore(ctx).Set(types.NextSessionIDKey, bz); err != nil {
			panic(err)
		}
	}
}

// ExportGenesis exports the x/session store to genesis state.
func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	params, err := k.GetParams(ctx)
	if err != nil {
		panic(err)
	}
	gs := &types.GenesisState{Params: params}

	if err := k.IterateSolvers(ctx, func(s types.Solver) bool {
		gs.Solvers = append(gs.Solvers, s)
		return false
	}); err != nil {
		panic(err)
	}
	if err := k.IterateSessions(ctx, func(s types.Session) bool {
		gs.Sessions = append(gs.Sessions, s)
		return false
	}); err != nil {
		panic(err)
	}

	bz, err := k.storeService.OpenKVStore(ctx).Get(types.NextSessionIDKey)
	if err != nil {
		panic(err)
	}
	if bz != nil {
		gs.NextSessionId = binary.BigEndian.Uint64(bz)
	}
	return gs
}
