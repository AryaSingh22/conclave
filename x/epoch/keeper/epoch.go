package keeper

import (
	"sort"
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/epoch/types"
)

// RegisterDeadline / dealDeadline / responseDeadline / justifyDeadline are the
// five-window boundaries (register → deal → response → justify → confirm),
// derived deterministically from the epoch's stored formed_height / deal_deadline
// / confirm_deadline and the params (Task 1.7, D-070). No new Epoch fields are
// stored: deal_deadline already spans [register+deal] and confirm_deadline spans
// the full DKG, so the intermediate boundaries recompute from params (the same
// recompute-from-params pattern the response window already used in 1.5).
func registerDeadline(e types.Epoch, p types.Params) uint64 {
	return e.FormedHeight + p.RegisterWindowBlocks
}
func responseDeadline(e types.Epoch, p types.Params) uint64 {
	return e.DealDeadline + p.ResponseWindowBlocks
}
func justifyDeadline(e types.Epoch, p types.Params) uint64 {
	return responseDeadline(e, p) + p.JustifyWindowBlocks
}

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

	candidates := k.snapshotCandidates(ctx)
	if uint32(len(candidates)) < params.MinCommitteeSize {
		// too few bonded validators to run a meaningful DKG — stay on the prior key.
		return
	}

	id := k.NextEpochID(ctx)
	k.setNextEpochID(ctx, id+1)

	// The committee is NOT fixed yet (Task 1.7 A2, D-070): it is finalized at the
	// register-window close from the validators that register a DKG long-term key.
	// Here we only snapshot the bonded set as the candidate pool a registration is
	// authorized against, and open the register window first. deal_deadline spans
	// [register+deal]; confirm_deadline spans the full five-window DKG.
	dealDeadline := height + params.RegisterWindowBlocks + params.DealWindowBlocks
	confirmDeadline := height + params.TotalDKGWindowBlocks()

	e := types.Epoch{
		Id:              id,
		Status:          types.EPOCH_STATUS_FORMING,
		Committee:       nil, // assigned at register-window close (FinalizeCommittee)
		Threshold:       0,   // assigned with the committee
		FormedHeight:    height,
		DealDeadline:    dealDeadline,
		ConfirmDeadline: confirmDeadline,
	}
	k.SetEpoch(ctx, e)
	k.setFormingEpochID(ctx, id)
	for _, v := range candidates {
		k.setCandidate(ctx, id, v)
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeEpochForming,
		sdk.NewAttribute(types.AttributeKeyEpochID, strconv.FormatUint(id, 10)),
		sdk.NewAttribute(types.AttributeKeyCommitteeSize, strconv.Itoa(len(candidates))),
	))
}

// snapshotCandidates reads the bonded validator set and returns the candidate
// valoper addresses sorted ascending (deterministic). GetBondedValidatorsByPower's
// power ordering is NOT used for the stored order precisely because ties in power
// are address-broken inconsistently across SDK versions; sorting by address is
// the stable, version-independent identity. The DKG COMMITTEE is the subset of
// these that register a long-term key (FinalizeCommittee), not the full set.
func (k Keeper) snapshotCandidates(ctx sdk.Context) []string {
	vals, err := k.stakingKeeper.GetBondedValidatorsByPower(ctx)
	if err != nil {
		panic(err)
	}
	addrs := make([]string, 0, len(vals))
	for i := range vals {
		addrs = append(addrs, vals[i].OperatorAddress)
	}
	sort.Strings(addrs)
	return addrs
}

// FinalizeCommittee runs once at the register-window close of a FORMING epoch
// (Task 1.7 A2, D-070): it builds the DKG committee from the validators that
// registered a long-term key, sorted by valoper ascending with dkg_index = dense
// position in that order, and sets the epoch's threshold = floor(2m/3)+1 for the
// registered size m. A candidate that did NOT register is simply excluded — it
// cannot veto the epoch (the no-single-point-veto property the registration round
// must preserve, consistent with the B2 justification guarantee). If fewer than
// min_committee_size validators registered, the epoch FAILEDs (the prior key
// stays). Deterministic: registrations iterate in sorted-valoper order.
func (k Keeper) FinalizeCommittee(ctx sdk.Context, e types.Epoch, params types.Params) {
	k.setCommitteeFinalized(ctx, e.Id)

	regs := k.Registrations(ctx, e.Id)
	if uint32(len(regs)) < params.MinCommitteeSize {
		e.Status = types.EPOCH_STATUS_FAILED
		k.SetEpoch(ctx, e)
		k.setFormingEpochID(ctx, 0)
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeEpochFailed,
			sdk.NewAttribute(types.AttributeKeyEpochID, strconv.FormatUint(e.Id, 10)),
			sdk.NewAttribute(types.AttributeKeyFailureReason, "too few DKG-key registrations"),
		))
		k.gcTranscript(ctx, e.Id)
		k.clearCandidates(ctx, e.Id)
		return
	}

	committee := make([]types.CommitteeMember, len(regs))
	for i, r := range regs {
		committee[i] = types.CommitteeMember{DkgIndex: uint32(i), Valoper: r.Valoper}
	}
	n := len(committee)
	e.Committee = committee
	e.Threshold = uint32(types.Threshold(n))
	k.SetEpoch(ctx, e)
	k.clearCandidates(ctx, e.Id)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeEpochForming,
		sdk.NewAttribute(types.AttributeKeyEpochID, strconv.FormatUint(e.Id, 10)),
		sdk.NewAttribute(types.AttributeKeyCommitteeSize, strconv.Itoa(n)),
		sdk.NewAttribute(types.AttributeKeyThreshold, strconv.Itoa(types.Threshold(n))),
	))
}

// --- candidate / finalized-flag store helpers (Task 1.7, D-070) ---

func (k Keeper) setCandidate(ctx sdk.Context, epochID uint64, valoper string) {
	if err := k.storeService.OpenKVStore(ctx).Set(types.CandidateKey(epochID, valoper), []byte{1}); err != nil {
		panic(err)
	}
}

// IsCandidate reports whether valoper was a bonded candidate when the epoch
// opened FORMING (the set a registration is authorized against).
func (k Keeper) IsCandidate(ctx sdk.Context, epochID uint64, valoper string) bool {
	has, _ := k.storeService.OpenKVStore(ctx).Has(types.CandidateKey(epochID, valoper))
	return has
}

func (k Keeper) clearCandidates(ctx sdk.Context, epochID uint64) {
	k.deletePrefix(ctx, types.BundleIterPrefix(types.CandidatePrefix, epochID))
}

func (k Keeper) setCommitteeFinalized(ctx sdk.Context, epochID uint64) {
	if err := k.storeService.OpenKVStore(ctx).Set(types.FinalizedKey(epochID), []byte{1}); err != nil {
		panic(err)
	}
}

// IsCommitteeFinalized reports whether the register-window close already assigned
// this epoch's committee (so FinalizeCommittee runs exactly once).
func (k Keeper) IsCommitteeFinalized(ctx sdk.Context, epochID uint64) bool {
	has, _ := k.storeService.OpenKVStore(ctx).Has(types.FinalizedKey(epochID))
	return has
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
