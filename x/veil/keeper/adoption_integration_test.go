package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	mandatetypes "github.com/AryaSingh22/conclave/x/mandate/types"
)

// TestRootToMandatedAdoptionAndSelfCorrection is the D-064 end-to-end proof,
// reproducing the seed-7 break shape through the real keepers: a key submits an
// intent while UNMANDATED (so its charged_spend is an orphan — never charged to
// any mandate), then becomes mandate-bound via the controller-binding-loss path
// the trace found (RotateController strips its sigil binding, making it FRESH-
// mandate-eligible). The fix ADOPTS the live exposure into the new mandate at
// issuance via the wired x/veil scan, so:
//
//   - conservation holds (oc == Σ charged_spend(live)) the instant the mandate
//     exists — the invariant the seed-7 panic tripped; and
//   - the key is BOUNDED from issuance (an over-ceiling charge is rejected); and
//   - adoption and release are EXACT INVERSES across the full lifecycle: when the
//     adopted intent terminates, DecommitIntent drops oc by exactly the adopted
//     figure (resolving to the SAME mandate), freeing capacity so a now-fitting
//     charge succeeds. That last property is what the zeroing fix would have
//     silently violated.
//
// RED without the fix: the new mandate's oc is ∅ while the orphan is live, so
// the adoption/conservation assertions fail and the over-ceiling charge wrongly
// succeeds.
func TestRootToMandatedAdoptionAndSelfCorrection(t *testing.T) {
	f := setupKeeper(t)
	// wire the real x/veil live-exposure scan into the mandate keeper, exactly as
	// app.go does at construction.
	f.mandate.SetLiveExposureFn(f.k.SumLiveChargedSpendByOwner)

	// C controls sigil S and submits a (give-less) intent while UNMANDATED: the
	// ante would take the root/controller passthrough (no charge), but veil still
	// persists charged_spend = bond — an orphan never bound to any mandate.
	c := randAddr()
	sigilS := f.registerSigil(t, c)
	f.bank.fund(c.String(), sdk.NewCoins(sdk.NewInt64Coin(bondDenom, 100_000_000)))
	orphanID, err := f.k.SubmitIntent(f.ctx, submitMsg(c, sigilS,
		payloadBytes(t), envelopeBytes(t, 100, ""), minBond()))
	require.NoError(t, err)
	orphan := f.mustRecord(t, orphanID).ChargedSpend
	require.Equal(t, minBond().String(), orphan.String(), "give-less intent charges bond only")
	require.False(t, orphan.IsZero())

	// rotate S's controller away from C: C now resolves to no sigil and is
	// FRESH-mandate-eligible (the controller-binding-loss path; D-017(c) no longer
	// blocks issuance to C).
	require.NoError(t, f.sigil.RotateController(f.ctx, 0, sigilS, []sdk.AccAddress{c}, randAddr()))

	// a second sigil S2 (controller C3) issues a FRESH mandate to C, window
	// ceiling just 400k above the orphan exposure.
	c3 := randAddr()
	sigilS2 := f.registerSigil(t, c3)
	ceiling := minBond().Amount.AddRaw(400_000) // 1.4M uclv
	terms := mandatetypes.MandateTerms{
		SigilId:            sigilS2,
		GranteeAddr:        c.String(),
		AllowedMsgTypes:    []string{"/conclave.veil.v1.MsgSubmitIntent"},
		SpendCeilingPerTx:  sdk.NewCoins(sdk.NewCoin(bondDenom, ceiling)),
		SpendCeilingWindow: sdk.NewCoins(sdk.NewCoin(bondDenom, ceiling)),
		WindowBlocks:       86_400,
		FeeDenoms:          []string{bondDenom},
		ExpiryHeight:       1_000_000,
	}
	_, err = f.mandate.IssueMandate(f.ctx, c3, terms)
	require.NoError(t, err)

	// ADOPTION + CONSERVATION: the new mandate's oc equals the orphan exposure,
	// which equals the live scan — the invariant holds the instant of issuance.
	m, found, err := f.mandate.ActiveMandateByGrantee(f.ctx, 0, c)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, orphan.String(), m.OutstandingCommitment.String(),
		"FRESH issuance adopts the live root-era exposure (D-064)")
	live, err := f.k.SumLiveChargedSpendByOwner(f.ctx, 0, c)
	require.NoError(t, err)
	require.Equal(t, m.OutstandingCommitment.String(), live.String(),
		"conservation: oc == Σ charged_spend(live) at issuance")

	// BOUNDED FROM ISSUANCE: oc is 1M of a 1.4M ceiling, so a 500k commit
	// (1.0M+0.5M > 1.4M) is rejected — no over-commitment across the transition.
	require.ErrorIs(t,
		f.mandate.ChargeWindow(f.ctx, m, sdk.NewCoins(sdk.NewInt64Coin(bondDenom, 500_000)), sdk.NewCoins()),
		mandatetypes.ErrMandateExceeded,
		"adopted exposure bounds the key from issuance")

	// SELF-CORRECTION: terminate the adopted intent. DecommitIntent resolves to
	// the SAME mandate and drops oc by EXACTLY the adopted figure — adoption and
	// release are inverses across the full lifecycle.
	require.NoError(t, f.k.CancelIntent(f.ctx, c.String(), orphanID))
	m, _, err = f.mandate.ActiveMandateByGrantee(f.ctx, 0, c)
	require.NoError(t, err)
	require.True(t, m.OutstandingCommitment.IsZero(),
		"terminate releases exactly the adopted commitment (DecommitIntent inverse of adoption)")
	live, err = f.k.SumLiveChargedSpendByOwner(f.ctx, 0, c)
	require.NoError(t, err)
	require.True(t, live.IsZero(), "conservation: oc == Σ charged_spend(live) == 0 after terminate")

	// the freed capacity is usable again — a now-fitting commit succeeds.
	require.NoError(t,
		f.mandate.ChargeWindow(f.ctx, m, sdk.NewCoins(sdk.NewInt64Coin(bondDenom, 500_000)), sdk.NewCoins()),
		"capacity freed by the decommit is available for new mandated intents")
}

// TestFreshIssuanceFromBirthAdoptsZero confirms the COMMON (happy) path is
// unperturbed by D-064: a key mandated FROM BIRTH (no pre-mandate intents) gets
// oc=∅ from the adoption scan, so outstanding_commitment still accumulates purely
// through ChargeWindow exactly as before. It also rules out same-block
// double-counting: the scan runs at issuance (sees nothing), and the key's FIRST
// intent — submitted AFTER it is mandate-bound, in the same block — is counted
// ONCE by ChargeWindow. It can never ALSO be adopted, because a key that already
// holds a mandate only ever takes the REPLACE branch (no scan), never FRESH
// (D-017: the grantee-index entry is never deleted, so a key cannot return to
// found==false). ChargeWindow-charging and FRESH-adoption partition the intent
// set and are temporally exclusive forever.
func TestFreshIssuanceFromBirthAdoptsZero(t *testing.T) {
	f := setupKeeper(t)
	f.mandate.SetLiveExposureFn(f.k.SumLiveChargedSpendByOwner)

	controller := randAddr()
	sigilS := f.registerSigil(t, controller)
	k := randAddr() // a fresh delegate key: controls nothing, has no intents

	terms := mandatetypes.MandateTerms{
		SigilId:            sigilS,
		GranteeAddr:        k.String(),
		AllowedMsgTypes:    []string{"/conclave.veil.v1.MsgSubmitIntent"},
		SpendCeilingPerTx:  sdk.NewCoins(sdk.NewInt64Coin(bondDenom, 100_000_000)),
		SpendCeilingWindow: sdk.NewCoins(sdk.NewInt64Coin(bondDenom, 100_000_000)),
		WindowBlocks:       86_400,
		FeeDenoms:          []string{bondDenom},
		ExpiryHeight:       1_000_000,
	}
	_, err := f.mandate.IssueMandate(f.ctx, controller, terms)
	require.NoError(t, err)

	// mandated-from-birth: the real scan sees no intents → adoption contributes ∅.
	m, found, err := f.mandate.ActiveMandateByGrantee(f.ctx, 0, k)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, m.OutstandingCommitment.IsZero(),
		"FRESH issuance with no pre-mandate intents adopts ∅ (common path unperturbed)")

	// k's FIRST intent, submitted in the SAME block AFTER it is mandate-bound, is
	// counted exactly ONCE by ChargeWindow — never also adopted.
	f.bank.fund(k.String(), sdk.NewCoins(sdk.NewInt64Coin(bondDenom, 100_000_000)))
	id, err := f.k.SubmitIntent(f.ctx, submitMsg(k, sigilS,
		payloadBytes(t), envelopeBytes(t, 100, ""), minBond()))
	require.NoError(t, err)
	charge := f.mustRecord(t, id).ChargedSpend
	require.NoError(t, f.mandate.ChargeWindow(f.ctx, m, charge, sdk.NewCoins()))

	m, _, err = f.mandate.ActiveMandateByGrantee(f.ctx, 0, k)
	require.NoError(t, err)
	require.Equal(t, charge.String(), m.OutstandingCommitment.String(),
		"first intent counted exactly once — no adoption/ChargeWindow double-count")
}

// sanity: the live-exposure scan excludes terminated intents and is owner-scoped.
func TestSumLiveChargedSpendByOwner(t *testing.T) {
	f := setupKeeper(t)
	owner := randAddr()
	sigilID := f.registerSigil(t, owner)
	other := randAddr()
	otherSigil := f.registerSigil(t, other)

	id1 := f.submitValid(t, owner, sigilID, 100)
	id2 := f.submitValid(t, owner, sigilID, 100)
	_ = f.submitValid(t, other, otherSigil, 100)

	rec1 := f.mustRecord(t, id1)
	rec2 := f.mustRecord(t, id2)
	want := rec1.ChargedSpend.Add(rec2.ChargedSpend...)

	got, err := f.k.SumLiveChargedSpendByOwner(f.ctx, 0, owner)
	require.NoError(t, err)
	require.Equal(t, want.String(), got.String(), "sums only this owner's live intents")

	// cancelling one excludes it (terminated states are not live).
	require.NoError(t, f.k.CancelIntent(f.ctx, owner.String(), id1))
	got, err = f.k.SumLiveChargedSpendByOwner(f.ctx, 0, owner)
	require.NoError(t, err)
	require.Equal(t, rec2.ChargedSpend.String(), got.String(), "cancelled intent drops out of the live sum")
}
