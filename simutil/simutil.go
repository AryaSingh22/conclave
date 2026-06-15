// Package simutil holds the shared tx-delivery helper for the task 0.11 module
// simulations. It deliberately TOLERATES ante/execution rejection: an
// intentionally-bounced tx (an over-ceiling mandate intent, a deliberately
// invalid solution) returns ok=false with no fatal error, so it never aborts
// the run. This does not weaken the 0.11 bug-finding gate — that gate is the
// per-block crisis invariant check in EndBlock, which runs regardless of
// whether any individual tx succeeded; tolerating rejection only changes which
// ops are no-ops. Construction failures (signing/encoding) DO return a fatal
// error, because a tx that cannot even be built is a simulation bug, not a
// protocol rejection.
package simutil

import (
	"context"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
)

// AccountKeeper is the minimal x/auth surface Deliver needs: account number and
// sequence for signing. Satisfied by the real authkeeper.AccountKeeper.
type AccountKeeper interface {
	GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI
}

// BankKeeper is the minimal x/bank surface the module simulations need to size
// give legs and bonds against an account's actual holdings. Satisfied by the
// real bankkeeper.Keeper.
type BankKeeper interface {
	SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
}

// FindAccount returns the simulation account whose address matches addr, if the
// sim controls its key. Many Conclave operations must sign as the controller of
// an existing on-chain sigil/mandate, which is only possible when that key is a
// sim account.
func FindAccount(accs []simtypes.Account, addr sdk.AccAddress) (simtypes.Account, bool) {
	for _, a := range accs {
		if a.Address.Equals(addr) {
			return a, true
		}
	}
	return simtypes.Account{}, false
}

// Deliver builds, signs, and delivers a single-msg tx from signer at zero fee
// (dev min-gas-prices is 0uclv), returning ok=true iff the tx was accepted and
// fully executed. A construction error is fatal (returned); an ante/exec
// rejection is tolerated (ok=false, nil error).
func Deliver(
	r *rand.Rand,
	app *baseapp.BaseApp,
	txGen client.TxConfig,
	ctx sdk.Context,
	ak AccountKeeper,
	msg sdk.Msg,
	signer simtypes.Account,
) (bool, error) {
	acc := ak.GetAccount(ctx, signer.Address)
	if acc == nil {
		return false, nil
	}
	tx, err := simtestutil.GenSignedMockTx(
		r,
		txGen,
		[]sdk.Msg{msg},
		sdk.NewCoins(), // zero fee
		simtestutil.DefaultGenTxGas,
		ctx.ChainID(),
		[]uint64{acc.GetAccountNumber()},
		[]uint64{acc.GetSequence()},
		signer.PrivKey,
	)
	if err != nil {
		return false, err
	}
	if _, _, err := app.SimDeliver(txGen.TxEncoder(), tx); err != nil {
		// tolerated protocol rejection — the tx bounced (e.g. ErrMandateExceeded)
		return false, nil
	}
	return true, nil
}
