# CONCLAVE

## Final Architecture & Build Specification v1.0

**Purpose of this document:** This is the single authoritative reference for building Conclave. It is written to be consumed both by humans and by AI coding agents (Claude Code) working on a local development machine. Every module has explicit state schemas, message types, validation rules, and acceptance criteria. When implementing, follow this document's naming, key layout, and phase ordering exactly. Where the document says STUB, implement the interface with a deterministic fake and leave a `// CONCLAVE-TODO(phase-N)` marker.

**One-line definition:** Conclave is a Layer-1 blockchain whose transaction layer is a consensus-native sealed-bid exchange for autonomous agents — agents submit Sealed Intents under Mandates, Sessions convene behind the Veil, and the conclave Concludes atomically.

---

## 0. Naming Canon (use these names everywhere — code, docs, protos, CLI)

| Concept | Canonical name | Code identifier | Replaces (never use) |
|---|---|---|---|
| The chain | Conclave | `conclave` | BlockXNet, x402 chain |
| Native staking/gas token | CLV | `uclv` (base denom, 10^6 uclv = 1 CLV) | x402 token |
| Agent identity layer | Sigil | `x/sigil` | xDID |
| Agent policy accounts | Mandate | `x/mandate` | — (name retained) |
| Encrypted intent mempool | The Veil | `x/veil` | Dark Intent Pool, darkpool |
| Pre-trade order object | Sealed Intent | `SealedIntent` | Intent Packet |
| Batch clearing engine | Session | `x/session` | x/clearing, batch auction module |
| N-party atomic settlement op | CONCLUDE | `MsgConclude` / opcode `CONCLUDE` | SWAP_ATOMIC, SETTLE_GRAPH |
| Delivery escrow | Escrow | `x/escrow` | — |
| Settlement receipts | Receipt | emitted by `x/session`, queried via `x/receipt` | — |
| Smart contract runtime | CVM (Conclave Virtual Machine) | `cvm` | xVM |
| Industry shards (Phase 3) | Chambers | `chamber` | Zone Shards |
| Chain ID (local dev) | `conclave-dev-1` | — | blockx_19077-1 |
| Bech32 prefix | `conc` | accounts `conc1...`, validators `concvaloper1...` | — |

Solver, validator, observer, and relayer keep their generic names. The act of threshold decryption at session open is called **the Reveal** in docs and comments.

---

## 1. Goals and Non-Goals

### 1.1 Goals (in priority order)

1. **Structural MEV elimination for agent trading.** No participant — validator, solver, or observing agent — can profit from seeing intent flow before ordering is fixed. Achieved by the Veil (order-then-reveal invariant) + Sessions (uniform-price batch clearing where latency buys nothing).
2. **N-party atomic commerce.** A single CONCLUDE commits an arbitrary directed graph of transfers (tokens, escrowed data-access grants, service vouchers) or reverts entirely. Circular trades (A→B→C→A) clear natively in one block.
3. **Consensus-enforced agent safety.** Every agent transaction is checked against an on-chain Mandate in the ante-handler, *before* execution. A leaked session key is bounded by its Mandate, never catastrophic.
4. **Per-agent parallel throughput.** One agent can keep ≥50 transactions in flight via unordered transactions (ADR-070 pattern) and laned blocks; a stuck tx never serializes an agent.
5. **Provable commerce.** Asset-for-data trades carry enforceable delivery conditions; every settlement emits a light-client-provable Receipt that feeds Sigil reputation and is IBC-portable.
6. **Ecosystem inheritance, not bootstrap.** Edge services speak x402 (payments in) and ERC-7683 (solver supply in) so Conclave plugs into existing agent-economy rails.

### 1.2 Non-Goals (explicitly out of scope; do not build)

- General-purpose DeFi for humans (AMMs, lending). Conclave is an agent exchange first.
- EVM compatibility in Phase 0–2. CVM is WASM (CosmWasm); EVM is a Phase-3+ question.
- Production threshold cryptography in Phase 0. The Veil ships as a stubbed interface first (see §8).
- Privacy of *settled* state. Post-Reveal, everything is transparent. Conclave hides pre-trade intent, not post-trade facts.
- Chambers (sharding). Phase 3 only. However, all IDs and protos must be chamber-aware from day one (every key includes a `chamber_id`, fixed to `0` until Phase 3).

### 1.3 Success criteria for the local build (what "done" means for Claude Code)

A single-node local devnet on WSL2 Ubuntu where the following demo script passes end-to-end:

1. Operator key registers a Sigil for `agent-alice` and `agent-bob` and `agent-carol`.
2. Operator issues a Mandate to an ephemeral session key for alice: max 500 USDC-equivalent/day, allowed msgs = `[MsgSubmitIntent]`, expiry +7 days.
3. Alice's session key submits an intent exceeding the ceiling → **rejected in ante-handler with `ErrMandateExceeded`**, no gas beyond ante.
4. Alice, Bob, Carol submit three intents forming a cycle (alice: 100 USDC → wants dataset D; bob: dataset D → wants 10 CMPT compute voucher; carol: 10 CMPT → wants 100 USDC).
5. A registered, bonded solver submits a solution; the Session scores it, and one `MsgConclude` settles the 3-leg cycle atomically in one block.
6. A Receipt is queryable for each party; alice's Sigil reputation increments.
7. Killing the solver process mid-session → fallback solver clears the batch (degraded prices, but liveness holds).

---

## 2. System Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│  EDGE SERVICES (off-chain, separate repos, Phase 2)             │
│  conclave-x402-gateway      conclave-7683-resolver              │
└───────────────┬─────────────────────────┬───────────────────────┘
                │ gRPC/REST               │ gRPC
┌───────────────▼─────────────────────────▼───────────────────────┐
│  CONCLAVE NODE (this repo: `conclaved`)                          │
│                                                                  │
│  ABCI++ layer                                                    │
│   ├─ Laned mempool (Block SDK): veil│session│commerce│ibc│general│
│   ├─ PrepareProposal / ProcessProposal (lane + reveal invariants)│
│   └─ Vote extensions (Phase 1: Reveal shares)                   │
│                                                                  │
│  Ante pipeline                                                   │
│   ├─ standard SDK decorators                                     │
│   ├─ UnorderedTxDecorator (ADR-070 pattern, hardened)            │
│   ├─ MandateDecorator  ◄── the agent-safety chokepoint           │
│   └─ FeeAbstractionDecorator (pay gas in USDC via conversion)    │
│                                                                  │
│  Modules (msg servers + keepers)                                 │
│   x/sigil   x/mandate   x/veil   x/session   x/escrow  x/receipt│
│   + standard: auth authz feegrant bank staking slashing gov ibc  │
│   + cvm (CosmWasm) exposing CONCLUDE + escrow bindings           │
│                                                                  │
│  Consensus: CometBFT (single-slot finality config)               │
└──────────────────────────────────────────────────────────────────┘
```

**The lifecycle in one paragraph.** An agent (a key bound to a Sigil, constrained by a Mandate) submits a `SealedIntent` into the veil lane. At session boundary `k` blocks, ordering of ciphertexts is finalized, then the Reveal exposes plaintext intents (Phase 0: "encryption" is a stub; ordering-before-reading is still enforced). Bonded solvers read the open Session, compute a matching over the intent set, and submit `MsgProposeSolution`. The session keeper deterministically scores solutions by delivered surplus, selects the winner, and executes a single `MsgConclude` that atomically applies the full transfer graph, instantiates any Escrows for conditioned legs, emits Receipts, updates Sigil reputation, pays the solver, and refunds/rolls unmatched intents.

---

## 3. Technology Stack (pin these versions at scaffold time)

| Layer | Choice | Notes for Claude Code |
|---|---|---|
| Language | Go ≥ 1.22 | gofmt + golangci-lint enforced |
| Framework | Cosmos SDK v0.50.x | use depinject + module wiring per v0.50 conventions |
| Consensus | CometBFT v0.38.x | `timeout_commit` tuned low for dev (see §12) |
| Lanes | skip-mev/block-sdk (compatible with SDK ≥ v0.47) | vendor it; we add two custom lanes |
| Contracts | CosmWasm (wasmd) — integrate Phase 2 | expose CONCLUDE via custom bindings |
| Protos | buf + gogoproto | all protos under `proto/conclave/{module}/v1/` |
| Crypto (Phase 1) | Ed25519 (txs), BLS12-381 (Reveal shares, aggregate sigs) | Phase 0: interfaces only |
| ZK (Phase 2) | groth16 via gnark for delivery proofs | Phase 0–1: condition class disabled |
| Stable settlement asset (dev) | mock `uusdc` minted by a faucet module account | replaced by IBC/bridged USDC later |
| Testing | Go unit + SDK simulation + property-based fuzz (rapid) | fuzz targets listed per module |

---

## 4. Repository Layout

```
conclave/
├── app/                      # app wiring, lanes, ante pipeline, upgrades
│   ├── app.go
│   ├── ante.go               # ordered decorator chain (see §6.4)
│   └── lanes.go              # lane definitions + block space shares
├── cmd/conclaved/            # daemon entrypoint
├── proto/conclave/
│   ├── sigil/v1/
│   ├── mandate/v1/
│   ├── veil/v1/
│   ├── session/v1/
│   ├── escrow/v1/
│   └── receipt/v1/
├── x/sigil/
├── x/mandate/
├── x/veil/
├── x/session/
├── x/escrow/
├── x/receipt/                # query-side module over receipts (no msgs)
├── crypto/reveal/            # ThresholdScheme interface + stub + (P1) BLS impl
├── solver/                   # reference solver binary `conclave-solver`
│   ├── naive/                # greedy pairwise matcher (fallback logic mirror)
│   └── cycles/               # Bellman-Ford-style cycle finder for graphs
├── scripts/
│   ├── localnet.sh           # init + start single node, fund test accounts
│   ├── demo_e2e.sh           # the §1.3 acceptance script
│   └── protocgen.sh
├── tests/
│   ├── e2e/                  # in-process app tests of full flows
│   └── fuzz/                 # rapid-based property tests
└── docs/                     # this file lives at docs/SPEC.md
```

Module-internal layout follows SDK convention: `keeper/`, `types/`, `module.go`, `abci.go` (where EndBlock logic exists), `client/cli/`.

---

## 5. Core Domain Model

### 5.1 The Sealed Intent

The atomic unit of the system. Proto sketch (authoritative field list — implement exactly):

```protobuf
// proto/conclave/veil/v1/intent.proto
message SealedIntent {
  string  id              = 1;  // hash(sender_sigil, nonce, chamber) hex
  uint32  chamber_id      = 2;  // fixed 0 until Phase 3
  string  sender_sigil    = 3;  // sigil ID, not raw address
  bytes   ciphertext      = 4;  // Phase 0: plaintext payload bytes (stub)
  bytes   envelope        = 5;  // ALWAYS plaintext: routing metadata
  cosmos.base.v1beta1.Coin bond = 6;  // anti-spam bond in uclv
  uint64  submit_height   = 7;
}

message IntentEnvelope {            // serialized into SealedIntent.envelope
  uint64  ttl_height      = 1;      // expiry; intent rolls over until then
  string  fee_bid         = 2;      // priority within veil lane
  bool    allow_partial   = 3;
  uint32  chamber_id      = 4;
}

message IntentPayload {             // the encrypted part (plaintext in P0)
  repeated IntentLeg give = 1;      // what I offer
  repeated IntentLeg want = 2;      // what I require (limit terms)
  DeliveryCondition condition = 3;  // optional; for data/service legs
  string  counterparty_class = 4;   // CEL predicate, e.g. "reputation >= 50"
}

message IntentLeg {
  oneof asset {
    cosmos.base.v1beta1.Coin coin   = 1;   // fungible
    DataGrant                data   = 2;   // content-hash + access URI commit
    ServiceVoucher           svc    = 3;   // typed voucher (e.g. CMPT units)
  }
  string limit = 4;  // max-give / min-want as decimal string
}
```

### 5.2 Intent lifecycle state machine

```
SUBMITTED ──(session boundary, ordering fixed)──► SEALED_IN_SESSION
SEALED_IN_SESSION ──(Reveal)──► OPEN
OPEN ──(matched in winning solution)──► CONCLUDED      → bond refunded
OPEN ──(unmatched, ttl not reached)──► ROLLED          → next session
OPEN ──(unmatched, ttl reached)──► EXPIRED             → bond refunded
SEALED_IN_SESSION ──(Reveal fails / malformed)──► INVALID → bond 50% slashed
any pre-OPEN state ──(MsgCancelIntent by owner)──► CANCELLED → bond refunded
```

State transitions happen only in `x/session` EndBlock or via explicit Msgs. Persist current state with the intent record; index by `(state, ttl_height)` for efficient expiry sweeps.

### 5.3 Store key conventions (all modules)

Single-byte prefix per object type within each module store; big-endian fixed-width encoding for numeric key components; every keyed object includes `chamber_id` as the first component after the prefix (always `0x00000000` for now). Example for veil: `0x01 | chamber(4) | intent_id(32)` → SealedIntent; `0x02 | state(1) | ttl(8) | intent_id(32)` → expiry index.

---

## 6. Module Specifications

Build order within Phase 0: `x/sigil` → `x/mandate` → `x/session` (plaintext) → `x/veil` (stub) → `x/receipt`. `x/escrow` is Phase 2 but its proto types are defined now so CONCLUDE legs can reference them.

### 6.1 x/sigil — Agent Identity

**Responsibility:** registry of agent identities (W3C-DID-compatible document roots), verifiable-credential anchors, key management (root key, recovery set), and the reputation score.

**State:**
- `Sigil{ id, controller_addr, did_doc_hash, metadata_uri, recovery_set []addr, recovery_threshold, created_height }` — key `0x01|chamber|sigil_id`
- `CredentialAnchor{ sigil_id, issuer_sigil, schema_id, claim_hash, expiry, revoked }` — key `0x02|chamber|sigil_id|cred_id`
- `Reputation{ sigil_id, score (Dec), last_decay_height, concluded_count, disputed_count }` — key `0x03|chamber|sigil_id`
- Reverse index `addr → sigil_id` (`0x04`), including delegate keys registered via Mandates.

**Msgs:** `MsgRegisterSigil`, `MsgRotateController` (requires recovery_threshold sigs from recovery_set OR current controller), `MsgAnchorCredential`, `MsgRevokeCredential` (issuer only).

**Reputation math (deterministic, no oracle):** on every Receipt emission, `score += base_gain * value_weight(settled_value)` where `value_weight = min(1, log10(value_usdc)/4)`. On every lost escrow dispute, `score -= penalty` (penalty = 10× base_gain). Decay: lazily applied on read/write — `score *= decay_factor ^ blocks_since_last_decay/decay_period`. All params (`base_gain=1.0`, `decay_factor=0.995`, `decay_period=daily blocks`) live in module params, governable.

**Invariants (register with crisis module):** every Mandate's agent key resolves to exactly one Sigil; reputation never negative; revoked credentials never satisfy counterparty-class predicates.

**Fuzz targets:** key rotation under concurrent recovery attempts; decay arithmetic over extreme block gaps (no overflow/underflow with sdk.Dec).

### 6.2 x/mandate — Agent Policy Accounts (THE PHASE-0 CENTERPIECE)

**Responsibility:** consensus-enforced spending and capability policy, checked in the ante-handler before any message from an agent key executes. Architecturally a superset of x/authz + x/feegrant semantics, but implemented as its own module with its own store — do NOT shell out to authz at runtime (one store read in ante, not three).

**State:**
- `Mandate{ id, sigil_id, grantee_addr (the agent/session key), allowed_msg_types []string (TypeURLs), spend_ceiling_per_tx Coins, spend_ceiling_window Coins, window_blocks, window_spent Coins, window_start_height, counterparty_class (CEL string, may be ""), fee_sponsor addr (may be ""), fee_denoms []string, expiry_height, revoked bool }` — key `0x01|chamber|grantee_addr|mandate_id`
- Per-grantee active-mandate index for O(1) ante lookup: `0x02|grantee_addr → mandate_id` (one active Mandate per key; issuing a new one to the same key replaces it — simpler and safer than stacking).

**Msgs:** `MsgIssueMandate` (signer must be sigil controller), `MsgRevokeMandate` (controller; takes effect immediately — next block), `MsgAmendMandate` (controller; full replacement semantics, never in-place mutation of ceilings).

**Ante enforcement — `MandateDecorator` (exact algorithm):**
1. For each signer of the tx: if signer has no Mandate index entry, they are a root/controller key → pass through (root keys are governed by normal account security, not Mandates).
2. If a Mandate exists: reject if `revoked || current_height >= expiry_height` → `ErrMandateExpired`.
3. For every msg in the tx: msg TypeURL must be in `allowed_msg_types`, else `ErrMsgNotPermitted`. **Nested msgs matter:** if the msg is `MsgExec` (authz) or any wrapper, recursively unwrap and check inner TypeURLs. Never allow `MsgIssueMandate`/`MsgRevokeMandate` themselves to be mandate-permitted (privilege escalation guard — enforce in `MsgIssueMandate` validation).
4. Compute the tx's *spend intent*: for `MsgSubmitIntent`, the max of `give` legs valued via the session module's last clearing prices (conservative: if no price, treat limit as face value); for bank sends, the send amount. Reject if > `spend_ceiling_per_tx` → `ErrMandateExceeded`.
5. Window accounting: if `current_height >= window_start_height + window_blocks`, reset `window_spent = 0`, `window_start_height = current_height`. Then if `window_spent + spend_intent > spend_ceiling_window` → `ErrMandateExceeded`. **Charge `window_spent` in ante (CheckTx AND DeliverTx)**; refund the delta in the session EndBlock if the intent later expires/cancels unmatched. Charging optimistically is what makes a stolen key safe even across many parallel unordered txs in one block.
6. Counterparty-class predicates are NOT checked in ante (counterparty unknown pre-match); they are enforced by `x/session` at solution validation (§6.4).

**Fee abstraction — `FeeAbstractionDecorator`:** runs after MandateDecorator. If tx fee denom ∈ mandate `fee_denoms` and ≠ `uclv`, convert at the module's posted conversion rate (Phase 0: fixed param `uusdc:uclv`; Phase 2: TWAP from session clearing prices) and deduct from `fee_sponsor` if set, else from grantee. Internally mints/burns nothing — the module holds a CLV float funded at genesis (param: `fee_pool`).

**Events:** `mandate_issued`, `mandate_enforced` (denied txs emit this in CheckTx logs for operator observability), `mandate_window_reset`.

**Fuzz targets:** window reset boundary off-by-ones; concurrent unordered txs draining a window in one block (must never exceed ceiling); nested-msg unwrap depth (cap recursion at 8, reject deeper → `ErrMsgTooNested`).

**Acceptance test:** §1.3 steps 2–3 exactly.

### 6.3 x/veil — Sealed Intents and the Reveal

**Responsibility:** accept SealedIntents, hold them with bonds, enforce the order-then-reveal invariant, hand revealed intents to x/session.

**Phase 0 (STUB mode — build this now):** `ciphertext` carries plaintext `IntentPayload` bytes; the `ThresholdScheme` interface is satisfied by `StubScheme` whose `Reveal()` is the identity function. **The invariant is still real in stub mode:** ProcessProposal rejects any block where a session's solution txs reference intents whose ordering wasn't fixed in a strictly earlier block. This means the entire pipeline, all state machines, and all tests are built against the true invariant from day one; Phase 1 swaps the scheme implementation only.

**ThresholdScheme interface (crypto/reveal/):**
```go
type ThresholdScheme interface {
    EpochSetup(valset []Validator) (EpochPubKey, []KeyShare, error) // DKG (P1)
    Encrypt(epk EpochPubKey, payload []byte) ([]byte, error)        // client side
    PartialReveal(share KeyShare, ct []byte) (RevealShare, error)   // per validator
    Combine(shares []RevealShare, ct []byte) ([]byte, error)        // t-of-n
    Threshold(n int) int                                            // = 2n/3+1
}
```

**Msgs:** `MsgSubmitIntent` (validates envelope, escrows bond, assigns id, stores SUBMITTED), `MsgCancelIntent` (owner; only pre-OPEN states).

**Bond rules:** `bond = max(min_bond_param, bond_bps * face_value)` — validated in msg server. Refund on CONCLUDED/EXPIRED/CANCELLED; 50% slash to community pool on INVALID (malformed payload at Reveal). Phase 0 stub: payload that fails proto-unmarshal at Reveal = INVALID — this path must be tested even in stub mode.

**ABCI hooks:** in `PrepareProposal`, veil-lane txs are ordered by `fee_bid` then submit order; in EndBlock at session boundary, all SUBMITTED intents transition to SEALED_IN_SESSION with the session id. The Reveal runs at the START of the next block's BeginBlock (P0: instant; P1: from vote-extension shares of the boundary block), transitioning to OPEN.

**Fuzz targets:** malformed payloads at Reveal never panic the chain (recover → INVALID); cancel/boundary races.

### 6.4 x/session — Batch Clearing and CONCLUDE

**Responsibility:** session lifecycle, solver registry/bonding, solution scoring, atomic graph settlement, receipt emission, rollover/expiry sweeps.

**Params:** `session_length_blocks` (dev default: 3), `solution_window_blocks` (dev: 2), `solver_min_bond` (uclv), `solver_fee_bps` (of settled surplus), `max_graph_legs` (256), `max_solutions_per_session` (16).

**Session lifecycle (per chamber):**
```
FORMING (collecting OPEN intents)
  → AUCTION (solution_window: bonded solvers submit MsgProposeSolution)
  → SCORING (EndBlock: deterministic winner selection)
  → CONCLUDING (winner's graph executed via internal Conclude)
  → CLOSED (receipts emitted, bonds settled, unmatched intents rolled)
If zero valid solutions by SCORING → FALLBACK: the in-protocol naive matcher
(greedy pairwise by price-time within the revealed set) runs in EndBlock.
The fallback is part of consensus — every node computes it identically.
```

**Solver registry:** `Solver{ addr, bond Coins, status, slashed_count, lifetime_surplus }`. `MsgRegisterSolver` (bond ≥ min), `MsgUnbondSolver` (unbonding period = 1 day of blocks). Slashing: invalid solution (fails validation) = 5% bond; equivocation (two solutions same session) = 20%.

**MsgProposeSolution:** `{ session_id, matches: []Match }` where `Match{ intent_ids []string, legs []SettleLeg, clearing_prices []PricePoint }`. Validation (ALL must hold or the solution is rejected and solver slashed 5%):
1. Every referenced intent is OPEN in this session; no intent in two matches.
2. **Conservation per match:** for every asset, sum(outflows) == sum(inflows) across legs. No value created or destroyed; solver fee is computed by the protocol, never embedded in legs.
3. **Limit satisfaction:** every party receives ≥ its `want` limits and gives ≤ its `give` limits at the stated clearing prices; partial fills only where `allow_partial`.
4. **Uniform price:** within one match, the same asset pair clears at one price for all parties.
5. **Counterparty class:** every pairing satisfies both sides' CEL predicates evaluated against Sigil state (reputation/credentials) at session-open height.
6. Graph size ≤ `max_graph_legs`; legs with DeliveryConditions reference valid condition specs.

**Scoring (deterministic):** `score = Σ over matched intents of surplus(intent)` where surplus = distance between clearing terms and the party's limit, normalized to uusdc via clearing prices. Ties → lower solution tx hash wins. Winner's solver receives `solver_fee_bps` of total surplus, paid from a protocol fee pool funded by a small skim (param) on settled value.

**Conclude execution (internal; also exposed as `MsgConclude` restricted to the module authority for CVM/testing):** within one cache-wrapped store context: apply every leg (bank sends via module escrow account; DataGrant legs instantiate `x/escrow` entries instead of transferring; ServiceVoucher legs mint voucher tokens denom `svc/{schema}`), emit one Receipt per intent, update reputations, refund bonds, mark CONCLUDED. Any leg failure → write nothing, slash solver 5%, run FALLBACK. **This whole block is the atomicity guarantee — implement with `ctx.CacheContext()` and a single `write()`.**

**EndBlock order of operations (must be exactly):** (1) advance session states, (2) Reveal hand-off intake, (3) scoring + conclude, (4) fallback if needed, (5) rollover sweep, (6) expiry sweep, (7) mandate window refunds for expired/cancelled intents.

**Fuzz targets:** conservation checker vs adversarial graphs (cycles, self-trades, duplicate legs); fallback determinism across simulated nodes; rollover under ttl edge cases.

### 6.5 x/escrow — Delivery Conditions (protos now, logic Phase 2)

**Condition classes:** `HASHLOCK{ payload_hash, timeout_blocks }` — buyer releases by presenting preimage receipt-of-access, auto-refund at timeout. `OPTIMISTIC{ dispute_window_blocks }` — funds release after window unless `MsgRaiseDispute`; dispute resolution Phase 2 = simple challenge with seller bond (slash on proven non-delivery via hash mismatch), NOT subjective arbitration. `ZK_DELIVERY{ schema_commitment, vk_id }` — seller submits groth16 proof that ciphertext delivered off-chain matches the committed schema/hash; verify on-chain via gnark verifier (Phase 2). 

**Msgs (Phase 2):** `MsgReleaseEscrow`, `MsgRaiseDispute`, `MsgSubmitDeliveryProof`. **Phase 0 behavior:** CONCLUDE legs carrying conditions other than `NONE` are rejected at solution validation with `ErrConditionsNotEnabled`.

### 6.6 x/receipt — Provable Settlement Records

Receipts are written by x/session; this module is the query/proof surface. `Receipt{ id, session_id, intent_id, sigil_id, counterparty_sigils, terms_hash, clearing_prices, condition_outcome, height }` keyed `0x01|chamber|sigil_id|height|receipt_id` (per-agent chronological scans) with a secondary `0x02|receipt_id` lookup. Receipts live in IAVL state, so ICS-23 existence proofs come free → any IBC counterparty chain can verify a Receipt against a Conclave consensus root. gRPC: `Receipts(sigil_id, pagination)`, `Receipt(id)`, `ReceiptProof(id)`.

### 6.7 Lanes and Unordered Transactions (app-level, not a module)

**Lanes (app/lanes.go), priority order and max block space:** `veil` 30% (MsgSubmitIntent/Cancel only) → `session` 25% (solver + conclude msgs) → `commerce` 15% (sigil/mandate/escrow) → `ibc` 15% → `general` 15% (default lane). Each lane has independent fee logic; veil lane orders by envelope `fee_bid`. Lane membership decided by a `match(tx)` predicate on msg TypeURLs — a tx mixing lanes routes to the lowest-priority matching lane (document this for client devs).

**Unordered txs:** implement the ADR-070 pattern: txs flagged `unordered=true` carry `timeout_timestamp` instead of relying on sequence; replay protection = on-chain TTL'd dedup cache of tx hashes (`0x0F|expiry|txhash`, swept in EndBlock). **Known upstream bug class: sequence still being checked for unordered txs — our conformance suite must include the exact reproduction: submit unordered txs with deliberately wrong sequence numbers; they MUST succeed.** Cap unordered TTL at 10 minutes of blocks (state-growth guard).


---

## 7. End-to-End Transaction Flow (reference sequence)

```
Block N      alice's session key (Mandate-bound) → MsgSubmitIntent
             ante: Mandate check (ceiling charged) → veil lane → SUBMITTED
Block N+2    session boundary: intents SEALED_IN_SESSION s_42 (ordering FIXED)
Block N+3    BeginBlock: the Reveal → intents OPEN; session s_42 → AUCTION
             solvers (off-chain) read open session via gRPC, compute matching
Block N+4    solver → MsgProposeSolution (session lane)
Block N+5    EndBlock: SCORING → winner → Conclude (atomic graph)
             → Receipts emitted, reputation updated, bonds refunded,
             solver paid, unmatched intents ROLLED into s_43
```

Worst-case latency intent→settlement with dev params: 5 blocks (~5s at 1s blocks). Tighten `session_length_blocks`/`solution_window_blocks` after benchmarking; the design goal is sub-3-block settlement on mainnet hardware.

---

## 8. Cryptography Plan (the Veil, Phase 1)

Phase 0 ships `StubScheme` (identity). Phase 1 replaces it with: **BLS12-381-based threshold encryption** — epoch DKG among the validator set at each epoch boundary (reuse CometBFT validator updates as the trigger); intents encrypted client-side to the epoch public key; at session boundary, each validator includes its `PartialReveal` share in its **vote extension** for the boundary block; any node combines `t = ⌊2n/3⌋+1` shares in the next BeginBlock to decrypt.

Hard requirements: (a) ordering is fixed strictly before any honest node can decrypt — enforced because shares only exist in vote extensions of the block that fixed ordering; (b) DKG handles validator churn: epochs are short (1 day), and mid-epoch joins simply wait for next epoch; (c) liveness degradation: if Combine fails (insufficient shares) the session re-runs the Reveal the following block, up to `max_reveal_retries` (3), after which the session CANCELs and bonds refund — the chain never halts on crypto failure; (d) **external audit of this component is a gating milestone before any mainnet** — budget it.

Claude Code guidance: do NOT implement DKG from scratch ad hoc. Phase 1 begins with an evaluation task: assess existing Go BLS threshold libraries (drand/kyber lineage) for license + maintenance, wrap behind `ThresholdScheme`, and write the adversarial test harness (withheld shares, corrupted shares, churn) before wiring into consensus.

---

## 9. Edge Services (separate repos, Phase 2 — specified here for completeness)

**conclave-x402-gateway:** an x402 facilitator. Speaks Coinbase's x402 HTTP handshake outward (402 → X-PAYMENT header → verify → X-PAYMENT-RESPONSE); inward, it submits intents/transfers on Conclave on behalf of the paying agent and fronts gas (gasless for clients), settling in `uusdc`. Stateless except for a hot wallet + replay-nonce store. Outcome: any x402-speaking agent (Visa TAP / Stripe ACP / AWS Bedrock ecosystems) can pay Conclave-native agents without knowing Conclave exists.

**conclave-7683-resolver:** consumes ERC-7683 orders from EVM origin chains, translates them into SealedIntents (resolver pattern — 7683 standardizes how solvers consume orders, not the settlement venue), and posts fills back via the verification bridge configured per origin chain. Outcome: existing 7683 solver firms can fill into Conclave sessions with one integration.

Both edges are clients of the node's gRPC; neither holds consensus-critical logic.

---

## 10. Phased Build Plan (Claude Code task breakdown)

### Phase 0 — local devnet (target: §1.3 demo passing)

| # | Task | Acceptance |
|---|---|---|
| 0.1 | Scaffold chain (SDK v0.50, CometBFT, bech32 `conc`, denom `uclv`, chain-id `conclave-dev-1`), `scripts/localnet.sh` | node produces blocks; faucet funds accounts incl. `uusdc` |
| 0.2 | Protos for all six modules; buf generate clean | `make proto-gen` idempotent |
| 0.3 | `x/sigil` full | register/rotate/anchor + reputation unit tests green |
| 0.4 | `x/mandate` full incl. MandateDecorator + FeeAbstractionDecorator | §1.3 steps 2–3; fuzz suite green |
| 0.5 | Unordered txs + dedup cache + conformance suite (incl. wrong-sequence reproduction test) | 50 parallel unordered txs from one key land in one block |
| 0.6 | Block SDK lanes wired | lane routing integration test |
| 0.7 | `x/veil` stub mode | lifecycle tests incl. INVALID slash path |
| 0.8 | `x/session`: lifecycle, solver registry, validation, scoring, Conclude, fallback, sweeps | 3-party cycle test; fallback-on-no-solver test |
| 0.9 | `x/receipt` + reputation hook | receipt query + ICS-23 proof test |
| 0.10 | Reference solver binary (naive + cycles) | demo_e2e.sh fully green |
| 0.11 | SDK simulation integration (all modules) | 500-block sim, no invariant breaks |

### Phase 1 — the Veil for real
1.1 Library evaluation + `ThresholdScheme` BLS implementation behind the existing interface. 1.2 DKG epoch lifecycle + vote-extension share transport. 1.3 Adversarial harness (withheld/corrupt shares, churn, retry/cancel paths). 1.4 Multi-node localnet (4 validators, docker-compose) proving order-then-reveal under an adversarial proposer. 1.5 External audit engagement. Exit: a validator running modified code provably cannot decrypt pre-ordering; 3-party cycle clears end-to-end encrypted.

### Phase 2 — provable commerce + edges
2.1 `x/escrow` HASHLOCK → OPTIMISTIC → ZK_DELIVERY (gnark verifier; circuit: payload matches committed hash+schema). 2.2 CosmWasm integration with CONCLUDE + escrow custom bindings. 2.3 x402 gateway. 2.4 7683 resolver. 2.5 Real USDC via IBC; replace fixed fee-conversion rate with session TWAP. Exit: §1.3 demo extended — external x402 agent buys ZK-verified data from a Conclave agent; receipt verified on a second Cosmos chain over IBC.

### Phase 3 — Chambers
Activate chamber_id > 0; cross-chamber CONCLUDE via two-phase commit over async chamber messaging (design doc required first — this is research-grade; do not improvise).

---

## 11. Testing Strategy (non-negotiable rules)

1. Every keeper function: table-driven unit tests, happy + ≥2 failure paths.
2. Every state machine (intent, session, mandate window, escrow): an explicit transition-table test asserting illegal transitions are impossible.
3. Property tests (rapid) for: conservation (no CONCLUDE creates/destroys value), mandate ceilings (no interleaving of txs exceeds a window), determinism (fallback matcher + scoring produce identical results across 1000 random seeds on two simulated nodes).
4. The §1.3 demo script runs in CI against a fresh localnet on every merge to main.
5. Chain must never panic on user input: all Reveal/unmarshal paths wrapped with recover→INVALID. A fuzz finding that panics the node is a release blocker.
6. Determinism rule for Claude Code: no map iteration into state writes, no wall-clock time, no floats — sdk.Dec/math.Int only. golangci-lint config must include the standard Cosmos determinism linters.

---

## 12. Local Development Environment (WSL2 Ubuntu)

```bash
# prerequisites
go 1.22+, make, gcc, jq, buf, docker (for multi-node P1 only)

# bootstrap
git clone <repo> conclave && cd conclave
make install                      # builds conclaved into $GOPATH/bin
./scripts/localnet.sh             # init conclave-dev-1, genesis with:
                                  #  - 1 validator (self-delegated 1M CLV)
                                  #  - test accounts: operator, alice, bob, carol,
                                  #    solver1 (each 10k CLV + 10k USDC mock)
                                  #  - fast blocks: timeout_commit = 1s
./scripts/demo_e2e.sh             # the §1.3 acceptance script
```

`localnet.sh` must set: `minimum-gas-prices = "0uclv"` (dev), API + gRPC enabled, all module params to the dev defaults named in §6. Genesis includes one pre-registered solver so the demo can also exercise the registered-solver path before testing fallback.

---

## 13. Conventions for AI-Assisted Implementation

- **Source of truth:** this document. If code and spec conflict, fix the code or propose a spec PR — never silently diverge.
- One module per PR-sized unit; each task in §10's table is one working session.
- Error registry per module in `types/errors.go` with stable codespaces (`sigil`=11xx, `mandate`=12xx, `veil`=13xx, `session`=14xx, `escrow`=15xx).
- Every exported keeper method gets a doc comment referencing the spec section it implements (e.g. `// implements SPEC §6.2 step 5`).
- All TODOs as `// CONCLAVE-TODO(phase-N): ...` — greppable.
- Never use legacy names (BlockXNet, x402-as-chain-name, xDID, Intent Packet, SWAP_ATOMIC, darkpool) anywhere in code, comments, or docs.
- Run `make lint test` before declaring any task complete; demo_e2e.sh before closing any Phase-0 milestone.

---

## 14. Open Decisions Log (resolve before the affected phase)

| # | Decision | Default if unresolved | Phase |
|---|---|---|---|
| D1 | Session cadence: every block vs every k | k=3 dev, k=1 target after bench | 0 |
| D2 | One-active-Mandate-per-key vs stacking | one active (replace semantics) | 0 |
| D3 | Solver fee source: surplus skim vs taker fee | surplus skim, `solver_fee_bps=500` | 0 |
| D4 | BLS threshold library | evaluate; drand/kyber lineage first | 1 |
| D5 | Optimistic dispute arbitration beyond hash-mismatch | hash-mismatch only; subjective disputes out of scope | 2 |
| D6 | CLV tokenomics (inflation, bond sizes at real prices) | dev placeholders; needs economic modeling | pre-testnet |
| D7 | Public naming: trademark/domain clearance for "Conclave" | required before any external publication | pre-announce |

---

## 15. Glossary

**Sealed Intent** — an agent's encrypted pre-trade order (give/want legs, limits, conditions, counterparty class). **The Veil** — the encrypted mempool holding Sealed Intents; ordering is fixed before contents are readable. **The Reveal** — threshold decryption of a session's intents after ordering is final. **Session** — one batch-auction cycle: form → auction → score → conclude. **CONCLUDE** — the n-party atomic settlement operation committing a whole transfer graph or nothing. **Mandate** — an on-chain, ante-enforced policy bounding what an agent key may do and spend. **Sigil** — an agent's on-chain identity: DID root, credentials, reputation. **Receipt** — a light-client-provable record of a settled intent. **Solver** — a bonded off-chain matcher competing to clear sessions. **Fallback** — the in-consensus naive matcher guaranteeing liveness. **Chamber** — an industry-scoped shard (Phase 3). **CLV** — the native staking/bonding/gas token (base denom `uclv`).

---

*Conclave Architecture & Build Specification v1.0 — supersedes all prior BlockXNet v1/v2 documents in full. The conclave concludes.*
