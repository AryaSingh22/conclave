# EXIT_DEMO.md — the Phase-2 §6 exit demo, acceptance record

**What this is.** PHASE2_DESIGN §6's exit criterion, executed: *"the §1.3 demo
extended — an external x402 agent buys ZK-verified data from a Conclave agent;
receipt verified on a second Cosmos chain over IBC."* One integration spanning
four tasks that had never run together.

**Split into two parts at the plan-stop (D-091).** Part **§6a** is **DONE**:
x402 ingress + ZK_DELIVERY escrow + CVM contract release, live on a real node.
Part **§6b** is **DONE** (D-092): the IBC leg — real ICS-20 USDC over a live
relayer, and receipt verification against a second chain's light client.

> ## THE PHASE-2 §6 EXIT CRITERION IS **ASSERTED** (§6b, D-092)
>
> An external x402 agent bought ZK-verified data from a Conclave agent,
> **settling in real IBC USDC** received over a **live relayer**, and the
> settlement receipt **ICS-23-verified against the root held by a second
> Cosmos chain's on-chain light client of Conclave**. Every boundary on that
> sentence is stated below — read *"What §6b does and does not claim"* before
> quoting it. The run is **INTERNAL** (see the classification directly below).
>
> **RE-VERIFIED AT HEAD (task 2.16, D-095).** §6b's last recorded run was at
> `1aecfbf`; `0d7f167` then edited its driver and `7283b11` inverted the consent
> semantic, and neither re-ran it — so the assertion had come to rest on
> §6a-green-at-HEAD plus an additivity argument. 2.16 re-ran
> `scripts/exit_demo_ibc.sh` at HEAD, which is what makes the criterion
> **evidence rather than inference**.

---

## Run classification: INTERNAL ACCEPTANCE RUN

The demo's buy is a **conditioned** ZK purchase, so its receipt reads
`condition_outcome = NONE` for a conditioned settlement (**D-087**). Both edge
contracts disclose the true semantics, so no consumer is misled, and that makes
this acceptable as an internal acceptance run (D-090).

> **If this output is ever repurposed as EXTERNAL-FACING evidence, task 2.9
> (truthful `condition_outcome`) is a PREREQUISITE.** This is a condition, not a
> caveat. The demo prints it in its own banner.

The demo does not route around the boundary — it **demonstrates** it. See stage 6.

---

## Leg status

| Leg | Task | §6a | §6b |
|---|---|---|---|
| x402 ingress: gateway fronts gas, submits the buy | 2.3 | ✅ live (settles in `uusdc`) | ✅ re-run on the `ibc/<hash>` voucher |
| ZK_DELIVERY escrow: a groth16 proof releases payment | 2.1b | ✅ live | ✅ escrow holds, and pays out, the voucher |
| CVM contract drives the escrow through the bindings | 2.2 | ✅ live — **D-079 discharged** | ✅ re-run unchanged |
| Real IBC USDC; receipt verified on a second chain | 2.5 | — | ✅ **D-082 DISCHARGED** |

## How to run

```sh
make demo-contract            # only if rebuilding the contract (needs Rust; artifact is committed)
bash scripts/exit_demo.sh     # §6a: one node, settles in native uusdc
bash scripts/exit_demo_ibc.sh # §6b: TWO nodes + a live relayer; asserts the exit criterion
```

`scripts/exit_demo_ibc.sh` installs the relayer if absent
(`go install github.com/cosmos/relayer/v2@v2.6.0`) and boots `conclave-dev-1`
(settlement) plus `conclave-dev-2` (the USDC source and light-client host) on
offset ports. **The relayer version pin lives in that script and in D-092, not
in the binary:** installed via `go install` it carries no ldflags, so
`rly version` prints an empty version string and cannot be used to identify
what is running.

`scripts/exit_demo.sh` sets `SESSION_LENGTH_BLOCKS=12` / `SOLUTION_WINDOW_BLOCKS=8`
(both default-off knobs added to `localnet.sh`). The SPEC §6.4 dev defaults are
3/2 blocks, which on a 1s-block node is an auction window the driver can lose to
the in-consensus fallback. The knobs keep the demo's *own* solver on the winning
path so the stages assert what they were written to assert.

> **The original reason for these knobs expired at task 2.7** (D-094), and it is
> corrected here rather than left standing: this paragraph used to say *"a
> fallback clearing would settle the pair **unconditioned**, opening no
> escrow."* Since 2.7 the fallback **ATTACHES** the signed condition
> (`attachFallbackCondition`), so losing the auction no longer silently
> downgrades the trade — it would open a funded escrow too. The knobs are now a
> determinism-of-narrative convenience, not a correctness guard. Demo timing
> only; no code path changes.

---

## What each stage proves

**Stage 1 — actors.** Model-A gateway (X402_SURFACE §1.2): the operator's own
sigil, a fresh mandate-bound hot key, allowlist `{SubmitIntent, CancelIntent}`,
`fee_denoms=[uusdc]`. The **principal** holds settlement funds because coin give
legs settle from the sigil's controller, not the submitting key.

**Stage 2 — x402 ingress.** The buy is submitted by the hot key with its gas fee
paid in `uusdc` through §6.2 fee abstraction — asserted by the mandate pool
balance moving `+5000`, not by reading an event. Buyer and seller sign the
**same** `ZkDeliveryCondition` (D-076 requires buyer-side consent for ZK).
`allow_partial` is left unset: a purchase is fill-or-kill.

**Stage 3 — the conditioned settlement.** A funded ZK escrow opens holding the
buyer's payment, with `payer=buyer` / `payee=seller`; the seller's `uusdc`
balance is asserted **zero** — the payment is held, not delivered.

**Stage 4 — governance upload.** A direct `MsgStoreCode` is rejected with
`ErrUnauthorized` (the D-079 permissioned posture, live), then the contract is
stored through a passing gov proposal whose `instantiate_permission` names the
deployer.

**Stage 5 — the CVM release (D-079's deferred smoke).** Three negatives first,
then the positive, all driven by a **real contract inside a real wasm VM**:

| # | Attempt | Result |
|---|---|---|
| a | proof bound to a **different** escrow id | `ErrInvalidProof` (1510) — D-076 instance binding, live |
| b | preimage `release_escrow` on a ZK escrow | `ErrWrongClass` — the binding reaches the real keeper gate |
| c | contract emits `conclude` | `ErrUnauthorized` — CVM allowlist layer 1, from a contract that genuinely emitted it |
| d | proof bound to **this** escrow | escrow → `RELEASED`, seller paid `250000000uusdc` |

Negative (a) runs **before** the positive on purpose: after release the escrow is
terminal, so a wrong-instance proof would fail as `ErrTerminalState` and prove
nothing about instance binding.

**Stage 6 — the evidence, and its limits.** The receipt + ICS-23 proof are
fetched and `terms_hash` is recomputed from the *queried* winning Solution
(7683_SURFACE §6.3 check 2). Then the D-087 boundary is **demonstrated**:

- `condition_outcome` reads `NONE` even though this settlement is conditioned;
- §6.3 **check 3 REJECTS** the conditioned delivery leg, so this receipt is
  correctly **inadmissible** as delivery evidence on the v1 fill-back rail;
- delivery is proven instead by the **escrow's terminal `RELEASED` state**.

A demo that exercises a documented boundary correctly is worth more than one that
avoids it. If `condition_outcome` ever stops reading `NONE`, stage 6 fails by
name and tells you to update task 2.9's status.

**Stage 7 — the consent gate from the live path.** The demo driver is the first
live-network writer of `SettleLeg.Condition` (see below), so the Rule-4 question
is whether that writer can **impose** a condition. It cannot: with the seller
signing the ZK condition and the buyer signing nothing, the proposed solution is
rejected specifically because *"buyer … did not sign this ZK condition"*
(D-076), and the over-reaching solver is slashed 5%.

---

## Boundaries — what §6a does NOT prove

1. ~~**No IBC.** Settlement is literal `uusdc`.~~ — **DISCHARGED by §6b**
   (D-092): the same flow re-ran with an `ibc/<hash>` voucher, moved by a live
   relayer, as the settlement denom.
2. **The Veil is in its epoch-0 plaintext bootstrap window.** A single-node
   localnet runs no DKG, so intents are not IBE-encrypted here. The encrypted
   reveal is proven by task 1.7 on its own 4-validator harness (D-070) and is not
   re-proven by this demo. The driver takes `CONCLAVE_GRPC`, so pointing it at
   `localnet4.sh KEEP_RUNNING=1` is a config change if that run is ever wanted.
3. ~~**Conditioned escrows are reachable live ONLY through demo tooling.**~~ —
   **NO LONGER TRUE; superseded by task 2.7 (D-094).** The in-consensus fallback
   now ATTACHES a signed condition (`attachFallbackCondition`,
   `x/session/keeper/fallback.go`), so consensus itself is a live writer of
   `SettleLeg.Condition`. What survives is narrower and is task **2.14**: the
   reference solver in `solver/` still never reads `IntentPayload.condition` —
   see below.
4. **The bridge checks run in the driver**, which is correct by contract
   (7683_SURFACE §6.3 makes the settler resolver-repo scope), but §6a's root is
   the node's own header. **§6b replaced the ROOT** with one held by a second
   chain's light client; the checks still run in the driver, and that half of
   the boundary is permanent — see *"What §6b does and does not claim"*.

### The condition-attaching proposer — CURRENT STATE (task 2.7, D-094)

**A signed delivery condition is a REQUIREMENT, not a permission ceiling.**
`attached ⇒ signed` and `signed ⇒ attached` are both enforced, at both
chokepoints (`x/session/keeper/condition_binding.go`), and the **in-consensus
fallback ATTACHES** the giver's signed condition (`attachFallbackCondition`,
`x/session/keeper/fallback.go`) rather than dropping it. So the escrow condition
system **is** reachable by a production path, and that path is consensus itself.
Dropping a signed condition is `ErrConditionNotHonored` (session `1429`).
Executable form: `TestSignedConditionIsRequirement_Enforced`.

`cmd/conclave-demo`'s `proposeConditionedSolution` was the **first** live-network
writer of `SettleLeg.Condition` (§6a's finding) and remains a valid one — it is
simply no longer the only one. Stage 7 still proves the thing worth proving
about it: that writer cannot **impose** a condition.

**What survives, and it is task 2.14:** non-test `solver/` never reads
`IntentPayload.condition` — re-verified by grep at this boundary, zero hits — so
the in-tree **reference solver would propose a slashable (5%) solution** the
first time it meets a condition-signing intent. "The chain now requires it" is
not "the reference client now does it."

> **SUPERSEDED — kept for the record (§6a/D-091 and 2.13/D-093, both closed by
> D-094; forward pointer: the three paragraphs above, `condition_binding.go`,
> and PHASE2_DESIGN §2.7):**
>
> *"A delivery condition is **solver-attached by design** (D-075: `IntentLeg`
> has no condition field). But **neither the in-consensus fallback matcher nor
> the reference solver ever attaches one**, and escrows are created only by
> CONCLUDE. So before this task, no component in the repo could produce a
> conditioned escrow on a live network, and every conditioned-escrow proof in
> the tree was a keeper-level hand-built `Match`. `cmd/conclave-demo` now
> attaches conditions — and it is dev tooling. Do not read 'the demo can do it
> now' as 'the network can do it now'.*
>
> *ANSWERED (task 2.13, D-093): **GAP** — and larger than 'no reference
> implementation'. The consent gates are one-directional: `attached ⇒ signed` is
> enforced at both chokepoints, `signed ⇒ attached` at neither, so a signed
> condition is a **permission ceiling, never a requirement**. The in-consensus
> FALLBACK emits no condition at all and is the default clearing path whenever
> no solver wins the auction, so a pair who BOTH signed the same ZK condition is
> cleared unconditioned by the state machine itself — a protocol property, not a
> tooling gap. Pinned by
> `TestSignedConditionIsPermissionNotRequirement_BoundaryPin`."*
>
> That pin was **REPLACED, not relaxed** — it instructed its own owner to do
> exactly that (D-094). Retained because the GAP finding is what motivated 2.7
> and D-094's design rests on it; marked with this pointer because a struck
> block without one still reads as current to a skimmer.

---

## Findings from the run

| # | Finding | Disposition |
|---|---|---|
| F-1 | `ProposeSolution` returns `errors.Join(ErrInvalidSolution, ruleErr)`; `cosmossdk.io/errors` cannot walk a multi-error `Unwrap() []error`, so the **registered code is lost** and the tx surfaces internal code `1` instead of session `1412`. The reason text does survive. Every invalid solution therefore looks like an internal error to an off-chain solver. | **CLOSED by task 2.13 (D-093).** Fixed with a typed composite (`types.InvalidSolution`) that carries `ABCICode()`/`Codespace()` explicitly while keeping both `errors.Is` directions. The stage-7 pin did exactly what it was built to do: with the fix applied it failed the live demo on its own message (`EXIT_RC=1`), and it is now replaced by assertions on the (codespace, code) PAIR — the run prints `codespace "session" code 1412`. Note the correction D-093 records: the determinism gate could never have caught this either way — the vector hashes the sim's store-commit app hash, and `SimDeliver` builds no CometBFT block, so result codes never enter it |
| F-2 | Rust ≥1.82 emits the wasm `reference-types` proposal by default — including inside the precompiled `rust-std` — and wasmvm v2.2.8 rejects such a module. Watched: no flags → failed at offset `0x6ff`; `-C target-feature=-reference-types` → **still** failed at `0x1c94d`; Rust 1.81.0 → accepted | fixed in the demo crate by `rust-toolchain.toml` (+ four lockfile pins, since 1.81's Cargo predates `edition2024`) |
| F-3 | The first version of stage 4's permissioned-posture check asserted only `code != 0`, and **passed for the wrong reason** (code 11, out of gas on tx size) | fixed in-task: it now requires `ErrUnauthorized` specifically. Recorded because a check that passes for the wrong reason is the exact failure Rule 4 exists to catch, and this one was mine |

---

# §6b — the IBC leg, and the exit criterion

**Run:** `bash scripts/exit_demo_ibc.sh` · two chains + a live
`cosmos/relayer` **v2.6.0** process · driver `conclave-demo exit-demo-ibc`.

§6b runs the **same driver code** as §6a. The settlement asset is a parameter
(`exitState.settleDenom`), so §6a's stages are not re-implemented for the
voucher — they are re-executed with a different denom. That makes
`scripts/exit_demo.sh` a live **regression test** of the parametrization, and
it is run alongside §6b at every close.

## What each stage proves

**Stage 1 — IBC USDC in, over a live relayer (D-082's obligation).**

- The ICS-20 channel is asserted **OPEN on both chains**, and the two ends are
  checked to **name each other** — querying one side alone would accept a
  half-open handshake.
- Each side's light client of the other is resolved **through the connection
  this channel actually travels** (channel → connection → client), never by
  scanning for "a" client, and each client's `ClientState.chain_id` is checked
  against the counterparty's real chain id.
- Chain B sends `2500000000uusdc`; chain A mints
  `ibc/8E27BA2D5493AF5636760E354E46004562C46AB7EC0CC4C1CA14E9E20E2545B5`.
- The trace resolves to `transfer/channel-0` + base denom `uusdc`, the trace
  **recomputes** the voucher denom, and the voucher is asserted **≠ `uusdc`** —
  chain A has a native `uusdc` of its own, which a weaker check would have
  accepted silently.
- **Cross-chain conservation:** chain B's ICS-20 escrow account grew by exactly
  what chain A minted.

**Stages 2–4 — §6a's flow, on the voucher.** The gateway's mandate carries
`fee_denoms=[ibc/8E27…]` and ceilings denominated in the voucher; gas is fronted
in the voucher through §6.2 abstraction (asserted by the mandate pool moving
`+5000`); the ZK escrow opens **holding `250000000ibc/8E27…`**; the contract —
uploaded through a passing gov proposal after a direct `MsgStoreCode` is refused
`ErrUnauthorized` — releases it with a real groth16 proof and the seller is paid
in the voucher. All three §6a negatives run first, unchanged.

**Stage 5 — the receipt, verified against chain B's light client of chain A.**

The height reconciliation is the load-bearing part, so it is spelled out:

> A light client's `ConsensusState` at height `H` stores
> `Root = A.header(H).AppHash`, and a header's app hash commits the **previous**
> block's state. So a proof checked against that root must be taken at store
> version **`H−1`**.

x/receipt's `ReceiptProof` always proves at the **tip** (it passes no `Height`),
so it cannot serve a chosen height. The stage therefore inverts the order —
**read the height chain B's client actually holds, then ask chain A for a proof
at the version that root commits to** — using the generic ABCI route
7683_SURFACE §6.2 already documents (`/store/session/key`, `prove=true`,
explicit height). The product surface's tip proof is still fetched, and the two
are cross-checked to prove **identical receipt bytes**.

Real A→B traffic (a token return transfer) is used to make the relayer advance
chain B's client — with traffic, not with a privileged command.

Then 7683_SURFACE §6.3's four checks run, exactly as an origin settler would:

| Check | Result |
|---|---|
| 1 — root authenticity | ICS-23 membership **verified against chain B's stored root** (e.g. `BD261E27FE2D5AF4…` at height 93, proof taken at version 92) |
| 2 — terms authenticity | `terms_hash` recomputed from the *queried* winning Solution; `receipt.intent_id ∈ match.intent_ids` |
| 3 — order ↔ fill | **REJECTS** the conditioned delivery leg (D-087 boundary, as in §6a); delivery proven instead by the escrow's terminal `RELEASED` |
| 4 — first-claim uniqueness | settler-side ledger; the verbatim replay is refused |

**Four negatives, each asserting its SPECIFIC failure** — never merely "an
error", which is the §6a F-3 lesson:

| Negative | Must fail with |
|---|---|
| one byte of the receipt flipped | `chained membership proof failed to verify membership of value` |
| the **nearest other** root chain B holds (not a distant one — a far root rejects trivially) | `proof did not commit to expected root` |
| **the off-by-one itself**: proof taken at version `H` instead of `H−1`, checked against the root for `H` | `proof did not commit to expected root` |
| check 3 skipped | the conditioned leg **is** admitted — which is what proves check 3 load-bearing |

The first two negatives fail with **different** messages, which is what proves
they test different things rather than each other.

**Stage 6 — the consent gate from the live path.** §6a's stage 7, re-run last
(the 5% slash takes solver1 below `solver_min_bond`).

## What §6b does and does not claim

This is the part to read before quoting the exit criterion anywhere.

- ✅ **B→A was verified by a state machine, in consensus.** The voucher can only
  exist because chain A executed `MsgRecvPacket`, and ibc-go's `OnRecvPacket`
  ICS-23-verifies chain B's packet commitment against chain A's **on-chain**
  light client of B. That direction is a genuine in-consensus cross-chain
  verification and is claimed as one.
- ⚠️ **A→B was verified in the DEMO DRIVER, against chain B's root.** The §6.3
  checks execute in `cmd/conclave-demo` because the settler/bridge is
  separate-repo scope (7683_SURFACE §1.2, §6.3). What makes it a **second-chain**
  verification is that the **root is read out of chain B's on-chain
  `ConsensusState`** for its client of chain A — established by a live relayer
  and advanced by real `MsgUpdateClient` txs on B — rather than out of chain A's
  own header, which is what §6a used.
- ❌ **NOT claimed: chain B's state machine verified the receipt.** Making that
  true means deploying the verification bridge (a contract on B running ICS-23
  against its client), which is resolver-repo scope by contract. It is named
  here as a boundary rather than blurred.
- The **INTERNAL** classification, the epoch-0 plaintext Veil, and
  "conditioned escrows are reachable live only through demo tooling" (task 2.13)
  all carry over from §6a unchanged.

## A constraint a real bridge operator inherits

Stage 5 fetches a proof at the height chain B's light client holds, and **a
pruned version cannot serve one**. That is a property of the rail, not of the
demo: any node serving light-client-driven fill-back must retain the versions
its counterparty's client tracks. The demo nodes therefore run
`pruning=nothing`, and the driver fails with that explanation rather than a bare
"no proof" if the version is gone.

## Findings from the §6b run

| # | Finding | Disposition |
|---|---|---|
| F-1 | §6a's wrong-instance-proof negative (stage 5a — the live proof of **D-076 instance binding**) asserted only `code != 0`. A rejection for *any* other reason — wrong class, terminal state, out of gas — would have "passed" it while proving nothing about instance binding. This is the same shape as §6a's own F-3, in a negative that guards a security property. | **Fixed in-task** (it is an assertion-strength gap in dev tooling, not a chain defect, and F-3 is the precedent): it now requires `ErrInvalidProof` (1510) specifically. Surfaced rather than quietly tightened, because it means §6a's D-076 live claim rested on a weaker check than its record implied |
| F-2 | The relayer installed via `go install` reports an **empty version string** (`rly version` → `version: ""`), because no ldflags are set. The running binary cannot be identified from itself. | Not fixable from here. The pin (`v2.6.0`) is recorded in `scripts/exit_demo_ibc.sh` and D-092, which are now the only sources of truth for what ran |

**Watch that did NOT fire.** The plan named the injected `Txs[0]` (task 1.6's
vote-extension transport) as the most likely place for a third-party relayer to
break, with a silent non-relay as the expected failure mode. It did not happen:
`cosmos/relayer` v2.6.0 relayed both directions without special configuration.
Recorded as a *negative result worth keeping* — Conclave's proposer-injected tx
does not impede standard IBC tooling at this version, which is a fact no test in
the tree asserts and which a future ABCI change could quietly break.

---

*Authored task §6a (D-091); extended by task §6b (D-092), which added the IBC
leg and **asserted the Phase-2 §6 exit criterion**; reconciled and re-verified
at the phase boundary by task 2.16 (D-095), which also declared Phase 2
**EXITED**.*

***"Both scripts are run at every close" is a RULE, and 2.16 found it had not
been held.** Tasks 2.13 and 2.7 each ran `exit_demo.sh` only, while the script
that actually asserts the criterion sat un-run across a driver edit and a
consent-semantic inversion. The rule was repaired by MAKING IT TRUE — 2.16 ran
`exit_demo_ibc.sh` at HEAD — not by softening the sentence. Any future task that
touches `cmd/conclave-demo`, `x/session/keeper`, or `x/escrow` and runs only
`exit_demo.sh` is repeating the same lapse: `exit_demo.sh` is the regression that
keeps §6a honest, and `exit_demo_ibc.sh` is the one that keeps the EXIT CRITERION
honest.*
