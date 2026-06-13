package keeper

import (
	"strconv"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/session/types"
)

// uusdcPerWhole scales numeraire base units (uusdc) to whole USDC for the
// SPEC §6.1 reputation value-weight (which takes value in whole USDC).
var uusdcPerWhole = math.LegacyNewDec(1_000_000)

// matchPlan is the pre-settlement economic snapshot of one match, gathered
// while its intents are still OPEN (before applyMatches marks them CONCLUDED):
// the net flows, the bound price lens, and each party's surplus and gross
// settled value. The skim, receipts, reputation, and payout all read from it.
type matchPlan struct {
	match          types.Match
	intents        []matchIntent
	flows          NetFlows
	numeraire      string
	priceOf        map[string]math.LegacyDec
	surplusBySigil map[string]math.LegacyDec
	settledBySigil map[string]math.LegacyDec
	totalSurplus   math.LegacyDec
}

// planMatches snapshots the economics of every match while its intents are OPEN.
// It re-derives net flows with the same CheckConservation the validator used, so
// the economic layer measures exactly the graph that settles.
func (k Keeper) planMatches(ctx sdk.Context, matches []types.Match) ([]matchPlan, error) {
	plans := make([]matchPlan, 0, len(matches))
	for mi := range matches {
		m := matches[mi]
		flows, err := CheckConservation([]types.Match{m})
		if err != nil {
			return nil, err
		}
		numeraire, priceOf, err := matchNumeraire(m)
		if err != nil {
			return nil, err
		}

		plan := matchPlan{
			match:          m,
			flows:          flows,
			numeraire:      numeraire,
			priceOf:        priceOf,
			surplusBySigil: make(map[string]math.LegacyDec),
			settledBySigil: make(map[string]math.LegacyDec),
			totalSurplus:   math.LegacyZeroDec(),
		}
		for _, id := range m.IntentIds {
			rec, payload, err := k.veil().GetOpenIntent(ctx, phase0Chamber, id)
			if err != nil {
				return nil, err
			}
			it := matchIntent{rec: rec, payload: payload, sigil: rec.Intent.SenderSigil}
			surplus, err := intentSurplus(it, flows[it.sigil], priceOf)
			if err != nil {
				return nil, err
			}
			plan.intents = append(plan.intents, it)
			plan.surplusBySigil[it.sigil] = surplus
			plan.settledBySigil[it.sigil] = settledValue(flows[it.sigil], priceOf)
			plan.totalSurplus = plan.totalSurplus.Add(surplus)
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

// settledValue is a party's gross value transacted in the match numeraire — the
// value it GAVE (== the value it received, by the bound prices D-047). Feeds the
// §6.1 reputation value-weight.
func settledValue(flow map[string]math.Int, priceOf map[string]math.LegacyDec) math.LegacyDec {
	v := math.LegacyZeroDec()
	for asset, net := range flow {
		p, ok := priceOf[asset]
		if !ok {
			continue
		}
		v = v.Add(p.MulInt(negPart(net)))
	}
	return v
}

// applyEconomics runs the SPEC §6.4 economic layer INSIDE the Conclude cache,
// after the core graph has settled (parties now hold their receipts): it takes
// the surplus skim (D-050/STOP #1), emits §6.6 receipts, fires the §6.1
// reputation hook, and pays the winning solver from the fee pool. A nil solver
// (direct Conclude / authority MsgConclude) skims to the pool and pays no one.
//
// Build condition #1 (STOP #1): the skim legs are run through the SAME
// CheckConservation as any leg and through checkSkimBinding — not asserted
// equivalent in a comment.
func (k Keeper) applyEconomics(ctx sdk.Context, sessionID uint64, plans []matchPlan, solver *sdk.AccAddress, params types.Params) error {
	totalSurplus := math.LegacyZeroDec()
	payNumeraire := ""

	for pi := range plans {
		plan := plans[pi]

		// derive the skim and validate the skim-inclusive graph LITERALLY.
		skimLegs := k.computeMatchSkim(plan.match, plan.intents, plan.flows, plan.numeraire, params.SurplusSkimBps)
		if len(skimLegs) > 0 {
			combined := types.Match{Legs: append(append([]types.SettleLeg{}, plan.match.Legs...), skimLegs...)}
			combinedFlows, err := CheckConservation([]types.Match{combined})
			if err != nil {
				return err
			}
			if err := checkSkimBinding(combinedFlows, plan.priceOf, plan.surplusBySigil); err != nil {
				return err
			}
			if err := k.applySkimLegs(ctx, sessionID, skimLegs); err != nil {
				return err
			}
		}

		// §6.6 receipts + §6.1 reputation, per matched intent.
		if err := k.writeMatchReceipts(ctx, sessionID, plan.match, plan.intents); err != nil {
			return err
		}
		for i := range plan.intents {
			sigil := plan.intents[i].sigil
			valueUSDC := plan.settledBySigil[sigil].Quo(uusdcPerWhole)
			if err := k.sigilKeeper.AddSettlementReputation(ctx, phase0Chamber, sigil, valueUSDC); err != nil {
				return err
			}
		}

		totalSurplus = totalSurplus.Add(plan.totalSurplus)
		if payNumeraire == "" {
			payNumeraire = plan.numeraire
		}
	}

	if solver != nil {
		return k.paySolver(ctx, *solver, totalSurplus, payNumeraire, params.SolverFeeBps)
	}
	return nil
}

// applySkimLegs moves each skim leg's numeraire from the giving party's
// controller treasury into the fee pool (the session module account). The pool
// RETAINS it — a skim is not routed onward like a settlement leg, so it does NOT
// go through applyLeg's account→module→account path. The party holds the
// numeraire because the core graph already credited it (skim runs after
// applyMatches). Principal-safe by construction (computeMatchSkim bounds the
// amount to receive-excess) and re-proven by checkSkimBinding above.
func (k Keeper) applySkimLegs(ctx sdk.Context, sessionID uint64, skimLegs []types.SettleLeg) error {
	for li := range skimLegs {
		leg := skimLegs[li]
		from, err := k.sigilKeeper.ControllerAddress(ctx, phase0Chamber, leg.FromSigil)
		if err != nil {
			return errorsmod.Wrapf(types.ErrInvalidLeg, "skim from sigil %s: %s", leg.FromSigil, err)
		}
		coin := *leg.GetCoin()
		if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, from, types.ModuleName, sdk.NewCoins(coin)); err != nil {
			return err
		}
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeSkimTaken,
			sdk.NewAttribute(types.AttributeKeySigilID, leg.FromSigil),
			sdk.NewAttribute(types.AttributeKeyAmount, coin.String()),
			sdk.NewAttribute(types.AttributeKeySessionID, strconv.FormatUint(sessionID, 10)),
		))
	}
	return nil
}

// paySolver pays the winning solver solver_fee_bps of the total surplus from the
// fee pool, in the numeraire, CAPPED at the pool balance so the pool is solvent
// by construction and never mints (STOP #1 §3 composition). The skim (a fraction
// of surplus) funds the pool; this fee (a fraction of surplus) draws from it —
// the two bps compose on the surplus base, with any pool remainder retained as
// protocol revenue. lifetime_surplus accumulates the delivered surplus.
func (k Keeper) paySolver(ctx sdk.Context, solverAddr sdk.AccAddress, totalSurplus math.LegacyDec, numeraire string, feeBps uint32) error {
	if !totalSurplus.IsPositive() || numeraire == "" {
		return nil
	}
	payout := totalSurplus.MulInt64(int64(feeBps)).QuoInt64(bpsDenominator).TruncateInt()
	if !payout.IsPositive() {
		return nil
	}
	poolBal := k.bankKeeper.GetBalance(ctx, k.ModuleAddress(), numeraire).Amount
	if payout.GT(poolBal) {
		payout = poolBal
	}

	solver, err := k.GetSolver(ctx, phase0Chamber, solverAddr)
	if err != nil {
		// no registry record (e.g. unbonded between propose and conclude): no fee.
		return nil
	}
	if payout.IsPositive() {
		coin := sdk.NewCoins(sdk.NewCoin(numeraire, payout))
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, solverAddr, coin); err != nil {
			return err
		}
	}
	solver.LifetimeSurplus = solver.LifetimeSurplus.Add(totalSurplus)
	if err := k.setSolver(ctx, solver); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeSolverPaid,
		sdk.NewAttribute(types.AttributeKeySolver, solverAddr.String()),
		sdk.NewAttribute(types.AttributeKeyAmount, sdk.NewCoin(numeraire, payout).String()),
	))
	return nil
}
