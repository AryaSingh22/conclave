package keeper

import (
	"strconv"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/session/types"
)

// RunEndBlock executes the SPEC §6.4 x/session EndBlock pipeline in the mandated
// order (D-038): (1) advance session states, (3) scoring + (4) conclude winner
// or fallback, (5) rollover sweep, then the solver-unbond sweep. Step (2), the
// Reveal hand-off intake, stays in x/veil's BeginBlock (feeding IntakeRevealed),
// and steps (6)/(7) — the expiry sweep and its mandate window refunds — run in
// x/veil's EndBlock, which app wiring registers AFTER this module. So the whole
// §6.4 step order holds across the two modules without reordering either.
func (k Keeper) RunEndBlock(ctx sdk.Context) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	// (1) advance states: open any matured carryover FORMING session even if no
	// new intents revealed into it this cycle (liveness — a rollover never stalls).
	if err := k.openMatureFormingSessions(ctx, params); err != nil {
		return err
	}
	// (3)(4)(5) score → conclude/fallback → rollover, per closing auction.
	if err := k.processClosingAuctions(ctx, params); err != nil {
		return err
	}
	// solver-unbond sweep (Half A).
	return k.SweepUnbondedSolvers(ctx)
}

// sessionIDsInState collects the ids of every session in one lifecycle state,
// in ascending id order, materialized before any mutation (no store write
// during iteration; §11.6 deterministic).
func (k Keeper) sessionIDsInState(ctx sdk.Context, state types.SessionState) ([]uint64, error) {
	store := k.storeService.OpenKVStore(ctx)
	prefix := types.SessionStateIdxIterPrefix(state)
	it, err := store.Iterator(prefix, types.PrefixEnd(prefix))
	if err != nil {
		return nil, err
	}
	defer it.Close()
	var ids []uint64
	for ; it.Valid(); it.Next() {
		ids = append(ids, types.SessionIDFromStateIdxKey(append([]byte{}, it.Key()...)))
	}
	return ids, nil
}

// openMatureFormingSessions transitions any FORMING carryover session whose
// boundary+reveal has passed (id < current height) to AUCTION, so rolled-over
// intents always get a fresh auction even in a cycle with no new reveals. A
// FORMING session whose boundary has not arrived yet is left untouched.
func (k Keeper) openMatureFormingSessions(ctx sdk.Context, params types.Params) error {
	height := uint64(ctx.BlockHeight())
	ids, err := k.sessionIDsInState(ctx, types.SESSION_STATE_FORMING)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if id >= height {
			continue
		}
		s, err := k.GetSession(ctx, phase0Chamber, id)
		if err != nil {
			return err
		}
		s.AuctionEndHeight = height + params.SolutionWindowBlocks - 1
		if err := k.transitionSession(ctx, &s, types.SESSION_STATE_AUCTION); err != nil {
			return err
		}
	}
	return nil
}

// processClosingAuctions drives every AUCTION session whose solution window has
// closed (auction_end_height <= height) through scoring → conclude-winner-or-
// fallback → rollover, in ascending id order (deterministic).
func (k Keeper) processClosingAuctions(ctx sdk.Context, params types.Params) error {
	height := uint64(ctx.BlockHeight())
	ids, err := k.sessionIDsInState(ctx, types.SESSION_STATE_AUCTION)
	if err != nil {
		return err
	}
	for _, id := range ids {
		s, err := k.GetSession(ctx, phase0Chamber, id)
		if err != nil {
			return err
		}
		if s.AuctionEndHeight > height {
			continue // window still open
		}
		if err := k.advanceAuction(ctx, s, params); err != nil {
			return err
		}
	}
	return nil
}

// advanceAuction scores the session's solutions, concludes the winner (or runs
// the fallback when there is no winner or the winner's Conclude fails), then
// rolls the unmatched ttl-ok intents into the next session.
func (k Keeper) advanceAuction(ctx sdk.Context, session types.Session, params types.Params) error {
	winner, ok, err := k.selectWinner(ctx, session.Id)
	if err != nil {
		return err
	}

	if ok {
		cErr := k.concludeSolution(ctx, session, winner)
		switch {
		case cErr == nil:
			// settled; session is CLOSED.
		case errorsmod.IsOf(cErr, types.ErrConcludeFailed):
			// concludeSolution already slashed the solver and moved the session to
			// FALLBACK on the parent ctx; run the in-protocol matcher.
			fresh, gErr := k.GetSession(ctx, phase0Chamber, session.Id)
			if gErr != nil {
				return gErr
			}
			if err := k.runFallback(ctx, fresh, params); err != nil {
				return err
			}
		default:
			return cErr
		}
	} else {
		// zero valid solutions → FALLBACK (SPEC §6.4).
		if err := k.transitionSession(ctx, &session, types.SESSION_STATE_FALLBACK); err != nil {
			return err
		}
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeSessionFallback,
			sdk.NewAttribute(types.AttributeKeySessionID, strconv.FormatUint(session.Id, 10)),
			sdk.NewAttribute(types.AttributeKeyReason, "no_solutions"),
		))
		fresh, gErr := k.GetSession(ctx, phase0Chamber, session.Id)
		if gErr != nil {
			return gErr
		}
		if err := k.runFallback(ctx, fresh, params); err != nil {
			return err
		}
	}

	// (5) rollover sweep: the now-closed session's unmatched ttl-ok intents.
	closed, err := k.GetSession(ctx, phase0Chamber, session.Id)
	if err != nil {
		return err
	}
	return k.rolloverSession(ctx, closed, params)
}

// runFallback settles the SPEC §6.4 in-protocol fallback matcher's output for a
// session in FALLBACK and closes it. A fallback that clears nothing (no fillable
// pair) still closes the session — its intents roll or expire. A fallback
// Conclude failure does NOT re-fallback; the session closes and the intents roll
// (liveness: the chain never stalls on a settlement failure).
//
// A fallback that DOES clear persists its matches as a Solution record and
// records it as the session's winning solution (D-086): the §6.6 receipt's
// terms_hash commits to the settled Match bytes, and without a stored
// Solution that pre-image would be unfetchable for fallback settlements — the
// Session → winning_solution_id → Solutions chain (the fill-back evidence
// path, docs/edge/7683_SURFACE.md §6) must be UNIFORM across both clearing
// paths. The stored matches are the exact values the receipts hashed, so the
// queried pre-image recomputes to the committed terms_hash.
func (k Keeper) runFallback(ctx sdk.Context, session types.Session, params types.Params) error {
	matches, err := k.buildFallbackMatches(ctx, session, params)
	if err != nil {
		return err
	}
	if len(matches) > 0 {
		if cErr := k.concludeInner(ctx, session.Id, matches, nil); cErr != nil {
			ctx.EventManager().EmitEvent(sdk.NewEvent(
				types.EventTypeSessionFallback,
				sdk.NewAttribute(types.AttributeKeySessionID, strconv.FormatUint(session.Id, 10)),
				sdk.NewAttribute(types.AttributeKeyReason, "fallback_conclude_failed"),
			))
		} else {
			// The id derives from the session alone — deterministic consensus
			// state (§11.6), reproducible on every node; domain-separated from
			// tx-hash-derived solver ids (keeper.go domainSolution). Only one
			// fallback ever runs per session (FALLBACK is entered once and the
			// session closes below), so the id is unique.
			sol := types.Solution{
				Id:        deriveID(domainSolution, phase0Chamber, u64be(session.Id)),
				SessionId: session.Id,
				Solver:    "", // in-consensus matcher: no solver, no payout
				Matches:   matches,
				Score:     math.LegacyZeroDec(),
			}
			if err := k.setSolution(ctx, sol); err != nil {
				return err
			}
			session.WinningSolutionId = sol.Id
			ctx.EventManager().EmitEvent(sdk.NewEvent(
				types.EventTypeSessionConcluded,
				sdk.NewAttribute(types.AttributeKeySessionID, strconv.FormatUint(session.Id, 10)),
				sdk.NewAttribute(types.AttributeKeySolutionID, sol.Id),
				sdk.NewAttribute(types.AttributeKeyReason, "fallback"),
			))
		}
	}
	return k.transitionSession(ctx, &session, types.SESSION_STATE_CLOSED)
}
