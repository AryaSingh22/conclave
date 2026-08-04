# Conclave Phase-0 Independent Audit

**Date:** 2026-06-15
**Commit audited:** `39e6509` (clean working tree, branch `main`)
**Environment:** WSL2 Ubuntu, Go 1.23.12, single-node localnet
**Auditor stance:** independent — did not build this code; verified Phase 0
against its own specification (docs/SPEC.md) adversarially, treating every
DECISIONS.md entry and commit claim as a hypothesis to falsify by reading the
code and running the acceptance scripts, not as fact.

---

## Verdict

**PHASE 0 — SOUND WITH FINDINGS.**

No BLOCKER and no MAJOR chain defect. The acceptance scripts pass under
independent re-run, and the core invariants were re-derived from the code (not
from DECISIONS) and hold. Four findings remain; none block the start of Phase 1
once the two pre-Phase-1 items below are owned.

---

## What was independently verified

### Part A — clean-room reproduction (all re-run by the auditor)
- **`make lint test`** — lint exit 0, test exit 0; 12 test packages green;
  **0 FAIL / 0 SKIP / 0 panic**. Simulation/determinism tests correctly gated
  behind `-Enabled` (D-057), excluded from `make test`.
- **`scripts/demo_e2e.sh`** — all 7 SPEC §1.3 steps pass end-to-end, including
  the step-7 split: with the solver killed, a 2-party swap clears via the
  in-consensus fallback (asserted **zero** solver solutions) while a fresh
  3-party cycle stays **unmatched** (proving `solver/cycles` is load-bearing).
  Over-ceiling intent rejected with `ErrMandateExceeded`; 3-party cycle
  CONCLUDED by the solver; receipts for alice/bob/carol; alice reputation → 0.5.
- **500-block simulation** — PASS (580 s), **non-vacuous**, no invariant break.
  Observed mix: CONCLUDED 522, EXPIRED 884, INVALID 70, ROLLED 0, SUBMITTED 0;
  sessions CLOSED 80; mandates 156 / 93 revoked; DecommitIntent calls 1501;
  91 grantees fully terminated → commitment == 0. The D-059 crisis
  commitment-conservation invariant held every block.
- **Same-seed determinism** — PASS. `TestAppStateDeterminism` completes with all
  seeds producing **byte-identical app hashes** across repeated runs (4 distinct
  seeds confirmed: `03C15441…`, `CC082E5F…`, `D693BFAD…`, `E5F1520E…`). See F-1
  for the harness-flakiness caveat — the *chain* is verified deterministic.

### Part B — invariants re-derived from code
- **Conservation** — `CheckConservation` is per-asset graph-zero bookkeeping;
  the three ledger laws are correctly distinct and code-matched (`applyLeg`):
  coin = value conservation, svc = authorized mint, data = authorized creation.
  Comments do not overclaim. No match that passes all six validation rules can
  rob a party below its own give-cap / want-floor (limits + exact price-binding +
  participant-only + no-unauthorized-asset + principal-safe skim).
- **Window/commitment (D-059)** — `Σ outstanding_commitment ==
  Σ charged_spend(live)` holds across rollover, amend, reissue, and bond. The
  over-commitment attack (commit to ceiling → force rollover → new intent) is
  rejected by the exposure check; `DecommitIntent` cannot clamp-lose;
  `window_spent` still resets per-period for settled spend (§6.2 step 5 intact).
- **Price binding** — exact LegacyDec zero (no rounding). The off-chain solver
  `gate` calls the **identical exported** `CheckPriceBinding` / `CheckConservation`
  — no drift. A non-terminating-price cycle (100/3) is rejected by both.
- **Order-then-reveal** — real: seal in EndBlock(h-1), reveal in BeginBlock(h);
  the phase-collapse guard test is load-bearing.
- **Determinism + receipt proof** — no map-iteration into state writes / wall
  clock / floats in any consensus path; fallback matcher deterministic; the
  ICS-23 receipt proof verifies against the committed root with working byte-flip
  and staleness negative cases (real ibc `VerifyMembership`, not a mock).

### Part C — conventions
- Naming canon clean; error codespaces match §13 (sigil 11xx … unordered 16xx);
  all `CONCLAVE-TODO(phase-N)` well-formed; six DECISIONS entries spot-checked
  against code with no divergence; no phase-1/2 TODO is a mislabeled Phase-0
  correctness gap (escrow conditions disabled **and** enforced-rejected).

### Part D — the four historically-found bugs
All four fixes are present with load-bearing regression tests: the goleveldb
replace pin; the custom (non-no-op) ProcessProposal
(`TestProcessProposalRejectsOverShareLane`); the TxBytes phantom-replay injection
(`TestUnorderedSameSequenceLaneSurvival`); and the window_spent conflation split
(`TestCommitmentSurvivesRolloverNoLeak`, `TestOverCommitmentRejectedAcrossRollover`).

---

## Findings

| ID | Severity | Ref | Finding |
|---|---|---|---|
| **F-1** | MINOR | §11.3 / 0.11 / D-057 | Chain determinism is **verified** (4 seeds byte-identical; `TestAppStateDeterminism` passes on completion). The **test harness is flaky**: it intermittently SKIPS on "empty validator set" (randomized sim-genesis depletes validators) and occasionally aborts early (~8 s), and self-seeds with un-seeded `rand.Int63()` (non-reproducible). Unreliable as a CI gate — not a chain defect. |
| **F-2** | MINOR | §6.2 step 4 / §1.1 goal 3 | `MandateDecorator.spendIntent` values only `MsgSend`/`MsgMultiSend`/`MsgSubmitIntent`; every other balance-moving msg (IBC `MsgTransfer`, `MsgDelegate`, …) returns **zero** spend, so the window ceiling is a no-op for them if a controller allowlists one. Spec-faithful (§6.2 step 4 names only those types), `MsgConclude` is gov-authority-only, but the leaked-key bound is weaker than the safety claim for non-valued types. Also: §5.1 `limit` rate accepted but ignored (D-049) — safe under Phase-0 fill-or-kill. |
| **F-3** | MINOR | D-020 / D-044 | DataGrant/ServiceVoucher give legs value at **0** against mandate ceilings; the price-source swap was deferred by D-044 to a "dedicated post-0.8 task" that is **not in the §10 ledger** — an unscheduled open `CONCLAVE-TODO(phase-0)` at Phase-0 exit. Bounded now (data = escrow stub, svc = mint with giver not debited, bond still counts). |
| **F-4** | NOTE → Phase-1 foundational | §6.3 | §6.3 frames order-then-reveal as a ProcessProposal check; in code ProcessProposal validates only lane structure (D-032), and the invariant is enforced by seal/reveal phase separation + OPEN-in-session gating. Equivalent in stub mode and the guard test is load-bearing, but Phase 1's real Veil must base the guarantee on **vote-extension share availability** (shares exist only in the vote extensions of the block that fixed ordering), not state-gating. |
| F-5 | NOTE | §11.6 | `.golangci.yml` header claims "no floats" but no linter bans float types (forbidigo only `^time\.Now$`). Zero float usages exist — review-enforced, not machine-enforced. |
| F-6 | NOTE | D-032 | The goleveldb replace pin has no automated regression guard (the determinism sim uses MemDB). A careless `go get -u` could silently drop it. |
| F-7 | NOTE | D-049 | §5.1 `limit` rate ignored; documented, safe under Phase-0 fill-or-kill quantity bounds. |
| F-8 | NOTE | — | Several `CONCLAVE-TODO(phase-0)` comments say "at task 0.8 …" as if future though 0.8 is complete and wired. Cosmetic. |

---

## Disposition of every finding (added 2026-07-28, D-090)

**Why this section exists.** The table above was written in 2026-06-15 and then
functioned as a *write-only* list. F-1…F-6 were picked up by the Phase-1 design
and the Phase-3 plan, but **F-7 and F-8 were never given an owner anywhere** — a
repo-wide grep at `aa21274` found zero mentions of either outside this file.
F-7 turned out to be closed by task 2.6 without anyone recording it against F-7;
F-8 stayed genuinely open for two phases and was **independently rediscovered**
by a marker sweep that did not know F-8 existed. That rediscovery is the proof
it was invisible.

> **The lesson, stated so it transfers: A FINDINGS LIST NEEDS THE SAME
> OWNER-PER-ROW DISCIPLINE AS A LEDGER, OR IT GOES WRITE-ONLY.** This is
> METHODOLOGY Rule 6's own counterexample — the D-044 "dedicated post-0.8 task"
> that was a phrase rather than a task number, floated across two phases, and
> came back as **F-3** — reproduced on the very list that recorded F-3.
> Rule 6 has been amended to say so explicitly, and this table now carries a
> disposition column that must never be blank.

| ID | Disposition | Status |
|---|---|---|
| **F-1** | Determinism-harness hardening → **task 1.0** (`a496e7e`, D-063) | ✅ **CLOSED.** The hardened gate then caught a real pre-existing mandate-accounting bug at seed 7, which became task 1.0b (D-064) — the gate paid for itself before the feature it was hardened for existed. |
| **F-2** | Unvalued msg types → **task 3.4** (PHASE3_PLAN §5.1) | 🔶 **OPEN, owned, and PINNED.** Task 2.5 (D-082) deliberately did not move it — real IBC vouchers make the exposure carry real value, so `TestSpendIntentF2BoundaryPin` asserts `MsgTransfer` values at zero BY NAME, forcing 3.4 to flip it via a deliberate red rather than by drift. |
| **F-3** | D-020 / D-044 PriceSource swap → **task 3.4** | 🔶 **OPEN, owned.** The index it will read now exists (2.5's session TWAP, D-082); 3.4 owns the decision to consume it, under D-082's recorded containment argument. The greppable markers are re-tagged `phase-3` and name 3.4 (D-090). |
| **F-4** | Order-then-reveal on vote-extension share availability → **task 1.6** (D-069) | ✅ **CLOSED by construction**, and strengthened: the ABCI++ off-by-one found during 1.6 means reveal is B+2, so a share needs the seal *committed*, not merely decided. Proven live on 4 validators in 1.7 (D-070). |
| **F-5** | Machine-enforced float ban → ~~task 3.3~~ **task 3.3a** (`make guards`, D-099) | ✅ **CLOSED — and the finding was righter than it knew.** F-5 said the config header claimed "no floats" while nothing enforced it. The obvious fix, a `forbidigo` rule, **does not work**: sabotage shows it fires on `var f float64` but MISSES struct fields, function params and `float64(x)` conversions — **identically on golangci-lint v1.64.8 and v2.12.2**. So the enforcing ban is `scripts/determinism_guards.sh`, watched red; forbidigo is a second layer whose limits are now written into `.golangci.yml` rather than implied by its header. **The same blind spot means the §11.6 wall-clock ban has never caught `return time.Now()` since Phase 0** — no live instance exists, but the guard was weaker than the record implied. AST replacement → task 3.15. |
| **F-6** | goleveldb pin regression guard → ~~task 3.3~~ **task 3.3a** (`make guards` G3, D-099) | ✅ **CLOSED.** G3 asserts the `replace` directive **and** the version that actually resolves — a replace can be present and overridden, so checking only the directive would have been presence-not-enforcement. Watched red by deleting the line. |
| **F-7** | §5.1 `limit` rate ignored (D-049) → **task 2.6** (`aa21274`, D-089) | ✅ **CLOSED — recorded here for the first time.** D-089 closed it under the name "D-049" and never mentioned F-7, so the finding sat closed-in-fact and open-on-paper. `checkLimitRate` now bounds each party's own achieved rate from flows alone; a shape whose rate is not computable is REJECTED (`ErrLimitRateUnsupported`) rather than settled against an unhonored limit. Note the original F-7 rationale — "safe under Phase-0 fill-or-kill" — was **half wrong**: 2.6's watched red showed the in-consensus fallback settling a party at a rate it never signed (state 4, CONCLUDED) once the clause was removed. |
| **F-8** | Stale `CONCLAVE-TODO(phase-0)` markers → **swept at PHASE2_DESIGN Rev 3** (D-090) | ✅ **CLOSED.** All 22 markers audited against Rule 6. Three that were the F-3/D-044 debt re-tagged `phase-3` and pointed at 3.4; two unowned ones given owners (3.5 for session-tx FinalizeBlock coverage, 3.3 for per-lane fee priority); four retired because the work completed (0.8's session-keeper swap and EndBlock hand-off, the counterparty-class predicate wiring, and 1.3/1.6's threshold implementation — where `StubScheme` now persists *deliberately* as the epoch-0 window, L1); the six `phase-3` Chambers markers now name **3.11** instead of relying on inference. **Not cosmetic after all:** the sweep is what surfaced that a completed design decision (StubScheme) had been wearing a TODO's clothing for two phases, which is how greppable-marker discipline decays into noise. |

---

## Phase-1 boundary triage

### PRE-PHASE-1 — entry criteria (resolve before threshold crypto begins)
- **F-1 — fix the determinism HARNESS.** Deterministic seeding; tune sim-genesis
  so the validator set survives the run. The chain is verified deterministic; the
  test is flaky. This becomes critical the moment Phase 1 adds vote extensions +
  DKG — the largest new determinism surface in the project — so the gate must be
  dependable *before* it is load-bearing. Cheap; do it first.
- **F-4 — Phase-1 foundational design task (not Phase-0 cleanup).**
  Order-then-reveal must rest on vote-extension share availability rather than the
  stub's state-gating. Carry F-4 as the **first item of the Phase-1 design doc**.
  The Phase-0 stub invariant is confirmed real and load-bearing; F-4 names exactly
  what must replace its enforcement.

### CARRY-FORWARD — should-resolve, do NOT block Phase 1
- **F-2** — bounded in Phase 0 (escrow conditions disabled). Keep the
  `CONCLAVE-TODO`s; logged as known.
- **F-3** — already deferred-by-design from the 0.8 Half-B decision (D-044).
  Schedule as the dedicated ante PriceSource-swap task, independent of Phase 1.

---

*Audit performed at `39e6509`; no code modified during the audit. This document is
the docs-only record of the verdict and the Phase-0 → Phase-1 entry criteria.*
