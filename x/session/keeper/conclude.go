package keeper

import (
	"strconv"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	escrowtypes "github.com/AryaSingh22/conclave/x/escrow/types"
	"github.com/AryaSingh22/conclave/x/session/types"
)

// Conclude is the SPEC §6.4 atomic settlement: it applies an entire transfer
// graph or nothing. The whole graph runs inside a single ctx.CacheContext();
// the cache is committed with one write() only if every leg, the Half-B economic
// layer (skim, receipts, reputation), AND every intent CONCLUDED-refund succeed.
// Any failure returns an error with ZERO state change — no leg, skim, receipt,
// bond refund, or window refund persists. This is the all-or-nothing guarantee
// (also exposed as MsgConclude for CVM/testing). The direct entry point takes no
// solver: it skims to the pool and pays no fee (winner payout flows through
// concludeWinner with the §6.4 winning solver).
func (k Keeper) Conclude(ctx sdk.Context, sessionID uint64, matches []types.Match) error {
	return k.concludeInner(ctx, sessionID, matches, nil)
}

// concludeInner is the cache-wrapped settlement shared by the direct Conclude
// and the §6.4 winner path. The economics are SNAPSHOTTED before applyMatches
// concludes the intents (their OPEN payloads carry the give/want limits surplus
// is measured against), settled, then the skim/receipts/reputation/payout run —
// all under the one cache so the whole block is atomic (§6.4).
func (k Keeper) concludeInner(ctx sdk.Context, sessionID uint64, matches []types.Match, solver *sdk.AccAddress) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	cctx, write := ctx.CacheContext()

	plans, err := k.planMatches(cctx, matches)
	if err != nil {
		return err
	}
	if err := k.applyMatches(cctx, sessionID, matches); err != nil {
		return err // discard the cache: nothing changed
	}
	if err := k.applyEconomics(cctx, sessionID, plans, solver, params); err != nil {
		return err // discard the cache: skim/payout failure rolls back the graph too
	}
	write()
	return nil
}

// applyMatches executes every match's legs and marks its intents CONCLUDED.
// Coin legs route through the module escrow account (it nets zero per match);
// ServiceVoucher legs mint svc/{schema} to the receiver; DataGrant legs
// instantiate an x/escrow stub entry (Phase-0: record only — escrow release
// logic is Phase 2, SPEC §6.5). Intent CONCLUDED + bond/window refunds run via
// x/veil so they share this cache (D-041 CONCLUDED row).
func (k Keeper) applyMatches(ctx sdk.Context, sessionID uint64, matches []types.Match) error {
	for mi := range matches {
		m := matches[mi]
		addrCache := make(map[string]sdk.AccAddress)
		resolve := func(sigil string) (sdk.AccAddress, error) {
			if a, ok := addrCache[sigil]; ok {
				return a, nil
			}
			a, err := k.sigilKeeper.ControllerAddress(ctx, phase0Chamber, sigil)
			if err != nil {
				return nil, errorsmod.Wrapf(types.ErrInvalidLeg, "sigil %s: %s", sigil, err)
			}
			addrCache[sigil] = a
			return a, nil
		}

		for li := range m.Legs {
			leg := m.Legs[li]
			if err := k.applyLeg(ctx, sessionID, leg, resolve); err != nil {
				return errorsmod.Wrapf(err, "match %d leg %d", mi, li)
			}
		}

		for _, id := range m.IntentIds {
			if err := k.veil().ConcludeIntent(ctx, id); err != nil {
				return errorsmod.Wrapf(err, "concluding intent %s", id)
			}
		}

		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeMatchSettled,
			sdk.NewAttribute(types.AttributeKeySessionID, strconv.FormatUint(sessionID, 10)),
		))
	}
	return nil
}

// applyLeg settles one directed leg into the cache context.
func (k Keeper) applyLeg(ctx sdk.Context, sessionID uint64, leg types.SettleLeg, resolve func(string) (sdk.AccAddress, error)) error {
	// Phase 0: NONE conditions only (also enforced at validation).
	if leg.Condition != nil && leg.Condition.Condition != nil {
		return errorsmod.Wrap(types.ErrConditionsNotEnabled, "settlement leg carries a delivery condition")
	}

	switch {
	case leg.GetCoin() != nil:
		// COIN — value conservation: debit an EXISTING balance and route it
		// through the module account (single audited path; nets zero per match,
		// SPEC §6.4). No coin is created.
		from, err := resolve(leg.FromSigil)
		if err != nil {
			return err
		}
		to, err := resolve(leg.ToSigil)
		if err != nil {
			return err
		}
		coins := sdk.NewCoins(*leg.GetCoin())
		if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, from, types.ModuleName, coins); err != nil {
			return err
		}
		return k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, to, coins)

	case leg.GetSvc() != nil:
		// SERVICE VOUCHER — authorized mint: svc/{schema} is MINTED to the
		// receiver (the giver holds no such token and is NOT debited; supply
		// rises by design). The mint is authorized because rule 3 bound the
		// giver's -units to its own give-leg svc offer (D-048).
		to, err := resolve(leg.ToSigil)
		if err != nil {
			return err
		}
		svc := leg.GetSvc()
		minted := sdk.NewCoins(sdk.NewCoin("svc/"+svc.Schema, svc.Units))
		if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, minted); err != nil {
			return err
		}
		return k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, to, minted)

	case leg.GetData() != nil:
		// DATA GRANT — authorized creation: instantiate an escrow stub; NO
		// ledger balance moves or is minted. Authorized because rule 3 bound the
		// grant to the giver's offer and the receiver's want (D-048).
		return k.openEscrowStub(ctx, sessionID, leg)

	default:
		return errorsmod.Wrap(types.ErrInvalidLeg, "leg has no asset")
	}
}

// openEscrowStub writes a Phase-0 x/escrow entry for a DataGrant leg (SPEC
// §6.4: "DataGrant legs instantiate x/escrow entries instead of transferring").
// Release/dispute logic is Phase 2; this records the conditioned delivery.
// CONCLAVE-TODO(phase-2): x/escrow takes ownership of these records and adds
// HASHLOCK/OPTIMISTIC/ZK_DELIVERY resolution.
func (k Keeper) openEscrowStub(ctx sdk.Context, sessionID uint64, leg types.SettleLeg) error {
	data := leg.GetData()
	idPayload := append(append([]byte{}, u64be(sessionID)...), data.ContentHash...)
	idPayload = append(idPayload, []byte(leg.FromSigil+leg.ToSigil)...)
	escrowID := deriveID(domainEscrow, phase0Chamber, idPayload)
	idBz, err := types.IDBytes(escrowID)
	if err != nil {
		return err
	}

	esc := escrowtypes.Escrow{
		Id:            escrowID,
		SessionId:     sessionID,
		PayerSigil:    leg.FromSigil,
		PayeeSigil:    leg.ToSigil,
		Condition:     escrowtypes.DeliveryCondition{}, // NONE oneof
		State:         escrowtypes.ESCROW_STATE_ACTIVE,
		CreatedHeight: uint64(ctx.BlockHeight()),
		ChamberId:     phase0Chamber,
	}
	bz, err := k.cdc.Marshal(&esc)
	if err != nil {
		return err
	}
	if err := k.storeService.OpenKVStore(ctx).Set(types.EscrowKey(phase0Chamber, idBz), bz); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeEscrowOpened,
		sdk.NewAttribute(types.AttributeKeyEscrowID, escrowID),
		sdk.NewAttribute(types.AttributeKeySessionID, strconv.FormatUint(sessionID, 10)),
	))
	return nil
}

// concludeSolution drives the SPEC §6.4 CONCLUDING phase for a chosen solution.
// It walks the session AUCTION→SCORING→CONCLUDING, runs the atomic Conclude,
// and on success closes the session (CLOSED), or on ANY leg failure slashes the
// solver 5% and falls through to FALLBACK. Winner SELECTION is Half B; this is
// the conclude+slash+fallback mechanism §6.4 mandates.
//
// Guard 3: the slash and the FALLBACK transition are applied on the PARENT ctx
// (here), OUTSIDE Conclude's cache, so they persist even though the failed
// graph's cache is discarded.
func (k Keeper) concludeSolution(ctx sdk.Context, session types.Session, sol types.Solution) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	if err := k.transitionSession(ctx, &session, types.SESSION_STATE_SCORING); err != nil {
		return err
	}
	if err := k.transitionSession(ctx, &session, types.SESSION_STATE_CONCLUDING); err != nil {
		return err
	}

	winner, wErr := sdk.AccAddressFromBech32(sol.Solver)
	if wErr != nil {
		return wErr
	}
	if err := k.concludeInner(ctx, session.Id, sol.Matches, &winner); err != nil {
		// the graph's cache was discarded — zero leg/refund change. Slash the
		// solver and fall through to FALLBACK on the parent ctx (persists).
		if solverAddr, aErr := sdk.AccAddressFromBech32(sol.Solver); aErr == nil {
			if solver, gErr := k.GetSolver(ctx, phase0Chamber, solverAddr); gErr == nil {
				if sErr := k.slashSolver(ctx, &solver, params.InvalidSolutionSlashBps, "conclude_failed"); sErr != nil {
					return sErr
				}
			}
		}
		if tErr := k.transitionSession(ctx, &session, types.SESSION_STATE_FALLBACK); tErr != nil {
			return tErr
		}
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeSessionFallback,
			sdk.NewAttribute(types.AttributeKeySessionID, strconv.FormatUint(session.Id, 10)),
			sdk.NewAttribute(types.AttributeKeyReason, "conclude_failed"),
		))
		return errorsmod.Wrapf(types.ErrConcludeFailed, "%s", err)
	}

	session.WinningSolutionId = sol.Id
	if err := k.transitionSession(ctx, &session, types.SESSION_STATE_CLOSED); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeSessionConcluded,
		sdk.NewAttribute(types.AttributeKeySessionID, strconv.FormatUint(session.Id, 10)),
		sdk.NewAttribute(types.AttributeKeySolutionID, sol.Id),
	))
	return nil
}
