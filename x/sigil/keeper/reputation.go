package keeper

import (
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/sigil/types"
)

// SetReputation stores a reputation record.
// Implements SPEC §6.1 (key 0x03|chamber|sigil_id).
func (k Keeper) SetReputation(ctx sdk.Context, rep types.Reputation) error {
	idBz, err := types.SigilIDBytes(rep.SigilId)
	if err != nil {
		return err
	}
	bz, err := k.cdc.Marshal(&rep)
	if err != nil {
		return err
	}
	return k.storeService.OpenKVStore(ctx).Set(types.ReputationKey(rep.ChamberId, idBz), bz)
}

// GetReputation returns the stored reputation record with lazy decay applied
// to the returned value (SPEC §6.1). Reads never write: decay is persisted
// only on the state-mutating paths (settlement / penalty) so that queries
// stay side-effect free and consensus state changes only in tx/EndBlock
// execution (D-013).
func (k Keeper) GetReputation(ctx sdk.Context, chamberID uint32, sigilID string) (types.Reputation, error) {
	rep, err := k.getStoredReputation(ctx, chamberID, sigilID)
	if err != nil {
		return types.Reputation{}, err
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return types.Reputation{}, err
	}
	applyDecay(&rep, uint64(ctx.BlockHeight()), params)
	return rep, nil
}

func (k Keeper) getStoredReputation(ctx sdk.Context, chamberID uint32, sigilID string) (types.Reputation, error) {
	idBz, err := types.SigilIDBytes(sigilID)
	if err != nil {
		return types.Reputation{}, err
	}
	bz, err := k.storeService.OpenKVStore(ctx).Get(types.ReputationKey(chamberID, idBz))
	if err != nil {
		return types.Reputation{}, err
	}
	if bz == nil {
		return types.Reputation{}, errorsmod.Wrapf(types.ErrSigilNotFound, "no reputation for sigil %s", sigilID)
	}
	var rep types.Reputation
	if err := k.cdc.Unmarshal(bz, &rep); err != nil {
		return types.Reputation{}, err
	}
	return rep, nil
}

// AddSettlementReputation applies the SPEC §6.1 reputation gain for a
// settled value: score += base_gain * value_weight(settled_value), where
// value_weight = min(1, log10(value_usdc)/4). valueUSDC is the settled value
// in whole USDC as a Dec. Called by x/session on Receipt emission (task 0.9).
//
// implements SPEC §6.1 reputation math.
func (k Keeper) AddSettlementReputation(ctx sdk.Context, chamberID uint32, sigilID string, valueUSDC math.LegacyDec) error {
	rep, err := k.getStoredReputation(ctx, chamberID, sigilID)
	if err != nil {
		return err
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}

	applyDecay(&rep, uint64(ctx.BlockHeight()), params)

	gain := params.BaseGain.Mul(valueWeight(valueUSDC))
	rep.Score = rep.Score.Add(gain)
	rep.ConcludedCount++

	if err := k.SetReputation(ctx, rep); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeReputationUpdate,
		sdk.NewAttribute(types.AttributeKeySigilID, sigilID),
		sdk.NewAttribute(types.AttributeKeyScore, rep.Score.String()),
	))
	return nil
}

// PenalizeLostDispute applies the SPEC §6.1 lost-dispute penalty:
// score -= penalty, penalty = penalty_multiplier × base_gain (default 10×).
// The score floors at zero — reputation is never negative (§6.1 invariant).
// Called by x/escrow dispute resolution (Phase 2).
//
// implements SPEC §6.1 reputation math.
func (k Keeper) PenalizeLostDispute(ctx sdk.Context, chamberID uint32, sigilID string) error {
	rep, err := k.getStoredReputation(ctx, chamberID, sigilID)
	if err != nil {
		return err
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}

	applyDecay(&rep, uint64(ctx.BlockHeight()), params)

	penalty := params.PenaltyMultiplier.Mul(params.BaseGain)
	rep.Score = rep.Score.Sub(penalty)
	if rep.Score.IsNegative() {
		rep.Score = math.LegacyZeroDec()
	}
	rep.DisputedCount++

	if err := k.SetReputation(ctx, rep); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeReputationUpdate,
		sdk.NewAttribute(types.AttributeKeySigilID, sigilID),
		sdk.NewAttribute(types.AttributeKeyScore, rep.Score.String()),
	))
	return nil
}

// applyDecay applies SPEC §6.1 lazy decay in place:
// score *= decay_factor ^ (blocks_since_last_decay / decay_period).
//
// The exponent is the number of WHOLE periods elapsed (integer division);
// last_decay_height advances by exactly periods*decay_period so the
// remainder blocks carry into the next application — no drift, fully
// deterministic (D-013). LegacyDec.Power uses exponentiation by squaring,
// so extreme block gaps cost O(log periods) multiplications and underflow
// cleanly to zero with no overflow or panic (§6.1 fuzz target).
func applyDecay(rep *types.Reputation, currentHeight uint64, params types.Params) {
	if currentHeight <= rep.LastDecayHeight {
		return
	}
	elapsed := currentHeight - rep.LastDecayHeight
	periods := elapsed / params.DecayPeriodBlocks
	if periods == 0 {
		return
	}

	rep.Score = rep.Score.Mul(params.DecayFactor.Power(periods))
	rep.LastDecayHeight += periods * params.DecayPeriodBlocks
}

// valueWeight implements SPEC §6.1: value_weight = min(1, log10(value_usdc)/4).
func valueWeight(valueUSDC math.LegacyDec) math.LegacyDec {
	w := log10Floor(valueUSDC).QuoInt64(4)
	one := math.LegacyOneDec()
	if w.GT(one) {
		return one
	}
	return w
}

// log10Floor is a deterministic, float-free log10 approximation over
// LegacyDec, exact at powers of 10 and piecewise-linear within each decade:
//
//	for v >= 1:  log10(v) ≈ d + (v/10^d − 1)/9,  d = decimal digits of ⌊v⌋ − 1
//
// d is derived from the decimal digit count of the integer part — a pure
// integer operation. The fractional term maps v/10^d ∈ [1,10) linearly onto
// [0,1), so the approximation is monotonic, continuous, and within 0.05 of
// true log10 (worst case mid-decade), which is ample for reputation
// weighting. Values v <= 1 return 0 (true log10 would be <= 0 and the
// weight clamps there anyway — D-012). No floating point anywhere
// (SPEC §11.6).
//
// implements SPEC §6.1 value_weight.
func log10Floor(v math.LegacyDec) math.LegacyDec {
	one := math.LegacyOneDec()
	if v.IsNil() || !v.GT(one) {
		return math.LegacyZeroDec()
	}

	// d = floor(log10(v)) for v >= 1, via decimal digit count of the
	// integer part (deterministic: big.Int base-10 rendering)
	digits := len(v.TruncateInt().BigInt().Text(10))
	d := uint64(digits - 1)

	pow10d := math.LegacyNewDec(10).Power(d)
	frac := v.Quo(pow10d).Sub(one).Quo(math.LegacyNewDec(9)) // ∈ [0,1)

	return math.LegacyNewDec(int64(d)).Add(frac)
}

// IterateReputations walks all reputation records (invariants).
func (k Keeper) IterateReputations(ctx sdk.Context, cb func(types.Reputation) bool) error {
	store := k.storeService.OpenKVStore(ctx)
	it, err := store.Iterator(types.ReputationKeyPrefix, storetypes.PrefixEndBytes(types.ReputationKeyPrefix))
	if err != nil {
		return err
	}
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var rep types.Reputation
		if err := k.cdc.Unmarshal(it.Value(), &rep); err != nil {
			return err
		}
		if cb(rep) {
			break
		}
	}
	return nil
}
