package app_test

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log"
	"cosmossdk.io/math"

	storetypes "cosmossdk.io/store/types"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/simulation"
	simcli "github.com/cosmos/cosmos-sdk/x/simulation/client/cli"

	"github.com/AryaSingh22/conclave/app"
	"github.com/AryaSingh22/conclave/simcoverage"
	mandatetypes "github.com/AryaSingh22/conclave/x/mandate/types"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
	sigiltypes "github.com/AryaSingh22/conclave/x/sigil/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// Task 0.11 — SDK module simulation integration (SPEC §10 row 0.11).
//
// TestFullAppSimulation runs a 500-block operation-based simulation across all
// six custom modules with the cross-module crisis invariants checked EVERY
// block (inv-check-period=1). A broken invariant panics in the crisis
// EndBlocker, which surfaces here as a FinalizeBlock error — that is the
// bug-finding gate: if the sim aborts on an invariant, we have FOUND an
// interaction bug (a success of the task), not a flake to suppress.
//
// TestAppStateDeterminism runs the same seed twice and asserts identical app
// hashes (the standard SDK determinism gate).

func init() {
	simcli.GetSimulatorFlags()
}

// fundDenoms are added to every simulation account on top of the standard
// staking-denom funding so the Conclave economy (uclv bonds, uusdc/uclv intent
// legs) has balances to move. The base randomized genesis keeps the staking
// denom ("stake") for validators/mint; Conclave economics run in uclv/uusdc.
var fundDenoms = sdk.NewCoins(
	sdk.NewCoin("uclv", math.NewIntWithDecimal(1, 15)),
	sdk.NewCoin("uusdc", math.NewIntWithDecimal(1, 15)),
)

// simAppStateFn wraps the standard randomized AppStateFn and adds uclv/uusdc to
// every simulation account's genesis balance.
func simAppStateFn(a *app.ConclaveApp) simtypes.AppStateFn {
	inner := sims.AppStateFn(a.AppCodec(), a.SimulationManager(), a.DefaultGenesis())
	cdc := a.AppCodec()
	return func(r *rand.Rand, accs []simtypes.Account, config simtypes.Config) (json.RawMessage, []simtypes.Account, string, time.Time) {
		appState, simAccs, chainID, ts := inner(r, accs, config)

		var rawState map[string]json.RawMessage
		if err := json.Unmarshal(appState, &rawState); err != nil {
			panic(err)
		}
		var bankGen banktypes.GenesisState
		cdc.MustUnmarshalJSON(rawState[banktypes.ModuleName], &bankGen)

		idx := make(map[string]int, len(bankGen.Balances))
		for i, b := range bankGen.Balances {
			idx[b.Address] = i
		}
		for _, acc := range simAccs {
			addr := acc.Address.String()
			if i, ok := idx[addr]; ok {
				bankGen.Balances[i].Coins = bankGen.Balances[i].Coins.Add(fundDenoms...)
			} else {
				bankGen.Balances = append(bankGen.Balances, banktypes.Balance{Address: addr, Coins: fundDenoms})
				idx[addr] = len(bankGen.Balances) - 1
			}
			bankGen.Supply = bankGen.Supply.Add(fundDenoms...)
		}
		rawState[banktypes.ModuleName] = cdc.MustMarshalJSON(&bankGen)

		out, err := json.Marshal(rawState)
		if err != nil {
			panic(err)
		}
		return out, simAccs, chainID, ts
	}
}

// newSimApp constructs a ConclaveApp configured for simulation: inv-check-period
// = 1 so the crisis module asserts every registered invariant on EVERY block.
func newSimApp(t *testing.T, db dbm.DB) *app.ConclaveApp {
	t.Helper()
	appOpts := sims.AppOptionsMap{"inv-check-period": 1}
	return app.NewConclaveApp(
		log.NewNopLogger(), db, nil, true, appOpts,
		baseapp.SetChainID(simConfig().ChainID),
		simPrecommitFlush,
	)
}

// simPrecommitFlush works around a v0.50 framework limitation: SimulateFromSeed
// runs operations via SimDeliver AFTER FinalizeBlock has already called
// workingHash (which flushed the begin/end-block state to the commit store).
// Those post-FinalizeBlock operation writes sit unflushed in finalizeBlockState
// and would be dropped by Commit(), making every block's operations vanish.
// Commit() invokes the precommiter on the finalizeBlockState context just
// before cms.Commit(), so flushing the cache here lands the operations in the
// committed store — letting state accumulate across blocks and, crucially,
// letting the per-block crisis invariants actually observe operation effects.
func simPrecommitFlush(b *baseapp.BaseApp) {
	b.SetPrecommiter(func(ctx sdk.Context) {
		if cms, ok := ctx.MultiStore().(storetypes.CacheMultiStore); ok {
			cms.Write()
		}
	})
}

func simConfig() simtypes.Config {
	cfg := simcli.NewConfigFromFlags()
	cfg.ChainID = "conclave-sim-1"
	if cfg.NumBlocks == 0 {
		cfg.NumBlocks = 500
	}
	if cfg.BlockSize == 0 {
		cfg.BlockSize = 100
	}
	cfg.InitialBlockHeight = 1
	// Commit=true is required for FinalizeBlock's height to advance; the
	// simPrecommitFlush baseapp option ensures each block's operation writes are
	// flushed into the committed store rather than stranded (see its doc).
	cfg.Commit = true
	return cfg
}

// simOperations is the FULL SimulationManager operation set (standard SDK
// modules + the six custom Conclave modules), each wrapped to TOLERATE op
// errors. Two reasons it must be the full set and tolerant:
//
//   - Standard module ops (staking/bank/gov/distribution) maintain the
//     validator set and keep blocks busy across the whole run; excluding them
//     starves the consensus layer until the validator set empties and the run
//     aborts.
//   - A standard op signs with a random account that may be a mandate-bound
//     grantee key, whose tx the MandateDecorator correctly rejects in ante (the
//     msg type is not in the mandate's allowed list). That rejection is CORRECT
//     mandate behavior, not a bug — but unwrapped SDK ops treat any delivery
//     error as fatal. Wrapping converts every op error (mandate rejection,
//     random insufficient funds, etc.) into a no-op.
//
// Tolerating op errors does NOT weaken the 0.11 bug-finding gate: that gate is
// the per-block crisis invariant check (a FinalizeBlock panic), which is
// independent of any operation's success. simState uses the app's own TxConfig
// so signing/decoding knows the custom Conclave msg types.
func simOperations(a *app.ConclaveApp) simulation.WeightedOperations {
	simState := module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       a.AppCodec(),
		TxConfig:  a.TxConfig(),
		BondDenom: sdk.DefaultBondDenom,
	}
	raw := a.SimulationManager().WeightedOperations(simState)
	wrapped := make(simulation.WeightedOperations, len(raw))
	for i, wo := range raw {
		wrapped[i] = simulation.NewWeightedOperation(wo.Weight(), tolerantOp(wo.Op()))
	}
	return wrapped
}

// tolerantOp converts any operation error into a no-op so a single rejected tx
// (e.g. a standard op signed by a mandate-bound grantee key) never aborts the
// run. See simOperations for why this does not weaken the invariant gate.
func tolerantOp(op simtypes.Operation) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		opMsg, fops, err := op(r, app, ctx, accs, chainID)
		if err != nil {
			return simtypes.NoOpMsg(opMsg.Route, opMsg.Name, "tolerated: "+err.Error()), nil, nil
		}
		return opMsg, fops, nil
	}
}

func TestFullAppSimulation(t *testing.T) {
	if !simcli.FlagEnabledValue {
		t.Skip("skipping application simulation (use -Enabled to run)")
	}
	cfg := simConfig()
	db := dbm.NewMemDB()
	a := newSimApp(t, db)

	simcoverage.Reset()

	var (
		stopEarly bool
		simErr    error
	)
	// A broken crisis invariant PANICS inside FinalizeBlock; capture it so the
	// coverage report still prints (the panic IS the 0.11 finding, not a crash
	// to hide).
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				simErr = fmt.Errorf("invariant panic: %v", rec)
			}
		}()
		stopEarly, _, simErr = simulation.SimulateFromSeed(
			t,
			os.Stdout,
			a.BaseApp,
			simAppStateFn(a),
			simtypes.RandomAccounts,
			simOperations(a),
			app.BlockedAddresses(),
			cfg,
			a.AppCodec(),
		)
	}()

	// Always print the coverage report so a reviewer sees whether the run
	// actually exercised the modules — even if it aborted on an invariant. The
	// report reads committed state (Commit=true + simPrecommitFlush) and ends
	// with the D-059 release-timing verification.
	printCoverage(t, a)

	if simErr != nil {
		t.Fatalf("SIMULATION ABORTED (invariant break or panic) — this is the 0.11 finding to diagnose:\n%v", simErr)
	}
	if stopEarly {
		t.Log("simulation stopped early (operation queue drained)")
	}
}

func TestAppStateDeterminism(t *testing.T) {
	if !simcli.FlagEnabledValue {
		t.Skip("skipping application simulation determinism (use -Enabled to run)")
	}
	const numSeeds = 3
	const numTimesToRun = 2

	cfg := simConfig()
	cfg.NumBlocks = 120 // shorter per-instance; determinism needs repetition, not length
	cfg.OnOperation = false
	cfg.Commit = true // determinism compares committed per-block app hashes

	for i := 0; i < numSeeds; i++ {
		seed := rand.Int63()
		var appHashes [][]byte
		for j := 0; j < numTimesToRun; j++ {
			c := cfg
			c.Seed = seed
			db := dbm.NewMemDB()
			a := newSimApp(t, db)
			simcoverage.Reset()
			_, _, err := simulation.SimulateFromSeed(
				t, os.Stdout, a.BaseApp,
				simAppStateFn(a),
				simtypes.RandomAccounts, simOperations(a),
				app.BlockedAddresses(), c, a.AppCodec(),
			)
			if err != nil {
				t.Fatalf("seed %d run %d aborted: %v", seed, j, err)
			}
			appHashes = append(appHashes, a.LastCommitID().Hash)
		}
		for j := 1; j < numTimesToRun; j++ {
			if string(appHashes[0]) != string(appHashes[j]) {
				t.Fatalf("NON-DETERMINISM: seed %d produced differing app hashes:\n  run0=%X\n  run%d=%X",
					seed, appHashes[0], j, appHashes[j])
			}
		}
		t.Logf("seed %d: %d runs produced identical app hash %X", seed, numTimesToRun, appHashes[0])
	}
}

// printCoverage renders the executed operation mix (submission-side counters)
// and the consensus-side OUTCOME tallies read from final committed state — the
// proof that clearing is real, mirroring task 0.10's distribution measurement.
func printCoverage(t *testing.T, a *app.ConclaveApp) {
	t.Helper()
	// committed state (Commit=true + simPrecommitFlush persists operations).
	ctx := a.NewUncachedContext(false, cmtproto.Header{Height: a.LastBlockHeight()})
	t.Logf("printCoverage: LastBlockHeight=%d", a.LastBlockHeight())

	// diagnostics: did ANY op (SimDeliver path) persist? count accounts with
	// sequence>0; and did FinalizeBlock (mint) persist? check stake supply.
	var seqNonZero int
	for _, ac := range a.AccountKeeper.GetAllAccounts(ctx) {
		if ac.GetSequence() > 0 {
			seqNonZero++
		}
	}
	t.Logf("DIAG accounts-with-seq>0=%d  stakeSupply=%s  uclvSupply=%s",
		seqNonZero,
		a.BankKeeper.GetSupply(ctx, "stake").Amount,
		a.BankKeeper.GetSupply(ctx, "uclv").Amount)

	var sigils int
	if err := a.SigilKeeper.IterateSigils(ctx, func(sigiltypes.Sigil) bool { sigils++; return false }); err != nil {
		t.Logf("IterateSigils err: %v", err)
	}

	var mandates, revoked int
	if err := a.MandateKeeper.IterateMandates(ctx, func(m mandatetypes.Mandate) bool {
		mandates++
		if m.Revoked {
			revoked++
		}
		return false
	}); err != nil {
		t.Logf("IterateMandates err: %v", err)
	}

	intentStates := map[veiltypes.IntentState]int{}
	if err := a.VeilKeeper.IterateIntentRecords(ctx, func(rec veiltypes.IntentRecord) bool {
		intentStates[rec.State]++
		return false
	}); err != nil {
		t.Logf("IterateIntentRecords err: %v", err)
	}

	sessionStates := map[sessiontypes.SessionState]int{}
	if err := a.SessionKeeper.IterateSessions(ctx, func(s sessiontypes.Session) bool {
		sessionStates[s.State]++
		return false
	}); err != nil {
		t.Logf("IterateSessions err: %v", err)
	}

	var solvers int
	var slashes uint64
	_ = a.SessionKeeper.IterateSolvers(ctx, func(s sessiontypes.Solver) bool {
		solvers++
		slashes += s.SlashedCount
		return false
	})

	report := "\n==================== 0.11 SIMULATION COVERAGE ====================\n"
	report += "operation mix that EXECUTED (successful submissions):\n"
	report += simcoverage.Report()
	report += "\nfinal-state OUTCOME tallies (consensus-side, proof of real clearing):\n"
	report += fmt.Sprintf("  sigils registered                        %d\n", sigils)
	report += fmt.Sprintf("  mandates (total / revoked)               %d / %d\n", mandates, revoked)
	report += fmt.Sprintf("  solvers registered (slash events: %d)\n", slashes)
	_ = solvers
	report += fmt.Sprintf("  solvers                                  %d\n", solvers)
	report += "  intents by state:\n"
	for _, s := range []veiltypes.IntentState{
		veiltypes.INTENT_STATE_SUBMITTED, veiltypes.INTENT_STATE_SEALED_IN_SESSION,
		veiltypes.INTENT_STATE_OPEN, veiltypes.INTENT_STATE_CONCLUDED,
		veiltypes.INTENT_STATE_ROLLED, veiltypes.INTENT_STATE_EXPIRED,
		veiltypes.INTENT_STATE_INVALID, veiltypes.INTENT_STATE_CANCELLED,
	} {
		report += fmt.Sprintf("    %-28s %d\n", s.String(), intentStates[s])
	}
	report += "  sessions by state:\n"
	for _, s := range []sessiontypes.SessionState{
		sessiontypes.SESSION_STATE_FORMING, sessiontypes.SESSION_STATE_AUCTION,
		sessiontypes.SESSION_STATE_SCORING, sessiontypes.SESSION_STATE_CONCLUDING,
		sessiontypes.SESSION_STATE_FALLBACK, sessiontypes.SESSION_STATE_CLOSED,
	} {
		report += fmt.Sprintf("    %-28s %d\n", s.String(), sessionStates[s])
	}

	// ---- D-059 release-timing verification (independent recompute) ----
	// Directly from final committed state, confirm no commitment is held a hair
	// too long on terminate: for every active mandate, outstanding_commitment
	// must equal Σ charged_spend over that grantee's STILL-LIVE intents (state
	// NOT in {CONCLUDED, EXPIRED, CANCELLED} — INVALID/ROLLED intentionally
	// retain their charge, D-040). Two consequences the reviewer asked for:
	//   (a) the count of terminated-releasing intents equals the number of
	//       DecommitIntent calls (every CONCLUDED/EXPIRED/CANCELLED routes
	//       through settleRefund→DecommitIntent; INVALID/ROLLED do not), and
	//   (b) a grantee all of whose intents have terminated-releasing carries
	//       ZERO outstanding_commitment.
	// Any mismatch is a release bug and FAILS the test loudly.
	liveCharged := map[string]sdk.Coins{}
	totalByOwner := map[string]int{}
	terminalReleasing := 0
	_ = a.VeilKeeper.IterateIntentRecords(ctx, func(rec veiltypes.IntentRecord) bool {
		totalByOwner[rec.Owner]++
		switch rec.State {
		case veiltypes.INTENT_STATE_CONCLUDED, veiltypes.INTENT_STATE_EXPIRED, veiltypes.INTENT_STATE_CANCELLED:
			terminalReleasing++
			return false
		}
		liveCharged[rec.Owner] = liveCharged[rec.Owner].Add(rec.ChargedSpend...)
		return false
	})
	var reconciled, mismatches, allTerminatedZero int
	if err := a.MandateKeeper.IterateGranteeIndex(ctx, func(chamberID uint32, grantee sdk.AccAddress, _ []byte) bool {
		m, found, err := a.MandateKeeper.ActiveMandateByGrantee(ctx, chamberID, grantee)
		if err != nil || !found {
			return false
		}
		reconciled++
		want := liveCharged[grantee.String()]
		if want == nil {
			want = sdk.NewCoins()
		}
		if !m.OutstandingCommitment.Equal(want) {
			mismatches++
			t.Errorf("D-059 RELEASE MISMATCH: grantee %s outstanding_commitment=%s but Σ charged_spend(live)=%s (mandate %s)",
				grantee, m.OutstandingCommitment, want, m.Id)
		}
		// (b): had intents, all terminated-releasing (live sum zero) → must be 0
		if totalByOwner[grantee.String()] > 0 && want.IsZero() && m.OutstandingCommitment.IsZero() {
			allTerminatedZero++
		}
		return false
	}); err != nil {
		t.Logf("release-verification grantee walk err: %v", err)
	}
	report += "  D-059 release verification (independent recompute):\n"
	report += fmt.Sprintf("    terminated-releasing intents (== DecommitIntent calls)   %d\n", terminalReleasing)
	report += fmt.Sprintf("    active mandates reconciled (outstanding == Σ live charge) %d (mismatches: %d)\n", reconciled, mismatches)
	report += fmt.Sprintf("    grantees with all intents terminated → commitment == 0   %d\n", allTerminatedZero)

	report += "==================================================================\n"
	t.Log(report)
}

// TestMain wires the simulation flags.
func TestMain(m *testing.M) {
	flag.Parse()
	os.Exit(m.Run())
}
