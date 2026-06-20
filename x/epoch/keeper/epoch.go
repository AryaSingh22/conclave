package keeper

import (
	"sort"
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/epoch/types"
)

// MaybeOpenEpoch opens a new FORMING epoch when the height is an epoch boundary
// (PHASE1_DESIGN §2(b), D-068). The boundary is HEIGHT-BASED — height %
// epoch_length_blocks == 0 — mirroring the proven x/session SessionBoundary
// pattern, and the committee is the staking-finalized bonded set snapshotted
// here. Reacting to raw ABCI ValidatorUpdate diffs would be nondeterministic to
// reconstruct; a height boundary with the bonded set staking has already
// committed for the block is deterministic AND is exactly the validator set
// CometBFT will use — the resolution of §2(b)'s "valset-update trigger". Because
// x/epoch's EndBlocker runs AFTER staking's (app_config.go), the set read here is
// final for the block (the load-bearing ordering).
//
// Degraded paths that keep the chain live (never halt): an already-open FORMING
// epoch is not stacked, and a bonded set below min_committee_size opens no epoch
// (the prior ACTIVE key stays the encryption target).
func (k Keeper) MaybeOpenEpoch(ctx sdk.Context, params types.Params) {
	height := uint64(ctx.BlockHeight())
	if height == 0 || height%params.EpochLengthBlocks != 0 {
		return
	}
	if k.FormingEpochID(ctx) != 0 {
		// a DKG is still in flight; do not stack a second FORMING epoch.
		return
	}

	committee := k.snapshotCommittee(ctx)
	if uint32(len(committee)) < params.MinCommitteeSize {
		// too few validators to run a meaningful DKG — stay on the prior key.
		return
	}

	id := k.NextEpochID(ctx)
	k.setNextEpochID(ctx, id+1)

	n := len(committee)
	dealDeadline := height + params.DealWindowBlocks
	confirmDeadline := dealDeadline + params.ResponseWindowBlocks + params.ConfirmWindowBlocks

	e := types.Epoch{
		Id:              id,
		Status:          types.EPOCH_STATUS_FORMING,
		Committee:       committee,
		Threshold:       uint32(types.Threshold(n)),
		FormedHeight:    height,
		DealDeadline:    dealDeadline,
		ConfirmDeadline: confirmDeadline,
	}
	k.SetEpoch(ctx, e)
	k.setFormingEpochID(ctx, id)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeEpochForming,
		sdk.NewAttribute(types.AttributeKeyEpochID, strconv.FormatUint(id, 10)),
		sdk.NewAttribute(types.AttributeKeyCommitteeSize, strconv.Itoa(n)),
		sdk.NewAttribute(types.AttributeKeyThreshold, strconv.Itoa(types.Threshold(n))),
	))
}

// snapshotCommittee reads the bonded validator set and returns the DKG committee
// sorted by valoper address ascending (deterministic), with dkg_index = position
// in that order. GetBondedValidatorsByPower's power ordering is NOT used for the
// stored order precisely because ties in power are address-broken inconsistently
// across SDK versions; sorting by address here is the stable, version-independent
// committee identity.
func (k Keeper) snapshotCommittee(ctx sdk.Context) []types.CommitteeMember {
	vals, err := k.stakingKeeper.GetBondedValidatorsByPower(ctx)
	if err != nil {
		panic(err)
	}
	addrs := make([]string, 0, len(vals))
	for i := range vals {
		addrs = append(addrs, vals[i].OperatorAddress)
	}
	sort.Strings(addrs)
	out := make([]types.CommitteeMember, len(addrs))
	for i, a := range addrs {
		out[i] = types.CommitteeMember{DkgIndex: uint32(i), Valoper: a}
	}
	return out
}

// RetireDrained retires every DRAINING epoch that has no in-flight (sealed-but-
// unrevealed) ciphertext left tagged to it (PHASE1_DESIGN §2(b) drain-before-
// rotate; seam (a)). This is the "rotation release" gated on drain: a DRAINING
// epoch's key material is held only until its in-flight intents reach a TERMINAL
// Reveal outcome — OPEN-and-cleared or CANCELLED. A stuck session that CANCELs
// stops counting as in-flight, so it cannot stall retirement forever (the
// session-state half of the 1.4 CANCEL-as-drain-terminus precondition).
func (k Keeper) RetireDrained(ctx sdk.Context) {
	var toRetire []uint64
	k.IterateEpochs(ctx, func(e types.Epoch) bool {
		if e.Status == types.EPOCH_STATUS_DRAINING && !k.veil().HasInFlightForEpoch(ctx, e.Id) {
			toRetire = append(toRetire, e.Id)
		}
		return false
	})
	for _, id := range toRetire {
		e, ok := k.GetEpoch(ctx, id)
		if !ok {
			continue
		}
		e.Status = types.EPOCH_STATUS_RETIRED
		k.SetEpoch(ctx, e)
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeEpochRetired,
			sdk.NewAttribute(types.AttributeKeyEpochID, strconv.FormatUint(id, 10)),
		))
	}
}

// rotateTo makes newID the ACTIVE encryption target and moves the prior ACTIVE
// epoch to DRAINING (retained, still reveal-capable for its in-flight cts). The
// successor becomes the encryption target IMMEDIATELY on DKG success; the prior
// epoch is not dropped — it drains under its own retained shares and RetireDrained
// reclaims it once empty. Clients tag each ciphertext with its epoch id so the
// Reveal selects the matching share set across the overlap (the §2(b) model the
// 1.4 TestChurnDrainBeforeRotate crypto precondition rests on).
func (k Keeper) rotateTo(ctx sdk.Context, newID uint64) {
	prevID := k.CurrentEpochID(ctx)
	if prevID != 0 && prevID != newID {
		if prev, ok := k.GetEpoch(ctx, prevID); ok && prev.Status == types.EPOCH_STATUS_ACTIVE {
			prev.Status = types.EPOCH_STATUS_DRAINING
			k.SetEpoch(ctx, prev)
			ctx.EventManager().EmitEvent(sdk.NewEvent(
				types.EventTypeEpochRotated,
				sdk.NewAttribute(types.AttributeKeyPrevEpochID, strconv.FormatUint(prevID, 10)),
				sdk.NewAttribute(types.AttributeKeyEpochID, strconv.FormatUint(newID, 10)),
			))
		}
	}
	k.setCurrentEpochID(ctx, newID)
}
