package keeper

import (
	"strconv"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/AryaSingh22/conclave/x/session/types"
)

const bpsDenominator = 10_000

// GetSolver loads a solver registry entry by address.
func (k Keeper) GetSolver(ctx sdk.Context, chamberID uint32, addr sdk.AccAddress) (types.Solver, error) {
	bz, err := k.storeService.OpenKVStore(ctx).Get(types.SolverKey(chamberID, addr.Bytes()))
	if err != nil {
		return types.Solver{}, err
	}
	if bz == nil {
		return types.Solver{}, errorsmod.Wrapf(types.ErrSolverNotFound, "addr %s", addr)
	}
	var s types.Solver
	if err := k.cdc.Unmarshal(bz, &s); err != nil {
		return types.Solver{}, err
	}
	return s, nil
}

// setSolver persists a solver record (no index side effects).
func (k Keeper) setSolver(ctx sdk.Context, s types.Solver) error {
	addr, err := sdk.AccAddressFromBech32(s.Addr)
	if err != nil {
		return err
	}
	bz, err := k.cdc.Marshal(&s)
	if err != nil {
		return err
	}
	// Solver records are phase0Chamber-scoped (the proto carries no chamber
	// field; solvers are chain-global until Phase 3).
	return k.storeService.OpenKVStore(ctx).Set(types.SolverKey(phase0Chamber, addr.Bytes()), bz)
}

// RegisterSolver registers and bonds a solver (SPEC §6.4: bond ≥
// solver_min_bond). The bond escrows into the session module account.
func (k Keeper) RegisterSolver(ctx sdk.Context, addr sdk.AccAddress, bond sdk.Coins) error {
	if _, err := k.GetSolver(ctx, phase0Chamber, addr); err == nil {
		return errorsmod.Wrapf(types.ErrSolverExists, "addr %s", addr)
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	if err := bond.Validate(); err != nil {
		return errorsmod.Wrapf(types.ErrInvalidBond, "%s", err)
	}
	minBond := params.SolverMinBond
	if bond.AmountOf(minBond.Denom).LT(minBond.Amount) {
		return errorsmod.Wrapf(types.ErrBondInsufficient,
			"bond %s below minimum %s", bond, minBond)
	}

	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, addr, types.ModuleName, bond); err != nil {
		return err
	}

	s := types.Solver{
		Addr:            addr.String(),
		Bond:            bond,
		Status:          types.SOLVER_STATUS_ACTIVE,
		LifetimeSurplus: math.LegacyZeroDec(),
	}
	if err := k.setSolver(ctx, s); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeSolverRegistered,
		sdk.NewAttribute(types.AttributeKeySolver, addr.String()),
		sdk.NewAttribute(types.AttributeKeyBond, bond.String()),
	))
	return nil
}

// UnbondSolver starts the SPEC §6.4 unbonding period (1 day of blocks). The
// bond releases at the completion height via the EndBlock sweep.
func (k Keeper) UnbondSolver(ctx sdk.Context, addr sdk.AccAddress) (uint64, error) {
	s, err := k.GetSolver(ctx, phase0Chamber, addr)
	if err != nil {
		return 0, err
	}
	if s.Status != types.SOLVER_STATUS_ACTIVE {
		return 0, errorsmod.Wrapf(types.ErrSolverNotActive, "addr %s status %s", addr, s.Status)
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return 0, err
	}
	completion := uint64(ctx.BlockHeight()) + params.SolverUnbondBlocks
	s.Status = types.SOLVER_STATUS_UNBONDING
	s.UnbondHeight = completion
	if err := k.setSolver(ctx, s); err != nil {
		return 0, err
	}
	if err := k.storeService.OpenKVStore(ctx).Set(types.SolverUnbondIdxKey(completion, addr.Bytes()), []byte{}); err != nil {
		return 0, err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeSolverUnbonding,
		sdk.NewAttribute(types.AttributeKeySolver, addr.String()),
		sdk.NewAttribute(types.AttributeKeyUnbondHeight, strconv.FormatUint(completion, 10)),
	))
	return completion, nil
}

// SweepUnbondedSolvers releases bonds whose unbonding period has elapsed and
// deletes the solver records (SPEC §6.4 unbonding). Walks the 0x04 index up to
// the current height in ascending order (deterministic, §11.6).
func (k Keeper) SweepUnbondedSolvers(ctx sdk.Context) error {
	height := uint64(ctx.BlockHeight())
	store := k.storeService.OpenKVStore(ctx)
	// inclusive of height: index keys with unbond_height <= height release now.
	end := types.SolverUnbondIdxKey(height+1, []byte{})
	it, err := store.Iterator(types.SolverUnbondIdxPrefix, end)
	if err != nil {
		return err
	}

	type release struct {
		idxKey []byte
		addr   sdk.AccAddress
	}
	var releases []release
	for ; it.Valid(); it.Next() {
		key := append([]byte{}, it.Key()...)
		releases = append(releases, release{idxKey: key, addr: sdk.AccAddress(types.AddrFromUnbondIdxKey(key))})
	}
	if err := it.Close(); err != nil {
		return err
	}

	for _, r := range releases {
		s, err := k.GetSolver(ctx, phase0Chamber, r.addr)
		if err != nil {
			return err
		}
		if !s.Bond.IsZero() {
			if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, r.addr, s.Bond); err != nil {
				return err
			}
		}
		if err := store.Delete(r.idxKey); err != nil {
			return err
		}
		if err := store.Delete(types.SolverKey(phase0Chamber, r.addr.Bytes())); err != nil {
			return err
		}
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeSolverUnbonded,
			sdk.NewAttribute(types.AttributeKeySolver, r.addr.String()),
		))
	}
	return nil
}

// slashSolver slashes slashBps of a solver's bond into the community pool and
// increments slashed_count (SPEC §6.4: invalid solution 5%, equivocation 20%).
// The slash is applied on the PARENT context by the caller — it persists even
// when a Conclude cache is discarded (Guard 3).
func (k Keeper) slashSolver(ctx sdk.Context, s *types.Solver, slashBps uint32, reason string) error {
	slash := sdk.NewCoins()
	for _, c := range s.Bond {
		amt := c.Amount.MulRaw(int64(slashBps)).QuoRaw(bpsDenominator)
		if amt.IsPositive() {
			slash = slash.Add(sdk.NewCoin(c.Denom, amt))
		}
	}
	if !slash.IsZero() {
		if err := k.distrKeeper.FundCommunityPool(ctx, slash, authtypes.NewModuleAddress(types.ModuleName)); err != nil {
			return err
		}
		s.Bond = s.Bond.Sub(slash...)
	}
	s.SlashedCount++
	if err := k.setSolver(ctx, *s); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeSolverSlashed,
		sdk.NewAttribute(types.AttributeKeySolver, s.Addr),
		sdk.NewAttribute(types.AttributeKeySlashed, slash.String()),
		sdk.NewAttribute(types.AttributeKeyReason, reason),
	))
	return nil
}

// IterateSolvers walks every solver in key order (genesis export, queries).
func (k Keeper) IterateSolvers(ctx sdk.Context, fn func(s types.Solver) bool) error {
	store := k.storeService.OpenKVStore(ctx)
	it, err := store.Iterator(types.SolverKeyPrefix, types.PrefixEnd(types.SolverKeyPrefix))
	if err != nil {
		return err
	}
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var s types.Solver
		if err := k.cdc.Unmarshal(it.Value(), &s); err != nil {
			return err
		}
		if fn(s) {
			return nil
		}
	}
	return nil
}
