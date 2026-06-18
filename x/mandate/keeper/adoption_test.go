package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/mandate/types"
)

// TestAdoptLiveExposureOnFreshIssuance is the D-064 load-bearing test: a key
// transitioning root->mandated must be bounded from the INSTANT of issuance, not
// one intent-TTL later. A FRESH IssueMandate folds the grantee's live root-era
// exposure (Σ charged_spend, reported by the wired x/veil scan — stubbed here)
// into the new mandate's outstanding_commitment, so the §1.1-goal-3 ceiling
// binds it immediately. RED without the fix: oc would be ∅ and the over-ceiling
// charge below would WRONGLY succeed — the over-commitment hole (the D-059
// Option-Y gap, reappearing across the root->mandated transition).
func TestAdoptLiveExposureOnFreshIssuance(t *testing.T) {
	f := setupKeeper(t)
	controller := randAddr()
	sigilID := f.registerSigil(t, controller)
	grantee := randAddr()

	// the grantee already holds live root-era intents totalling this exposure.
	exposure := sdk.NewCoins(sdk.NewInt64Coin("uusdc", 300_000_000))
	f.k.SetLiveExposureFn(func(_ sdk.Context, _ uint32, g sdk.AccAddress) (sdk.Coins, error) {
		if g.Equals(grantee) {
			return exposure, nil
		}
		return sdk.NewCoins(), nil
	})

	_, err := f.k.IssueMandate(f.ctx, controller, validTerms(sigilID, grantee))
	require.NoError(t, err)

	m, found, err := f.k.ActiveMandateByGrantee(f.ctx, 0, grantee)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, exposure.String(), m.OutstandingCommitment.String(),
		"FRESH issuance must ADOPT the grantee's live root-era exposure (D-064)")

	// bounded FROM issuance: window ceiling is 500M, 300M already committed by
	// adoption, so a new 250M commitment (300+250 > 500) is REJECTED.
	require.ErrorIs(t,
		f.k.ChargeWindow(f.ctx, m, sdk.NewCoins(sdk.NewInt64Coin("uusdc", 250_000_000)), sdk.NewCoins()),
		types.ErrMandateExceeded,
		"adopted exposure must bound the key from the instant of issuance")

	// a fitting 200M commitment (300+200 == 500) is accepted — the bound is exact.
	require.NoError(t,
		f.k.ChargeWindow(f.ctx, m, sdk.NewCoins(sdk.NewInt64Coin("uusdc", 200_000_000)), sdk.NewCoins()))
}

// TestAdoptionConfinedToFreshBranch proves IN CODE (not just by argument) the
// Construction-B containment: the live-exposure scan/adopt runs ONLY on the
// FRESH branch (found==false). REPLACE carries the superseded balance (D-059)
// and AMEND carries it directly — neither may re-scan, or the carried commitment
// would be double-counted. A future refactor that moves the adoption call out of
// the found==false branch makes this fail, the same load-bearing discipline as
// the 0.6 registration-order guard.
func TestAdoptionConfinedToFreshBranch(t *testing.T) {
	f := setupKeeper(t)
	controller := randAddr()
	sigilID := f.registerSigil(t, controller)
	grantee := randAddr()

	calls := 0
	f.k.SetLiveExposureFn(func(_ sdk.Context, _ uint32, _ sdk.AccAddress) (sdk.Coins, error) {
		calls++
		return sdk.NewCoins(), nil
	})

	// FRESH issuance scans live exposure exactly once.
	_, err := f.k.IssueMandate(f.ctx, controller, validTerms(sigilID, grantee))
	require.NoError(t, err)
	require.Equal(t, 1, calls, "FRESH issuance scans live exposure exactly once")

	// REPLACE (reissue to the same grantee) must NOT scan: it carries the
	// superseded outstanding_commitment; orphans cannot exist past the first
	// mandate (a mandate-bound key cannot submit unmandated root intents).
	id2, err := f.k.IssueMandate(f.ctx, controller, validTerms(sigilID, grantee))
	require.NoError(t, err)
	require.Equal(t, 1, calls, "REPLACE must not re-scan (carry, not adopt)")

	// AMEND must NOT scan either — it carries the balance wholesale (D-059).
	require.NoError(t, f.k.AmendMandate(f.ctx, controller, id2, validTerms(sigilID, grantee)))
	require.Equal(t, 1, calls, "AMEND must not scan")
}
