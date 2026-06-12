# Conclave Block Lanes — Client Developer Guide

Implements SPEC §6.7 via [skip-mev/block-sdk v2.1.5](https://github.com/skip-mev/block-sdk)
(task 0.6; DECISIONS.md D-032…D-036). This document is the client-facing
contract for how transactions are routed into block space.

## The five lanes

Block proposals are partitioned into five lanes, in this priority order,
each capped at a share of the block's `max_tx_bytes` and gas:

| Priority | Lane | Max block space | Member msg TypeURLs |
|---|---|---|---|
| 1 (highest) | `veil` | 30% | `conclave.veil.v1.MsgSubmitIntent`, `conclave.veil.v1.MsgCancelIntent` — **only** these two |
| 2 | `session` | 25% | `conclave.session.v1.MsgRegisterSolver`, `MsgUnbondSolver`, `MsgProposeSolution`, `MsgConclude` |
| 3 | `commerce` | 15% | `conclave.sigil.v1.MsgRegisterSigil`, `MsgRotateController`, `MsgAnchorCredential`, `MsgRevokeCredential`; `conclave.mandate.v1.MsgIssueMandate`, `MsgRevokeMandate`, `MsgAmendMandate`; `conclave.escrow.v1.MsgReleaseEscrow`, `MsgRaiseDispute`, `MsgSubmitDeliveryProof` |
| 4 | `ibc` | 15% | every msg whose TypeURL starts with `/ibc.` (core client/connection/channel + transfer, etc.) |
| 5 (lowest) | `general` | 15% | everything else — bank sends, gov, authz, staking, **all `MsgUpdateParams`**, … |

The shares sum to exactly 1.0 and are validated at app construction.

## Routing rules (what client devs must know)

1. **A tx routes to the lane of its LOWEST-priority msg class.** Lane
   membership is computed per msg from its top-level TypeURL; the tx goes
   to the lowest-priority (numerically last) lane any of its msgs belongs
   to. Examples:
   - `[MsgSubmitIntent]` → `veil`
   - `[MsgSubmitIntent, MsgProposeSolution]` → `session`
   - `[MsgSubmitIntent, MsgRegisterSigil]` → `commerce`
   - `[MsgSubmitIntent, MsgTransfer]` → `ibc`
   - `[MsgSubmitIntent, MsgSend]` → `general`

   **Consequence: do not mix msg classes in one tx** if you want
   high-priority block space. A Sealed Intent batched with anything else
   loses the veil lane's 30% share and its fee_bid ordering.

2. **Every tx matches exactly one lane.** The predicates partition the tx
   universe — there is no tx that matches zero or two lanes, so routing is
   deterministic and independent of internal lane registration order.

3. **Wrapper msgs route as the wrapper.** Routing reads the *top-level*
   TypeURL only. An authz `MsgExec` wrapping `MsgSubmitIntent` routes as
   `MsgExec` (= `general`), never as its contents. Don't wrap lane-class
   msgs if you want their lane.

4. **`MsgUpdateParams` of veil/session/etc. are `general`-class.** The
   spec pins the veil lane to MsgSubmitIntent/MsgCancelIntent *only*; the
   same named-msgs-only reading applies to every lane. Params changes ride
   in gov proposals through the general lane.

## Ordering within lanes

- **veil** (SPEC §6.3): ordered by the envelope `fee_bid` (a decimal
  string, parsed as a non-negative decimal), highest first. A multi-msg tx
  takes the max fee_bid across its `MsgSubmitIntent` envelopes;
  `MsgCancelIntent` carries no bid and counts as zero. Unparseable, empty,
  or negative bids are treated as zero — ordering metadata never rejects a
  tx (envelope *validation* is the msg server's job). Ties between
  different senders break deterministically by (sender address, sequence);
  multiple txs from one sender are always ordered by their sequence index,
  ascending (for unordered txs, a hash-derived synthetic index — see
  D-034).
- **session / commerce / ibc / general**: FIFO-like deterministic order —
  all txs carry equal priority and order by the same (sender, sequence)
  tie-break.

## What consensus validates (ProcessProposal)

Every honest node re-verifies each proposal:

1. **Partition structure** — txs grouped contiguously per lane, lanes in
   priority order, no tx outside its lane's partition.
2. **Intra-lane priority order** — for veil, descending fee_bid is
   re-computed from the tx bytes and enforced; a proposer that reorders
   *across different fee_bids* produces an invalid block.
3. **Lane space limits** — each partition must fit its lane's share of the
   consensus `max_bytes`/`max_gas`. An over-share partition rejects the
   whole proposal.
4. **Per-tx ante verification** — the production ante chain (including
   mandate window charging, accumulated across the whole proposal) runs on
   every tx. A proposal carrying more spend than a Mandate window allows is
   rejected — proposal verification and block execution agree on mandate
   aggregation.

## The veil-ordering trust model (read this before relying on fee_bid)

**What is enforced in consensus:** descending `fee_bid` order within the
veil lane partition — this is a pure function of tx bytes and every node
checks it.

**What is NOT enforced:** the tie-break among *equal* fee_bids. The spec's
"then submit order" (§6.3) refers to proposer-local mempool arrival, which
no other node can observe or verify; ProcessProposal cannot validate it. A
malicious proposer can therefore reorder txs *within* an equal-fee_bid tie
(and can always choose *which* txs to include at all — that is inherent to
proposing).

**Why this is acceptable:**

- Session clearing is order-independent: matched intents clear at one
  uniform batch price per asset pair (SPEC §6.4 validation rule 4), so
  position within the lane buys no price improvement — latency and
  ordering games are worthless inside a batch.
- The order-then-reveal invariant (SPEC §6.3) is what protects intent
  contents, and in Phase 1 ordering is fixed strictly before any node can
  decrypt — a proposer reordering ciphertexts it cannot read gains
  nothing.

**`fee_bid` is congestion pricing, not a security boundary.** It buys
admission priority when the veil lane's 30% share is contended; it does
not buy execution advantage, and you must not design strategies that
assume fine-grained ordering within a fee level.

## Over-share behavior: queueing, not spilling (D-036)

A lane whose pending txs exceed its block-space share includes what fits
and **queues the rest in its own lane mempool for subsequent blocks**.
Excess veil txs do not spill into the general lane's space (or anywhere
else), and unused space in one lane is not redistributed to others. A
client seeing its tx miss a block during lane congestion should expect it
in a following block, not resubmit.

## Unordered txs × lanes (D-034)

Lane mempools index txs by (sender, sequence). Unordered txs
(`UnorderedTxExtension`, D-026) don't consume real sequences, so the lanes
index them by a synthetic per-tx value derived from the tx hash. Client
impact: none — submit unordered txs with any claimed sequence (including
the same one repeatedly); they will not evict each other and any number
from one sender can share a block, subject to lane space and Mandate
windows. Replay protection remains the dedup cache (D-028), exactly as in
task 0.5.

## Phase status

Veil and session lane routing is wired and consensus-validated since 0.6,
but those msgs cannot *execute* until their msg servers land (x/veil 0.7,
x/session 0.8) — CheckTx rejects them today (D-024), so they cannot enter
the mempool from the network yet. End-to-end (execution) lane coverage is
a 0.7/0.8 deliverable; grep `CONCLAVE-TODO(phase-0)` in
`app/lane_routing_integration_test.go`.
