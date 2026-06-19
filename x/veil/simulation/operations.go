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
	mandatekeeper "github.com/AryaSingh22/conclave/x/mandate/keeper"
	mandatetypes "github.com/AryaSingh22/conclave/x/mandate/types"
	sigilkeeper "github.com/AryaSingh22/conclave/x/sigil/keeper"
	sigiltypes "github.com/AryaSingh22/conclave/x/sigil/types"
	"github.com/AryaSingh22/conclave/x/veil/keeper"
	"github.com/AryaSingh22/conclave/x/veil/types"
)

const (
	weightMatchablePair  = 30
	weightMatchableCycle = 18
	weightMandatedIntent = 25
	weightMalformed      = 6
	weightCancel         = 10

	denomCLV  = "uclv"
	denomUSDC = "uusdc"
)

// WeightedOperations returns the x/veil simulation operations.
func WeightedOperations(
	txGen client.TxConfig,
	ak simutil.AccountKeeper,
	bk simutil.BankKeeper,
	k keeper.Keeper,
	sk sigilkeeper.Keeper,
	mk mandatekeeper.Keeper,
) simulation.WeightedOperations {
	return simulation.WeightedOperations{
		simulation.NewWeightedOperation(weightMatchablePair, SimulateMatchablePair(txGen, ak, bk, k, sk)),
		simulation.NewWeightedOperation(weightMatchableCycle, SimulateMatchableCycle(txGen, ak, bk, k, sk)),
		simulation.NewWeightedOperation(weightMandatedIntent, SimulateMandatedIntent(txGen, ak, bk, k, sk, mk)),
		simulation.NewWeightedOperation(weightMalformed, SimulateMalformedIntent(txGen, ak, k, sk)),
		simulation.NewWeightedOperation(weightCancel, SimulateCancelIntent(txGen, ak, k)),
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

// coinLeg / dataLeg / svcLeg build single-asset intent legs.
func coinLeg(c sdk.Coin) types.IntentLeg {
	return types.IntentLeg{Asset: &types.IntentLeg_Coin{Coin: &c}}
}
func dataLeg(schema string) types.IntentLeg {
	return types.IntentLeg{Asset: &types.IntentLeg_Data{Data: &types.DataGrant{
		ContentHash: []byte(schema + "-content"), AccessUriCommitment: []byte(schema + "-uri"), SchemaId: schema,
	}}}
}
func svcLeg(schema string, units int64) types.IntentLeg {
	return types.IntentLeg{Asset: &types.IntentLeg_Svc{Svc: &types.ServiceVoucher{Schema: schema, Units: math.NewInt(units)}}}
}

// buildIntent marshals a give/want payload and a ttl envelope. The envelope
// declares spend_commitment = coinGive (the give value the sim sizes the bond
// and ceilings on, D-065): the ante binds the ceilings to commitment + bond
// without reading give legs, and the Reveal opens the intent only because the
// actual give equals the declared commitment — a value mismatch would slash.
func buildIntent(give, want []types.IntentLeg, ttl uint64, coinGive *sdk.Coin) (ciphertext, envelope []byte, err error) {
	payload := types.IntentPayload{Give: give, Want: want}
	ciphertext, err = payload.Marshal()
	if err != nil {
		return nil, nil, err
	}
	env := types.IntentEnvelope{TtlHeight: ttl}
	if coinGive != nil {
		env.SpendCommitment = sdk.NewCoins(*coinGive)
	}
	envelope, err = env.Marshal()
	if err != nil {
		return nil, nil, err
	}
	return ciphertext, envelope, nil
}

// requiredBond replicates the §6.3 bond formula for a single coin give leg (the
// only give shape the sim values non-zero): bond = RequiredBond(face), face =
// the give coin amount; non-coin gives value zero → min_bond.
func requiredBond(k keeper.Keeper, ctx sdk.Context, coinGive *sdk.Coin) (sdk.Coin, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return sdk.Coin{}, err
	}
	face := math.ZeroInt()
	if coinGive != nil {
		face = coinGive.Amount
	}
	return params.RequiredBond(face), nil
}

func submitMsg(sender, sigilID string, ct, env []byte, bond sdk.Coin) *types.MsgSubmitIntent {
	return &types.MsgSubmitIntent{
		Sender:      sender,
		SenderSigil: sigilID,
		Ciphertext:  ct,
		Envelope:    env,
		Bond:        bond,
	}
}

// SimulateMatchablePair submits a 2-party coin swap (A gives X uclv wants Y
// uusdc; B gives Y uusdc wants X uclv) from two sigil CONTROLLERS in ONE block,
// so both seal into one session and the §6.4 fallback (or a solver) clears them.
func SimulateMatchablePair(txGen client.TxConfig, ak simutil.AccountKeeper, bk simutil.BankKeeper, k keeper.Keeper, sk sigilkeeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		owned := controlledSigils(ctx, sk, accs)
		if len(owned) < 2 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSubmitIntent{}), "need 2 sigils"), nil, nil
		}
		r.Shuffle(len(owned), func(i, j int) { owned[i], owned[j] = owned[j], owned[i] })
		a, b := owned[0], owned[1]
		height := uint64(ctx.BlockHeight())
		ttl := height + 12 + uint64(r.Intn(20))

		x := sdk.NewCoin(denomCLV, math.NewInt(1_000_000+int64(r.Intn(5_000_000))))
		y := sdk.NewCoin(denomUSDC, math.NewInt(1_000_000+int64(r.Intn(50_000_000))))
		// A must hold X uclv; B must hold Y uusdc
		if bk.SpendableCoins(ctx, a.acc.Address).AmountOf(denomCLV).LT(x.Amount) ||
			bk.SpendableCoins(ctx, b.acc.Address).AmountOf(denomUSDC).LT(y.Amount) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSubmitIntent{}), "insufficient holdings"), nil, nil
		}

		delivered := 0
		// leg A: give X uclv, want Y uusdc
		if submitOne(r, app, txGen, ctx, ak, k, a.acc, a.id, []types.IntentLeg{coinLeg(x)}, []types.IntentLeg{coinLeg(y)}, ttl, &x) {
			delivered++
		}
		// leg B: give Y uusdc, want X uclv
		if submitOne(r, app, txGen, ctx, ak, k, b.acc, b.id, []types.IntentLeg{coinLeg(y)}, []types.IntentLeg{coinLeg(x)}, ttl, &y) {
			delivered++
		}
		if delivered > 0 {
			simcoverage.Inc("veil/matchable_pair_legs_submitted")
		}
		return simtypes.NewOperationMsg(&types.MsgSubmitIntent{}, delivered == 2, "matchable-pair"), nil, nil
	}
}

// SimulateMatchableCycle submits the §1.3 3-party data/svc/coin cycle from three
// controllers in one block. Only a solver's cycle finder can clear it (the
// fallback is pairwise-only), so it exercises the MsgProposeSolution → scoring →
// Conclude path end to end, plus svc mint and data escrow-stub creation.
func SimulateMatchableCycle(txGen client.TxConfig, ak simutil.AccountKeeper, bk simutil.BankKeeper, k keeper.Keeper, sk sigilkeeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		owned := controlledSigils(ctx, sk, accs)
		if len(owned) < 3 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSubmitIntent{}), "need 3 sigils"), nil, nil
		}
		r.Shuffle(len(owned), func(i, j int) { owned[i], owned[j] = owned[j], owned[i] })
		i0, i1, i2 := owned[0], owned[1], owned[2]
		height := uint64(ctx.BlockHeight())
		ttl := height + 12 + uint64(r.Intn(20))

		// exact-fill, price-representable cycle (D-055): uusdc=M, data=1, svc=N
		// with M divisible by N so price[svc]=M/N is exact LegacyDec.
		const dataSchema, svcSchema = "dataset-D", "CMPT"
		n := int64(10)
		m := int64(100_000_000)
		usdc := sdk.NewCoin(denomUSDC, math.NewInt(m))
		if bk.SpendableCoins(ctx, i0.acc.Address).AmountOf(denomUSDC).LT(usdc.Amount) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSubmitIntent{}), "i0 lacks uusdc"), nil, nil
		}

		delivered := 0
		// I0: give M uusdc, want 1 data
		if submitOne(r, app, txGen, ctx, ak, k, i0.acc, i0.id, []types.IntentLeg{coinLeg(usdc)}, []types.IntentLeg{dataLeg(dataSchema)}, ttl, &usdc) {
			delivered++
		}
		// I1: give 1 data, want N svc
		if submitOne(r, app, txGen, ctx, ak, k, i1.acc, i1.id, []types.IntentLeg{dataLeg(dataSchema)}, []types.IntentLeg{svcLeg(svcSchema, n)}, ttl, nil) {
			delivered++
		}
		// I2: give N svc, want M uusdc
		if submitOne(r, app, txGen, ctx, ak, k, i2.acc, i2.id, []types.IntentLeg{svcLeg(svcSchema, n)}, []types.IntentLeg{coinLeg(usdc)}, ttl, nil) {
			delivered++
		}
		if delivered > 0 {
			simcoverage.Inc("veil/matchable_cycle_legs_submitted")
		}
		return simtypes.NewOperationMsg(&types.MsgSubmitIntent{}, delivered == 3, "matchable-cycle"), nil, nil
	}
}

// submitOne builds+delivers a single intent; coinGive (if non-nil) is the give
// coin used to size the bond.
func submitOne(r *rand.Rand, app *baseapp.BaseApp, txGen client.TxConfig, ctx sdk.Context, ak simutil.AccountKeeper, k keeper.Keeper,
	signer simtypes.Account, sigilID string, give, want []types.IntentLeg, ttl uint64, coinGive *sdk.Coin) bool {
	ct, env, err := buildIntent(give, want, ttl, coinGive)
	if err != nil {
		return false
	}
	bond, err := requiredBond(k, ctx, coinGive)
	if err != nil {
		return false
	}
	ok, _ := simutil.Deliver(r, app, txGen, ctx, ak, submitMsg(signer.Address.String(), sigilID, ct, env, bond), signer)
	return ok
}

// SimulateMandatedIntent submits an intent from a mandate-bound session key.
// Within-ceiling intents charge the mandate window (populating the D-041
// invariant's state); ~1-in-4 deliberately exceed the per-tx ceiling so the
// MandateDecorator rejects them in ante (mandate enforcement). These intents
// rarely match, so they expire at ttl and exercise the §6.4 step-7 window refund.
func SimulateMandatedIntent(txGen client.TxConfig, ak simutil.AccountKeeper, bk simutil.BankKeeper, k keeper.Keeper, sk sigilkeeper.Keeper, mk mandatekeeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// pick an active, non-revoked, unexpired mandate whose grantee is a sim
		// account (so we can sign as it).
		var picks []struct {
			m   mandatetypes.Mandate
			acc simtypes.Account
		}
		height := uint64(ctx.BlockHeight())
		_ = mk.IterateMandates(ctx, func(m mandatetypes.Mandate) bool {
			if m.Revoked || height >= m.ExpiryHeight {
				return false
			}
			gAddr, err := sdk.AccAddressFromBech32(m.GranteeAddr)
			if err != nil {
				return false
			}
			if acc, ok := simutil.FindAccount(accs, gAddr); ok {
				picks = append(picks, struct {
					m   mandatetypes.Mandate
					acc simtypes.Account
				}{m, acc})
			}
			return false
		})
		if len(picks) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSubmitIntent{}), "no usable mandate"), nil, nil
		}
		p := picks[r.Intn(len(picks))]

		ceil := p.m.SpendCeilingPerTx.AmountOf(denomUSDC)
		if !ceil.IsPositive() {
			ceil = math.NewInt(100_000_000)
		}
		overCeiling := r.Intn(4) == 0
		var giveAmt math.Int
		if overCeiling {
			giveAmt = ceil.MulRaw(2).AddRaw(1) // exceeds per-tx ceiling → ante reject
		} else {
			// leave headroom below the ceiling for the bond (also ceiling-charged, D-025)
			giveAmt = math.NewInt(1).Add(simtypes.RandomAmount(r, ceil.QuoRaw(2)))
		}
		give := sdk.NewCoin(denomUSDC, giveAmt)
		if bk.SpendableCoins(ctx, p.acc.Address).AmountOf(denomUSDC).LT(giveAmt) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSubmitIntent{}), "grantee lacks uusdc"), nil, nil
		}
		ttl := height + 3 + uint64(r.Intn(10)) // short → usually expires unmatched
		// want a coin unlikely to be filled, so it expires (exercises refund)
		want := sdk.NewCoin(denomCLV, math.NewInt(1+int64(r.Intn(1_000))))
		ct, env, err := buildIntent([]types.IntentLeg{coinLeg(give)}, []types.IntentLeg{coinLeg(want)}, ttl, &give)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSubmitIntent{}), "build"), nil, nil
		}
		bondCoin := give // for bond sizing only the denom-blind face matters; pass uusdc amount
		bond, err := requiredBond(k, ctx, &bondCoin)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSubmitIntent{}), "bond"), nil, nil
		}
		msg := submitMsg(p.acc.Address.String(), p.m.SigilId, ct, env, bond)
		ok, err := simutil.Deliver(r, app, txGen, ctx, ak, msg, p.acc)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "deliver"), nil, err
		}
		switch {
		case ok:
			simcoverage.Inc("veil/mandated_intent_submitted")
		case overCeiling:
			simcoverage.Inc("veil/mandate_enforced_over_ceiling")
		}
		return simtypes.NewOperationMsg(msg, ok, "mandated"), nil, nil
	}
}

// SimulateMalformedIntent submits an intent whose ciphertext is garbage; it
// seals, then goes INVALID at the Reveal (50% bond slash to the community pool —
// SPEC §6.3, D-040). The §11.6 recover path is exercised under random data.
func SimulateMalformedIntent(txGen client.TxConfig, ak simutil.AccountKeeper, k keeper.Keeper, sk sigilkeeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		owned := controlledSigils(ctx, sk, accs)
		if len(owned) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSubmitIntent{}), "no sigils"), nil, nil
		}
		s := owned[r.Intn(len(owned))]
		height := uint64(ctx.BlockHeight())
		garbage := make([]byte, 1+r.Intn(24))
		for i := range garbage {
			garbage[i] = byte(r.Intn(256))
		}
		env := types.IntentEnvelope{TtlHeight: height + 6 + uint64(r.Intn(10))}
		envBz, err := env.Marshal()
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSubmitIntent{}), "env"), nil, nil
		}
		// malformed payload values zero give (D-021) → bond = min_bond
		bond, err := requiredBond(k, ctx, nil)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSubmitIntent{}), "bond"), nil, nil
		}
		msg := submitMsg(s.acc.Address.String(), s.id, garbage, envBz, bond)
		ok, err := simutil.Deliver(r, app, txGen, ctx, ak, msg, s.acc)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "deliver"), nil, err
		}
		if ok {
			simcoverage.Inc("veil/malformed_intent_submitted")
		}
		return simtypes.NewOperationMsg(msg, ok, "malformed"), nil, nil
	}
}

// SimulateCancelIntent cancels a random pre-OPEN (SUBMITTED) intent owned by a
// sim account.
func SimulateCancelIntent(txGen client.TxConfig, ak simutil.AccountKeeper, k keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		var (
			id    string
			owner simtypes.Account
			found bool
		)
		_ = k.IterateIntentRecords(ctx, func(rec types.IntentRecord) bool {
			if rec.State != types.INTENT_STATE_SUBMITTED {
				return false
			}
			oAddr, err := sdk.AccAddressFromBech32(rec.Owner)
			if err != nil {
				return false
			}
			if acc, ok := simutil.FindAccount(accs, oAddr); ok {
				id, owner, found = rec.Intent.Id, acc, true
				if r.Intn(2) == 0 {
					return true
				}
			}
			return false
		})
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCancelIntent{}), "no cancellable intent"), nil, nil
		}
		msg := &types.MsgCancelIntent{Sender: owner.Address.String(), IntentId: id}
		ok, err := simutil.Deliver(r, app, txGen, ctx, ak, msg, owner)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "deliver"), nil, err
		}
		if ok {
			simcoverage.Inc("veil/cancel_intent")
		}
		return simtypes.NewOperationMsg(msg, ok, ""), nil, nil
	}
}
