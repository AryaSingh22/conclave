package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// --- task 2.6 item 6a: 2-party PARTIAL-FILL fallback ---

// altDenom is a second coin denom that sorts BEFORE uusdc, so it is the
// numeraire pickNumeraire chooses for the pair below (deterministic tie-break to
// the lexicographically smaller coin).
const altDenom = "uaaa"

// submitOpenAllowPartial mirrors keeper_test.go's submitOpen but SIGNS
// allow_partial in the envelope — the flag the 2.6 floor-fill candidate reads
// (submitOpen hardcodes false).
func (f *fixture) submitOpenAllowPartial(t testingT, owner sdk.AccAddress, sigilID string, give, want []veiltypes.IntentLeg, ttl uint64) string {
	t.Helper()
	f.bank.fund(owner.String(), sdk.NewCoins(minBond()))
	msg := &veiltypes.MsgSubmitIntent{
		Sender:      owner.String(),
		SenderSigil: sigilID,
		Ciphertext:  payloadBytes(t, give, want, ""),
		Envelope:    envelopeBytes(t, ttl, true, coinGiveValue(give)...),
		Bond:        minBond(),
	}
	id, err := f.veil.SubmitIntent(f.ctx, msg)
	require.NoError(t, err)
	return id
}

type partialSwap struct {
	alice, bob           sdk.AccAddress
	aliceSigil, bobSigil string
	aliceID, bobID       string
	sessionID            uint64
}

// setupPartialSwap builds the item-6a pair:
//
//	alice: give 7 uaaa, want 3 uusdc   (allow_partial = alicePartial)
//	bob:   give 3 uusdc, want 6 uaaa   (always fill-or-kill)
//
// The FULL-GIVE terms (7 uaaa against 3 uusdc) price uusdc at 7/3, which is not
// exactly representable in LegacyDec — the matcher never rounds (D-047), so the
// pre-2.6 fallback left this pair unmatched. The FLOOR-FILL terms hand bob his
// want-floor of 6 uaaa and leave alice's remaining 1 uaaa untouched, pricing
// uusdc at 6/3 = 2 EXACTLY, with both floors still met.
func (f *fixture) setupPartialSwap(t testingT, alicePartial bool) partialSwap {
	t.Helper()
	s := partialSwap{alice: randAddr(), bob: randAddr()}
	s.aliceSigil = f.registerSigil(t, s.alice)
	s.bobSigil = f.registerSigil(t, s.bob)
	f.bank.fund(s.alice.String(), sdk.NewCoins(sdk.NewInt64Coin(altDenom, 7)))
	f.bank.fund(s.bob.String(), sdk.NewCoins(sdk.NewInt64Coin(usdcDenom, 3)))

	aliceGive := []veiltypes.IntentLeg{coinLeg(sdk.NewInt64Coin(altDenom, 7))}
	aliceWant := []veiltypes.IntentLeg{coinLeg(sdk.NewInt64Coin(usdcDenom, 3))}
	if alicePartial {
		s.aliceID = f.submitOpenAllowPartial(t, s.alice, s.aliceSigil, aliceGive, aliceWant, 100)
	} else {
		s.aliceID = f.submitOpen(t, s.alice, s.aliceSigil, aliceGive, aliceWant, 100, "")
	}
	s.bobID = f.submitOpen(t, s.bob, s.bobSigil, s.bobGive(), s.bobWant(), 100, "")
	s.sessionID = f.openSession(t)
	return s
}

func (s partialSwap) bobGive() []veiltypes.IntentLeg {
	return []veiltypes.IntentLeg{coinLeg(sdk.NewInt64Coin(usdcDenom, 3))}
}
func (s partialSwap) bobWant() []veiltypes.IntentLeg {
	return []veiltypes.IntentLeg{coinLeg(sdk.NewInt64Coin(altDenom, 6))}
}

// TestFallbackPartialFill_ClearsWhatFullGiveCannot is item 6a's acceptance: the
// pair the pre-2.6 full-give terms could not price now settles at the floor-fill
// terms, with alice handing over 6 of her 7 uaaa and KEEPING the remainder.
func TestFallbackPartialFill_ClearsWhatFullGiveCannot(t *testing.T) {
	f := setupKeeper(t)
	s := f.setupPartialSwap(t, true)

	f.processSessionAt(t, 5) // no solver → FALLBACK

	for _, id := range []string{s.aliceID, s.bobID} {
		rec, err := f.veil.GetIntentRecord(f.ctx, 0, id)
		require.NoError(t, err)
		require.Equal(t, veiltypes.INTENT_STATE_CONCLUDED, rec.State, "intent %s settled", id)
	}
	require.Equal(t, math.NewInt(1), f.bank.balanceOf(s.alice.String()).AmountOf(altDenom),
		"alice keeps the 1 uaaa remainder of her 7")
	require.Equal(t, math.NewInt(6), f.bank.balanceOf(s.bob.String()).AmountOf(altDenom),
		"bob receives exactly his 6 uaaa floor")
	require.Equal(t, math.NewInt(3), f.bank.balanceOf(s.alice.String()).AmountOf(usdcDenom),
		"alice receives her 3 uusdc floor")
	require.True(t, f.bank.balanceOf(s.bob.String()).AmountOf(usdcDenom).IsZero())
}

// TestFallbackPartialFill_RemainderConservation is item 6a's load-bearing
// conservation proof — D-048 invariant (i), coin VALUE conservation, on the new
// partial-fill shape. Three things must hold at once, and the test fails if the
// remainder is either DROPPED (debited from alice but never credited to bob) or
// DOUBLE-COUNTED (settled and then re-offered):
//
//  1. the partially-filled denom nets ZERO across every account (total supply
//     unchanged) and the session module account holds none of it — the leg moved
//     through escrow and out again;
//  2. the unfilled remainder stays attributed to its intent's owner: alice's
//     wallet holds exactly 7−6 == 1, so no unit vanished into the graph;
//  3. the remainder is NOT re-offered — a partially-filled intent is CONCLUDED
//     (terminal), so a later EndBlock cannot settle the same offer twice.
func TestFallbackPartialFill_RemainderConservation(t *testing.T) {
	f := setupKeeper(t)
	s := f.setupPartialSwap(t, true)

	altBefore := f.bank.total(altDenom)
	usdcBefore := f.bank.total(usdcDenom)

	f.processSessionAt(t, 5)

	// (1) supply-invariance per denom: a partial fill mints and burns nothing.
	require.Equal(t, altBefore, f.bank.total(altDenom), "uaaa supply unchanged by a partial fill")
	require.Equal(t, usdcBefore, f.bank.total(usdcDenom), "uusdc supply unchanged")
	require.True(t, f.bank.balanceOf(sessionModuleAddr).AmountOf(altDenom).IsZero(),
		"the settlement escrow nets zero on the partially-filled denom")

	// (2) giver-side accounting: exactly the FILLED amount left alice, and the
	// remainder is still hers (dropping it would leave 0 here, over-filling 7).
	require.Equal(t, math.NewInt(1), f.bank.balanceOf(s.alice.String()).AmountOf(altDenom))
	require.Equal(t, math.NewInt(6), f.bank.balanceOf(s.bob.String()).AmountOf(altDenom))

	// (3) settled once, not re-offered: the intent is terminal and a further
	// EndBlock moves nothing.
	rec, err := f.veil.GetIntentRecord(f.ctx, 0, s.aliceID)
	require.NoError(t, err)
	require.Equal(t, veiltypes.INTENT_STATE_CONCLUDED, rec.State)

	f.processSessionAt(t, 9)
	require.Equal(t, math.NewInt(1), f.bank.balanceOf(s.alice.String()).AmountOf(altDenom),
		"the remainder is not re-offered in a later session")
	require.Equal(t, altBefore, f.bank.total(altDenom))
}

// TestFallbackPartialFill_FillOrKillNeverPartiallyFilled: with allow_partial
// UNSIGNED, alice's offer is all-or-nothing. The floor-fill candidate is not even
// built for her, and §6.4 rule 3 would reject it if it were — so the pair simply
// does not clear and her 7 uaaa is untouched. Liveness holds: the session closes.
func TestFallbackPartialFill_FillOrKillNeverPartiallyFilled(t *testing.T) {
	f := setupKeeper(t)
	s := f.setupPartialSwap(t, false)

	f.processSessionAt(t, 5)

	for _, id := range []string{s.aliceID, s.bobID} {
		rec, err := f.veil.GetIntentRecord(f.ctx, 0, id)
		require.NoError(t, err)
		require.NotEqual(t, veiltypes.INTENT_STATE_CONCLUDED, rec.State,
			"fill-or-kill intent %s must not settle partially", id)
	}
	require.Equal(t, math.NewInt(7), f.bank.balanceOf(s.alice.String()).AmountOf(altDenom),
		"a fill-or-kill offer is never split")
	require.Equal(t, math.NewInt(3), f.bank.balanceOf(s.bob.String()).AmountOf(usdcDenom))

	session, err := f.k.GetSession(f.ctx, 0, s.sessionID)
	require.NoError(t, err)
	require.Equal(t, types.SESSION_STATE_CLOSED, session.State)
}

// TestFallbackPartialFill_PreImageUniformity extends D-086 to the shape task 2.6
// introduces. A partial fill produces a Match whose leg amount is NOT the
// intent's offered quantity, so it is a genuinely new pre-image; the §6.6
// receipt's terms_hash must still recompute from the PERSISTED fallback Solution
// (the 2.4 conformance test proves this for full fills only). If the stored
// Solution held the full-give legs while the receipt committed to the partial
// ones — or vice versa — the fill-back evidence chain would break silently.
func TestFallbackPartialFill_PreImageUniformity(t *testing.T) {
	f := setupKeeper(t)
	s := f.setupPartialSwap(t, true)

	f.processSessionAt(t, 5)

	session, err := f.k.GetSession(f.ctx, 0, s.sessionID)
	require.NoError(t, err)
	require.NotEmpty(t, session.WinningSolutionId, "a fallback clearing records its Solution (D-086)")

	sols, err := f.k.SolutionsForSession(f.ctx, s.sessionID)
	require.NoError(t, err)
	var winner types.Solution
	for i := range sols {
		if sols[i].Id == session.WinningSolutionId {
			winner = sols[i]
		}
	}
	require.Equal(t, session.WinningSolutionId, winner.Id, "the winning solution is fetchable")
	require.Len(t, winner.Matches, 1)

	// the persisted pre-image is the PARTIAL shape, not the full offer.
	var settledAlt math.Int
	for _, leg := range winner.Matches[0].Legs {
		if c := leg.GetCoin(); c != nil && c.Denom == altDenom {
			settledAlt = c.Amount
		}
	}
	require.Equal(t, math.NewInt(6), settledAlt, "the stored Match carries the filled 6, not the offered 7")

	// and it recomputes to exactly what both receipts committed to.
	want := matchTermsHash(*f.k, winner.Matches[0])
	for _, id := range []string{s.aliceID, s.bobID} {
		payload := append(append([]byte{}, u64be(s.sessionID)...), []byte(id)...)
		rec, found, rErr := f.k.GetReceiptByID(f.ctx, deriveID(domainReceipt, 0, payload))
		require.NoError(t, rErr)
		require.True(t, found, "receipt for intent %s", id)
		require.Equal(t, want, rec.TermsHash,
			"the queried partial-fill Solution must recompute to the receipt's terms_hash")
	}
}
