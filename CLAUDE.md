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
- [ ] 0.10 Reference solver binary (naive + cycles) — NEXT
- [ ] 0.11 SDK simulation integration (all modules)
