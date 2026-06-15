package simulation

import (
	"math/rand"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"github.com/AryaSingh22/conclave/simcoverage"
	"github.com/AryaSingh22/conclave/simutil"
	"github.com/AryaSingh22/conclave/x/mandate/keeper"
	"github.com/AryaSingh22/conclave/x/mandate/types"
	sigilkeeper "github.com/AryaSingh22/conclave/x/sigil/keeper"
	sigiltypes "github.com/AryaSingh22/conclave/x/sigil/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

const (
	weightIssueMandate  = 35
	weightAmendMandate  = 20
	weightRevokeMandate = 8

	denomCLV  = "uclv"
	denomUSDC = "uusdc"
)

// submitTypeURL / cancelTypeURL are the only msg types a simulated mandate
// permits — never a mandate/authz/feegrant msg (D-016 privilege-escalation
// guard), so issuance always passes validation on that axis.
var (
	submitTypeURL = sdk.MsgTypeURL(&veiltypes.MsgSubmitIntent{})
	cancelTypeURL = sdk.MsgTypeURL(&veiltypes.MsgCancelIntent{})
)

// WeightedOperations returns the x/mandate simulation operations.
func WeightedOperations(txGen client.TxConfig, ak simutil.AccountKeeper, k keeper.Keeper, sk sigilkeeper.Keeper) simulation.WeightedOperations {
	return simulation.WeightedOperations{
		simulation.NewWeightedOperation(weightIssueMandate, SimulateMsgIssueMandate(txGen, ak, k, sk)),
		simulation.NewWeightedOperation(weightAmendMandate, SimulateMsgAmendMandate(txGen, ak, k, sk)),
		simulation.NewWeightedOperation(weightRevokeMandate, SimulateMsgRevokeMandate(txGen, ak, k, sk)),
	}
}

type controlledSigil struct {
	id  string
	acc simtypes.Account
}

func controlledSigils(ctx sdk.Context, sk sigilkeeper.Keeper, accs []simtypes.Account) []controlledSigil {
	var out []controlledSigil
	_ = sk.IterateSigils(ctx, func(s sigiltypes.Sigil) bool {
		addr, err := sdk.AccAddressFromBech32(s.ControllerAddr)
		if err != nil {
			return false
		}
		if acc, ok := simutil.FindAccount(accs, addr); ok {
			out = append(out, controlledSigil{id: s.Id, acc: acc})
		}
		return false
	})
	return out
}

// randomTerms builds a valid MandateTerms for the given sigil/grantee. Ceilings
// are sized comfortably above ordinary intent give-legs+bonds so most intents
// land, while the veil over-ceiling operation deliberately exceeds them to
// exercise ante enforcement. window_blocks is drawn from {large, small} so the
// run exercises BOTH the no-reset path and the window-rollover path.
func randomTerms(r *rand.Rand, sigilID, grantee string, height uint64) types.MandateTerms {
	perTx := sdk.NewCoins(
		sdk.NewCoin(denomCLV, math.NewInt(2_000_000+int64(r.Intn(8_000_000)))),
		sdk.NewCoin(denomUSDC, math.NewInt(200_000_000+int64(r.Intn(800_000_000)))),
	)
	mul := int64(2 + r.Intn(5))
	window := sdk.NewCoins(
		sdk.NewCoin(denomCLV, perTx.AmountOf(denomCLV).MulRaw(mul)),
		sdk.NewCoin(denomUSDC, perTx.AmountOf(denomUSDC).MulRaw(mul)),
	)
	windowBlocks := uint64(100_000) // effectively never resets in a 500-block run
	if r.Intn(2) == 0 {
		windowBlocks = uint64(20 + r.Intn(60)) // resets several times
	}
	return types.MandateTerms{
		SigilId:            sigilID,
		GranteeAddr:        grantee,
		AllowedMsgTypes:    []string{submitTypeURL, cancelTypeURL},
		SpendCeilingPerTx:  perTx,
		SpendCeilingWindow: window,
		WindowBlocks:       windowBlocks,
		CounterpartyClass:  "",
		FeeSponsor:         "",
		FeeDenoms:          []string{denomUSDC},
		ExpiryHeight:       height + 200 + uint64(r.Intn(800)),
		ChamberId:          0,
	}
}

// SimulateMsgIssueMandate issues a mandate from a controlled sigil to a fresh
// session key. A grantee that already holds a mandate is permitted (D-017a
// REPLACE) — which, when that grantee has live intents, also resets the window
// and is one of the predicted D-041 stressors.
func SimulateMsgIssueMandate(txGen client.TxConfig, ak simutil.AccountKeeper, k keeper.Keeper, sk sigilkeeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		owned := controlledSigils(ctx, sk, accs)
		if len(owned) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgIssueMandate{}), "no controlled sigils"), nil, nil
		}
		issuer := owned[r.Intn(len(owned))]
		grantee, _ := simtypes.RandomAcc(r, accs)
		// a grantee may not itself control a sigil (D-017c ErrGranteeConflict)
		if _, err := sk.SigilIDByAddress(ctx, 0, grantee.Address); err == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgIssueMandate{}), "grantee controls a sigil"), nil, nil
		}
		msg := &types.MsgIssueMandate{
			Controller: issuer.acc.Address.String(),
			Terms:      randomTerms(r, issuer.id, grantee.Address.String(), uint64(ctx.BlockHeight())),
		}
		ok, err := simutil.Deliver(r, app, txGen, ctx, ak, msg, issuer.acc)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "deliver"), nil, err
		}
		if ok {
			simcoverage.Inc("mandate/issue")
		}
		return simtypes.NewOperationMsg(msg, ok, ""), nil, nil
	}
}

// activeMandate pairs a mandate with the sim account controlling its sigil.
type activeMandate struct {
	m   types.Mandate
	ctl simtypes.Account
}

func activeMandates(ctx sdk.Context, k keeper.Keeper, sk sigilkeeper.Keeper, accs []simtypes.Account) []activeMandate {
	var out []activeMandate
	_ = k.IterateMandates(ctx, func(m types.Mandate) bool {
		ctlAddr, err := sk.ControllerAddress(ctx, m.ChamberId, m.SigilId)
		if err != nil {
			return false
		}
		if acc, ok := simutil.FindAccount(accs, ctlAddr); ok {
			out = append(out, activeMandate{m: m, ctl: acc})
		}
		return false
	})
	return out
}

// SimulateMsgAmendMandate amends an existing mandate (full term replacement,
// window reset — D-018). When the mandate's grantee has live intents charged
// against the pre-amend window, this is the predicted D-041 stressor (the
// interaction flagged for task 0.11) — the operation does NOT avoid that case.
func SimulateMsgAmendMandate(txGen client.TxConfig, ak simutil.AccountKeeper, k keeper.Keeper, sk sigilkeeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		ms := activeMandates(ctx, k, sk, accs)
		// only non-revoked mandates can be amended
		var live []activeMandate
		for _, am := range ms {
			if !am.m.Revoked {
				live = append(live, am)
			}
		}
		if len(live) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAmendMandate{}), "no amendable mandates"), nil, nil
		}
		am := live[r.Intn(len(live))]
		// grantee and sigil are immutable under amendment (D-018)
		terms := randomTerms(r, am.m.SigilId, am.m.GranteeAddr, uint64(ctx.BlockHeight()))
		msg := &types.MsgAmendMandate{
			Controller: am.ctl.Address.String(),
			MandateId:  am.m.Id,
			Terms:      terms,
		}
		ok, err := simutil.Deliver(r, app, txGen, ctx, ak, msg, am.ctl)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "deliver"), nil, err
		}
		if ok {
			simcoverage.Inc("mandate/amend")
		}
		return simtypes.NewOperationMsg(msg, ok, ""), nil, nil
	}
}

// SimulateMsgRevokeMandate revokes a non-revoked mandate (takes effect next
// block; the index entry is retained, D-017b).
func SimulateMsgRevokeMandate(txGen client.TxConfig, ak simutil.AccountKeeper, k keeper.Keeper, sk sigilkeeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		ms := activeMandates(ctx, k, sk, accs)
		var live []activeMandate
		for _, am := range ms {
			if !am.m.Revoked {
				live = append(live, am)
			}
		}
		if len(live) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRevokeMandate{}), "no revocable mandates"), nil, nil
		}
		am := live[r.Intn(len(live))]
		msg := &types.MsgRevokeMandate{
			Controller: am.ctl.Address.String(),
			MandateId:  am.m.Id,
		}
		ok, err := simutil.Deliver(r, app, txGen, ctx, ak, msg, am.ctl)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "deliver"), nil, err
		}
		if ok {
			simcoverage.Inc("mandate/revoke")
		}
		return simtypes.NewOperationMsg(msg, ok, ""), nil, nil
	}
}
