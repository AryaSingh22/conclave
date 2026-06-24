# CLAUDE.md — Conclave repo conventions

**Single source of truth: [docs/SPEC.md](docs/SPEC.md)** (Conclave Architecture &
Build Specification v1.0). If code and spec conflict, fix the code or propose a
spec PR — never silently diverge. If the spec seems wrong or ambiguous, stop
and ask, citing the section.

**Decisions log: [docs/DECISIONS.md](docs/DECISIONS.md).** Every field-level or
behavioral decision the SPEC doesn't pin down gets an ADR-style entry there —
appended in the same task that makes it.

## Naming canon (SPEC §0 — use everywhere: code, docs, protos, CLI)

| Concept | Canonical name | Code identifier | NEVER use |
|---|---|---|---|
| The chain | Conclave | `conclave` | BlockXNet, x402 chain |
| Native token | CLV | `uclv` (10^6 uclv = 1 CLV) | x402 token |
| Agent identity | Sigil | `x/sigil` | xDID |
| Agent policy accounts | Mandate | `x/mandate` | — |
| Encrypted intent mempool | The Veil | `x/veil` | Dark Intent Pool, darkpool |
| Pre-trade order object | Sealed Intent | `SealedIntent` | Intent Packet |
| Batch clearing engine | Session | `x/session` | x/clearing, batch auction module |
| N-party atomic settlement | CONCLUDE | `MsgConclude` | SWAP_ATOMIC, SETTLE_GRAPH |
| Delivery escrow | Escrow | `x/escrow` | — |
| Settlement receipts | Receipt | `x/receipt` (query side) | — |
| Contract runtime | CVM | `cvm` | xVM |
| Industry shards (P3) | Chambers | `chamber` | Zone Shards |
| Chain ID (dev) | `conclave-dev-1` | — | blockx_19077-1 |
| Bech32 prefix | `conc` | `conc1...`, `concvaloper1...` | — |

Threshold decryption at session open is called **the Reveal**.

## Conventions (SPEC §13)

- **Error codespaces** in `x/{module}/types/errors.go`, stable per module:
  `sigil`=11xx · `mandate`=12xx · `veil`=13xx · `session`=14xx · `escrow`=15xx
- **Every exported keeper method** gets a doc comment citing the SPEC section
  it implements, e.g. `// implements SPEC §6.2 step 5`.
- **All TODOs** as `// CONCLAVE-TODO(phase-N): ...` — greppable. Stub exactly
  where SPEC says STUB, never beyond.
- One SPEC §10 task per working session.

## Determinism rules (SPEC §11.6 — hard requirements, release blockers)

- No map iteration into state writes (sort keys first).
- No wall-clock time in consensus code — use `ctx.BlockTime()` / `ctx.BlockHeight()`.
- No floats — `sdk.Dec` (`math.LegacyDec`) and `math.Int` only.
- The chain must never panic on user input: every Reveal/unmarshal path wraps
  recover → INVALID.
- `.golangci.yml` enforces `time.Now` / `math/rand` bans outside tests and cmd.

## Working method

1. One SPEC §10 task per session, in order. Do not start the next task.
   One task per session — each task starts in a fresh session that
   bootstraps from CLAUDE.md, docs/SPEC.md, and docs/DECISIONS.md.
2. Before declaring any task complete: `make lint test` must be green; show
   the output. `scripts/demo_e2e.sh` before closing any Phase-0 milestone.
3. Stop for review after each task.
4. Repo is canonical at `~/conclave` in WSL. After each completed task run
   `scripts/dev-mirror-sync.sh` to refresh the read-only Windows mirror at
   `C:\Pro\Conclave Protocol\conclave`.

## Key invariants to protect (SPEC §1.1, §6)

- Mandate enforcement happens in the **ante handler before execution**, with
  optimistic window charging (SPEC §6.2, all 6 steps incl. nested-msg unwrap).
- The **order-then-reveal** invariant in x/veil is real even in stub mode (§6.3).
- CONCLUDE is **all-or-nothing** via a single cache-wrapped context (§6.4).
- The fallback matcher is deterministic consensus logic — every node computes
  it identically.

## Phase-0 task ledger (SPEC §10)

One task per session, in order. Mark complete only after `make lint test` is
green and the task is reviewed + committed.

- [x] 0.1 Scaffold chain + `scripts/localnet.sh`
- [x] 0.2 Protos for all six modules; buf generate
- [x] 0.3 `x/sigil` full + reputation
- [x] 0.4 `x/mandate` full incl. MandateDecorator + FeeAbstractionDecorator
- [x] 0.5 Unordered txs + dedup cache + conformance suite
- [x] 0.6 Block SDK lanes wired
- [x] 0.7 `x/veil` stub mode
- [x] 0.8 `x/session`: lifecycle, solver registry, validation, scoring, Conclude, fallback, sweeps
- [x] 0.9 `x/receipt` query/proof surface + reputation hook (verified ICS-23 proof)
- [x] 0.10 Reference solver binary (naive + cycles)
- [x] 0.11 SDK simulation integration (all modules) — 500-block run, no invariant
      break, same-seed determinism; surfaced + fixed the D-059 window-accounting
      conflation bug (lifetime `outstanding_commitment` vs per-period
      `window_spent`)

**Phase 0 build COMPLETE.** Next is the independent Phase-0 audit (not a build
task): a fresh-context auditor re-runs every acceptance script, re-derives the
conservation/accounting invariants from scratch (incl. the D-059 lifetime/
per-period split), sweeps the naming canon, and reads adversarially.

**Phase-0 audit COMPLETE** (2026-06-15, SOUND WITH FINDINGS; F-1 → Task 1.0).

## Phase-1 task ledger (docs/PHASE1_DESIGN.md §5)

One task per session, in order. Mark complete only after `make lint test` is
green and the task is reviewed + committed.

- [x] 1.0 Determinism harness hardening (F-1) — `a496e7e`, D-063; the hardened
      gate immediately surfaced a real mandate-commitment bug at seed 7
- [x] 1.0b Mandate-commitment-conservation fix — `e0f74ee`, D-064; the seed-7
      break was a root-key-submit → FRESH-mandate adoption (NOT D-063's
      revoke/reissue guess); FRESH issuance adopts live root-era exposure so the
      §1.1-goal-3 bound attaches from issuance. The gate (1.0 hardening + 1.0b
      first-catch) is now trustworthy end-to-end.
- [x] 1.1 `spend_commitment` (§4), stub mode — D-065. Envelope field; ante binds
      `spend_commitment+bond` → `outstanding_commitment` (D-059) UNIFORMLY (never
      reads give legs, stub or encrypted — the property that lets stub testing
      prove the shipping path); bond magnitude now commitment-derived; Reveal
      slashes a commitment violation (actual give > commitment in any denom) →
      INVALID (50%); empty=zero, Reveal-slash is the sole enforcement (no
      ErrMissingSpendCommitment); `Encrypt` still identity. Gate byte-identical.
- [x] 1.2 D8 ratification (docs-only; D-060) — SPEC §14 D8 → RESOLVED
      (stay-on-v0.50 through Phase 1); fork-site CONCLAVE-TODO re-pointed
      phase-1 → phase-2 (retirement deferred to the wasmd-2.2 boundary). D-060
      already complete from planning; no source/proto/logic touched.
- [x] 1.3 BLS library wrap behind `ThresholdScheme` + DKG interface revision
      (offline) — D-066. `crypto/threshold`: the published drand **tlock**
      construction (IBE-over-threshold-BLS, hybrid KEM/DEM) on `drand/kyber`
      v1.3.1 + `kyber-bls12381` v0.3.1 (pinned pre-v0.3.4 — NO go-1.24 toolchain
      bump, `golang.org/x/*` unchanged; tlock root + age/drand-v2 avoided by
      importing `encrypt/ibe` directly; DEM = stdlib AES-256-GCM). Authored
      surface = KEM/DEM wrapper + `id = H(DST‖C_dem)` derivation + the
      `ThresholdScheme` adapter + share-gating; everything else is library. Per-
      share proof = the INTRINSIC pairing check (`tbls.VerifyPartial`), no NIZK.
      Two barriers: gate = liveness/robustness, IBE FO check = integrity (corrupt
      share RED-without-gate, proven). `EpochSetup` REMOVED from the consuming
      interface (D-062) → multi-round `DKGParticipant`, shares node-local.
      Round-trip t-of-n, t-1-cannot/t-can (info-theoretic), deterministic Combine.
      StubScheme stays the live scheme; no consensus wiring. `make lint test` +
      determinism vector green. 1.8 audit obligations recorded in D-066.
- [x] 1.4 Adversarial test harness (offline) — D-067. `crypto/threshold/adversarial_test.go`
      (test-only; `threshold.go` byte-identical to HEAD): 7 property tests + a kyber
      selection-rule probe. PROVES (vs 1.3's happy-path asserts) under an adversary —
      t-1 real-secret-share COALITION cannot recover (`ErrInsufficientShares` "not yet",
      info-theoretic at the raw primitive), t can; the gate's LIVENESS/ROBUSTNESS (not
      silent-corruption — D-066): f∈{1,n-t} forged shares that DESERIALIZE-but-fail-the-
      pairing-check cannot force `ErrInsufficientShares` when ≥t honest exist (load-bearing,
      RED-without-gate proven), and a forged share cannot fake progress to t (retry
      semantic); WITHHELD recovers from any honest t-subset (scattered-middle). CHURN:
      drain-before-rotate keeps an epoch-e in-flight ct decryptable under e's retained
      shares across a FULL (independent) rotation while e+1's shares cannot (cross-epoch
      isolation); n-t leavers tolerated / n-t+1 not; CANCEL-as-drain-terminus crypto
      precondition (terminal, deterministic, PURE signal) proven. `TestKyberRecoverCommit
      SelectsLowestIndex` pins the lowest-t-by-index premise so a silent kyber bump fails
      by name, not vacuously. Three seams carried to 1.5 (D-067): (a) CANCEL-releases-
      rotation session-state, (b) grace-window mechanism, (c) partial-overlap resharing.
      `make lint test` + determinism vector green; StubScheme stays live; no consensus wiring.
- [x] 1.5 DKG epoch lifecycle — D-068. New `x/epoch` module (codespace 16xx).
      STOP #1 approved transport = **on-chain txs** (Candidate A: chain is a
      deterministic relay of OPAQUE deal/response bundle bytes; only validators
      run the DKG off-chain; finalize = count ≥t byte-identical key confirmations
      — VE rejected as pulling 1.6 forward, side-channel as 1.7 infra). Boundary =
      height-based (`height % epoch_length`) + staking-snapshot committee (epoch
      EndBlocker AFTER staking — load-bearing); EpochPubKey+commits published to
      queryable state, secret shares node-local. Status FORMING→ACTIVE→DRAINING→
      RETIRED | FAILED. Drain-before-rotate REAL: successor ACTIVE immediately,
      prior epoch DRAINS (retained) until no in-flight `epoch_id`-tagged ct, then
      RETIRES — seam (a) CANCEL/OPEN releases it; seam (c) partial-overlap isolates
      by epoch id not validor identity (both tested). New `IntentEnvelope.epoch_id`
      + submit-time BOTH-edge gate (reject RETIRED past + not-yet-published future;
      0=stub). Failed-DKG degrades (no rotation, prior key stays, chain never
      halts). Justification round + epoch-0 bootstrap = recorded forward seams to
      1.6 (single faulty dealer can deny rotation = bounded liveness-grief). Bundle
      serde = `crypto/threshold/transport.go` (off-chain). `make lint test` +
      determinism vector green; `StubScheme` stays live; no Reveal/VE wiring.
- [x] 1.6 Vote-extension share transport + Reveal wiring — D-069. The F-4
      convergence: the LIVE Reveal now uses real threshold decryption and
      order-then-reveal rests on **vote-extension share availability**, not
      state-gating (AUDIT F-4 CLOSED). `ExtendVote(h)` enumerates the committed
      `SEALED_IN_SESSION` set (epoch-tagged, deterministic 0x02 index), emits
      `PartialReveal` shares; the proposer INJECTS the prior block's
      `ExtendedCommitInfo` at `Txs[0]`, `ProcessProposal` re-validates
      (`ValidateVoteExtensions`) + strips, `PreBlocker` collects, `BeginBlock`
      Combines ≥t → OPEN. TWO STOPS (plan; transport-in-isolation before cutover).
      Injected-`Txs[0]` unforgeability proven on 4 legs (position+height-gated,
      never content-sniffed). **ABCI++ off-by-one found+acted on:** `ExtendVote(h)`
      runs before `FinalizeBlock(h)` and reads committed `h-1`, so reveal is **B+2**,
      not B+1 (strengthens F-4); the retry counter floors at B+2. Cutover keyed
      per-intent on `epoch_id` (0=StubScheme bootstrap window; ≠0=real
      `threshold.NewSchemeFromCommitBytes` + VE shares); revealed plaintext persisted
      into `Ciphertext` at OPEN. Retry→CANCEL per-session (bonds refunded, releases
      rotation, chain never halts). `max_intents_per_session` cap-at-seal (0x02 order)
      + `max_reveal_retries`. **Epoch-0 bootstrap = first-DKG-then-live + an HONEST
      plaintext-stub window** (the Veil guarantee does NOT hold genesis→first-ACTIVE-
      epoch; operators must not route sensitive flow before epoch 1). Justification-
      grief RE-DEFERRED to 1.7 (needs the DKG daemon + faulty-node localnet; bounded/
      survivable today). Determinism vector GREEN with the cutover live (sim is
      epoch-inert → real path not exercised; real-path determinism proven by a
      cross-keeper test). Node-local `ShareProvider` shipped (interface + in-memory +
      harness); the live DKG daemon is 1.7. `make lint test` green.
- [x] 1.7 Multi-node adversarial localnet (4 validators) — D-070. The off-chain
      **DKG daemon** (`app/dkgdaemon`, in-process via `start --dkg-daemon` PostSetup,
      D1) is the missing client 1.5/1.6 stubbed: it watches FORMING epochs, runs the
      kyber rounds, submits the register/deal/response/justify/confirm txs, and
      PERSISTS the node-local secret share to a file store (`app/dkgshare`,
      `<home>/data/dkg_shares`, 0600 — never consensus state / gossip, D-062). **A2
      (STOP approved):** an on-chain DKG-key **registration round** (`MsgRegisterDKGKey`,
      a 5th window before deal) binds each long-term G2 key to its validator via the
      same account↔valoper check the bundle rounds use (protocol-enforced, not
      genesis-trust); the COMMITTEE is the REGISTERED set (a non-registrant is excluded,
      no single-point veto), finalized at the register-window close with `t=floor(2m/3)+1`,
      FAILED if m<min_committee_size. **B2 (STOP approved): justification round CLOSED** —
      the pre-1.7 Finalize collapsed on ANY complaint, so one faulty dealer in the FIRST
      committee vetoed the whole confidentiality guarantee (epoch 1 never activates →
      Veil stays plaintext). A `justify` window + `MsgSubmitJustification` excludes a
      faulty dealer; n/threshold unchanged (all holders keep valid shares of the QUAL-set
      secret), kyber aborts if qualified dealers<t (the floor), the existing ≥t-confirm
      tally publishes the QUAL-set key unchanged. **Epoch proto unchanged (no new fields)**;
      Params += register/justify windows. **3 build conditions proven** (faulty-excluded-
      completes, register-binding-rejects-non-committee, FAILED-below-floor) at crypto AND
      chain level. **LIVE 4-validator localnet (`scripts/localnet4.sh`, scripted):** real
      DKG → epoch 1 ACTIVE; **app-hash agreement across all 4 nodes** (closes the 1.5
      sim-boundary gap); encrypted intent → B+2 reveal via real VE shares → CLEARED;
      **F-4** (a modified validator with its own share still can't decrypt:
      ErrInsufficientShares); **faulty-dealer no-halt** (justification excludes it, chain
      lives). **Reveal-cost benchmark:** ≈12ms/intent (n=4), recommended
      `max_intents_per_session`≈25 — the default 0 is safe only on the epoch-0 stub path.
      `make lint test` + determinism vector green. **Phase-1 BUILD complete.**
- [x] 1.8 External audit engagement — SCOPE PACKAGE delivered (`docs/AUDIT_SCOPE_1.8.md`,
      D-071). Not a code task: the deliverable is the auditor-facing scope document
      (authored-vs-adopted boundary, the open obligations O1–O4 collected from
      D-066–070, the security arguments to verify/refute, the evidence map, the
      limitations + trust assumptions). "1.8 complete" means the SCOPE is delivered —
      the external cryptographic audit itself is the mainnet gate and has NOT been
      run/passed. `make lint test` green (no source touched); all 44 cited test refs +
      code-location citations verified to resolve in-tree at `5f5feb8`.

**Phase-1 BUILD + SCOPE COMPLETE.** The external cryptographic audit is the
**mainnet** gate (owned externally) and does **not** block Phase-2 build work.

## Phase-2 task ledger (docs/PHASE2_DESIGN.md §2)

One task per session, in order. Mark complete only after `make lint test` is
green and the task is reviewed + committed. **`docs/PHASE2_DESIGN.md` is the
approved phase authority** (D-072) — each task below bootstraps from it.

- [x] 2.0 Phase-2 design ledger authored — `docs/PHASE2_DESIGN.md` (D-072). Docs-only
      (no source/proto/logic touched; `make lint test` green at `39cb207`). Per-task
      sections 2.1–2.5 + numbered task 2.6 (matcher-richness hardening pass owning the
      five latent matcher TODOs); all ten `CONCLAVE-TODO(phase-2)` markers
      dispositioned; v0.53 held as decide-when-reached (2.2-owned), edges scoped
      separate-repo. The completeness-gate review passed before prose.
- [ ] 2.1 `x/escrow` delivery conditions — three condition classes as §11.2 transition
      tables (HASHLOCK / OPTIMISTIC / ZK_DELIVERY); `conclude.go:152` record-ownership
      handoff; `PenalizeLostDispute` reputation-hook wiring; D5 ratification. Recommended
      split: **2.1a (HASHLOCK+OPTIMISTIC)** = next session; **2.1b (ZK_DELIVERY/gnark)**
      = own session, gnark-Go-floor spike with a STOP branch vs the kyber pin (FLAG A).
- [ ] 2.2 wasmd + CVM custom bindings (CONCLUDE + escrow) — **owns the D-060 v0.53
      re-evaluation** (spike-then-decide against then-current block-sdk/ibc-go/wasmd;
      do NOT pre-bake the outcome).
- [ ] 2.3 x402 gateway — **SEPARATE REPO** (`conclave-x402-gateway`); in-repo deliverable
      = the node-side gRPC surface contract.
- [ ] 2.4 7683 resolver — **SEPARATE REPO** (`conclave-7683-resolver`); in-repo deliverable
      = the node-side gRPC + receipt-proof surface contract.
- [ ] 2.5 Real USDC via IBC + session-TWAP fee (swaps the `window.go:138` fee seam;
      shares the IBC-line question with 2.2).
- [ ] 2.6 Matcher-richness hardening pass — owns latent TODOs: partial-fill/cycle-aware
      fallback, partial-fill cycles + alt numeraire anchoring, §5.1 limit-rate honoring,
      multi-intent-per-sigil, multi-asset in-kind skim. One determinism-focused session
      behind the existing `validateSolution` bar + determinism vector.

**Phase-2 exit:** the §1.3 demo extended — external x402 agent buys ZK-verified
data from a Conclave agent; receipt IBC-verified on a second chain (spans
2.1+2.2+2.3+2.5; 2.4 is the parallel inbound rail, off the exit path).
