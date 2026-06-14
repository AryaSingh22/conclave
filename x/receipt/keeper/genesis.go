package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/receipt/types"
)

// InitGenesis is a no-op: x/receipt owns no store (D-052). The §6.6 receipts
// live in the x/session store (D-045) and are restored by x/session's genesis,
// not here. Validation rejects any non-empty receipt list (see types.Validate).
func (k Keeper) InitGenesis(_ sdk.Context, _ types.GenesisState) {}

// ExportGenesis returns the empty x/receipt genesis: the module owns no state
// to export (D-045/D-052).
func (k Keeper) ExportGenesis(_ sdk.Context) *types.GenesisState {
	return types.DefaultGenesis()
}
