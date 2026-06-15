package simulation

import (
	"crypto/sha256"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"github.com/AryaSingh22/conclave/simcoverage"
	"github.com/AryaSingh22/conclave/simutil"
	"github.com/AryaSingh22/conclave/x/sigil/keeper"
	"github.com/AryaSingh22/conclave/x/sigil/types"
)

// operation weights (task 0.11). RegisterSigil is weighted high so the identity
// set — the substrate every other module's operations act on — grows quickly.
const (
	weightRegisterSigil    = 60
	weightRotateController = 10
	weightAnchorCredential = 25
	weightRevokeCredential = 10
)

// credentialSchemas are the schema ids the simulation anchors; they double as
// the universe the §6.4 counterparty-class predicates (has_credential("…"))
// reference, so the predicate path is exercised against real credential state.
var credentialSchemas = []string{"kyc", "accredited", "compute-provider", "data-vendor"}

// WeightedOperations returns the x/sigil simulation operations.
func WeightedOperations(txGen client.TxConfig, ak simutil.AccountKeeper, k keeper.Keeper) simulation.WeightedOperations {
	return simulation.WeightedOperations{
		simulation.NewWeightedOperation(weightRegisterSigil, SimulateMsgRegisterSigil(txGen, ak, k)),
		simulation.NewWeightedOperation(weightRotateController, SimulateMsgRotateController(txGen, ak, k)),
		simulation.NewWeightedOperation(weightAnchorCredential, SimulateMsgAnchorCredential(txGen, ak, k)),
		simulation.NewWeightedOperation(weightRevokeCredential, SimulateMsgRevokeCredential(txGen, ak, k)),
	}
}

// controlledSigil pairs an existing sigil with the sim account that controls it
// (only such sigils can originate signed msgs).
type controlledSigil struct {
	sigil types.Sigil
	acc   simtypes.Account
}

func controlledSigils(ctx sdk.Context, k keeper.Keeper, accs []simtypes.Account) []controlledSigil {
	var out []controlledSigil
	_ = k.IterateSigils(ctx, func(s types.Sigil) bool {
		addr, err := sdk.AccAddressFromBech32(s.ControllerAddr)
		if err != nil {
			return false
		}
		if acc, ok := simutil.FindAccount(accs, addr); ok {
			out = append(out, controlledSigil{sigil: s, acc: acc})
		}
		return false
	})
	return out
}

// SimulateMsgRegisterSigil registers a sigil for a random account that does not
// yet control one (D-010 single-valued reverse index).
func SimulateMsgRegisterSigil(txGen client.TxConfig, ak simutil.AccountKeeper, k keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		acc, _ := simtypes.RandomAcc(r, accs)
		if _, err := k.SigilIDByAddress(ctx, 0, acc.Address); err == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterSigil{}), "already controls a sigil"), nil, nil
		}
		didHash := sha256.Sum256(append([]byte("did"), acc.Address.Bytes()...))
		msg := &types.MsgRegisterSigil{
			Controller:        acc.Address.String(),
			DidDocHash:        didHash[:],
			MetadataUri:       "sim://register",
			RecoverySet:       nil,
			RecoveryThreshold: 0,
			ChamberId:         0,
		}
		ok, err := simutil.Deliver(r, app, txGen, ctx, ak, msg, acc)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "deliver"), nil, err
		}
		if ok {
			simcoverage.Inc("sigil/register_sigil")
		}
		return simtypes.NewOperationMsg(msg, ok, ""), nil, nil
	}
}

// SimulateMsgRotateController rotates a controlled sigil to a fresh account.
func SimulateMsgRotateController(txGen client.TxConfig, ak simutil.AccountKeeper, k keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		owned := controlledSigils(ctx, k, accs)
		if len(owned) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRotateController{}), "no controlled sigils"), nil, nil
		}
		cs := owned[r.Intn(len(owned))]
		newCtrl, _ := simtypes.RandomAcc(r, accs)
		// the new controller must not already control a sigil (D-010)
		if _, err := k.SigilIDByAddress(ctx, 0, newCtrl.Address); err == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRotateController{}), "new controller busy"), nil, nil
		}
		msg := &types.MsgRotateController{
			Signers:       []string{cs.acc.Address.String()},
			SigilId:       cs.sigil.Id,
			NewController: newCtrl.Address.String(),
		}
		ok, err := simutil.Deliver(r, app, txGen, ctx, ak, msg, cs.acc)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "deliver"), nil, err
		}
		if ok {
			simcoverage.Inc("sigil/rotate_controller")
		}
		return simtypes.NewOperationMsg(msg, ok, ""), nil, nil
	}
}

// SimulateMsgAnchorCredential has one controlled sigil issue a credential to
// another sigil under a random schema.
func SimulateMsgAnchorCredential(txGen client.TxConfig, ak simutil.AccountKeeper, k keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		owned := controlledSigils(ctx, k, accs)
		all := allSigils(ctx, k)
		if len(owned) == 0 || len(all) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAnchorCredential{}), "insufficient sigils"), nil, nil
		}
		issuer := owned[r.Intn(len(owned))]
		subject := all[r.Intn(len(all))]
		schema := credentialSchemas[r.Intn(len(credentialSchemas))]
		claim := sha256.Sum256([]byte(schema + subject.Id))
		msg := &types.MsgAnchorCredential{
			Issuer:       issuer.acc.Address.String(),
			IssuerSigil:  issuer.sigil.Id,
			SigilId:      subject.Id,
			SchemaId:     schema,
			ClaimHash:    claim[:],
			ExpiryHeight: uint64(ctx.BlockHeight()) + 1 + uint64(r.Intn(400)),
		}
		ok, err := simutil.Deliver(r, app, txGen, ctx, ak, msg, issuer.acc)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "deliver"), nil, err
		}
		if ok {
			simcoverage.Inc("sigil/anchor_credential")
		}
		return simtypes.NewOperationMsg(msg, ok, ""), nil, nil
	}
}

// SimulateMsgRevokeCredential revokes an existing, non-revoked credential by its
// issuer.
func SimulateMsgRevokeCredential(txGen client.TxConfig, ak simutil.AccountKeeper, k keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// find a credential whose issuer sigil is controlled by a sim account
		issuerBySigil := make(map[string]simtypes.Account)
		for _, cs := range controlledSigils(ctx, k, accs) {
			issuerBySigil[cs.sigil.Id] = cs.acc
		}
		var (
			target    types.CredentialAnchor
			issuerAcc simtypes.Account
			found     bool
		)
		_ = k.IterateAllCredentials(ctx, func(c types.CredentialAnchor) bool {
			if c.Revoked {
				return false
			}
			if acc, ok := issuerBySigil[c.IssuerSigil]; ok {
				target, issuerAcc, found = c, acc, true
				if r.Intn(3) == 0 { // randomize which one
					return true
				}
			}
			return false
		})
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRevokeCredential{}), "no revocable credential"), nil, nil
		}
		msg := &types.MsgRevokeCredential{
			Issuer:       issuerAcc.Address.String(),
			SigilId:      target.SigilId,
			CredentialId: target.Id,
		}
		ok, err := simutil.Deliver(r, app, txGen, ctx, ak, msg, issuerAcc)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "deliver"), nil, err
		}
		if ok {
			simcoverage.Inc("sigil/revoke_credential")
		}
		return simtypes.NewOperationMsg(msg, ok, ""), nil, nil
	}
}

func allSigils(ctx sdk.Context, k keeper.Keeper) []types.Sigil {
	var out []types.Sigil
	_ = k.IterateSigils(ctx, func(s types.Sigil) bool {
		out = append(out, s)
		return false
	})
	return out
}
