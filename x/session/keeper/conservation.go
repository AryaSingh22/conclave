package keeper

import (
	"encoding/hex"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	"github.com/AryaSingh22/conclave/x/session/types"
)

// legAssetKey returns the conservation namespace and amount for one settlement
// leg across all asset types: coins by denom, service vouchers as svc/{schema},
// data grants as data/{hex(content_hash)} (D-006 pricing keys). Conservation
// and limit satisfaction both key on this.
func legAssetKey(leg types.SettleLeg) (string, math.Int, error) {
	set := 0
	var key string
	amount := math.ZeroInt()
	if c := leg.GetCoin(); c != nil {
		set++
		key = c.Denom
		amount = c.Amount
	}
	if v := leg.GetSvc(); v != nil {
		set++
		key = "svc/" + v.Schema
		amount = v.Units
	}
	if d := leg.GetData(); d != nil {
		set++
		key = "data/" + hex.EncodeToString(d.ContentHash)
		// a DataGrant has NO ledger quantity: amount 1 is a bookkeeping fiction
		// so the graph balances (D-048). The real invariant is offered+wanted,
		// enforced by rule 3 — not value conservation.
		amount = math.OneInt()
	}
	if set != 1 {
		return "", math.ZeroInt(), errorsmod.Wrap(types.ErrInvalidLeg, "leg must set exactly one asset")
	}
	return key, amount, nil
}

// NetFlows maps sigil → assetKey → net flow (positive = net received, negative
// = net given) across a match's legs.
type NetFlows map[string]map[string]math.Int

func (n NetFlows) add(sigil, asset string, delta math.Int) {
	row, ok := n[sigil]
	if !ok {
		row = make(map[string]math.Int)
		n[sigil] = row
	}
	cur, ok := row[asset]
	if !ok {
		cur = math.ZeroInt()
	}
	row[asset] = cur.Add(delta)
}

// CheckConservation is the standalone SPEC §6.4 rule-2 graph-balance check. It
// is a BOOKKEEPING device, not a ledger-conservation proof: it verifies each
// leg structurally and that, per asset, Σ net flow over all parties == 0 (every
// unit "given" in the leg graph is "received"). It is a pure function of the
// leg data (no ctx, no store) so it can be exhaustively fuzzed against
// adversarial graphs (cycles, self-trades, duplicate legs).
//
// The ledger invariant this graph balance BACKS differs by asset class (see
// applyLeg in conclude.go; D-048). It is NOT "value conserved" for all three:
//   - coin  → value conservation: applyLeg debits an existing balance and
//     routes it through the module account; graph-zero == supply-zero.
//   - svc   → authorized mint: applyLeg MINTS svc/{schema} to the receiver
//     (the giver is never debited; supply rises by design). Graph-zero
//     means every minted unit is matched by a giver who took the -units
//     side, whose offer is authorized by rule 3.
//   - data  → authorized creation: applyLeg writes an escrow stub; NO ledger
//     quantity exists. The per-leg amount of 1 is a pure accounting
//     fiction so the graph balances; the real invariant is that the
//     grant was offered (give) and wanted (want), enforced by rule 3.
//
// What it enforces structurally (the teeth):
//   - exactly one recognized asset per leg, with a positive amount;
//   - non-empty, distinct endpoints (from != to) — an empty endpoint would be
//     a phantom source/sink; a self-trade is graph noise that can mask exploits;
//   - Phase-0: no delivery condition (NONE only — SPEC §6.5);
//   - per asset, Σ net flow over all parties == 0.
//
// It returns the per-party net flows so limit satisfaction (rule 3) and the
// price-binding check reuse the same accounting rather than recomputing it.
func CheckConservation(matches []types.Match) (NetFlows, error) {
	flows := make(NetFlows)
	for mi := range matches {
		for li := range matches[mi].Legs {
			leg := matches[mi].Legs[li]
			if leg.FromSigil == "" || leg.ToSigil == "" {
				return nil, errorsmod.Wrapf(types.ErrInvalidLeg, "match %d leg %d: empty endpoint", mi, li)
			}
			if leg.FromSigil == leg.ToSigil {
				return nil, errorsmod.Wrapf(types.ErrInvalidLeg, "match %d leg %d: self-trade %s", mi, li, leg.FromSigil)
			}
			if leg.Condition != nil && leg.Condition.Condition != nil {
				return nil, errorsmod.Wrapf(types.ErrConditionsNotEnabled, "match %d leg %d", mi, li)
			}
			key, amount, err := legAssetKey(leg)
			if err != nil {
				return nil, errorsmod.Wrapf(err, "match %d leg %d", mi, li)
			}
			if !amount.IsPositive() {
				return nil, errorsmod.Wrapf(types.ErrInvalidLeg, "match %d leg %d: non-positive amount", mi, li)
			}
			flows.add(leg.FromSigil, key, amount.Neg())
			flows.add(leg.ToSigil, key, amount)
		}
	}

	// per-asset global balance: Σ over all parties == 0 for every asset. The
	// net per asset is accumulated independently of party iteration order, so
	// the zero check is deterministic regardless of map ordering (§11.6: this
	// is pure computation, never iterated into a state write).
	assetNet := make(map[string]math.Int)
	for _, row := range flows {
		for asset, net := range row {
			cur, ok := assetNet[asset]
			if !ok {
				cur = math.ZeroInt()
			}
			assetNet[asset] = cur.Add(net)
		}
	}
	for asset, net := range assetNet {
		if !net.IsZero() {
			return nil, errorsmod.Wrapf(types.ErrConservation, "asset %s nets %s", asset, net)
		}
	}
	return flows, nil
}
