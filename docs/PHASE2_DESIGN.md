# Conclave Phase 2 — Provable Commerce + Edges (Design Document)

**Status:** Approved design (D-072), **Rev 3 — reconciliation audit**
(2026-07-28, D-090). Rev 1 was authored at the Phase-1→Phase-2 boundary
(baseline `39cb207`) before any Phase-2 build work; Rev 2 (2026-07-02, D-078)
reconciled it to as-built state after 2.1a/2.1b with 2.2 in flight.

**Why Rev 3.** Between Rev 2 and `aa21274`, five more tasks landed (2.2, 2.5,
2.3, 2.4, 2.11, 2.6) and five new numbered tasks were created (2.8–2.12) — but
this document only ever received the §5 disposition rows. A reconciliation
audit found §0, §1, §2.3/§2.4/§2.6, §6 and the FLAGS section still describing a
tree that is five tasks old: the scoreboard called **2.3, 2.4 and 2.6 "not
started" when all three had shipped**, and tasks 2.8–2.12 had no rows at all.
Because CLAUDE.md names this document the phase authority every Phase-2 session
bootstraps from (Rule 1), a stale scoreboard does not merely misinform — it
would lead a discipline-faithful session to re-plan committed work. Rev 3 is
that correction, plus the ownership collisions the audit surfaced (§7). The
external cryptographic audit stays the **mainnet** gate, owned externally — it
does **not** block Phase-2 build work.

**Scope:** SPEC §10's five Phase-2 tasks — `x/escrow` delivery conditions (2.1),
CosmWasm/CVM integration with CONCLUDE + escrow bindings (2.2), the x402 gateway
(2.3) and 7683 resolver (2.4) edge services, real USDC over IBC + a session-TWAP
fee (2.5) — **all five DONE** — plus seven numbered follow-on tasks: the
matcher-richness hardening pass (2.6, **done**), the store-iterator fix (2.11,
**done**), and the five open ones (2.7 escrow consent completeness, 2.8
submit-charge structural fork, 2.9 receipt condition-outcome integrity, 2.10
in-consensus cycle-aware fallback, 2.12 multi-asset in-kind skim). Exit: the
§1.3 demo extended end-to-end across chains (§6 below) — **now unblocked: all
four exit legs are green.**
**Working method:** unchanged — one task per session with a stop-and-review and
its own plan-stop decisions, each recorded in DECISIONS.md in the task that
makes it. Where this doc names a version, the binding instruction remains
"re-check the current release at task time" (FLAG D), except where a pin is now
load-bearing and recorded (§4).

---

## Section 0 — Scoreboard (result state, one line per task)

| Task | Deliverable | Status | Record |
|---|---|---|---|
| 2.0 | Phase-2 design ledger authored (this document, Rev 1) | **DONE** | `ee70722`, D-072 |
| 2.1a | `x/escrow` HASHLOCK + OPTIMISTIC, ownership handoff, D5 ratified | **DONE** | `22bdc98`, D-073/074/075 |
| 2.1b | ZK_DELIVERY (gnark groth16), buyer-side consent gate | **DONE** | `4122fda`, D-076 |
| 2.2 | wasmd + CVM bindings (read-only CONCLUDE + escrow allowlist) | **DONE** — wasmd v0.54.9 wired, bindings + forge-prevention e2e green; localnet contract smoke deferred to the §6 exit demo (needs a CVM-schema contract binary) | `9722912`, D-077 + D-079 |
| 2.5 | IBC USDC + session-TWAP fee (swaps `window.go:138`); owned the NEXT v0.53 checkpoint | **DONE** — stay ibc-go v8.8.0 (posture under P3-B); session-TWAP index + fee seam live, vector re-baselined; two-chain ICS-20 e2e; v0.53 checkpoint re-failed on (a); live relayer demo = §6 exit demo | `dccd607`, D-081 + D-082 |
| 2.3 | x402 gateway — in-repo deliverable = node-side surface contract | **DONE** — node surface verdict ALREADY COMPLETE (no new gRPC); contract + app conformance + live sweep shipped; G1 rebind hole FIXED, G2 strand hole MITIGATED | `8756575`, D-083/084/085 · artifact: `docs/edge/X402_SURFACE.md` |
| 2.4 | 7683 resolver — in-repo deliverable = surface + proof contract | **DONE** — surface complete after ONE gap the conformance test found (fallback clearings now persist their winning Solution); G1(2.4) `condition_outcome` disclosed + neutralized | `c830e55`, D-086/087 · artifact: `docs/edge/7683_SURFACE.md` |
| 2.11 | Store-iterator range-end correctness (x/mandate, x/sigil) | **DONE** — split out of 2.6 at a STOP and landed FIRST; 8 sites, 2 LIVE-broken (one on the §6.4 rule-5 consensus path); vector byte-identical | `d34ede0`, D-088 |
| 2.6 | Matcher-richness hardening (3 of the 5 matcher TODOs closed; 2 split out) | **DONE** — §5.1 limit RATE honored (closes D-049/F-7), partial-fill cycles + alternate anchoring, 2-party partial-fill fallback; all three strictly ADDITIVE, vector byte-identical three times | `aa21274`, D-089 |
| 2.7 | Escrow consent completeness (FW-1 + FW-2, incl. the items re-pointed here by D-089) | **open** | D-078, D-089 |
| 2.8 | Submit-charge STRUCTURAL fork (design question only — the G2 security hole is CLOSED in 2.3) + the timeouts→params promotion | **open** | D-085 |
| 2.9 | Receipt condition-outcome integrity (`CONDITION_OUTCOME_PENDING` + escrow write-back) | **open** | D-087 |
| 2.10 | In-consensus cycle-aware fallback (the second half of `fallback.go:35`) | **open** | D-089 |
| 2.12 | Multi-asset in-kind skim + in-kind solver payout (`skim.go:37`) | **open** | D-089 |
| 2.13 | Solver-side condition attachment + the F-1 registered-error-code loss | **open** (NEW in §6a) | D-091 |
| §6a | Exit demo part A — x402 ingress + ZK escrow + CVM release, LIVE | **DONE** — D-079 discharged (a real contract released a real ZK escrow with a real proof); the condition system's only live writer is DEMO tooling (→ 2.13) | D-091 · artifact: `docs/EXIT_DEMO.md` |
| §6b | Exit demo part B — live-relayer IBC USDC + second-chain receipt verification | **DONE — THE §6 EXIT CRITERION IS ASSERTED.** Real ICS-20 uusdc over a live `cosmos/relayer` v2.6.0 process; the buy settles in the `ibc/<hash>` voucher end to end; the receipt ICS-23-verified against chain B's on-chain light-client root of chain A, with the honest B→A / A→B boundary stated. **D-082 DISCHARGED** | D-092 · artifact: `docs/EXIT_DEMO.md` |

> ## PHASE-2 EXIT CRITERION: **MET** (§6b, D-092)
>
> Every task on the exit path is complete. The remaining open tasks
> (2.7–2.10, 2.12, 2.13) are each recorded as off the exit path, so
> **PHASE3_PLAN task 3.0 — the Phase-2 boundary re-validation — is now
> TRIGGERED.** Its stated trigger was this demo. 3.0 owns the Rule-9
> authored-surface re-derivation and the §5.1 disposition re-verification.
>
> The run is **INTERNAL** (D-090): task 2.9 remains a prerequisite before any
> external-facing use of this evidence.

**Exit-demo leg readiness (§6): all four legs are now DONE.** ZK_DELIVERY
escrow leg (end-to-end conditioned ZK CONCLUDE proven: both parties signing
opens a funded escrow, a bound proof releases it — `TestConcludeConditioned_Zk`);
CVM leg **DONE in-process** (2.2, D-079: a real contract releases a HASHLOCK
escrow through the bindings under wasmvm; the LIVE-localnet slice moved to the
demo itself, which owns the purpose-built CVM-schema demo contract); x402
ingress (2.3, D-083 — the auth model proven at app level and swept live); IBC
settlement/receipt (2.5, D-082 — two-chain ICS-20 e2e; the live relayer slice
moved to the demo). **2.4 and every one of 2.7–2.12 are off the exit path** —
each says so in its own row — so the demo is the only remaining phase-closing
item. It carries two deferred obligations that have no other home: the D-079
localnet contract-release smoke and the D-082 live relayer USDC demo.

**Condition on the demo (D-090, decided at this Rev, not a note):** the demo's
x402 leg is precisely the conditioned ZK buy that hits **D-087** — its receipt
will read `condition_outcome = NONE` for a conditioned settlement. That is
acceptable for an **internal acceptance run**, because §6.4 of
`docs/edge/7683_SURFACE.md` and §6 of `X402_SURFACE.md` disclose the true
semantics, so no consumer is misled. **If the demo output is ever repurposed as
EXTERNAL-FACING evidence, task 2.9 becomes a prerequisite** — that is a hard
condition at the demo's plan-stop, not a caveat to weigh.

---

## The four load-bearing flags — Rev 3 disposition

Rev 1 surfaced four places where SPEC §10 was underspecified or carried
dependency risk. Two are now resolved with recorded outcomes; two remain live
(FLAG C is now DISCHARGED — both artifacts exist and are conformance-pinned):

- **FLAG A — gnark vs the kyber pin: RESOLVED (D-076, spike outcome (a)).**
  gnark works on the CURRENT toolchain with **gnark v0.13.0 + gnark-crypto
  v0.18.0 pinned** (floor go 1.23.0). The collision Rev 1 feared is real but
  avoided, not gone: gnark v0.14.0 needs go 1.24.0 (the exact bump D-066
  avoided) and v0.15.0 needs go 1.25.7. The determinism vector was re-run after
  the dependency add and came back byte-identical. Consequence: the toolchain
  ceiling is now guarded by **two** pins, not one (§4).
- **FLAG B — the v0.53 flip: RESOLVED (D-077, 2.2 STOP #1).** Stay on SDK
  v0.50 for Phase 2; integrate wasmd on the v0.50 line (recommended **wasmd
  v0.54.x** — SDK v0.50.11 + ibc-go v8, matching our stack; v0.55.1 only at
  the cost of a v8→v10 IBC major, a STOP #2 detail). The answer matches D-060
  but the binding reason MOVED: flip-condition (a) still fails because
  **block-sdk is frozen at v2.1.5 with no v0.53 path** (bumping means losing
  the §6.7 laned mempool), and axis (e) showed the v0.53 wasmd line's current
  patches (v0.61.14+) already require go 1.24 — one patch away from the kyber
  collision. Condition (b) FLIPPED (ibc-go v10.3.0+ targets v0.53) but is moot
  without (a). ~~for 2.5, **ibc-go v8 is still maintained and suffices**~~ —
  **that half went STALE; corrected here (Rev 3, Rule 8).** D-080's research
  found ibc-go v8 sits in **no active release family**, and D-081 (2.5's STOP
  #1) re-derived the line from task-time evidence: stay on v8.8.0 with the EOL
  usage recorded as an explicit **posture** under PHASE3_PLAN FLAG P3-B — not as
  "still maintained". **2.5 re-ran the v0.53 checkpoint and CLOSED it** (block-sdk
  still frozen at v2.1.5 ⇒ flip-condition (a) still fails; the sigverify fork
  stays). The next re-evaluation is **3.3's STOP #1**, which also owns TODO #5's
  residual (§5).
- **FLAG C — edge services (2.3, 2.4) are SEPARATE-REPO: DISCHARGED (Rev 3).**
  Per SPEC §9 both are off-chain gRPC clients holding no consensus-critical
  logic; the in-repo build and `make test` must not expect their code. Rev 2
  sharpened the in-repo deliverable from "a recorded contract" to a **named
  artifact each**; **both artifacts now exist and are conformance-pinned** —
  `docs/edge/X402_SURFACE.md` + `app/x402_surface_conformance_test.go` (D-083)
  and `docs/edge/7683_SURFACE.md` + `app/erc7683_surface_conformance_test.go`
  (D-086). The separate-repo posture itself is unchanged and permanent.
- **FLAG D — dependency currency is a per-task check: still live, narrowed.**
  The generic "re-check at task time" now has a hard boundary: any dependency
  choice must clear the **two-pin go-1.23.x ceiling** (§4). D-077's spike table
  was the Rev-2 snapshot; **D-081's (2026-07-10) is the current one**, and it
  added a 3.3 input: ibc-go v10.7.0 itself floors at go 1.23.8 — inside the
  ceiling — so on the v0.53 path the toolchain collision now comes from **wasmd**
  (v0.61.14+ → go 1.24), not from ibc-go. **No Phase-2 task changed a dependency
  after 2.2**; the next currency check belongs to 3.3.

---

## Section 1 — Phase-2 dependency spine (Rev 3: as executed)

```
2.1 x/escrow ✅ ──────────────► 2.2 wasmd/CVM ✅ ────────────┐
   (HASHLOCK+OPTIMISTIC           (read-only CONCLUDE +       │
    D-073/074/075;                 escrow allowlist;          │
    ZK_DELIVERY D-076)             D-077 + D-079)             │
        │                            │                        │
        │   ┌────────────────────────┘                        │
        ▼   ▼                                                 ▼
  2.3 x402 ✅            2.4 7683 ✅          2.5 IBC USDC + session-TWAP ✅
  (separate repo;        (separate repo;      (stay ibc-go v8.8.0 as a recorded
   D-083/084/085)         D-086/087)           POSTURE, D-081; v0.53 checkpoint
        │                      │               re-run and CLOSED → 3.3)
        └──────────┬───────────┴────────────────────┘
                   │
                   │   ┌── 2.11 store-iterator fix ✅ (D-088 — split out of 2.6
                   │   │      at a STOP and landed FIRST; 1.0/1.0b precedent)
                   ▼   ▼
        2.6 matcher-richness hardening ✅ (D-089)
                   │
                   ├──► 2.10 in-consensus cycle-aware fallback   ┐
                   ├──► 2.12 multi-asset in-kind skim            │ all OFF
                   ├──► 2.7  escrow consent completeness         │ the exit
                   ├──► 2.8  submit-charge structural fork       │ path
                   └──► 2.9  receipt condition-outcome integrity ┘
                   
        Phase-2 exit demo (§6): x402 → ZK-delivery → IBC-verified receipt
        ◄── UNBLOCKED: gated only on 2.1/2.2/2.3/2.5, all DONE
```

- **2.2 depended on 2.1** — satisfied; the escrow msg surface the bindings
  expose exists (`MsgReleaseEscrow` / `MsgRaiseDispute` /
  `MsgSubmitDeliveryProof`, live since 2.1).
- **2.3 / 2.4 depend on the node's gRPC surface only** (FLAG C); both verdicts
  came back "the node surface is ALREADY COMPLETE" — 2.3 added no node gRPC at
  all, and 2.4 added only the fallback-Solution persistence its own conformance
  test found missing (D-086).
- **Execution order deviated from ledger order, and the deviation is recorded
  here (Rule 8):** 2.5 landed *before* 2.3 and 2.4 (`dccd607` 07-11 <
  `8756575` 07-12 < `c830e55` 07-13). This spine has always made 2.3/2.4/2.5
  parallel siblings of 2.1/2.2, so nothing was built on an unbuilt dependency —
  but CLAUDE.md's working method says "in order", so the resequencing is stated
  rather than left to be inferred from dates. (The other resequencing, 2.11
  before 2.6, *was* recorded at the time — D-088.) Ordering 2.5 first also paid
  off: 2.3's `Twap` endpoint and settlement-denom parameterisation could be
  written against a shipped index instead of a promised one.
- **2.7–2.12 are independent of the commerce/edge work** and every one is off
  the exit path; each lands in its own determinism-focused session because each
  touches either the consensus matcher (2.10), a proto (2.7, 2.9), the charging
  path (2.8), or the fee model (2.12).

---

## Section 2 — Per-task design

### 2.1 — `x/escrow` delivery conditions — ✅ COMPLETE (as built)

Both sessions done: **2.1a** (`22bdc98`, D-073/074/075) and **2.1b**
(`4122fda`, D-076). `make lint test` + determinism vector green at each close.
What shipped, and where it deviated from Rev 1's plan (the deltas are the
valuable part — they correct assumptions the next tasks might otherwise
inherit):

**As built (2.1a — HASHLOCK + OPTIMISTIC, D-073).** Full module: keeper,
`AppModule`, msg server, query, genesis, EndBlock sweep; codespace `15xx`. A
**single authoritative transition gate (`nextState`)** backs an exhaustive
§11-rule-2 transition-table test — every illegal transition is a load-bearing
REJECT, covering all three classes after 2.1b. Funding = payment-into-escrow
(buyer pays in; seller paid on release; buyer refunded on timeout/lost
dispute). OPTIMISTIC seller bond = 10%, returned on honest delivery,
slashed-to-buyer + `PenalizeLostDispute` on a lost dispute (the reputation
hook is wired). Dispute = seller-reveal of the **buyer-agreed**
`DataGrant.ContentHash` (rule-3-enforced, not seller-supplied). Deterministic
height-keyed sweep, ordered after x/session. **D5 ratified as narrowed:**
hash-mismatch challenge + seller bond only; subjective arbitration out of
scope.

**As built (2.1b — ZK_DELIVERY, D-076).** `crypto/zkdelivery`: BN254 groth16
`DeliveryCircuit` (MiMC) proving `MiMC(payload) == schema_commitment` bound to
the escrow id. **vk_id registry = COMPILED-IN** (`//go:embed` VK, 396 B; PK
116 KB test-only in `zktest`, never in the consensus binary). **Replay defense
is instance-bound:** the verifier reconstructs the public witness (escrow id +
signed `schema_commitment`) from the RECORD, never from the submitter — a
proof for escrow A is rejected against escrow B (watched test); same-instance
replay → `ErrTerminalState`. Timeout = compile-time constant
`ZkDeliveryTimeoutBlocks` (no proto change — scope discipline; see 2.7).
On-chain there is only `groth16.Verify` (fixed VK, randomness-free,
recover→INVALID, never panics on adversarial proof bytes).

**Two consent invariants Rev 1 did not plan (found during build — protect them
in 2.2's bindings and 2.6's matcher work):**
- **D-075 — seller-signed condition.** A delivery condition is solver-attached
  (`IntentLeg` has no condition field), so it is bound to the seller's SIGNED
  `IntentPayload.condition` at BOTH solution-validation and the settlement
  boundary (`ErrConditionNotConsented`, fail-closed). An untrusted solver
  cannot impose a delivery obligation/bond the seller never signed.
- **D-076 — buyer-side consent for ZK.** ZK release is seller-driven on an
  advance-committed schema untethered to the buyer's want, so for ZK the BUYER
  must ALSO sign the exact condition (both chokepoints, fail-closed →
  fallback). ZK-only; the HASHLOCK/OPTIMISTIC class/window-level buyer consent
  is deferred as **FW-1** (→ 2.7).

**Plan-vs-built corrections (Rev 1 claims now known wrong or superseded):**
- The ownership handoff chose **writer-going-forward** (x/session stops
  writing `0x09`; `conclude.go:152` TODO closed), not in-place migration.
- Rev 1's dependency claim "x/receipt reads escrow records and must keep
  working across the handoff" was **false** — x/receipt never read escrows;
  the handoff had no receipt-side constraint.
- **D-074 (recorded limitation):** conditioned escrows require an unambiguous
  **bilateral** match; multi-party conditioned graphs are out of 2.1's scope
  (FW-2's per-leg field is the seam that could relax this — a 2.7 plan-stop
  question).
- The conservation-gate ZK rejection (`ErrConditionsNotEnabled`) is LIFTED —
  all three classes enabled; conservation no longer polices conditions.

**Forward obligations opened by 2.1 (each has an owner):**
- **FW-1 + FW-2 → task 2.7** (buyer consent for HASHLOCK/OPTIMISTIC; per-leg
  `IntentLeg.condition` proto refinement).
- **Flake-hardening → shipped as task 2.11 (D-088). ⚠️ THIS BULLET'S ORIGINAL
  PRESCRIPTION WAS WRONG, and the correction is kept visible (Rule 8).** What
  Rev 2 wrote: *"`TestIssueMandateReplace` flaked once on a `randAddr()`-seeded
  fixture (`x/mandate/keeper/keeper_test.go:101`, `secp256k1.GenPrivKey()` per
  call); not a 2.1b regression (proven); **fix = deterministic fixture
  seeding**."* The diagnosis "flaky fixture" was wrong and **the prescribed fix
  would have MASKED a real bug**: 2.11 reproduced the flake at **8/2000
  (0.40% ≈ 1/256)** before touching it, and that rate is the signature of a
  one-byte range boundary — `store.Iterator(prefix, append(prefix, 0xFF))`
  silently excluded every record whose first post-prefix byte is `0xFF`, across
  **8 sites, 2 of them LIVE-broken** (one on the §6.4 rule-5 consensus path).
  Seeding the fixtures first would have pinned the sampler to a passing draw and
  shipped the defect permanently green. **The flakiness was a correctness oracle
  running at a low duty cycle.** Correct ordering (load-bearing, do not reorder
  on a future re-read): fix the sites → forced-`0xFF` watched-red → *only then*
  `randAddr()` determinism. The cited line no longer says what this bullet
  claimed: `keeper_test.go:101` now reads
  `// fixtureAddrSeq backs randAddr's deterministic fixture addresses`.
- **Mainnet gates (NOT phase exit):** VK trusted-setup MPC ceremony (embedded
  VK is single-party dev `groth16.Setup`); circuit soundness/completeness
  audit (MiMC opening + EscrowID public-input binding); gnark-verifier-in-
  consensus audit unit. All carried to the 1.8-style audit scope.
- **Richer typed-schema circuit** (structural conformance, multi-field/Merkle
  payloads) — recorded forward item, Phase-3 candidate; the demo does not need
  it.

### 2.2 — wasmd + CVM custom bindings — DONE (D-077 + D-079; resume checklist retired, outcomes in D-079)

**What it is.** Integrate CosmWasm (`wasmd`) and expose CONCLUDE + escrow to
contracts via custom bindings; the seam is `session/types/tx.pb.go:322`
(`MsgConclude` is module-authority-only; "exposed to CVM via custom
bindings"). 2.2 owned the D-060 v0.53 re-evaluation — **that STOP is done**.

**STOP #1 result (D-077, approved, drafted uncommitted).** Stay on SDK v0.50:
keep cosmos-sdk v0.50.x, block-sdk/v2 v2.1.5, ibc-go v8, the goleveldb pin, go
1.23.x, and the sigverify fork (D-027/D-026). Decisive: block-sdk is frozen at
v2.1.5 (no v0.53 path — a bump forfeits the laned mempool), and the v0.53
wasmd line's current patches need go 1.24 (kyber collision). Recommended wasmd
line: **v0.54.x** (SDK v0.50.11, ibc-go v8, go floor 1.23.1–1.23.6 —
two-pin-safe). The sigverify-fork retirement re-points forward to 2.5-or-later.

**Resume checklist (in order — each step gates the next):**
1. **Commit hygiene:** land D-077 (placement fixed by Rev 2) as the STOP-#1
   record; delete/relocate the throwaway `scripts/_baseline.sh` /
   `_d060_spike*.sh` before the task commit.
2. **Determinism re-baseline BEFORE wasmd wiring.** The working tree already
   carries iavl 1.2.2→**1.2.4** + math v1.5.0 + log v1.6.1 (MVS pre-pulls;
   wasmd not yet added). iavl is on the state-commitment path: run
   `make lint test` + the determinism vector on the dep-bumped tree in
   isolation, so any drift is attributable to the bumps and not to wasmd. If
   the vector shifts, that is a STOP (re-baseline is a recorded decision, not
   a silent re-pin). *Live note: wiring began while Rev 2 was written — if the
   vector is first run after wiring, bisect any drift between the
   dep-bumps-only point and the wasmd point instead.*
3. **Pin wasmd v0.54.x** (confirm current patch at resume time — FLAG D; check
   its go floor stays ≤1.23.x, the §4 ceiling) and wire keeper/`AppModule`,
   preserving the block-sdk laned mempool (§6.7). Plan-stop: which lane wasm
   txs ride.
4. **CVM custom bindings (STOP #2 — the remaining plan-stop):** contract
   queries for open sessions/receipts + escrow msg invocation under a
   re-examined CONCLUDE authority model — bindings must not let an arbitrary
   contract forge a CONCLUDE (§6.4 all-or-nothing holds) **or bypass the
   D-075/D-076 consent gates** (a contract-submitted condition is still
   solver-attached from the chain's view; the signed-consent chokepoints must
   hold for contract-originated flows). Gas metering for CVM → native calls.
   STOP #2 also fixes v0.54-vs-v0.55 and the binding surface.
5. **Regression + demo hook:** full Phase-0/1 suites + determinism vector on
   the final baseline; `scripts/localnet4.sh` smoke where a contract invokes
   an escrow release via the binding (this is the 2.2 slice of the §6 exit
   demo).

**Acceptance criteria (updated).** (1) ~~D-060 spike~~ **done** (D-077).
(2) wasmd wired on v0.50/v0.54.x, laned mempool + determinism preserved, full
regression green. (3) Bindings live under the authority + consent model above,
gas metering defined. (4) `make lint test` green on the final baseline.

### 2.3 — x402 gateway — ✅ COMPLETE (`8756575`, D-083/084/085) (SEPARATE REPO: `conclave-x402-gateway`)

**As built (Rev 3).** **Surface verdict: the node side was ALREADY COMPLETE —
2.3 added no node gRPC.** Every endpoint SPEC §9 / this section names existed at
`dccd607`: `cosmos.tx.v1beta1.Service` + the D-026 unordered extension (the
replay model), the veil/session/receipt/mandate/sigil/epoch query services incl.
`Twap` (D-082) and `ReceiptProof` (ICS-23, D-053), and §6.2 fee abstraction. So
the in-repo deliverable became the CONTRACT plus its proof:
`docs/edge/X402_SURFACE.md` + `app/x402_surface_conformance_test.go` (in
`make test`) + `scripts/x402_surface_check.sh` / `conclave-demo x402-check`
(live localnet sweep, dev tooling). The auth model is **"hold a key + hold a
mandate", and there is no other path** — no server-side signing, no
submit-on-behalf-of RPC. Two deployment models are documented (A
gateway-as-principal = the §6 exit-demo shape; B per-agent delegation), and the
**settlement-custody split** was surfaced live and folded into the contract: a
matched intent's coin *give* legs settle from the sigil's **controller address**,
so the gateway key fronts only gas + bond.

**Two security findings the task produced (neither was in Rev 2's plan):**
- **G1 → FIXED in-task (D-084).** `IssueMandate`'s REPLACE branch never
  compared the existing mandate's sigil to the new terms', so **any** sigil
  controller could rebind an already-bound grantee key — deleting the victim's
  mandate, carrying its `outstanding_commitment` cross-principal, and bricking
  the key's submissions. Now same-sigil-only (`ErrGranteeConflict`), watched
  red-first. This is exactly the key class an edge gateway runs.
- **G2 → FOUND, severity-graded, MITIGATED in-task (D-085).** A mandate-bound
  `MsgSubmitIntent` that passed the ante but failed a veil msg-server
  precondition permanently stranded `spend_commitment + bond` in
  `outstanding_commitment` — an attacker-reachable, on-demand corruption of the
  D-059/D-064 conservation invariant. Closed by ante pre-validation
  (`ValidateSubmitIntentPreconditions` as one source of truth, called by both the
  msg server and a new `SubmitIntentPrevalidationDecorator`), so **no
  ante-passing submit can fail the msg server**. Task **2.8** inherits only the
  design question, not the hole.
- **G3 (dev tooling, Rule 8).** `scripts/demo_e2e.sh` was silently stale twice
  over — `buildIntent` predated D-065 and declared no `spend_commitment`, and the
  driver's gRPC conn used a codec that panics on gogoproto `customtype`. Both
  fixed. Same lesson as the epoch-inert vector: an acceptance artifact nothing
  re-runs is not covering anything.

**Vector:** run #1 (docs/tests/tooling) byte-identical; run #2 (D-084 in
isolation) byte-identical — no re-baseline. `make lint test` exit 0; live sweep
PASS end to end.

---

**Original Rev-1/Rev-2 specification (kept for the record):**

**What it is.** An off-chain x402 facilitator (SPEC §9): outward the Coinbase
x402 HTTP handshake (402 → `X-PAYMENT` → verify → `X-PAYMENT-RESPONSE`);
inward it submits intents/transfers on behalf of the paying agent, fronts gas
(gasless clients), settles in `uusdc`. Stateless except a hot wallet +
replay-nonce store. Separate-repo (FLAG C).

**In-repo deliverable (sharpened): `docs/edge/X402_SURFACE.md`** — the
node-side surface contract, now written against msgs that exist:
- `MsgSubmitIntent` (+ transfers) via a **Mandate-bound gateway key** (§6.2
  ante enforces the ceiling; `FeeAbstractionDecorator` lets the gateway front
  gas). The intent carries a `spend_commitment` (D-065) the gateway derives
  from the quoted price.
- gRPC queries: intent/session state + §6.6 `Receipts` / `Receipt` /
  `ReceiptProof` for `X-PAYMENT-RESPONSE` settlement confirmation.
- `uusdc` settlement denom (mock until 2.5 supplies the real IBC denom — the
  contract doc names the denom as a parameter, not a constant).

**Acceptance.** The surface doc exists, is confirmed stable against a running
node (each named endpoint exercised once by script), and is the interface the
separate repo builds against. Gateway implementation/tests are NOT in
`make test`.

**Plan-stop decisions (mostly gateway-repo):** hot-wallet + replay-nonce
model; which Mandate-bound key + ceiling; gas-fronting accounting (fronted CLV
vs `uusdc` settlement reconciliation).

### 2.4 — 7683 resolver — ✅ COMPLETE (`c830e55`, D-086/087) (SEPARATE REPO: `conclave-7683-resolver`)

**As built (Rev 3).** Everything 2.3 verified carried over; 2.4's *additional*
requirement was the `terms_hash` **pre-image chain** `Session →
winning_solution_id → Solutions → Match`. That was true for solver-cleared
sessions and **FALSE for fallback-cleared ones** — the no-solver default, i.e.
every localnet settlement. `runFallback` applied its matches without persisting
them and only `concludeWinner` ever set `winning_solution_id`, so a fallback
settlement's receipts committed to Match bytes nothing could fetch. Found by the
new conformance test, **STOPPED, and fixed as a stop-approved mid-build
deviation** (Rule 8): `runFallback` now stores a synthetic
`Solution{id: deriveID(0x12‖chamber‖u64be(session_id)), solver: "", score: 0}`
and sets `winning_solution_id`. **Uniformity is the acceptance property** — the
queried pre-image recomputes to the receipt's `terms_hash` byte-for-byte across
both clearing paths.

**Translation authority — the honest claim.** A 7683 order becomes a
`SealedIntent` under the **resolver's own** key + mandate; **nothing on Conclave
binds an intent to an origin-chain order**. An `origin_order_id` field was
considered and **REJECTED as decoration** (Rule 4): it would be
resolver-attached, the chain cannot verify EVM order authenticity, and the
bridge must check the settled legs against the order regardless. The binding
lives bridge-side, and the contract states the four **mandated** bridge checks
(root / terms / leg / dedup) with each gap demonstrated by an in-tree
skip-variant that shows the forged or replayed claim being ACCEPTED when the
check is skipped.

**G1(2.4) → FOUND, disposition (b) (D-087).** `Receipt.condition_outcome` is
decorative: the only writer stamps `NONE` unconditionally, including for
settlements whose legs carried conditions, and nothing updates a receipt when
its escrow later resolves. No funds at risk and no current consumer, so the
disposition **neutralizes rather than ships the falsehood** — three-place
disclosure plus the mandated unconditioned-leg check makes the v1 coin-fill rail
safe by construction with zero node changes. **The fix is task 2.9**, and it is
a **hard prerequisite** if the §6 demo's output is ever used as external-facing
evidence (§0).

**Vector:** run in ISOLATION on the fallback-Solution change — byte-identical,
no re-baseline, with the honest coverage note that the sim generates no
mutually-fillable pair so the vector never exercises the new write; the
conformance test (watched RED against pre-fix HEAD) is the covering gate.

---

**Original Rev-1/Rev-2 specification (kept for the record):**

**What it is.** Consumes ERC-7683 orders from EVM origin chains, translates
them into `SealedIntent`s, posts fills back via a per-origin-chain
verification bridge (SPEC §9). Separate-repo (FLAG C).

**In-repo deliverable (sharpened): `docs/edge/7683_SURFACE.md`** —
- `MsgSubmitIntent` submission for the translated intent, including the
  7683-order → `spend_commitment` mapping (§4/D-065) and — new since Rev 1 —
  the **condition-consent rule**: if the translated intent carries a delivery
  condition, the D-075/D-076 signing requirements apply to the translated
  payload (the resolver signs what its principal authorized, or submits
  unconditioned).
- The receipt / ICS-23 proof surface (§6.6 `ReceiptProof`) for fill-back
  verification on the origin chain.

**Acceptance.** Surface doc exists and is exercised as in 2.3. Resolver code
lives in its own repo, not in `make test`.

**Plan-stop decisions (mostly resolver-repo):** per-origin-chain bridge
mechanism; order→intent translation incl. value→`spend_commitment` and target
counterparty class.

### 2.5 — Real USDC via IBC + session-TWAP fee — ✅ COMPLETE (`dccd607`, D-081/082)

**As built (Rev 3).** A clean two-stop run: **STOP #1** decided the IBC line in
isolation (D-081, above), **STOP #2** the integration + TWAP plan. Shipped: an
`x/session` per-pair price index (store `0x0B`, observations folded *inside* the
Conclude cache with the receipts so they commit atomically with the settlement
that produced them), per-height-mean-then-across-heights TWAP recomputed at
write, window/min-obs params `1200`/`10`, genesis round-trip + a gRPC `Twap`
query; and a nil-safe `TwapSource` seam into `ExecuteFeeAbstraction` — warm ⇒
fee/price with a single truncate-to-pool rounding, cold or stale ⇒
`fee_conversion_rate`, whose meaning is now "deterministic cold-start fallback".
The `window.go:138` TODO is CLOSED. Real IBC USDC proven by a **two-chain ICS-20
e2e** (ibctesting): voucher lands, values at face, `ibc/<hash>` ceilings BIND,
fee converts cold→warm through the app-wired seam.

**The boundaries were PINNED, not moved.** F-2 is *more* live now (real vouchers
give unvalued `MsgTransfer` real value), so 2.5 deliberately did not touch the
`spendIntent` switch — instead `TestSpendIntentF2BoundaryPin` asserts
`MsgTransfer` values at zero **by name**, so task 3.4 flips it by a deliberate
red rather than by drift. `FaceValuePriceSource` stays wired untouched; the TWAP
has exactly ONE consumer (the fee seam) and is never offered to ceiling
valuation, which is the containment argument that keeps this from being the
D-044 problem.

**Vector — RE-BASELINE (the one in Phase 2).** Sequenced for attribution with
**zero dependency changes all task**: run #1 (proto regen + read-only plumbing)
byte-identical; run #2 (the write path) a NEW baseline, two-pass identical,
single-cause by construction. **Those five hashes are the current fixed point**
and every later task (D-084/085/086/088/089) has returned to them byte-identically.

**Forward item with a named owner:** the **fee-TWAP manipulation surface** → 3.1
audit input. A winning solver sets clearing prices, so the fee rate is steerable
at the cost of bonds/skim/gas; windowing + per-height damping + a fee-only blast
radius bound it — an argument, not a proof.

---

**Original Rev-1/Rev-2 specification (kept for the record):**

**What it is.** (1) Replace mock `uusdc` with real USDC over IBC
(`ibc/<hash>` denom). (2) Replace the fixed `fee_conversion_rate` in the §6.2
`FeeAbstractionDecorator` with a TWAP from session clearing prices — seam
`mandate/keeper/window.go:138`. **New in Rev 2:** per D-077, 2.5 also owns the
**next v0.53 re-evaluation checkpoint** (the sigverify-fork TODO #5 re-points
here) — re-run the D-077-style empirical spike at task time; if block-sdk is
still frozen, record and move on (five minutes, not a session).

**Dependencies.** IBC line: ibc-go v8 suffices — **but D-077's "still
maintained" premise went STALE (D-080 research); the task-time re-check
(D-081, 2026-07-10) resolved it: stay on v8.8.0, with the EOL usage recorded
as an explicit posture under PHASE3_PLAN FLAG P3-B (internal-only; 3.3 owns
the exit and gates the public testnet).** The v10 line is NOT required (its
only v0.50-compatible sub-line died 2025-04). Session `clearing_prices`
(already on receipts, §6.6). The `ExecuteFeeAbstraction` seam.

**Acceptance criteria.**
1. **IBC USDC:** a transfer channel brings real USDC; the `ibc/<hash>` denom
   is plumbed through mandate ceilings and the price source valuing it.
2. **Session TWAP:** `fee_conversion_rate` derived from clearing prices over a
   window, float-free (`sdk.Dec`/`math.Int`, §11.6), deterministic,
   persisted/queryable.
3. **Seam swap:** `ExecuteFeeAbstraction` consumes the TWAP; the
   `window.go:138` TODO closes.
4. **Cold-start:** deterministic fixed-rate fallback until a defined warm-up
   of cleared sessions exists.
5. **Skim coherence re-check (owed to 2.6's #10):** confirm the D-051
   numeraire-only skim narrowing is still coherent under the TWAP fee source.
6. `make lint test` green; TWAP math has its own determinism test.

**Plan-stop decisions.** TWAP window + price pair(s) (USDC/CLV); cold-start
threshold/fallback semantics; ibc-go v8 patch; denom/price-source handling —
real-USDC vs `FaceValuePriceSource` (AUDIT F-2/F-3; the D-044 price-source
swap stays a separate task unless the denom work forces it — flag, don't
silently expand); the v0.53 checkpoint result.

### 2.6 — Matcher-richness hardening pass — ✅ COMPLETE (`aa21274`, D-089)

**As built (Rev 3): three of the five TODOs closed, two split out, one
re-pointed — and the scope cut was made at the plan-stop, not improvised.**

- **(8) §5.1 limit-RATE honoring — CLOSES D-049 *and* AUDIT F-7.**
  `IntentLeg.limit` was signed by the agent and read by **nobody** — Rule 4 in
  its purest form. `checkLimitRate` is a separate final clause in `checkLimits`
  (the quantity guards above it byte-identical), bounding each party's own
  achieved rate **from flows alone**: no D-044 price source *and* not the
  solver's stated prices either, because `CheckPriceBinding`'s exact-zero
  residual makes a two-asset party's flow ratio and price ratio the same number.
  Exact by cross-multiplication, never `Quo`. **New consensus risk, recorded:**
  `MulInt` panics on overflow and `checkLimits` runs in EndBlock, so
  `rateWithinBound` carries a `recover()` that fails CLOSED — nothing bounds an
  agent-signed limit. **Recorded limitation:** a leg whose counter-side has >1
  distinct asset has no flows-only rate and is **REJECTED**
  (`ErrLimitRateUnsupported`, session `1428`) rather than settled against an
  unhonored limit — liftable by FW-2's per-leg field (→ 2.7, §2.7).
- **(7) partial-fill cycles + alternate numeraire anchoring** (`solver/cycles`,
  off-chain). `Find` returns one GROUP per cycle — the same cycle priced under
  each coin give-asset, pre-2.6 default anchor first — and `Solve` admits the
  first the **chain gate** accepts, so D-055's arbiter is unchanged and the
  package keeps its no-keeper purity.
- **(6a) 2-party partial-fill fallback.** `tryFallbackPair` tries FULL-GIVE
  first (so nothing that cleared pre-2.6 changes), then FLOOR-FILL, which clears
  pairs whose full-give price is non-representable (D-047) or whose full-give
  rate breaches a signed limit.
- **(9) multi-intent-per-sigil → re-pointed to 2.7/FW-2**; **(10) multi-asset
  in-kind skim → NEW task 2.12**; **in-consensus cycle-aware fallback (6b) →
  NEW task 2.10.**

**All three enrichments are strictly ADDITIVE by wiring**, which is why the
vector came back byte-identical to the D-082 baseline **three times**, once per
item, each run in isolation. **The honest reading is recorded rather than the
green being presented as confirmation:** nothing in-tree sets `limit` or
`allow_partial` and the sim's cycle has one coin give-asset, so the vector never
reaches these branches — the unit and e2e tests are the covering gate. On its
fourth instance this was **PROMOTED from an observation to a structural property
of the gate** (§8 below).

**A watched red DISPROVED a held belief, and the code now says so.** Removing
the solver-side `allow_partial` candidate gates left
`FillOrKillNeverPartiallyFilled` **GREEN 25/25** — the fill-or-kill guarantee
does not rest there. Removing §6.4 **rule 3**'s clause as well turned it **RED
25/25**. So rule 3 is the authoritative guarantee and it *is* reached on the new
partial-fill path; the candidate gates are an efficiency guard. (25 runs
deliberately: the pair's roles follow content-derived intent-id order, so a
one-shot red would have been an accident of ordering.)

**Obligation this task did NOT discharge (found by the Rev-3 audit).**
`docs/edge/7683_SURFACE.md` carried a *"Warning owned by task 2.6"* about the
fallback matcher's internals changing, and 2.6 changed them without touching
either edge contract — even though it added two consumer-visible rejections
(`ErrLimitViolation`, `ErrLimitRateUnsupported`) and made fallback partial fills
reachable. Discharged at Rev 3 (D-090); both contracts updated.

---

**Original Rev-1/Rev-2 specification (kept for the record):**

**What it is (unchanged in substance).** One determinism-focused session
owning the five latent matcher `CONCLAVE-TODO(phase-2)` markers (§5): partial-
fill/cycle-aware fallback, partial-fill cycles + alternate numeraire
anchoring, §5.1 limit-rate honoring, multi-intent-per-sigil, multi-asset
in-kind skim + in-kind payout. All behind the **same `validateSolution` bar**
and the **same determinism vector** (§11.3) guarding the matcher today.
**Added in Rev 2:** the flake-hardening item from D-076 (deterministic fixture
seeding for `x/mandate/keeper` `randAddr()`, `keeper_test.go:101`) rides along
— it is test-harness determinism discipline, this session's theme. **⚠️ That
prescription was wrong — see §2.1's corrected flake bullet and D-088.**
**Constraint to respect:** matcher changes that touch conditioned legs must
preserve the D-074 bilateral constraint and the D-075/D-076 consent gates
(multi-intent/partial-fill must not create a path where a condition settles
against a non-signer) — if a richness item collides with D-074, the resolution
belongs to 2.7's FW-2, not to an ad-hoc 2.6 carve-out.

**Acceptance (one-line shape, expanded at its plan-stop).** The five TODOs
implemented; every node computes the richer matcher identically; determinism
harness (1.0) green over the new surface; the flake fix in. **As executed
(Rev 3):** three implemented, two split to numbered tasks at the plan-stop
(2.10, 2.12), one re-pointed to 2.7/FW-2; the flake item became task 2.11.

### 2.7 — Escrow consent completeness (NEW in Rev 2; owns the D-075/076 FW seams + the D-089 re-points)

**Ownership correction (Rev 3, D-090).** Two items were re-pointed *into* this
task by D-089 but were never written into its acceptance criteria — a one-way
pointer, which is how a debt becomes invisible at the exact moment it is
"owned". They are accepted here explicitly (items 4 and 5 below). Separately,
the **timeouts→params promotion was double-owned** — assigned to *both* 2.7
(here, and in the §0 scoreboard) and 2.8 (D-085's closing paragraph). **It is
resolved to task 2.8**, because 2.8 is already the params/charging-shape task
and 2.7 is already carrying a proto change; 2.7's item 3 below is struck and
D-085's assignment stands.

**What it is.** The deliberate scope-discipline deferrals from 2.1 become a
numbered task so they are a ledger entry, not a floating phrase (the same
discipline that created 2.6):
- **FW-1 — buyer-side consent for HASHLOCK / OPTIMISTIC** (class/window-level
  form; ZK's exact-param buyer gate shipped in 2.1b). HASHLOCK's
  `payload_hash` is a post-delivery token the buyer cannot pre-commit, and
  OPTIMISTIC's committed hash IS the buyer-tied `DataGrant.ContentHash` — so
  their consent form is class/window-level, not exact-param.
- **FW-2 — per-leg `IntentLeg.condition` proto refinement**, replacing the
  single signed `IntentPayload.condition` chokepoint with a per-leg field.
  Plan-stop question: whether FW-2 relaxes the D-074 bilateral constraint
  (multi-party conditioned graphs) or explicitly keeps it.
- ~~**Escrow timeouts → params:** promote `ZkDeliveryTimeoutBlocks` (and the
  other compile-time timeout/bond constants, D-073/D-076 posture) to module
  `Params`.~~ **MOVED to task 2.8 (Rev 3, D-090)** — it was owned twice; 2.8 is
  the single owner.
- **NEW (accepted from D-089) — multi-intent-per-sigil** (the old TODO 9,
  `x/session/keeper/solution.go:143`). Decision D today requires a **distinct
  `sender_sigil` per match**, because legs name sigils and §6.4 rule 3 is
  per-intent, so a sigil must map to exactly one intent in a match. Lifting it
  needs a **`SettleLeg` intent-tag proto field** so rule 3 can attribute a leg
  to one of a sigil's several intents — and it collides head-on with D-074's
  two-distinct-sigil bilateral constraint. That collision **is** FW-2's plan-stop
  question, which is why it lands here rather than as an ad-hoc carve-out.
- **NEW (accepted from D-089) — lift `ErrLimitRateUnsupported`'s multi-counter-
  asset shape** (session `1428`). 2.6 honors a signed §5.1 `limit` from flows
  alone, which has no meaning when the counter-side carries more than one
  distinct asset; such a match is REJECTED rather than settled against an
  unhonored limit. **FW-2's per-leg field is the natural lift**: a per-leg
  counter-asset makes the rate computable without reaching for the D-044 price
  source (which stays task 3.4). If FW-2's plan-stop concludes the per-leg field
  does not give a well-defined counter-asset either, the rejection stays and
  that outcome is recorded — the limitation must not silently persist as an
  unowned greppable error.

**Sequencing.** Off the exit path; after 2.6 (proto change + consent-gate
rework each demand their own determinism re-run; don't stack them under the
matcher session). If 2.6 hits the D-074 collision above, 2.7 can be pulled
earlier — that is a re-sequencing decision to record, not improvise.

**Acceptance (shape).** Buyer consent enforced for all three classes at both
chokepoints (fail-closed → fallback), watched tests per class; per-leg
condition field live with the D-074 decision recorded; **the FW-2 plan-stop
returns an explicit verdict on each of the two D-089 re-points — the
multi-intent-per-sigil lift and the `ErrLimitRateUnsupported` lift — either
implemented or re-deferred to a named task with reasoning (neither may be left
silently open)**; `make lint test` + determinism vector green (proto change ⇒
vector re-baseline recorded).

### 2.8 / 2.9 / 2.10 / 2.12 — the tasks created after Rev 2

Rev 3 records these here so this document holds every Phase-2 task; their
full acceptance criteria live in **CLAUDE.md's Phase-2 ledger**, which is
normative for scope.

- **2.8 — Submit-charge structural fork** (NEW in 2.3, D-085). The G2 security
  hole is **already CLOSED** by 2.3's ante pre-validation; 2.8 owns only the
  design QUESTION — keep ante-prevalidation as the permanent design, or adopt a
  v0.50 mode-split charging (CheckTx-only ante charge, DeliverTx charge inside
  the veil msg server, revisiting D-059's "zero mode branching"). **Plus the
  escrow/veil timeouts→params promotion**, which Rev 3 consolidates here as its
  single owner. Opens with that plan-stop; any charging-path change ⇒ vector in
  isolation. Off the exit path.
- **2.9 — Receipt condition-outcome integrity** (NEW in 2.4, D-087). Additive
  `CONDITION_OUTCOME_PENDING`, a faithful stamp at CONCLUDE (`NONE` only when
  truly unconditioned), and escrow-resolution write-back
  (RELEASED/REFUNDED/DISPUTED). Proto + cross-module consensus work: own
  session, plan-stop first, vector re-baseline expected, Rule-3 watched-red on
  the write-back. Off the exit path — **but a hard prerequisite if the §6 demo
  output is ever used as external-facing evidence (§0).**
- **2.10 — In-consensus cycle-aware fallback** (NEW in 2.6, D-089). The second
  half of the old `fallback.go:35` TODO. **Stated design input, so it is not
  rediscovered: the determinism vector CANNOT be 2.10's primary gate** (§8) —
  a cycle-finder whose determinism the vector cannot exercise must be gated by
  hand-built adversarial fixtures (cycle shapes, tie orderings, enumeration-bound
  boundaries, node-identical traversal), each watched red, with the vector as a
  regression backstop only. Enumeration must be bounded by an explicit param and
  runs in EndBlock where a panic halts the chain. Off the exit path.
- **2.12 — Multi-asset in-kind skim + in-kind solver payout** (NEW in 2.6,
  D-089; `skim.go:37`). Split out because it is a **fee-model** question
  (D-050/D-051), not a matcher enrichment: D-051 skims numeraire coin
  receive-excess only, because `svc/{schema}` is an authorized mint and
  `data/{hash}` has no ledger quantity (D-048) — so "skim them too" must first
  answer whether those classes are skimmable at all. Opens with that plan-stop.
  Off the exit path.

---

## Section 3 — §11.2 transition tables: DELIVERED (kept for the record)

SPEC §11.2 requires every state machine to carry a transition-table test
asserting illegal transitions impossible. **As built (D-073/D-076):** one
authoritative `nextState` gate backs an exhaustive transition-table test over
all three condition classes; every illegal transition is a load-bearing REJECT
(wrong preimage, post-timeout release, dispute-on-HASHLOCK
(`ErrNoDisputePath`), release-on-ZK-without-proof (`ErrInvalidProof`),
`ReleaseEscrow`-on-ZK (`ErrWrongClass`), any terminal-state re-entry
(`ErrTerminalState`)). The Rev 1 rationale stands: three per-class legal
sub-graphs, not one permissive union — HASHLOCK never reaches DISPUTED,
OPTIMISTIC alone uses DISPUTED, ZK gates RELEASED on a proof. RELEASED and
REFUNDED are terminal in every class. Rev 1's per-class tables remain the
normative statement of the legal sub-graphs; the code's exhaustive test is
their implementation.

---

## Section 4 — The toolchain ceiling (a named invariant, two pins)

Rev 1's FLAG D said "re-check versions at task time." The build made the
boundary concrete. **Invariant: the module stays on go 1.23.x until BOTH pins
below are deliberately re-validated in one recorded decision.** Any dependency
whose floor crosses go 1.24 is a STOP, not a bump.

| Pin | Version | Breaks at | Guarded by |
|---|---|---|---|
| `drand/kyber` + `kyber-bls12381` | v1.3.1 / **v0.3.1** | go 1.24 forces kyber-bls12381 v0.3.4 (API/behavior re-validation of the whole Veil threshold path) | D-066 |
| `gnark` + `gnark-crypto` | **v0.13.0** / v0.18.0 | gnark v0.14.0 needs go 1.24.0; v0.15.0 needs go 1.25.7 | D-076 |

Current per-task consequences (D-077 spike snapshot, 2026-06-26):
- **2.2 (wasmd):** v0.54.x floors at go 1.23.1–1.23.6 → inside the ceiling.
  The v0.53-SDK wasmd line (v0.61.14+) is already at go 1.24 → outside.
  block-sdk/v2 frozen at v2.1.5 (v0.50-only) — decisive against the SDK bump.
- **2.5 (ibc-go):** v8 line maintained, inside the ceiling; v10 not required.
- **Escape hatch (recorded, not planned):** if a future task MUST cross to go
  1.24+, the single recorded decision re-validates both pins together —
  kyber-bls12381 v0.3.4 API delta + gnark v0.14/v0.15 — and re-baselines the
  determinism vector + full crypto suite. Nobody bumps one pin "in passing."

---

## Section 5 — Latent-TODO disposition (statuses refreshed by live grep, Rev 2)

| # | Marker | Location (current) | Disposition | Status |
|---|---|---|---|---|
| 1 | escrow record ownership + condition resolution | ~~`conclude.go:152`~~ | FOLD → 2.1 | ✅ **CLOSED** (D-073, writer-going-forward) |
| 2 | reputation dispute hook wiring | ~~`reputation.go`~~ | FOLD → 2.1 | ✅ **CLOSED** (D-073, wired to lost-dispute slash) |
| 3 | `MsgConclude` exposed to CVM | ~~`x/session/types/tx.pb.go:322`~~ | FOLD → 2.2 | ✅ **CLOSED** (D-079; the line now reads "CVM exposure RESOLVED READ-ONLY (task 2.2)") |
| 4 | fixed `fee_conversion_rate` → session TWAP | ~~`x/mandate/keeper/window.go:138`~~ | FOLD → 2.5 | ✅ **CLOSED** (D-082, the `TwapSource` seam; marker gone) |
| 5 | sigverify fork → native ADR-070 (v0.53) | `app/unordered/sigverify.go:16` | **RE-POINTED again by D-081** → **3.3 STOP #1** | open — 2.5 RAN the checkpoint and closed it (block-sdk still frozen ⇒ no bump); only the residual retirement remains |
| 6 | partial-fill + cycle-aware fallback | ~~`x/session/keeper/fallback.go:35`~~ | SPLIT by 2.6's plan-stop | 🟡 **6a CLOSED** (D-089, 2-party partial fill); **6b → NEW task 2.10** (in-consensus cycle-aware) |
| 7 | partial-fill cycles + alt numeraire | ~~`solver/cycles/cycles.go:89`~~ | FOLD → 2.6 | ✅ **CLOSED** (D-089; anchor GROUPS judged by the chain gate, D-055 arbiter unchanged) |
| 8 | honor §5.1 limit rate | ~~`x/session/keeper/solution.go:349`~~ | FOLD → 2.6 | ✅ **CLOSED** (D-089, closes D-049; flows-only rate, no D-044 price source) |
| 9 | multi-intent-per-sigil | `x/session/keeper/solution.go:143` | **RE-POINTED by D-089** → 2.7/FW-2 — **now accepted into §2.7's criteria (Rev 3)** | open (needs a SettleLeg intent tag; collides with D-074) |
| 10 | multi-asset in-kind skim + payout | `x/session/keeper/skim.go:37` | **RE-POINTED by D-089** → NEW task 2.12 | open (a fee-model question, D-050/D-051 — not a matcher enrichment) |

**Marker hygiene (Rev 3).** A repo-wide sweep at `aa21274` found **22**
`CONCLAVE-TODO` markers. The three `phase-2`/`phase-2+` ones are rows 5, 9 and
10 above and are correctly owned. The remaining nineteen were audited against
Rule 6 ("every hit must map to a ledger task") and swept in this Rev — **that
sweep closes AUDIT F-8**, which had sat unowned since 2026-06-15. Three
`phase-0` markers were the F-3/D-044 price-source debt mis-tagged and pointing
at long-completed task 0.8; they are re-tagged and re-pointed to **3.4**. Five
were unowned and either given an owner or deleted. Three described work that
task 0.8 / 1.3 / 1.6 completed — including `crypto/reveal/stub.go`'s "replace
with the BLS12-381 implementation", which 1.3/1.6 *did*: `StubScheme` now
persists **deliberately** as the epoch-0 bootstrap path (D-069, L1), i.e. a
recorded design decision was wearing a TODO's clothing. The six `phase-3`
markers (chamber routing, index sharding) now name **3.11** explicitly instead
of relying on inference.

New since Rev 1 (opened by the build, owned above): **FW-1, FW-2 → 2.7**;
**flake-hardening → shipped as task 2.11 (D-088 — a real 1-in-256 iterator bug,
not a flaky fixture)**; **richer typed-schema circuit → Phase-3 candidate
(3.12)**; **mainnet gates** (VK MPC ceremony, circuit-soundness audit,
gnark-in-consensus audit unit) → the 1.8-style external-audit scope, NOT Phase-2
tasks; **the fee-TWAP manipulation surface (D-082) → 3.1 audit input**; **the
reputation whole-USDC `1e6` scaling quirk (D-082, "noted not fixed") → 3.4**,
given a home at Rev 3 after 2.6 did not surface it.

---

## Section 6 — Phase-2 exit criterion (with leg readiness)

SPEC §10 exit: *"the §1.3 demo extended — external x402 agent buys ZK-verified
data from a Conclave agent; receipt verified on a second Cosmos chain over
IBC."* One integration, four tasks:

| Leg | Task | Status |
|---|---|---|
| x402 agent pays in; gateway fronts gas, submits intent | 2.3 | ✅ **DONE** → **LIVE in §6a** (D-091; the mandate-bound hot key's gas fronted in the settlement denom, asserted by the pool delta) |
| ZK_DELIVERY escrow: groth16 proof releases payment ("ZK-verified" is literal) | 2.1 | ✅ **DONE** → **LIVE in §6a** (D-091; the first conditioned escrow ever opened on a running network) |
| CVM contract drives the escrow/CONCLUDE path via bindings | 2.2 | ✅ **DONE in-process** → **LIVE in §6a; D-079 DISCHARGED** (D-091) |
| Settlement in real IBC USDC; Receipt ICS-23-verified on a second chain | 2.5 | ✅ in-process (D-082) — **the live-relayer slice is §6b, still open** |

**The demo was SPLIT at its plan-stop (D-091): §6a is DONE and §6b remains.**
Only the build is split — **the exit criterion is asserted at §6b**, which owns
**D-082's live relayer USDC demo** and second-chain verification. §6a discharged
the other deferred obligation, the **D-079 localnet contract-release smoke**, in
full. The acceptance record for both parts is `docs/EXIT_DEMO.md`.

**§6a's headline finding, carried forward as task 2.13:** conditioned escrows had
**no live-network writer at all** — the in-consensus fallback never attaches a
condition, non-test `solver/` never does, and escrows are created only by
CONCLUDE — so the entire escrow condition system had been exercised solely by
keeper-level hand-built Matches. §6a's driver is now the only live writer, and it
is **demo tooling**: "the demo can do it now" is not "the network can do it now".

**Hard condition at the demo's plan-stop (D-090).** The x402 leg is the
conditioned ZK buy, so its receipt reads `condition_outcome = NONE` for a
conditioned settlement (D-087). Acceptable for an **internal acceptance run** —
both edge contracts disclose the true semantics, so no consumer is misled. **If
the demo output is ever repurposed as EXTERNAL-FACING evidence, task 2.9 is a
prerequisite.** This is a condition, not a note.

**2.4 stays deliberately off this exit path** (the parallel inbound rail,
proven by its own repo's integration). 2.6/2.7 and 2.8–2.12 harden beneath it
but are not part of the exit assertion. **Distinct from phase exit — the mainnet
gates:** the external cryptographic audit (1.8 scope), the VK trusted-setup MPC
ceremony, and the circuit-soundness audit. The demo can run on the dev VK;
mainnet cannot.

---

## Consolidated FLAGS and decisions (Rev 3 state)

1. **gnark/kyber collision** — RESOLVED: gnark v0.13.0 pin, no Go bump, vector
   byte-identical (FLAG A, D-076). Ceiling now two-pin (§4).
2. **v0.53 flip** — RESOLVED for Phase 2: stay v0.50; block-sdk frozen is
   decisive; wasmd v0.54.9 shipped. **2.5 re-ran and CLOSED the checkpoint
   (D-081); the next re-evaluation is 3.3's STOP #1** (FLAG B, D-077 + D-081).
3. **Edge services separate-repo** — **FLAG C DISCHARGED:** both named artifacts
   exist and are conformance-pinned (`docs/edge/X402_SURFACE.md` + D-083's app
   test; `docs/edge/7683_SURFACE.md` + D-086's). The separate-repo posture is
   permanent and unchanged.
4. **Dependency currency** — live, narrowed to the §4 two-pin go-1.23.x
   ceiling; crossing it is a STOP with a single recorded double-re-validation
   (FLAG D). **No Phase-2 task changed a dependency after 2.2**; D-081's table
   is the current snapshot and the next check belongs to 3.3.
5. **D5 dispute model** — RATIFIED as narrowed: seller-reveal of the
   buyer-agreed hash + 10% seller bond; subjective arbitration out of scope
   (D-073).
6. **Escrow record ownership** — CLOSED: writer-going-forward; x/receipt was
   never a constraint (D-073, correcting Rev 1). **Re-verified at Rev 3:**
   non-test `x/receipt` code contains zero escrow references.
7. **Consent invariants** — load-bearing and now proven reached from every
   direction: D-075 seller-signed condition + D-076 buyer-signed ZK condition,
   enforced fail-closed at validation *and* settlement; 2.2 proved them
   structurally unreachable from CVM (D-079) and 2.6 proved partial fills cannot
   arise on a conditioned leg (a DataGrant is exact-fill only in both new
   matchers), so D-074's bilateral shape is reached exactly as before.
   Completeness (FW-1/FW-2) owned by **2.7**.
8. **The determinism vector's coverage shadow is a STRUCTURAL PROPERTY, not an
   observation** (promoted at D-089 on its fourth instance; supersedes the old
   "five matcher TODOs owned by 2.6" entry, whose scope is now split across
   2.6/2.7/2.10/2.12). The sim's **intent generator** — not the seed vector —
   emits thin, one-sided, single-asset flow: one leg per side, `limit` and
   `allow_partial` never set, one coin give-asset in its cycle. So the matcher's
   rich paths are unreachable however many seeds are added. Four instances:
   **D-069** (epoch-inert ⇒ the real Reveal path is never exercised), **D-086**
   (no mutually-fillable pair ⇒ the fallback-Solution write is never reached),
   **D-088** (no `0xFF`-prefixed id in 5 seeds), **D-089** (no `limit` /
   `allow_partial` / multi-coin cycle). **Binding consequences:** (a) the vector
   is a whole-system REGRESSION backstop — a byte-identical result on a matcher
   change is evidence of ADDITIVITY, never of correctness, and the covering gate
   must be named separately every time; (b) **task 2.10 inherits this as a
   stated design input** (§2.8/2.9/2.10/2.12 above); (c) widening the sim's
   generator until the vector reaches these paths is a legitimate task in its
   own right — **now owned by PHASE3_PLAN 3.5**, no longer a floating phrase.
9. **2.2 resume order was load-bearing** — determinism re-baseline on the
   already-bumped deps (iavl 1.2.4) BEFORE wasmd wiring, so drift is
   attributable. As executed the vector ran *after* wiring; the deviation was
   surfaced and recorded rather than normalized (D-079), and it got lucky
   (byte-identical). The lesson is now METHODOLOGY Rule 7.
10. **Ownership hygiene (NEW at Rev 3, D-090).** `timeouts→params` was owned by
    **both** 2.7 and 2.8 — resolved to **2.8**. Two D-089 items were pointed
    *at* 2.7 without being accepted *into* it — now in §2.7's criteria. AUDIT
    **F-7** is closed-in-fact by 2.6 and **F-8** by this Rev's marker sweep;
    both were recorded nowhere, which is the lesson written into METHODOLOGY
    Rule 6: **a findings list needs the same owner-per-row discipline as a
    ledger, or it becomes write-only.**

---

*Rev 1 approved at the Phase-1→Phase-2 boundary (baseline `39cb207`, D-072).
Rev 2 (D-078) reconciled to as-built state at `4122fda` + the in-flight 2.2
working tree: 2.1 complete, FLAGs A/B resolved, task 2.7 added, toolchain
ceiling named. **Rev 3 (D-090) reconciled at `aa21274` after a full
state-reconciliation audit:** all five SPEC §10 tasks plus 2.6 and 2.11 are
DONE, five tasks (2.8–2.12) gained rows, FLAG C discharged, the ownership
collisions resolved, and the exit demo unblocked with its 2.9 condition
recorded. **Next action: the §6 exit demo.** The conclave convenes.*
