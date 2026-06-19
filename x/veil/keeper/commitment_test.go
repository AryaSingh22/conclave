package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/veil/types"
)

// The D-065 Reveal cross-check: the ante bound the ceilings to the declared
// spend_commitment WITHOUT reading give legs (uniform in stub and encrypted
// mode); the Reveal reads the real give legs and turns an intent INVALID
// (50% bond slash, exactly like a malformed payload — D-040) iff its actual
// coin give exceeds the commitment in ANY denom. These tests pin both
// branches and the inherited F-3 boundary.

// dataGiveBytes builds a payload whose sole give leg is a (zero-valued under
// FaceValuePriceSource) DataGrant — the F-3 boundary case.
func dataGiveBytes(t testingT) []byte {
	t.Helper()
	payload := types.IntentPayload{
		Give: []types.IntentLeg{{
			Asset: &types.IntentLeg_Data{Data: &types.DataGrant{
				ContentHash: []byte("dataset-hash"),
				SchemaId:    "schema-1",
			}},
		}},
	}
	bz, err := payload.Marshal()
	require.NoError(t, err)
	return bz
}

// submitSealReveal funds the owner, submits ct/env at height 1, seals at the
// cadence-3 boundary (height 3) and runs the Reveal at height 4 — the real
// ABCI phase order — returning the post-Reveal record.
func (f *fixture) submitSealReveal(t testingT, owner sdk.AccAddress, sigilID string, ct, env []byte, bond sdk.Coin) types.IntentRecord {
	t.Helper()
	f.bank.fund(owner.String(), sdk.NewCoins(sdk.NewInt64Coin(bondDenom, 100_000_000)))
	f.beginBlock(t, 1)
	id, err := f.k.SubmitIntent(f.ctx, submitMsg(owner, sigilID, ct, env, bond))
	require.NoError(t, err)

	f.ctx = f.ctx.WithBlockHeight(3)
	f.endBlock(t)
	f.beginBlock(t, 4)
	return f.mustRecord(t, id)
}

// TestRevealUnderDeclarationSlashes: an intent giving 200 uusdc but declaring
// only 100 uusdc is under-declared → INVALID, 50% bond slashed to the
// community pool, the charge persists (no window refund, D-040), and no bond
// is created or destroyed.
func TestRevealUnderDeclarationSlashes(t *testing.T) {
	f := setupKeeper(t)
	owner := randAddr()
	sigilID := f.registerSigil(t, owner)

	rec := f.submitSealReveal(t, owner, sigilID,
		payloadBytes(t, sdk.NewInt64Coin(usdcDenom, 200_000_000)),
		envelopeBytes(t, 100, "", sdk.NewInt64Coin(usdcDenom, 100_000_000)),
		minBond())

	require.Equal(t, types.INTENT_STATE_INVALID, rec.State,
		"actual give 200 uusdc exceeds the 100 uusdc commitment → INVALID")

	// 50% of the bond funds the community pool; the remainder refunds to the
	// owner, so the veil module account is left empty.
	slash := minBond().Amount.QuoRaw(2)
	require.Equal(t, slash, f.bank.balanceOf(distrModuleAddr).AmountOf(bondDenom))
	require.True(t, f.bank.balanceOf(veilModuleAddr).AmountOf(bondDenom).IsZero(),
		"the bond fully left the module: half slashed, half refunded")
	// nothing minted or burned across the whole flow
	require.Equal(t, math.NewInt(100_000_000), f.bank.total(bondDenom))
}

// TestRevealUndeclaredDenomSlashes: a give leg in a denom the commitment does
// NOT mention compares against AmountOf(denom)=0 and therefore fires — an
// undeclared-denom give is itself an under-declaration (the loop iterates the
// ACTUAL give coins, not the commitment, so it cannot be dodged by omitting a
// denom from the commitment).
func TestRevealUndeclaredDenomSlashes(t *testing.T) {
	f := setupKeeper(t)
	owner := randAddr()
	sigilID := f.registerSigil(t, owner)

	// commitment is denominated only in uclv; the give is in uusdc.
	rec := f.submitSealReveal(t, owner, sigilID,
		payloadBytes(t, sdk.NewInt64Coin(usdcDenom, 1)),
		envelopeBytes(t, 100, "", sdk.NewInt64Coin(bondDenom, 500_000_000)),
		minBond())

	require.Equal(t, types.INTENT_STATE_INVALID, rec.State,
		"a uusdc give against a uclv-only commitment is an undeclared-denom under-declaration")
}

// TestRevealOverDeclarationOpens: declaring MORE than the actual give is
// allowed and self-penalizing (it consumed more ceiling and a larger bond);
// it never slashes.
func TestRevealOverDeclarationOpens(t *testing.T) {
	f := setupKeeper(t)
	owner := randAddr()
	sigilID := f.registerSigil(t, owner)

	rec := f.submitSealReveal(t, owner, sigilID,
		payloadBytes(t, sdk.NewInt64Coin(usdcDenom, 100_000_000)),
		envelopeBytes(t, 100, "", sdk.NewInt64Coin(usdcDenom, 200_000_000)),
		minBond())

	require.Equal(t, types.INTENT_STATE_OPEN, rec.State)
	require.True(t, f.bank.balanceOf(distrModuleAddr).AmountOf(bondDenom).IsZero(),
		"over-declaration is not a violation: no slash")
}

// TestRevealExactDeclarationOpens: actual give == commitment is the boundary
// and opens cleanly (the comparison is strict >).
func TestRevealExactDeclarationOpens(t *testing.T) {
	f := setupKeeper(t)
	owner := randAddr()
	sigilID := f.registerSigil(t, owner)

	rec := f.submitSealReveal(t, owner, sigilID,
		payloadBytes(t, sdk.NewInt64Coin(usdcDenom, 100_000_000)),
		envelopeBytes(t, 100, "", sdk.NewInt64Coin(usdcDenom, 100_000_000)),
		minBond())

	require.Equal(t, types.INTENT_STATE_OPEN, rec.State)
	require.True(t, f.bank.balanceOf(distrModuleAddr).AmountOf(bondDenom).IsZero())
}

// TestRevealDataOnlyEmptyCommitmentOpens is the F-3 inverse-pin: a data/svc-
// only intent gives no coin value (DataGrant/ServiceVoucher legs value 0
// under FaceValuePriceSource), so an empty commitment is honest — the loop
// never fires and the intent opens without slashing. Together with the
// under-declaration test this pins that the comparison is exactly "coin give
// vs coin commitment" in consistent units, and that the F-3 boundary (coin
// commitments do not bound data/svc legs) is carried, not accidentally
// slashing a legitimate coin-give-free intent.
func TestRevealDataOnlyEmptyCommitmentOpens(t *testing.T) {
	f := setupKeeper(t)
	owner := randAddr()
	sigilID := f.registerSigil(t, owner)

	rec := f.submitSealReveal(t, owner, sigilID,
		dataGiveBytes(t),
		envelopeBytes(t, 100, ""), // empty commitment = zero, a valid declaration
		minBond())

	require.Equal(t, types.INTENT_STATE_OPEN, rec.State,
		"a coin-give-free intent with a zero commitment is not a violation")
	require.True(t, f.bank.balanceOf(distrModuleAddr).AmountOf(bondDenom).IsZero())
}

// TestFuzzCommitmentDeclaration drives random give legs against random
// commitments through submit → seal → Reveal and asserts the outcome is
// exactly INVALID iff the actual coin give exceeds the commitment in some
// denom, OPEN otherwise — never a panic, in either denom.
func TestFuzzCommitmentDeclaration(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		f := setupKeeper(rt)
		owner := sdk.AccAddress([]byte("fuzz-commit-owner-01"))
		sigilID := f.registerSigil(rt, owner)

		amt := func(label string) int64 { return rapid.Int64Range(0, 1000).Draw(rt, label) }
		giveUsdc, giveClv := amt("give_usdc"), amt("give_clv")
		commUsdc, commClv := amt("commit_usdc"), amt("commit_clv")

		give := sdk.NewCoins(
			sdk.NewInt64Coin(usdcDenom, giveUsdc),
			sdk.NewInt64Coin(bondDenom, giveClv),
		)
		commitment := sdk.NewCoins(
			sdk.NewInt64Coin(usdcDenom, commUsdc),
			sdk.NewInt64Coin(bondDenom, commClv),
		)

		payload := types.IntentPayload{}
		for _, c := range give {
			coin := c
			payload.Give = append(payload.Give, types.IntentLeg{Asset: &types.IntentLeg_Coin{Coin: &coin}})
		}
		ct, err := payload.Marshal()
		require.NoError(rt, err)

		rec := f.submitSealReveal(rt, owner, sigilID, ct,
			envelopeBytes(rt, 100, "", commitment...), minBond())

		// expected: INVALID iff some actual give denom exceeds the commitment.
		expectInvalid := false
		for _, c := range give {
			if c.Amount.GT(commitment.AmountOf(c.Denom)) {
				expectInvalid = true
				break
			}
		}
		if expectInvalid {
			require.Equal(rt, types.INTENT_STATE_INVALID, rec.State)
		} else {
			require.Equal(rt, types.INTENT_STATE_OPEN, rec.State)
		}
	})
}
