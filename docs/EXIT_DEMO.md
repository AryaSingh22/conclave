# EXIT_DEMO.md — the Phase-2 §6 exit demo, acceptance record

**What this is.** PHASE2_DESIGN §6's exit criterion, executed: *"the §1.3 demo
extended — an external x402 agent buys ZK-verified data from a Conclave agent;
receipt verified on a second Cosmos chain over IBC."* One integration spanning
four tasks that had never run together.

**Split into two parts at the plan-stop (D-091).** Part **§6a** — this document's
subject — is **DONE**: x402 ingress + ZK_DELIVERY escrow + CVM contract release,
live on a real node. Part **§6b** adds the IBC leg (real ICS-20 USDC over a live
relayer, and receipt verification against a second chain's light client) and is
where the exit criterion is asserted in full.

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
| x402 ingress: gateway fronts gas, submits the buy | 2.3 | ✅ live (settles in `uusdc`) | re-run on the `ibc/<hash>` voucher |
| ZK_DELIVERY escrow: a groth16 proof releases payment | 2.1b | ✅ live | unchanged |
| CVM contract drives the escrow through the bindings | 2.2 | ✅ live — **D-079 discharged** | unchanged |
| Real IBC USDC; receipt verified on a second chain | 2.5 | — | **D-082's obligation** |

## How to run

```sh
make demo-contract        # only if rebuilding the contract (needs Rust; artifact is committed)
bash scripts/exit_demo.sh # boots a localnet and runs every stage
```

`scripts/exit_demo.sh` sets `SESSION_LENGTH_BLOCKS=12` / `SOLUTION_WINDOW_BLOCKS=8`
(both default-off knobs added to `localnet.sh`). The SPEC §6.4 dev defaults are
3/2 blocks, which on a 1s-block node is an auction window the driver can lose to
the in-consensus fallback — and a fallback clearing would settle the pair
**unconditioned**, opening no escrow. Demo timing only; no code path changes.

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

1. **No IBC.** Settlement is literal `uusdc`. The `ibc/<hash>` voucher, the live
   relayer, and second-chain verification are §6b (D-082's obligation).
2. **The Veil is in its epoch-0 plaintext bootstrap window.** A single-node
   localnet runs no DKG, so intents are not IBE-encrypted here. The encrypted
   reveal is proven by task 1.7 on its own 4-validator harness (D-070) and is not
   re-proven by this demo. The driver takes `CONCLAVE_GRPC`, so pointing it at
   `localnet4.sh KEEP_RUNNING=1` is a config change if that run is ever wanted.
3. **Conditioned escrows are reachable live ONLY through demo tooling.** This is
   the finding the task opened with and it survives the task — see below.
4. **The bridge checks run in the driver**, which is correct by contract
   (7683_SURFACE §6.3 makes the settler resolver-repo scope), but §6a's root is
   the node's own header. §6b replaces it with a root held by a second chain's
   light client.

### The condition-attaching proposer is DEMO tooling — read this precisely

A delivery condition is **solver-attached by design** (D-075: `IntentLeg` has no
condition field, so a solver reads the seller's signed
`IntentPayload.condition` and puts it on the `SettleLeg`). But **neither the
in-consensus fallback matcher nor the reference solver ever attaches one**, and
escrows are created only by CONCLUDE. So before this task, no component in the
repo could produce a conditioned escrow on a live network, and every
conditioned-escrow proof in the tree was a keeper-level hand-built `Match`.

`cmd/conclave-demo` now attaches conditions — and it is dev tooling. **After
§6a, the escrow condition system is still not reachable by any production
path.** Whether that is intended (third-party solvers will attach conditions,
and the chain's job is only to verify consent — which stage 7 proves it does) or
a gap (no reference implementation shows a solver how) is an open question owned
by **task 2.13**. Do not read "the demo can do it now" as "the network can do it
now".

---

## Findings from the run

| # | Finding | Disposition |
|---|---|---|
| F-1 | `ProposeSolution` returns `errors.Join(ErrInvalidSolution, ruleErr)`; `cosmossdk.io/errors` cannot walk a multi-error `Unwrap() []error`, so the **registered code is lost** and the tx surfaces internal code `1` instead of session `1412`. The reason text does survive. Every invalid solution therefore looks like an internal error to an off-chain solver. | **NOT fixed here** — tx result codes are consensus data (`LastResultsHash`), so changing one is a state-machine-visible change needing the determinism gate. Owned by **task 2.13**; pinned by name in stage 7 so the fix flips it deliberately |
| F-2 | Rust ≥1.82 emits the wasm `reference-types` proposal by default — including inside the precompiled `rust-std` — and wasmvm v2.2.8 rejects such a module. Watched: no flags → failed at offset `0x6ff`; `-C target-feature=-reference-types` → **still** failed at `0x1c94d`; Rust 1.81.0 → accepted | fixed in the demo crate by `rust-toolchain.toml` (+ four lockfile pins, since 1.81's Cargo predates `edition2024`) |
| F-3 | The first version of stage 4's permissioned-posture check asserted only `code != 0`, and **passed for the wrong reason** (code 11, out of gas on tx size) | fixed in-task: it now requires `ErrUnauthorized` specifically. Recorded because a check that passes for the wrong reason is the exact failure Rule 4 exists to catch, and this one was mine |

---

*Authored task §6a (D-091). §6b extends this document with the IBC leg; the
exit criterion is asserted there.*
