# Conclave — External Cryptographic Audit Scope (Phase-1 Task 1.8)

**Status:** engagement scope, auditor-facing. Not a build artifact.
**Audited revision:** `5f5feb8` (Phase-1 BUILD complete; tag the exact commit at
engagement start).
**Gating role:** SPEC §8(d) — this audit gates **mainnet**, not testnet. The
construction has **no prior audit**. The engagement is scheduled *after* the
4-validator adversarial localnet (Task 1.7) so the auditor reviews a **working
system**, not a paper design.

This document tells an auditor (1) what the system is, (2) precisely which
cryptography Conclave **authored** (your primary target) versus **adopted** from
drand/kyber/tlock (in scope only for correct *usage*), (3) the **open
obligations** stated as specific verify/refute questions, (4) the **security
arguments we have already made** so you check them rather than reconstruct them,
(5) an **evidence map** to the tests that demonstrate each property on the
working system, and (6) the **known limitations and trust assumptions** we are
asserting honestly.

Depth references, not duplicated here:
- `docs/SPEC.md` §8 (the Veil), §6.3 (the Reveal / `ThresholdScheme`), §11.6
  (determinism).
- `docs/PHASE1_DESIGN.md` §2 (order-then-reveal under real crypto), §3 (library
  evaluation), §5 (task breakdown).
- `docs/DECISIONS.md` D-061, D-062, **D-066 through D-070** (the accumulated
  design record this scope is distilled from).

---

## 1. System overview

Conclave is an agent-first sealed-bid exchange built as a Cosmos SDK L1. The
confidentiality product is **the Veil** (`x/veil`): agents submit **sealed
intents** (encrypted trade orders) into a mempool that no one — including
validators — can read before the order of execution is fixed. The core security
property is **order-then-reveal**:

1. **Order.** At a session boundary (`EndBlock(B)`), the set and ordering of
   sealed ciphertexts is fixed deterministically in committed state (the §6.3
   `0x02` state/ttl index). Nothing about the plaintext is needed to do this.
2. **Reveal.** Two blocks later (see the ABCI++ timing note below), validators
   collectively threshold-decrypt the sealed intents. The recovered plaintexts
   become public, the batch auction (`x/session`) clears on them, and settlement
   (`CONCLUDE`) is atomic.

The security goal: **a valid decryption share for a ciphertext cannot exist
until the seal that fixed its ordering is committed.** This replaces the Phase-0
stub, where "you can't read it yet" was enforced only by the state machine (a
modified node could read the plaintext at submission). In Phase 1 the barrier is
**cryptographic**: confidentiality rests on a `t`-of-`n` threshold over a
per-epoch key, and the shares are transported through **vote extensions** that
are causally produced *after* ordering is committed.

### The construction in one paragraph (depth: PHASE1_DESIGN §2/§3)

Encryption is **tlock-style timelock encryption over threshold BLS** (drand's
published Gailly–Melissaris construction). It is a hybrid KEM/DEM: a random data
encryption key (DEK) encrypts the payload under **AES-256-GCM** (the DEM); the
DEK is **Boneh–Franklin-IBE-wrapped** to a per-ciphertext **identity** (the KEM).
The IBE decryption key for that identity is a **threshold BLS signature** on the
identity — so `t`-of-`n` validators, each contributing a signature *share*,
jointly reconstruct the IBE decryption key and open the DEK. Signatures /
IBE-decryption-keys live on **G1**; public keys / the epoch master live on
**G2** (the drand "sigs-on-G1" orientation). The epoch key is produced by an
interactive **DKG** among the validator committee; the per-validator secret share
never aggregates at any single node.

### ABCI++ timing (a load-bearing detail for the order-then-reveal review)

CometBFT delivers a height-`h` vote extension to exactly one place:
`PrepareProposal(h+1).LocalLastCommit`. **`ExtendVote(h)` runs *before*
`FinalizeBlock(h)`** and reads committed state through `h-1`. Therefore a seal
committed at block `B` is first visible to `ExtendVote(B+1)`, whose shares reach
`BeginBlock(B+2)`. **Reveal is `B+2`, not `B+1`.** This was an off-by-one in the
original design diagram; it was found and acted on (the retry counter floors at
`B+2`), and it *strengthens* the guarantee (a share needs the seal **committed**,
not merely decided). See D-069 and §A-F4 below.

---

## 2. The authored vs adopted boundary (orientation — read this first)

The single most important orientation for the engagement: **concentrate your
time on what Conclave authored.** The adopted libraries are externally
audited-adjacent drand production code; they are in scope **only for correct
usage** (right primitive, right parameters, right orientation), not for
re-auditing their internals.

### 2.1 ADOPTED — in scope for correct usage only

| Component | Source | Used for |
|---|---|---|
| Pairing groups + hash-to-curve (BLS12-381) | `github.com/drand/kyber-bls12381 v0.3.1` | the curve, G1/G2, suite |
| Secret sharing & Lagrange (`share`) | `github.com/drand/kyber v1.3.1` | `RecoverCommit`, `PubPoly`, `PriShare` |
| Threshold BLS sign/verify (`sign/tbls`) | `drand/kyber` | partial signatures = reveal shares; the intrinsic pairing verify |
| Pedersen DKG (`share/dkg`) | `drand/kyber` | key generation rounds (deal/response/justify) |
| BF-IBE primitive (`encrypt/ibe`, `EncryptCCAonG2` / `DecryptCCAonG2`) | `drand/kyber` | the KEM; the FO/CCA `rP` integrity check |
| AES-256-GCM | Go stdlib `crypto/aes`,`crypto/cipher` | the DEM |

> **`drand/tlock` is NOT imported.** Its IBE primitive lives in
> `drand/kyber/encrypt/ibe`, importable in isolation; tlock's root package drags
> in `filippo.io/age` + `drand/drand/v2` (the live drand node) and is avoided.
> See the dependency appendix (§8) for pin/license facts.

### 2.2 AUTHORED — the primary audit target

**(A) The `crypto/threshold` cryptographic surface (the four primitive-adjacent
pieces, D-066).** File: `crypto/threshold/threshold.go`.

1. **KEM/DEM hybrid wrapper** — `Encrypt` / `recoverAndDecrypt` / the
   length-prefixed wire encoding (`encodeCiphertext`/`decodeCiphertext`); the
   AES-256-GCM DEM glue (`aeadSeal`/`aeadOpen`).
2. **Identity-derivation rule** — `deriveIdentity`: `id = SHA-256(DST ‖ C_dem)`,
   `DST = "conclave/veil/ibe-identity/v1"`. **The one authored, security-bearing
   choice that diverges from tlock's reference** (see O1).
3. **The `reveal.ThresholdScheme` adapter** — `Scheme`,
   `PartialReveal`/`VerifyShare`/`Combine`/`Threshold`, and the
   `NewScheme*` constructors (incl. `NewSchemeFromCommitBytes`, the public-commits
   bridge the live Reveal builds per-epoch).
4. **The share-verification gating** — `gate`: verifies every share with the
   intrinsic pairing check, drops invalid/duplicate-index shares, returns a
   deterministically sorted survivor set *before* recovery.

**(B) The DKG sub-protocol authored wrapping (D-066/D-068/D-070).** Files:
`crypto/threshold/dkg.go`, `crypto/threshold/transport.go`.

5. **`DKGParticipant`** — the kyber-faithful round wrapper
   (`Round1Deal → Round2Response → ProcessResponses → Finalize(justifications)`),
   the long-term-key helpers (`GenerateLongTermKey`, `DKGGroup`), and
   `DeriveNonce(epoch, roster)` (the deterministic roster nonce). The kyber
   `dkg.Generator` and `ProcessJustifications`/floor are **library**; the round
   sequencing, the node-local `EpochKeyMaterial` shaping, and the nonce derivation
   are authored.
6. **Bundle serde** — `Marshal/Unmarshal{Deal,Response,Justification}Bundle`,
   `MarshalCommits`. kyber ships no bundle serializer (drand serializes in its own
   repo); the byte-stable round-trip is authored and is load-bearing (a
   deserialized deal bundle's `Hash()` must still verify the dealer signature).

**(C) The 1.7 consensus-integration authored surface (D-069/D-070 — NEW since
D-066's four-piece enumeration; do not overlook).** This is consensus code that
*uses* the crypto; the primitives underneath are library, but the integration is
authored and security-bearing:

7. **VE share transport** — `app/vote_extensions.go`: `ExtendVote` (enumerate the
   committed sealed set, `PartialReveal` each), `VerifyVoteExtension`, the proposer
   **injection of the prior block's `ExtendedCommitInfo` at `Txs[0]`**,
   `ProcessProposal` re-validation + strip, and `PreBlocker` collection into the
   per-block share cache. (See §A-inject, §A-F4.)
8. **The live Reveal cutover + retry/cancel** — split across two files.
   `x/veil/keeper/reveal_transport.go` holds `EnumerateInFlightForReveal` (the
   deterministic ordering↔share contract), the `RevealShareSource` seam, and the
   per-session reveal-attempt counter helpers (the `B+2` retry floor).
   `x/veil/keeper/lifecycle.go` holds `RevealSealed` — the per-intent `epoch_id`
   cutover (`0`→StubScheme; `≠0`→`threshold.NewSchemeFromCommitBytes` from the
   epoch's published commits + the collected VE shares) and the retry-exhaustion →
   CANCEL path. Plus `x/veil/keeper/epoch_tag.go` (the both-edge submit-time epoch
   gate).
9. **A2 — on-chain DKG-key registration** — `x/epoch` registration window +
   `MsgRegisterDKGKey`, the account↔valoper binding, and `FinalizeCommittee`
   (committee = registered set, `t=⌊2m/3⌋+1`, FAILED below floor). (See §A-reg.)
10. **B2 — justification round transport** — `x/epoch` justify window +
    `MsgSubmitJustification`, carrying justification bundles to the off-chain
    `Finalize`. (The exclusion/floor logic itself is kyber; the chain-side window
    and the qualified-set tally are authored. See §A-just.)
11. **The honest-threshold confirmation tally** — `x/epoch`'s
    `MsgConfirmEpochKey` counting: the chain publishes the EpochPubKey that
    `≥ t` committee members confirm byte-identically. (This is the basis of trust
    assumption **T1** — flagged because it is a *consensus-uses-crypto* assumption,
    not a crypto property.)
12. **The node-local share custody** — the `ShareProvider` interface and the
    in-memory `MapShareProvider` are defined in `app/vote_extensions.go`; the
    file-backed `FileProvider` (writing `<home>/data/dkg_shares/epoch_<id>.share`,
    0600) is in `app/dkgshare/{provider.go,keyfile.go}`; and
    `app/dkgdaemon/{daemon.go,transport.go}` is the in-process DKG daemon that
    drives the rounds, submits the txs, and persists the share. (See trust
    assumption **T3**.)

> **No novel cryptographic primitive is authored** anywhere in (A)–(C). The
> authored surface is *composition, derivation, gating, transport, and custody*
> over audited primitives. O3 asks you to confirm exactly that.

---

## 3. The open obligations (the heart — verify/refute)

Each obligation is stated as: **Claimed / Proven informally / NOT proven /
Question.** These are the items we know are not formally closed.

### O1 — IND-CCA of the composed KEM/DEM under an attacker-chosen identity *(D-066(a); prompt item a)*

- **Claimed.** The hybrid scheme (BF-IBE-KEM wrapping the DEK + AES-256-GCM DEM)
  remains IND-CCA-secure even though Conclave's IBE identity is
  `id = SHA-256(DST ‖ C_dem)` — a hash of **attacker-influenced ciphertext bytes**
  — rather than tlock's externally-fixed beacon round.
- **Why we diverge (not improvisation).** tlock's identity is an external label
  the client knows in advance (`scheme.DigestBeacon` over a future round). A
  Conclave client **cannot predict its future ordering position** at encryption
  time, so the identity instead binds to the **DEM body** (`C_dem`), which is
  fixed *before* KEM encapsulation — this also breaks the circularity, since the
  KEM output depends on the identity. The hash-to-curve of the identity is still
  performed inside the library IBE with its own domain separation; only the
  *preimage bytes* differ (DEM body vs. round), which they must.
- **Proven informally.** The transitive-binding **mix-and-match closure**
  argument (§A-bind): no cross-component swap yields a fresh valid ciphertext —
  swapping `C_dem` changes `id` so the IBE FO `rP` check rejects; swapping `C_kem`
  breaks the AES-256-GCM tag; the only constructible self-consistent triple is "a
  brand-new intent." Per-ciphertext identity ⇒ shares revealed for one ciphertext
  cannot decrypt another.
- **NOT proven.** A **formal IND-CCA reduction** for the composed scheme under
  adversarial identity. This is the single most important authored, security-
  bearing divergence from the reference and has no written proof.
- **Question.** *Prove or refute IND-CCA of the composed BF-IBE-KEM +
  AES-256-GCM-DEM when the IBE identity is `id = H(DST ‖ C_dem)` and the adversary
  controls/influences `C_dem` (contrast tlock, where the identity is an external
  fixed round). If it does not reduce cleanly, characterize the exact adversarial
  capability the divergence introduces.*

### O2 — DST uniqueness and shared hash-to-curve DST *(D-066(b); prompt item b)*

- **Claimed.** `"conclave/veil/ibe-identity/v1"` is unique across the codebase's
  SHA-256 preimage/signing domain tags (no collision with another hash or signing
  domain), and the library G1 hash-to-curve DST is **shared by the IBE and tBLS
  paths** — so the `id` bytes IBE hashes to G1 are exactly the bytes tBLS signs,
  which is what makes the recovered threshold signature equal the IBE private key.
- **Proven informally.** Holds by construction: a single `newSuite()`
  (`bls12381.NewBLS12381Suite()`) drives both `ibe.EncryptCCAonG2`/`DecryptCCAonG2`
  and `tbls.NewThresholdSchemeOnG1`, so the suite (and its hash-to-curve DST) is
  identical on both paths.
- **NOT verified under audit.** A deliberate cross-codebase uniqueness sweep and
  a suite-identity assertion. This is a **pre-mainnet checklist item** (a grep +
  one assertion), not a deep result, but it must be checked, not assumed.
- **Question.** *Confirm the identity DST is unique across all domain-separation
  tags in the tree, and that IBE encapsulation and tBLS signing hash the identity
  to G1 under the same DST/suite. Flag any second SHA-256 preimage that could
  alias the identity preimage.*

### O3 — Completeness and accuracy of the authored-surface inventory *(D-066(c); prompt item c — UPDATED for 1.7)*

- **Claimed.** §2.2 (A)–(C) is the **complete** authored surface as of `5f5feb8`,
  and nothing labeled "library" in §2.1 is in fact locally modified.
- **Important correction.** D-066's original phrasing — "the four authored pieces
  are the *entire* authored surface" — **is no longer true as stated.** It
  described only the `crypto/threshold` primitive-adjacent pieces (A). Task 1.7
  authored additional consensus-integration surface (C): the registration binding,
  the justification-window transport + qualified-set tally, the VE share transport,
  the epoch-tag gate, and the share custody/daemon. These are consensus code over
  library primitives, but they are authored and security-bearing, and pointing you
  at only the four-piece list would steer you *away* from exactly what 1.7 added.
  §2.2 is the corrected complete inventory; §A-reg/§A-just give the detailed
  treatment of the two highest-risk 1.7 additions.
- **NOT verified under audit.** That the inventory is exhaustive (no authored
  crypto-bearing code outside §2.2) and accurate (no library file locally patched).
- **Question.** *Independently confirm §2.2 captures every authored, security-
  bearing line; confirm `go.mod`/vendor shows the kyber/kyber-bls12381 packages
  unmodified at the pinned versions; flag any authored code path that touches
  key material, share verification, ciphertext structure, or the reveal ordering
  that §2.2 omits.*

### O4 — Reveal-cost cap re-derivation at the production committee size *(D-070; prompt item e)*

- **Claimed.** Per-intent reveal is pairing-dominated (the share gate + IBE open)
  at **≈12 ms/intent at n=4**; BeginBlock reveal cost is **linear** in the sealed
  intent count, so the per-session cap is load-bearing. The recommended
  `max_intents_per_session ≈ 25` gives a ~300 ms reveal budget in a 1 s block; the
  localnet genesis sets 25.
- **NOT closed.** The ≈25 figure is **n=4-specific.** Per-intent Combine cost
  **scales with committee size n** (more partial shares to gate + interpolate), so
  a larger production/testnet committee **raises** per-intent cost and **lowers**
  the safe cap. The default `0` (unbounded) is safe **only** on the epoch-0 stub
  path (no shares, no Combine); a live deployment (epoch ≥ 1) MUST set a finite cap.
- **Question.** *Re-derive the safe `max_intents_per_session` at the production
  committee n (and across plausible n), confirm BeginBlock reveal stays within the
  block-time budget at that n, and confirm the ≈25 figure is not hardened into a
  mainnet default. Identify the n at which a single-block reveal of a full session
  threatens the block budget.*

---

## 4. Security arguments already made (verify or refute)

Here are the arguments we have constructed. Please **check** them rather than
reconstruct from scratch; each names the code and tests that embody it.

### A-bind — Transitive-binding mix-and-match closure (1.3 review item Q1b)

The closure underpinning O1. Three swaps, three rejections: (i) swap `C_dem` →
`id = H(DST‖C_dem)` changes → recovered IBE key no longer opens it → IBE FO `rP`
check ERRORS; (ii) swap `C_kem` (the IBE block) → wrong DEK → AES-256-GCM tag
fails on open; (iii) mix shares across ciphertexts → wrong identity → gate
rejects each share by the intrinsic pairing check. Net: the only path to a fresh
valid ciphertext is to author a new self-consistent triple, i.e. submit a new
intent. **Verify the closure is exhaustive** (no fourth manipulation evades all
three rejections). Code: `threshold.go` `Encrypt`/`Combine`/`gate`/`deriveIdentity`.

### A-barriers — Two independent barriers (gate = robustness, FO = integrity) — *with a self-correction we surface deliberately*

- **The corrected claim.** There are **two independent barriers**:
  - **Liveness/robustness — the authored gate.** `Combine` runs `VerifyPartial`
    over every share and excludes any that fails *before* recovery, so recovery
    succeeds from **any** `t` honest shares. Without the gate, one
    valid-looking-but-wrong share poisons the first-`t` Lagrange set and forces
    retry → eventual CANCEL of an otherwise-decryptable intent (a single-bad-
    validator liveness DoS). This robustness is what the "verifiable partial
    decryption" requirement (FLAG III) buys.
  - **Integrity — the library IBE FO/CCA `rP` check** (in `DecryptCCAonG2`). A
    wrong recovered signature deterministically **ERRORS**; it never silently
    corrupts into a plausible wrong plaintext. This barrier is **independent of
    the authored gate** and rests on the audited library, not authored code.
- **The self-correction (surfaced, not hidden).** An earlier draft of the 1.3
  code comments called the gate "the sole guard against a poisoned recovery."
  That **overstated** the gate: integrity actually rests on the library FO check,
  and the gate's role is robustness/liveness. The claim was corrected during
  internal review to the independent-barriers characterization above. We surface
  this because an auditor probing "what exactly does the gate guarantee" will
  rediscover it; stating it first lets you verify the corrected division of labour
  rather than treat it as a finding.
- **Design choice that keeps this testable.** `recoverAndDecrypt` uses the **raw,
  non-verifying** `share.RecoverCommit`, **not** `tbls.Recover` (which would
  self-filter bad shares and mask the gate's role). Verify this is intentional and
  correct.
- **Question.** *Confirm the two-barrier split: that the gate is necessary and
  sufficient for robustness/liveness, that integrity is delivered by the FO check
  independently of the gate, and that removing the gate costs liveness but not
  integrity (no silent corruption).* Evidence: `TestCorruptShareGateIsLoadBearing`
  (threshold_test), `TestRobustnessCorruptCannotForceInsufficient`,
  `TestForgedShareCannotFakeQuorum` (adversarial_test) — the gate is proven
  RED-without-gate.

### A-floor — The `t−1` information-theoretic boundary and the floor

`t−1` shares **underdetermine** the degree-`(t−1)` polynomial — `RecoverCommit`
errors at the raw primitive; there is no "wrong σ" to compute (Shamir perfect
secrecy: `t−1` shares reveal zero about σ). At the `Combine` API this surfaces as
the **"not yet" vs "rejected" liveness semantic**: `t−1` *valid* shares →
`ErrInsufficientShares` (retry), distinct from the malformed/INVALID path. Because
the gate runs first, `(t−1 genuine + 1 forged)` collapses to `t−1` valid → retry —
a forged share **cannot fake progress to `t`**. **Verify** the boundary is
genuinely information-theoretic at the primitive (not an early reject), and the
`Combine` semantic is exactly "not yet." Evidence: `TestThresholdExactBoundary`,
`TestSecrecyCoalitionBelowThreshold`.

### A-F4 — Order-then-reveal "F-4 by construction" + the full-local-view adversary *(prompt item d)*

- **By construction.** A valid reveal share for a ciphertext cannot exist until
  the seal that fixed its ordering is committed: shares are produced in
  `ExtendVote(B+1)` reading committed state through `B`, and consumed in
  `BeginBlock(B+2)`. No honest node produces a share before ordering is final; a
  single malicious validator holds 1 of `n` shares and cannot reach `t` alone.
  The **B+2 timing** (the ABCI++ off-by-one, §1) strengthens this — a share needs
  the seal **committed**.
- **The F-4 proof construction (state it precisely for the auditor).** The
  real-network F-4 proof is a **full-local-view adversary holding `< t` shares,
  NOT a forked node binary.** The adversary is handed a **real** node's secret
  share + the sealed ciphertext + all public material, and still cannot decrypt —
  because the barrier is the **cryptographic threshold**, not node-binary
  integrity. Granting a real share + full local view is if anything **stronger**
  than a binary fork (it gives the adversary strictly more than a modified node
  would have). The guarantee rests on the threshold and the test exercises exactly
  that.
- **Question.** *Confirm order-then-reveal holds by construction: that no `t`-of-`n`
  share set for a ciphertext is obtainable before its seal is committed, that the
  B+2 timing is correct against CometBFT v0.38 ABCI++, and that the full-local-view
  `<t`-share adversary is the right (and a sufficiently strong) model.* Evidence:
  `TestRevealRealSchemeEndToEnd` (does NOT open at B+1 with shares withheld even
  though ordering is fixed), `TestEnumerateInFlightForReveal`,
  `TestRevealTransportCollectAndDecrypt` (incl. 0/`t−1`-cannot), and the live
  `veil-f4-adversary` on `scripts/localnet4.sh`.

### A-inject — Injected-`Txs[0]` unforgeability (four legs)

The chain distinguishes "`Txs[0]` is injected commit-info" from "`Txs[0]` is a
normal tx" by **position + height, never by content-sniffing**: (1) at
injection-active heights ProcessProposal REQUIRES `Txs[0]` to decode as
`InjectedRevealVotes` and pass `ValidateVoteExtensions`, else REJECT; (2)
`ValidateVoteExtensions` authenticates every extension against the validator's
**consensus key + ≥2/3 power** — a proposer cannot forge/alter another's shares;
(3) `Txs[0]` is stripped before the Block-SDK handler and consumed in PreBlocker
before baseapp's tx loop (a stray blob → harmless `ErrTxDecode`, never executed);
(4) share **authenticity** rests independently on the BeginBlock pairing gate
(a second barrier). The split is: **VE signature authenticates the SUBMITTER;
the pairing gate authenticates CORRECTNESS.** **Verify** a normal tx cannot
masquerade at index 0 and a proposer cannot inject forged/altered shares.
Evidence: `TestRevealVoteExtensionUnforgeable`, `TestInjectedRoundTrip`,
`TestCollectIsDeterministicAndDropsGarbage`.

### A-reg — A2 on-chain DKG-key registration binding

kyber needs each validator's long-term **G2** public key + the agreed roster +
a shared nonce before round 1, none derivable from the ed25519 consensus key —
so the G2 key must be **published**. `MsgRegisterDKGKey` is keyed by the
**signer's** validator identity (msg-server derives valoper from the signer
account), authorized against the bonded candidate set — the **same
account↔valoper binding** the deal/response/confirm rounds use, so a key cannot
be registered in another validator's slot. The **committee is the registered
set** (a non-registrant is simply excluded — no single-point veto), finalized at
the register-window close with `t = ⌊2m/3⌋+1` for registered size `m`; FAILED if
`m < min_committee_size`. The roster nonce is `DeriveNonce(epoch, sorted-roster)`
(deterministic, no extra round). **Verify** the binding is unforgeable and the
"registered set = committee, no veto" property holds. Evidence:
`TestRegisterBindingRejectsNonCandidate`, `TestRegisterBindingViaMsgServer`,
`TestNonRegisteringCandidateDoesNotVeto`, `TestTooFewRegistrationsFailsEpoch`.

### A-just — B2 justification round / qualified-set finalize

Pre-1.7, `Finalize` collapsed on **any** complaint, so **one faulty dealer in the
first committee vetoed the entire confidentiality guarantee** (epoch 1 never
activates → the Veil stays in the epoch-0 plaintext window — a Byzantine veto, not
bounded grief, since the first epoch has no prior key). The fix restructures
`crypto/threshold` into the kyber-faithful phases and adds a `justify` window +
`MsgSubmitJustification`. **Key insight:** excluding a faulty *dealer* does **not**
change `n` or the reveal threshold — all `n` holders still hold valid shares of
the **qualified-set** master secret; kyber's `ProcessJustifications` **aborts if
qualified dealers < t** (the floor, enforced *inside* kyber, not re-derived in
consensus); and the existing `≥ t`-identical-confirmation tally publishes the
qualified-set key **unchanged**. So the chain-side is a transport window only, no
finalize surgery. **Verify** the exclusion is sound (excluded dealer's
contribution is genuinely removed, surviving shares are consistent), the floor is
correctly enforced, and the tally publishes the right key. Evidence:
`TestJustificationExcludesFaultyDealerAndCompletes` (f=1 completes),
`TestJustificationFailsBelowFloor` (f=2 aborts),
`TestFaultyDealerExcludedEpochActivates` (on-chain: epoch ACTIVE, published key
decrypts a t-of-n reveal).

---

## 5. Evidence map (the working system you review)

The auditor reviews a working 4-validator system, not a paper design. Properties
→ the tests / live proofs that demonstrate them. (All under `make lint test`;
the live proofs under `scripts/localnet4.sh`.)

| Property | Evidence |
|---|---|
| Round-trip t-of-n recovery (n∈{4,5,7,10}) | `crypto/threshold/threshold_test.go` `TestRoundTripTofN` |
| `t−1` cannot / `t` can (info-theoretic) | `TestThresholdExactBoundary`, `TestSecrecyCoalitionBelowThreshold` |
| Gate is load-bearing (RED without it) | `TestCorruptShareGateIsLoadBearing`, `TestRobustnessCorruptCannotForceInsufficient` |
| Forged share cannot fake quorum | `TestForgedShareCannotFakeQuorum` |
| Withheld-share robustness (scattered subsets) | `TestRobustnessWithheldShares` |
| `Combine` determinism | `TestCombineDeterministic` |
| kyber lowest-index selection premise (T2) | `TestKyberRecoverCommitSelectsLowestIndex` (RED on wrong expectation) |
| DKG shares stay node-local | `TestDKGSharesAreNodeLocal` |
| Drain-before-rotate / cross-epoch isolation | `TestChurnDrainBeforeRotate`, `x/epoch` `TestDrainBeforeRotateAndCancelReleases` |
| Leaver tolerance (n−t / n−t+1) | `TestChurnLeaverTolerance` |
| Terminal CANCEL signal | `TestStuckSessionTerminalSignal`, `x/veil` `TestRevealRetryExhaustionCancels` |
| Partial-overlap resharing isolates by epoch id | `x/epoch` `TestPartialOverlapResharing` |
| Bundle serde byte-stable | `crypto/threshold/transport_test.go` `TestDealBundleSerdeRoundTrip`, `TestDKGViaTransportConsistentKey` |
| Justification exclusion / floor | `TestJustificationExcludesFaultyDealerAndCompletes`, `TestJustificationFailsBelowFloor`, `TestFaultyDealerExcludedEpochActivates` |
| Registration binding / no-veto / floor | `TestRegisterBindingRejectsNonCandidate`, `TestNonRegisteringCandidateDoesNotVeto`, `TestTooFewRegistrationsFailsEpoch` |
| DKG publishes consistent key cross-node | `x/epoch` `TestDKGLifecyclePublishesConsistentKey` |
| Failed-DKG degrades, no halt | `TestFailedDKGDegradesNoRotation` |
| Epoch boundary determinism | `TestBoundaryDetectionDeterministic`, `TestEpochChainStateDeterministic` |
| Both-edge epoch-tag gate | `x/veil` `TestSubmitIntentEpochTagBothEdges`, `TestIsDecryptableEpochBothEdges` |
| Order-then-reveal / F-4 by construction | `x/veil` `TestRevealRealSchemeEndToEnd`, `TestEnumerateInFlightForReveal`; `app` `TestRevealTransportCollectAndDecrypt` |
| Injected-`Txs[0]` unforgeability | `app` `TestRevealVoteExtensionUnforgeable`, `TestInjectedRoundTrip`, `TestCollectIsDeterministicAndDropsGarbage` |
| Real-path reveal determinism | `x/veil` `TestRevealRealSchemeDeterministic` |
| spend_commitment Reveal-slash (agent-safety chokepoint) | `x/veil` `TestRevealUnderDeclarationSlashes`, `TestRevealUndeclaredDenomSlashes`, `TestRevealOverDeclarationOpens` |
| Reveal never panics on hostile input | `x/veil` `TestFuzzRevealNeverPanics` |
| Reveal-cost benchmark (O4 basis) | `crypto/threshold/reveal_bench_test.go` `BenchmarkRevealCombineUnderCap`, `TestRevealCostWithinBound` |
| **Live (localnet4):** epoch ACTIVE, app-hash agreement across 4 nodes, encrypted reveal+clear at B+2, F-4 adversary, faulty-dealer no-halt | `scripts/localnet4.sh` + `conclave-demo veil-e2e` / `veil-f4-adversary` |

---

## 6. Known limitations / non-goals (honestly bounded)

### L1 — Epoch-0 plaintext-bootstrap window *(D-069)*

The Veil guarantee **does NOT hold** from genesis until the first DKG reaches
ACTIVE. Genesis ships no ACTIVE epoch (`current_epoch_id = 0`); until epoch 1,
clients submit `epoch_id == 0` intents that reveal via `StubScheme` (identity) —
**effectively plaintext**. This is the honest security claim for a fresh chain,
not a silent gap: a genesis-provisioned key would require shipping secret shares
at genesis (defeats the DKG / D-062) or trusting a dealer. **Operators MUST NOT
route sensitive flow before epoch 1 is ACTIVE.** In scope: confirm there is no
path by which an `epoch_id == 0` intent is treated as confidential, and that the
window is operationally detectable.

### L2 — `max_intents_per_session` default is unsafe for live epochs *(D-070; the operational face of O4)*

The default `0` (unbounded) is safe **only** on the epoch-0 stub path (no shares,
no Combine). A live deployment (epoch ≥ 1) MUST set a finite cap, re-derived per
O4. Mis-set, BeginBlock reveal can exceed the block budget.

### L3 — Per-share proof is the intrinsic pairing check, not a separate NIZK *(D-066, FLAG III) — cross-references O1*

The "verifiable partial decryption" requirement is satisfied **by construction**:
the share *is* its own proof (the tBLS signature share; verified by the intrinsic
pairing check `e(σ_i, g2) == e(H(id), PK_i)`), with no separate zero-knowledge
proof authored. This is sound, but the auditor's question is **not** "is the
pairing check correct" — it is **"does the check bind the share to *this*
ciphertext's identity, so a share valid for one ciphertext cannot be replayed
against another?"** That is **O1 viewed from the other side**: the same
`id = H(DST‖C_dem)` per-ciphertext binding that O1 interrogates for IND-CCA is
what makes a share non-replayable across ciphertexts here. Verify the two
together — if the identity binding is sound for O1, share non-replayability
follows, and vice versa.

---

## 7. Assumptions & trust model

### T1 — Honest-threshold confirmation-counting basis *(D-068 — the most easily overlooked; a consensus-USES-crypto assumption, not a crypto property)*

The published `EpochPubKey`'s correctness rests on the **honest-threshold
assumption**: the chain **cannot compute** the public key itself (that needs
node-local DKG state, and re-deriving it in consensus would be authored crypto in
the consensus path — rejected). Instead, `MsgConfirmEpochKey` carries each node's
locally finalized `(epoch_pub_key, commits)`, and the chain **publishes the value
that `≥ t` committee members confirm byte-identically**, else FAILED. This is a
trivially deterministic count over committed bytes (no crypto in BeginBlock/
EndBlock), but its **security basis is `≥ t` honest nodes not confirming a wrong
key — NOT consensus recomputation.** Cross-node agreement on the *same* key is the
DKG's own guarantee; the chain trusts the `≥ t` identical confirmations. This is
the standard threshold assumption and we judge it acceptable, but it is the
**basis** and we flag it explicitly because it is where consensus *uses* a crypto
assumption rather than verifying a crypto property. **In scope:** confirm `t` is
the right quorum for this confirmation count (it is the reveal threshold), and
that no `< t` set of confirmations can publish a key.

### T2 — kyber `RecoverCommit` lowest-index selection premise *(D-067)*

The adversarial robustness tests depend on kyber's `share.RecoverCommit` selecting
the **lowest-`t` shares by index** (`poly.go` `xyCommit` sorts `byIndexPub`,
breaks at `len==t`) — undocumented-in-contract behavior that could change via a
silent dependency bump, not our diff. `TestKyberRecoverCommitSelectsLowestIndex`
pins it (RED if the rule changes), and the version pin (§8) freezes it. **In
scope:** confirm the robustness argument does not depend on this selection rule
for *correctness* (only the test premise does), and that the pin + probe are
sufficient guard.

### T3 — DKG-daemon trust boundary (D1) *(D-070)*

The in-process DKG daemon (`conclaved start --dkg-daemon`,
`StartCmdOptions.PostSetup`) **holds the node-local secret share AND signs with
the validator account key** — so it is node-coupled by design. The share is
written straight to `<home>/data/dkg_shares/epoch_<id>.share` (mode `0600`) and
**never leaves the node process, never enters consensus state, and is never
gossiped** (D-061/D-062). **In scope:** confirm the share never transits a
consensus or network path; review the file-store permissions and the daemon's
tx-signing/sequence handling for share leakage or key-misuse surface.

### T4 — Dependency pin & license *(D-066)* — see §8.

### Overall trust model

- **Threshold.** Confidentiality holds against any coalition of `< t = ⌊2n/3⌋+1`
  validators (Byzantine). Liveness of the reveal holds while `≥ t` honest
  validators emit shares; below that, sessions retry then CANCEL (bonds refunded,
  chain never halts).
- **DKG.** Key generation is an **off-chain validator protocol** using
  `crypto/rand`; only public bundles transit chain state. A faulty dealer is
  excluded by justification (does not veto); the committee is the registered set.
- **Determinism (§11.6).** All on-chain reveal paths take no fresh randomness:
  signature shares are deterministic (`σ_i = sk_i·H(id)`), the gate is a
  deterministic pairing check over a sorted/deduped set, and Lagrange + IBE/AEAD
  decryption are deterministic given `(shares, ct)`. Encryption and the DKG's
  randomness are off-chain.

---

## 8. Appendix — dependency, version, and license facts *(D-066)*

- `github.com/drand/kyber v1.3.1` — the drand fork (it, not `go.dedis.ch/kyber/v3`,
  carries `encrypt/ibe`). Go directive `1.18`.
- `github.com/drand/kyber-bls12381 v0.3.1` — **pinned pre-`v0.3.4` deliberately:**
  the pin keeps the go directive at 1.23 (**no go-1.24 toolchain bump**) and
  `golang.org/x/*` **unchanged**. `kyber-bls12381@latest` (v0.3.4) would force
  go 1.24 + x/* churn.
- **License:** MPL-2.0 (file-level copyleft — fine for linking a Go binary).
  **Re-verify on any bump.**
- **`drand/tlock` not imported** (its root drags in `filippo.io/age` +
  `drand/drand/v2`); only `drand/kyber/encrypt/ibe` is used. DEM is stdlib
  AES-256-GCM (no `golang.org/x/crypto` dependency).
- Module-count footprint at adoption: 761 → ~772 (kyber, kyber-bls12381, and small
  pure-Go transitives: circl, clockwork, edwards25519).
- **In scope:** confirm the pinned versions are the audited ones, the license
  obligations are met, and no transitive dependency introduces a divergent
  BLS12-381 or hash-to-curve implementation.

---

*Prepared for the Phase-1 Task 1.8 external cryptographic audit, the SPEC §8(d)
mainnet-gating milestone. The authored construction (§2.2), the open obligations
(§3), and the assumptions (§7) are the engagement's center of gravity. Findings
are triaged back into the decisions log.*
