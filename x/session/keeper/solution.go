package keeper

import (
	"encoding/hex"
	"errors"
	"sort"
	"strconv"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// matchIntent bundles an OPEN intent's record, decoded payload, and sigil for
// solution validation.
type matchIntent struct {
	rec     veiltypes.IntentRecord
	payload veiltypes.IntentPayload
	sigil   string
}

// setSolution persists a proposed solution.
func (k Keeper) setSolution(ctx sdk.Context, sol types.Solution) error {
	idBz, err := types.IDBytes(sol.Id)
	if err != nil {
		return err
	}
	bz, err := k.cdc.Marshal(&sol)
	if err != nil {
		return err
	}
	return k.storeService.OpenKVStore(ctx).Set(types.SolutionKey(phase0Chamber, sol.SessionId, idBz), bz)
}

// SolutionsForSession returns every stored solution of a session, in id order
// (deterministic, §11.6 — scoring iterates this in Half B).
func (k Keeper) SolutionsForSession(ctx sdk.Context, sessionID uint64) ([]types.Solution, error) {
	store := k.storeService.OpenKVStore(ctx)
	prefix := types.SolutionIterPrefix(phase0Chamber, sessionID)
	it, err := store.Iterator(prefix, types.PrefixEnd(prefix))
	if err != nil {
		return nil, err
	}
	defer it.Close()

	var sols []types.Solution
	for ; it.Valid(); it.Next() {
		var sol types.Solution
		if err := k.cdc.Unmarshal(it.Value(), &sol); err != nil {
			return nil, err
		}
		sols = append(sols, sol)
	}
	return sols, nil
}

// ProposeSolution implements SPEC §6.4 MsgProposeSolution. The solver must be
// ACTIVE and the session in AUCTION. A second solution from the same solver is
// equivocation (20% slash); an invalid solution is slashed 5%; both reject.
// On success the solution is stored (scoring runs in Half B's EndBlock).
//
// Slashing is applied on the passed ctx so it persists even though the call
// returns an error (Guard 3 analogue — there is no cache here; the slash
// commits with the tx's failure state via the msg server).
func (k Keeper) ProposeSolution(ctx sdk.Context, solver sdk.AccAddress, sessionID uint64, matches []types.Match, solutionID string) error {
	s, err := k.GetSolver(ctx, phase0Chamber, solver)
	if err != nil {
		return err
	}
	if s.Status != types.SOLVER_STATUS_ACTIVE {
		return errorsmod.Wrapf(types.ErrSolverNotActive, "solver %s", solver)
	}

	session, err := k.GetSession(ctx, phase0Chamber, sessionID)
	if err != nil {
		return err
	}
	if session.State != types.SESSION_STATE_AUCTION {
		return errorsmod.Wrapf(types.ErrSessionNotInAuction, "session %d is %s", sessionID, session.State)
	}

	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}

	existing, err := k.SolutionsForSession(ctx, sessionID)
	if err != nil {
		return err
	}
	for _, sol := range existing {
		if sol.Solver == solver.String() {
			// equivocation: two solutions for one session = 20% slash.
			if err := k.slashSolver(ctx, &s, params.EquivocationSlashBps, "equivocation"); err != nil {
				return err
			}
			return errorsmod.Wrapf(types.ErrEquivocation, "solver %s session %d", solver, sessionID)
		}
	}
	if uint32(len(existing)) >= params.MaxSolutionsPerSession {
		return errorsmod.Wrapf(types.ErrTooManySolutions, "session %d at cap %d", sessionID, params.MaxSolutionsPerSession)
	}

	if err := k.validateSolution(ctx, session, matches, params); err != nil {
		// invalid solution: 5% slash, reject (SPEC §6.4). Join so both the
		// ErrInvalidSolution sentinel and the specific rule error stay in the
		// chain (errorsmod.Wrap with %s would flatten the inner sentinel).
		if slashErr := k.slashSolver(ctx, &s, params.InvalidSolutionSlashBps, "invalid_solution"); slashErr != nil {
			return slashErr
		}
		return errors.Join(types.ErrInvalidSolution, err)
	}

	sol := types.Solution{
		Id:        solutionID,
		SessionId: sessionID,
		Solver:    solver.String(),
		Matches:   matches,
		Score:     math.LegacyZeroDec(),
	}
	if err := k.setSolution(ctx, sol); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeSolutionProposed,
		sdk.NewAttribute(types.AttributeKeySolver, solver.String()),
		sdk.NewAttribute(types.AttributeKeySessionID, strconv.FormatUint(sessionID, 10)),
		sdk.NewAttribute(types.AttributeKeySolutionID, solutionID),
	))
	return nil
}

// validateSolution runs the SPEC §6.4 validation (all six rules must hold):
//  1. every referenced intent OPEN in this session; no intent in two matches;
//     distinct sender_sigil per match (Decision D: per-intent limit attribution
//     — legs name sigils and rule 3 is per-intent, so a sigil must map to one
//     intent in the match; CONCLAVE-TODO(phase-2): allow multi-intent-per-sigil
//     via intent-tagged legs so one agent can clear two intents in a batch).
//  2. conservation (CheckConservation) + legs reference only participants.
//  3. limit satisfaction at the stated quantities (partial only if allow_partial).
//  4. uniform price: one price per asset pair within a match.
//  5. counterparty class: each party's predicate holds against every other
//     party's Sigil state (resolves the 0.4 CONCLAVE-TODO).
//  6. graph size ≤ max_graph_legs; NONE conditions only (checked in rule 2).
func (k Keeper) validateSolution(ctx sdk.Context, session types.Session, matches []types.Match, params types.Params) error {
	totalLegs := 0
	for i := range matches {
		totalLegs += len(matches[i].Legs)
	}
	if uint32(totalLegs) > params.MaxGraphLegs {
		return errorsmod.Wrapf(types.ErrGraphTooLarge, "%d legs > max %d", totalLegs, params.MaxGraphLegs)
	}

	openSet := make(map[string]bool, len(session.IntentIds))
	for _, id := range session.IntentIds {
		openSet[id] = true
	}
	usedIntents := make(map[string]bool)

	for mi := range matches {
		m := matches[mi]

		// rule 1: gather intents, enforce OPEN-in-session, no reuse, distinct sigil.
		byID := make(map[string]matchIntent, len(m.IntentIds))
		participants := make(map[string]bool, len(m.IntentIds))
		for _, id := range m.IntentIds {
			if !openSet[id] {
				return errorsmod.Wrapf(types.ErrIntentNotOpen, "intent %s not in session %d", id, session.Id)
			}
			if usedIntents[id] {
				return errorsmod.Wrapf(types.ErrIntentReused, "intent %s", id)
			}
			usedIntents[id] = true

			rec, payload, err := k.veil().GetOpenIntent(ctx, phase0Chamber, id)
			if err != nil {
				return errorsmod.Wrapf(types.ErrIntentNotOpen, "intent %s: %s", id, err)
			}
			sigil := rec.Intent.SenderSigil
			if participants[sigil] {
				return errorsmod.Wrapf(types.ErrDuplicateSigil, "sigil %s appears twice in match %d", sigil, mi)
			}
			byID[id] = matchIntent{rec: rec, payload: payload, sigil: sigil}
			participants[sigil] = true
		}

		// rule 2: conservation (also rule 6's NONE-condition check).
		flows, err := CheckConservation([]types.Match{m})
		if err != nil {
			return err
		}
		// legs must move assets only between match participants (no leak to a
		// non-participant — closes the conservation gap the pure checker
		// cannot see).
		for li := range m.Legs {
			if !participants[m.Legs[li].FromSigil] || !participants[m.Legs[li].ToSigil] {
				return errorsmod.Wrapf(types.ErrInvalidLeg, "match %d leg %d references a non-participant", mi, li)
			}
		}

		// rule 4: uniform price.
		if err := checkUniformPrice(m.ClearingPrices); err != nil {
			return err
		}

		// rules 3 and 5, per intent in fixed (m.IntentIds) order.
		for _, idA := range m.IntentIds {
			a := byID[idA]
			// rule 3: limit satisfaction.
			if err := checkLimits(a, flows[a.sigil]); err != nil {
				return err
			}
			// rule 5: counterparty class against every other participant.
			if a.payload.CounterpartyClass != "" {
				for _, idB := range m.IntentIds {
					b := byID[idB]
					if a.sigil == b.sigil {
						continue
					}
					if !k.counterpartySatisfied(ctx, a.payload.CounterpartyClass, b.sigil) {
						return errorsmod.Wrapf(types.ErrCounterpartyClass,
							"sigil %s predicate %q rejects %s", a.sigil, a.payload.CounterpartyClass, b.sigil)
					}
				}
			}
		}

		// rules 3/4, second half: every leg amount must obey the stated clearing
		// prices (checked AFTER limits so a cap/floor breach still surfaces as
		// ErrLimitViolation, not a price residual).
		if err := CheckPriceBinding(m, flows); err != nil {
			return err
		}
	}
	return nil
}

// CheckPriceBinding enforces the binding half of SPEC §6.4 rules 3/4: every leg
// amount must clear at the match's stated prices. Equivalently, valued at those
// prices each party's give value equals its receive value EXACTLY — the legs
// clear at the prices, leaving no residual a Half-B surplus skim could be
// extracted from. Without this, rule 3 bounds only quantities and "surplus"
// would be measured against prices the transfers need not respect.
//
// Values are LegacyDec in the match numeraire (the common price quote; dev:
// uusdc). Rounding rule: NONE — exact (residual must be LegacyDec-zero). The
// solver controls both the legs and the prices and can always present an exact
// clearing, so any nonzero per-party residual is a mispriced/extractive
// solution and is rejected.
//
// EXPORTED as the SINGLE SOURCE OF TRUTH for the price-representability rule
// (D-055): the off-chain reference solver (solver/gate) calls this exact
// function as its pre-submission self-check, so the rule deciding "which prices
// are exactly representable in LegacyDec, hence which cycles are clearable"
// cannot drift between the chain and the solver. Pure (no ctx/store): the
// off-chain caller passes flows from the exported CheckConservation.
func CheckPriceBinding(m types.Match, flows NetFlows) error {
	numeraire := ""
	priceOf := make(map[string]math.LegacyDec, len(m.ClearingPrices))
	for i := range m.ClearingPrices {
		pp := m.ClearingPrices[i]
		if numeraire == "" {
			numeraire = pp.Quote
		} else if pp.Quote != numeraire {
			return errorsmod.Wrapf(types.ErrPriceMismatch, "mixed price quotes %s and %s", numeraire, pp.Quote)
		}
		priceOf[pp.Base] = pp.Price
	}
	// the numeraire's self-price is 1 (a leg moving the numeraire is valued at
	// face); reject an explicit self-price that is not 1.
	if numeraire != "" {
		if p, ok := priceOf[numeraire]; ok && !p.Equal(math.LegacyOneDec()) {
			return errorsmod.Wrapf(types.ErrPriceMismatch, "numeraire %s self-price %s != 1", numeraire, p)
		}
		priceOf[numeraire] = math.LegacyOneDec()
	}

	// iterate parties in sorted sigil order so the reported residual is
	// deterministic (the pass/fail result is order-independent regardless).
	sigils := make([]string, 0, len(flows))
	for sigil := range flows {
		sigils = append(sigils, sigil)
	}
	sort.Strings(sigils)
	for _, sigil := range sigils {
		value := math.LegacyZeroDec()
		for asset, net := range flows[sigil] {
			p, ok := priceOf[asset]
			if !ok {
				return errorsmod.Wrapf(types.ErrPriceMismatch, "sigil %s flows unpriced asset %s", sigil, asset)
			}
			value = value.Add(p.MulInt(net))
		}
		if !value.IsZero() {
			return errorsmod.Wrapf(types.ErrPriceMismatch,
				"sigil %s clears at value residual %s (legs violate stated prices)", sigil, value)
		}
	}
	return nil
}

// checkUniformPrice enforces SPEC §6.4 rule 4: each asset pair clears at one
// positive price within a match.
func checkUniformPrice(prices []types.PricePoint) error {
	seen := make(map[string]math.LegacyDec, len(prices))
	for i := range prices {
		pp := prices[i]
		if pp.Base == "" || pp.Quote == "" {
			return errorsmod.Wrap(types.ErrNonUniformPrice, "price has empty base/quote")
		}
		if pp.Price.IsNil() || !pp.Price.IsPositive() {
			return errorsmod.Wrapf(types.ErrNonUniformPrice, "price for %s/%s not positive", pp.Base, pp.Quote)
		}
		key := pp.Base + "|" + pp.Quote
		if prev, ok := seen[key]; ok && !prev.Equal(pp.Price) {
			return errorsmod.Wrapf(types.ErrNonUniformPrice, "asset pair %s clears at two prices", key)
		}
		seen[key] = pp.Price
	}
	return nil
}

// checkLimits enforces SPEC §6.4 rule 3 for one intent against its net flows.
// Phase 0 reads the give/want quantity from each leg's asset amount; the
// optional `limit` decimal (a rate) is reserved for Phase 2.
// CONCLAVE-TODO(phase-2): honor the §5.1 limit rate at the stated clearing
// prices for richer partial-fill semantics.
func checkLimits(mi matchIntent, flow map[string]math.Int) error {
	allowPartial := intentAllowsPartial(mi.rec)

	giveKeys := make(map[string]bool)
	for i := range mi.payload.Give {
		key, qty, err := intentLegKeyAmount(mi.payload.Give[i])
		if err != nil {
			return err
		}
		giveKeys[key] = true
		given := math.ZeroInt()
		if net, ok := flow[key]; ok {
			given = net.Neg() // net negative for a giver
		}
		if given.GT(qty) {
			return errorsmod.Wrapf(types.ErrLimitViolation, "sigil %s gives %s of %s over offer %s", mi.sigil, given, key, qty)
		}
		if !allowPartial && !given.Equal(qty) {
			return errorsmod.Wrapf(types.ErrLimitViolation, "sigil %s fill-or-kill: gave %s of offered %s of %s", mi.sigil, given, qty, key)
		}
	}

	wantKeys := make(map[string]bool)
	for i := range mi.payload.Want {
		key, qty, err := intentLegKeyAmount(mi.payload.Want[i])
		if err != nil {
			return err
		}
		wantKeys[key] = true
		recv := math.ZeroInt()
		if net, ok := flow[key]; ok {
			recv = net
		}
		if recv.LT(qty) {
			return errorsmod.Wrapf(types.ErrLimitViolation, "sigil %s receives %s of %s under floor %s", mi.sigil, recv, key, qty)
		}
	}

	// the solver may not move any asset the party did not authorize: a net
	// outflow must be a give asset, a net inflow a want asset.
	for key, net := range flow {
		if net.IsNegative() && !giveKeys[key] {
			return errorsmod.Wrapf(types.ErrLimitViolation, "sigil %s gives unauthorized asset %s", mi.sigil, key)
		}
		if net.IsPositive() && !wantKeys[key] {
			return errorsmod.Wrapf(types.ErrLimitViolation, "sigil %s receives unauthorized asset %s", mi.sigil, key)
		}
	}
	return nil
}

// intentLegKeyAmount returns the conservation asset key and Phase-0 quantity of
// an intent give/want leg (mirrors legAssetKey for SettleLeg).
func intentLegKeyAmount(leg veiltypes.IntentLeg) (string, math.Int, error) {
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
		amount = math.OneInt()
	}
	if set != 1 {
		return "", math.ZeroInt(), errorsmod.Wrap(types.ErrInvalidLeg, "intent leg must set exactly one asset")
	}
	return key, amount, nil
}

// counterpartySatisfied evaluates a counterparty-class predicate against a
// target sigil's state (SPEC §6.4 rule 5). Malformed predicates and any
// panic fail CLOSED (Decision A): the match is rejected, deterministically.
func (k Keeper) counterpartySatisfied(ctx sdk.Context, predicate, targetSigil string) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
		}
	}()
	pred, err := types.ParsePredicate(predicate)
	if err != nil {
		return false
	}
	return pred.Eval(predEnv{k: k, ctx: ctx, sigil: targetSigil})
}

// predEnv exposes a counterparty's Sigil state to predicate evaluation.
type predEnv struct {
	k     Keeper
	ctx   sdk.Context
	sigil string
}

func (e predEnv) Reputation() math.LegacyDec {
	rep, err := e.k.sigilKeeper.GetReputation(e.ctx, phase0Chamber, e.sigil)
	if err != nil {
		return math.LegacyZeroDec()
	}
	return rep.Score
}

func (e predEnv) HasCredential(schema string) bool {
	return e.k.sigilKeeper.HasActiveCredentialSchema(e.ctx, phase0Chamber, e.sigil, schema)
}

// intentAllowsPartial reads the envelope allow_partial flag (best-effort; a
// stored intent always has a decodable envelope).
func intentAllowsPartial(rec veiltypes.IntentRecord) bool {
	var env veiltypes.IntentEnvelope
	if err := env.Unmarshal(rec.Intent.Envelope); err != nil {
		return false
	}
	return env.AllowPartial
}
