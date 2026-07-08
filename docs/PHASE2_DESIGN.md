# Conclave Phase 2 — Provable Commerce + Edges (Design Document)

**Status:** Approved design (D-072), **Rev 2 — as-built reconciliation**
(2026-07-02, D-078). Rev 1 was authored at the Phase-1→Phase-2 boundary
(baseline `39cb207`) before any Phase-2 build work. Three sessions later parts
of its risk register were stale: **2.1a + 2.1b are COMPLETE** (`22bdc98`,
`4122fda`; D-073–D-076) and **2.2 is IN FLIGHT** (STOP #1 dependency spike done
and approved — D-077, drafted in the working tree, uncommitted). Rev 2 turns
resolved risks into recorded outcomes with citations and spends its lines only
where decisions remain. The external cryptographic audit stays the **mainnet**
gate, owned externally — it does **not** block Phase-2 build work.
**Scope:** SPEC §10's five Phase-2 tasks — `x/escrow` delivery conditions (2.1,
**done**), CosmWasm/CVM integration with CONCLUDE + escrow bindings (2.2, **in
flight**), the x402 gateway (2.3) and 7683 resolver (2.4) edge services, real
USDC over IBC + a session-TWAP fee (2.5) — plus two numbered follow-on tasks:
the matcher-richness hardening pass (2.6) and **NEW in Rev 2** the escrow
consent-completeness pass (2.7, owning the D-075/D-076 FW seams). Exit: the
§1.3 demo extended end-to-end across chains (§6 below).
**Working method:** unchanged — one task per session with a stop-and-review and
its own plan-stop decisions, each recorded in DECISIONS.md in the task that
makes it. Where this doc names a version, the binding instruction remains
"re-check the current release at task time" (FLAG D), except where a pin is now
load-bearing and recorded (§4).

---

## Section 0 — Scoreboard (result state, one line per task)

| Task | Deliverable | Status | Record |
|---|---|---|---|
| 2.1a | `x/escrow` HASHLOCK + OPTIMISTIC, ownership handoff, D5 ratified | **DONE** | `22bdc98`, D-073/074/075 |
| 2.1b | ZK_DELIVERY (gnark groth16), buyer-side consent gate | **DONE** | `4122fda`, D-076 |
| 2.2 | wasmd + CVM bindings (read-only CONCLUDE + escrow allowlist) | **DONE** — wasmd v0.54.9 wired, bindings + forge-prevention e2e green; localnet contract smoke deferred to the §6 exit demo (needs a CVM-schema contract binary) | D-077 + D-079 |
| 2.3 | x402 gateway — in-repo deliverable = node-side surface contract | not started | artifact: `docs/edge/X402_SURFACE.md` |
| 2.4 | 7683 resolver — in-repo deliverable = surface + proof contract | not started | artifact: `docs/edge/7683_SURFACE.md` |
| 2.5 | IBC USDC + session-TWAP fee (swaps `window.go:138`); owns the NEXT v0.53 checkpoint | not started | — |
| 2.6 | Matcher-richness hardening (5 matcher TODOs + flake-hardening item) | not started | — |
| 2.7 | **NEW** — escrow consent completeness (FW-1 + FW-2 + timeouts→params) | not started | D-078 |
| Exit | x402 → ZK-delivery → IBC-verified receipt demo | blocked on 2.2/2.3/2.5 | §6 |

**Exit-demo leg readiness (§6):** ZK_DELIVERY escrow leg **DONE** (end-to-end
conditioned ZK CONCLUDE proven: both parties signing opens a funded escrow, a
bound proof releases it — `TestConcludeConditioned_Zk`). CVM leg **DONE
in-process** (2.2, D-079: a real contract releases a HASHLOCK escrow through
the bindings under wasmvm; the LIVE-localnet slice moved to the demo itself,
which owns the purpose-built CVM-schema demo contract). x402 ingress (2.3) and
IBC settlement/receipt (2.5) not started. 2.4 and 2.6/2.7 are off the exit
path.

**Working-tree state Rev 2 reconciles (uncommitted, from the 2.2 session):**
the D-077 draft in DECISIONS.md (placement fixed in this revision — it had been
inserted mid-D-076); `go.mod`/`go.sum` dependency bumps (go directive
1.23.0→1.23.2, iavl 1.2.2→**1.2.4**, math v1.4.0→v1.5.0, log v1.6.1, new
indirects); three throwaway spike scripts (`scripts/_baseline.sh`,
`_d060_spike.sh`, `_d060_spike2.sh` — untracked, delete or move out before the
2.2 commit). **Live note (2026-07-02):** while Rev 2 was being written the 2.2
session resumed — `wasmd v0.54.9` + `wasmvm/v2 v2.2.8` are now pinned (the
D-077-recommended v0.54.x line) and app wiring is underway (`app/wasm.go`,
`app/cvm/`, ante/app_config edits). The iavl bump sits on the state-commitment
path — the §2.2 checklist's determinism re-baseline is still mandatory; since
wiring has begun, any vector drift must be **bisected** (dep-bumps-only point
vs wasmd wiring) rather than attributed wholesale.

---

## The four load-bearing flags — Rev 2 disposition

Rev 1 surfaced four places where SPEC §10 was underspecified or carried
dependency risk. Two are now resolved with recorded outcomes; two remain live:

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
  without (a); for 2.5, **ibc-go v8 is still maintained and suffices**. The
  next v0.53 re-evaluation is re-pointed to 2.5's IBC-line check or a later
  boundary (this also re-points TODO #5, §5).
- **FLAG C — edge services (2.3, 2.4) are SEPARATE-REPO: still live.** Per
  SPEC §9 both are off-chain gRPC clients holding no consensus-critical logic;
  the in-repo build and `make test` must not expect their code. Rev 2 sharpens
  the in-repo deliverable from "a recorded contract" to a **named artifact
  each** (§2.3/§2.4) so "done" is checkable.
- **FLAG D — dependency currency is a per-task check: still live, narrowed.**
  The generic "re-check at task time" now has a hard boundary: any dependency
  choice must clear the **two-pin go-1.23.x ceiling** (§4). D-077's spike table
  is the current snapshot; 2.5 re-checks ibc-go at task time.

---

## Section 1 — Phase-2 dependency spine (updated)

```
2.1 x/escrow ✅ DONE ─────────► 2.2 wasmd/CVM ◄── IN FLIGHT ──┐
   (HASHLOCK+OPTIMISTIC           (CONCLUDE + escrow           │
    D-073/074/075;                 bindings; D-060 re-eval      │
    ZK_DELIVERY D-076)             DONE = D-077)                │
        │                            │                          │
        │   ┌────────────────────────┘                          │
        ▼   ▼                                                   ▼
   2.3 x402 gateway        2.4 7683 resolver     2.5 IBC USDC + session-TWAP fee
   (separate repo)         (separate repo)       (ibc-go v8 suffices, D-077;
        │                        │                owns next v0.53 checkpoint)
        └───────────────┬────────┴──────────────────────┘
                        ▼
        2.6 matcher-richness hardening (5 matcher TODOs + flake item)
                        ▼
        2.7 escrow consent completeness (FW-1 + FW-2; off the exit path)
                        ▼
        Phase-2 exit demo (§6): x402 → ZK-delivery → IBC-verified receipt
```

- **2.2 depended on 2.1** — satisfied; the escrow msg surface the bindings
  expose now exists (`MsgReleaseEscrow` / `MsgRaiseDispute` /
  `MsgSubmitDeliveryProof`, live since 2.1).
- **2.3 / 2.4 depend on the node's gRPC surface only** (FLAG C); buildable in
  their own repos against a running node once the surface contract is written.
  2.3 settles in `uusdc` and runs on mock USDC until 2.5 brings the real denom.
- **2.5** swaps the fee seam (`window.go:138`) and — per D-077 — inherits the
  next v0.53/ibc-line re-evaluation checkpoint.
- **2.6 and 2.7 are independent of the commerce/edge work** and off the exit
  path; sequenced last so risky consensus-matcher changes (2.6) and proto/
  consent changes (2.7) each land in one determinism-focused session.

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
- **Flake-hardening → 2.6-adjacent:** `TestIssueMandateReplace` flaked once on
  a `randAddr()`-seeded fixture (`x/mandate/keeper/keeper_test.go:101`,
  `secp256k1.GenPrivKey()` per call); not a 2.1b regression (proven); fix =
  deterministic fixture seeding, folded into 2.6's determinism session.
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

### 2.3 — x402 gateway (SEPARATE REPO: `conclave-x402-gateway`)

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

### 2.4 — 7683 resolver (SEPARATE REPO: `conclave-7683-resolver`)

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

### 2.5 — Real USDC via IBC + session-TWAP fee

**What it is.** (1) Replace mock `uusdc` with real USDC over IBC
(`ibc/<hash>` denom). (2) Replace the fixed `fee_conversion_rate` in the §6.2
`FeeAbstractionDecorator` with a TWAP from session clearing prices — seam
`mandate/keeper/window.go:138`. **New in Rev 2:** per D-077, 2.5 also owns the
**next v0.53 re-evaluation checkpoint** (the sigverify-fork TODO #5 re-points
here) — re-run the D-077-style empirical spike at task time; if block-sdk is
still frozen, record and move on (five minutes, not a session).

**Dependencies.** IBC line: **ibc-go v8 suffices and is still maintained
(D-077)** — the v10 line is NOT required; confirm the v8 patch current at task
time. Session `clearing_prices` (already on receipts, §6.6). The
`ExecuteFeeAbstraction` seam.

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

### 2.6 — Matcher-richness hardening pass

**What it is (unchanged in substance).** One determinism-focused session
owning the five latent matcher `CONCLAVE-TODO(phase-2)` markers (§5): partial-
fill/cycle-aware fallback, partial-fill cycles + alternate numeraire
anchoring, §5.1 limit-rate honoring, multi-intent-per-sigil, multi-asset
in-kind skim + in-kind payout. All behind the **same `validateSolution` bar**
and the **same determinism vector** (§11.3) guarding the matcher today.
**Added in Rev 2:** the flake-hardening item from D-076 (deterministic fixture
seeding for `x/mandate/keeper` `randAddr()`, `keeper_test.go:101`) rides along
— it is test-harness determinism discipline, this session's theme.
**Constraint to respect:** matcher changes that touch conditioned legs must
preserve the D-074 bilateral constraint and the D-075/D-076 consent gates
(multi-intent/partial-fill must not create a path where a condition settles
against a non-signer) — if a richness item collides with D-074, the resolution
belongs to 2.7's FW-2, not to an ad-hoc 2.6 carve-out.

**Acceptance (one-line shape, expanded at its plan-stop).** The five TODOs
implemented; every node computes the richer matcher identically; determinism
harness (1.0) green over the new surface; the flake fix in.

### 2.7 — Escrow consent completeness (NEW in Rev 2; owns the D-075/076 FW seams)

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
- **Escrow timeouts → params:** promote `ZkDeliveryTimeoutBlocks` (and the
  other compile-time timeout/bond constants, D-073/D-076 posture) to module
  `Params` — a plan-stop decision, not presumed.

**Sequencing.** Off the exit path; after 2.6 (proto change + consent-gate
rework each demand their own determinism re-run; don't stack them under the
matcher session). If 2.6 hits the D-074 collision above, 2.7 can be pulled
earlier — that is a re-sequencing decision to record, not improvise.

**Acceptance (shape).** Buyer consent enforced for all three classes at both
chokepoints (fail-closed → fallback), watched tests per class; per-leg
condition field live with the D-074 decision recorded; `make lint test` +
determinism vector green (proto change ⇒ vector re-baseline recorded).

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

## Section 4 — The toolchain ceiling (Rev 2: a named invariant, two pins)

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
| 3 | `MsgConclude` exposed to CVM | `x/session/types/tx.pb.go:322` | FOLD → 2.2 | in flight |
| 4 | fixed `fee_conversion_rate` → session TWAP | `x/mandate/keeper/window.go:138` | FOLD → 2.5 | open |
| 5 | sigverify fork → native ADR-070 (v0.53) | `app/unordered/sigverify.go:16` | **RE-POINTED by D-077** → 2.5's v0.53 checkpoint or later | open (not 2.2) |
| 6 | partial-fill + cycle-aware fallback | `x/session/keeper/fallback.go:35` | FOLD → 2.6 | open |
| 7 | partial-fill cycles + alt numeraire | `solver/cycles/cycles.go:89` | FOLD → 2.6 | open |
| 8 | honor §5.1 limit rate | `x/session/keeper/solution.go:349` (drift: was 330) | FOLD → 2.6 | open |
| 9 | multi-intent-per-sigil | `x/session/keeper/solution.go:143` (drift: was 141) | FOLD → 2.6 | open |
| 10 | multi-asset in-kind skim + payout | `x/session/keeper/skim.go:37` | FOLD → 2.6; 2.5 re-confirms skim/fee coherence | open |

New since Rev 1 (opened by the build, owned above): **FW-1, FW-2 → 2.7**;
**flake-hardening → 2.6**; **richer typed-schema circuit → Phase-3 candidate**;
**mainnet gates** (VK MPC ceremony, circuit-soundness audit, gnark-in-consensus
audit unit) → the 1.8-style external-audit scope, NOT Phase-2 tasks.

---

## Section 6 — Phase-2 exit criterion (with leg readiness)

SPEC §10 exit: *"the §1.3 demo extended — external x402 agent buys ZK-verified
data from a Conclave agent; receipt verified on a second Cosmos chain over
IBC."* One integration, four tasks:

| Leg | Task | Status |
|---|---|---|
| x402 agent pays in; gateway fronts gas, submits intent | 2.3 | not started |
| ZK_DELIVERY escrow: groth16 proof releases payment ("ZK-verified" is literal) | 2.1 | ✅ **DONE** (`TestConcludeConditioned_Zk` end-to-end; both-party consent enforced) |
| CVM contract drives the escrow/CONCLUDE path via bindings | 2.2 | in flight |
| Settlement in real IBC USDC; Receipt ICS-23-verified on a second chain | 2.5 | not started |

**2.4 stays deliberately off this exit path** (the parallel inbound rail,
proven by its own repo's integration). 2.6/2.7 harden beneath it but are not
part of the exit assertion. **Distinct from phase exit — the mainnet gates:**
the external cryptographic audit (1.8 scope), the VK trusted-setup MPC
ceremony, and the circuit-soundness audit. The demo can run on the dev VK;
mainnet cannot.

---

## Consolidated FLAGS and decisions (Rev 2 state)

1. **gnark/kyber collision** — RESOLVED: gnark v0.13.0 pin, no Go bump, vector
   byte-identical (FLAG A, D-076). Ceiling now two-pin (§4).
2. **v0.53 flip** — RESOLVED for Phase 2: stay v0.50; block-sdk frozen is
   decisive; wasmd v0.54.x; next checkpoint owned by 2.5 (FLAG B, D-077).
3. **Edge services separate-repo** — live; in-repo deliverable sharpened to
   named artifacts `docs/edge/X402_SURFACE.md` / `docs/edge/7683_SURFACE.md`
   (FLAG C).
4. **Dependency currency** — live, narrowed to the §4 two-pin go-1.23.x
   ceiling; crossing it is a STOP with a single recorded double-re-validation
   (FLAG D).
5. **D5 dispute model** — RATIFIED as narrowed: seller-reveal of the
   buyer-agreed hash + 10% seller bond; subjective arbitration out of scope
   (D-073).
6. **Escrow record ownership** — CLOSED: writer-going-forward; x/receipt was
   never a constraint (D-073, correcting Rev 1).
7. **Consent invariants** — NEW, load-bearing for 2.2/2.6: D-075 seller-signed
   condition + D-076 buyer-signed ZK condition, enforced fail-closed at
   validation and settlement; bindings and matcher-richness work must preserve
   both; completeness (FW-1/FW-2) owned by **2.7**.
8. **The five matcher TODOs** — owned by 2.6, now + the flake-hardening item;
   #10 keeps its 2.5 coherence cross-reference.
9. **2.2 resume order is load-bearing** — determinism re-baseline on the
   already-bumped deps (iavl 1.2.4) BEFORE wasmd wiring, so drift is
   attributable (§2.2 checklist).

---

*Rev 1 approved at the Phase-1→Phase-2 boundary (baseline `39cb207`, D-072).
Rev 2 (D-078) reconciled to as-built state at `4122fda` + the in-flight 2.2
working tree: 2.1 complete, FLAGs A/B resolved, task 2.7 added, toolchain
ceiling named. Next action: resume 2.2 at the §2.2 checklist, step 1. The
conclave convenes.*
