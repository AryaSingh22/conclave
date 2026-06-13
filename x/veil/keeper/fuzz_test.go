package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	mandatetypes "github.com/AryaSingh22/conclave/x/mandate/types"
	"github.com/AryaSingh22/conclave/x/veil/types"
)

// TestFuzzRevealNeverPanics is the SPEC §6.3 fuzz target "malformed
// payloads at Reveal never panic the chain (recover → INVALID)": arbitrary
// ciphertext bytes go through submit → seal → Reveal; the outcome is
// always exactly OPEN or INVALID, with bond conservation and the
// community-pool delta asserted on the INVALID branch.
func TestFuzzRevealNeverPanics(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		f := setupKeeper(rt)
		owner := sdk.AccAddress([]byte("fuzz-reveal-owner-01"))
		sigilID := f.registerSigil(rt, owner)

		ct := rapid.SliceOfN(rapid.Byte(), 0, 256).Draw(rt, "ciphertext")

		f.beginBlock(rt, 1)
		params, err := f.k.GetParams(f.ctx)
		require.NoError(rt, err)

		// fund exactly what the §6.3 formula demands so submit always
		// succeeds regardless of how large the fuzzed give legs decode;
		// fuzzed payloads demanding an unpayable bond (beyond any real
		// balance) must reject cleanly instead
		bond := params.RequiredBond(faceValueScalar(f.k.giveLegSpend(f.ctx, ct)))
		fundCap := math.NewIntWithDecimal(1, 30)
		if bond.Amount.GT(fundCap) {
			f.bank.fund(owner.String(), sdk.NewCoins(sdk.NewCoin(bondDenom, fundCap)))
			_, err := f.k.SubmitIntent(f.ctx, submitMsg(owner, sigilID, ct,
				envelopeBytes(rt, 1000, ""), bond))
			require.Error(rt, err, "an unpayable bond must reject, never panic")
			return
		}
		initial := bond.Amount.AddRaw(1_000_000)
		f.bank.fund(owner.String(), sdk.NewCoins(sdk.NewCoin(bondDenom, initial)))

		id, err := f.k.SubmitIntent(f.ctx, submitMsg(owner, sigilID, ct,
			envelopeBytes(rt, 1000, ""), bond))
		require.NoError(rt, err)

		// seal at boundary 3, Reveal at 4 — must never panic
		f.ctx = f.ctx.WithBlockHeight(3)
		f.endBlock(rt)
		f.beginBlock(rt, 4)

		rec := f.mustRecord(rt, id)
		var payload types.IntentPayload
		expectInvalid := payload.Unmarshal(ct) != nil
		if expectInvalid {
			require.Equal(rt, types.INTENT_STATE_INVALID, rec.State)
		} else {
			require.Equal(rt, types.INTENT_STATE_OPEN, rec.State)
		}

		slash := math.ZeroInt()
		if expectInvalid {
			slash = bond.Amount.MulRaw(int64(params.InvalidSlashBps)).QuoRaw(10_000)
		}

		// community-pool delta: exactly the slash, only on INVALID
		require.Equal(rt, slash.String(),
			f.bank.balanceOf(distrModuleAddr).AmountOf(bondDenom).String())
		// the module account holds the bond iff the intent is still live
		held := math.ZeroInt()
		if !expectInvalid {
			held = bond.Amount
		}
		require.Equal(rt, held.String(),
			f.bank.balanceOf(veilModuleAddr).AmountOf(bondDenom).String())
		// conservation: nothing minted, nothing burned
		require.Equal(rt, initial.String(), f.bank.total(bondDenom).String())
	})
}

// fuzzIntentPlan is one intent's schedule in the cancel/boundary race fuzz.
type fuzzIntentPlan struct {
	submitHeight int64
	cancelHeight int64 // 0 = never
	ttl          uint64
	malformed    bool
}

// fuzzOutcome captures what happened to one intent in a run.
type fuzzOutcome struct {
	id                string
	finalState        types.IntentState
	cancelStateBefore types.IntentState // state observed right before cancel
	cancelErr         error
	bond              math.Int
}

// runRaceSchedule executes a schedule against a fresh fixture, mirroring
// real ABCI phase order per height: BeginBlock (Reveal) → DeliverTx
// (submits, cancels) → EndBlock (expiry sweep, boundary seal).
func runRaceSchedule(rt *rapid.T, cadence uint64, plans []fuzzIntentPlan) (*fixture, []fuzzOutcome) {
	f := setupKeeper(rt)
	f.k.sessionKeeper = types.NewStubSessionKeeper(cadence)

	owner := sdk.AccAddress([]byte("fuzz-race-owner-0001"))
	sigilID := f.registerSigil(rt, owner)
	f.bank.fund(owner.String(), sdk.NewCoins(sdk.NewInt64Coin(bondDenom, 1_000_000_000)))

	outcomes := make([]fuzzOutcome, len(plans))
	for i := range plans {
		outcomes[i].bond = minBond().Amount
	}

	const lastHeight = 16
	for h := int64(1); h <= lastHeight; h++ {
		f.beginBlock(rt, h)
		for i, plan := range plans {
			if plan.submitHeight == h {
				ct := payloadBytes(rt, sdk.NewInt64Coin(usdcDenom, 100_000_000))
				if plan.malformed {
					ct = []byte{0xDE, 0xAD, 0xBE, 0xEF}
				}
				id, err := f.k.SubmitIntent(f.ctx, submitMsg(owner, sigilID, ct,
					envelopeBytes(rt, plan.ttl, ""), minBond()))
				require.NoError(rt, err)
				outcomes[i].id = id
			}
			if plan.cancelHeight == h && outcomes[i].id != "" {
				rec := f.mustRecord(rt, outcomes[i].id)
				outcomes[i].cancelStateBefore = rec.State
				outcomes[i].cancelErr = f.k.CancelIntent(f.ctx, owner.String(), outcomes[i].id)
			}
		}
		f.endBlock(rt)
	}

	for i := range outcomes {
		outcomes[i].finalState = f.mustRecord(rt, outcomes[i].id).State
	}
	return f, outcomes
}

// TestFuzzCancelBoundaryRace is the SPEC §6.3 fuzz target "cancel/boundary
// races": random submit/cancel schedules against random session cadences.
// It pins (a) the D-039 rule — a cancel succeeds iff the intent was
// pre-OPEN at the moment the tx executed, which makes a cancel landing in
// the boundary block deterministically win (DeliverTx precedes EndBlock)
// and a cancel after the Reveal deterministically lose; (b) determinism —
// two runs of one schedule produce identical state and balances; and
// (c) bond conservation across ALL terminal states, CANCELLED included,
// with the community-pool delta asserted for INVALID intents.
func TestFuzzCancelBoundaryRace(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		cadence := rapid.Uint64Range(1, 4).Draw(rt, "cadence")
		n := rapid.IntRange(1, 4).Draw(rt, "intents")

		plans := make([]fuzzIntentPlan, n)
		for i := range plans {
			submitH := rapid.Int64Range(1, 8).Draw(rt, "submit")
			cancelH := int64(0)
			if rapid.Bool().Draw(rt, "wants_cancel") {
				cancelH = rapid.Int64Range(submitH, 12).Draw(rt, "cancel")
			}
			plans[i] = fuzzIntentPlan{
				submitHeight: submitH,
				cancelHeight: cancelH,
				ttl:          rapid.Uint64Range(uint64(submitH)+1, 14).Draw(rt, "ttl"),
				malformed:    rapid.Bool().Draw(rt, "malformed"),
			}
		}

		f, outcomes := runRaceSchedule(rt, cadence, plans)

		// (a) the D-039 race rule, exactly
		for _, o := range outcomes {
			if o.cancelStateBefore != types.INTENT_STATE_UNSPECIFIED {
				if types.IsPreOpen(o.cancelStateBefore) {
					require.NoError(rt, o.cancelErr,
						"cancel from pre-OPEN state %s must win", o.cancelStateBefore)
					require.Equal(rt, types.INTENT_STATE_CANCELLED, o.finalState)
				} else {
					require.Error(rt, o.cancelErr,
						"cancel from %s must lose", o.cancelStateBefore)
				}
			}
		}

		// (b) determinism: an identical schedule replays to identical state
		f2, outcomes2 := runRaceSchedule(rt, cadence, plans)
		for i := range outcomes {
			require.Equal(rt, outcomes[i].id, outcomes2[i].id, "intent ids must replay")
			require.Equal(rt, outcomes[i].finalState, outcomes2[i].finalState, "states must replay")
		}
		require.Equal(rt, f.k.ExportGenesis(f.ctx), f2.k.ExportGenesis(f2.ctx),
			"exported veil state must replay identically")
		require.Equal(rt, f.bank.balances, f2.bank.balances, "balances must replay identically")

		// (c) bond conservation over every terminal state
		held, slashed := math.ZeroInt(), math.ZeroInt()
		for _, o := range outcomes {
			switch o.finalState {
			case types.INTENT_STATE_SUBMITTED, types.INTENT_STATE_SEALED_IN_SESSION, types.INTENT_STATE_OPEN:
				held = held.Add(o.bond) // escrow still live
			case types.INTENT_STATE_INVALID:
				slashed = slashed.Add(o.bond.QuoRaw(2)) // 5000 bps
			case types.INTENT_STATE_CANCELLED, types.INTENT_STATE_EXPIRED:
				// fully refunded
			default:
				rt.Fatalf("unreachable Phase-0 state %s", o.finalState)
			}
		}
		require.Equal(rt, held.String(),
			f.bank.balanceOf(veilModuleAddr).AmountOf(bondDenom).String(),
			"module account holds exactly the live bonds")
		require.Equal(rt, slashed.String(),
			f.bank.balanceOf(distrModuleAddr).AmountOf(bondDenom).String(),
			"community pool holds exactly the INVALID slashes")
		require.Equal(rt, math.NewInt(1_000_000_000).String(),
			f.bank.total(bondDenom).String(),
			"no bond value created or destroyed across any terminal state")
	})
}

// windowFuzzAgent is one mandate-bound grantee in the window-conservation
// fuzz: it owns the intents whose charged_spend should sum to its mandate
// window_spent.
type windowFuzzAgent struct {
	grantee sdk.AccAddress
	sigilID string
}

// TestFuzzWindowAccountingConservation is the accounting analog of the
// bond-conservation fuzz: over random submit/seal/open/expire/cancel/
// invalid sequences across multiple mandate-bound grantees, every
// mandate's window_spent must equal the sum of charged_spend over its own
// intents that have NOT been refunded — i.e. all states except EXPIRED and
// CANCELLED (D-041 refund matrix: INVALID and ROLLED keep their charge,
// D-040; EXPIRED/CANCELLED refund). Equivalently
// sum(window_spent) == sum(charged_spend of live intents) - sum(refunds).
//
// The test charges each window exactly as the ante would (same
// charged_spend, at submit) and lets the keeper's settleRefund drive the
// refunds; it catches both double-refund (window goes too low) and
// forgotten-refund (window stays too high) drift, and — by checking each
// mandate independently — refunds credited to the wrong mandate.
func TestFuzzWindowAccountingConservation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		f := setupKeeper(rt)

		// huge, non-resetting windows in both denoms so every charge is
		// admitted and no window reset perturbs the accounting — the test
		// isolates refund correctness, not ceiling logic
		bigCeil := sdk.NewCoins(
			sdk.NewInt64Coin(usdcDenom, 1_000_000_000_000),
			sdk.NewInt64Coin(bondDenom, 1_000_000_000_000),
		)
		nAgents := rapid.IntRange(1, 2).Draw(rt, "agents")
		agents := make([]windowFuzzAgent, nAgents)
		for i := range agents {
			controller := randAddr()
			grantee := randAddr()
			sigilID := f.registerSigil(rt, controller)
			_, err := f.mandate.IssueMandate(f.ctx, controller, mandatetypes.MandateTerms{
				SigilId:            sigilID,
				GranteeAddr:        grantee.String(),
				AllowedMsgTypes:    []string{sdk.MsgTypeURL(&types.MsgSubmitIntent{})},
				SpendCeilingPerTx:  bigCeil,
				SpendCeilingWindow: bigCeil,
				WindowBlocks:       1_000_000, // no reset within the horizon
				ExpiryHeight:       1_000_000,
			})
			require.NoError(rt, err)
			f.bank.fund(grantee.String(), sdk.NewCoins(sdk.NewInt64Coin(bondDenom, 1_000_000_000)))
			agents[i] = windowFuzzAgent{grantee: grantee, sigilID: sigilID}
		}

		// track each submitted intent's owner so the expected per-mandate
		// window can be reconstructed from stored records
		ownerOf := make(map[string]sdk.AccAddress)

		const lastHeight = 18
		nSubmits := rapid.IntRange(1, 6).Draw(rt, "submits")
		// pre-draw a submission plan: which agent, at what height, valid or
		// malformed, with what ttl
		type plan struct {
			agent     int
			height    int64
			ttl       uint64
			malformed bool
		}
		plans := make([]plan, nSubmits)
		for i := range plans {
			h := rapid.Int64Range(1, 10).Draw(rt, "submit_h")
			plans[i] = plan{
				agent:     rapid.IntRange(0, nAgents-1).Draw(rt, "which_agent"),
				height:    h,
				ttl:       rapid.Uint64Range(uint64(h)+1, 16).Draw(rt, "ttl"),
				malformed: rapid.Bool().Draw(rt, "malformed"),
			}
		}
		// optional cancels keyed by height
		cancelAt := rapid.IntRange(0, lastHeight).Draw(rt, "cancel_height")

		assertConservation := func() {
			rt.Helper()
			// expected per-owner window = Σ charged_spend over that owner's
			// intents not in {EXPIRED, CANCELLED}
			expected := make(map[string]sdk.Coins)
			require.NoError(rt, f.k.IterateIntentRecords(f.ctx, func(rec types.IntentRecord) bool {
				if rec.State == types.INTENT_STATE_EXPIRED || rec.State == types.INTENT_STATE_CANCELLED {
					return false
				}
				expected[rec.Owner] = expected[rec.Owner].Add(rec.ChargedSpend...)
				return false
			}))
			for _, a := range agents {
				m, found, err := f.mandate.ActiveMandateByGrantee(f.ctx, 0, a.grantee)
				require.NoError(rt, err)
				require.True(rt, found)
				want := expected[a.grantee.String()]
				if want == nil {
					want = sdk.NewCoins()
				}
				require.Equal(rt, want.String(), m.WindowSpent.String(),
					"window_spent must equal Σ charged_spend of non-refunded intents for %s", a.grantee)
			}
		}

		for h := int64(1); h <= lastHeight; h++ {
			f.beginBlock(rt, h)

			// submits at this height: charge the window exactly as the ante
			// would, then store the intent (mirroring ante-before-msgserver)
			for _, p := range plans {
				if p.height != h {
					continue
				}
				a := agents[p.agent]
				ct := payloadBytes(rt, sdk.NewInt64Coin(usdcDenom, 100_000_000))
				if p.malformed {
					ct = []byte{0xDE, 0xAD, 0xBE, 0xEF}
				}
				msg := submitMsg(a.grantee, a.sigilID, ct, envelopeBytes(rt, p.ttl, ""), minBond())
				charged := f.k.giveLegSpend(f.ctx, ct).Add(minBond())
				m, found, err := f.mandate.ActiveMandateByGrantee(f.ctx, 0, a.grantee)
				require.NoError(rt, err)
				require.True(rt, found)
				require.NoError(rt, f.mandate.ChargeWindow(f.ctx, m, charged))
				id, err := f.k.SubmitIntent(f.ctx, msg)
				require.NoError(rt, err)
				ownerOf[id] = a.grantee
			}

			// an optional cancel of the lowest-id still-cancellable intent
			if int64(cancelAt) == h {
				require.NoError(rt, f.k.IterateIntentRecords(f.ctx, func(rec types.IntentRecord) bool {
					if types.IsPreOpen(rec.State) {
						require.NoError(rt, f.k.CancelIntent(f.ctx, rec.Owner, rec.Intent.Id))
						return true
					}
					return false
				}))
			}

			f.endBlock(rt)
			assertConservation()
		}
	})
}
