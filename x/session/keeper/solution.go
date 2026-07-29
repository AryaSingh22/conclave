package keeper

import (
	"bytes"
	"encoding/hex"
	"sort"
	"strconv"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	escrowtypes "github.com/AryaSingh22/conclave/x/escrow/types"
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
		// invalid solution: 5% slash, reject (SPEC §6.4). types.InvalidSolution
		// keeps BOTH the registered ErrInvalidSolution code on the wire (session
		// 1412 — SPEC §13) and the specific rule error reachable by errors.Is.
		// This was errors.Join, which loses the registered code: cosmossdk.io/
		// errors cannot walk a multi-error, so every invalid solution surfaced
		// the internal code 1 (D-091 finding F-1, fixed in task 2.13/D-093).
		if slashErr := k.slashSolver(ctx, &s, params.InvalidSolutionSlashBps, "invalid_solution"); slashErr != nil {
			return slashErr
		}
		return types.InvalidSolution(err)
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
//     via intent-tagged legs so one agent can clear two intents in a batch —
//     RE-POINTED by D-089 from task 2.6 to **2.7/FW-2**: it needs a SettleLeg
//     intent-tag proto field and collides with D-074's two-distinct-sigil
//     bilateral constraint, which is FW-2's own plan-stop question).
//  2. conservation (CheckConservation) + legs reference only participants.
//  3. limit satisfaction: quantities (partial only if allow_partial) AND, since
//     task 2.6, the §5.1 `limit` RATE each party signed (checkLimitRate).
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

		// rule 6 (Phase 2): condition policy. All three classes
		// (HASHLOCK/OPTIMISTIC, D-073; ZK_DELIVERY, D-076) require an unambiguous
		// bilateral match (D-074) AND the seller's SIGNED consent (D-075).
		if err := validateConditionedMatch(m); err != nil {
			return errorsmod.Wrapf(err, "match %d", mi)
		}
		// CONSENT (D-075): the condition lives only on the solver-built
		// SettleLeg, which no party signs. A solver is UNTRUSTED, so it must not
		// be able to bind a seller to a delivery obligation + bond/slash the
		// seller never signed. Enforce that every conditioned leg's condition
		// EQUALS the seller (FromSigil) intent's signed IntentPayload.condition
		// (carried in the signed ciphertext). A seller's default NONE rejects
		// any imposed condition → the conditioned match fails → fallback.
		if err := validateConditionConsent(m, byID); err != nil {
			return errorsmod.Wrapf(err, "match %d", mi)
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
// The QUANTITY half reads the give/want amount from each leg's asset amount
// (D-049) — given ≤ offered, recv ≥ wanted, and no unauthorized asset moves.
// The RATE half (the §5.1 `limit` decimal D-049 left unhonored) is task 2.6's
// checkLimitRate, called as a SEPARATE final clause so the quantity guard above
// it is unchanged: the rate clause can only ever ADD a rejection, never accept a
// graph these checks reject.
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

	// rule 3, RATE half (§5.1 `limit`, task 2.6) — a separate clause, deliberately
	// last: every quantity guard above has already run unchanged.
	return checkLimitRate(mi, flow)
}

// checkLimitRate enforces the RATE half of SPEC §6.4 rule 3 — the §5.1
// `IntentLeg.limit` decimal ("max-give / min-want as a decimal string") that
// D-049 deliberately left UNHONORED in Phase 0. Task 2.6 closes that boundary:
// until now the field was SIGNED by the agent and read by nobody, the worst of
// both worlds — an agent could believe it had bounded its rate while the matcher
// bounded only its quantities.
//
// The rate is the party's OWN exchange rate, computed FROM THE SETTLED FLOWS
// ALONE — no external price source (the D-044/F-3 ante price-source swap stays
// task 3.4) and not from the solver-stated clearing prices either:
//
//   - a GIVE leg of asset X bounds  given_X / recv_Y ≤ limit   (max-give);
//   - a WANT leg of asset Y bounds  recv_Y / given_X ≥ limit   (min-want);
//
// where Y (resp. X) is the intent's single counter-side asset. This IS §6.4's
// "at the stated clearing prices" for the shapes the matcher builds: because
// CheckPriceBinding forces each party's price-valued net to EXACTLY zero
// (p_X·given_X == p_Y·recv_Y), a two-asset party's flow ratio and stated-price
// ratio are the same number — so reading the flows honors the spec's rule
// without letting a solver's choice of prices define the bound a party signed.
//
// The comparison is EXACT by cross-multiplication (given ⋛ limit×recv), never
// LegacyDec division, which rounds at 18 places and could admit a rate the party
// never signed. A zero denominator needs no special case: on a give leg the
// bound becomes 0, so giving something for nothing breaches; on a want leg it
// becomes ≥ 0, trivially met.
//
// RECORDED LIMITATION (the D-049/D-074 discipline — deliberate, greppable, and
// REJECTING rather than silently ignoring): a give leg's rate is undefined when
// the intent wants more than one distinct asset (and symmetrically for a want
// leg), because there is no single counter-asset to divide by and valuing the
// basket would need exactly the price lens this check refuses. Those matches are
// REJECTED with ErrLimitRateUnsupported, never settled against an unhonored
// limit — as are a malformed/negative limit and operands too large to compare
// exactly. Liftable when a per-leg counter-asset exists (D-075-FW-2's per-leg
// condition field is the natural home for that refinement).
//
// An intent that signs NO limit keeps the pre-2.6 quantity-only regime exactly:
// the clause returns before touching anything.
func checkLimitRate(mi matchIntent, flow map[string]math.Int) error {
	if !intentSignsLimit(mi.payload) {
		return nil
	}
	giveKeys, err := distinctIntentLegKeys(mi.payload.Give)
	if err != nil {
		return err
	}
	wantKeys, err := distinctIntentLegKeys(mi.payload.Want)
	if err != nil {
		return err
	}

	for i := range mi.payload.Give {
		leg := mi.payload.Give[i]
		if leg.Limit == "" {
			continue
		}
		key, _, err := intentLegKeyAmount(leg)
		if err != nil {
			return err
		}
		limit, err := parseSignedLimit(mi.sigil, key, leg.Limit)
		if err != nil {
			return err
		}
		if len(wantKeys) != 1 {
			return errorsmod.Wrapf(types.ErrLimitRateUnsupported,
				"sigil %s: max-give limit on %s but the intent wants %d distinct assets (no single counter-asset)",
				mi.sigil, key, len(wantKeys))
		}
		counter := wantKeys[0]
		given, recv := negPart(flowOf(flow, key)), posPart(flowOf(flow, counter))
		within, ok := rateWithinBound(given, limit, recv, true)
		if !ok {
			return errorsmod.Wrapf(types.ErrLimitRateUnsupported,
				"sigil %s: max-give rate %s %s per %s %s is not exactly comparable to limit %s",
				mi.sigil, given, key, recv, counter, limit)
		}
		if !within {
			return errorsmod.Wrapf(types.ErrLimitViolation,
				"sigil %s gives %s %s for %s %s — above its signed max-give limit %s",
				mi.sigil, given, key, recv, counter, limit)
		}
	}

	for i := range mi.payload.Want {
		leg := mi.payload.Want[i]
		if leg.Limit == "" {
			continue
		}
		key, _, err := intentLegKeyAmount(leg)
		if err != nil {
			return err
		}
		limit, err := parseSignedLimit(mi.sigil, key, leg.Limit)
		if err != nil {
			return err
		}
		if len(giveKeys) != 1 {
			return errorsmod.Wrapf(types.ErrLimitRateUnsupported,
				"sigil %s: min-want limit on %s but the intent gives %d distinct assets (no single counter-asset)",
				mi.sigil, key, len(giveKeys))
		}
		counter := giveKeys[0]
		recv, given := posPart(flowOf(flow, key)), negPart(flowOf(flow, counter))
		within, ok := rateWithinBound(recv, limit, given, false)
		if !ok {
			return errorsmod.Wrapf(types.ErrLimitRateUnsupported,
				"sigil %s: min-want rate %s %s per %s %s is not exactly comparable to limit %s",
				mi.sigil, recv, key, given, counter, limit)
		}
		if !within {
			return errorsmod.Wrapf(types.ErrLimitViolation,
				"sigil %s receives %s %s for %s %s — below its signed min-want limit %s",
				mi.sigil, recv, key, given, counter, limit)
		}
	}
	return nil
}

// intentSignsLimit reports whether any give/want leg carries a §5.1 limit. An
// intent that signs none takes the pre-2.6 quantity-only path unchanged (empty =
// unconstrained — the D-065 "an empty declaration is still a valid declaration"
// posture applied to a rate).
func intentSignsLimit(p veiltypes.IntentPayload) bool {
	for i := range p.Give {
		if p.Give[i].Limit != "" {
			return true
		}
	}
	for i := range p.Want {
		if p.Want[i].Limit != "" {
			return true
		}
	}
	return false
}

// distinctIntentLegKeys returns the distinct conservation asset keys of one side
// of an intent, in FIRST-APPEARANCE order — a slice built by scanning the signed
// leg list, never a map iteration (§11.6).
func distinctIntentLegKeys(legs []veiltypes.IntentLeg) ([]string, error) {
	keys := make([]string, 0, len(legs))
	seen := make(map[string]bool, len(legs))
	for i := range legs {
		key, _, err := intentLegKeyAmount(legs[i])
		if err != nil {
			return nil, err
		}
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys, nil
}

// parseSignedLimit parses a §5.1 limit decimal. A limit that is not an exact
// LegacyDec, or is negative, cannot bound anything — it fails CLOSED (the match
// is rejected) rather than being ignored, so a garbage rate is never read as
// "no rate".
func parseSignedLimit(sigil, key, limit string) (math.LegacyDec, error) {
	d, err := math.LegacyNewDecFromStr(limit)
	if err != nil {
		return math.LegacyDec{}, errorsmod.Wrapf(types.ErrLimitRateUnsupported,
			"sigil %s leg %s: limit %q is not a decimal (%s)", sigil, key, limit, err)
	}
	if d.IsNegative() {
		return math.LegacyDec{}, errorsmod.Wrapf(types.ErrLimitRateUnsupported,
			"sigil %s leg %s: negative limit %s", sigil, key, d)
	}
	return d, nil
}

// rateWithinBound compares an achieved rate num/den against `limit` by EXACT
// cross-multiplication: the give side requires num ≤ limit×den, the want side
// num ≥ limit×den. There is no division, so nothing rounds.
//
// ok=false means the comparison itself could not be made. `LegacyDec.MulInt`
// PANICS ("Int overflow") once a product leaves the ±2^256·10^18 range, and the
// fallback matcher runs this inside EndBlock where §11.6 forbids a panic on user
// input. Unlike the existing price×flow products — which cancel by construction
// in the fallback's own pricing — nothing bounds the magnitude of an
// agent-signed limit, so the guard is required here. It fails CLOSED, the same
// Decision-A posture counterpartySatisfied uses, and stays deterministic: the
// operands are pure signed-payload and match data, so every node reaches the
// identical verdict.
//
// The recover is SCOPED TO THE ARITHMETIC and must stay that way: this function
// takes no ctx, touches no store, and calls nothing but LegacyDec/Int operators,
// so the only panic reachable in its frame is the range assertion below. Keep
// the body to the comparison — moving any keeper call, store read, or callback
// inside would turn a narrow fail-closed guard into one that silently swallows
// unrelated bugs. (It is deliberately narrower than counterpartySatisfied's,
// which has to wrap a whole predicate evaluation.)
func rateWithinBound(num math.Int, limit math.LegacyDec, den math.Int, giveSide bool) (within, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			within, ok = false, false
		}
	}()
	bound := limit.MulInt(den)
	achieved := math.LegacyNewDecFromInt(num)
	if giveSide {
		return !achieved.GT(bound), true
	}
	return !achieved.LT(bound), true
}

// validateConditionedMatch enforces the condition policy (SPEC §6.5, D-073/
// D-076). A match carrying ANY condition (HASHLOCK/OPTIMISTIC/ZK_DELIVERY) must
// be an unambiguous bilateral exchange: exactly one conditioned leg, a DataGrant
// from seller→buyer, with the only other legs being coin payment legs
// buyer→seller, across exactly two distinct sigils. This is the pairing the
// escrow funding (payment-into-escrow + seller bond) depends on; multi-party,
// multi-conditioned, or non-coin-paid conditioned matches are a deliberate,
// recorded limitation (spirit of D-049), liftable when richer escrow funding
// lands. ZK_DELIVERY is a DataGrant leg like the others, so it routes through
// this same bilateral + consent gate (D-076).
func validateConditionedMatch(m types.Match) error {
	condIdx := -1
	for li := range m.Legs {
		c := m.Legs[li].Condition
		if c != nil && c.Condition != nil {
			if condIdx >= 0 {
				return errorsmod.Wrap(types.ErrConditionUnsupportedMatch, "more than one conditioned leg")
			}
			condIdx = li
		}
	}
	if condIdx < 0 {
		return nil // unconditioned match — the unchanged settlement path
	}

	cond := m.Legs[condIdx]
	if cond.GetData() == nil {
		return errorsmod.Wrap(types.ErrConditionUnsupportedMatch, "conditioned leg is not a DataGrant")
	}
	seller, buyer := cond.FromSigil, cond.ToSigil
	if seller == buyer {
		return errorsmod.Wrap(types.ErrConditionUnsupportedMatch, "conditioned self-trade")
	}

	sigils := make(map[string]bool, 2)
	for li := range m.Legs {
		sigils[m.Legs[li].FromSigil] = true
		sigils[m.Legs[li].ToSigil] = true
	}
	if len(sigils) != 2 || !sigils[seller] || !sigils[buyer] {
		return errorsmod.Wrap(types.ErrConditionUnsupportedMatch, "conditioned match is not two-party")
	}

	// every other leg must be a coin payment from the buyer to the seller.
	for li := range m.Legs {
		if li == condIdx {
			continue
		}
		leg := m.Legs[li]
		if leg.GetCoin() == nil || leg.FromSigil != buyer || leg.ToSigil != seller {
			return errorsmod.Wrap(types.ErrConditionUnsupportedMatch, "non-payment leg in a conditioned match")
		}
	}
	return nil
}

// validateConditionConsent enforces SPEC §6.5 / D-075: a delivery condition is
// solver-attached (it lives only on the unsigned SettleLeg — IntentLeg has no
// condition field), so a solver — an untrusted party (SPEC §1.1) — must not be
// able to bind a seller to a delivery obligation and the OPTIMISTIC bond/slash
// state machine the seller never signed. Every conditioned leg's condition must
// EQUAL the seller (FromSigil) intent's signed IntentPayload.condition (carried
// in the signed ciphertext); for ZK_DELIVERY the BUYER (ToSigil) must sign it
// too (D-076 — see the inline note). The default NONE rejects any imposed
// condition, so the conditioned match fails closed → fallback. The signed
// per-leg consent field (an IntentLeg.condition proto addition) is the longer-
// term refinement (D-075-FW-2); this binds to the existing signed payload-level
// field with no proto change.
func validateConditionConsent(m types.Match, byID map[string]matchIntent) error {
	for li := range m.Legs {
		leg := m.Legs[li]
		if leg.Condition == nil || leg.Condition.Condition == nil {
			continue // NONE leg — nothing to consent to
		}
		seller, ok := intentBySigil(byID, leg.FromSigil)
		if !ok {
			return errorsmod.Wrapf(types.ErrConditionNotConsented, "leg %d: no seller intent for sigil %s", li, leg.FromSigil)
		}
		if !conditionEqual(seller.payload.Condition, leg.Condition) {
			return errorsmod.Wrapf(types.ErrConditionNotConsented, "leg %d: seller %s did not sign this condition", li, leg.FromSigil)
		}
		// ZK_DELIVERY (D-076): the BUYER (ToSigil) must ALSO have signed the
		// exact condition. ZK release is seller-driven (the seller submits the
		// proof) on an advance-"committed schema/hash" (SPEC §6.5) the buyer can
		// know at intent time, and it is NOT tied to the buyer's want-key — so
		// without buyer consent an untrusted solver could route a non-consenting
		// buyer's payment into a seller-chosen release gate (the buyer-side
		// mirror of the D-075 seller hole). Exact-param consent also ties release
		// to the schema the buyer agreed to. HASHLOCK/OPTIMISTIC buyer consent
		// stays D-075-FW-1 (post-delivery receipt token / ContentHash-tied — the
		// buyer cannot pre-commit those).
		if leg.Condition.GetZkDelivery() != nil {
			buyer, ok := intentBySigil(byID, leg.ToSigil)
			if !ok {
				return errorsmod.Wrapf(types.ErrConditionNotConsented, "leg %d: no buyer intent for sigil %s", li, leg.ToSigil)
			}
			if !conditionEqual(buyer.payload.Condition, leg.Condition) {
				return errorsmod.Wrapf(types.ErrConditionNotConsented, "leg %d: buyer %s did not sign this ZK condition", li, leg.ToSigil)
			}
		}
	}
	return nil
}

// intentBySigil returns the match intent a sigil submitted. Rule 1 guarantees a
// distinct sigil per intent in a match, so the result is unambiguous and the
// map-iteration order does not affect it (no state write — §11.6 safe).
func intentBySigil(byID map[string]matchIntent, sigil string) (matchIntent, bool) {
	for _, mi := range byID {
		if mi.sigil == sigil {
			return mi, true
		}
	}
	return matchIntent{}, false
}

// conditionEqual reports whether two delivery conditions are byte-for-byte the
// same (including class and all parameters), treating nil / empty-oneof as the
// same NONE. It is the consent-equality check, so it compares the FULL params
// the seller signed (a solver may not alter the timeout, payload hash, or
// dispute window).
func conditionEqual(a, b *escrowtypes.DeliveryCondition) bool {
	aNone := a == nil || a.Condition == nil
	bNone := b == nil || b.Condition == nil
	if aNone || bNone {
		return aNone && bNone
	}
	switch at := a.Condition.(type) {
	case *escrowtypes.DeliveryCondition_Hashlock:
		bt, ok := b.Condition.(*escrowtypes.DeliveryCondition_Hashlock)
		return ok &&
			bytes.Equal(at.Hashlock.PayloadHash, bt.Hashlock.PayloadHash) &&
			at.Hashlock.TimeoutBlocks == bt.Hashlock.TimeoutBlocks
	case *escrowtypes.DeliveryCondition_Optimistic:
		bt, ok := b.Condition.(*escrowtypes.DeliveryCondition_Optimistic)
		return ok && at.Optimistic.DisputeWindowBlocks == bt.Optimistic.DisputeWindowBlocks
	case *escrowtypes.DeliveryCondition_ZkDelivery:
		bt, ok := b.Condition.(*escrowtypes.DeliveryCondition_ZkDelivery)
		return ok &&
			bytes.Equal(at.ZkDelivery.SchemaCommitment, bt.ZkDelivery.SchemaCommitment) &&
			at.ZkDelivery.VkId == bt.ZkDelivery.VkId
	default:
		return false
	}
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
