# Conclave Phase 3 — Production Readiness + Chambers (Proposed Plan)

**Status:** PROPOSED plan, authored mid-Phase-2 (baseline `9722912`, 2026-07-10,
tasks 2.3–2.7 + the §6 exit demo still open). This document becomes the phase
authority only after (a) the Phase-2 ledger closes and (b) a Rev-at-boundary
reconciliation (task 3.0) re-validates it against as-built state — the same
discipline PHASE2_DESIGN Rev 2 applied (D-078). Until then it is a plan of
record, not an execution license.
**Baseline (verified 2026-07-10):** `make lint` exit 0 · `make test` exit 0
(all packages ok, 0 FAIL/SKIP) · `make sim-determinism` PASS (5 seeds
`[1,2,7,12,42]`, byte-identical across two independent processes). Pins:
go 1.23.2 / toolchain go1.23.12 · cosmos-sdk v0.50.15 · CometBFT v0.38.23 ·
ibc-go v8.8.0 · wasmd v0.54.9 · block-sdk v2.1.5 · kyber v1.3.1 +
kyber-bls12381 v0.3.1 · gnark v0.13.0.
**Working method:** unchanged — one task per session, plan-stop before code,
every decision an ADR in the task that makes it. The transferable discipline is
codified separately in **`docs/METHODOLOGY.md`**; every Phase-3 task bootstraps
from CLAUDE.md + SPEC + DECISIONS + this plan + that document.

---

## 0. Authority statement — read this first

**SPEC §10 defines Phase 3 as ONE task:**

> *"Phase 3 — Chambers. Activate chamber_id > 0; cross-chamber CONCLUDE via
> two-phase commit over async chamber messaging (design doc required first —
> this is research-grade; do not improvise)."*

That is the spec's entire Phase 3. **This plan proposes the surrounding
production-readiness work that mainnet requires, of which Chambers is one task
(3.11).** The numbering 3.0–3.12 is this plan's proposal, not the SPEC's.

Every task below carries one of three authority classes **in its section
header**:

| Class | Meaning |
|---|---|
| **[SPEC §10 PHASE 3]** | The one task §10 names: Chambers (3.11). |
| **[SPEC-MANDATED, NOT PHASE-NUMBERED]** | The SPEC or the decision ledger requires it but never assigned it a phase: the §8(d) external audit (mainnet gate; 1.8 scope delivered, engagement not run), D6 tokenomics ("pre-testnet"), D7 naming ("pre-announce"), and the D-076-recorded mainnet gates (VK MPC ceremony, circuit-soundness audit). |
| **[PROPOSED — BEYOND CURRENT SPEC]** | Production-readiness scope the spec does not name: platform re-baseline, mandate-bound completeness sequencing, gate-coverage expansion, upgrade infra, observability, public testnet, governance/ops. Justified by the Part-1 re-evaluation and Part-2 research below; adopting them is a decision this plan's approval makes, recorded here rather than silently assumed. |

If this plan and the SPEC conflict, the SPEC wins and the conflict is surfaced
(CLAUDE.md rule). Where a proposed task's plan-stop concludes the SPEC itself
should change (e.g. a `require_encryption` envelope gate, §3.8), the outcome is
a **spec PR proposal**, never silent divergence.

---

## 1. Load-bearing FLAGS (read before any task)

- **FLAG P3-A — the audit is THE gate, not a task among tasks.** Nothing
  downstream matters if O1 fails: if the composed KEM/DEM is not IND-CCA under
  attacker-chosen identity, the Veil is broken and testnet/Chambers/tokenomics
  build on sand. 3.1 is therefore the **gating dependency of the entire forward
  path**, it starts first, and it opens with an **adopt-vs-audit STOP** (§3.1)
  — the authored-vs-adopted discipline applied to the one authored piece never
  closed. Every other task proceeds under the explicit assumption "O1 will
  hold"; a refutation re-plans the phase from §3.1's fallback branch.
- **FLAG P3-B — the platform is already unsafe for a public network (research
  finding, 2026-07-10).** Go 1.23 has been end-of-life since **2025-08-31**
  (~10 months of unpatched stdlib at plan time); cosmos-sdk v0.50.x is listed
  **end-of-life** by the Cosmos release-family policy; ibc-go v8 is in **no
  active release family**; block-sdk was **archived 2025-01-31** ("fork and
  maintain" is upstream's own recommendation). Consequence (Calibration-2
  resolution): the platform re-baseline (3.3) is a **blocker on the public
  testnet (3.8)**, not a schedulable mid-phase task. A private/invite localnet
  can run on the current pins; nothing public may. CometBFT v0.38 is the one
  bright spot — upstream discontinued v1/v2 and concentrates on v0.38→v0.39,
  so the vote-extension surface is NOT stranded. Note the shape of the
  conflict: the kyber pin (D-066) — the guard protecting the crypto
  foundation — is itself what holds the project on the EOL toolchain, so
  3.3's STOP #1 is the knot where the crypto-pin protection meets the
  toolchain cliff: an ACTIVE conflict, not a future one. The gnark-advisories
  finding (≤v0.10.0 only) means it is resolved as a support-horizon decision,
  not an exploit fire.
- **FLAG P3-C — Chambers is research-grade.** SPEC §10 says "design doc
  required first; do not improvise." 3.11's deliverable is the **design doc**;
  a design-only Phase-3 exit is explicitly permitted (§6). Implementation is a
  separate, post-review decision.
- **FLAG P3-D — every genesis re-opens the epoch-0 plaintext window (L1,
  D-069).** Testnet resets included. Every 3.8 genesis plan must carry the
  activation drill, the operator warning, and the detection alarm (3.7); the
  window is minimized, documented, and alarmed — never assumed away.
- **FLAG P3-E — modeling/process tasks have a different acceptance shape.**
  3.9 (tokenomics), 3.10 (governance/runbooks), and 3.11 (Chambers design) do
  not exit on `make lint test` — they exit on a **reviewed written deliverable
  + a recorded ADR**. Do not let "no code changed" stand in for "accepted."
- **FLAG P3-F — two tasks run on external calendars.** The audit (3.1) and the
  ceremony (3.2) are engagements, not sessions: they span weeks, block on
  third parties, and return findings on their own schedule. Their task entries
  define **re-entrant checkpoints** (engage → wait → triage), and the rest of
  the phase is sequenced to make progress while they run.
- **FLAG P3-G — the ceremony binds the circuit.** The groth16 trusted setup
  (3.2) is circuit-specific: any later circuit change (3.12's richer schema)
  voids it. 3.12's fate — fold in before the ceremony or defer past Phase 3 —
  **must be decided at the 3.2 plan-stop**, never after the ceremony runs.

---

## 2. Grounding

### 2.1 Re-evaluation summary (what holds, what does not — full detail in the session record)

**Holds, verified against the repo:** the mandate chokepoint values
`spend_commitment + bond` uniformly and the bound attaches from issuance
(D-064/D-065); consent is enforced fail-closed at both chokepoints for all
three escrow classes (D-075/D-076) and is structurally unreachable from CVM
(D-079); the authored crypto surface is completely inventoried with
self-corrections surfaced (AUDIT_SCOPE_1.8 §2.2, A-barriers); F-4 is closed by
construction with the B+2 timing strengthening it; the determinism gate is
hardened and has caught two real bugs (D-063→D-064).

**Weak, in priority order:**
1. O1 unproven and the audit unengaged (→ 3.1; FLAG P3-A).
2. §1.1 goal 3 holds only for valued flows: F-2 (unvalued msg types charge
   zero) and F-3/D-020/D-044 (data/svc legs value zero; the PriceSource swap
   was deferred to a task that was never scheduled — it floated for two phases
   and became an audit finding) (→ 3.4).
3. Epoch-0 plaintext window: purely operational mitigation today; no
   protocol-level gate, no detection alarm (→ 3.7, 3.8; FLAG P3-D).
4. Platform EOL stack-wide (→ 3.3; FLAG P3-B).
5. Determinism-gate coverage shadows: epoch-inert and wasm-inert sim — the
   vector never exercises the real reveal path it exists to guard (→ 3.5).
6. Zero production infrastructure: no upgrade handlers (UpgradeKeeper wired,
   zero `SetUpgradeHandler`), no testnet, no genesis ceremony, no
   observability, no runbooks, no tokenomics, D7 open (→ 3.6–3.10).
7. Operational defaults unsafe for live epochs: `max_intents_per_session=0`
   (L2/O4) (→ 3.8 genesis policy).

### 2.2 Research record (searched 2026-07-10; re-verify every version at task time — FLAG D discipline)

Tags: **CONFIRMS** (the in-repo position stands) / **SUGGESTS-REVISION** (a
recorded position is stale) / **CONTEXT** (new information, no position
changed).

**R1 — Cosmos platform.**
- block-sdk **archived 2025-01-31**, read-only; upstream recommends fork-and-
  maintain. [github.com/skip-mev/block-sdk] — **SUGGESTS-REVISION** of D-077's
  "frozen" (it is stronger: dead, with an explicit fork recommendation); the
  3.3 disposition options follow.
- cosmos-sdk active release families: **2025.1** (SDK 0.53.x + CometBFT 0.38.x
  + ibc-go v10) and **2026.1** (SDK 0.54.x + CometBFT 0.39.x + ibc-go v11);
  **SDK v0.50.x listed end-of-life**; two families supported at a time.
  [docs.cosmos.network/sdk/latest/release-family] — **SUGGESTS-REVISION**: the
  v0.50 island is no longer a maintained line at all.
- ibc-go v8 in no active family. [same source] — **SUGGESTS-REVISION** of
  D-077's "v8 is still maintained and suffices" as carried into 2.5's design:
  the 2.5 session must treat v8 as EOL (advisory to Phase 2, §5.2).
- CometBFT **v1/v2 discontinued; all future work concentrates on v0.38→v0.39**
  (2026 roadmap; BlockSTM, native PoA, BLS signing in the 2026.1 family).
  [cosmos.network/blog/the-cosmos-stack-roadmap-2026] — **CONFIRMS** the
  CometBFT v0.38 anchor: the ABCI++/vote-extension surface is not stranded.
- wasmd actively maintained (releases through June 2026); the SDK-0.53 line
  exists (go ≥1.24, per the D-077 spike). [github.com/CosmWasm/wasmd] —
  **CONTEXT** for 3.3's target-family choice.

**R2 — Go toolchain + the two pins.**
- Go 1.23 **EOL 2025-08-31**; supported lines at plan time are 1.25/1.26
  (two-release policy). [endoflife.date/go, go.dev/doc/devel/release] —
  **SUGGESTS-REVISION**: the §4 two-pin ceiling escape hatch is no longer
  optional; it is task 3.3 and it gates 3.8 (FLAG P3-B).
- gnark latest **v0.15.0 (May 2026)**; the two published Groth16 advisories
  (GHSA-9xcg-3q8v-7fq6, GHSA-q3hw-3gm4-w5cr, Sept 2024) affect **≤v0.10.0**,
  fixed in v0.11.0 — the pinned **v0.13.0 is not exposed**, and the
  DeliveryCircuit does not use the Groth16 commitment extension they concern.
  [github.com/Consensys/gnark advisories/releases] — **CONFIRMS** the pin is a
  currency problem, not a known-vulnerability problem; the 3.3 bump is still
  required (EOL toolchain) but not emergency-shaped.
- kyber-bls12381 v0.3.4+ delta (go-1.24 floor) unchanged since D-066 —
  **CONTEXT**; the 3.3 double-re-validation covers it.

**R3 — Threshold/encrypted-mempool landscape (the adopt-vs-audit input).**
- **Fairblock/FairyRing**: Cosmos-SDK chain, threshold IBE, uses Block-SDK
  lanes + vote extensions — architecturally the nearest neighbor. Its identity
  model is **condition/height-based (externally fixed, tlock-like)**, not
  per-ciphertext; honest-majority assumption; **no public audit of the
  threshold-IBE implementation surfaced in this research**.
  [docs.fairblock.network, Fairblock medium/architecture posts] —
  **CONFIRMS**: no drop-in audited construction covers Conclave's authored
  surface (the attacker-influenced `id = H(DST‖C_dem)` divergence remains
  Conclave's alone).
- **Batched threshold encryption** matured: Choudhuri et al. (USENIX Sec '24
  "Mempool Privacy via Batched Threshold Encryption"), weighted-BTE
  (eprint 2025/2115), and **Aptos shipping a native encrypted mempool with
  batched threshold decryption (2026)** — one epoch key, batch decryption at
  block finality. [usenix.org, eprint.iacr.org, Aptos announcements] —
  **CONTEXT + fork-enabler**: an externally-fixed **session-scoped identity**
  (Conclave's analog of tlock's round number) is now a well-trodden design that
  would shrink the O1 divergence toward the audited reference AND collapse O4
  cost from per-intent to per-session. This is option (b) in the 3.1 STOP —
  with a recorded boundary-race caveat (§3.1).
- **Shutter** (Ethereum, keyper-based, PoC 2025/26 mainnet path) —
  **CONTEXT**: validates the product thesis; different trust architecture.
- Trail of Bits' disclosed **Pedersen-DKG DoS class** (single malicious
  participant raises the reconstruction threshold; FROST/GG18/GG20-family) —
  **CONTEXT** for 3.1: a named probe area for the auditor; Conclave's B2
  justification round + kyber's internal floor are the existing mitigations to
  verify against exactly this class.

**R4 — Edge standards.**
- **x402**: x402 Foundation (Coinbase + Cloudflare; core members incl. Google,
  Visa, AWS, Circle, Anthropic, Vercel); **spec v1.0 targeted Q3 2026** with
  backward-compat guarantees only after; 69k active agents / 165M transactions
  / ~$50M cumulative volume by Apr 2026; AWS Bedrock AgentCore, Stripe
  (preview), Cloudflare Agents SDK, Google AP2 integrations.
  [docs.cdp.coinbase.com/x402, chainalysis.com, blockeden.xyz] — **CONFIRMS**
  the SPEC §9 bet decisively; **advisory to 2.3** (§5.2): target the v1.0 RC,
  flag pre-1.0 breakage risk in the surface doc.
- **ERC-7683**: production standard — Across (88% of its volume), UniswapX,
  CoW, Eco endpoints; Open Intents Framework (30+ teams); major wallet
  support. [erc7683.org, eips.ethereum.org/EIPS/eip-7683] — **CONFIRMS** the
  2.4 bet; the resolver design gains OIF as a reference framework.

**R5 — Agent-commerce landscape.**
- Layered standards, not rivals: **UCP** (discovery/cart), **ACP**
  (OpenAI+Stripe, in-chat checkout), **AP2** (authorization; three signed
  Mandates as W3C VCs; now FIDO-governed; x402 as default stablecoin rail),
  **x402/MPP** (machine-to-machine payment execution).
  [orium.com, crossmint.com comparisons, ap2-protocol.org] — **CONTEXT**:
  Conclave's differentiation is exactly the layer none of these provide —
  **consensus-enforced** mandates and settlement (theirs are signed-credential
  trust, off-chain enforcement); the positioning writes itself, and the x402
  gateway (2.3) is the bridge into that stack. **Naming note for D7:** AP2's
  core primitive is also called a "Mandate" — a collision to address in any
  public positioning (Conclave's Mandate is consensus-enforced; AP2's is a
  signed credential).

**R6 — Ceremony tooling.**
- gnark ships `backend/groth16/bn254/mpcsetup`; Worldcoin's
  **semaphore-mtb-setup** wraps it as a ceremony coordinator;
  **ptau-deserializer** (worldcoin/dcbuild3r) imports snarkjs Powers-of-Tau
  `.ptau` artifacts as gnark phase 1 — so phase 1 can be **reused** from an
  existing public ceremony and only the circuit-specific phase 2 needs fresh
  participants. [github.com/worldcoin/ptau-deserializer,
  github.com/Consensys/gnark #1428] — **CONFIRMS** 3.2 is buildable with
  existing tooling; plan-stop questions are version alignment (ceremony
  tooling gnark version vs the pinned verifier) and participant set.

**R7 — Audit market.**
- The right firm class exists and has directly relevant track records: Trail
  of Bits (threshold-signature/DKG audits, the Pedersen-DKG DoS disclosure),
  NCC Group (DFINITY threshold ECDSA review), Kudelski (tss-lib CVEs);
  Least Authority also active in this space. — **CONFIRMS** 3.1's engagement
  shape; selection is a 3.1 plan-stop output.

---

## 3. The task plan

### Dependency spine

```
3.0 Rev-at-boundary (entry gate)
 │
 ├─► 3.1 AUDIT: adopt-vs-audit STOP ──► engagement ──► findings triage ──┐
 │        │ (external calendar — runs in parallel with everything)       │
 │        └─► 3.2 ZK MPC ceremony (after the STOP confirms the circuit;  │
 │             FLAG P3-G: decides 3.12's fate)                           │
 │                                                                       │
 ├─► 3.3 PLATFORM re-baseline (two-stop) ══ GATES ══► 3.8                │
 │                                                                       │
 ├─► 3.4 mandate-bound completeness ─┐                                   │
 ├─► 3.5 gate-coverage expansion ────┤                                   │
 ├─► 3.6 upgrade/migration infra ────┼─► 3.8 GENESIS + PUBLIC TESTNET    │
 ├─► 3.7 observability ──────────────┤    (D7 gate before announcement)  │
 ├─► 3.9 tokenomics (D6, pre-testnet)┘         │                         │
 ├─► 3.10 governance + ops runbooks ───────────┘                         │
 ├─► 3.11 Chambers design doc (independent; design-only exit OK)         │
 └─► 3.12 richer schema circuit (deferrable; fate fixed at 3.2's STOP)   │
                                                                         ▼
                              MAINNET GATE CHECKLIST (§7 — beyond Phase 3 exit)
```

Critical path to testnet: **3.0 → 3.3 → {3.4, 3.5, 3.6, 3.7, 3.9} → 3.8**.
External path to mainnet: **3.1 → findings closed** + **3.2** (FLAG P3-F).

---

### 3.0 — Phase-3 plan Rev at the Phase-2 boundary *(housekeeping; entry gate)*

**What.** When the Phase-2 ledger closes (2.3–2.7 + §6 exit demo), reconcile
this plan against as-built state — exactly the PHASE2_DESIGN Rev-2 discipline
(D-078). Confirm which Phase-2 debts actually closed (FW-1/FW-2, matcher
TODOs, flake fix, sigverify checkpoint result from 2.5); re-run the §2.2
version checks that are stale by then; re-affirm or re-cut the task list; the
Rev is recorded as an ADR.
**Depends on.** Phase-2 exit.
**Acceptance.** Rev committed; every disposition-table row (§5) re-verified;
any Phase-2 leftovers explicitly re-owned (nothing floats).
**Plan-stop decisions.** Whether 3.3 must be pulled even earlier (ahead of any
lingering Phase-2 work) given platform-EOL drift since this writing.

### 3.1 — External cryptographic audit: adopt-vs-audit STOP, engagement, triage *[SPEC-MANDATED, NOT PHASE-NUMBERED — §8(d) mainnet gate; FLAGS P3-A, P3-F]*

**What.** Close the outstanding §8(d) obligation: the scope package exists
(`docs/AUDIT_SCOPE_1.8.md`, D-071); the engagement has never run. The task has
three checkpoints:

1. **STOP #1 — adopt-vs-audit (the fork research enables).** Decide, as a real
   isolated stop, which construction goes to audit:
   - **(a) Audit as-built.** O1 as scoped: prove/refute IND-CCA of the
     composed KEM/DEM under attacker-influenced identity `id = H(DST‖C_dem)`.
     Cost: the divergence is Conclave's alone; the proof burden is maximal.
     Benefit: zero rebuild; the shipped, localnet-proven system is the audited
     system.
   - **(b) Re-derive the identity to a session-scoped external label** (the
     session id is Conclave's analog of tlock's round number), THEN audit the
     (much smaller) residual divergence. Benefit: collapses the O1 question
     toward the audited tlock reference AND collapses O4/VE cost from
     per-intent to per-session (one share set per session — the batched-
     decryption shape Aptos/the BTE literature validated). Cost: a consensus
     redesign late in the build (ExtendVote enumeration, share cache, A-bind
     closure, L3 non-replay all re-derived); **recorded caveat:** a
     boundary-race intent encrypted to session `s` but sealed into `s+1`
     would be decryptable by `s`-identity shares before `s+1`'s ordering is
     fixed — the design must gate submission so identity == assigned session,
     deterministically, or the order-then-reveal guarantee breaks for
     stragglers. This wrinkle is why (b) is a STOP, not an obvious win.
   - **(c) Adopt a batched-threshold-encryption construction** from the
     literature. Rejected by default: research-grade, no audited Go
     implementation surfaced (R3) — record and revisit only if (a) and (b)
     both fail.
   The STOP's inputs: R3 findings, O4 economics at target committee size, the
   audit-calendar cost difference, and the 1.7 localnet evidence (all of which
   is invalidated under (b) and must be re-proven).
2. **Engagement.** O2 pre-closure lands in-repo BEFORE the engagement (the DST
   cross-codebase uniqueness sweep + the suite-identity assertion — one grep +
   one test, D-066(b)); auditor selected from the threshold-crypto-experienced
   class (R7); audited commit tagged; scope = AUDIT_SCOPE_1.8 **extended with
   the Phase-2 authored surface enumerated in §3.1.1 below**, T1–T4 and L1–L3
   verification, the seven §4 security arguments (A-bind, A-barriers, A-floor,
   A-F4, A-inject, A-reg, A-just) checked rather than reconstructed, and the
   named Pedersen-DKG DoS probe area (R3).

#### 3.1.1 — The Phase-2 authored security-bearing surface (scope extension)

**Why this list exists, stated plainly:** `AUDIT_SCOPE_1.8.md` is pinned at
`5f5feb8` (Phase-1 BUILD complete) and its §2.2 inventory is *scoped-true for
that revision*. Everything tasks 2.1–2.6 authored is therefore **outside** it,
and an engagement contracted against the scope document alone would review the
wrong tree. Each item below is authored, security-bearing, and post-`5f5feb8`:

- **`crypto/zkdelivery` (D-076)** — the BN254 groth16 `DeliveryCircuit` (MiMC)
  proving `MiMC(payload) == schema_commitment`; the **compiled-in vk_id
  registry** (`//go:embed`, 396 B); and the **instance-binding replay defense**:
  the verifier reconstructs the public witness (escrow id + signed
  `schema_commitment`) **from the RECORD, never from the submitter**. Verify the
  binding is sound, that `groth16.Verify` in consensus is randomness-free and
  cannot panic on adversarial proof bytes, and the circuit's soundness/
  completeness (MiMC opening + EscrowID public-input binding).
- **The D-075 / D-076 consent chokepoints** — arguably the highest-value
  authored security surface Phase 2 produced, and both were **Rule-4 holes found
  in review, not by test**. A delivery condition is *solver-attached*
  (`IntentLeg` has no condition field), so it is bound to the seller's SIGNED
  `IntentPayload.condition` at **both** solution-validation and the settlement
  boundary (`ErrConditionNotConsented`, fail-closed) — and for ZK the **buyer
  must also sign the exact condition**, closing the mirror-image hole. Verify
  both chokepoints are unbypassable, including from contract-originated flows
  (D-079 claims they are structurally unreachable from CVM — check it) and
  across 2.6's new partial-fill paths (D-089 claims a DataGrant is exact-fill
  only in both new matchers, so no partial fill can arise on a conditioned leg).
- **The escrow `nextState` gate (D-073/D-076)** — one authoritative transition
  gate over three per-class legal sub-graphs, backed by an exhaustive
  transition-table test. Verify the sub-graphs are not a permissive union and
  that terminal states are genuinely terminal in every class.
- **D-084 — the grantee-rebind guard.** `IssueMandate` REPLACE is same-sigil
  only (`ErrGranteeConflict`); without it any sigil controller could delete a
  victim's mandate and carry its `outstanding_commitment` cross-principal.
  Verify the guard covers every path that writes the reverse index.
- **D-085 — `SubmitIntentPrevalidationDecorator` + `ValidateSubmitIntent
  Preconditions`.** An **ante-chain change** protecting the D-059/D-064
  conservation invariant, resting on a one-source-of-truth claim: the ante and
  the veil msg server call the same function, so no ante-passing submit can fail
  the msg server. Verify that equivalence actually holds (including the
  nested-msg walk and the deliberate root-key scoping), because if it ever
  diverges the strand vector reopens silently.
- **`app/cvm` (D-079)** — the three-layer no-forge authority model: escrow-msg
  allowlist with contract-address signer stamping, Any/Stargate passthrough
  disabled, bounded query plugins, read-only CONCLUDE. Verify a contract cannot
  forge a CONCLUDE or bypass the consent gates.
- **D-089 — `checkLimitRate`, and note this one is NEW consensus panic
  surface.** The clause runs inside **EndBlock**, `LegacyDec.MulInt` panics on
  overflow, and **nothing bounds an agent-signed `limit`** — so it carries a
  `recover()` that fails closed. Verify the recover's scope is exactly the
  arithmetic, that it is deterministic across nodes, and that failing closed is
  the right posture (a rejected match, never a halted chain).
- **D-088 — the store-iterator range ends.** A truncated exclusive upper bound
  made 1-in-256 records invisible on a consensus path (the §6.4 rule-5
  `has_credential` predicate). Fixed with the SDK's `PrefixEndBytes`; verify no
  remaining iterator anywhere constructs its range end by appending `0xFF`.
- **The fee-TWAP manipulation surface (D-082, task 2.5)** — the §6.2
  fee-conversion rate is a windowed average of solver-set clearing prices, so
  whoever wins solutions can steer it at the cost of bonds/skim/gas. The
  recorded bound (window averaging + per-height damping + fee-only blast radius,
  never ceilings) is an **ARGUMENT, not a proof** — price the manipulation
  economics. A 2.6-style outlier-clipping hardening task is created IF the bound
  fails review.

#### 3.1.2 — STANDING OBLIGATION: re-derive the inventory from the tree

**This is the mechanism, and it exists because the failure is structural, not
incidental.** The authored-surface inventory has now gone stale **twice**:

1. D-066 claimed "the four authored pieces are the ENTIRE authored surface";
   **task 1.7** authored consensus-integration surface (registration binding,
   justification transport, VE injection, share custody) that the four-piece
   list would have steered an auditor *away* from. O3 exists to record that
   correction.
2. **Tasks 2.1–2.6** then authored everything in §3.1.1 — escrow gates, consent
   binding, zkdelivery, ante prevalidation, limit-rate — and the scope document,
   pinned at `5f5feb8`, could not have covered it. Caught only by the D-090
   reconciliation audit.

Twice is a pattern, and the pattern says a hand-maintained inventory decays
between boundaries. So METHODOLOGY Rule 9's inventory-diff is **promoted from
advice to a scheduled gate**:

> **The authored-surface inventory is RE-DERIVED FROM THE TREE — never
> re-read — at (a) every phase boundary and (b) before the audit engagement is
> contracted. The re-derivation is diffed against the last recorded inventory,
> and every addition gets a direct read (Rule 9) before the diff is closed.**

The mechanical procedure, so it is executable rather than aspirational:

```
# 1. crypto surface: what imports a crypto primitive, anywhere
grep -rln 'drand/kyber\|consensys/gnark\|groth16\|encrypt/ibe\|crypto/aes\|crypto/rand' \
     --include='*.go' . | grep -v '_test.go'
# 2. authorization chokepoints: every fail-closed authority/consent rejection
grep -rn 'ErrUnauthorized\|ErrConditionNotConsented\|ErrSigilMismatch\|ErrGranteeConflict\|ErrMandateExceeded\|ErrMsgNotPermitted' \
     --include='*.go' x/ app/ | grep -v '_test.go'
# 3. key material + share custody
grep -rn 'PrivKey\|SecretShare\|KeyShare\|ShareProvider\|dkg_shares' \
     --include='*.go' . | grep -v '_test.go'
# 4. consensus-path recover() sites — each is a fail-closed panic boundary
grep -rn 'recover()' --include='*.go' x/ app/ crypto/ | grep -v '_test.go'
# 5. ante-chain composition (order is security-bearing)
grep -n 'Decorator' app/ante.go
```

Diff the result against §2.2 of `AUDIT_SCOPE_1.8.md` plus §3.1.1 above. **Any
line the greps surface that neither document names is a finding**, and the
inventory is updated in the same session that finds it — never in a later "docs
pass" (Rule 6). Without this gate §3.1.1 goes stale again during Phase 3
exactly as §2.2 went stale during Phase 2; **task 3.0 runs it first, and 3.1
step 2 runs it again immediately before contracting.**
3. **Findings triage (re-entrant).** Every finding → an ADR: fixed (with a
   watched test), or accepted-risk with reasoning. A refuted O1 re-plans the
   phase through this task's fallback branch — (b) or (c) above.

**Depends on.** 3.0. STOP #1 has no build dependency — it starts immediately.
**Acceptance.** STOP #1 recorded as an ADR; O2 closed in-repo (watched:
the assertion fails if the suites diverge); engagement contracted with the
extended scope; on return, zero untriaged findings.
**Plan-stop decisions.** The (a)/(b)/(c) fork; auditor + calendar + budget;
whether 3.12's circuit change folds in before the ceremony (with 3.2,
FLAG P3-G).

### 3.2 — ZK trusted-setup MPC ceremony + VK swap *[SPEC-MANDATED via ledger — D-076 mainnet gate; FLAGS P3-F, P3-G]*

**What.** Replace the single-party dev `groth16.Setup` VK (396 B, `//go:embed`)
with a ceremony-produced VK: phase 1 reused from an existing public
Powers-of-Tau artifact via `ptau-deserializer`; circuit-specific phase 2 run
with gnark's `mpcsetup` (coordinator: semaphore-mtb-setup or equivalent);
transcript published; the embedded VK swapped by recorded decision; the
determinism vector re-baselined (embedded-asset change on the consensus
binary).
**Depends on.** 3.1 STOP #1 (the circuit must be final — FLAG P3-G: if 3.12's
richer schema is ever happening, it happens BEFORE this ceremony or after a
deliberate second ceremony; decide here, record it).
**Acceptance.** Ceremony transcript public with ≥ the decided participant
count; VK reproducible from the transcript; `zkdelivery` suite green against
the new VK; a watched test proves the OLD dev VK no longer verifies (the swap
is real); vector re-baselined by ADR.
**Plan-stop decisions.** Ceremony-tooling gnark version vs the pinned
verifier (VK serialization compatibility — spike first; if the tooling
requires the post-3.3 gnark line, sequence 3.2 after 3.3); which `.ptau`
artifact + its provenance; participant set + coordination venue; 3.12's fate.

### 3.3 — Platform re-baseline *(two-stop)* *[PROPOSED — BEYOND CURRENT SPEC; FLAG P3-B: GATES 3.8]*

**What.** Leave the EOL island in ONE deliberate, fully-recorded migration:
go 1.23.x → a supported line (1.25/1.26 at plan time); cosmos-sdk v0.50.15 →
an active release family; ibc-go v8 → the family's line; wasmd to the matching
line; **both crypto pins re-validated together** (kyber-bls12381 v0.3.1→v0.3.4+
API/behavior delta over the whole Veil path; gnark v0.13.0→v0.14/v0.15 — R2:
no known-vuln pressure, currency only); block-sdk **disposition** (archived
upstream): fork-and-maintain under the project's org (upstream's own
recommendation; the §6.7 laned mempool is consensus-load-bearing) vs replace
with an app-side PrepareProposal lane implementation vs (recorded-and-rejected
unless forced) dropping lanes. Retire the sigverify fork if the target SDK's
native ADR-070 unordered support covers the conformance suite (the fork's
whole purpose dissolves on v0.53+). F-5 (machine-enforced float ban) and F-6
(goleveldb-pin regression guard) land here as CI guards, plus a
dependency-EOL watch (the class of rot this task exists to end).
**Two stops (hard-to-reverse foundation — METHODOLOGY Rule 2):**
- **STOP #1 (decision):** target family — **2025.1** (SDK 0.53.x + CometBFT
  0.38.x + ibc-go v10; keeps the exact CometBFT line the VE surface is proven
  on; native unordered txs; wasmd 0.6x line) vs **2026.1** (SDK 0.54.x +
  CometBFT 0.39.x + ibc-go v11; BlockSTM + native BLS — bigger jump, unknown
  interaction with lanes/VE/deterministic ordering; note 2025.1 retires when
  the next family lands, per the two-family policy). Plus the block-sdk
  disposition and the two-pin re-validation plan. Presented with a D-077-style
  empirical spike table, reviewed in isolation.
- **STOP #2 (post-wiring review):** the migrated tree's full evidence —
  determinism vector re-baseline (recorded ADR; expect drift on an SDK major —
  bisect dep-bumps vs wiring per the D-078 lesson), all Phase-0/1/2 suites,
  unordered conformance, lane routing, VE share transport + reveal e2e,
  localnet4 relaunch (real DKG → encrypted reveal), CGO/libwasmvm packaging.
**Depends on.** 3.0. Nothing else starts against the OLD platform that would
have to be redone (3.4/3.5 sequence AFTER 3.3 for this reason; 3.1/3.9/3.10/
3.11 are platform-independent and run in parallel).
**Acceptance.** Supported toolchain + active release family everywhere; the
laned-mempool answer proven by the lane suites on the new line; both crypto
pins re-validated in one ADR (the §4 escape-hatch discipline, executed);
sigverify fork retired-or-re-recorded; vector re-baselined; localnet4 green
end-to-end; CI guards (F-5, F-6, EOL watch) live and watched red.
**Plan-stop decisions.** Named above; plus whether the 3.3 migration itself
becomes the first 3.6 upgrade rehearsal (a chain that has never upgraded
should not debut upgrades on a public testnet).

### 3.4 — Mandate-bound completeness (F-2 + F-3/D-044) *[AUDIT-mandated debts; PROPOSED sequencing]*

**What.** Close the two-phase-old gap between §1.1 goal 3's claim and the
code. **F-2:** decide and enforce a policy for msg types the valuator cannot
value — options: (i) deny-by-default at issuance (`MsgIssueMandate` rejects
`allowed_msg_types` entries with no valuation rule — fail-closed, matches
D-019's deny-by-default spirit), (ii) extend the valuator (IBC `MsgTransfer`,
staking msgs, wasm execute…), (iii) hybrid: valuator extended for common
types + explicit per-type opt-in for the rest. **F-3:** execute the D-044
PriceSource swap — DataGrant/ServiceVoucher legs valued from session clearing
prices (if 2.5's TWAP shipped, reuse its price index; the fee TWAP and the
valuation source share provenance), with the F-2 policy covering the
no-price-yet cold start.
**Depends on.** 3.3 (build once on the new platform); reads 2.5's as-built
TWAP.
**Acceptance.** A leaked mandate key cannot move unbounded value through ANY
allowlisted msg type — watched per class (each newly-valued type has a
red-without-valuation test; the deny-path has a rejection test); data/svc
legs charge real value against ceilings at the Reveal cross-check too
(commitment comparison inherits the new source — D-065's F-3 boundary note
closes); vector re-baselined if state paths changed.
**Plan-stop decisions.** The (i)/(ii)/(iii) policy; whether existing issued
mandates grandfather (migration semantics); cold-start valuation.

### 3.5 — Gate-coverage expansion (epoch-active sim + fuzz) *[PROPOSED — BEYOND CURRENT SPEC]*

**What.** Point the determinism gate at the surfaces it currently shadows.
(1) **Epoch-active sim:** the vector must exercise the REAL reveal path
(`epoch_id ≠ 0` → `NewSchemeFromCommitBytes` + share collection), not only the
stub — via deterministic epoch fixtures in sim genesis (pre-computed DKG
outputs seeded per sim seed; the DKG itself stays off-chain/non-deterministic,
its OUTPUTS are fixture data) or scripted epoch ops. The D-057/D-063
custom-ops posture holds; the fixture path must be watched: break the real
reveal path, the vector goes red (existence-vs-enforcement applied to the gate
itself). (2) **Wasm posture re-check:** re-affirm (or revise) sim-exclusion as
a recorded decision now that contracts can move escrow state. (3) **Fuzz
expansion:** transport serde (deal/response/justification bundles), envelope +
epoch-tag edges, VE handler bytes (`InjectedRevealVotes`), CVM encoder JSON —
each target either panics-never (recover→INVALID) or is dropped
deterministically.
**Depends on.** 3.3 (don't harden the old platform's gate twice).
**Acceptance.** Vector demonstrably reaches the real reveal path (a sabotage
run — e.g. corrupt the fixture commits — fails the vector by name); posture
ADRs recorded; new fuzz targets in `make test` or a named CI lane.
**Plan-stop decisions.** Fixture mechanism for deterministic epochs in-sim;
whether wasm ops enter the sim or stay excluded; fuzz corpus/budget in CI.

### 3.6 — Upgrade + migration infrastructure *[PROPOSED — BEYOND CURRENT SPEC]*

**What.** The chain currently has no upgrade path (zero `SetUpgradeHandler`).
Introduce the `app/upgrades` pattern (named plans, module migrations, store
upgrades), Cosmovisor operation docs, and the halt-height/rollback runbook.
The natural first rehearsal: IF 3.3's migration is expressible as an on-chain
upgrade (v0.50→family jump may not be — it may be a genesis restart; decide
and record), rehearse THAT; otherwise rehearse a smaller real upgrade (e.g. a
3.4 param/valuator change) end-to-end on localnet4: propose via gov → halt →
Cosmovisor swap → resume → state verified.
**Depends on.** 3.3 (the handler pattern targets the new SDK line).
**Acceptance.** An upgrade executes on localnet4 through the full gov →
Cosmovisor path with a state-migration assertion test (watched: a
deliberately-wrong migration fails it); the runbook exists and was followed
verbatim during the rehearsal (a doc that wasn't executed is not a runbook);
rollback procedure tested once (halt → restore snapshot → restart).
**Plan-stop decisions.** Genesis-restart vs in-place for the 3.3 jump;
snapshot/state-sync policy for operators; upgrade-naming convention.

### 3.7 — Observability + the epoch-0 alarm *[PROPOSED — BEYOND CURRENT SPEC; FLAG P3-D]*

**What.** Wire SDK telemetry + custom metrics across the authored surface:
veil (sealed/open/INVALID counts, reveal retries, shares collected per block,
reveal duration histogram — O4's operational face), epoch (status by id, DKG
window progress, confirmation counts, FAILED events), session (solutions
received, fallback rate, surplus, cap saturation), escrow (state counts,
dispute/timeout rates), mandate (denial counts by error — the
`mandate_enforced` event §6.2 already names), CVM (binding rejections).
**The L1 alarm is load-bearing:** a node-level and dashboard-level signal that
the chain is in the epoch-0 plaintext window (or degraded back onto a prior
key after FAILED DKG) — the D-069 "operationally detectable" obligation,
finally discharged. Dashboards + alert rules ship as artifacts operators can
import.
**Depends on.** 3.3 (metrics API of the new line).
**Acceptance.** Metrics visible on localnet4 under load; the epoch-0 alarm
fires at a fresh genesis and clears at epoch-1 ACTIVE (watched in the 3.8
drill); no consensus-path allocation regressions (bench before/after);
cardinality budget recorded.
**Plan-stop decisions.** Telemetry surface (SDK telemetry vs direct
Prometheus); metric naming convention; log-structure standards; what ships
in-repo vs an ops repo.

### 3.8 — Genesis ceremony + public multi-host testnet *[PROPOSED — BEYOND CURRENT SPEC; FLAGS P3-B (gated by 3.3), P3-D; D7 gates the announcement]*

**What.** The first public deployment. Deliverables: (1) **genesis policy** —
every param leaves dev defaults by recorded decision: finite
`max_intents_per_session` from the O4 re-derivation at the ACTUAL committee
size n (L2 closes here), mandate/bond/session params from 3.9's model, gov
params, permissioned-wasm posture; (2) **genesis ceremony** — validator
onboarding (keys, DKG long-term G2 keys, share-custody per T3), gentx
collection, a scripted rehearsal on throwaway infra first; (3) **the epoch-1
activation drill** — genesis → registration → DKG → ACTIVE as a timed,
rehearsed procedure with the 3.7 alarm watched from fire to clear (FLAG P3-D);
(4) **O4 re-derivation** — the reveal-cost benchmark at testnet n, cap set
from measurement, recorded; (5) faucet + explorer + public RPC posture;
(6) reset policy (every reset = a new L1 window — rehearsed, not rediscovered).
**Beyond-spec proposal to decide here (spec PR if adopted):** a
`require_encryption` envelope flag — an intent that sets it is REJECTED at
submission while no ACTIVE epoch exists, closing L1 for opted-in flows at the
protocol level (proto change ⇒ vector re-baseline; interacts with the
epoch-tag both-edge gate, D-068).
**Depends on.** 3.3 (hard gate — FLAG P3-B), 3.6, 3.7, 3.9; 3.4/3.5 before
public exposure; **D7 clearance before any public announcement** (SPEC §14).
**Acceptance.** ≥4 independently-hosted validators (target n a plan-stop);
real DKG to ACTIVE on public infra; an encrypted 3-party cycle clears
end-to-end (the §1.3 demo, public edition); one 3.6-pattern upgrade executed
live; one induced-failure drill (faulty dealer / withheld shares) survived
with the alarm + runbook exercised; caps finite and derived; the network runs
≥ a decided soak period without manual intervention.
**Plan-stop decisions.** n + host topology + operator model (self-hosted
first vs external operators at genesis); epoch_length for a public network;
the `require_encryption` proposal; soak duration; explorer/faucet choices;
reset cadence.

### 3.9 — Tokenomics + economic parameters (D6) *[SPEC-MANDATED, NOT PHASE-NUMBERED — §14 D6 "pre-testnet"; FLAG P3-E]*

**What.** Replace dev placeholders with a defended economic model: CLV
issuance/inflation and staking incentives; bond magnitudes at real prices
(veil `bond_bps`/`min_bond`, solver `solver_min_bond`, escrow 10% seller bond,
slash fractions) sized against the MEV/griefing value they deter — the D-025
"bonds count toward ceilings" and D-040 slash semantics give the mechanism,
this task gives the magnitudes; solver economics (`solver_fee_bps`, skim base
D-050/D-051) sized for a viable third-party solver market; fee-abstraction
float sustainability (`fee_pool`, its 2.5 TWAP exposure); attack-cost
analysis (cheapest ceiling-bypass, spam-to-CANCEL cost, dispute griefing).
**Depends on.** 3.0 (2.5's TWAP as-built); independent of 3.3.
**Acceptance (FLAG P3-E shape).** A written model (`docs/ECONOMICS.md`) with
stated assumptions + sensitivity ranges; every genesis param it derives
recorded in one ratifying ADR; the 3.8 genesis policy consumes it; explicit
open-risk register for what modeling cannot settle pre-mainnet.
**Plan-stop decisions.** Modeling scope (what's defensible vs deferred);
whether external economic review is engaged (a 3.1-style external
dependency); CLV distribution/allocation questions the SPEC never addressed
(surface loudly — this is where "beyond current spec" is starkest).

### 3.10 — Governance + operational readiness *[PROPOSED — BEYOND CURRENT SPEC; FLAG P3-E]*

**What.** (1) **Param registry sweep**: every module param classified
(gov-votable / authority-gated / compile-time constant), the **2.8**
timeouts→params outcome inherited (that item was owned by both 2.7 and 2.8 and
was resolved to 2.8 at D-090 — this row said 2.7), the permissioned-wasm
posture's governance path documented; (2) **incident runbooks**: chain halt, DKG failure /
epoch-FAILED loop, reveal-retry storm → mass CANCEL, compromised validator
(incl. DKG share custody breach — T3's operational face: containment =
exclusion at next epoch, drain-before-rotate bounds exposure), compromised
agent key (the §1.1-goal-3 drill: revoke → bound verified), solver-market
failure (fallback-only operation); (3) **security process**: disclosure
policy, security contact, embargo norms; (4) validator onboarding/exit docs.
**Depends on.** 3.7 (runbooks reference alarms); informs 3.8's drill.
**Acceptance (FLAG P3-E shape).** Each runbook EXECUTED once against localnet4
or the testnet (a drill log per runbook — unexecuted docs don't count); param
registry recorded as an ADR; disclosure policy published.
**Plan-stop decisions.** Governance activism level at testnet (param-change
cadence); which drills are testnet-public vs private.

### 3.11 — Chambers design document *[SPEC §10 PHASE 3 — the one spec-named task; FLAGS P3-C, P3-E]*

**What.** The design doc SPEC §10 requires before any Chambers code. Must
resolve, at minimum: chamber semantics (what partitions — sessions/veil
per-chamber; what stays global — sigils, mandates, staking, epochs?); whether
chambers share one DKG committee/epoch key or key-per-chamber (interacts with
O4 share-cost and drain-before-rotate); cross-chamber CONCLUDE via two-phase
commit over async chamber messaging — the coordinator, prepare/commit
persistence, timeout/abort semantics, conservation across the 2PC boundary
(D-048's three invariants extended), partial-failure recovery, and the
bilateral/consent gates (D-074/D-075/D-076) across chambers; MEV surface of
cross-chamber sequencing; why-not-IBC (or why-IBC — a chamber as a
same-binary IBC zone is a candidate the doc must weigh against bespoke 2PC);
migration path for chamber_id=0 state (every key is already chamber-prefixed
— D-003 pays off here). The doc mirrors PHASE1_DESIGN's shape: FLAGS first,
per-decision analysis, a task cut for a hypothetical Phase 3b.
**Depends on.** Nothing in-phase (parallel-safe); consumes 3.1's outcome if
the identity model changes (b-branch).
**Acceptance (FLAG P3-C/E shape).** The design doc approved after its own
completeness-gate outline + stop; every open question either resolved or
carried as a numbered flag; **implementation explicitly NOT started** — a
build decision is a separate, post-Phase-3 gate.
**Plan-stop decisions.** The doc's own outline IS the plan-stop.

### 3.12 — Richer typed-schema delivery circuit *[PROPOSED — BEYOND CURRENT SPEC; DEFERRABLE; fate fixed at 3.2's STOP — FLAG P3-G]*

**What.** The D-076 forward item: structural schema conformance
(multi-field/Merkle payloads) beyond `MiMC(payload)==commitment`. No demo,
exit, or safety property needs it.
**Disposition default: DEFER past Phase 3** unless the 3.2 plan-stop finds a
concrete near-term need — because the ceremony binds the circuit (FLAG P3-G),
"later" means a second ceremony, and that cost is only worth paying for a
driven requirement, not completeness. Recorded either way at 3.2's STOP.

### Gate row — D7 naming clearance *[SPEC-MANDATED, NOT PHASE-NUMBERED — §14 D7 "pre-announce"]*

Not a session task: trademark/domain clearance for "Conclave" (and the
positioning answer to AP2's "Mandate" naming collision, R5) completes before
ANY public 3.8 announcement. Owned by the operator; tracked here so the 3.8
plan-stop can hard-require it.

---

## 4. §11.2-style task/authority summary

| # | Task | Authority | Gates / gated by |
|---|---|---|---|
| 3.0 | Rev at Phase-2 boundary | housekeeping | entry gate for all |
| 3.1 | Audit: STOP + engagement + triage | SPEC §8(d) mainnet gate | gates MAINNET; starts first (FLAG P3-A) |
| 3.2 | ZK MPC ceremony + VK swap | D-076 mainnet gate | after 3.1 STOP; decides 3.12 |
| 3.3 | Platform re-baseline (two-stop) | PROPOSED | **gates 3.8** (FLAG P3-B) |
| 3.4 | Mandate-bound completeness | AUDIT F-2/F-3 debts | after 3.3; before 3.8 |
| 3.5 | Gate-coverage expansion | PROPOSED | after 3.3; before 3.8 |
| 3.6 | Upgrade/migration infra | PROPOSED | after 3.3; before 3.8 |
| 3.7 | Observability + L1 alarm | PROPOSED (discharges D-069 obligation) | after 3.3; before 3.8 |
| 3.8 | Genesis + public testnet | PROPOSED (D6/D7 hooks are SPEC) | gated by 3.3/3.6/3.7/3.9 + D7 |
| 3.9 | Tokenomics (D6) | SPEC §14 D6 pre-testnet | before 3.8 genesis policy |
| 3.10 | Governance + ops runbooks | PROPOSED | drills feed 3.8 |
| 3.11 | Chambers design doc | **SPEC §10 Phase 3** | independent; design-only exit OK |
| 3.12 | Richer schema circuit | PROPOSED, deferrable | fate fixed at 3.2 STOP |
| D7 | Naming clearance | SPEC §14 pre-announce | gates 3.8 announcement |

---

## 5. Disposition of every known debt and forward item

### 5.1 The table (nothing floats)

| Debt / forward item | Source | Disposition |
|---|---|---|
| External crypto audit; O1 | SPEC §8(d), D-071 | → **3.1** (with adopt-vs-audit STOP) |
| O2 DST sweep + suite assertion | D-066(b) | → **3.1** step 2 (in-repo, pre-engagement) |
| O3 authored-surface completeness | D-071 §3 | → **3.1** (auditor verifies §2.2 inventory) |
| O4 reveal-cost cap at production n | D-070 | → **3.8** (re-derived at testnet n) + **3.7** (histogram) |
| T1 confirmation-count basis | D-068 | → **3.1** (verify quorum soundness) |
| T2 kyber lowest-index premise | D-067 | → **3.1** (verify) + **3.3** (pin-bump re-runs the probe) |
| T3 share custody / daemon boundary | D-070 | → **3.1** (review) + **3.10** (custody runbook) |
| **T4 dependency pin & license** *(added D-090)* | D-066 §8 | → **3.3** — the two-pin double-re-validation IS T4's re-verification (`kyber`/`kyber-bls12381` MPL-2.0 re-checked on the bump, no transitive divergent BLS12-381) + **3.1** (auditor confirms the pinned versions are the audited ones) |
| **L3 per-share proof = intrinsic pairing check** *(added D-090)* | D-066, FLAG III | → **3.1**, jointly with O1 — L3 says so itself ("verify the two together"): the same `id = H(DST‖C_dem)` per-ciphertext binding that O1 interrogates for IND-CCA is what makes a share non-replayable across ciphertexts |
| **A-bind, A-barriers, A-floor, A-F4, A-inject, A-reg, A-just** *(added D-090)* | AUDIT_SCOPE_1.8 §4 | → **3.1** — these are the arguments already constructed, handed to the auditor to CHECK rather than reconstruct; each names its code and its evidence tests. They had no rows before, which risked an engagement scoped to O1–O4 alone and silently dropping seven security arguments |
| VK MPC ceremony | D-076 | → **3.2** |
| Circuit-soundness audit unit | D-076 | → **3.1** scope extension |
| gnark-verifier-in-consensus audit unit | D-076 | → **3.1** scope extension |
| L1 epoch-0 plaintext window | D-069 | → **3.7** (alarm) + **3.8** (drill, docs, `require_encryption` plan-stop) |
| L2 unsafe default cap | D-070 | → **3.8** genesis policy |
| F-2 unvalued msg types | AUDIT | → **3.4** |
| F-3 / D-020 / D-044 PriceSource swap | AUDIT | → **3.4** |
| F-5 float ban review-only | AUDIT | → **3.3** CI guard |
| F-6 goleveldb pin unguarded | AUDIT | → **3.3** CI guard |
| **F-7 §5.1 limit rate ignored (D-049)** *(added D-090)* | AUDIT | ✅ **CLOSED by task 2.6** (D-089) — recorded against F-7 for the first time at D-090; it had been closed-in-fact and open-on-paper |
| **F-8 stale `CONCLAVE-TODO(phase-0)` markers** *(added D-090)* | AUDIT | ✅ **CLOSED by the Rev-3 marker sweep** (D-090). It was unowned for two phases and independently rediscovered — see docs/AUDIT.md's disposition section for the transferable lesson |
| Two-pin go-1.23 ceiling (now EOL) | PHASE2 §4, R2 | → **3.3** (executes the recorded escape hatch; gates 3.8) |
| block-sdk archived | D-077, R1 | → **3.3** STOP #1 disposition |
| SDK v0.50 / ibc-go v8 EOL | R1 | → **3.3** (new since D-077 — see §5.2 advisory) |
| Sigverify fork retirement | D-027/D-077 | → 2.5 checkpoint result; residual → **3.3** |
| Epoch-inert determinism sim | D-069 | → **3.5** |
| wasm-inert sim posture | D-079 | → **3.5** (re-affirm or revise, recorded) |
| **Sim intent-generator widening** *(added D-090)* | D-089 | → **3.5**, as a NAMED sub-item. D-089 called it "a legitimate task in its own right" and gave it no number — a floating phrase, Rule 6's failure mode. The generator emits one leg per side, never sets `limit`/`allow_partial`, and its cycle has one coin give-asset, which is *why* the vector cannot reach the matcher's rich paths (D-069/086/088/089). 3.5 must decide explicitly: widen the generator, or ratify hand-built adversarial fixtures as the permanent primary gate for matcher richness — and record which |
| **Mirror `.git` foot-gun** *(added D-090)* | this-audit / D-090 | ✅ **FIXED in-task** — `scripts/dev-mirror-sync.sh` rsyncs with `--exclude .git`, so a stale `.git` left in the Windows mirror was never updated: `git log` there reported `b2f633b` (5 commits behind) with 40+ phantom "modified" files, and a `git checkout`/`git stash` run there from habit would have silently reverted the mirror. The script now removes any `.git` in the mirror and drops a sentinel. No forward owner needed |
| **Reputation whole-USDC `1e6` scaling quirk** *(added D-090)* | D-082 | → **3.4**. D-082 recorded it as "noted not fixed … 2.6/3.4 surface if it matters"; 2.6 did not surface it, so it was half-owned by a task that has closed. `economics.go` divides ANY numeraire by 1e6 — correct for 6-decimal USDC, inherited for others. 3.4 owns it because it is the same valuation-provenance question as the D-044 swap |
| No upgrade handlers | re-eval | → **3.6** |
| No observability | re-eval | → **3.7** |
| No testnet / genesis infra | re-eval | → **3.8** |
| D6 tokenomics | SPEC §14 | → **3.9** |
| D7 naming (+ AP2 "Mandate" collision) | SPEC §14, R5 | → gate row (pre-3.8-announcement) |
| Chambers | SPEC §10 | → **3.11** |
| Richer typed-schema circuit | D-076 | → **3.12** (default: defer past Phase 3; fixed at 3.2 STOP) |
| FW-1, FW-2 (+ the two D-089 re-points) | D-075/076/078/089 | **stay Phase 2 (2.7)**; 3.0 verifies closure. *Updated D-090:* `timeouts→params` was owned by BOTH 2.7 and 2.8 — resolved to **2.8**; and D-089's two re-points (multi-intent-per-sigil, the `ErrLimitRateUnsupported` lift) are now written INTO 2.7's acceptance criteria rather than merely pointed at it |
| 5 matcher TODOs + `randAddr()` flake | D-072/078/088/089 | *Updated D-090 — this row was stale:* **3 closed by 2.6** (limit rate, cycles, 2-party partial fill); **2 split into new Phase-2 tasks** (2.10 in-consensus cycle-aware fallback, 2.12 multi-asset in-kind skim); **1 re-pointed** to 2.7/FW-2 (multi-intent-per-sigil); the "flake" was **not a flaky fixture** — it was a real 1-in-256 store-iterator bug shipped as **task 2.11** (D-088). 3.0 verifies the remaining 2.10/2.12 |
| Localnet contract smoke | D-079 | **stays Phase 2** (§6 exit demo); 3.0 verifies |
| **Live relayer USDC demo** *(added D-090)* | D-082 | **stays Phase 2** (§6 exit demo) — decided at 2.5's plan-stop, not a late deviation; 3.0 verifies |
| **§6 exit demo itself** *(added D-090)* | SPEC §10 | **stays Phase 2 — it is 3.0's trigger.** UNBLOCKED at PHASE2_DESIGN Rev 3: all four legs DONE, and 2.7–2.12 are all off the exit path. Carries a hard condition: if its output is ever used as EXTERNAL-FACING evidence, task **2.9** becomes a prerequisite (D-087's `condition_outcome` reads NONE for the conditioned ZK buy) |

### 5.2 Advisories to the remaining Phase-2 tasks (research fallout; not Phase-3 scope)

Recorded here so the Phase-2 sessions inherit them without re-research:
- **2.3 (x402):** the x402 Foundation targets **spec v1.0 in Q3 2026**;
  backward-compat guarantees apply only after. `docs/edge/X402_SURFACE.md`
  should version-tag against the v1.0 RC and flag pre-1.0 breakage risk.
- **2.4 (7683):** standard confirmed production-dominant; the Open Intents
  Framework is the reference integration frame for the resolver repo.
- **2.5 (IBC USDC):** D-077's "ibc-go v8 is still maintained and suffices" is
  **stale** — v8 is in no active release family (R1). The 2.5 session must
  treat v8 as EOL: either ship on v8 accepting recorded EOL risk for a
  non-public devnet rail (and let 3.3 lift it), or pull parts of 3.3 forward.
  That resequencing decision belongs to 2.5's plan-stop + task 3.0, not to
  silence. The 2.5-owned v0.53 checkpoint should consume §2.2's R1 findings.

---

## 6. Phase-3 exit criterion

Phase 3 exits when ALL of:
1. **Audit closed:** engagement complete; every finding triaged to
   fixed-with-watched-test or accepted-risk ADR; O1's verdict recorded (a
   refutation means Phase 3 does NOT exit — it re-plans via 3.1's fallback).
2. **Ceremony done:** transcript-public VK live in the consensus binary; dev
   VK provably dead.
3. **Public testnet:** ≥4 independently-hosted validators; real-DKG epoch
   ACTIVE; encrypted 3-party cycle cleared publicly; one live upgrade; one
   induced-failure drill survived (alarm → runbook → recovery); finite,
   derived caps; soak period met.
4. **Bound complete:** F-2/F-3 closed — no allowlisted msg type moves
   unbounded value past a mandate.
5. **Gate coverage:** the determinism vector demonstrably exercises the real
   reveal path.
6. **Chambers design doc approved** (build not required — FLAG P3-C).
7. **Economics ratified:** D6 model + params ADR consumed by the testnet
   genesis.
8. **Runbooks executed:** every 3.10 runbook has a drill log.

**Explicitly NOT Phase-3 exit (the mainnet-gate checklist, §7).**

## 7. Mainnet-gate checklist (beyond Phase 3)

Mainnet remains gated, independent of phase numbering, on: audit findings
CLOSED (not merely triaged) including O1 affirmed; ceremony VK in the mainnet
genesis binary; D7 cleared; O4-derived caps in mainnet genesis; the L1 window
plan for mainnet genesis (operator comms + alarm + possibly
`require_encryption`); economic params ratified; incident-response on-call
reality (not just runbooks); and a mainnet-genesis rehearsal on a final
release candidate. This list lives here so "testnet is up" is never mistaken
for "mainnet-ready."

---

*Proposed at `9722912` (2026-07-10), mid-Phase-2. Task 3.0 re-validates this
plan at the Phase-2 boundary before any Phase-3 session begins. The working
discipline that executes it is codified in `docs/METHODOLOGY.md`. The conclave
prepares to convene in public.*
