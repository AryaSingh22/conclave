# Conclave Phase 3 — Production Readiness + Chambers (Phase Authority)

**Status: THE PHASE AUTHORITY — Rev 1, task 3.0, 2026-08-03 (D-096).** Authored
mid-Phase-2 as a PROPOSED plan (baseline `9722912`, 2026-07-10); Rev 1 is the
Rev-at-boundary reconciliation task 3.0 owed, run against as-built state at
`15c2470` after Phase 2 was declared EXITED (2.16, D-095) — the same discipline
PHASE2_DESIGN Rev 2 applied (D-078). **Both gate conditions are met**, so this
document is now an execution licence rather than a plan of record.

**Rev 1 is materially different from a refresh.** A toolchain spike run before
any planning (§2.3) **refuted the load-bearing half of FLAG P3-B at the
artifact**, and the correction runs in the expensive direction: the platform was
never trapped. Every sequencing decision below was re-derived from that
evidence rather than inherited. Rev 1 also discharges 3.0's six written-in
obligations with evidence (§3.0), rules on the D-095 methodology amendment and
on the re-evaluation amendment set row by row (§8), and adds six §5.1 rows that
a row-by-row pass could not have surfaced.

**Baseline (re-verified 2026-08-03 at `15c2470`, clean tree):** `make lint`
exit 0 · `make test` exit 0 (17/17 packages, 0 FAIL/SKIP) · `make
sim-determinism` PASS (5 seeds `[1,2,7,12,42]`, byte-identical across two
independent processes, **hashes equal to the D-082 baseline** — verified against
`DECISIONS.md:3300-3304`, not asserted). Tree fingerprinted before and after:
`git diff | sha256sum` = `e3b0c44…b855` (empty) both times.
Pins UNCHANGED by this Rev: go 1.23.2 / toolchain go1.23.12 · cosmos-sdk
v0.50.15 · CometBFT v0.38.23 · ibc-go v8.8.0 · wasmd v0.54.9 · block-sdk v2.1.5 ·
kyber v1.3.1 + kyber-bls12381 v0.3.1 · gnark v0.13.0. **Rev 1 changes no pin and
no source byte — it is a docs-only Rev** (§9 records the two-fingerprint
close-out).
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
- **FLAG P3-B — the platform is EOL and that is now urgent; but it was never
  TRAPPED.** *(Rev 1: half CONFIRMED and sharpened, half REFUTED AT THE
  ARTIFACT — read both halves, they move in opposite directions.)*

  **CONFIRMED and worse than written.** Go 1.23 has been end-of-life since
  **2025-08-31**; cosmos-sdk v0.50.x is **end-of-life**; ibc-go v8 is in **no
  active release family**; block-sdk was **archived 2025-01-31** and Rev 1
  confirms empirically that **v2.1.5 is the last tag that will ever exist** (the
  proxy version list ends there). The re-evaluation's `govulncheck -mode binary`
  on the built `conclaved` found **16 vulnerabilities with reachable symbols**,
  10 of them in the `go1.23.12` stdlib — including **GO-2026-5058**
  (`hashicorp/go-getter` arbitrary file read) sitting on the upgrade-binary
  download path that **3.6** is about. So the original clause *"a support-horizon
  decision, not an exploit fire"* is **FALSE stack-wide** and true only of gnark.
  Consequence unchanged and reinforced: 3.3 **gates the public testnet (3.8)**. A
  private/invite localnet may run on the current pins; nothing public may.

  > ~~"the kyber pin (D-066) — the guard protecting the crypto foundation — is
  > itself what holds the project on the EOL toolchain, so 3.3's STOP #1 is the
  > knot where the crypto-pin protection meets the toolchain cliff: an ACTIVE
  > conflict, not a future one."~~
  >
  > **REFUTED at the artifact by the 3.0 toolchain spike (§2.3, D-096).** A `go`
  > directive is a **MINIMUM, never a maximum**: `kyber-bls12381 v0.3.1` declares
  > `go 1.18` and `kilic/bls12-381 v0.1.0` declares `go 1.13`, so **neither
  > imposes any ceiling at all**. Empirically, bumping only the main module's
  > directive to `go 1.25.0`/`toolchain go1.25.12` with **every pin untouched**
  > builds, vets, passes 17/17 packages and 24/24 crypto tests, and returns the
  > determinism vector **byte-identical to D-082**. There was no knot. D-066's
  > pin genuinely avoided a **FORCED** bump and was correct in Phase 1 — it never
  > prevented a **VOLUNTARY** one, and that inference is what was wrong.

  **Why this correction is recorded loudly rather than quietly fixed.** The flag
  was wrong **in the expensive direction**: it made the phase's central platform
  problem look intractable when it was testable in an afternoon, and a pessimistic
  flag is not a safe default — it silently deletes options from every plan that
  inherits it. The flag had been carried, cited and built on across multiple
  documents for three weeks **and had never once been tested against the artifact
  it described.** The transferable rule is now METHODOLOGY's (see §8 ruling A1):
  **a FLAG is a claim, and flags accumulate authority precisely because nobody
  re-checks them.**

  CometBFT v0.38 remains the bright spot — upstream discontinued v1/v2 and
  concentrates on v0.38→v0.39, so the vote-extension surface is NOT stranded.
  Rev 1 adds: **v0.38.25 is available and declares `go 1.22.11`**, so the
  security-relevant CometBFT patch bump costs **nothing** on the toolchain axis
  and must not wait on the family decision (§3.3 STOP #1, item 0).
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
4. Platform EOL stack-wide (→ **3.3a** for the toolchain + the 16 reachable advisories, **3.3b** for the release family; FLAG P3-B).
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

**R2 — Go toolchain + the two pins.** *(Rev 1: §2.3's spike supersedes this
sub-section's INFERENCES wherever they conflict; the version facts stand.)*
- Go 1.23 **EOL 2025-08-31**; supported lines at plan time are 1.25/1.26
  (two-release policy). [endoflife.date/go, go.dev/doc/devel/release] —
  **SUGGESTS-REVISION**: the §4 two-pin ceiling escape hatch is no longer
  optional; it is task **3.3a** and **3.3b** gates 3.8 (FLAG P3-B).
  **Rev 1 correction:** the "escape hatch" framing implied crossing the ceiling
  required a coordinated two-pin re-validation *in order to bump Go at all*. It
  does not. **PHASE2_DESIGN §4's invariant survives, but only in its literal
  form** — it is a statement about what `kyber-bls12381 **v0.3.4**` and `gnark
  **v0.14/v0.15**` would cost **if adopted**, and those bumps still require the
  single recorded re-validation. It was **never a constraint on the `go`
  directive**, because a `go` directive is a minimum. Bumping Go **without**
  touching either pin is now empirically green (§2.3(a)) and is what 3.3a does.
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
  (eprint 2025/2115), and ~~**Aptos shipping a native encrypted mempool with
  batched threshold decryption (2026)**~~ — **CORRECTED at Rev 1 (D-096): Aptos's
  encrypted mempool was ANNOUNCED (12 May 2026) and is AWAITING GOVERNANCE, not
  shipped.** This materially **weakens** option (b)'s "well-trodden design"
  support at 3.1 STOP #1, which is precisely where the claim would have been
  load-bearing — one epoch key, batch decryption at
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

### 2.3 The task-3.0 TOOLCHAIN SPIKE (empirical, 2026-08-03 — supersedes §2.2's documentary readings on every point it touches)

**Why it exists.** §2.2 R1/R2 are *documentary* readings of release notes and
`go.mod` files. The re-evaluation proposed that they be settled empirically
before any Phase-3 sequencing decision was made, on the grounds that the plan
written before the spike would be the plan the spike invalidated. It was, and it
would have been.

**Method (recorded so the result is checkable, not trusted).** One throwaway
branch off `15c2470`; the only tracked edit was `go.mod`'s two directive lines;
every other experiment ran in `/tmp` copies made by `git archive main | tar -x`
(no `.git`). Two-fingerprint discipline was carried INSIDE the spike — the
working-diff hash was recorded before **and** after the determinism-vector run
(`9ebb6df8…`, identical), which is what makes *"the vector ran on the go-1.25
tree and nothing drifted"* evidence rather than assertion. Branch destroyed;
post-teardown fingerprint `e3b0c44…b855` = the pre-spike baseline.

| # | Question | Answer | Evidence |
|---|---|---|---|
| **(a)** | Bump only the `go` directive — builds? tests? vector? | **YES / YES / YES** | `go 1.25.0`+`toolchain go1.25.12`, pins untouched: build **0**, vet **0**, `go test ./...` **0** (17/17), crypto **24/24**, vector **PASS 5 seeds byte-identical to D-082** across two processes |
| **(b)** | Does a 2025.1 stack resolve MVS-clean? Does any **transitive** lift the floor above 1.23.8? | **Resolves clean / NOTHING above 1.23.8** | `go get sdk@v0.53.8 cometbft@v0.38.24 wasmd@v0.60.8 ibc-go/v10@v10.7.0` exit **0**; `go mod tidy` exit **0**; directive settled at exactly **`go 1.23.8`**; census of modules declaring >1.23.8 came back **EMPTY** |
| **(c)** | What ACTUALLY binds the toolchain? | **At 1.25: nothing. At 1.26: `bytedance/sonic v1.14.0`** | `sonic/internal/rt/stubs.go:33: undefined: GoMapIterator` — a `//go:linkname` into runtime internals removed by Go 1.26's map rework. Reaches us via `conclave/app → cosmossdk.io/log → sonic`, and `sonic/internal/rt` **is in `conclaved`'s own import graph** |
| **(d)** | Does pinned assembly-bearing `kilic/bls12-381 v0.1.0` compile AND behave identically? | **YES — bit-identical, three toolchains AND against its own pure-Go fallback** | KAT (scalar→G1/G2, BLS sign/verify, 4 Feldman commits) `SHA-256 = 6d98cd34…` under **go1.23.12 ≡ go1.25.12 ≡ go1.26.5**; `-tags generic` (assembly disabled) **also byte-identical**; crypto suite green ×5 per toolchain; **T2 probe `TestKyberRecoverCommitSelectsLowestIndex` 50/50 per toolchain** (dosed per Rule 3 step 7) |

**The recorded block-sdk hypothesis is REFUTED — and it was mine to test, not to
inherit.** The re-evaluation guessed the real constraint was the archived
block-sdk v2.1.5 rather than kyber. It is neither: block-sdk **compiles clean
under both 1.25.12 and 1.26.5** (it declares `go 1.22.4`), and per (g) below it
**also compiles clean against cosmos-sdk v0.53.8**. Stated precisely, because the
distinction is the whole point: **compiling is not behaving.** Lane routing,
`PrepareProposal` semantics and block-sdk's own suites are untested on v0.53 and
remain 3.3 STOP #2's evidence list. But the inherited framing asserted a
**compile/MVS impossibility** ("decisive against the SDK bump"), and that
specific claim is false.

**Two further results that create menu items rather than answer questions.**
- **(f) go 1.26 is reachable today for four indirect `go.mod` lines.** Bumping
  *only* `sonic v1.14.0 → v1.15.2` (pulling `sonic/loader v0.5.1`, `base64x
  v0.1.6`, adding `bytedance/gopkg`; **no direct dependency touched, no `go`
  directive change**) makes `go build ./...` exit **0 under go1.26.5** — and
  still exit **0 under go1.25.12**, so it is a forward fix, not a swap.
- **(g) the 2025.1 family costs ONE compile error in ONE package.** SDK v0.53.8 +
  CometBFT v0.38.24 + wasmd v0.60.8 + ibc-go v10.7.0 (alongside our still-imported
  v8), tidied clean, built under 1.25.12: `app/cvm/encoder.go:58` — wasmd's custom
  `AnyMsg` encoder callback gained a leading `ctx sdk.Context`. That is the entire
  **source-level** surface of the family jump.

**HONEST LIMITS — binding on every row below that leans on 1.26 or on 2025.1.**
Full gates (`make test` + the determinism vector) ran on **1.25.12 ONLY**. Under
**1.26.5** only build/vet + the crypto suite + the KAT + T2 ran, so *"1.26 is
green"* is **NOT established** — the established claim is exactly *"1.26 compiles
once sonic moves, and the crypto path is bit-identical."* **(g) was
compile-only**: no tests, no vector, no CGO/libwasmvm packaging, no lane/VE/reveal
behaviour. Any §3.3 or §5.1 row citing these results must cite them at this
strength and no higher.

**Two corrections to the recorded dependency table fall out of (b):**
- **D-077's spike table never saw the wasmd v0.60 line.** It jumped v0.55.1 →
  v0.61.0 and concluded the v0.53-SDK wasmd line needs go 1.24. In fact **wasmd
  v0.60.8 targets SDK v0.53.6 + ibc-go v10.5.0 at floor `go 1.23.8`** — inside the
  old ceiling. (v0.61.14 → 1.24.0 is true, but of a *different* line.)
- **CometBFT v0.38.25 exists** (the re-evaluation had .24) and v0.38.23/24/25 all
  declare `go 1.22.11`.

**One methodological note against the spike's own instrument (Rule 8).** The
per-dependency compile probe reported `FAIL` for block-sdk, kyber, gnark, wasmd
and cometbft — **every one a `missing go.sum entry`** for packages outside our
import graph (testutils, code generators, wasmd's nft app), not a compile error.
Absolute results from that probe are confounded and nothing here rests on them.
What *is* load-bearing is the **differential**: the failure sets were **identical
under 1.25.12 and 1.26.5**, which is the signature of go.sum artifacts rather
than a toolchain ceiling — and the one genuine compile error stood out precisely
because it appeared under 1.26 only.

**And one failure that was chased rather than believed (Rule 3's corollary).**
`TestRevealCostWithinBound` failed under go1.26.5 at **56.3 ms/intent** against a
50 ms ceiling, with the message *"the share gate / IBE open regressed."* Rule 3
says reproduce and count before concluding. Pre-compiled and run standalone ×10
per toolchain: **5.26–5.38 ms (1.23.12), 5.29–5.46 ms (1.25.12), 5.36–5.44 ms
(1.26.5) — 10/10 PASS each**, and `BenchmarkRevealCombineUnderCap` within **1.1%**
across all three (341.0 / 341.3 / 344.7 ms/op). There is **no regression**; the
cause was `go test ./crypto/...` overlapping gnark's cold compile with the timing
window. The ~9× headroom is a useful byproduct — and the test itself became
finding **F-9** (§5.1).

---

## 3. The task plan

### Dependency spine

```
3.0 Rev-at-boundary (entry gate) ✅ CLOSED — Rev 1, D-096
 │
 ├─► 2.14 reference solver condition attachment (LIVE DEFECT — §5.3 order 1)
 │
 ├─► 3.1 AUDIT: adopt-vs-audit STOP ──► engagement ──► findings triage ──┐
 │        │ (external calendar — runs in parallel with everything)       │
 │        └─► 3.2 ZK MPC ceremony (after the STOP confirms the circuit;  │
 │             FLAG P3-G: decides 3.12's fate)                           │
 │                                                                       │
 ├─► 3.3a TOOLCHAIN + SECURITY PATCHING (single-stop; no dep churn)      │
 │     │   go 1.25 · CometBFT v0.38.25 · advisories · golangci-lint      │
 │     │   · F-5/F-6/EOL-watch CI guards · iavl (vector in isolation)    │
 │     ▼                                                                 │
 │   2.9 receipt condition-outcome (proto ⇒ its OWN vector re-baseline)  │
 │     │                                                                 │
 ├─► 3.3b SDK-FAMILY MIGRATION (two-stop) ══ GATES ══► 3.8               │
 │     │   family · block-sdk disposition · both crypto pins · sigverify │
 │     └─► then 2.8, 2.15, 2.10 (consensus surface — build once)         │
 │                                                                       │
 ├─► 3.4 mandate-bound completeness ─┐                                   │
 ├─► 3.5 gate-coverage expansion ────┤                                   │
 ├─► 3.6 upgrade/migration infra ────┼─► 3.8 GENESIS + PUBLIC TESTNET    │
 ├─► 3.7 observability ──────────────┤    (D7 gate before announcement)  │
 ├─► 3.9 tokenomics (D6, pre-testnet)┘ └─► then 2.12 (after 3.9, BY      │
 │                                          DECISION — nothing blocks it)│
 ├─► 3.10 governance + ops runbooks ───────────┘                         │
 ├─► 3.11 Chambers design doc (independent; design-only exit OK)         │
 └─► 3.12 richer schema circuit (deferrable; fate fixed at 3.2's STOP)   │
                                                                         ▼
                              MAINNET GATE CHECKLIST (§7 — beyond Phase 3 exit)
```

Critical path to testnet: **3.0 → 3.3a → 3.3b → {3.4, 3.5, 3.6, 3.7, 3.9} → 3.8**.
External path to mainnet: **3.1 → findings closed** + **3.2** (FLAG P3-F).
**3.3a is on the critical path but gates nothing** — it is pulled forward for
security, not for sequence (§3.3's Rev-1 decision block). **3.3b holds the 3.8
gate**: a patched toolchain on an EOL SDK is still an EOL SDK.

---

### 3.0 — Phase-3 plan Rev at the Phase-2 boundary *(housekeeping; entry gate)*

**TRIGGERED.** Phase 2 was declared EXITED at task **2.16** (D-095,
PHASE2_DESIGN Rev 4): all five SPEC §10 tasks complete, the §6 criterion
asserted at §6b and **re-verified live at HEAD**, and the six-task tail (2.8,
2.9, 2.10, 2.12, 2.14, 2.15) carried under one uniform rule with §5.1 rows
already written. 3.0 plans from a settled boundary rather than adjudicating one.

**What.** Reconcile this plan against as-built state — exactly the
PHASE2_DESIGN Rev-2 discipline (D-078). Confirm which Phase-2 debts actually
closed (FW-1/FW-2 — both landed at 2.7; matcher TODOs; flake fix; sigverify
checkpoint result from 2.5); re-run the §2.2 version checks that are stale by
then; re-affirm or re-cut the task list; the Rev is recorded as an ADR.

**ACCEPTED FROM TASK 2.16 (D-095) — written in, not pointed at.** Four
obligations 3.0 owns that its original acceptance line did not name:

1. **Run §3.1.2's re-derivation block from the TREE, all seven steps**, and
   diff against `AUDIT_SCOPE_1.8.md` §2.2 **plus** §3.1.1. 2.16 corrected
   §3.1.1's factual errors but did NOT re-derive the full inventory — that is
   3.0's, and it is a precondition of contracting the 3.1 engagement.
2. **Read §3.1.1 as a CORRECTION TARGET, not only as a diff base.** A grep-diff
   surfaces *additions*; it cannot surface that existing prose went FALSE. That
   is exactly how §3.1.1's D-075/D-076 bullet came to tell an auditor that
   `IntentLeg` has no condition field, eight days after FW-2 added one. For
   every bullet already in §3.1.1, re-verify the claim against the code before
   trusting the absence of a diff.
3. **Re-verify the §5.1 row SET, not only the rows.** 2.16 found four open
   Phase-2 tasks (2.8's structural fork, 2.9, 2.14, 2.15) with no row at all in
   a table headed "nothing floats" — and a row-by-row pass is structurally
   incapable of surfacing a missing row. That is AUDIT F-8's mechanism
   reproduced in the receiving document. **Enumerate from CLAUDE.md's ledger and
   from the marker census, then check the table covers the enumeration** — not
   the reverse.
4. **Treat a task's NARROWING edits as the trigger to re-check every item
   pointed at it** (D-093's input, restated because it has now recurred a fourth
   time): a narrowing edit to a receiving task silently orphans what was pointed
   at it, and nothing re-checks the pointer.
5. **RULE ON THE CANDIDATE METHODOLOGY AMENDMENT (D-095), and rule on it against
   a stated standard.** The amendment: Rule 8 step 3 ("propagate the correction
   to any document that repeated it") needs the sub-rule Rule 6 already
   learned — **enumerate the consumers FIRST**, because a correction propagated
   from memory reaches exactly the documents you remember. Task 2.7 corrected
   four documents and left five wrong, including the auditor-facing scope; that
   is not a diligence failure, it is the absence of an enumeration step.

   **Its MECHANICAL FORM, which is the part that makes it fire — carry this
   verbatim into any amendment text:**

   > After a task that STRENGTHENS a guarantee, grep for assertions of the OLD
   > guarantee's **NEGATION** — `"not enforced"`, `"never attaches"`, `"has no X
   > field"`, `"only through"`, `"is a … ceiling"` — **NOT for the new
   > mechanism's name.** Stale text by definition does not mention the new thing,
   > so searching for `condition_binding.go` or `attachFallbackCondition` finds
   > exactly the documents already corrected. The negation is what the stale text
   > actually says.

   **The standard to rule by, which generalises past this amendment: a rule that
   depends on REMEMBERING does not fire; a rule with a RUNNABLE STEP does.** Two
   independent data points from one session say the same thing — Rule 6 sub-rule
   3's explicitly-named worst case recurred **one task after the rule was
   written**, and what fixed it was a SCHEDULED SWEEP rather than a resolution to
   be careful. Apply that test to this amendment and to any other 3.0 considers:
   if it cannot be reduced to a command someone runs at a boundary, it is advice,
   and advice has a measured failure rate in this repo.
6. **A FREE CALIBRATION of obligation 3 — use it, do not just fix it.** Task
   2.16's own commit `6d6b730` is cited in neither CLAUDE.md nor
   PHASE2_DESIGN's scoreboard, because a commit cannot contain its own hash. It
   is a **known, documented, cost-free omission**, deliberately left for 3.0
   (the alternatives regress: a follow-up commit whose only content references
   the commit before it is itself uncitable, and amending would have relocated
   the gap while destroying the fingerprint chain). Its value is diagnostic:
   a row-by-row pass CANNOT surface it and a row-SET re-derivation SHOULD.
   **If 3.0's set-re-derivation catches it, obligation 3 is proven to work; if it
   misses it, obligation 3 needs sharpening — and you learn that on an item whose
   absence costs nothing** rather than on the next one that matters. Record which
   happened.

**Depends on.** Phase-2 exit — **satisfied** (2.16, D-095).

#### 3.0 — CLOSED at Rev 1 (D-096). The six obligations, each with its evidence.

| # | Obligation | Verdict | Evidence |
|---|---|---|---|
| **1** | Run §3.1.2's re-derivation **from the tree**, all seven steps; diff vs `AUDIT_SCOPE_1.8` §2.2 **and** §3.1.1 | **RUN — 4 findings** | Executed at `15c2470`, clean tree. Steps 1/3 surfaced `crypto/reveal/{reveal,stub}.go` and `cmd/conclave-demo/exit_demo.go`; step 4 returned **8** `recover()` sites against §3.1.1's **one**; step 6 returned exactly the 3 allowlisted names; step 7 returned **15/15** markers with both `phase-2` markers naming **OPEN** tasks (`solution.go:143`→2.15(a), `skim.go:37`→2.12). Findings → **F-10…F-13** (§5.1) |
| **2** | Read §3.1.1 as a **CORRECTION TARGET**, re-verifying existing bullets against code | **RUN — 1 correction, 1 sharpening** | Every bullet re-verified against the tree: vk **396 B** ✓, `IntentLeg.condition = 5` ✓ (`intent.proto:102`), `effectiveCondition` ✓ (`condition_binding.go:250`), file **297 lines** ✓, 1427/1429 both directions ✓, HASHLOCK **MAXIMUM** / OPTIMISTIC **MINIMUM** ✓ (`condition_binding.go:137-149`), `attachFallbackCondition` ✓ (`fallback.go:317`), `FundingRequirement` with **exactly** the two named readers ✓ (`escrow.go:177`, `fallback.go:197`), `nextState` ✓, `ErrGranteeConflict` on **two** write paths ✓, ante one-source-of-truth ✓. **CORRECTION: the `recover()` lives in `rateWithinBound` (`solution.go:617`), NOT in `checkLimitRate`** — an auditor sent to the named function would find no recover there. **SHARPENING: D-088's invariant is now maintained in THREE places** |
| **3** | Re-verify the §5.1 row **SET**, enumerating from the ledger + marker census | **RUN — complete for TASKS, incomplete for GAPS** | Enumerated the six open ledger tasks (2.8, 2.9, 2.10, 2.12, 2.14, 2.15) — **all six have rows**, confirming 2.16's work; zero blank disposition cells. **But the enumeration surfaced 10 items with no row at all**: G-1…G-4, F-9…F-13, and the uncited-commit hygiene item. All added at this Rev |
| **4** | Treat a task's **NARROWING** edits as the trigger to re-check every item pointed at it | **RUN — clean, the recurrence did NOT happen** | 2.7 narrowed by re-deferring two items to **2.15**; 2.13 narrowed by splitting into 2.7-enforcement + 2.14-reference. Checked the RECEIVING tasks' own criteria: **2.15 carries (a) and (b) written in, with acceptance**; **2.14 carries (a) and (b) under an explicit "written in, not pointed at."** Rule 6 sub-rule 6 satisfied on both |
| **5** | **RULE** on the D-095 candidate methodology amendment against a stated standard | **ADOPTED, mechanical form verbatim** | §8 ruling **A1** |
| **6** | The `6d6b730` **free calibration** — record which happened | **OBLIGATION 3 IS PROVEN TO WORK** | Below |

**Obligation 6 — the calibration result, recorded precisely because its whole
value is diagnostic.** The seeded item was found, **and the mechanism found more
than was seeded.**

`6d6b730` (task 2.16's own commit) appears exactly twice in the corpus — **both
occurrences inside §3.0's own description of the omission.** It remains absent
from CLAUDE.md's 2.16 row and from PHASE2_DESIGN's §0 scoreboard, exactly as the
plan said. The omission was live, and **a row-by-row pass over §5.1 could never
have surfaced it.**

The pass that *did* surface it enumerated from an **independent source** — `git
log` — and then asked whether the documents covered that enumeration. The same
pass surfaced something nobody seeded: **`15c2470`, the current HEAD, is cited
nowhere in any authority document.** That confirms the plan's own prediction that
a follow-up commit *relocates* the gap rather than closing it — it moved from
`6d6b730` to `15c2470` precisely as forecast.

**Verdict: obligation 3's mechanism WORKS and needs no sharpening. What needs
fixing is the hygiene gap it keeps re-finding** — recurring by construction, since
the newest commit can never cite its own hash. That is now **F-13**, with a
runnable check, instead of something each boundary rediscovers by hand.

**Plan-stop decisions — all three DECIDED at this Rev.**
1. **Is 3.3 pulled ahead of the Phase-2 tail?** → **YES, and it SPLITS into 3.3a
   / 3.3b.** See §3.3's preamble; reasoning at §8 ruling **B1**.
2. **Does §3.1.2's block become a committed `scripts/` artifact?** → **YES** — the
   precondition ("once 3.0 has run it once by hand and knows what it wants to
   freeze") is now met, and the two things worth freezing are known: the
   known-non-finding lists, and the `git log` citation check obligation 6 proved.
   Owned by **F-13**; deliberately NOT built inside this docs-only Rev.
3. **Where do the six tail tasks sequence?** → **§5.3.**

**Acceptance — MET.** Rev authored; every §5 disposition row re-verified **and
the row SET re-derived** (obl. 3); the authored-surface inventory re-derived from
the tree with every addition directly read (obl. 1); §3.1.1's existing bullets
re-verified against code (obl. 2); the D-095 amendment **ruled on against the
runnable-step standard** with its mechanical form carried verbatim (obl. 5); the
`6d6b730` calibration **recorded** (obl. 6); nothing floats.

### 3.1 — External cryptographic audit: adopt-vs-audit STOP, engagement, triage *[SPEC-MANDATED, NOT PHASE-NUMBERED — §8(d) mainnet gate; FLAGS P3-A, P3-F]*

**What.** Close the outstanding §8(d) obligation: the scope package exists
(`docs/AUDIT_SCOPE_1.8.md`, D-071); the engagement has never run. The task has
three checkpoints:

**O1 IS RE-SCOPED AT REV 1 — and the re-scoping is a LEAD FOR THE AUDITOR, not a
result handed to them.** *(D-096, from the re-evaluation's literature pass.)*

`id = H(DST‖C_dem)` is **structurally recognisable as** an instance of the
**CHK/BCHK "CCA-from-IBE" paradigm** (Boneh–Canetti–Halevi–Katz, *SIAM J.
Comput.* 36(5), 2007), which binds the IBE identity to the ciphertext **by
design** — on that reading **tlock's fixed round number is the unusual case**, and
Conclave's divergence runs *toward* the CCA literature rather than away from it.
That reframing changes O1's expected cost and calendar, which is why it is
recorded here.

> **CARRY THIS CAUTION INTO THE ENGAGEMENT LETTER, VERBATIM.** The placement is
> the auditor's **STARTING POINT**, phrased as a task: *"verify that this
> placement is legitimate, and that the AEAD tag + IBE FO `rP` check together
> discharge the role CHK assigns to the one-time signature."* It is **NOT** an
> established premise. An auditor handed *"we have established this is CHK/BCHK"*
> may accept the framing and let it do all the work — which would convert an
> unproven security argument into an assumption at exactly the point the
> engagement exists to test. The sharpened question, which is the deliverable:
> **does the AES-GCM tag + FO check discharge CHK's one-time-signature role, and
> does that survive thresholdisation with PUBLISHED partial decryptions?**

**The result with teeth, and it lands on a sentence we have already written.**
**PKC 2026, eprint 2025/1665** (Brzuska–Klooß–Woo) shows that security under
*maximal* corruptions does **NOT** imply security under *fewer* corruptions when
the adversary also holds partial decryptions of challenge ciphertexts. Conclave
publishes VE shares for **every** sealed ciphertext. That lands directly on
`AUDIT_SCOPE_1.8` **§7's "confidentiality holds against any coalition of < t"**,
which is stated as though corruption composes monotonically. **§7's sentence is
hereby FLAGGED for explicit re-examination as a named audit sub-question** — it
is not withdrawn (no counterexample against *our* construction is known), but it
may no longer be asserted as self-evident, and the engagement must return a
verdict on it specifically. Nearest published construction: **eprint 2025/341**
(CCS 2025), CCA-secure threshold Boneh–Franklin IBE, motivated by mempool
privacy — still **no audited implementation**, so Q2(a)'s core answer holds.

1. **STOP #1 — adopt-vs-audit (the fork research enables).** Decide, as a real
   isolated stop, which construction goes to audit:
   - **(a) Audit as-built.** O1 as scoped: prove/refute IND-CCA of the
     composed KEM/DEM under attacker-influenced identity `id = H(DST‖C_dem)`.
     Cost: the divergence is Conclave's alone; the proof burden is maximal.
     Benefit: zero rebuild; the shipped, localnet-proven system is the audited
     system. **Rev 1 lowers this option's expected cost** via the CHK/BCHK
     placement above — subject to the caution that the placement is a lead.
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
- **The D-075 / D-076 / D-094 consent chokepoints, and `x/session/keeper/`
  `condition_binding.go` (297 lines, NEW at task 2.7)** — the highest-value
  authored security surface Phase 2 produced, and the first two holes were
  **Rule-4 holes found in review, not by test**. **This bullet was corrected at
  task 2.16 (D-095) and the correction is the point:** it previously told an
  auditor that *"`IntentLeg` has no condition field"* and described the binding
  as one-directional. Both statements went FALSE at 2.7 and the bullet would
  have steered a reviewer **away from the newest, least-reviewed consent code in
  the tree**. As built at `7283b11`:
  - A condition may be carried at the payload level (`IntentPayload.condition`)
    **or per-leg (`IntentLeg.condition = 5`, FW-2)**, the per-leg field REFINING
    the payload one; `effectiveCondition` resolves which governs a given leg for
    a given party. Verify the resolver cannot be made to select a condition the
    party did not sign, and that present-but-empty does not exempt a leg
    (D-094 records it fail-closed — check it).
  - **Both directions are enforced, at both chokepoints:** `attached ⇒ signed`
    (`ErrConditionNotConsented` 1427) and `signed ⇒ attached`
    (`ErrConditionNotHonored` 1429). D-094 records that the operative fix was
    the **loop bound**, not the predicate — both chokepoints previously walked
    only already-conditioned legs, so a dropped condition was the one case they
    could never see. **Verify the bounds independently**; a predicate that is
    correct over the wrong domain is the exact defect that shipped here once.
  - The buyer's relation is **per-class and directional**: seller and ZK buyer
    EXACT; HASHLOCK buyer `timeout ≤ signed` (a MAXIMUM — the timer is when the
    buyer's locked payment refunds); OPTIMISTIC buyer `window ≥ signed` (a
    MINIMUM — the window IS the inspect-and-dispute protection). Verify each
    direction is the monotone-SAFE one for the party at risk, and that the two
    cannot be transposed.
  - **`attachFallbackCondition`** (`x/session/keeper/fallback.go`) — the
    in-consensus matcher now WRITES a delivery condition into a settlement leg.
    This is new consensus-path authority surface: verify a fallback clearing
    cannot attach a condition no party signed, and that it is deterministic
    across nodes.
  - **`FundingRequirement`** (`x/escrow/types/params.go`) — one definition of
    escrow amounts, read by both `CreateEscrow` and the fallback's affordability
    pre-check. Verify the two readers cannot diverge (this is D-085's shape, and
    D-085's lesson is what happens when they do).

  Verify all of the above are unbypassable, including from contract-originated
  flows (D-079 claims they are structurally unreachable from CVM — check it) and
  across 2.6's new partial-fill paths (D-089 claims a DataGrant is exact-fill
  only in both new matchers, so no partial fill can arise on a conditioned leg).
  ~~**Still open, and NOT part of the authored surface to audit — task 2.14:**
  non-test `solver/` never reads `IntentPayload.condition`, so the reference
  client does not yet meet the rule the chain now enforces.~~ **CLOSED by task
  2.14 (D-097).** The reference client now meets it — and the auditor should note
  that this ADDED authored surface on the consensus side: `x/session/keeper`
  exports `CheckConditionBinding`, `EffectiveSellerCondition`, and
  `CheckConditionedMatchShape` so the off-chain solver CALLS the chain's rule
  instead of mirroring it. The exported functions are the same code the
  chokepoints run (pure extraction, no behaviour change), but their being
  exported is itself an audit-relevant fact: an off-chain consumer now depends on
  their exact semantics.
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
- **D-089 — the §5.1 limit-RATE clause, NEW consensus panic surface.** The clause
  runs inside **EndBlock**, `LegacyDec.MulInt` panics on overflow, and **nothing
  bounds an agent-signed `limit`** — so it is guarded by a `recover()` that fails
  closed. Verify the recover's scope is exactly the arithmetic, that it is
  deterministic across nodes, and that failing closed is the right posture (a
  rejected match, never a halted chain).

  > **CORRECTED AT REV 1 (obligation 2, D-096).** This bullet previously said the
  > `recover()` is carried by **`checkLimitRate`**. It is not. `checkLimitRate`
  > (`x/session/keeper/solution.go:461`) contains **no** `recover`; the guard
  > lives in **`rateWithinBound` (`solution.go:617`)**, which `checkLimitRate`
  > calls at `:494` and `:527`. An auditor sent to the named function would have
  > found no recover and drawn the wrong conclusion about a consensus panic
  > boundary. The distinction also matters to the review itself: `rateWithinBound`
  > is deliberately the NARROWER guard (it takes no `ctx`, touches no store, and
  > calls only `LegacyDec`/`Int` operators, so the only reachable panic in its
  > frame is the range assertion) — and the code says so in a comment that must
  > stay true. **Verify the scope claim against `rateWithinBound`, not against
  > `checkLimitRate`.**

- **ALL EIGHT consensus-path `recover()` sites — the full census, NEW at Rev 1.**
  §3.1.2 step 4 returns **8** non-test sites; the bullet above named **one**. Each
  is a fail-closed panic boundary on a consensus or crypto path, so each is an
  audit target and each must be checked for the same three properties (scope is
  exactly what the comment claims · deterministic across nodes · failing closed is
  the right posture). **A recover whose scope is wider than its comment silently
  swallows unrelated bugs, which is the failure mode this census exists to
  prevent.**

  | # | Site | Guards | Fails closed to | Note for the auditor |
  |---|---|---|---|---|
  | 1 | `x/veil/keeper/lifecycle.go:408` `revealIntent` | the whole per-intent reveal (stub **and** real threshold path) | `revealMalformed` | **The widest of the eight and the most security-bearing** — it wraps `Combine` over attacker-supplied ciphertext AND the epoch scheme. Verify a crafted ciphertext cannot reach a *silent* wrong-plaintext rather than a recovered panic |
  | 2 | `x/veil/keeper/lifecycle.go:645` `decodePayload` | proto-unmarshal of recovered plaintext | `ok=false` → §6.3 INVALID | The §6.3 "payload that fails proto-unmarshal at Reveal" path |
  | 3 | `x/veil/keeper/lifecycle.go:690` `giveLegValue` | give-leg valuation via `PriceSource` | empty `sdk.Coins` | **Interacts with 3.4**: failing closed to *zero spend* is safe only while the D-065 cross-check treats zero as "no claim". Re-verify after the F-3 price-source swap lands |
  | 4 | `x/mandate/ante/ante.go:227` `spendCommitment` | envelope decode **in the ante** | zero commitment | The one site on the **ante** path, not EndBlock. Verify the zero-commitment fallback cannot under-charge a well-formed envelope (D-085's shape) |
  | 5 | `x/session/keeper/solution.go:619` `rateWithinBound` | the rate arithmetic only | `within,ok = false,false` | The D-089 site — see the correction above |
  | 6 | `x/session/keeper/solution.go:776` `counterpartySatisfied` | whole §6.4 rule-5 predicate evaluation | `ok=false` (Decision A) | Deliberately **wider** than #5 because it wraps predicate parse + eval; it *does* take `ctx` and read the store. Verify a store error cannot be laundered into "predicate false" |
  | 7 | `x/escrow/keeper/sweep.go:25` `EndBlocker` | the **entire escrow EndBlock sweep** | returns `error` (§11.5) | Second-widest. Verify a single malformed record cannot silently abort the sweep for *other* due escrows in the same block |
  | 8 | `crypto/zkdelivery/verify.go:80` `Verify` | groth16 verification over adversarial proof bytes | `ErrInvalidProof` | Already an audit unit via the D-076 rows; listed here so the census is complete |

- **`crypto/reveal/{reveal.go,stub.go}` — the epoch-0 StubScheme, NEW at Rev 1.**
  The §3.1.2 key-material sweep surfaces these and neither `AUDIT_SCOPE §2.2` nor
  this section named them as targets. `stub.go` is the **identity** scheme used
  while `epoch_id == 0` — i.e. it is the literal implementation of the **L1
  epoch-0 plaintext window** that FLAG P3-D is about. Verify the cutover cannot be
  induced backwards (an intent sealed under a real epoch must never be revealable
  through the stub), and that the seam cannot be selected by anything an attacker
  influences.

- **Out-of-scope by decision, recorded so the next re-derivation does not
  re-investigate:** `cmd/conclave-demo/exit_demo.go` and `crypto/zkdelivery/
  {gen/main.go,zktest/prove.go}` carry crypto imports but are **dev tooling** —
  they enter no consensus path and ship in no validator binary. Named here
  because §3.1.2's grep will surface them at every future boundary.
- **D-088 — the store-iterator range ends.** A truncated exclusive upper bound
  made 1-in-256 records invisible on a consensus path (the §6.4 rule-5
  `has_credential` predicate). Fixed with the SDK's `PrefixEndBytes`; verify no
  remaining iterator anywhere constructs its range end by appending `0xFF`.

  > **SHARPENED AT REV 1 (obligation 2, D-096) — the bullet as written PASSES and
  > still under-describes the risk.** Re-derivation confirms **no** site appends
  > `0xFF`. But the invariant is now maintained in **THREE independent
  > implementations**: the SDK's `storetypes.PrefixEndBytes` (used by `x/sigil`,
  > `x/mandate`), plus **two hand-rolled copies** — `x/veil/keeper.prefixEnd`
  > (`intent.go:163`) and `x/session/types.PrefixEnd` (`keys.go:236`). All three
  > are currently byte-equivalent, **so this is a latent divergence, not a live
  > defect.** It is exactly **D-085's shape** (two readers of one rule that can
  > drift apart) applied to D-088's own fix, and the audit question is therefore
  > not "does any site append `0xFF`" but **"can these three implementations ever
  > disagree, and what re-checks them?"** Owned by **F-12**.
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
# 2. authorization chokepoints: every fail-closed authority/consent rejection.
#    KEYED ON THE REGISTRATION SITE, NOT ON A NAME LIST (sharpened at task 2.16,
#    D-095). The original form hardcoded a 2026-07-28 inventory of error names
#    and therefore went stale by construction — a grep that ages is a poor
#    anti-staleness mechanism. It missed 1426/1428/1429 entirely, and
#    condition_binding.go surfaced only because it happens to ALSO use 1427:
#    the mechanism worked BY LUCK, which is not a mechanism.
#    Step (a) derives the current inventory from the tree; step (b) uses it.
grep -rn 'errorsmod\.Register' --include='*.go' x/ app/ | grep -v '_test.go'
ERRS=$(grep -rhoE '^\s*(Err[A-Za-z0-9_]+)\s*=\s*errorsmod\.Register' \
       --include='*.go' x/ app/ | grep -oE 'Err[A-Za-z0-9_]+' | sort -u | paste -sd'|')
grep -rnE "($ERRS)" --include='*.go' x/ app/ | grep -v '_test.go'
# 3. key material + share custody
grep -rn 'PrivKey\|SecretShare\|KeyShare\|ShareProvider\|dkg_shares' \
     --include='*.go' . | grep -v '_test.go'
# 4. consensus-path recover() sites — each is a fail-closed panic boundary
grep -rn 'recover()' --include='*.go' x/ app/ crypto/ | grep -v '_test.go'
# 5. ante-chain composition (order is security-bearing)
grep -n 'Decorator' app/ante.go

# 6. CITATION RESOLUTION — every Test* name cited by an authority document must
#    resolve in the tree (METHODOLOGY §11's boundary checklist, made executable
#    at task 2.16 / D-095). Until then the check existed only as a manual
#    checklist line, so it was reproducible only by whoever reinvented the
#    command — and the boundary audit duly found a citation to a test task 2.7
#    had DELETED, live in three places. Output must be EMPTY.
DOCS="docs/EXIT_DEMO.md docs/PHASE2_DESIGN.md docs/PHASE3_PLAN.md \
      docs/edge/X402_SURFACE.md docs/edge/7683_SURFACE.md docs/AUDIT.md \
      docs/AUDIT_SCOPE_1.8.md CLAUDE.md docs/METHODOLOGY.md"
# grep -v '^>' DROPS keep-and-mark blocks: a SUPERSEDED block QUOTES the old
# claim, so it legitimately cites a test that no longer exists. This is why the
# Rule-8 convention is a Markdown blockquote (and a tab-indented Go comment) —
# it is not decoration, it is what makes this sweep DISCRIMINATING. Without the
# filter the check is red forever, and a check that is always red is one the
# team learns to ignore (Rule 7 step 4).
comm -23 <(grep -rhE 'Test[A-Za-z0-9_]+' $DOCS | grep -v '^\s*>' \
           | grep -oE 'Test[A-Za-z0-9_]+' | sort -u) \
         <(grep -rhoE '^func (Test[A-Za-z0-9_]+)' --include='*_test.go' . \
           | sed 's/^func //' | sort -u)
# KNOWN NON-FINDINGS — anything NOT on this list is a finding. Each entry names
# why, so a future reader does not re-investigate, and adding one is a decision
# recorded in the task that deletes the test (D-095).
#   TestKyberRecoverCommit — a doc WRAPS …SelectsLowestIndex across two lines,
#     so the prefix looks unresolved. Confirm any hit by grepping the prefix.
#   Testnet — an English word, not a citation.
#   TestSignedConditionIsPermissionNotRequirement_BoundaryPin — DELETED by task
#     2.7 (D-094), which REPLACED it with
#     TestSignedConditionIsRequirement_Enforced (kept on ONE line on purpose:
#     wrapping a test name is what creates this sweep's false positives).
#     Its surviving occurrences are QUOTED historical claims inside
#     marked supersession blocks, retained deliberately. Legitimate in exactly
#     four places: the retained pre-2.7 problem statement and task 2.13's row in
#     CLAUDE.md, plus the marked blocks in EXIT_DEMO.md and PHASE2_DESIGN.md.
#     A FIFTH occurrence, or one outside a marked block, IS a finding.
#
# WHY AN ALLOWLIST AND NOT A WIDER FILTER (the honest tradeoff, D-095): the `>`
# filter handles prose documents, whose keep-and-mark convention is a Markdown
# blockquote. CLAUDE.md's LEDGER rows mark supersession with strikethrough plus
# an inline forward pointer instead, because a blockquote cannot nest cleanly in
# a checklist item — so format-based filtering cannot reach them. Widening the
# filter until the check goes green would have blinded it to real re-citations;
# naming the exception keeps it discriminating. The cost is that this list must
# be maintained, which is the same cost every findings list carries (Rule 6).

# 7. MARKER OWNERSHIP — every CONCLAVE-TODO must name a task that is still OPEN.
#    A marker naming a COMPLETED task is Rule 6 sub-rule 3's worst case: it
#    reads as pending while being unowned, and is invisible to "is anything
#    unowned?" checks. Found once at the Phase-2 boundary (solution.go:143
#    naming the closed task 2.7 — D-095).
grep -rn 'CONCLAVE-TODO' --include='*.go' --include='*.rs' --include='*.sh' \
     --include='*.proto' . | grep -oE 'CONCLAVE-TODO\([a-z0-9+.-]*\)' | sort | uniq -c
```

**KNOWN NON-FINDINGS for step 7 (added at Rev 1 — each cost an investigation
once, and naming them means it happens once).** The raw line count of
`CONCLAVE-TODO` in the tree is **17**; the tagged census returns **15**. The
delta is **not** an undercount and the tag-grep is **not** blind: two lines
mention the marker string in *prose* while describing a marker that was
**resolved in phase 0.4** — `x/session/keeper/solution.go:157` and
`x/session/types/expected_keepers.go:51`, both reading *"resolving the 0.4
CONCLAVE-TODO"*. They are references, not markers. **15/15 is the correct
census**, and at `15c2470` both `phase-2` markers name OPEN tasks
(`solution.go:143`→**2.15(a)**, `skim.go:37`→**2.12**).

**A calibration note on step 6's allowlist (Rev 1).** Within the swept `$DOCS`
set the deleted name `TestSignedConditionIsPermissionNotRequirement_BoundaryPin`
occurs **five** times, while the allowlist declares four legitimate places and
says a fifth "IS a finding." The fifth is **the allowlist's own text in this
document** — the entry naming the test necessarily contains the test's name. This
is a self-reference, not a re-citation, and it is recorded rather than
"fixed by widening", because a check that can never come back clean is one the
team learns to ignore (Rule 7 step 4). **The rule to apply: five occurrences of
which one is this allowlist entry = CLEAN; a sixth, or any occurrence outside a
marked block, IS a finding.**

**A real `scripts/` implementation of this block is deliberately NOT created
here** (decided at 2.16, and **re-decided at 3.0**): the block is executable as
written, and turning it into a committed artifact was 3.0's call once 3.0 had run
it once by hand and knew what it wanted to freeze. **3.0 has now run it, and the
answer is YES — see F-13**, which also carries the one check this block does NOT
have and obligation 6 proved it needs: **enumerate commits from `git log` and
assert every one is cited somewhere.** The build is F-13's, not this docs-only
Rev's.

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

### 3.3 — Platform re-baseline *[PROPOSED — BEYOND CURRENT SPEC; FLAG P3-B: GATES 3.8]*

> ## REV 1 DECISION: 3.3 **SPLITS**, and 3.3a goes FIRST — ahead of the Phase-2 tail (D-096)
>
> **This is a decision, not an inheritance** — the plan-stop asked "must 3.3 be
> pulled even earlier?", and the spike changed the answer's basis, so it is
> re-derived here with reasoning rather than carried forward.
>
> **The spike decoupled two things the original 3.3 fused.** Bumping the
> toolchain was believed to require the SDK-family migration (because the kyber
> pin was believed to block the toolchain). It does not: `go 1.25.12` with
> **every pin untouched** is green on the full suite and vector-identical to
> D-082. So the security-patch axis and the platform-migration axis are now
> **separable**, and fusing them would mean holding 16 reachable advisories
> hostage to a multi-week SDK migration.
>
> | | **3.3a — toolchain + security patching** | **3.3b — SDK-family migration** *(two-stop)* |
> |---|---|---|
> | Content | `go` directive → a supported line; CometBFT v0.38.23→**.25**; the reachable-advisory remediations (grpc, x/net, go-getter); **golangci-lint upgraded in the same task**; F-5/F-6 CI guards + the dependency-EOL watch | SDK v0.50.15 → active family; ibc-go v8 → the family line; wasmd to match; **block-sdk disposition**; both crypto pins re-validated together; sigverify-fork retirement |
> | Dependency churn | **Effectively none** — (a) proved the directive bump alone is green; the advisory bumps are leaf/indirect | Large — one compile error at `app/cvm/encoder.go:58` is the *source* surface, but behaviour is untested |
> | Risk | Low, and **measured**: build/vet/test/vector all green at 1.25.12 | Unmeasured: lanes, `PrepareProposal`, VE/reveal e2e, CGO/libwasmvm packaging, vector re-baseline |
> | Gate | Vector must stay **byte-identical to D-082** (it did in the spike) | Vector re-baseline **expected**; recorded as an ADR |
>
> **Why 3.3a outranks the Phase-2 tail.** The re-evaluation's point stands and is
> now sharper: **if the 16 reachable advisories remediate through 3.3, then 3.3 is
> the project's security-patch vehicle**, and a security-patch vehicle does not
> queue behind reference-solver ergonomics. 3.3a is cheap, measured, and
> reversible; nothing in the tail is exit-blocking or time-critical. **Only 2.14
> is argued ahead of it** (§5.3) and only because it is a live functional defect
> whose fix is independent of the platform.
>
> **Why 3.3b does NOT get pulled forward with it.** 3.3b is exactly the work whose
> risk the spike did **not** retire. Nothing in (g) licenses "the migration is
> small" — (g) was **compile-only**: no tests, no vector, no lane or VE or reveal
> behaviour, no CGO packaging. 3.3b keeps its two-stop shape and its position.
>
> **3.3b still GATES 3.8** (FLAG P3-B). 3.3a does not discharge that gate: a
> patched toolchain on an EOL SDK is still an EOL SDK.

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
**Two stops (hard-to-reverse foundation — METHODOLOGY Rule 2). These belong to
3.3b; 3.3a is single-stop, because the spike already IS its decision evidence.**
- **STOP #1 (decision):** target family — **2025.1** (SDK 0.53.x + CometBFT
  0.38.x + ibc-go v10; keeps the exact CometBFT line the VE surface is proven
  on; native unordered txs; wasmd 0.6x line) vs **2026.1** (SDK 0.54.x/0.55.x +
  CometBFT 0.39.x/0.40.x + ibc-go v11; BlockSTM + native BLS — bigger jump,
  unknown interaction with lanes/VE/deterministic ordering; note 2025.1 retires
  when the next family lands, per the two-family policy). Plus the block-sdk
  disposition and the two-pin re-validation plan. Presented with a D-077-style
  empirical spike table, reviewed in isolation.

  > **REV 1 INPUTS TO STOP #1 — derive the option table AT TASK TIME, never from
  > this prose (the 2026.1 family moved twice in a month).** Established by the
  > 3.0 spike (§2.3): the 2025.1 set resolves MVS-clean with the directive
  > settling at **exactly `go 1.23.8`** and **nothing transitive above it**;
  > `wasmd v0.60.8` (not v0.61.x) is the SDK-v0.53 line inside that floor, which
  > **D-077's table missed**; `block-sdk v2.1.5` **compiles** against SDK v0.53.8,
  > so the disposition is a **behavioural** question, not a compile one; the
  > source surface of the jump is **one** signature change at
  > `app/cvm/encoder.go:58`. **All of this is compile-only evidence** — it sizes
  > the decision, it does not make it.
  >
  > **Item 0, and it does not wait for the family choice:** CometBFT
  > **v0.38.23 → v0.38.25** (consensus off-by-one in `double_sign_check_height`,
  > mempool race, heap-amplification DoS guard) declares `go 1.22.11` and costs
  > **nothing** on the toolchain axis. It belongs to **3.3a**.
- **STOP #2 (post-wiring review):** the migrated tree's full evidence —
  determinism vector re-baseline (recorded ADR; expect drift on an SDK major —
  bisect dep-bumps vs wiring per the D-078 lesson), all Phase-0/1/2 suites,
  unordered conformance, lane routing, VE share transport + reveal e2e,
  localnet4 relaunch (real DKG → encrypted reveal), CGO/libwasmvm packaging.
**Depends on.** 3.0. Nothing else starts against the OLD platform that would
have to be redone (3.4/3.5 sequence AFTER **3.3b** for this reason; 3.1/3.9/3.10/
3.11 are platform-independent and run in parallel).

**Acceptance — 3.3a.** Supported Go line live; **`golangci-lint` upgraded in the
same task and CI green on it** (see below); CometBFT at v0.38.25; every
remediable reachable advisory from the `govulncheck -mode binary` census either
closed or carried with a written reason; **determinism vector byte-identical to
D-082** (the spike says it will be — if it is not, that is a real finding, not a
re-baseline); CI guards (F-5 float ban, F-6 goleveldb pin, dependency-EOL watch)
live and **watched red**; `govulncheck` re-run and the before/after counts
recorded in the ADR.

> **NAMED WORK ITEM, on 3.3a's critical path — not a footnote.** `golangci-lint`
> **hard-fails on any `go` newer than the version the linter binary was built
> with**: `can't load config: the Go language version (go1.24) used to build
> golangci-lint is lower than the targeted Go version (1.25.12)` — observed in the
> spike, exit 2. It is **not** a toolchain ceiling (it is a dev-tooling install),
> but it **stops `make lint` and therefore CI on the day the bump lands**. Upgrade
> the linter **in the same commit** as the directive, and pin its version
> alongside the toolchain so the coupling is explicit rather than rediscovered.

**Acceptance — 3.3b.** Active release family everywhere; the laned-mempool answer
proven by **the lane suites on the new line** (compile-clean is explicitly NOT
sufficient — §2.3's limits); both crypto pins re-validated in one ADR (the §4
escape-hatch discipline, executed); sigverify fork retired-or-re-recorded; vector
re-baselined by recorded ADR; localnet4 green end-to-end (real DKG → encrypted
reveal); CGO/libwasmvm packaging proven.
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
**Depends on.** 3.3b (don't harden the old platform's gate twice).

**Acceptance — WIDENED AT REV 1 to all FIVE Category-1 shadows, one verdict per
shadow (D-096).** The original acceptance line named only the reveal path. But
**five** distinct Category-1 shadows are now on record — the generator cannot
reach any of them, so each is *structurally* unreachable rather than merely
untested, and an acceptance criterion that names one licenses silence on four.
**3.5 must return a per-shadow verdict of either "the gate now reaches it (with a
watched sabotage run)" or "hand-built adversarial fixtures are the permanent
primary gate here, and this is the named fixture."**

| Shadow | Source | What the vector cannot reach | Required verdict |
|---|---|---|---|
| **1. Epoch-inert reveal** | **D-069** | the real `epoch_id ≠ 0` path — the cryptographic Veil the vector exists to guard | reach it, watched: corrupt the fixture commits ⇒ vector RED **by name** |
| **2. Fallback Solution write** | **D-086** | the no-solver default path; the sim generates no mutually fillable pair | reach it, or name the covering fixture |
| **3. `0xFF`-prefixed ids** | **D-088** | a live 1-in-256 consensus bug passed 5 seeds | forced-`0xFF` fixture is the primary gate — **ratify explicitly** |
| **4. Matcher rich paths** | **D-089** | `limit`/`allow_partial` never set; one leg per side; one coin give-asset | widen the generator **or** ratify fixtures — record which |
| **5. Consent/condition paths** | **D-094** | conditioned legs, per-class buyer relations, the fallback attacher | name the covering fixtures; the vector is backstop only |

Plus: posture ADRs recorded (wasm); new fuzz targets in `make test` or a named CI
lane. **The generator-widening sub-item (§5.1) is decided HERE, once, for all five
— not per-shadow by drift.**
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
| 3.0 | Rev at Phase-2 boundary | housekeeping | entry gate for all — ✅ **CLOSED, Rev 1, D-096** |
| 3.1 | Audit: STOP + engagement + triage | SPEC §8(d) mainnet gate | gates MAINNET; starts first (FLAG P3-A) |
| 3.2 | ZK MPC ceremony + VK swap | D-076 mainnet gate | after 3.1 STOP; decides 3.12 |
| **3.3a** | **Toolchain + security patching** *(single-stop)* | PROPOSED | **the security-patch vehicle**; ahead of the Phase-2 tail except 2.14 (§5.3) |
| **3.3b** | **SDK-family migration** *(two-stop)* | PROPOSED | **gates 3.8** (FLAG P3-B); gates 3.4/3.5/3.6/3.7 |
| 3.4 | Mandate-bound completeness | AUDIT F-2/F-3 debts | after **3.3b**; before 3.8 |
| 3.5 | Gate-coverage expansion | PROPOSED | after **3.3b**; before 3.8 |
| 3.6 | Upgrade/migration infra | PROPOSED | after **3.3b**; before 3.8 |
| 3.7 | Observability + L1 alarm | PROPOSED (discharges D-069 obligation) | after **3.3b**; before 3.8 |
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
| T2 kyber lowest-index premise | D-067 | → **3.1** (verify) + **3.3b** (pin-bump re-runs the probe). *Rev 1: the 3.0 spike already ran this probe **50/50 under each of go1.23.12 / go1.25.12 / go1.26.5**, so a Go bump alone does not disturb T2; the re-run remains owed for a `kyber-bls12381 v0.3.4` adoption.* |
| T3 share custody / daemon boundary | D-070 | → **3.1** (review) + **3.10** (custody runbook) |
| **T4 dependency pin & license** *(added D-090)* | D-066 §8 | → **3.3b** — the two-pin double-re-validation IS T4's re-verification (`kyber`/`kyber-bls12381` MPL-2.0 re-checked on the bump, no transitive divergent BLS12-381) + **3.1** (auditor confirms the pinned versions are the audited ones) |
| **L3 per-share proof = intrinsic pairing check** *(added D-090)* | D-066, FLAG III | → **3.1**, jointly with O1 — L3 says so itself ("verify the two together"): the same `id = H(DST‖C_dem)` per-ciphertext binding that O1 interrogates for IND-CCA is what makes a share non-replayable across ciphertexts |
| **A-bind, A-barriers, A-floor, A-F4, A-inject, A-reg, A-just** *(added D-090)* | AUDIT_SCOPE_1.8 §4 | → **3.1** — these are the arguments already constructed, handed to the auditor to CHECK rather than reconstruct; each names its code and its evidence tests. They had no rows before, which risked an engagement scoped to O1–O4 alone and silently dropping seven security arguments |
| VK MPC ceremony | D-076 | → **3.2** |
| Circuit-soundness audit unit | D-076 | → **3.1** scope extension |
| gnark-verifier-in-consensus audit unit | D-076 | → **3.1** scope extension |
| L1 epoch-0 plaintext window | D-069 | → **3.7** (alarm) + **3.8** (drill, docs, `require_encryption` plan-stop) |
| L2 unsafe default cap | D-070 | → **3.8** genesis policy |
| F-2 unvalued msg types | AUDIT | → **3.4** |
| F-3 / D-020 / D-044 PriceSource swap | AUDIT | → **3.4** |
| F-5 float ban review-only | AUDIT | → **3.3a** CI guard |
| F-6 goleveldb pin unguarded | AUDIT | → **3.3a** CI guard |
| **F-7 §5.1 limit rate ignored (D-049)** *(added D-090)* | AUDIT | ✅ **CLOSED by task 2.6** (D-089) — recorded against F-7 for the first time at D-090; it had been closed-in-fact and open-on-paper |
| **F-8 stale `CONCLAVE-TODO(phase-0)` markers** *(added D-090)* | AUDIT | ✅ **CLOSED by the Rev-3 marker sweep** (D-090). It was unowned for two phases and independently rediscovered — see docs/AUDIT.md's disposition section for the transferable lesson |
| Two-pin go-1.23 ceiling (now EOL) | PHASE2 §4, R2 | → **3.3a** (the Go bump, which the 3.0 spike proved needs NEITHER pin touched) + **3.3b** (the actual two-pin re-validation, if v0.3.4 / gnark v0.14+ are adopted). **Rev 1 re-frames this row:** PHASE2_DESIGN §4's invariant was never a constraint on the `go` directive — see §2.3(a) and §2.2 R2 |
| block-sdk archived | D-077, R1 | → **3.3b** STOP #1 disposition. *Rev 1: **v2.1.5 is the last tag that will ever exist** (the proxy version list ends there), and it **compiles** against SDK v0.53.8 — so the disposition is a BEHAVIOURAL question (lanes, PrepareProposal), not a compile one.* |
| SDK v0.50 / ibc-go v8 EOL | R1 | → **3.3b** (new since D-077 — see §5.2 advisory). *Rev 1: **wasmd v0.60.8** is the SDK-v0.53 line at floor `go 1.23.8` — the line D-077's table never saw.* |
| Sigverify fork retirement | D-027/D-077 | → 2.5 checkpoint result; residual → **3.3b** (the fork's whole purpose dissolves on v0.53+ native ADR-070) |
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
| FW-1, FW-2 (+ the two D-089 re-points) | D-075/076/078/089 | ✅ **CLOSED by task 2.7** (`7283b11`, D-094). FW-1(a)+(b) landed as ONE per-class relation; FW-2's `IntentLeg.condition = 5` shipped with a real reader (`effectiveCondition`). *Updated D-090:* `timeouts→params` was owned by BOTH 2.7 and 2.8 — resolved to **2.8**. *Updated D-095:* the two D-089 re-points were **explicitly re-deferred by 2.7 to new task 2.15**, each with a written verdict (TODO 9 on SCOPE alone; the 1428 lift NOT enabled by FW-2) — so this row is closed and 2.15's row above owns what remains |
| 5 matcher TODOs + `randAddr()` flake | D-072/078/088/089 | *Updated D-090 — this row was stale:* **3 closed by 2.6** (limit rate, cycles, 2-party partial fill); **2 split into new Phase-2 tasks** (2.10 in-consensus cycle-aware fallback, 2.12 multi-asset in-kind skim); **1 re-pointed** to 2.7/FW-2 (multi-intent-per-sigil); the "flake" was **not a flaky fixture** — it was a real 1-in-256 store-iterator bug shipped as **task 2.11** (D-088). *Updated D-095:* the sixth item (multi-intent-per-sigil) is now **2.15(a)**, not 2.7 — its in-tree marker was retargeted at 2.16, having named 2.7 after 2.7 closed. 2.10 and 2.12 have their own rows below |
| **2.10 in-consensus cycle-aware fallback** *(added D-095)* | D-089 | **Phase-2 tail.** The second half of the old `fallback.go:35` TODO. **Stated design input, carried so it is not rediscovered: the determinism vector CANNOT be its primary gate** — hand-built adversarial fixtures (cycle shapes, tie orderings, enumeration bounds, node-identical traversal) are, with the vector as regression backstop only. Enumeration must be bounded by an explicit param and runs in EndBlock where a panic halts the chain. The D-074 bilateral constraint on conditioned legs must be preserved (a conditioned leg must not become reachable through a cycle) |
| **2.12 multi-asset in-kind skim + in-kind solver payout** *(added D-095)* | D-089 | **Phase-2 tail. SEQUENCED AFTER 3.9 BY DECISION (D-095) — it is NOT blocked, and the distinction is the point.** No document gates 2.12 on anything: its row calls it a fee-MODEL question in the sense of **D-050/D-051** (Conclave ADRs, already decided), and its plan-stop question — *is an authorized `svc/{schema}` mint or a quantity-less `data/{hash}` grant skimmable at all* — is answerable from **D-048**'s asset-class split with no tokenomics model. It is sequenced after **3.9** because that question is an economics question 3.9 ratifies, and this row is where that sequencing decision is written down. **Recording it as "blocked on D6" would be a floating phrase asserting a dependency no document creates** — precisely the failure that produced audit finding F-3, and precisely what this table exists to prevent |
| Localnet contract smoke | D-079 | ✅ **DISCHARGED in §6a** (`65376ab`, D-091) — a purpose-built CVM-schema contract, uploaded through a passing gov proposal, released a real ZK escrow with a real groth16 proof |
| **Live relayer USDC demo** *(added D-090)* | D-082 | ✅ **DISCHARGED in §6b** (`1aecfbf`, D-092) — real ICS-20 uusdc over a live `cosmos/relayer` v2.6.0 process; **D-082 DISCHARGED**; re-run green at HEAD by 2.16 |
| **§6 exit demo itself** *(added D-090)* | SPEC §10 | ✅ **DONE — the trigger FIRED.** §6a (`65376ab`, D-091) + §6b (`1aecfbf`, D-092) asserted the criterion; **re-run green at HEAD by task 2.16** after 2.7's semantic inversion, which converted the assertion from inference to evidence. The hard condition STANDS: if its output is ever used as EXTERNAL-FACING evidence, task **2.9** is a prerequisite (D-087's `condition_outcome` reads NONE for the conditioned ZK buy), and 2.7 widened it by making conditioned settlements more common |
| **THE PHASE-2 TAIL — six open tasks, one uniform rule** *(added D-095, task 2.16)* | CLAUDE.md ledger | **Phase 2 EXITED with these open; none is exit-blocking**, established by re-deriving the test from SPEC §10's text rather than inheriting each row's claim. They keep their `2.x` numbers (identifiers, not a schedule — renumbering breaks ~15 `DECISIONS.md` cross-references and the in-tree markers). **Rows written at 2.16, BEFORE 3.0's pass, because a row-by-row re-verification structurally cannot surface a MISSING row** — F-8's mechanism in the receiving document. Individually dispositioned below; 3.0 sequences them against the Phase-3 critical path |
| **2.8 submit-charge structural fork + timeouts→params** *(added D-095)* | D-085, D-090 | **Phase-2 tail.** The G2 SECURITY hole is already CLOSED (2.3's `SubmitIntentPrevalidationDecorator`, verified in the ante chain at `app/ante.go:125`); 2.8 owns only the DESIGN question (keep ante-prevalidation, or a v0.50 mode-split charge) plus the timeouts→params promotion, of which it is the **single** owner (Rev 3 resolved the double-ownership). No blocker; opens with its own plan-stop |
| **2.9 receipt condition-outcome integrity** *(added D-095)* | D-087, D-094 | **Phase-2 tail, HIGHEST PRIORITY of the six.** Not exit-blocking — SPEC §10 asks the receipt be *verified* on a second chain (ICS-23 check 1 passes); §6.3 check 3 is a 7683-rail admissibility rule, not the SPEC's criterion. **But it is the hard prerequisite for any external-facing use of the §6 evidence**, and 2.7 made conditioned settlements strictly more common, so the unconditional `NONE` stamp is now wrong more often. Proto + cross-module consensus work; vector re-baseline expected |
| **2.14 reference solver-side condition attachment** *(added D-095)* | D-093, D-094, **D-097** | ✅ **CLOSED.** `solver.Solve` attaches the signed condition and self-checks every candidate against the chain's own `CheckConditionBinding` + `CheckConditionedMatchShape`, so it drops what the chain would reject instead of proposing it. Placement decided at a STOP: **in `solver/`** (an import-graph-preserving boundary was *established* impossible, and cmd/-side would leave `Solve` itself broken). Vector run in isolation TWICE — byte-identical to D-082 both times. Deliverable `docs/edge/SOLVER_SURFACE.md`; its load-bearing limit is §6.1, **no in-tree LIVE path reaches HASHLOCK or OPTIMISTIC**. Spawned rows **2.17** and **2.18** below |
| **2.17 solver bond slashes do not persist on the tx path** *(added D-097)* | D-097 F-1 | **Phase-2 tail — NEW, and the highest-severity item in it.** SPEC §6.4 / D-024 price an invalid solution at 5% and equivocation at 20%; `solution.go` calls `slashSolver` for both **and then returns an error**, so baseapp reverts the failing message's cache context and the slash goes with it. A rejected proposal costs a solver **gas only** — the economic assumption the untrusted-solver model rests on. **Verified on both paths** (`1412`, `1410`) through real txs; green until now because every in-tree assertion calls the keeper on a raw, un-cache-wrapped context (`solution_test.go:53`) — Rule 4 step 5, the D-093 shape. Pinned by two boundary tests that instruct their own replacement. Not exit-blocking; **sequence with or just before 2.8**, which owns the sibling "what survives a failing tx" charging question |
| **2.18 METHODOLOGY amendment backlog** *(added D-097)* | F-14, D-095, D-097 | **Phase-2 tail — NEW, docs-only.** **F-14 IS NOT DISCHARGED**, and this row exists because 3.0 assigned it to *"the session that commits D-096"* — a session reference, not a task number — so it went unowned the moment that session ended. Neither amendment is in `docs/METHODOLOGY.md` (grep: zero hits for both). **This is the F-3 floating-owner failure recurring inside the document that catalogues it**, which is the strongest available evidence for the rule it violates. Carries F-14's (a) Rule-8 enumerate-the-consumers sub-rule and (b) Rule-9 "a FLAG is a CLAIM", plus (c) NEW from 2.14: **a watched-red harness must fail loudly when the sabotage does not COMPILE** — 2.14's harness reported a non-building sabotage as "no failures" and nearly recorded a load-bearing guard as inert |
| **2.15 §6.4 rule-3 per-leg attribution** *(added D-095)* | D-089, D-094 | **Phase-2 tail.** Receives BOTH items 2.7 assessed and explicitly re-deferred: (a) TODO 9 multi-intent-per-sigil (`solution.go:143` — the marker was retargeted here at 2.16, having named the closed 2.7), and (b) the `ErrLimitRateUnsupported` 1428 lift, which needs a **counter-asset designator** — D-094's verdict is that FW-2 does NOT enable it, since a `DeliveryCondition` names no counter-asset. **D-074 is orthogonal, not colliding** (D-094), and must not be re-litigated. The D-044 price source stays 3.4 and must not be reached for |
| **2.13 condition-attachment verdict + wire-code loss** *(added D-095)* | D-093 | ✅ **CLOSED** (`0d7f167`). Recorded here because it had no row at all: (b) session `1412` now reaches the wire via the typed composite `types.InvalidSolution`; (a) the GAP verdict was closed by 2.7 for enforcement and remains open only as 2.14's reference half |

| **G-1 IBC relayer operations** *(added D-096)* | Rev-1 gap analysis | → **3.10** (runbook) + **3.8** (testnet posture). Verified by grep, not asserted: the corpus has **one** relayer mention — the discharged §6b demo. A live IBC rail needs relayer **operations**: process supervision, wallet funding/rotation, stuck-packet clearing, and **light-client expiry** (an expired client is an unrecoverable-by-relayer channel state, and §6b's evidence rests on chain B's client of chain A). Not exit-blocking; it becomes real the moment 3.8 exposes a rail anyone else uses |
| **G-2 gas / DoS pricing of the authored surface** *(added D-096)* | Rev-1 gap analysis | → **3.9** (pricing model) + **3.4** (enforcement) + **3.8** (caps at real n). **RANKED HIGHEST of the four new rows.** A **pairing-bound Reveal runs in BeginBlock paid for by nobody**, and the matcher runs in EndBlock likewise, **on an exchange chain**. O4 caps the *count*; **nothing prices the COST**. The spike supplies the first real number — **5.3 ms/intent** at n=4,t=3, i.e. a 64-intent batch is ~340 ms of pairing work in a 1 s block — so the surface is not hypothetical. The question 3.9 must answer: what does it cost an adversary to fill a session with intents that maximise consensus work and never settle? |
| **G-3 validator key custody / double-sign protection** *(added D-096)* | Rev-1 gap analysis | → **3.10** (custody runbook, alongside T3) + **3.8** (genesis onboarding). **Zero hits** across the corpus for tmkms / horcrux / HSM / slashing-protection-file. Conclave validators hold **two** secrets, not one: the consensus key **and** the DKG share (T3) — so the standard Cosmos custody story is necessary but not sufficient, and the interaction (a validator restored from backup may double-sign **and** hold a stale share) has no owner |
| **G-4 load / capacity testing** *(added D-096)* | Rev-1 gap analysis | → **3.8** (O4 re-derivation at real n) + **3.7** (histograms). **Zero hits**; O4 derives from a **single-node bench at n=4**. **F-9 is live evidence this row is real**, not speculative: the same measurement, run under incidental machine load, moved the recommended `max_intents_per_session` from ~56 to **5** — a 10× swing in a **consensus parameter** with no isolation guard anywhere |
| **F-9 `TestRevealCostWithinBound` diagnoses an unestablished cause AND feeds a consensus param** *(added D-096)* | 3.0 spike | → **3.5** (the test) **and → G-4/3.8** (the parameter). **Two defects, one test, and the second is the serious one.** (a) On failure it prints *"the share gate / IBE open regressed"* — a **specific diagnosis it has not established**; it fired in the spike naming a crypto regression that did not exist. This is the negative-assertion rule applied to a **diagnosis** rather than an error code. (b) Its `recommendedCap` is an unguarded **wall-clock** measurement that **derives an O4 consensus parameter**, and it swung **5 vs ~56** purely on machine load. Fix (a) by making the message state what was measured, not what it means; fix (b) by isolating the measurement (or refusing to emit a cap when the machine is loaded) — **route (b) as G-4 evidence, not as a test cleanup** |
| **F-10 seven of eight `recover()` sites undispositioned** *(added D-096)* | 3.0 obligation 1 | ✅ **CLOSED IN-TASK** — §3.1.2 step 4 returns **8** consensus/crypto panic boundaries; §3.1.1 named **one**. All eight are now enumerated in §3.1.1 with per-site guard/fail-closed/auditor-note. No forward owner needed; the **audit** checks them |
| **F-11 §3.1.1 named the wrong function for the D-089 recover** *(added D-096)* | 3.0 obligation 2 | ✅ **CLOSED IN-TASK** — the guard is in `rateWithinBound` (`solution.go:617`), not `checkLimitRate`. Corrected in place with a marked block. **Recorded rather than silently fixed because it is obligation 2's proof-of-value**: a grep-diff would never have surfaced it, since nothing was *added* — existing prose was simply wrong |
| **F-12 D-088's invariant now has THREE implementations** *(added D-096)* | 3.0 obligation 2 | → **3.5**, as a named sub-item (it is a gate-coverage question: what *test* would catch a divergence?). `storetypes.PrefixEndBytes` + `x/veil/keeper.prefixEnd` + `x/session/types.PrefixEnd`, all currently byte-equivalent. **Latent, not live.** Decide: collapse to the SDK's, or keep the copies and add a test asserting all three agree on the adversarial inputs (all-`0xFF`, empty, single-byte) |
| **F-13 commit-hash traceability recurs BY CONSTRUCTION** *(added D-096)* | 3.0 obligation 6 | → **3.3a** (ship it with the CI-guard work). 2.16 restored hash traceability by hand; `6d6b730` was left as a deliberate calibration seed; 3.0's pass found it **and** found **`15c2470` uncited**. The gap **relocates to the newest commit every time**, so hand-fixing is a treadmill. Deliverable: the §3.1.2 block as a committed `scripts/` artifact (plan-stop decision 2, decided YES) **plus one new check obligation 6 proved is needed — enumerate from `git log` and assert every commit is cited somewhere.** The check must tolerate exactly the HEAD-can't-cite-itself case and no other |

| **F-14 two METHODOLOGY amendments owed** *(added D-096)* | 3.0 obligation 5 | → **the session that commits D-096**, as its own edit to `docs/METHODOLOGY.md`. (1) Rule 8 step 3 gains the D-095 **enumerate-the-consumers-first** sub-rule with its mechanical form **verbatim** — and **written to BOTH trigger classes** (*strengthened a guarantee* **and** *renamed or split a referent*), because Rev 1 fired it on a structural split D-095's wording does not cover, and writing it narrowly would leave that case unowned (§8.1); (2) Rule 9 gains the companion **"a FLAG is a CLAIM"** rule. **Deliberately NOT made inside this Rev** — METHODOLOGY is a separate authority document, and amending it from inside a plan Rev is exactly the silent cross-document edit Rule 8 step 3 exists to prevent. Written here so the item is owned by a row, not by the memory of whoever writes the commit |

### 5.2 Advisories to the remaining Phase-2 tasks (research fallout; not Phase-3 scope)

> **ALL THREE ADVISORIES ARE SPENT (task 2.16, D-095).** Every task they address
> has closed: 2.3 (`8756575`), 2.4 (`c830e55`), 2.5 (`dccd607`). Each was
> consumed by its target — 2.5's plan-stop took the v8-EOL advisory and produced
> D-081's recorded POSTURE, and the v0.53 checkpoint was re-run and CLOSED.
> Retained as the research record, not as live instructions to anyone.

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

### 5.3 Where the Phase-2 tail tasks sequence (3.0 plan-stop decision 3, D-096; updated D-097)

~~The tail is **2.8, 2.9, 2.10, 2.12, 2.14, 2.15**~~ — **updated at task 2.14
(D-097): 2.14 is CLOSED and produced two new rows, so the tail is 2.8, 2.9,
2.10, 2.12, 2.15, 2.17, 2.18** — none exit-blocking (2.16 re-derived that from
SPEC §10's text, and the same test applied to 2.17/2.18 confirms it: SPEC §10's
Phase-2 paragraph can be truthfully asserted without either). Rev 1 places them
against the Phase-3 critical path. **One uniform rule, and the ordering is
derived from two properties only: does it fix a LIVE defect, and does it gate
anything.**

> **The rule reproduced itself.** 2.14 was sequenced first because it was the
> only tail item with a live functional defect; executing it surfaced **2.17**,
> a live defect of the same kind one layer down, which now inherits the slot.
> That is the ordering rule working, not drifting — a tail is finished when
> executing its head stops producing new heads.

| Order | Task | Placement | Why |
|---|---|---|---|
| ~~**1**~~ | ~~**2.14**~~ | ✅ **DONE** (D-097) | ~~The **only** tail item with a **live functional defect**~~ — closed. It ran ahead of 3.3a as planned and the reasoning held. **It also invalidated its own parenthetical:** the "slashable (5%)" price does not exist, because the slash is reverted with the failing tx — that is the new **2.17**, which inherits this row's "fix a live defect first" argument |
| **1′** | **2.17** | **ahead of 3.3a**, with or just before **2.8** | Inherits 2.14's slot. The SPEC §6.4 bond slashes never land on the msg-server path, so the untrusted-solver model's economics are currently absent. Platform-independent. **2.8 is the natural sibling** — it already owns the v0.50 "what survives a failing tx" charging question (D-085), and doing them apart risks two answers to one problem. **2.18** (docs-only) can land alongside either |
| **2** | **3.3a** | next | The security-patch vehicle (see §3.3's Rev-1 decision block) |
| **3** | **2.9** | after 3.3a, before any external use of §6 evidence | Highest-priority of the remaining five and a **hard prerequisite** for external-facing use of the exit-demo evidence; 2.7 made conditioned settlements strictly more common, so the unconditional `NONE` stamp is wrong more often. Proto + cross-module consensus work ⇒ vector re-baseline expected ⇒ **do it before 3.3b re-baselines the vector anyway**, so the two re-baselines stay attributable (Rule 7 step 2) |
| **4** | **2.8, 2.15, 2.10** | after **3.3b** | All three are consensus-surface work that would be **built twice** if done against the old platform — the exact reason 3.4/3.5 sequence after 3.3b. 2.8 is a design question with no blocker; 2.15 and 2.10 are matcher/attribution surface whose primary gate is hand-built fixtures either way |
| **5** | **2.12** | after **3.9** | Unchanged and re-affirmed: **"sequenced after 3.9 BY DECISION", never "blocked on D6"** — no document creates that gate, and recording a dependency no document creates is the floating phrase that produced audit finding F-3 |

**Note on 2.9 vs 3.3b ordering (the non-obvious one).** Both re-baseline the
determinism vector. Rule 7 step 2 says sequence risky changes so the gate runs
BETWEEN causes. Doing 2.9 first means one re-baseline attributable to a **proto
change** and a later one attributable to a **platform change**; doing them in the
other order, or together, would fuse two causes into one unexplainable hash
movement.

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
   reveal path, **and each of the other four Category-1 shadows has a recorded
   per-shadow verdict** — "the gate reaches it, watched" or "these named
   hand-built fixtures are the permanent primary gate" (§3.5, widened at Rev 1).
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

---

## 8. Rulings — the D-095 methodology amendment and the re-evaluation amendment set

**Every row is ruled on. A row I decline is ruled on with reasoning, not
dropped** — 3.0's obligation was to rule, not to accept.

### 8.1 Ruling A1 — the D-095 candidate METHODOLOGY amendment: **ADOPTED**

**The amendment.** METHODOLOGY Rule 8 step 3 ("propagate the correction to any
document that repeated it") needs the sub-rule Rule 6 already learned:
**enumerate the consumers FIRST**, because a correction propagated from memory
reaches exactly the documents you remember. Task 2.7 corrected four documents and
left five wrong, including the auditor-facing scope. That is not a diligence
failure; it is the **absence of an enumeration step**.

**The standard it was to be ruled by:** *a rule that depends on REMEMBERING does
not fire; a rule with a RUNNABLE STEP does.*

**Ruling: ADOPTED, and it passes the standard cleanly** — the amendment is not
advice ("be thorough about propagation"), it is a command someone runs at a
boundary. Its mechanical form is carried **verbatim**, as required:

> After a task that STRENGTHENS a guarantee, grep for assertions of the OLD
> guarantee's **NEGATION** — `"not enforced"`, `"never attaches"`, `"has no X
> field"`, `"only through"`, `"is a … ceiling"` — **NOT for the new mechanism's
> name.** Stale text by definition does not mention the new thing, so searching
> for `condition_binding.go` or `attachFallbackCondition` finds exactly the
> documents already corrected. The negation is what the stale text actually says.

**Rev-1 adds a third independent data point, and it is the strongest of the
three.** The two recorded were 2.16's own sweep (found a 20th stale statement a
manual pass missed) and Rule 6 sub-rule 3's named worst case recurring one task
after the rule was written. The third: **FLAG P3-B's refutation came from running
`govulncheck` and reading four `go.mod` files** — not from research, and not from
anyone re-reading the flag. The flag had been carried and cited for three weeks
without once being tested against the artifact. So the amendment generalises past
correction-propagation into a companion rule, which Rev 1 also adopts:

> **A FLAG is a CLAIM, and flags accumulate authority precisely because nobody
> re-checks them.** At every phase boundary, list the document's FLAGS and ask of
> each: *what artifact would falsify this, and has anyone run it?* A flag with no
> falsifying test named beside it is prose. **Flags asserting a CONSTRAINT ("X
> blocks Y", "we cannot do Z") get priority** — they delete options, and their
> cost is invisible because nobody plans the work a constraint forbade.

**THE TRIGGER CLASS IS WIDER THAN D-095 STATED, and Rev 1 proves it on itself.**
D-095 wrote the amendment for a **semantic strengthening**. Rev 1's own case was a
**structural rename/split** — `3.3` became `3.3a`/`3.3b` — and the same mechanism
caught **nine** stale referents that a search for the new names could not have
reached. Generalised, and this is the form F-14 must carry:

> **Any change that RENAMES or SPLITS a referent leaves the old referent behind in
> every document that pointed at it, and the intuitive search is systematically
> ANTI-CORRELATED with finding them.** Searching for the NEW name returns exactly
> the sites already updated — **by construction**, since stale text cannot mention
> a name that did not exist when it was written. Search for **what the stale text
> still says**: the old name, or the old guarantee's negation. **Two triggers
> (strengthened a guarantee · renamed or split a referent), one mechanism.**

**Both rules go to METHODOLOGY as a Rule 8 amendment + a Rule 9 companion — and
the Rule 8 amendment must be written to BOTH triggers, not just D-095's.** That
edit is **NOT made in this Rev**: METHODOLOGY is a separate authority document,
and amending it from inside a plan Rev would be exactly the silent cross-document
edit Rule 8 step 3 is about. It is **F-14**, owned by the session that commits
D-096.

### 8.2 The re-evaluation amendment set — row by row

| # | Proposed | Ruling |
|---|---|---|
| **1** | FLAG P3-B is wrong in the expensive direction; correct both halves | **ENACTED** — §1 FLAG P3-B rewritten, refuted half kept-and-marked with a forward pointer (D-095's load-bearingness rule: someone could re-derive a *decision* from it) |
| **2** | 16 reachable advisories make 3.3 the security-patch vehicle; its priority is a DECISION | **ENACTED, and taken further than proposed** — 3.3 **splits**; 3.3a is pulled ahead of the tail (§3.3, §5.3). The re-evaluation asked for an explicit decision; the spike's decoupling made a *better* option available than the one on offer |
| **3** | CometBFT v0.38.23→.24/.25 is free today; must not wait on the family decision | **ENACTED** — STOP #1 "item 0", assigned to **3.3a**. Version corrected to **v0.38.25** (the re-evaluation had .24); floor `go 1.22.11` verified at the proxy |
| **4** | G-2 ranks highest of the four new gaps | **ENACTED** — §5.1 G-2, with the spike's 5.3 ms/intent as the first real cost datum |
| **5** | O1 reframed via CHK/BCHK | **ENACTED WITH A HARD CAUTION** — §3.1, framed as the auditor's **starting point** phrased as a task. The caution is the substance: an auditor handed *"we have established this is CHK/BCHK"* may let the premise do all the work |
| **6** | PKC-2026 non-monotone corruption lands on §7's "< t" | **ENACTED** — §3.1 flags `AUDIT_SCOPE §7`'s sentence for explicit re-examination as a named sub-question. **Flagged, not withdrawn**: no counterexample against *our* construction is known |
| **7** | Four new gap rows (G-1…G-4) | **ENACTED** — all four in §5.1 with dispositions |
| **8** | 3.5's acceptance is too narrow — five Category-1 shadows, not one | **ENACTED** — §3.5 widened to a per-shadow verdict table |
| **9** | `recover()` census returns 8; §3.1.1 names one | **ENACTED** — all eight in §3.1.1, per-site. **Independently re-derived at 3.0**, not copied |
| **10** | §3.1.2's grep block was dry-run and works (3 hits, all allowlisted; markers 15/15) | **CONFIRMED, with two additions the dry-run did not record** — the 17-vs-15 raw/tagged delta is explained as a **known non-finding**, and the step-6 allowlist **self-references** (five occurrences of which one is the allowlist entry = clean) |
| **11** | Aptos's encrypted mempool is announced, not shipped | **ENACTED as a correction to §2.2 R3** — it weakens option (b)'s "well-trodden" support, which is a 3.1 STOP #1 input |
| **12** | 2026.1 moved to SDK 0.55.x / CometBFT 0.40.x / ibc-go v11 in 23 days | **ENACTED as a DISCIPLINE, not a version** — §3.3 STOP #1 now says derive the option table **at task time, never from this prose**. Recording the moving numbers here would reproduce the very failure |
| **13** | iavl pinned v1.2.4 vs v1.2.9; 5 patches behind on pruning | **ACCEPTED, assigned to 3.3a** — state-commitment code ⇒ **vector-in-isolation** event (Rule 7 step 2). Not folded into the family jump |
| **14** | x402 moved to Linux Foundation; V2 renamed headers | **NOTED, no action** — it *strengthens* D-083's version-independence claim (the churn is HTTP-layer). 2.3 is closed and the §5.2 advisories are spent; recording it as live scope would re-open a closed task |
| **15** | The toolchain spike runs FIRST, before any other 3.0 work | **ENACTED AND VINDICATED** — §2.3. Every sequencing decision in this Rev depends on its result, and the plan written before it would have been the plan it invalidated |

**One row of the amendment set is REJECTED as stated.** The handoff's spike
hypothesis — *"the real toolchain constraint is more likely the archived
block-sdk v2.1.5 than kyber"* — is **REFUTED** (§2.3). It is recorded here rather
than quietly dropped, and held to the same standard as FLAG P3-B: it was
correct to flag it as **unverified** and correct to make testing it the spike's
job. Authorship is not evidence in either direction.

---


## 9. Rev-1 provenance and close-out

**Proposed** at `9722912` (2026-07-10), mid-Phase-2. **Rev 1** authored at task
3.0 against `15c2470` (2026-08-03, D-096), after Phase 2 was declared EXITED
(2.16, D-095) — both gate conditions met, so this document is now **the phase
authority**.

**Gate evidence (Rule 9's two-fingerprint standard).** The full suite and the
determinism vector were run on `main` at `15c2470` with a **clean tree**:
`make lint` **0** · `make test` **0** (17/17) · vector **PASS**, 5 seeds
byte-identical to the D-082 baseline across two independent processes. Tree
fingerprint at the gate: `git diff | sha256sum` = **`e3b0c44…b855`** (empty).
**Rev 1 is docs-only**, so the close-out proof is the SOURCE-ONLY fingerprint of
the shipping tree — it must still equal `e3b0c44…b855`, which shows not one
source or test byte moved after the gate. That is a proof rather than a promise,
and it is printed in the task record rather than asserted here.

**What Rev 1 did NOT do, stated so nobody infers it.** It changed **no pin and no
source byte**. It did not amend `docs/METHODOLOGY.md` (that is **F-14**), did not
build the `scripts/` re-derivation artifact (that is **F-13**), and did not open
any 3.x task. The toolchain spike's branch was destroyed; its deliverable was the
**result**, not code.

**The one sentence worth carrying forward.** The spike's finding was not that the
platform is fine — 16 reachable advisories say otherwise, and 3.3a is now urgent.
It was that **the platform was never trapped**, and that a constraint nobody
tested had been quietly deleting options from every plan that inherited it for
three weeks. *The conclave prepares to convene in public.*
