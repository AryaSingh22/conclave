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

		// Conditioned (HASHLOCK/OPTIMISTIC) DataGrant legs and their paired
		// coin payment route into x/escrow (D-073 handoff) rather than settling
		// directly; the returned set marks the legs already consumed so the
		// normal loop skips them.
		handled, err := k.openConditionedEscrows(ctx, sessionID, m, resolve)
		if err != nil {
			return errorsmod.Wrapf(err, "match %d", mi)
		}

		for li := range m.Legs {
			if handled[li] {
				continue
			}
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

// applyLeg settles one directed leg into the cache context. Conditioned
// (HASHLOCK/OPTIMISTIC) legs and their paired payment are routed into x/escrow
// by openConditionedEscrows BEFORE this runs and never reach here; a
// conditioned leg arriving here is an internal invariant break (defense).
func (k Keeper) applyLeg(ctx sdk.Context, sessionID uint64, leg types.SettleLeg, resolve func(string) (sdk.AccAddress, error)) error {
	if leg.Condition != nil && leg.Condition.Condition != nil {
		return errorsmod.Wrap(types.ErrConcludeFailed, "conditioned leg reached direct settlement (should be escrow-routed)")
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
		// DATA GRANT (NONE condition) — authorized creation: a record-only
		// x/escrow entry; NO ledger balance moves or is minted. Authorized
		// because rule 3 bound the grant to the giver's offer and the
		// receiver's want (D-048). Conditioned DataGrants are handled earlier
		// (openConditionedEscrows) and never reach this case.
		return k.openEscrow(ctx, sessionID, leg, sdk.NewCoins(), resolve)

	default:
		return errorsmod.Wrap(types.ErrInvalidLeg, "leg has no asset")
	}
}

// buildEscrowRecord derives the deterministic escrow id and assembles the
// x/escrow record for a DataGrant leg. The id derivation (domainEscrow over
// session_id ‖ content_hash ‖ from‖to) is unchanged from the Phase-0 stub so
// ids are stable across the D-073 ownership handoff. payer = the data
// RECIPIENT (buyer, leg.ToSigil) who funds the escrow; payee = the data GIVER
// (seller, leg.FromSigil) who is paid on release — the conditioned-delivery
// direction (corrected from the pre-handoff stub, which had no funds so the
// assignment was inert).
func (k Keeper) buildEscrowRecord(sessionID uint64, leg types.SettleLeg, amount sdk.Coins) escrowtypes.Escrow {
	data := leg.GetData()
	idPayload := append(append([]byte{}, u64be(sessionID)...), data.ContentHash...)
	idPayload = append(idPayload, []byte(leg.FromSigil+leg.ToSigil)...)
	escrowID := deriveID(domainEscrow, phase0Chamber, idPayload)

	cond := escrowtypes.DeliveryCondition{} // NONE oneof
	if leg.Condition != nil {
		cond = *leg.Condition
	}
	return escrowtypes.Escrow{
		Id:         escrowID,
		SessionId:  sessionID,
		PayerSigil: leg.ToSigil,   // buyer funds the escrow
		PayeeSigil: leg.FromSigil, // seller is paid on release
		Amount:     amount,
		Condition:  cond,
		ChamberId:  phase0Chamber,
	}
}

// openEscrow writes a DataGrant escrow record through x/escrow (D-073: x/escrow
// is the writer; x/session no longer touches 0x09). x/escrow sets the lifecycle
// fields, moves any funds, and rebuilds its indexes. NONE-condition grants pass
// an empty amount (record-only); conditioned grants pass the buyer's payment
// and the committed content hash (see openConditionedEscrows).
//
// implements SPEC §6.4 "DataGrant legs instantiate x/escrow entries".
func (k Keeper) openEscrow(ctx sdk.Context, sessionID uint64, leg types.SettleLeg, payment sdk.Coins, resolve func(string) (sdk.AccAddress, error)) error {
	esc := k.buildEscrowRecord(sessionID, leg, payment)
	payerAddr, err := resolve(esc.PayerSigil)
	if err != nil {
		return err
	}
	payeeAddr, err := resolve(esc.PayeeSigil)
	if err != nil {
		return err
	}
	var contentHash []byte
	if leg.GetData() != nil {
		contentHash = leg.GetData().ContentHash
	}
	return k.escrowKeeper.CreateEscrow(ctx, esc, payerAddr, payeeAddr, contentHash)
}

// intentConditionForSigil loads the signed IntentPayload.condition of the
// match intent submitted by a sigil (the consent the seller actually signed,
// D-075). Returns ok=false if no intent in the match was submitted by that
// sigil.
func (k Keeper) intentConditionForSigil(ctx sdk.Context, m types.Match, sigil string) (*escrowtypes.DeliveryCondition, bool, error) {
	for _, id := range m.IntentIds {
		rec, payload, err := k.veil().GetOpenIntent(ctx, phase0Chamber, id)
		if err != nil {
			return nil, false, errorsmod.Wrapf(err, "loading intent %s", id)
		}
		if rec.Intent.SenderSigil == sigil {
			return payload.Condition, true, nil
		}
	}
	return nil, false, nil
}

// openConditionedEscrows routes each HASHLOCK/OPTIMISTIC DataGrant leg in a
// match (and its paired coin payment) into x/escrow, returning the set of leg
// indexes consumed so applyMatches skips them. validateSolution has already
// proven the conditioned-match shape (one DataGrant seller→buyer plus coin
// payment legs buyer→seller, two sigils — D-073/ErrConditionUnsupportedMatch);
// this re-derives the payment defensively. The buyer's payment is escrowed
// (released to the seller on a valid release, refunded to the buyer on timeout/
// lost dispute); the seller bond is posted inside CreateEscrow.
func (k Keeper) openConditionedEscrows(ctx sdk.Context, sessionID uint64, m types.Match, resolve func(string) (sdk.AccAddress, error)) (map[int]bool, error) {
	handled := make(map[int]bool)
	for li := range m.Legs {
		leg := m.Legs[li]
		if leg.Condition == nil || leg.Condition.Condition == nil {
			continue
		}
		if leg.GetData() == nil {
			return nil, errorsmod.Wrapf(types.ErrConditionUnsupportedMatch, "match leg %d: conditioned leg is not a DataGrant", li)
		}
		seller, buyer := leg.FromSigil, leg.ToSigil

		// CONSENT (D-075), enforced again at the settlement boundary so no path
		// to escrow creation can bind a seller to a condition it did not sign,
		// even one that skips solution validation: the seller's signed
		// IntentPayload.condition must equal the (unsigned, solver-built)
		// SettleLeg.condition.
		signed, ok, err := k.intentConditionForSigil(ctx, m, seller)
		if err != nil {
			return nil, err
		}
		if !ok || !conditionEqual(signed, leg.Condition) {
			return nil, errorsmod.Wrapf(types.ErrConditionNotConsented, "match leg %d: seller %s did not sign this condition", li, seller)
		}
		// ZK_DELIVERY (D-076): the BUYER must ALSO have signed the exact
		// condition — the authoritative buyer-side mirror of the seller guard,
		// since ZK release is seller-driven on a schema not otherwise tied to the
		// buyer's want (see validateConditionConsent). HASHLOCK/OPTIMISTIC buyer
		// consent stays D-075-FW-1.
		if leg.Condition.GetZkDelivery() != nil {
			bsigned, bok, err := k.intentConditionForSigil(ctx, m, buyer)
			if err != nil {
				return nil, err
			}
			if !bok || !conditionEqual(bsigned, leg.Condition) {
				return nil, errorsmod.Wrapf(types.ErrConditionNotConsented, "match leg %d: buyer %s did not sign this ZK condition", li, buyer)
			}
		}

		// Collect the payment: every coin leg buyer→seller in this match.
		payment := sdk.NewCoins()
		for pj := range m.Legs {
			if pj == li {
				continue
			}
			p := m.Legs[pj]
			if p.GetCoin() != nil && p.FromSigil == buyer && p.ToSigil == seller {
				payment = payment.Add(*p.GetCoin())
				handled[pj] = true
			}
		}

		if err := k.openEscrow(ctx, sessionID, leg, payment, resolve); err != nil {
			return nil, err
		}
		handled[li] = true
	}
	return handled, nil
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
