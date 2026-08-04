# SOLVER_SURFACE.md — the node-side contract for a third-party solver

**What this is.** The binding surface contract between the Conclave node and an
**off-chain solver** (SPEC §4, §6.4, §7; PHASE2_DESIGN §2.14; D-097). A solver
is an external, permissionless gRPC client: it observes AUCTION sessions, reads
their OPEN intents, computes a settlement graph, and submits
`MsgProposeSolution`. It is **untrusted** — the chain's on-chain validation is
the sole authority over whether a match is accepted, and a solver that proposes
an invalid one is rejected and, by SPEC §6.4's design, penalised.

This document exists because of a specific gap. Task 2.7 (D-094) made a signed
delivery condition a **REQUIREMENT** rather than a permission ceiling, and
nothing in the tree told a solver author what that obliged them to do — the
in-tree reference solver itself read no condition anywhere and would have
proposed an invalid solution the first time it met a condition-signing intent.
Task 2.14 closed that in code (`solver/condition`, `solver/gate`) and this
document is the written half.

**Scope note, unlike its two siblings.** `X402_SURFACE.md` and
`7683_SURFACE.md` are contracts for named separate repos. This one is not:
there is no `conclave-solver` repo. The in-repo reference (`solver/`,
`cmd/conclave-solver`) is the implementation this describes, and the contract is
addressed to anyone writing a competing one.

**Contract stability.** Everything named here lives in `conclave.*.v1` proto
packages. Fields are never renumbered; additive evolution only. If a future
task changes a named behaviour, that task updates this document in the same
session (CLAUDE.md working method).

---

## 1. The authority model (load-bearing)

### 1.1 The solver proposes; the chain decides

`validateSolution` (`x/session/keeper/solution.go`) runs the SPEC §6.4 six-rule
check on every proposed solution. A solver can never make an invalid settlement
valid by asserting it, and can never be *required* to be trusted: every rule is
re-derived on-chain from the signed intents. The solver's job is narrower and
entirely economic — **propose solutions that pass, and never one that does
not.**

Solvers register and bond (`MsgRegisterSolver`, `solver_min_bond`, 100 CLV on
dev). Bonding is what makes proposing costly enough to be honest.

### 1.2 What the node does NOT bound (solver-repo obligations)

- **Profitability, inventory, and competition.** Which matches are worth
  proposing, at what score, against how many rival solvers, is entirely the
  solver's business. The chain scores solutions and picks a winner; it offers
  no view on whether proposing was a good idea.
- **Liveness of your own proposal.** The solution window is
  `solution_window_blocks` (dev: 2) after a session enters AUCTION. Miss it and
  the session settles without you — via the **in-consensus fallback**, which
  needs no solver at all.
- **Reading the mempool.** Intents are sealed until the Reveal; a solver sees
  them only once they are OPEN, which is the point of the Veil.

---

## 2. Chain constants

Chain-id / bech32 `conc` / `uclv` gas+bond denom as in
[X402_SURFACE.md](X402_SURFACE.md) §2. Solver-specific:

| Constant | Dev default | Source |
|---|---|---|
| `solver_min_bond` | 100 CLV (`100000000uclv`) | `x/session/types/params.go` |
| `session_length_blocks` | 3 | same |
| `solution_window_blocks` | 2 | same |
| `max_solutions_per_session` | 16 | same |
| `max_graph_legs` | 256 | same |
| `invalid_solution_slash_bps` | 500 (5%) | same — governable (D-007); charged, see §6.3 |
| `equivocation_slash_bps` | 2000 (20%) | same — governable (D-007); charged, see §6.3 |

---

## 3. Transaction surface

| Msg | Purpose |
|---|---|
| `conclave.session.v1.MsgRegisterSolver` | register + bond (idempotent for an ACTIVE solver) |
| `conclave.session.v1.MsgProposeSolution` | propose a settlement graph for an AUCTION session |
| `conclave.session.v1.MsgUnbondSolver` | begin unbonding (`solver_unbond_blocks`) |

Submission is `cosmos.tx.v1beta1.Service`; the replay/nonce model is
X402_SURFACE.md §3.2. `MsgProposeSolution` rides the **general** lane.

**`MsgProposeSolution` must be the SOLE top-level message of its transaction**
(task 2.17). No siblings, and no wrapper — a proposal inside authz `MsgExec` is
rejected too. The error is `ErrProposalNotSole` (`1430`), raised in the ante
before anything executes and **not** slashed. Why: the SPEC §6.4 bond slash is
applied in the ante, against pre-transaction state, so a sibling message that
moved that state could make the penalty wrong in either direction. See §6.3.

**Never propose twice for one session.** `ErrEquivocation` (`1410`) is a
distinct, harsher category from an invalid solution — **20% of bond against
5%** — and since task 2.17 both are actually taken. The reference solver
queries `Solutions(session_id)` for its own address before proposing
(`cmd/conclave-solver/main.go`); that self-check is now worth real money.

---

## 4. Query surface

| RPC | Used for |
|---|---|
| `session.CurrentSession` / `session.Session` | find AUCTION sessions |
| `session.Solutions` | equivocation self-check; scoring visibility |
| `session.Solver` | own registration/bond state |
| `veil.Intents(state=OPEN)` | the matchable set — decode with the shape in §5.2 |

---

## 5. Delivery conditions — the attachment contract

**This section is the reason the document exists. Read it before writing a
matcher that touches a DataGrant leg.**

### 5.1 A signed delivery condition is a REQUIREMENT

An agent may sign a `DeliveryCondition` on its intent. Since task 2.7 (D-094)
the chain enforces the binding in **both** directions on every DataGrant leg:

- **Nothing attached that a party did not sign** — a solver cannot impose a
  delivery obligation, a bond/slash exposure, or a payment timeline on anyone.
  `ErrConditionNotConsented` (`1427`).
- **Nothing dropped that a party did sign** — a solver cannot silently settle a
  conditioned trade as an unconditioned one. `ErrConditionNotHonored` (`1429`).

The second direction is the one that surprises solver authors, because "just
leave the optional field empty" is the intuitive default and it is **invalid**.
A dropped condition sends the buyer's payment straight to the seller and leaves
a record-only zero-amount escrow shell with nothing to release, refund, or
dispute — which is why it is a requirement and not a courtesy.

### 5.2 Which condition governs a leg — resolution order

Two signed fields, and the **per-leg one refines the payload-level one**:

1. `IntentLeg.condition` (field 5) — if the matching leg sets it, it governs.
2. `IntentPayload.condition` (field 3) — otherwise this governs, and it governs
   **every** data/service leg of that intent at once.

A per-leg field that is **present but empty is NOT an exemption** — it falls
back to the payload-level condition exactly like an absent one. Refinement may
only ever ADD a condition, never remove one (fail-closed, D-094).

> **Do not reimplement this resolution.** Call
> `sessionkeeper.EffectiveSellerCondition(payload, legKey)` — the chain's own
> resolver, which `validateSolution` runs. A solver that reads only the
> payload-level field attaches the wrong condition for any intent that uses the
> per-leg refinement, and is rejected `1427` for a mistake its own tests would
> not show. This is pinned in-tree by
> `TestPerLegConditionRefinesPayloadLevel`, which goes red under exactly that
> shortcut.

### 5.3 The relations — per role, per class

The seller's consent is EXACT. The buyer's is **not**, and the two
non-exact classes bound in **opposite directions**, each being the direction
that is monotone-safe for the party carrying the risk:

| Role / class | Relation on the attached condition | Why |
|---|---|---|
| **Seller**, any class | **EXACT** equality, all parameters | it generates the deliverable and can name every parameter in advance |
| **Buyer**, `ZK_DELIVERY` | **EXACT** | `schema_commitment`/`vk_id` are advance-known and release is seller-driven against them |
| **Buyer**, `HASHLOCK` | `timeout_blocks` **≤** signed | the timeout is when the buyer's LOCKED payment auto-refunds, so longer is strictly worse: the signed value is a **MAXIMUM** |
| **Buyer**, `OPTIMISTIC` | `dispute_window_blocks` **≥** signed | the window IS the buyer's time to inspect and dispute before funds release, so shorter is strictly worse: the signed value is a **MINIMUM** |

`HASHLOCK.payload_hash`: empty in the buyer's signature = UNCONSTRAINED (it is a
post-delivery token a buyer cannot pre-commit); non-empty = exact, and pinning
it can only tighten the bound.

**A single "the window must match" rule is wrong for both, and a single "at
least as large" rule is wrong for HASHLOCK.** Do not generalise one class's
relation onto another.

### 5.4 The shape constraint (D-074) — and it is a separate rule

A match carrying **any** condition must be an unambiguous bilateral exchange:
exactly one conditioned DataGrant leg seller→buyer, every other leg a coin
payment buyer→seller, across exactly **two** distinct sigils. Anything else is
`ErrConditionUnsupportedMatch` (`1426`).

**Consequence for cycle-finding solvers:** a condition-signing intent may not
appear in an n-party cycle *at all*. Both moves are invalid there — attaching is
`1426`, dropping is `1429` — so the only correct action is to leave that intent
to a bilateral match. The reference withholds such intents from its cycle pass
(`condition.CycleEligible`) and, independently, gates every finished candidate
on the shape rule (`gate.AcceptConditionShape`).

Note that consent and shape are **independent**: a condition both parties signed
identically is still invalid inside a cycle. Pinned by
`TestConsentedConditionInACycleIsStillRejected`.

### 5.5 The algorithm

What the reference does, in order (`solver/solve.go`, `solver/condition`):

1. **Decode** OPEN intents, carrying `IntentPayload.condition` (`match.OpenIntent.Condition`).
2. **Withhold** condition-signing intents from the cycle pass (§5.4).
3. **Build** candidates exactly as before — conditions change no amount or price.
4. **Attach**, per DataGrant leg, the condition its GIVER signed, resolved by
   `EffectiveSellerCondition` against that leg's asset key (§5.2). The
   counterparty's signature is *not* consulted here.
5. **Self-check** each finished candidate against the chain's own functions —
   `gate.Accept` (conservation + price binding), `gate.AcceptConditionShape`
   (§5.4), `gate.AcceptConsent` (§5.1/§5.3).
6. **Drop** anything that fails. Never "propose it and see."

Steps 4 and 5 are deliberately separate. Attachment is best-effort matching;
the self-check is the invariant, and it is the chain's code rather than a copy
of it. The guarantee that you never propose a downgrade must not depend on your
attacher covering every shape you will ever build.

### 5.6 When you cannot attach validly: **do not fill**

If no condition satisfies both parties — most commonly a buyer that signed one
against a seller that signed none — there is no valid settlement. The intents go
unmatched and roll to their TTL, where the bond is refunded. That is the chain's
own posture (D-094: *no fill beats a wrong fill, because a wrong fill cannot be
undone*), and a solver must mirror it rather than reach for a fill.

### 5.7 What gets your solution rejected

| Error | Code | Cause |
|---|---|---|
| `ErrInvalidSolution` | `1412` | the envelope error every rule failure arrives under — dispatch on the **(codespace, code) PAIR** `("session", 1412)`, then `errors.Is` the specific rule (D-093) |
| `ErrConditionNotHonored` | `1429` | you DROPPED a condition a party signed (§5.1) |
| `ErrConditionNotConsented` | `1427` | you attached one a party did not sign, or one the buyer's relation rejects (§5.3) |
| `ErrConditionUnsupportedMatch` | `1426` | the conditioned match is not the D-074 bilateral shape (§5.4) |
| `ErrLimitRateUnsupported` | `1428` | a leg's signed §5.1 limit RATE is not evaluable for this match shape (D-089; the lift is task 2.15) |
| `ErrEquivocation` | `1410` | a second solution for one session |
| `ErrSessionNotInAuction` | `1409` | the window closed |
| `ErrProposalNotSole` | `1430` | the proposal shared its transaction with another message, or was nested in a wrapper (§6.3) |

**Two of these cost bond, the rest cost gas.** `1412` costs 5% and `1410` costs
20% (SPEC §6.4, rates in `session.Params`); every other row above costs gas
only. See §6.3 — and note that this was **not** true before task 2.17, when the
slash was reverted along with the rejection.

---

## 6. Limits — what the reference does and does NOT demonstrate

**Read this section with §5. Taken alone, §5 reads as more settled than it is.**

### 6.1 No in-tree LIVE path reaches HASHLOCK or OPTIMISTIC

The reference attachment path is exercised for all three classes by
`solver/condition_attachment_test.go` and end-to-end against a real
`ConclaveApp` by `app/solver_conformance_test.go` (OPTIMISTIC). But the only
**live-network** conditioned settlement in this repo — `scripts/exit_demo.sh`,
the §6 exit demo — is **ZK_DELIVERY only**. No localnet or multi-node run has
ever settled a HASHLOCK or OPTIMISTIC condition end to end. Their state
machines, timeouts, dispute path, and bond mechanics are covered by keeper-level
and app-level tests, not by a live node.

A solver author should read §5.3's HASHLOCK and OPTIMISTIC rows as
**specified and unit-proven, not field-proven**.

### 6.2 The reference is a matching library, not a production solver

`solver/` implements the matching IDEA (cycles + greedy pairwise) and the
correctness self-checks. It has no inventory model, no profitability model, no
gas strategy, no competition strategy, and no view on whether a given match is
worth the bond risk. `cmd/conclave-solver` is a thin daemon around it for
localnet use.

### 6.3 ~~The bond slash is currently NOT applied on the proposal path~~ — CLOSED by task 2.17

> **This section documented a temporary gap and said it would be closed. It is
> closed.** Retained, marked, with the forward pointer, because a solver author
> who read the previous version may have designed around it — and it explicitly
> told them not to.
>
> **What it said** (its "D-024" citation was wrong — the rates are **D-007**'s;
> corrected at 2.17): SPEC §6.4 prices an invalid solution at 5% of bond and
> equivocation at 20%; `x/session/keeper/solution.go` called `slashSolver` for
> both and then returned an error, so baseapp discarded the failing message's
> branch and the slash went with it. A rejected proposal cost a solver **gas
> only**. Found by task 2.14 (D-097 finding F-1), pinned by
> `TestDroppingTheConditionIsRejectedAtTheWire` and
> `TestEquivocationSlashAlsoDoesNotPersist`.
>
> **What changed (task 2.17):** the slash is applied in the ante handler
> (`ProposeSolutionSlashDecorator`), the only cosmos-sdk v0.50 store branch that
> outlives the message it penalises. Both pins were watched RED and replaced
> with the slash assertions. The warning in the old text — *"a solver whose
> economics assume a free retry loop will start losing bond the day 2.17
> ships"* — is now in effect.

**The slashes are real. Both of them.**

| Rejection | Cost | Where it is applied |
|---|---|---|
| invalid solution (`1412`) | `invalid_solution_slash_bps`, **5%** of bond | ante, before the message is rejected |
| equivocation (`1410`) | `equivocation_slash_bps`, **20%** of bond | ante, same |
| every other rejection (`1409`, `1411`, inactive solver) | **gas only** | nothing is charged — and this is asserted, not assumed |
| a winning solution that fails at settlement | **5%** | EndBlock (`conclude_failed`) — this one always worked |

The rates are **governable params** (D-007), not constants: read them from
`session.Params` rather than hardcoding 500/2000.

**One consequence for how you submit.** A `MsgProposeSolution` must be the
**sole top-level message of its transaction**, and must not be nested inside a
wrapper such as authz `MsgExec`. Anything else is `ErrProposalNotSole`
(`1430`), rejected before execution and **not** slashed. The reason is
structural: the ante prices the proposal against pre-transaction state, so a
sibling message that moved that state could make the price wrong in either
direction. Bundling is the only thing this rule costs you; batching several
proposals means several transactions.

**What was never affected, and it is the part that protects counterparties.**
Rejection was always entirely real: an invalid solution was refused, nothing
mis-settled, and every rule in §5 was enforced exactly as written. What was
missing was the DETERRENT, not the check — which is why §5 needed no correction
when 2.17 landed.

### 6.4 Conditioned settlements and `condition_outcome`

All of [7683_SURFACE.md](7683_SURFACE.md) §6.4 applies: `Receipt.condition_outcome`
is stamped `NONE` unconditionally and never updated when an escrow resolves
(D-087; the fix is task 2.9). A receipt for a conditioned settlement is
evidence that CONCLUDE happened, **not** that delivery completed. Since task 2.7
made conditioned settlements more common, this affects more receipts than when
it was filed.

### 6.5 Not covered by the determinism vector

The chain's global determinism gate covers **none** of this. Its intent
generator never sets `IntentPayload.condition` or `IntentLeg.condition`
(`x/veil/simulation/operations.go`), so every path in §5 is structurally
unreachable by it under any seed count — a Category-1 coverage shadow whose
remedy is a wider generator (PHASE3_PLAN 3.5), not more seeds. The tests in §7
are the gate.

---

## 7. Conformance (what pins this contract in-tree)

- **`solver/condition_attachment_test.go`** (in `make test`) — the attachment
  RULES: all three classes attached exactly for the seller; the per-class buyer
  relations including both boundary blocks and the opposite directions; the FW-2
  per-leg refinement and its fail-closed empty case; buyer-signed/seller-unsigned
  clearing nothing; the D-074 cycle exclusion on both sides; and
  `TestBothMovesInACycleAreSlashable` / `TestConsentedConditionInACycleIsStillRejected`,
  which assert `1426` and `1429` by their SPECIFIC identities.
- **`app/solver_conformance_test.go`** (in `make test`) — the reference solver,
  unmodified, clearing a conditioned settlement through a real `ConclaveApp`
  with no hand-built keeper Matches: proposal accepted (code 0), solver not
  slashed, and **the solver's solution is the one that settles** rather than the
  fallback's. Its sibling pins the rejection identity (`("session", 1412)` +
  the `1429` message) and — since task 2.17 — the 5% slash actually landing.
- **`app/proposal_slash_test.go`** (in `make test`) — the task-2.17 economics:
  that an UNPRICED rejection (`1409`) still costs nothing, that the
  sole-message rule holds for both a sibling message and an authz `MsgExec`
  wrapper, and that the conclude-time slash lands too, asserted by its event
  reason rather than by a count.
- **`x/session/keeper/condition_binding_test.go`** — the chain-side rule these
  self-checks call (task 2.7, D-094).
- Watched red: removing the attachment path, the consent gate, or the resolver
  call each reds a distinct, recorded set of the above (D-097); removing the
  ante slash call, the sole-message rule, its nested walk, or the unpriced-verdict
  guard each reds a distinct set (task 2.17).
- **Live:** `scripts/exit_demo.sh`'s consent stage reads the solver's bond back
  after the rejection and checks the governed rate. It used to *print* that the
  solver was slashed 5% without querying anything, while the slash was in fact
  being reverted — which is how a banner can be wrong for two phases.

*Authored task 2.14 (D-097). The chain-side semantic it documents is task 2.7
(D-094); the wire-code dispatchability in §5.7 is task 2.13 (D-093); the
proposal-time slash economics in §6.3 are task 2.17.*
