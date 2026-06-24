# Conclave Phase 2 — Provable Commerce + Edges (Design Document)

**Status:** Approved design (planning session, close of Phase 1 → Phase 2).
**Baseline:** HEAD `39cb207`, `make lint test` green (lint exit 0; all test
packages `ok`; 0 FAIL / 0 SKIP — the `-Enabled`-gated sim/determinism suite is
correctly excluded from `make test`, D-057). Phase-1 build (1.0–1.7) +
audit-scope (1.8) complete and pushed. The external cryptographic audit is the
**mainnet** gate, owned externally — it does **not** block Phase-2 build work.
**Scope:** Implement SPEC §10's five Phase-2 tasks — `x/escrow` delivery
conditions (2.1), CosmWasm/CVM integration with CONCLUDE + escrow bindings
(2.2), the x402 gateway (2.3) and 7683 resolver (2.4) edge services, and real
USDC over IBC + a session-TWAP fee (2.5) — plus a numbered matcher-richness
hardening pass (2.6) that owns the latent matcher TODOs. Exit: the §1.3 demo
extended end-to-end across chains (§6 below).
**Working method:** this document is the Phase-2 plan, reviewed before any build
task. No module code was written to produce it (only read-only greps to ground
its claims). Each task below is one working session with a stop-and-review and
its own plan-stop decisions. Where SPEC §10 is a one-line plan, this document
flags every under-specification and toolchain risk loudly rather than
improvising at build time.

---

## Three load-bearing flags surfaced up front

SPEC §10 Phase-2 is five sentences. Four places where it is underspecified or
carries dependency risk change the build plan and are recorded loudly here (and
will be recorded in DECISIONS at the task that resolves each) rather than
quietly reshaped:

- **FLAG A — gnark (ZK_DELIVERY) may collide with the kyber pin.** The §6.5
  `ZK_DELIVERY` class needs an on-chain groth16 verifier (gnark). gnark is
  heavyweight and tends to track a recent Go toolchain. Task 1.3 (**D-066**)
  deliberately held `drand/kyber` v1.3.1 + `kyber-bls12381` v0.3.1 by **avoiding
  a go-1.24 toolchain bump** (pre-v0.3.4 kyber, `golang.org/x/*` unchanged) —
  that pin is the foundation the whole Veil rests on. A gnark that forces a Go
  bump can therefore ripple into the crypto foundation. Resolution: **isolate
  `ZK_DELIVERY` into its own session (recommended split 2.1a / 2.1b)** and gate
  it on a gnark-Go-floor spike whose failure mode is a STOP, not a workaround
  (§2.1b). See also FLAG D.
- **FLAG B — the v0.53 flip is DECIDE-WHEN-REACHED, not now.** SPEC §14 D8 /
  **D-060** resolved "stay on SDK v0.50 through Phase 1" and named the
  Phase-1→Phase-2 boundary (the wasmd task) as the re-evaluation point. The flip
  conditions (a)/(b)/(c) depend on **then-current** block-sdk / ibc-go / wasmd
  releases, which will have moved by the time 2.2 runs. This document records
  *that the decision exists* and *that 2.2 is its trigger and owner* — it does
  **not** pre-bake a v0.50-vs-v0.53 outcome (§2.2). 2.5 shares the IBC-line
  sub-question (b).
- **FLAG C — the edge services (2.3, 2.4) are SEPARATE-REPO deliverables.** Per
  SPEC §9 both are off-chain gRPC clients of the node holding no
  consensus-critical logic. They are **not** in-repo modules; the in-repo build
  and `make test` must not expect their code. Their design-doc deliverable is the
  **node-side gRPC surface** each depends on, recorded as a stable contract — the
  service code and its tests live in `conclave-x402-gateway` /
  `conclave-7683-resolver` (§2.3, §2.4).
- **FLAG D — dependency currency is a per-task check.** wasmd, ibc-go, and gnark
  all move fast. Every place this doc names a version, the binding instruction is
  "**re-check the current release at task time**," not "use the version named
  here" (those are recorded only to make the flip-conditions concrete, the same
  way D-060 recorded its spike). The cross-cutting risk is the Go-toolchain-bump
  interaction (gnark/wasmd vs the kyber pin) that FLAGs A and B both touch.

---

## Section 1 — Phase-2 dependency spine

Phase 2 is sequenced by genuine dependency, mirroring SPEC §10's order:

```
2.1 x/escrow ───────────────► 2.2 wasmd/CVM ──┐
   (HASHLOCK→OPTIMISTIC          (CONCLUDE +    │
    →ZK_DELIVERY)                 escrow         │
        │                         bindings)      │
        │                            │           │
        │   ┌────────────────────────┘           │
        ▼   ▼                                     ▼
   2.3 x402 gateway        2.4 7683 resolver   2.5 IBC USDC + session-TWAP fee
   (separate repo)         (separate repo)     (shares IBC line with 2.2)
        └───────────────┬──────────────────────────┘
                        ▼
        2.6 matcher-richness hardening pass (owns the latent matcher TODOs)
                        ▼
        Phase-2 exit demo (§6): x402 → ZK-delivery → IBC-verified receipt
```

- **2.2 depends on 2.1** — the CVM custom bindings expose escrow msgs, which must
  exist first. 2.2 also owns the **D-060 v0.53 re-evaluation** (FLAG B).
- **2.3 / 2.4 depend on the node's gRPC surface only** (FLAG C); they can be
  built in their own repos against a running node once that surface is stable.
  2.3 settles in `uusdc` and runs on mock USDC until 2.5 brings the real denom.
- **2.5 shares the IBC-line question (b) with 2.2** and swaps the fee seam
  (`window.go:138`).
- **2.6 is independent of the commerce/edge work** and is sequenced last so the
  risky consensus-matcher changes land in one determinism-focused session.

---

## Section 2 — Per-task design

Each task is one working session with a stop-and-review and its own plan-stop
decisions, mirroring SPEC §10 and PHASE1_DESIGN §5. State machines carry an
explicit §11.2 transition-table acceptance criterion (illegal transitions
provably impossible).

### 2.1 — `x/escrow`: delivery conditions (HASHLOCK / OPTIMISTIC / ZK_DELIVERY)

**What it is.** Stand up the `x/escrow` module for real: keeper, `AppModule`,
msg server, genesis, and EndBlock sweeps. The error codespace `15xx` is already
reserved (CLAUDE.md §13). The protos already exist and are generated —
`escrow.proto` (the `DeliveryCondition` oneof, the `EscrowState` enum
`UNSPECIFIED/ACTIVE/RELEASED/REFUNDED/DISPUTED`, the `Escrow` record),
`tx.proto` (`MsgReleaseEscrow` / `MsgRaiseDispute` / `MsgSubmitDeliveryProof`),
`query.proto`, `genesis.proto`. 2.1 writes the **logic** SPEC §6.5 deferred:
the three condition classes as state machines, the record-ownership handoff from
x/session, the reputation dispute hook, and the D5 dispute-model ratification.

**Dependencies.**
- **Session-store escrow stubs** — `conclude.go:152` `openEscrowStub` writes a
  Phase-0 escrow entry per DataGrant leg (key `0x09|chamber|escrow_id`, state
  ACTIVE, no release logic) into the **x/session** store (D-045). The
  `CONCLAVE-TODO(phase-2)` there says "x/escrow takes ownership of these records
  and adds HASHLOCK/OPTIMISTIC/ZK_DELIVERY resolution." This is the ownership
  handoff 2.1 closes.
- **Reputation dispute hook** — `x/sigil/keeper/reputation.go`
  `PenalizeLostDispute` is already authored ("Called by x/escrow dispute
  resolution (Phase 2)"); 2.1 wires the OPTIMISTIC dispute path to it.
- **x/receipt** reads escrow/receipt records (D-045) and must keep working across
  the ownership handoff (ICS-23 proofs unaffected).
- Phase-1 complete (the Veil is real; DataGrant legs flow through a real Reveal).

**Acceptance criteria.**

1. **Module wired:** keeper + `AppModule` + msg server + genesis registered in
   `app/`; `15xx` errors in `x/escrow/types/errors.go`; every exported keeper
   method cites its SPEC §6.5 clause (§13).
2. **HASHLOCK state machine (§11.2 transition table):**

   | From | Event | To | Guard |
   |---|---|---|---|
   | ACTIVE | `MsgReleaseEscrow{preimage}` | RELEASED | `H(preimage) == payload_hash` ∧ height < timeout |
   | ACTIVE | EndBlock sweep | REFUNDED | height ≥ `created_height + timeout_blocks` |

   Illegal (asserted impossible): release with wrong preimage; release at/after
   timeout; `MsgRaiseDispute` on a HASHLOCK; release from RELEASED/REFUNDED
   (double-release / post-refund).
3. **OPTIMISTIC state machine (§11.2 transition table):**

   | From | Event | To | Guard |
   |---|---|---|---|
   | ACTIVE | EndBlock sweep | RELEASED | height ≥ `created_height + dispute_window_blocks` ∧ no dispute |
   | ACTIVE | `MsgRaiseDispute` | DISPUTED | height < window end |
   | DISPUTED | hash-mismatch proven | REFUNDED | + seller-bond slash + `PenalizeLostDispute` |
   | DISPUTED | challenge fails (hash matches) | RELEASED | — |

   Illegal: dispute after the window; auto-release while DISPUTED; double-dispute;
   release/refund from a terminal state.
4. **ZK_DELIVERY state machine (§11.2 transition table):**

   | From | Event | To | Guard |
   |---|---|---|---|
   | ACTIVE | `MsgSubmitDeliveryProof{proof}` | RELEASED | groth16 verifies `proof` under `vk_id` over `schema_commitment` |
   | ACTIVE | EndBlock sweep | REFUNDED | timeout reached, no valid proof |

   Illegal: release without a proof; release on an invalid proof; replay of a
   spent proof; release from a terminal state.
5. **Record-ownership handoff:** the `conclude.go:152` TODO is closed — escrow
   records are owned by `x/escrow` (the chosen mechanism is a 2.1 plan-stop
   decision, below); x/receipt's reads and ICS-23 proofs still resolve.
6. **Determinism + safety:** groth16 verification and preimage hashing are
   fresh-randomness-free and deterministic on-chain (§11.6); every
   `MsgSubmitDeliveryProof` / `MsgReleaseEscrow` proof/preimage path wraps
   `recover → INVALID` and never panics on user input (§11.5 — a panic here is a
   release blocker).
7. `make lint test` green; per-keeper table tests (happy + ≥2 failure paths) and
   the three transition tables present (§11.1–§11.2).

**Decisions for the 2.1 plan-stop.**
- **D5 ratification (DECISIONS entry when 2.1 runs):** OPTIMISTIC dispute
  resolution is **hash-mismatch challenge + seller bond only**; subjective
  arbitration is explicitly out of scope (SPEC §14 D5 default). Ratified, not
  re-opened.
- **Ownership-handoff mechanism:** in-place migration of the `0x09` session-store
  stubs into an x/escrow store at the upgrade boundary, **vs.** x/escrow becomes
  the writer from `conclude.go` going forward (session stops writing `0x09`).
  Whichever is chosen must keep x/receipt's reads valid.
- **Seller-bond source and sizing** for the OPTIMISTIC dispute slash (does the
  bond come from the escrowed `amount`, a separate seller bond posted at escrow
  creation, or the intent bond?) and the slash fraction.
- **Hashlock hash function + preimage encoding** (`payload_hash` is `bytes`; pin
  the hash and the receipt-of-access encoding).
- **`vk_id` registry:** where verification keys live — a genesis param map, an
  on-chain registry msg, or a compiled-in set — and whether `vk_id` is
  governance-gated.
- **Timeout / auto-refund sweep:** the `(state, timeout_height)` index and the
  EndBlock sweep that fires HASHLOCK/ZK refunds and OPTIMISTIC auto-releases
  deterministically (§5.2/§5.3 index discipline).
- **Session split (recommend-and-confirm, see 2.1a/2.1b):** confirm at the
  plan-stop whether `ZK_DELIVERY` is its own session, gated on the gnark spike.

#### 2.1a — HASHLOCK + OPTIMISTIC (recommended first session)

Pure in-repo work on the stable v0.50 baseline: no new heavyweight dependency,
no toolchain risk. Delivers the module skeleton, the ownership handoff, the
reputation-hook wiring, the D5 ratification, and the first two condition-class
state machines (HASHLOCK, OPTIMISTIC) with their §11.2 transition tables. This
is the session that follows directly from the approved doc.

#### 2.1b — ZK_DELIVERY (separate session, gnark-spike-gated)

`ZK_DELIVERY` adds the gnark groth16 verifier — the one Phase-2 dependency that
can collide with the crypto foundation (FLAG A). 2.1b **begins** with a
throwaway spike establishing gnark's Go-toolchain floor and its interaction with
the D-066 kyber pin (`kyber-bls12381` v0.3.1, held by avoiding a go-1.24 bump).
The spike's outcome branches the session, and the failure mode is a **STOP**,
not a workaround — a dependency conflict with the foundation the whole Veil
rests on is a decision, not an obstacle to route around:

- **(a) gnark works on the current toolchain** (no Go bump, kyber pin intact) →
  **proceed.** Implement the verifier, the `vk_id` registry, and the
  ZK_DELIVERY state machine.
- **(b) gnark needs a newer Go that kyber also tolerates** → **proceed with the
  bump, recorded.** Note the toolchain change in DECISIONS, re-run the
  determinism vector + the full crypto suite to confirm the kyber path is
  byte-identical under the new toolchain, then implement.
- **(c) gnark needs a Go that BREAKS the kyber pin** → **STOP and decide.**
  Surface the conflict as a plan-stop decision with options — a different/older
  gnark line, a kyber version that tolerates the new toolchain (re-validating the
  threshold-encryption construction against any kyber API change), or **defer
  ZK_DELIVERY** to a later session/Phase-3 with the foundation untouched. Do not
  push the bump through against the crypto pin.

Acceptance for 2.1b is the ZK_DELIVERY transition table (above) + a determinism
re-confirmation of the crypto path under whatever toolchain the spike selects.

### 2.2 — wasmd + CVM custom bindings

**What it is.** Integrate CosmWasm (`wasmd`) and expose CONCLUDE + escrow to
contracts via custom bindings. SPEC §0 names the contract runtime **CVM**; the
seam is `session/types/tx.pb.go:322` — "`MsgConclude` … authority must be the
module authority; CONCLAVE-TODO(phase-2): exposed to CVM via custom bindings."
2.2 also **owns the D-060 v0.53 re-evaluation** (FLAG B): the wasmd integration
is exactly the dependency revisit D-060 named as its trigger.

**Dependencies.** 2.1 (escrow msgs must exist for the bindings to expose); the
D-060 flip-condition inputs (then-current block-sdk / ibc-go / wasmd).

**Acceptance criteria (shape — the dependency outcome is spike-gated).**
1. **D-060 spike-then-decide (first sub-task, FLAG B):** re-check D-060's three
   flip conditions against **then-current** releases — (a) an MVS-clean block-sdk
   release targeting SDK v0.53 exists; (b) a v0.53-compatible ibc-go line is
   adopted (shared with 2.5); (c) the D-057 sim/FinalizeBlock lifecycle is intact
   or cheaply replaced. Record the result as a **new DECISIONS entry updating
   D-060** — the doc does not pre-bake the outcome. The decision is "v0.50 vs
   v0.53 for Phase 2," made on data current at task time.
2. **wasmd wired** on whichever SDK line the spike selects, with the laned
   mempool (§6.7) and determinism (§11.6) preserved; full regression of the
   Phase-0/1 suites on the chosen dependency baseline.
3. **CVM custom bindings:** a contract can query open sessions / receipts and
   invoke escrow msgs, under a **re-examined CONCLUDE authority model** —
   `MsgConclude` is module-authority-only today; bindings must not let an
   arbitrary contract forge a CONCLUDE (the all-or-nothing settlement invariant,
   §6.4, must hold). Gas metering defined for CVM → native-module calls.
4. `make lint test` green on the chosen dependency baseline.

**Decisions for the 2.2 plan-stop.**
- **v0.50 vs v0.53** (the re-evaluated D-060), with the spike data attached.
- **wasmd version** + whether it forces a Go-toolchain bump (FLAG A adjacency —
  if 2.1b already bumped, this is consistent; if not, re-check against the kyber
  pin).
- **Binding surface + contract-permission model:** which msgs/queries are
  exposed, and the authority model that prevents contract-forged CONCLUDE/escrow.
- **Gas metering** for contract calls into native modules.

### 2.3 — x402 gateway (SEPARATE REPO: `conclave-x402-gateway`)

**What it is.** An off-chain x402 facilitator (SPEC §9). Outward it speaks
Coinbase's x402 HTTP handshake (402 → `X-PAYMENT` → verify → `X-PAYMENT-RESPONSE`);
inward it submits intents/transfers on Conclave on behalf of the paying agent,
**fronts gas** (gasless for clients), and settles in `uusdc`. Stateless except a
hot wallet + replay-nonce store. **This is a separate-repo deliverable** (FLAG
C) — an off-chain gRPC client, not an in-repo module; the in-repo build does not
expect its code.

**Dependencies (node-side surface this design records as a stable contract).**
- Tx submission for `MsgSubmitIntent` (and transfers) on behalf of a paying
  agent, via a **Mandate-bound gateway key** (the §6.2 ante enforces the ceiling;
  fee-abstraction, §6.2 `FeeAbstractionDecorator`, lets the gateway front gas).
- Intent / session / receipt **gRPC queries** (§6.6 `Receipts`, `Receipt`,
  `ReceiptProof`) for outward `X-PAYMENT-RESPONSE` settlement confirmation.
- `uusdc` settlement denom (mock until 2.5 supplies the real IBC USDC denom).

**Acceptance criteria.** The design doc's in-repo deliverable for 2.3 is the
**node-side gRPC surface contract** (the bullets above), confirmed stable and
documented for the separate-repo team. The gateway's own implementation and
tests live in `conclave-x402-gateway` and are **not** part of `make test`.

**Decisions for the 2.3 plan-stop (most live in the gateway repo).** Hot-wallet +
replay-nonce model; which Mandate-bound key the gateway operates under and its
ceiling; the gas-fronting accounting (how fronted CLV gas reconciles against
`uusdc` settlement).

### 2.4 — 7683 resolver (SEPARATE REPO: `conclave-7683-resolver`)

**What it is.** Consumes ERC-7683 orders from EVM origin chains, translates them
into `SealedIntent`s (the resolver pattern — 7683 standardizes how solvers
consume orders, not the settlement venue), and posts fills back via a
per-origin-chain verification bridge (SPEC §9). **Separate-repo** (FLAG C),
off-chain gRPC client; not an in-repo module.

**Dependencies (node-side surface this design records).**
- `MsgSubmitIntent` submission (the translated SealedIntent), including a
  `spend_commitment` derived from the 7683 order (§4/D-065 — the commitment is
  the give-leg upper bound the ante binds).
- The receipt / ICS-23 **proof surface** (§6.6 `ReceiptProof`) so a settled fill
  can be proven back to the origin-chain bridge.

**Acceptance criteria.** The in-repo deliverable is the node-side surface
contract (submission + receipt proofs for fill-back verification); the resolver
implementation and tests live in `conclave-7683-resolver` and are not part of
`make test`.

**Decisions for the 2.4 plan-stop (most in the resolver repo).** Per-origin-chain
bridge mechanism; the 7683-order → SealedIntent translation, including how the
order's value maps to `spend_commitment` and which counterparty class it targets.

### 2.5 — Real USDC via IBC + session-TWAP fee

**What it is.** Two coupled changes. (1) Replace mock `uusdc` with **real USDC
over IBC** (an `ibc/<hash>` denom). (2) Replace the **fixed
`fee_conversion_rate`** in the §6.2 `FeeAbstractionDecorator` with a **TWAP
derived from session clearing prices** — the seam is `mandate/keeper/window.go:138`:
"task 2.5 replaces the fixed `fee_conversion_rate` with a TWAP from session
clearing prices."

**Dependencies.** An IBC line — **shares sub-question (b) with 2.2** (a
v0.53-compatible ibc-go adoption, if 2.2 flipped; otherwise the v0.50 ibc-go v8
line). Session `clearing_prices` (already written on receipts, §6.6). The
`ExecuteFeeAbstraction` seam (`window.go`).

**Acceptance criteria.**
1. **IBC USDC:** an IBC transfer channel brings real USDC; the `ibc/<hash>` denom
   is plumbed through the mandate ceilings and the price source that value it.
2. **Session TWAP:** `fee_conversion_rate` is derived from session clearing
   prices over a window, computed **float-free** (`sdk.Dec` / `math.Int`,
   §11.6), deterministic, and persisted/queryable.
3. **Seam swap:** `ExecuteFeeAbstraction` (`window.go:138`) consumes the TWAP
   instead of the fixed param; the `CONCLAVE-TODO(phase-2)` there is closed.
4. **Cold-start:** the TWAP needs price history — fall back to the fixed rate
   until enough sessions have cleared (a defined warm-up), deterministically.
5. `make lint test` green; TWAP math has its own determinism test.

**Decisions for the 2.5 plan-stop.**
- **TWAP window length** and which price pair(s) it averages (USDC/CLV).
- **Cold-start bootstrap** threshold and the fixed-rate fallback semantics.
- **IBC version** (shared with 2.2's sub-question (b)).
- **Denom + price-source handling:** real `ibc/<hash>` USDC vs the mock-`uusdc`
  migration, and whether real USDC changes the `FaceValuePriceSource` story
  (AUDIT F-2/F-3; the D-044 price-source swap remains a separate, non-2.5 task
  unless 2.5's denom work forces it — flag, don't silently expand).

### 2.6 — Matcher-richness hardening pass (owns the latent matcher TODOs)

**What it is.** A single, determinism-focused session that owns the five latent
`CONCLAVE-TODO(phase-2)` markers in the matching engine (§5 disposition table).
These are matching-engine **richness** items sitting on consensus-critical
(`fallback`, `solution`, `skim`) or solver (`cycles`) code — not one of SPEC
§10's five named tasks, and not a dependency of the commerce/edge tasks.
Clustering them keeps risky consensus-matcher changes out of the commerce tasks
("don't stack hard things") and lets one session re-run the determinism gate
over the whole matcher-richness surface at once.

**Dependencies.** Independent of 2.1–2.5; sequenced after them so the commerce
work lands first. (#10's multi-asset skim carries a cross-reference to 2.5 — see
§5.)

**Acceptance criteria (one-line shape, to be expanded at its own plan-stop).**
The five matcher TODOs (partial-fill/cycle-aware fallback, partial-fill cycles +
alternate numeraire anchoring, §5.1 limit-rate honoring, multi-intent-per-sigil,
multi-asset in-kind skim + in-kind payout) implemented behind the **same
`validateSolution` bar** and the **same determinism vector** (§11.3) that guard
the matcher today — every node computes the richer matcher identically; the
determinism harness (1.0) is green over the new surface. This is the gate the
clustering buys.

---

## Section 3 — §11.2 transition-table requirement (why escrow gets three tables)

SPEC §11.2 requires every state machine — "intent, session, mandate window,
**escrow**" — to carry an explicit transition-table test asserting illegal
transitions are impossible. The `EscrowState` enum
(`UNSPECIFIED/ACTIVE/RELEASED/REFUNDED/DISPUTED`) is shared across the three
condition classes, but each class admits a **different** legal sub-graph:
HASHLOCK never reaches DISPUTED; OPTIMISTIC is the only class that uses DISPUTED;
ZK_DELIVERY gates RELEASED on a proof, not a preimage or a timer. Modeling them
as three separate transition tables (§2.1 criteria 2–4) — rather than one
permissive table over the union — is what makes the §11.2 "illegal transitions
impossible" assertion meaningful: a preimage cannot release a ZK escrow, a
dispute cannot touch a HASHLOCK, a timed-out escrow cannot release. RELEASED and
REFUNDED are terminal in every class.

---

## Section 4 — Dependency-currency and toolchain flags (per-task, at task time)

Recorded so no task assumes a stale version (FLAG D):

- **2.1b (gnark):** spike the Go-toolchain floor vs the D-066 kyber pin **before**
  committing; outcome branch (a)/(b)/(c) in §2.1b, (c) is a STOP.
- **2.2 (wasmd / block-sdk / ibc-go):** the D-060 re-evaluation is the spike;
  check current releases at task time, not the versions D-060 recorded
  (cosmos-sdk v0.50.15 / ibc-go v8.8.0 / block-sdk v2.1.5 were the
  Phase-1-boundary snapshot). Re-check the wasmd Go floor against the kyber pin.
- **2.5 (ibc-go):** the IBC-line adoption shares 2.2's sub-question (b); confirm
  the chosen line at task time.
- **Cross-cutting:** the single highest toolchain risk across Phase 2 is a Go
  bump (gnark or wasmd) breaking `kyber-bls12381` v0.3.1. Both FLAG A and the
  2.2 wasmd check route to the same pin; whichever task bumps first records it,
  and the other re-confirms the crypto determinism vector under the new
  toolchain.

---

## Section 5 — Latent-TODO disposition (completeness — nothing floats)

Every `CONCLAVE-TODO(phase-2)` marker in the source tree (non-test), each given
an explicit disposition. The five matcher items are **folded into the numbered
task 2.6**, not parked behind a phrase — a future session can pick 2.6 up as a
real ledger entry.

| # | Marker | Location | Disposition |
|---|---|---|---|
| 1 | escrow takes ownership of records + adds HASHLOCK/OPTIMISTIC/ZK_DELIVERY resolution | `x/session/keeper/conclude.go:152` | **FOLD → 2.1** |
| 2 | reputation dispute hook (`PenalizeLostDispute`, "called by x/escrow dispute Phase 2") | `x/sigil/keeper/reputation.go` | **FOLD → 2.1** (wiring; hook already authored) |
| 3 | `MsgConclude` exposed to CVM via custom bindings | `x/session/types/tx.pb.go:322` | **FOLD → 2.2** |
| 4 | fixed `fee_conversion_rate` → session TWAP ("task 2.5") | `x/mandate/keeper/window.go:138` | **FOLD → 2.5** |
| 5 | replace the sigverify fork with native ADR-070 (v0.53) | `app/unordered/sigverify.go:16` | **FOLD → 2.2** (the D-060 flip re-evaluation) |
| 6 | partial-fill and cycle-aware fallback | `x/session/keeper/fallback.go:35` | **FOLD → 2.6** |
| 7 | partial-fill cycles + alternate numeraire anchoring | `solver/cycles/cycles.go:89` | **FOLD → 2.6** (off-chain twin of #6) |
| 8 | honor §5.1 limit rate at stated clearing prices | `x/session/keeper/solution.go:330` | **FOLD → 2.6** |
| 9 | allow multi-intent-per-sigil via intent-tagged legs | `x/session/keeper/solution.go:141` | **FOLD → 2.6** |
| 10 | multi-asset in-kind skim + in-kind solver payout | `x/session/keeper/skim.go:37` | **FOLD → 2.6**; **2.5 re-confirms** skim/fee coherence after the TWAP fee-source swap |

**On #6–10 → 2.6 (the re-deferral reasoning).** All five are matching-engine
richness on consensus-critical or solver code. None is one of SPEC §10's five
named Phase-2 tasks (they were marked `phase-2` = "not Phase 0/1," not "one of
the five"), and none blocks the commerce/edge tasks. Rather than scatter risky
consensus-matcher changes across 2.1–2.5, they cluster into the numbered task
**2.6**, a single determinism-focused pass behind the existing `validateSolution`
bar and determinism vector (§2.6). **#10's cross-reference to 2.5:** the
multi-asset skim is matcher capability (the D-051 numeraire-only narrowing), not
2.5's fee-source plumbing — so it stays in 2.6, but when 2.5 swaps the fee source
to a TWAP it must re-confirm the skim narrowing is still coherent under the new
fee model.

(Markers #1–5 are listed for completeness; they are not "latent" — each folds
into a named §10 task above.)

---

## Section 6 — Phase-2 exit criterion

SPEC §10 Phase-2 exit: *"the §1.3 demo extended — external x402 agent buys
ZK-verified data from a Conclave agent; receipt verified on a second Cosmos chain
over IBC."* This single integration proves the phase and spans four tasks:

- **2.3 (x402 gateway)** — the external x402-speaking agent pays in without
  knowing Conclave exists; the gateway fronts gas and submits the intent.
- **2.1 (ZK_DELIVERY escrow)** — the Conclave agent sells data under a
  `ZK_DELIVERY` condition; release is gated on the on-chain groth16 proof that
  the delivered ciphertext matches the committed schema/hash (so "ZK-verified
  data" is literal, not a label).
- **2.2 (wasmd/CVM)** — if the selling Conclave agent is a CVM contract (or the
  flow routes through a contract), the custom bindings drive the
  escrow/CONCLUDE path; 2.2 is on the exit path because the demo exercises the
  contract-to-native binding surface.
- **2.5 (IBC USDC + receipt)** — settlement is in real IBC USDC, and the
  settlement **Receipt** is IBC-verified (ICS-23) on a second Cosmos chain — the
  §6.6 receipt-proof surface consumed across an IBC connection.

**2.4 (7683 resolver) is deliberately not on this exit path** — it is the
parallel *inbound* rail (EVM-origin orders) and is proven by its own repo's
integration, not by the x402 exit demo. The Phase-2 exit is the x402 → ZK →
IBC-receipt chain across 2.1 + 2.2 + 2.3 + 2.5; 2.6 hardens the matcher beneath
all of it but is not part of the exit assertion.

---

## Consolidated FLAGS and decisions (SPEC §10 under-specifications, with resolutions)

1. **gnark may collide with the kyber pin** — ZK_DELIVERY isolated to 2.1b,
   gnark-Go-floor spike with a STOP failure branch (FLAG A, §2.1b).
2. **v0.53 flip is decide-when-reached** — owned by 2.2, re-checked against
   then-current releases, not pre-baked (FLAG B / D-060, §2.2).
3. **Edge services are separate-repo** — 2.3/2.4 deliver a node-side gRPC
   contract in-repo; the service code lives in its own repo and is not in
   `make test` (FLAG C, §2.3/§2.4).
4. **Dependency currency is a per-task check** — wasmd/ibc-go/gnark re-checked at
   task time; the Go-toolchain-bump-vs-kyber-pin risk is the cross-cutting one
   (FLAG D, §4).
5. **D5 dispute model** — hash-mismatch + seller bond only; subjective
   arbitration out of scope (ratified at the 2.1 plan-stop, §2.1).
6. **Escrow record ownership** — the `conclude.go:152` handoff closes in 2.1; the
   migration-vs-rewriter mechanism is a 2.1 plan-stop decision (§2.1, §5 #1).
7. **The five matcher TODOs are owned by the numbered task 2.6**, not a floating
   phrase; #10 carries a 2.5 coherence cross-reference (§2.6, §5).

---

*Phase-2 design approved at the Phase-1 → Phase-2 boundary (baseline `39cb207`).
Task 2.1a (`x/escrow` HASHLOCK + OPTIMISTIC) begins the Phase-2 build in a fresh
session on this approved doc. The conclave convenes.*
