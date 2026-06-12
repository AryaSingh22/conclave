package keeper

import (
	"strconv"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/AryaSingh22/conclave/x/mandate/types"
)

// ChargeWindow implements SPEC §6.2 steps 4–5 for one tx's aggregate spend
// intent: per-tx ceiling, window reset, window ceiling, then OPTIMISTIC
// charging. The caller (MandateDecorator) invokes this identically in
// CheckTx and DeliverTx — zero mode branching. CheckTx charges accumulate
// in the mempool's check state so parallel unordered txs from one key are
// bounded before any of them executes; DeliverTx charges commit; the check
// state re-branches from committed state each block and surviving mempool
// txs re-charge on recheck.
//
// Ceiling comparisons are deny-by-default per denom (D-019): a spend denom
// absent from a ceiling Coins list has an implicit ceiling of zero.
func (k Keeper) ChargeWindow(ctx sdk.Context, m types.Mandate, spend sdk.Coins) error {
	// step 4: per-tx spend ceiling
	if !spend.IsAllLTE(m.SpendCeilingPerTx) {
		return errorsmod.Wrapf(types.ErrMandateExceeded,
			"spend intent %s exceeds per-tx ceiling %s", spend, m.SpendCeilingPerTx)
	}

	// step 5: window reset
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

	// step 5: window ceiling, then charge optimistically (refunds for
	// expired/cancelled unmatched intents are x/session's job — see
	// RefundWindowSpend)
	newSpent := m.WindowSpent.Add(spend...)
	if !newSpent.IsAllLTE(m.SpendCeilingWindow) {
		return errorsmod.Wrapf(types.ErrMandateExceeded,
			"window spend %s + intent %s exceeds window ceiling %s",
			m.WindowSpent, spend, m.SpendCeilingWindow)
	}
	m.WindowSpent = newSpent
	return k.SetMandate(ctx, m)
}

// RefundWindowSpend returns optimistically charged spend to the grantee's
// current window, clamped at zero per denom: refunds never cross a window
// reset, and a replaced/absent mandate refunds nothing.
//
// CONCLAVE-TODO(phase-0): x/session EndBlock calls this at task 0.8 (SPEC
// §6.4 EndBlock step 7 "mandate window refunds for expired/cancelled
// intents") with the spend intent charged for each unmatched intent.
func (k Keeper) RefundWindowSpend(ctx sdk.Context, chamberID uint32, grantee sdk.AccAddress, amount sdk.Coins) error {
	m, found, err := k.ActiveMandateByGrantee(ctx, chamberID, grantee)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	refund := m.WindowSpent.Min(amount)
	if refund.IsZero() {
		return nil
	}
	m.WindowSpent = m.WindowSpent.Sub(refund...)
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
