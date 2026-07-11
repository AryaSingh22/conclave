package app

// ibctesting.go — the ibc-go v8 ibctesting.TestingApp surface (task 2.5).
// Pure getters over already-exported state: no behavior, no wiring. They let
// the ibctesting coordinator drive two in-process Conclave chains through a
// REAL ICS-20 transfer channel (the 2.5 IBC-USDC e2e), replacing nothing in
// production paths.

import (
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"

	capabilitykeeper "github.com/cosmos/ibc-go/modules/capability/keeper"
	ibckeeper "github.com/cosmos/ibc-go/v8/modules/core/keeper"
	ibctestingtypes "github.com/cosmos/ibc-go/v8/testing/types"
)

// GetBaseApp implements ibctesting.TestingApp.
func (app *ConclaveApp) GetBaseApp() *baseapp.BaseApp { return app.App.BaseApp }

// GetStakingKeeper implements ibctesting.TestingApp (client/header creation).
func (app *ConclaveApp) GetStakingKeeper() ibctestingtypes.StakingKeeper {
	return app.StakingKeeper
}

// GetIBCKeeper implements ibctesting.TestingApp.
func (app *ConclaveApp) GetIBCKeeper() *ibckeeper.Keeper { return app.IBCKeeper }

// GetScopedIBCKeeper implements ibctesting.TestingApp.
func (app *ConclaveApp) GetScopedIBCKeeper() capabilitykeeper.ScopedKeeper {
	return app.ScopedIBCKeeper
}

// GetTxConfig implements ibctesting.TestingApp.
func (app *ConclaveApp) GetTxConfig() client.TxConfig { return app.txConfig }
