package keeper

import (
	"strconv"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/AryaSingh22/conclave/x/mandate/types"
)

// ChargeWindow implements SPEC §6.2 steps 4–5 for one tx, charging its spend
// against the mandate. D-059 splits the spend into two physically distinct
// quantities with two distinct conservation laws:
//
//   - commit  — refundable optimistic intent commitments (MsgSubmitIntent):
//     accumulated in OUTSTANDING_COMMITMENT, a LIFETIME balance that is
//     released only when the intent terminates (DecommitIntent) and is NEVER
//     reset by a window rollover or amendment, because the funds remain
//     committed while the intent is live.
//   - settled — non-refundable settled spend (bank sends, etc.): accumulated
//     in WINDOW_SPENT, a PER-PERIOD consumption counter that resets each
//     window (SPEC §6.2 step 5) to grant fresh per-period budget.
//
// Conflating the two (the pre-D-059 single window_spent) caused the task-0.11
// finding: a window reset zeroed live intent commitments, freeing ceiling
// capacity while the intent was still outstanding (over-commitment, a
// §1.1-goal-3 bound weakening) and making the later DecommitIntent clamp-lose.
//
// The caller (MandateDecorator) invokes this identically in CheckTx and
// DeliverTx — zero mode branching. Ceiling comparisons are deny-by-default per
// denom (D-019).
func (k Keeper) ChargeWindow(ctx sdk.Context, m types.Mandate, commit, settled sdk.Coins) error {
	// step 4: per-tx spend ceiling applies to this tx's TOTAL spend intent
	// (commitment + settled), regardless of which counter each part lands in.
	total := commit.Add(settled...)
	if !total.IsAllLTE(m.SpendCeilingPerTx) {
		return errorsmod.Wrapf(types.ErrMandateExceeded,
			"spend intent %s exceeds per-tx ceiling %s", total, m.SpendCeilingPerTx)
	}

	// step 5: per-period reset of the SETTLED-spend counter only. The lifetime
	// outstanding_commitment is deliberately untouched here (D-059).
	height := uint64(ctx.BlockHeight())
	if height >= m.WindowStartHeight+m.WindowBlocks {
		m.WindowSpent = sdk.NewCoins()
		m.WindowStartHeight = height
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeMandateWindowReset,
			sdk.NewAttribute(types.AttributeKeyMandateID, m.Id),
			sdk.NewAttribute(types.AttributeKeyGrantee, m.GranteeAddr),
			sdk.NewAttribute(types.AttributeKeyWindowStart, strconv.FormatUint(height, 10)),
		))
	}

	// step 5: the window ceiling bounds TOTAL live exposure at any instant —
	// outstanding intent commitments plus settled spend this period — so a
	// leaked key can never commit + spend beyond spend_ceiling_window even
	// across a reset (the over-commitment the conflation allowed).
	newCommit := m.OutstandingCommitment.Add(commit...)
	newSpent := m.WindowSpent.Add(settled...)
	exposure := newCommit.Add(newSpent...)
	if !exposure.IsAllLTE(m.SpendCeilingWindow) {
		return errorsmod.Wrapf(types.ErrMandateExceeded,
			"exposure (outstanding %s + window %s) exceeds window ceiling %s",
			newCommit, newSpent, m.SpendCeilingWindow)
	}
	m.OutstandingCommitment = newCommit
	m.WindowSpent = newSpent
	return k.SetMandate(ctx, m)
}

// DecommitIntent releases an intent's optimistic commitment from the grantee's
// LIFETIME outstanding_commitment when the intent terminates (SPEC §6.4 EndBlock
// step 7; D-059). x/veil calls it for EXPIRED/CANCELLED/CONCLUDED intents with
// the exact charged_spend it persisted, so the release is exact and the global
// invariant Σ outstanding_commitment == Σ charged_spend(live intents) is
// preserved. Because outstanding_commitment is a lifetime balance (never reset),
// the .Min clamp is a defensive belt that never bites in correct operation: a
// live intent's charge is always present to release (it was the
// clamp-and-lose under the old per-period window_spent that this fix removes).
// A replaced/absent mandate releases nothing.
func (k Keeper) DecommitIntent(ctx sdk.Context, chamberID uint32, grantee sdk.AccAddress, amount sdk.Coins) error {
	m, found, err := k.ActiveMandateByGrantee(ctx, chamberID, grantee)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	release := m.OutstandingCommitment.Min(amount)
	if release.IsZero() {
		return nil
	}
	m.OutstandingCommitment = m.OutstandingCommitment.Sub(release...)
	return k.SetMandate(ctx, m)
}

// FeeAbstractionMandate is the single predicate deciding whether SPEC §6.2
// fee abstraction applies to a tx: the fee payer is mandate-bound (active),
// and the fee is exactly one coin in a non-native denom listed in the
// mandate's fee_denoms. Both the DeductFee skip-wrapper and the
// FeeAbstractionDecorator call this, so exactly one of them handles any tx.
func (k Keeper) FeeAbstractionMandate(ctx sdk.Context, feePayer sdk.AccAddress, fee sdk.Coins) (types.Mandate, bool) {
	if len(fee) != 1 {
		return types.Mandate{}, false
	}
	m, found, err := k.ActiveMandateByGrantee(ctx, phase0Chamber, feePayer)
	if err != nil || !found {
		return types.Mandate{}, false
	}
	if m.Revoked || uint64(ctx.BlockHeight()) >= m.ExpiryHeight {
		return types.Mandate{}, false
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return types.Mandate{}, false
	}
	if fee[0].Denom == params.FeePool.Denom {
		return types.Mandate{}, false
	}
	for _, denom := range m.FeeDenoms {
		if denom == fee[0].Denom {
			return m, true
		}
	}
	return types.Mandate{}, false
}

// ExecuteFeeAbstraction implements the SPEC §6.2 FeeAbstractionDecorator
// settlement: the foreign-denom fee is deducted from fee_sponsor if set,
// else from the grantee, into the module fee pool account; the pool pays
// the fee collector the converted amount in the native denom at the fixed
// param rate. No mint, no burn — the pool is a genesis-funded CLV float.
//
// CONCLAVE-TODO(phase-2): task 2.5 replaces the fixed fee_conversion_rate
// with a TWAP from session clearing prices.
func (k Keeper) ExecuteFeeAbstraction(ctx sdk.Context, m types.Mandate, fee sdk.Coins) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}

	payer := sdk.MustAccAddressFromBech32(m.GranteeAddr)
	if m.FeeSponsor != "" {
		payer = sdk.MustAccAddressFromBech32(m.FeeSponsor)
	}

	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, payer, types.ModuleName, fee); err != nil {
		return err
	}

	// truncation favors the pool: the fee collector never receives more
	// native value than the posted rate implies
	converted := fee[0].Amount.ToLegacyDec().Mul(params.FeeConversionRate).TruncateInt()
	if converted.IsPositive() {
		poolCoin := sdk.NewCoin(params.FeePool.Denom, converted)
		poolAddr := k.accountKeeper.GetModuleAddress(types.ModuleName)
		if balance := k.bankKeeper.GetBalance(ctx, poolAddr, params.FeePool.Denom); balance.Amount.LT(converted) {
			return errorsmod.Wrapf(types.ErrFeePoolInsufficient,
				"pool holds %s, conversion needs %s", balance, poolCoin)
		}
		if err := k.bankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, authtypes.FeeCollectorName, sdk.NewCoins(poolCoin)); err != nil {
			return err
		}
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeFeeConverted,
		sdk.NewAttribute(types.AttributeKeyMandateID, m.Id),
		sdk.NewAttribute(types.AttributeKeyFeePayer, payer.String()),
		sdk.NewAttribute(types.AttributeKeyFee, fee.String()),
		sdk.NewAttribute(types.AttributeKeyFeeClv, converted.String()),
	))
	return nil
}

// phase0Chamber is the only active chamber until Phase 3 (SPEC §1.2).
// CONCLAVE-TODO(phase-3): chamber routing once chamber_id > 0 activates.
const phase0Chamber uint32 = 0

// Phase0Chamber exposes the active chamber to the ante decorators.
func Phase0Chamber() uint32 { return phase0Chamber }
