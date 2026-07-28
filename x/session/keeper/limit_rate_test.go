package keeper

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// --- task 2.6 item 8: SPEC §5.1 limit-RATE honoring (closes D-049) ---
//
// Every fixture below keeps rule 3's QUANTITY half satisfied (full give, floor
// met exactly) and varies ONLY the signed rate, so a rejection can come from
// nothing but the new checkLimitRate clause. That is what makes these tests
// load-bearing: delete the clause and the breach cases go green.

// withLimit stamps a §5.1 limit rate onto an intent leg. The leg is marshaled
// into the SIGNED IntentPayload by submitOpen, so these tests exercise the same
// signed field a real agent sets — not a side channel.
func withLimit(leg veiltypes.IntentLeg, limit string) veiltypes.IntentLeg {
	leg.Limit = limit
	return leg
}

// rateLimits carries the four optional signed rates of a rateSwap ("" = unset).
type rateLimits struct{ aliceGive, aliceWant, bobGive, bobWant string }

// rateSwap is the item-8 fixture: alice gives 100_000_000 uusdc and wants 10
// cmpt; bob gives 10 cmpt and wants 100_000_000 uusdc. The exact full-give match
// clears at 10_000_000 uusdc per cmpt, so the ACHIEVED rates are exactly
//
//	alice max-give : 100_000_000 uusdc given / 10 cmpt received == 10_000_000
//	bob   min-want : 100_000_000 uusdc received / 10 cmpt given == 10_000_000
type rateSwap struct {
	alice, bob           sdk.AccAddress
	aliceSigil, bobSigil string
	aliceID, bobID       string
	sessionID            uint64
}

func (f *fixture) setupRateSwap(t testingT, l rateLimits) rateSwap {
	t.Helper()
	s := rateSwap{alice: randAddr(), bob: randAddr()}
	s.aliceSigil = f.registerSigil(t, s.alice)
	s.bobSigil = f.registerSigil(t, s.bob)
	f.bank.fund(s.alice.String(), sdk.NewCoins(sdk.NewInt64Coin(usdcDenom, 100_000_000)))

	usdc := coinLeg(sdk.NewInt64Coin(usdcDenom, 100_000_000))
	cmpt := svcLeg("cmpt", 10)
	s.aliceID = f.submitOpen(t, s.alice, s.aliceSigil,
		[]veiltypes.IntentLeg{withLimit(usdc, l.aliceGive)},
		[]veiltypes.IntentLeg{withLimit(cmpt, l.aliceWant)}, 100, "")
	s.bobID = f.submitOpen(t, s.bob, s.bobSigil,
		[]veiltypes.IntentLeg{withLimit(cmpt, l.bobGive)},
		[]veiltypes.IntentLeg{withLimit(usdc, l.bobWant)}, 100, "")
	s.sessionID = f.openSession(t)
	return s
}

// match is the exact full-give clearing of a rateSwap: quantities satisfied on
// both sides, achieved rate 10_000_000 uusdc per cmpt.
func (s rateSwap) match() types.Match {
	usdc := sdk.NewInt64Coin(usdcDenom, 100_000_000)
	return types.Match{
		IntentIds: []string{s.aliceID, s.bobID},
		Legs: []types.SettleLeg{
			{FromSigil: s.aliceSigil, ToSigil: s.bobSigil, Asset: &types.SettleLeg_Coin{Coin: &usdc}},
			{FromSigil: s.bobSigil, ToSigil: s.aliceSigil, Asset: &types.SettleLeg_Svc{Svc: &veiltypes.ServiceVoucher{Schema: "cmpt", Units: math.NewInt(10)}}},
		},
		ClearingPrices: []types.PricePoint{
			{Base: usdcDenom, Quote: usdcDenom, Price: math.LegacyOneDec()},
			{Base: svcDenom, Quote: usdcDenom, Price: math.LegacyNewDec(10_000_000)},
		},
	}
}

// validateMatches runs the §6.4 solution validator exactly as ProposeSolution
// and the in-consensus fallback matcher do.
func (f *fixture) validateMatches(t testingT, sessionID uint64, matches []types.Match) error {
	t.Helper()
	session, err := f.k.GetSession(f.ctx, 0, sessionID)
	require.NoError(t, err)
	params, err := f.k.GetParams(f.ctx)
	require.NoError(t, err)
	return f.k.validateSolution(f.ctx, session, matches, params)
}

// TestLimitRate_UnsetIsUnconstrained pins the pre-2.6 regime: with no signed
// limit anywhere the rate clause is inert and the exact-quantity match is valid.
// This is the property that keeps the determinism vector byte-identical (nothing
// in-tree writes IntentLeg.limit).
func TestLimitRate_UnsetIsUnconstrained(t *testing.T) {
	f := setupKeeper(t)
	s := f.setupRateSwap(t, rateLimits{})
	require.NoError(t, f.validateMatches(t, s.sessionID, []types.Match{s.match()}))
}

// TestLimitRate_AtTheBoundAccepted: a limit EQUAL to the achieved rate is
// satisfied on both sides. The bound is inclusive and the comparison exact, so
// 100_000_000 vs 10_000_000×10 must not drift by a single rounding unit.
func TestLimitRate_AtTheBoundAccepted(t *testing.T) {
	f := setupKeeper(t)
	s := f.setupRateSwap(t, rateLimits{aliceGive: "10000000", bobWant: "10000000"})
	require.NoError(t, f.validateMatches(t, s.sessionID, []types.Match{s.match()}))
}

// TestLimitRate_GiveRateBreachRejected is item 8's watched red: alice signed "at
// most 9_999_999 uusdc per cmpt" and the match pays 10_000_000. Her QUANTITIES
// are perfect (she gives exactly her offer and receives exactly her floor), so
// the pre-2.6 validator ACCEPTS this graph — only the rate clause rejects it.
func TestLimitRate_GiveRateBreachRejected(t *testing.T) {
	f := setupKeeper(t)
	s := f.setupRateSwap(t, rateLimits{aliceGive: "9999999"})
	err := f.validateMatches(t, s.sessionID, []types.Match{s.match()})
	require.ErrorIs(t, err, types.ErrLimitViolation)
}

// TestLimitRate_WantRateBreachRejected is the mirror: bob signed "at least
// 10_000_001 uusdc per cmpt given" and receives 10_000_000. His floor
// (100_000_000 uusdc) is met exactly, so again only the rate clause rejects.
func TestLimitRate_WantRateBreachRejected(t *testing.T) {
	f := setupKeeper(t)
	s := f.setupRateSwap(t, rateLimits{bobWant: "10000001"})
	err := f.validateMatches(t, s.sessionID, []types.Match{s.match()})
	require.ErrorIs(t, err, types.ErrLimitViolation)
}

// TestLimitRate_ProposeSolutionRejectsAndSlashes proves the clause is REACHED on
// the real tx path (Rule 4 — existence is not enforcement): a rate-breaching
// solution is an invalid solution, so the solver eats the §6.4 5% slash.
func TestLimitRate_ProposeSolutionRejectsAndSlashes(t *testing.T) {
	f := setupKeeper(t)
	s := f.setupRateSwap(t, rateLimits{aliceGive: "9999999"})
	solver := randAddr()
	f.registerSolver(t, solver, minSolverBond())

	err := f.propose(t, solver, s.sessionID, []types.Match{s.match()})
	require.ErrorIs(t, err, types.ErrInvalidSolution)
	require.ErrorIs(t, err, types.ErrLimitViolation)

	sv, gErr := f.k.GetSolver(f.ctx, 0, solver)
	require.NoError(t, gErr)
	require.Equal(t, uint64(1), sv.SlashedCount)
}

// TestLimitRate_FallbackDeclinesRateViolatingPair proves the clause binds the
// IN-CONSENSUS matcher too: the fallback holds every candidate to the same
// validateSolution bar, so a pair whose full-give terms breach a signed rate is
// simply not matched — the intents roll instead of settling at a rate their
// owner never signed. Liveness is unaffected: the session still CLOSES.
func TestLimitRate_FallbackDeclinesRateViolatingPair(t *testing.T) {
	f := setupKeeper(t)
	s := f.setupRateSwap(t, rateLimits{bobWant: "10000001"})

	f.processSessionAt(t, 5) // no solver registered → FALLBACK

	for _, id := range []string{s.aliceID, s.bobID} {
		rec, err := f.veil.GetIntentRecord(f.ctx, 0, id)
		require.NoError(t, err)
		require.NotEqual(t, veiltypes.INTENT_STATE_CONCLUDED, rec.State,
			"intent %s must NOT settle against a breached signed rate", id)
	}
	session, err := f.k.GetSession(f.ctx, 0, s.sessionID)
	require.NoError(t, err)
	require.Equal(t, types.SESSION_STATE_CLOSED, session.State)
	require.True(t, f.sessionPoolUUSDC().IsZero(), "nothing settled → no skim")
}

// TestLimitRate_FallbackClearsWhenRateHonored is the positive control for the
// test above: the SAME fixture with a satisfiable signed rate settles normally,
// so the rejection is attributable to the BREACH, not to the mere presence of a
// limit.
func TestLimitRate_FallbackClearsWhenRateHonored(t *testing.T) {
	f := setupKeeper(t)
	s := f.setupRateSwap(t, rateLimits{aliceGive: "10000000", bobWant: "10000000"})

	f.processSessionAt(t, 5)

	for _, id := range []string{s.aliceID, s.bobID} {
		rec, err := f.veil.GetIntentRecord(f.ctx, 0, id)
		require.NoError(t, err)
		require.Equal(t, veiltypes.INTENT_STATE_CONCLUDED, rec.State,
			"intent %s settles at its signed rate", id)
	}
}

// TestLimitRate_AmbiguousCounterSideRejected pins the RECORDED LIMITATION: with
// two distinct want assets there is no single counter-asset to divide by, so the
// signed max-give rate cannot be evaluated from flows alone. The match is
// REJECTED (fail closed) rather than settled against an unhonored limit — the
// D-049/D-074 discipline, and the reason this item needs no price source.
func TestLimitRate_AmbiguousCounterSideRejected(t *testing.T) {
	dataKey := "data/" + hex.EncodeToString(contentHash("basket"))
	mi := matchIntent{
		sigil: "alice",
		payload: veiltypes.IntentPayload{
			Give: []veiltypes.IntentLeg{withLimit(coinLeg(sdk.NewInt64Coin(usdcDenom, 100)), "10")},
			Want: []veiltypes.IntentLeg{svcLeg("cmpt", 5), dataLeg(contentHash("basket"))},
		},
	}
	flow := map[string]math.Int{
		usdcDenom: math.NewInt(-100),
		svcDenom:  math.NewInt(5),
		dataKey:   math.NewInt(1),
	}
	require.ErrorIs(t, checkLimitRate(mi, flow), types.ErrLimitRateUnsupported)
}

// TestLimitRate_MalformedLimitRejected: a limit that is not an exact decimal, or
// is negative, bounds nothing — it must fail CLOSED, never be read as "no rate".
// The "" case is the control: empty means UNSET, not malformed.
func TestLimitRate_MalformedLimitRejected(t *testing.T) {
	for _, bad := range []string{"not-a-decimal", "-1", "1e6", ""} {
		mi := matchIntent{
			sigil: "alice",
			payload: veiltypes.IntentPayload{
				Give: []veiltypes.IntentLeg{withLimit(coinLeg(sdk.NewInt64Coin(usdcDenom, 100)), bad)},
				// the want leg's min-want bound is satisfied by the flow below
				// (5 cmpt received per 100 uusdc given == 0.05), so it never
				// masks the give-leg verdict under test.
				Want: []veiltypes.IntentLeg{withLimit(svcLeg("cmpt", 5), "0.05")},
			},
		}
		flow := map[string]math.Int{usdcDenom: math.NewInt(-100), svcDenom: math.NewInt(5)}
		err := checkLimitRate(mi, flow)
		if bad == "" {
			require.NoError(t, err, "an empty give limit is UNSET, not malformed")
			continue
		}
		require.ErrorIs(t, err, types.ErrLimitRateUnsupported, "limit %q", bad)
	}
}

// TestRateWithinBound_ZeroDenominator pins the documented zero-denominator
// semantics of the exact comparison (there is no division, so no special case is
// needed): giving something for nothing breaches ANY max-give bound, while
// receiving something for nothing satisfies ANY min-want bound.
func TestRateWithinBound_ZeroDenominator(t *testing.T) {
	limit := math.LegacyNewDec(10)
	zero, some := math.ZeroInt(), math.NewInt(7)

	within, ok := rateWithinBound(some, limit, zero, true) // gave 7, received 0
	require.True(t, ok)
	require.False(t, within, "give-for-nothing must breach a max-give limit")

	within, ok = rateWithinBound(zero, limit, zero, true) // gave nothing
	require.True(t, ok)
	require.True(t, within, "giving nothing cannot breach a max-give limit")

	within, ok = rateWithinBound(some, limit, zero, false) // received 7, gave 0
	require.True(t, ok)
	require.True(t, within, "receive-for-nothing satisfies any min-want limit")
}
