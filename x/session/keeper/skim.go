package keeper

import (
	"sort"
	"strings"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/session/types"
)

// feePoolSigil is the reserved graph-node label for the protocol fee pool (the
// session module account, keys.go). A skim leg moves value party → feePoolSigil
// so it runs through the SAME CheckConservation as any other leg (the skim
// neither mints nor burns — it redirects already-received numeraire to the
// pool). Real sigil ids are 64-char hex (D-009), so this "@"-prefixed label can
// never collide with one. It is a checker/validation label only: skim coins are
// applied via SendCoinsFromAccountToModule, never resolved to an address.
const feePoolSigil = "@feepool"

// computeMatchSkim derives the SPEC §6.4 surplus skim for one match (Half B,
// approved STOP #1 + D-050). The skim is taken ONLY from each party's realized
// numeraire RECEIVE-EXCESS — recv_N − floor_N, the numeraire it physically
// received above its want-floor — so it can never touch principal (the party
// still ends ≥ floor and, give legs untouched, ≤ cap). The amount is
// floor(surplus_skim_bps × excess / 10000), an exact integer in numeraire base
// units (no rounding, D-047). Each non-zero skim becomes a party → feePoolSigil
// leg. Returns the skim legs in fixed (m.IntentIds) order — deterministic,
// never iterated from a map (§11.6).
//
// Phase-0 narrowing (D-051): only numeraire (coin) receive-excess is skimmed;
// give-side savings and non-numeraire receive-excess are SCORED but not skimmed.
// A data/{hash} numeraire has no ledger quantity, so it is never skimmable.
// CONCLAVE-TODO(phase-2): multi-asset in-kind skim + in-kind solver payout.
func (k Keeper) computeMatchSkim(m types.Match, intents []matchIntent, flows NetFlows, numeraire string, skimBps uint32) []types.SettleLeg {
	if numeraire == "" || strings.HasPrefix(numeraire, "data/") {
		return nil
	}
	var legs []types.SettleLeg
	for i := range intents {
		mi := intents[i]
		row := flows[mi.sigil]
		if row == nil {
			continue
		}
		recvN := posPart(flowOf(row, numeraire))
		floorN := wantFloor(mi, numeraire)
		excess := recvN.Sub(floorN)
		if !excess.IsPositive() {
			continue
		}
		skim := excess.MulRaw(int64(skimBps)).QuoRaw(bpsDenominator)
		if !skim.IsPositive() {
			continue
		}
		coin := sdk.NewCoin(numeraire, skim)
		legs = append(legs, types.SettleLeg{
			FromSigil: mi.sigil,
			ToSigil:   feePoolSigil,
			Asset:     &types.SettleLeg_Coin{Coin: &coin},
		})
	}
	return legs
}

// wantFloor returns an intent's want-leg amount for one asset key (its floor in
// that asset; 0 if it does not want the asset). Mirrors intentLegKeyAmount.
func wantFloor(mi matchIntent, assetKey string) math.Int {
	floor := math.ZeroInt()
	for i := range mi.payload.Want {
		key, qty, err := intentLegKeyAmount(mi.payload.Want[i])
		if err != nil {
			continue
		}
		if key == assetKey {
			floor = floor.Add(qty)
		}
	}
	return floor
}

// checkSkimBinding is the Half-B generalization of D-047 to the skim-inclusive
// graph. Re-running Half-A's exact-zero checkPriceBinding here is provably
// impossible — extracting value v>0 from a party forces its price-valued
// residual to −v and the pool's to +Σv (a conservation identity; STOP #1 §4).
// So this asserts the residual == −skim_p invariant instead, on flows produced
// by the SAME CheckConservation any leg runs through:
//
//   - every REAL party's residual ≤ 0 (it only gives value to the pool) and
//     |residual| ≤ its surplus (the skim never bites principal);
//   - the fee pool is the SOLE sink: any non-pool party with a POSITIVE residual
//     means value was routed somewhere other than the pool → reject;
//   - the pool's residual equals the total skim taken from the parties.
//
// On the happy path the skim is derived from receive-excess and bounded by it,
// so this passes by construction; it is the load-bearing guard (like 0.7's
// Guard 1) that makes an over-extracting or mis-routed skim impossible —
// exercised red in skim_test.go. Values are LegacyDec in the numeraire; the
// residual is exact (no rounding, D-047). Parties iterate in sorted sigil order
// so a reported violation is deterministic (the pass/fail is order-independent).
func checkSkimBinding(flows NetFlows, priceOf map[string]math.LegacyDec, surplusBySigil map[string]math.LegacyDec) error {
	sigils := make([]string, 0, len(flows))
	for sigil := range flows {
		sigils = append(sigils, sigil)
	}
	sort.Strings(sigils)

	totalSkim := math.LegacyZeroDec()
	poolResidual := math.LegacyZeroDec()
	havePool := false

	for _, sigil := range sigils {
		residual := math.LegacyZeroDec()
		for asset, net := range flows[sigil] {
			p, ok := priceOf[asset]
			if !ok {
				return errorsmod.Wrapf(types.ErrSkimViolation, "sigil %s flows unpriced asset %s", sigil, asset)
			}
			residual = residual.Add(p.MulInt(net))
		}

		if sigil == feePoolSigil {
			havePool = true
			poolResidual = residual
			continue
		}

		// a real party may only GIVE value to the skim (residual ≤ 0); a positive
		// residual means value landed on a non-pool party — skim mis-routed.
		if residual.IsPositive() {
			return errorsmod.Wrapf(types.ErrSkimViolation,
				"non-pool party %s nets positive value %s (skim routed off the fee pool)", sigil, residual)
		}
		skimVal := residual.Neg()
		if skimVal.IsZero() {
			continue
		}
		surplus, ok := surplusBySigil[sigil]
		if !ok {
			surplus = math.LegacyZeroDec()
		}
		if skimVal.GT(surplus) {
			return errorsmod.Wrapf(types.ErrSkimViolation,
				"skim from %s is %s but its surplus is only %s (skim would touch principal)", sigil, skimVal, surplus)
		}
		totalSkim = totalSkim.Add(skimVal)
	}

	if totalSkim.IsPositive() && !havePool {
		return errorsmod.Wrapf(types.ErrSkimViolation, "skim of %s taken but no fee-pool sink leg present", totalSkim)
	}
	if havePool && !poolResidual.Equal(totalSkim) {
		return errorsmod.Wrapf(types.ErrSkimViolation,
			"fee-pool residual %s != total skim %s", poolResidual, totalSkim)
	}
	return nil
}
