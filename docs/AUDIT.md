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
