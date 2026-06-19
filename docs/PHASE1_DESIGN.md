# Conclave Phase 1 — The Veil for Real (Design Document)

**Status:** Approved design (planning session, close of Phase 0 → Phase 1).
**Baseline:** HEAD `9ea63b2`, `make lint test` green (lint exit 0; 12 test
packages `ok`; 0 FAIL / 0 SKIP — the `-Enabled`-gated sim/determinism suite is
correctly excluded from `make test`, D-057).
**Scope:** Replace `StubScheme` (identity-Reveal) with BLS12-381 threshold
encryption per SPEC §8, behind the existing `ThresholdScheme` interface, with
epoch DKG, vote-extension share transport, and the order-then-reveal guarantee
resting on share availability rather than state-gating (AUDIT F-4).
**Working method:** this document is the Phase-1 plan reviewed before any build
task. No consensus code was written to produce it. Each task below is one
working session with a stop-and-review. Phase 1 is where SPEC §8 is thinnest;
this document flags every under-specification rather than improvising.

---

## Three load-bearing flags surfaced up front

SPEC §8 is a one-paragraph plan. Three places where it is underspecified change
the build plan and are recorded loudly here (and in DECISIONS) rather than
quietly reshaped:

- **FLAG I — "threshold encryption" ≠ "threshold signatures."** §8 names the
  drand/kyber lineage, but those libraries ship threshold *signatures* + DKG, not
  threshold *encryption*. The encrypt-to-epoch-key layer is something Conclave
  **authors** on kyber primitives. Because we author it, it has no prior audit,
  so it is the **heart of the §8(d) external audit** (Task 1.8). See §3.
- **FLAG II — `EpochSetup` cannot model a real DKG.** The current interface
  returns *all* key shares to a *single caller*; a real distributed DKG never
  lets one node hold all shares. "Phase 1 swaps the implementation only" is true
  for the consuming state machines (`Combine`, client `Encrypt`, `Threshold`) but
  **false for key generation** — `EpochSetup` must be redesigned into a
  multi-round DKG sub-protocol. See §3.
- **FLAG III — verifiable partial decryption is a hard MUST-HAVE.** One
  syntactically-valid but wrong share poisons `Combine` undetectably. Each
  `RevealShare` must carry a NIZK / pairing proof that it is a correct partial
  decryption under that validator's published key share. A library/construction
  that cannot produce such a proof is **disqualified** in the Task 1.3
  evaluation. See §2(c).

---

## Section 0 — Entry-criteria resolution (F-1)

F-4 is foundational and gets its own section (§2). This section resolves
**AUDIT F-1**, which lands **first**, as Task 1.0, before any vote-extension /
DKG code.

**Why first.** The audit verified the *chain* is deterministic (4 seeds,
byte-identical app hashes) but the determinism *harness* is flaky and therefore
not trustworthy as a CI gate. Phase 1 adds vote extensions + DKG — the largest
new nondeterminism surface in the project's history. A flaky gate guarding the
surface most likely to introduce nondeterminism is the worst possible ordering.
Fix the gate while it still guards known-good code, so a *new* flake in Phase 1
is unambiguously a real defect.

**The three F-1 defects and their fixes:**

1. **Un-seeded self-seeding** (`rand.Int63()` → non-reproducible). *Fix:* drive
   the determinism run from an explicit, committed seed vector (CLI flag,
   default a fixed vector). No call into an un-seeded global RNG anywhere in the
   harness's own control flow.
2. **Intermittent SKIP on "empty validator set"** (randomized sim-genesis
   depletes validators; the harness SKIPs, silently disarming the gate). *Fix:*
   (a) tune sim-genesis so the active set survives 500 blocks — pin a floor on
   bonded validators and down-weight (not remove) the standard ops that can fully
   unbond/jail the set to zero (the tolerant-full-op set of D-057 stays); (b)
   convert the SKIP into a hard precondition — assert a non-empty validator set
   at genesis and **fail** (not skip) if the set empties during a determinism
   run. A gate that can SKIP itself is not a gate.
3. **Early abort (~8 s)** — tied to (2)'s depletion; resolved by the same genesis
   tuning.

**Acceptance (Task 1.0):** reproducible per fixed seed (byte-identical app hash
across two independent process invocations, every seed); a CI target runs the
vector twice and exits non-zero on any SKIP / abort / hash mismatch; the
500-block run keeps the validator set intact for every seed; D-057's
`SetPrecommiter` flush + tolerant ops preserved; **no crypto code.**

---

## Section 1 — The D8 decision (resolved: stay on SDK v0.50)

SPEC §14 D8: bump to SDK v0.53 to replace the `app/unordered/sigverify.go` fork
(D-027) with native ADR-070, or stay on v0.50 through Phase 1.

**What the bump buys:** retire the sigverify fork (D-027) and the TxBody
extension-option encoding of the unordered flag (D-026) — v0.53 has native
`unordered`/`timeout_timestamp` fields; removes a forked file, its provenance
pin, and conformance-suite maintenance. Long-term alignment for Phase 2.

**What the bump costs/risks:** block-sdk/v2 compatibility (the laned mempool,
§6.7, is consensus-load-bearing); ibc-go compatibility; re-verify the goleveldb
replace pin (D-032/F-6); re-evaluate the D-057 sim persistence workaround
(`SetPrecommiter`), which §8/D-057 explicitly flag against a v0.53 lifecycle
change; full 0.1–0.11 regression.

**Decisive observation:** vote extensions — the actual Phase-1 consensus surface
— are a CometBFT v0.38 / SDK v0.50 feature **already available on our current
pins**. The bump buys *nothing Phase 1 strictly needs* while re-baselining the
entire dependency graph at the exact moment we add threshold crypto + DKG + vote
extensions. Isolate variables.

**Empirical spike (Phase-1 boundary, throwaway; Go module proxy):**

- `skip-mev/block-sdk/v2` latest = **`v2.1.5`** (our exact pin) → requires
  `cosmos-sdk v0.50.3`, `cometbft v0.38.5`. **No newer v2 release exists; no
  `block-sdk/v3` module path exists.** block-sdk has **no SDK-v0.53 line at all.**
- `cosmos/ibc-go/v8` latest = **`v8.8.0`**; the entire v8 line targets
  `cosmos-sdk v0.50.x` (v8.8.0 → v0.50.9). SDK v0.53 would force an ibc-go
  **major** bump (v8 → the v0.53-compatible line), not a point release.
- `cosmos-sdk v0.53` is mature (through v0.53.7) — the SDK is ready; its
  ecosystem (block-sdk, ibc-go v8) is not.

**Decision: stay on SDK v0.50 through Phase 1.** Keep the sigverify fork
(byte-identical to upstream for ordered txs; behavior-pinned by the conformance
suite — a low, bounded, tested cost). Recorded as **D-060**.

**Accurate flip-condition for the Phase-1 → Phase-2 re-evaluation:** the bump
becomes viable only when **both** (a) an MVS-clean block-sdk release targeting
SDK v0.53 exists (today: none — the gating dependency), **and** (b) a
v0.53-compatible IBC line is adopted (today this is an ibc-go major bump, not a
point release), **and** (c) the D-057 sim lifecycle is confirmed intact (or
cheaply replaced). The Phase-2 wasmd integration (SPEC §10 task 2.2) forces a
dependency revisit regardless, making it the natural re-evaluation point.

---

## Section 2 — F-4: order-then-reveal under real crypto

**Today (stub):** ordering is fixed by the seal in `EndBlock(B)`; the Reveal in
`BeginBlock(B+1)` calls `scheme.Combine(nil, ct)` (identity,
`x/veil/keeper/lifecycle.go`). The impossibility of early decrypt is enforced
**only by the state machine** (intents merely not-yet-OPEN); there is no
cryptographic barrier — modified code could read the plaintext `ciphertext` at
submission. AUDIT F-4 names exactly this: Phase 1 moves the guarantee onto
**vote-extension share availability.**

### The Phase-1 mechanism

```
Block B   EndBlock(B): session s seal — the SET and ORDERING of sealed
          ciphertexts is fixed in committed state (deterministic enumeration,
          reusing the §6.3 0x02 state/ttl index).
          ── ordering is now final and part of B's app state ──
Precommit ExtendVote(B): each validator i computes, for every sealed ct in s,
 of B        share_i = PartialReveal(keyshare_i, ct)  + a validity proof,
          and packs them into its vote extension for B.
Block B+1 PrepareProposal(B+1): proposer has B's vote extensions available
          (ABCI++: VEs of height H reach the proposer of H+1).
          BeginBlock(B+1): collect >= t = floor(2n/3)+1 VERIFIED shares per ct,
          Combine -> plaintext -> intent OPEN. (retry/cancel below)
```

**Why this is the real guarantee.** A `RevealShare` for `ct` does not exist
anywhere until a validator runs `ExtendVote(B)` — during the precommit of block
B, i.e. *after* B's content (including the seal that fixed ordering) is decided.
No honest node produces a share before ordering is final, and a single malicious
validator holds one of `n` shares — it cannot reach `t`-of-`n` alone. The
cryptographic threshold replaces the stub's state-gate.

### SPEC §8 hard requirements (a)–(d)

**(a) Ordering strictly before decrypt — satisfied by construction.** Shares are
generated in `ExtendVote(B)` (causally after B's ordering commit) and consumed in
`BeginBlock(B+1)`. The binding that makes this airtight: the set and order of
sealed ciphertexts must be a **deterministic enumeration of B's committed state**
so every validator's `ExtendVote` covers the same ciphertext set. The §6.3 0x02
state/ttl index already gives a deterministic seal ordering — reuse it.
ProcessProposal's role is unchanged (it validates lane structure, D-032/F-4); the
order-then-reveal guarantee in Phase 1 rests on the VE/threshold mechanism, **not**
on a ProcessProposal check. This supersedes §6.3's "ProcessProposal check"
framing and closes F-4 (recorded in DECISIONS).

**(b) DKG under validator churn.** Epochs are short (~1 day); the epoch boundary
reuses the CometBFT validator-set-update trigger; mid-epoch joins wait for the
next epoch (§8). Sessions run every `k=3` blocks, so many sessions per epoch all
encrypt to one `EpochPubKey`. Design resolutions:

- **Epoch key handover = drain-before-rotate (primary).** All sealed intents
  from epoch `e` are revealed under epoch `e`'s shares before epoch `e+1`'s key
  becomes the encryption target; clients tag each ciphertext with its epoch id so
  the Reveal selects the correct share set.
  - **CANCEL is a valid drain terminus.** A session that CANCELs after
    `max_reveal_retries` (§8(c)) **releases the rotation** — a stuck/undecryptable
    session must not stall epoch rotation behind it. The rotation waits only on
    each in-flight session reaching a *terminal* Reveal outcome (OPEN-and-cleared
    or CANCELLED), never on indefinite retry. This interaction is specified
    explicitly and tested in Task 1.5/1.6.
  - **Grace-window retention = named fallback.** If drain-before-rotate is
    observed to stall liveness under churn in the Task 1.5 harness, the fallback
    is to retain epoch `e`'s share material for a bounded grace window so `e` and
    `e+1` overlap. Documented now as a scoped fallback (same primary+fallback
    pattern as §8(c)), not adopted by default — not indecision.
- **Leavers mid-epoch.** A validator that exits mid-epoch held a share needed for
  `t`-of-`n` on in-flight ciphertexts; drain-before-rotate bounds this exposure to
  one epoch, and the threshold tolerates up to `n−t` missing shares.
- **FLAG (highest-risk, deferred to a design sub-review):** the DKG protocol
  itself (interactive Pedersen-style, multi-round) and **how its messages are
  transported** (on-chain txs vs VE-injected vs side channel) are entirely
  unspecified in §8. This is the single most unspecified, highest-risk decision
  in Phase 1 and gets its **own stop-and-review before any 1.5 code** (see §5).

**(c) Liveness degradation / retry / CANCEL.** If `Combine` has `< t` verified
shares, re-run the Reveal the following block, up to `max_reveal_retries` (3),
then CANCEL the session (intents → CANCELLED, bonds refunded); the chain never
halts. The available shares for a session in a given block are observable
identically by every node (committed VEs), so "did we reach `t`?", the retry
counter, and the CANCEL decision are deterministic consensus state.

- **FLAG — share re-transport (made explicit).** B's VEs are a one-shot source;
  if `BeginBlock(B+1)` is short of `t`, retry shares for `B+2` come from
  **validators re-emitting shares for any not-yet-revealed session in each
  subsequent block's VE** until the session is revealed or CANCELLED. §8's
  "re-runs the Reveal the following block" presumes this; it is now specified.
- **FLAG — corrupted shares → verifiable partial decryption (MUST-HAVE).** A
  malicious validator can submit a syntactically valid but wrong share that
  poisons `Combine`. Each `RevealShare` **carries a proof** (pairing check /
  NIZK) that it is a correct partial decryption under that validator's *published*
  key share; bad shares are deterministically excluded before `Combine` rather
  than corrupting output. This is a hard requirement on the crypto construction
  (§3) and on the `RevealShare` type (it carries the proof). A construction that
  cannot produce this proof is **disqualified** in Task 1.3.

**(d) External-audit gating milestone — Task 1.8, gates mainnet (not testnet).**
Audited unit: the authored threshold-encryption construction (FLAG I), the DKG,
the VE share transport, the order-then-reveal guarantee, the retry/cancel
liveness paths, and the verifiable-share exclusion. Engage *after* the
multi-node adversarial localnet (Task 1.7) demonstrates the property, so the
auditor reviews a working system. Budget calendar + cost now.

### Additional FLAGS (performance / DoS surface §8 omits)

- **VE size.** Every validator includes a share (+proof) per sealed ciphertext
  per block → VE bloat. Mitigation: a new `max_intents_per_session` param bounds
  VE size and Reveal cost.
- **Reveal cost.** `BeginBlock` Combine cost scales with (sealed intents ×
  validators); needs the same cap plus a benchmark gate (Task 1.7).
- **EpochPubKey distribution.** Clients must fetch the current `EpochPubKey` to
  encrypt; it lives in queryable chain state (gRPC), tagged by epoch id.

---

## Section 3 — BLS threshold library evaluation

Per §8: assess existing Go BLS threshold libraries (drand/kyber lineage); wrap
behind the existing `ThresholdScheme`; do **not** implement DKG ad hoc.
Throwaway evaluation spikes are fine — marked, never committed to the chain.

**Evaluation criteria:** license · maintenance · audit status · fit behind
`ThresholdScheme` · and three Conclave-specific MUST-HAVEs: **threshold
*encryption* (not just signatures)** (FLAG I), **verifiable partial decryption**
(FLAG III — disqualifying if absent), **deterministic on-chain decryption**
(§11.6).

### FLAG I — "threshold encryption" vs "threshold signatures"

SPEC §8 says threshold *encryption* (encrypt to a public key; `t`-of-`n`
decrypt). The canonical drand/kyber libraries ship threshold **signatures**
(`tbls`) + **DKG** — not threshold encryption. Threshold encryption over
BLS12-381 (threshold ElGamal, or a Boneh–Franklin IBE-style KEM as in drand's
`tlock` timelock) is a real but **separate construction Conclave authors** on the
library's primitives. This is the biggest substance gap between §8's one-line
plan and a buildable system, and (because authored) the heart of the §8(d) audit.

### Candidate libraries

| Library | Provides | License | Notes |
|---|---|---|---|
| `go.dedis.ch/kyber/v3` (+ `drand/kyber-bls12381`) | pairing groups, secret-sharing (`share`), threshold BLS sigs (`tbls`), Pedersen/Rabin **DKG** | MPL-2.0 (file-level copyleft; OK for linking) | The §8-named lineage, drand-proven, actively maintained. **DKG + primitives, NOT threshold encryption.** |
| `supranational/blst` | fast BLS12-381 sigs/pairings (CGO) | Apache-2.0 | Ethereum-grade perf; low-level only — no DKG, no threshold encryption. The primitive floor if building. |
| `drand/tlock` | timelock **encryption** (age + IBE over a drand threshold network) | permissive | Reference for the encrypt-to-threshold-key construction; not a drop-in (assumes the public drand network). |
| `herumi/bls` | BLS (CGO) | BSD | Alternative primitive layer; CGO dependency. |

### Recommendation

Use **`kyber/v3` + `kyber-bls12381`** for the DKG and threshold primitives
(mature, drand-proven, the SPEC's named lineage), and **author the
threshold-encryption layer** (encrypt-to-`EpochPubKey`, verifiable partial
decryption) on top, referencing `tlock`'s IBE/KEM approach. Do **not** build BLS
or DKG from scratch. The encryption construction is the part Conclave owns and is
the heart of the §8(d) audit. Recorded as **D-061**.

- **Determinism.** DKG randomness uses `crypto/rand`, but DKG runs **off-chain**
  among validators (a protocol, not consensus state) — acceptable. The on-chain
  path is `PartialReveal` (in `ExtendVote`) and `Combine` + verification (in
  `BeginBlock`); pairing-based partial decryption is **deterministic given
  (share, ct)** with no fresh randomness — verify this holds for the chosen
  construction. Encryption is client-side (off-chain). Consensus stays float-free
  and deterministic (§11.6).
- **License.** MPL-2.0 is file-level copyleft, fine for linking in a Go binary;
  recorded in DECISIONS.

### FLAG II — `ThresholdScheme.EpochSetup` cannot model a real DKG

"Phase 1 swaps the implementation only" is true for the **consuming state
machines** — `Combine` (Reveal), client `Encrypt`, `Threshold(n)` keep their
signatures. It is **not** true for `EpochSetup`:

```go
EpochSetup(valset []Validator) (EpochPubKey, []KeyShare, error)
```

returns *all* key shares to a *single caller*. A real distributed DKG never lets
one node hold all shares — that defeats the threshold security model.
`EpochSetup` as written is a stub artifact and **must be revised** into a
multi-round DKG sub-protocol (per-validator round functions + a transcript/state
object), with `KeyShare` staying local to each validator and never aggregated at
one node. The consuming code (Reveal, client Encrypt) is unaffected; the
*interface for key generation* is a redesign, not a swap. Task 1.3 includes this
revision explicitly. Recorded as **D-062**.

---

## Section 4 — The `spend_commitment` requirement (the 0.4 discovery)

SPEC §6.3 / §8(e) and D-021/D-025/D-059 mandate this **before the encrypted Veil
lands**: once `ciphertext` is opaque, the MandateDecorator (the §6.2 Phase-0
centerpiece, the agent-safety chokepoint) cannot read give legs to value spend
intent. Without a plaintext commitment, real encryption blinds the chokepoint and
a stolen key could submit unbounded-value encrypted intents that pass the ceiling
— breaking §1.1 goal 3. Load-bearing.

**(1) Envelope field.** Add to `IntentEnvelope` (always-plaintext routing
metadata):

```protobuf
message IntentEnvelope {
  uint64 ttl_height = 1;
  string fee_bid    = 2;
  bool   allow_partial = 3;
  uint32 chamber_id = 4;
  repeated cosmos.base.v1beta1.Coin spend_commitment = 5
    [(gogoproto.castrepeated) = "github.com/cosmos/cosmos-sdk/types.Coins",
     (gogoproto.nullable) = false];
}
```

`spend_commitment` is the agent's declared **upper bound on give-leg value**, in
the denoms the mandate ceilings use (native + uusdc, per the current
`FaceValuePriceSource`). Validated in `MsgSubmitIntent` envelope validation
(extends D-042) **structurally only**: it must be well-formed, non-negative
`Coins`. **An empty commitment is a valid declaration of zero** — there is no
presence/absence gate and no `ErrMissingSpendCommitment` (see the resolution
note below and **D-065**). The agent's truthfulness is enforced at the Reveal,
not at submission, because the msg server cannot read give legs to cross-check a
commitment once payloads are encrypted — and need not.

**(2) Ante binding — UNIFORM, never reads give legs.** §6.2 step 4 in Phase 0
read plaintext give legs via the PriceSource. From Task 1.1 the
`MandateDecorator` values `MsgSubmitIntent` as **`spend_commitment + bond`**
**always** — identically in stub and encrypted mode, never reading give legs
(D-025 keeps the bond in the sum; the bond is plaintext on `SealedIntent.bond`).
This routes to **`outstanding_commitment`** (D-059 — a refundable lifetime
commitment, not per-period settled spend). The per-tx ceiling and the
window-exposure ceiling (`outstanding_commitment + window_spent`) bind to
`spend_commitment + bond` exactly as they bound `giveLegSpend + bond` before; the
D-059 accounting is untouched. **The uniformity is the whole point:** if the ante
read give legs in stub mode and the commitment in encrypted mode, stub testing
would prove a *different* code path than the one that ships in Task 1.6. Making
the ante's behavior identical across stub/encrypted is precisely what lets stub
testing prove the encrypted-mode chokepoint. The `PriceSource` is therefore
removed from the `MandateDecorator` entirely (the give-leg valuation moves to the
Reveal cross-check, which keeps its own `PriceSource`).

**(3) `charged_spend` persistence + Reveal-time slash.**

- Persist `charged_spend = spend_commitment + bond` (D-041 — every refund path
  returns the stored value via `DecommitIntent`, never recomputes; INVALID still
  persists the charge, D-040). Because the ante values the same `commitment +
  bond`, the ante charge, the persisted `charged_spend`, and the `DecommitIntent`
  release all agree, so the D-059/D-064 commitment-conservation invariant is
  untouched.
- **Bond magnitude is also commitment-derived** (not give-value-derived, a
  behavior change recorded in D-065): the §6.3 face value feeding
  `RequiredBond` is `faceValueScalar(spend_commitment)`. Same uniformity argument
  — the msg server is blind to give legs in encrypted mode, so a give-derived
  bond would size differently in stub mode than the shipping path. Over-declaring
  therefore costs a larger bond (correct self-penalizing incentive).
- At the Reveal (`BeginBlock` decrypt), after recovering the real payload, value
  the actual give legs (`giveLegValue`). **If actual give value > `spend_commitment`
  for any denom → the intent is a commitment violation → INVALID**, 50% bond slash
  to the community pool, exactly like a malformed payload (D-040). The intent
  never reaches OPEN/matching. This makes the declared bound truthful: under-
  declare to dodge the ceiling → slashed, no trade. **This Reveal slash is the
  SOLE enforcement** — the ante cannot cross-check a ciphertext, and an up-front
  reject cannot be done correctly (the proto cannot distinguish absent from zero),
  so enforcement lives at the one place the give legs are readable.

**Edge cases (designed):**

- **Empty `spend_commitment` = a commitment of zero.** No up-front reject (the
  `repeated Coin` proto cannot distinguish absent from zero, and a zero
  declaration is legitimate for a coin-give-free intent). The spam vector an
  up-front reject would have addressed — declare zero, give a large coin amount —
  is already deterred: the bond is charged and 50% slashed at the Reveal when the
  give exceeds the (zero) commitment.
- **Under-declaration is the violation, including by undeclared denom.** The
  comparison iterates the **actual give coins** and checks each against
  `spend_commitment.AmountOf(denom)`, so a give in a denom the commitment does not
  mention compares against 0 and fires — omitting a denom cannot dodge the bound.
- **Over-declaration** (commitment > actual): allowed and self-penalizing
  (consumes more ceiling and a larger bond); **no slash.**
- **`allow_partial`:** the commitment bounds the full give; partial fills are ≤
  full, so still bounded.
- **Unit consistency (inherited boundary).** The Reveal compares per-denom actual
  give amount ≤ per-denom `spend_commitment`, both valued by the same
  `FaceValuePriceSource` the commitment is declared in. **Data/svc give legs value
  0** under `FaceValuePriceSource` (AUDIT F-2/F-3), so a coin-denominated
  commitment does not bound data/svc legs and a coin-give-free intent honestly
  declares zero (opens cleanly, never slashes) — the existing Phase-0 boundary
  (F-3), inherited unchanged. The D-044 PriceSource swap is the proper fix and
  remains a separate, non-Phase-1 task. Documented, not silently expanded.

**Resolution note (supersedes this section's earlier framing — D-065).** The
draft above originally conditioned the mechanism on a per-intent "encrypted"
classification and rejected an absent commitment up front
(`ErrMissingSpendCommitment`). Both were dropped during Task 1.1: there is no
per-intent encrypted signal in stub mode (the `StubScheme` is pure identity), the
classification would split stub-mode from encrypted-mode behavior (defeating the
build-in-stub strategy), and the `repeated Coin` type cannot distinguish "absent"
from "zero" — so an up-front reject cannot be implemented faithfully and is not
needed. The mechanism is **uniform** (always commitment + bond, in both modes)
and **empty = zero, enforced solely at the Reveal**. Recorded loudly in D-065.

**Where it is built:** entirely in **stub mode** — `Encrypt` stays identity while
the field, the uniform ante binding, the commitment-derived bond, and the Reveal
slash are wired and tested. This is why it is an early task (1.1): it de-risks the
chokepoint independent of the crypto, and proves the exact code path that ships in
Task 1.6 against plaintext the Reveal can still cross-check.

---

## Section 5 — Task breakdown

One working session per task, each with a stop-and-review and acceptance
criteria, mirroring SPEC §10. Sequenced by risk and dependency, per §8's guidance
to wrap the library behind the interface and build the adversarial harness
*before* wiring into consensus.

| # | Task | Depends on | Acceptance criteria |
|---|---|---|---|
| **1.0** | **Determinism harness hardening (F-1)** | — | Reproducible per fixed seed vector (byte-identical hash across two invocations); CI target runs vector ×2, exits non-zero on any SKIP/abort/mismatch; 500-block run keeps validator set intact for every seed; D-057 workaround + tolerant ops preserved. **No crypto code.** |
| **1.1** | **`spend_commitment` (§4), stub mode** | 1.0 | Envelope field added; ante binds `spend_commitment+bond` → `outstanding_commitment` (D-059); absent-commitment rejects (`ErrMissingSpendCommitment`); Reveal slashes a commitment violation → INVALID (50%); fuzz under/over-declaration; **all 0.x tests green; `Encrypt` still identity.** |
| **1.2** | **D8 ratification (§1)** | — (parallel-able) | D-060 entry = stay-on-v0.50 with the accurate post-spike flip-condition; fork-site `CONCLAVE-TODO` re-pointed to the Phase-2 boundary; build unchanged green. Docs-only. |
| **1.3** | **BLS library wrap behind `ThresholdScheme` + interface revision (§3), OFFLINE** | 1.0 | Library chosen + recorded (license/maint/audit, D-061); threshold-encryption construction authored on kyber primitives; `Encrypt → PartialReveal → Combine` round-trips `t`-of-`n` in unit tests with a locally generated key set; **a corrupted partial is rejected by the verifiable-share check**; **`Combine` is deterministic**; `EpochSetup` revised to a DKG sub-protocol shape (shares stay node-local, D-062). **Spikes marked, not committed; no consensus wiring.** |
| **1.4** | **Adversarial test harness (§8 churn/withheld/corrupt), OFFLINE** | 1.3 | Proves `t−1` shares cannot decrypt, `t` can; withheld/corrupt shares deterministically excluded; drain-before-rotate resharing across a simulated valset change preserves decryptability of in-flight ciphertexts. No consensus wiring. |
| **1.5** | **DKG epoch lifecycle (§2(b))** — *transport design sub-review BEFORE code* | 1.4 | **STOP #1 (design):** the multi-round DKG message-transport choice (on-chain tx vs VE-injected vs side channel) is reviewed as its own stop before implementation — the highest-risk decision in Phase 1. **Then:** epoch boundary triggered by valset update; `EpochPubKey` published to queryable state (epoch-tagged); per-validator shares held locally; mid-epoch joins wait; drain-before-rotate enforced with CANCEL as a valid drain terminus that releases rotation; multi-round DKG tested in a simulated multi-validator harness. |
| **1.6** | **Vote-extension share transport + Reveal wiring (§2, F-4)** | 1.5 | `ExtendVote` emits verifiable `PartialReveal` shares for the deterministically-enumerated sealed set; B's VEs feed `BeginBlock(B+1)` Combine → OPEN; share re-transport across blocks; retry up to `max_reveal_retries=3` → session CANCEL (bonds refunded, releases epoch rotation), chain never halts; `max_intents_per_session` cap; **determinism harness (1.0) green over the new surface.** |
| **1.7** | **Multi-node adversarial localnet (4 validators, docker-compose)** | 1.6 | A validator running modified code **provably cannot decrypt pre-ordering**; encrypted 3-party cycle clears end-to-end; Reveal-cost benchmark within bound under the per-session cap. (SPEC §10 Phase-1 exit.) |
| **1.8** | **External audit engagement (§8(d), gates mainnet)** | 1.7 | Audit-scope doc (authored construction + DKG + VE transport + order-then-reveal + retry/cancel + verifiable-share exclusion); engagement; findings triaged. Not a code task — budgeted now. |

**Deviations from SPEC §10 Phase-1 numbering (recorded).** §10 lists 1.1
lib+impl, 1.2 DKG+VE, 1.3 adversarial harness, 1.4 multi-node, 1.5 audit. This
plan prepends F-1 (1.0), `spend_commitment` (1.1), and D8 (1.2); splits the
crypto into offline-wrap (1.3) + offline adversarial harness (1.4) **before** DKG
(1.5) and VE consensus wiring (1.6) — moving the adversarial harness ahead of
consensus wiring per §8's explicit "before wiring into consensus" instruction.
Task 1.5 carries an explicit design sub-review (transport choice) as its own stop.

---

## Consolidated FLAGS (SPEC §8 under-specifications, with resolutions)

1. **Threshold *encryption* layer is unbuilt by the named libraries** — authored
   on kyber primitives; the heart of the 1.8 audit (FLAG I, D-061).
2. **`EpochSetup` cannot model a real DKG** — interface revision in 1.3; broader
   than "swap implementation only" (FLAG II, D-062).
3. **DKG protocol + message transport unspecified** — highest-risk; explicit
   design sub-review (Task 1.5 STOP #1) before code (§2(b)).
4. **Epoch key handover** — drain-before-rotate primary, CANCEL is a valid drain
   terminus that releases rotation, grace-window retention as the named fallback
   if 1.5's harness shows liveness stalls (§2(b)).
5. **Retry share re-transport** — validators re-emit shares in each block's VE
   until the session is revealed/cancelled (§2(c)).
6. **Verifiable partial decryptions required** — disqualifying criterion in 1.3;
   the `RevealShare` type carries the proof (FLAG III, §2(c)).
7. **VE size + Reveal cost unbounded in §8** — `max_intents_per_session` cap
   (§2 perf FLAGS, enforced in 1.6, benchmarked in 1.7).

---

*Phase-1 design approved at the Phase-0 → Phase-1 boundary (baseline `9ea63b2`).
Task 1.0 (determinism harness) begins the Phase-1 build in a fresh session. The
conclave concludes.*
