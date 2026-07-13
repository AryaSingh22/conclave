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
approved phase authority** (D-072; **Rev 2** as-built reconciliation, D-078 —
scoreboard, resolved FLAGs A/B, 2.2 resume checklist, new task 2.7) — each
task below bootstraps from it.

- [x] 2.0 Phase-2 design ledger authored — `docs/PHASE2_DESIGN.md` (D-072). Docs-only
      (no source/proto/logic touched; `make lint test` green at `39cb207`). Per-task
      sections 2.1–2.5 + numbered task 2.6 (matcher-richness hardening pass owning the
      five latent matcher TODOs); all ten `CONCLAVE-TODO(phase-2)` markers
      dispositioned; v0.53 held as decide-when-reached (2.2-owned), edges scoped
      separate-repo. The completeness-gate review passed before prose.
- [x] 2.1 `x/escrow` delivery conditions — three condition classes as §11.2 transition
      tables (HASHLOCK / OPTIMISTIC / ZK_DELIVERY); `conclude.go:152` record-ownership
      handoff; `PenalizeLostDispute` reputation-hook wiring; D5 ratification. Split into
      **2.1a (HASHLOCK+OPTIMISTIC, D-073/074/075)** and **2.1b (ZK_DELIVERY/gnark, D-076)**;
      both complete.
  - [x] 2.1a HASHLOCK + OPTIMISTIC — D-073/D-074/D-075. `x/escrow` stood up (keeper,
        AppModule, msg server, query, genesis, EndBlock sweep; codespace 15xx). Single
        authoritative transition gate (`nextState`) → §11-rule-2 exhaustive transition-table
        test; every illegal transition a load-bearing REJECT. Funding = payment-into-escrow
        (buyer pays in, seller paid on release / buyer refunded on timeout/lost-dispute);
        OPTIMISTIC seller bond (10%, returned on honest delivery, slashed-to-buyer +
        `PenalizeLostDispute` on lost dispute). OPTIMISTIC dispute = seller-reveal of the
        BUYER-AGREED `DataGrant.ContentHash` (rule-3-enforced, not seller-supplied); narrowed
        scope recorded (D-073). **CONSENT (D-075):** a condition is solver-attached
        (`IntentLeg` has no condition field), so it is bound to the seller's SIGNED
        `IntentPayload.condition` at both solution-validation and the settlement boundary
        (`ErrConditionNotConsented`, fail-closed) — an untrusted solver cannot impose a
        delivery obligation/bond the seller never signed. `conclude.go:152` handoff closed
        (writer-going-forward; x/receipt unaffected — never read escrows). Bilateral-pairing
        constraint recorded (D-074). Deterministic height-keyed sweep, ordered after
        x/session. NO ZK/gnark. `make lint test` + determinism vector green.
  - [x] 2.1b ZK_DELIVERY/gnark — D-076. **Spike outcome (a):** gnark works on the CURRENT
        toolchain (no Go bump, kyber pin intact) by PINNING `gnark v0.13.0` + `gnark-crypto
        v0.18.0` (floor go 1.23.0 = the directive); latest v0.15.0 needs go 1.25.7 and
        v0.14.0 needs go 1.24.0 (the bump D-066 avoided), so v0.13.0 is the highest line ≤
        1.23. Determinism vector re-run AFTER the dep add = byte-identical to baseline (the
        threshold/Veil path unperturbed). New `crypto/zkdelivery`: a BN254 groth16
        `DeliveryCircuit` (MiMC) proving `MiMC(payload)==schema_commitment` bound to the
        escrow id; **vk_id registry = COMPILED-IN** (`//go:embed` VK, 396 B; matches escrow's
        compile-time-constant posture, D-073; PK 116 KB test-only in `zktest`, never in the
        consensus binary). **Replay/instance binding (D-072):** the verifier reconstructs the
        public witness (escrow id + signed schema_commitment) from the RECORD, never from the
        submitter — a proof for escrow A is REJECTED against escrow B (watched test fails red
        if the id binding is removed); same-instance replay → `ErrTerminalState`. Transition
        table: `evtZkRelease` (ACTIVE→RELEASED on a verifying proof) / `evtZkTimeout`
        (ACTIVE→REFUNDED); funds like HASHLOCK (payment-in, no bond); release-without-proof /
        invalid-proof → `ErrInvalidProof`, `ReleaseEscrow`-on-ZK → `ErrWrongClass`, dispute-on-
        ZK → `ErrNoDisputePath`; exhaustive table now covers `classZk`. Auto-refund timeout =
        a compile-time constant `ZkDeliveryTimeoutBlocks` (no proto change — FW-1/FW-2 NOT
        folded in, scope discipline). Determinism: on-chain only `groth16.Verify` (fixed VK,
        randomness-free, recover→INVALID, never panics); setup toxic-waste off-chain (the `gen`
        tool, analog of DKG `crypto/rand`). ZK routes through the SAME bilateral (D-074) +
        signed-consent (D-075) gate; the conservation-gate ZK rejection is lifted. **Buyer-side
        consent gate (review-driven):** the 2.1a consent gate was seller-only, but ZK release is
        seller-driven on a `schema_commitment` not tied to the buyer's want, so a NONE-buyer
        could be matched into a ZK escrow — the buyer-side mirror of the D-075 seller hole. Fix:
        for ZK the BUYER must ALSO sign the exact condition (both chokepoints, fail-closed →
        fallback); exact-param, so it also ties release to the buyer-agreed schema. ZK-only
        (HASHLOCK post-delivery token / OPTIMISTIC ContentHash-tied buyer consent stays FW-1);
        reuses the existing signed field, no FW-2 proto work. End-to-end conditioned ZK CONCLUDE
        (both signing) opens a funded escrow + a bound proof releases it; NONE-buyer match
        rejected (watched). Trusted-setup
        MPC ceremony + circuit-soundness audit = mainnet gates (carried to 1.8-style audit).
        `make lint test` + determinism vector green; 2.1a HASHLOCK/OPTIMISTIC suites green.
- [x] 2.2 wasmd + CVM custom bindings — **DONE (D-077 + D-079).** STOP #1: stay
      v0.50 (D-077 — block-sdk frozen = decisive; v0.53-wasmd line needs go 1.24 =
      kyber collision). STOP #2 approved: wasmd v0.54.9 (SDK v0.50.15 / ibc-go
      v8.8.0 exact match; go 1.23.2 — two-pin ceiling intact), READ-ONLY CONCLUDE,
      permissioned gov-only upload (Nobody/Nobody default genesis). CVM bindings =
      `app/cvm`: escrow-msg allowlist {release/prove/dispute} with contract-address
      signer stamping, Any/Stargate passthrough disabled, bounded session/receipt/
      escrow query plugins; three-layer no-forge model watched by encoder units +
      a real-contract (reflect via wasmvm) e2e incl. the positive contract-releases-
      hashlock path + gas assertion. wasm msgs ride the GENERAL lane; x/wasm
      excluded from the sim manager (vector wasm-inert, byte-identical on the
      iavl-1.2.4 tree). FIRST CGO DEP (libwasmvm; musl builds need
      libwasmvm_muslc). DEVIATION surfaced (D-079): the localnet contract-release
      smoke is deferred to the §6 exit demo — needs a CVM-schema contract binary
      (Rust toolchain); reflect's typed Custom cannot spell it.
- [x] 2.3 x402 gateway — surface contract **(D-083/D-084/D-085).** Node-surface
      verdict: **ALREADY COMPLETE** (no new gRPC — tx service + unordered ext,
      veil/session/receipt/mandate/sigil/epoch queries incl. Twap +
      ReceiptProof, §6.2 fee abstraction). In-repo deliverable =
      `docs/edge/X402_SURFACE.md` (auth model: "hold a key + hold a mandate",
      no server-side signing; deployment models A/B; one-key-one-sigil;
      fee-not-ceilinged + Simulate-first gateway obligations; x402 v1.0-RC
      posture per the R4 advisory — node surface x402-version-independent) +
      `app/x402_surface_conformance_test.go` (gateway-shaped auth proof incl.
      live uusdc fee abstraction; every negative asserted by code) +
      `scripts/x402_surface_check.sh` / `conclave-demo x402-check` (every
      named endpoint against a live localnet). **G1 FIXED (D-084):**
      IssueMandate REPLACE is now SAME-SIGIL only (`ErrGranteeConflict`;
      red-first proven) — any sigil controller could previously rebind a
      bound grantee key, deleting the victim mandate + carrying its
      outstanding_commitment cross-principal. **G2 FOUND + MITIGATED (D-085):**
      a msg-server-failed submit stranded its ante charge and broke the
      conservation invariant on demand (postHandler refund structurally
      impossible in v0.50); severity-graded (not a permissionless halt — crisis
      `MsgVerifyInvariant` panic is recovered — but attacker-reachable invariant
      corruption + operator-conditional `--inv-check-period` halt + gateway
      self-DoS). CLOSED in-task by ante pre-validation:
      `ValidateSubmitIntentPreconditions` (one source of truth) called by both
      the veil msg server AND a `SubmitIntentPrevalidationDecorator` before
      `MandateDecorator`, so no ante-passing submit can fail the msg server;
      watched-red proven. Task 2.8 keeps only the STRUCTURAL fork. **G3 fixed (dev tooling, two stacked breaks):** demo
      `buildIntent` now declares `spend_commitment` (stale since D-065) and
      the driver's gRPC conn forces the gogo-aware codec (the default codec
      panics on LegacyDec customtype — hit live by Twap/Reputation reads);
      demo_e2e.sh was silently broken on both counts. Gateway itself = separate repo
      `conclave-x402-gateway`, NOT in `make test`. Vector: run #1
      (docs/tests/tooling) byte-identical; run #2 (D-084 in isolation)
      byte-identical — no re-baseline.
- [x] 2.4 7683 resolver — surface contract **(D-086/D-087).** Node-surface
      verdict: COMPLETE after ONE genuine gap the new conformance test found
      by walking the FALLBACK path (the no-solver default): fallback
      clearings applied their matches without persisting them, so the
      receipt's terms_hash PRE-IMAGE was unfetchable (`winning_solution_id`
      "" — only concludeWinner set it). Fixed as a stop-approved mid-build
      deviation: `runFallback` now stores a synthetic Solution (id =
      deriveID(0x12‖chamber‖u64be(session_id)), solver "", score 0) + sets
      `winning_solution_id` — the pre-image chain Session →
      winning_solution_id → Solutions → Match is UNIFORM across both
      clearing paths (uniformity proven: the conformance test recomputes
      terms_hash from the QUERIED fallback Solution). In-repo deliverable =
      `docs/edge/7683_SURFACE.md` (translation authority: the resolver's
      OWN mandate, NO on-chain order binding — an origin_order_id field
      rejected as Rule-4 decoration; spend_commitment under-declaration =
      CAN-declare/CANNOT-settle; §6 fill-back evidence contract: what the
      ICS-23 proof proves, ProofHeight = the last header-anchored version,
      and the four MANDATED bridge checks — root/terms/LEG/dedup — each
      demonstrated load-bearing by in-tree skip-variant acceptance) +
      `app/erc7683_surface_conformance_test.go` (watched RED against
      pre-fix HEAD) + `scripts/7683_surface_check.sh` / `conclave-demo
      7683-check` (live sweep PASS incl. ICS-23 against the real
      header-anchored AppHash). **G1(2.4) FOUND (D-087):**
      `Receipt.condition_outcome` is decorative — always NONE, never
      updated on escrow resolution; receipt ≠ delivery completion for
      conditioned legs. Disposition (b): three-place disclosure + the
      mandated unconditioned-leg check makes the v1 coin-fill rail safe by
      construction; the fix is task 2.9. X402_SURFACE.md got the same
      disclosure (its ZK exit-demo buy hits it). Resolver itself = separate
      repo `conclave-7683-resolver`, NOT in `make test`. Vector: run in
      ISOLATION on the fallback-Solution change — **byte-identical**, no
      re-baseline (the sim generates no fillable pair; the conformance test
      is the covering gate).
- [x] 2.5 Real USDC via IBC + session-TWAP fee — **D-081 (STOP #1) + D-082 (build).**
      TWO-STOP run: STOP #1 = the IBC-line decision in isolation (stay ibc-go
      v8.8.0 — the pin already wired; v10-on-v0.50 is a DEADER sub-line;
      the family bump is 3.3's, still blocked by block-sdk-frozen + the
      wasmd-v0.53/go-1.24 kyber collision; EOL-v8 usage recorded as POSTURE
      under P3-B, not drift; the D-077 v0.53 checkpoint re-ran and re-failed
      on (a)). STOP #2 = the integration+TWAP plan. Shipped: x/session
      per-pair price index (0x0B, observations folded inside the Conclude
      cache with the receipts; per-height-mean-then-across-heights TWAP
      recomputed at write; window/min-obs params 1200/10; genesis + gRPC
      `Twap` query) + the nil-safe `TwapSource` seam into
      `ExecuteFeeAbstraction` (warm → fee/price single-rounding truncate-to-
      pool; cold/stale → `fee_conversion_rate`, now the deterministic
      fallback — the `window.go:138` TODO closed; conversion provenance
      events). F-2/F-3 carried EXACTLY: MsgTransfer still zero-valued
      (PINNED by `TestSpendIntentF2BoundaryPin` — 3.4 flips it by deliberate
      red), FaceValuePriceSource untouched (the TWAP's ONLY consumer is the
      fee seam; D-044 swap stays 3.4). IBC-USDC proven by a REAL two-chain
      ICS-20 e2e (ibctesting; `app/ibctesting.go` getters): voucher lands,
      values at face, ibc/<hash> ceilings BIND, fee converts cold→warm
      through the app-wired seam; the live relayer demo = §6 exit demo.
      Vector sequenced for attribution (NO dep changes all task): run #1
      post-regen read-only = byte-identical; run #2 post-wiring = NEW
      baseline (recorded in D-082), two-pass identical. Watched-red proofs:
      seam / cold-gate / pruning each red-without-guard. Fee-TWAP
      manipulation surface → 3.1 audit input (PHASE3_PLAN §3.1 updated).
- [ ] 2.6 Matcher-richness hardening pass — owns latent TODOs: partial-fill/cycle-aware
      fallback, partial-fill cycles + alt numeraire anchoring, §5.1 limit-rate honoring,
      multi-intent-per-sigil, multi-asset in-kind skim. One determinism-focused session
      behind the existing `validateSolution` bar + determinism vector. Rev 2 adds the
      D-076 flake-hardening item (deterministic `randAddr()` fixture seeding,
      `x/mandate/keeper/keeper_test.go:101`).
- [ ] 2.7 Escrow consent completeness (NEW in Rev 2, D-078) — owns the D-075/D-076 FW
      seams: FW-1 (HASHLOCK/OPTIMISTIC class/window-level buyer-side consent; ZK's
      exact-param gate shipped in 2.1b), FW-2 (per-leg `IntentLeg.condition` proto
      refinement, incl. the does-it-relax-D-074 plan-stop question), and the
      timeouts→params promotion (`ZkDeliveryTimeoutBlocks` et al., plan-stop). Off the
      exit path; sequenced after 2.6; proto change ⇒ determinism vector re-baseline.
- [ ] 2.8 Submit-charge STRUCTURAL fork (NEW in 2.3, G2/D-085) — the G2 SECURITY
      hole (a msg-server-failing submit stranding its ante charge → conservation
      break) is already CLOSED in 2.3 by ante pre-validation
      (`SubmitIntentPrevalidationDecorator` + shared `ValidateSubmitIntent
      Preconditions`, watched-red proven). 2.8 owns only the DESIGN QUESTION:
      keep ante-prevalidation as the permanent design, or adopt a v0.50 mode-split
      charging (CheckTx-only ante charge; DeliverTx charge inside the veil msg
      server, revisiting D-059's "zero mode branching") — plus the escrow/veil
      timeouts→params promotion. Opens with that plan-stop; off the exit path;
      any charging-path change ⇒ determinism vector re-run in isolation.
- [ ] 2.9 Receipt condition-outcome integrity (NEW in 2.4, G1/D-087) — the §6.6
      receipt's `condition_outcome` is stamped NONE unconditionally and never
      updated when the escrow resolves, so an ICS-23-provable receipt asserts
      "no condition" for conditioned settlements and receipt-existence gets
      misread as delivery-completion (funds may still REFUND). 2.4's edge
      contracts disclose + neutralize (the bridge-side unconditioned-leg
      rule); 2.9 makes the field TRUE: additive `CONDITION_OUTCOME_PENDING`,
      faithful stamp at CONCLUDE, escrow-resolution write-back
      (RELEASED/REFUNDED/DISPUTED). Proto + cross-module consensus work: own
      session, plan-stop first, determinism vector re-baseline expected,
      Rule-3 watched-red on the write-back. Acceptance: a conditioned
      settlement's receipt reflects its escrow terminal state; unconditioned
      receipts unchanged. Off the exit path.

**Phase-2 exit:** the §1.3 demo extended — external x402 agent buys ZK-verified
data from a Conclave agent; receipt IBC-verified on a second chain (spans
2.1+2.2+2.3+2.5; 2.4 is the parallel inbound rail, off the exit path).

## Phase-3 plan (PROPOSED — docs/PHASE3_PLAN.md, D-080)

Authored 2026-07-10 mid-Phase-2 (research + planning session, docs-only).
**SPEC §10's Phase 3 is Chambers ALONE**; the plan proposes the surrounding
production-readiness scope with three-class authority labels in every section
header ([SPEC §10 PHASE 3] / [SPEC-MANDATED, NOT PHASE-NUMBERED] /
[PROPOSED — BEYOND CURRENT SPEC]). **NOT yet the phase authority** — task 3.0
(Rev at the Phase-2 boundary) re-validates it after 2.3–2.7 + the exit demo
close. Headline structure: 3.1 audit = the gating dependency (opens with an
adopt-vs-audit STOP); 3.3 platform migration GATES the public testnet (go 1.23
+ SDK v0.50 + ibc-go v8 all EOL, block-sdk archived — D-080 research record);
3.11 Chambers = the one spec-named task, design-doc-first. Carries §5.2
advisories for the remaining Phase-2 tasks (x402 v1.0 timing → 2.3; ibc-go v8
EOL supersedes D-077's "v8 suffices" → 2.5's plan-stop). The transferable
working discipline is codified in **docs/METHODOLOGY.md** (10 rules + the
per-task checklist); Phase-3 sessions bootstrap from CLAUDE.md + SPEC +
DECISIONS + PHASE3_PLAN + METHODOLOGY.
