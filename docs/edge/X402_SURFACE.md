# X402_SURFACE.md — the node-side contract for `conclave-x402-gateway`

**What this is.** The binding surface contract between the Conclave node and
the `conclave-x402-gateway` edge service (SPEC §9; PHASE2_DESIGN §2.3; D-083).
The gateway is a **separate repo**: an off-chain x402 facilitator that speaks
Coinbase's x402 HTTP handshake outward (402 → `X-PAYMENT` → verify →
`X-PAYMENT-RESPONSE`) and, inward, submits intents/transfers on Conclave on
behalf of paying agents, fronting gas and settling in `uusdc`. Its code and
tests live in its own repository and are **not** part of this repo's build.
This document is what that repository builds against: every endpoint, shape,
and authority rule the gateway may rely on, and nothing else.

**x402 protocol version posture (R4 advisory, PHASE3_PLAN §5.2).** The x402
Foundation targets **spec v1.0 in Q3 2026**; backward-compatibility
guarantees apply only after v1.0. The gateway targets the **v1.0 RC**, and
the outward x402 HTTP surface (header names, payload schemas, verification
flow) carries **pre-1.0 breakage risk** owned entirely by the gateway repo.
The node-side surface in this document does **not** depend on any x402
protocol detail — it is plain Conclave gRPC — so x402 spec drift never
invalidates this contract, only the gateway's outward facade.

**Contract stability.** Everything named here lives in `conclave.*.v1` proto
packages at pinned commit lineage `dccd607`→`aa21274`. Fields are never
renumbered; additive evolution only. If a future task changes a named
behavior, that task updates this document in the same session (CLAUDE.md
working method). *Task 2.6 changed two named behaviors without updating this
document; the omission was caught by the D-090 reconciliation audit and is
recorded in §3.4.*

---

## 1. The authority model (load-bearing)

**Submission authority is "hold a key + hold a mandate." There is no other
path.** The node exposes **no server-side signing endpoint** and no
submit-on-behalf-of RPC: the only way a transaction enters the chain is
`cosmos.tx.v1beta1.Service/BroadcastTx` carrying a transaction already signed
by the sending key. Whether that key may act, and for whom, is decided
on-chain by the chokepoint chain below — never by the gateway's own claims.

### 1.1 The chokepoint chain

Every gateway-submitted transaction passes, in order:

1. **Signature verification** (SDK ante). `MsgSubmitIntent.sender` is the
   msg's signer (`cosmos.msg.v1.signer`); an unsigned or wrongly-signed
   sender never reaches execution.
2. **Sigil binding** (`x/veil` keeper, SubmitIntent). `sender` must resolve
   to `sender_sigil` through the x/sigil **single-valued reverse index**
   (which includes mandate-bound delegate keys, D-017(d)); any mismatch is
   rejected with `ErrSigilMismatch` (codespace 13xx). A key can act for
   exactly **one** sigil per chamber.
3. **Mandate enforcement** (`x/mandate` ante, SPEC §6.2 — before execution,
   in CheckTx AND DeliverTx). For a mandate-bound signer: TypeURL allowlist
   (deny-by-default, nested msgs unwrapped recursively — wrapping cannot
   widen a mandate), per-tx spend ceiling bound to
   `spend_commitment + bond` (D-065 — the ante never reads give legs),
   rolling-window ceiling with **optimistic charge** (the stolen-key
   parallel-tx bound), expiry height, and revocation. The
   privilege-escalation guard forbids allowlisting
   `MsgIssueMandate`/`MsgRevokeMandate`/`MsgAmendMandate` — a mandate can
   never widen itself.
4. **Consent gates** (D-075/D-076, settlement side). A delivery condition on
   a gateway-submitted intent is consented because it is inside the signed
   payload of the mandate-bound tx — delegated consent, bounded by the
   mandate. The gateway can never impose a condition on a counterparty;
   those bindings are enforced against each party's own signed payload at
   solution validation and at the settlement boundary.

   **A signed condition IS a guarantee it will be attached — since task 2.7
   (D-094).** This paragraph previously warned the opposite, and the warning was
   accurate for the tree it described: the gates ran in ONE direction (they
   stopped a condition being imposed on you) and nothing required a condition you
   signed to be honored, so a solver could settle your intent unconditioned and
   the in-consensus fallback always did. **That is no longer true, and the
   correction is stated rather than the old text quietly deleted** (D-093 → D-094).
   What holds now:
   - `signed ⇒ attached` is enforced at solution validation AND at the settlement
     boundary. A settlement that drops a condition your principal signed is an
     invalid solution — **`ErrConditionNotHonored`, codespace `session` code
     `1429`** — and the solver is slashed 5%.
   - The in-consensus fallback matcher **attaches** your signed condition rather
     than dropping it. If the counterparty has not consented, the pair is left
     UNMATCHED — never downgraded to an unconditioned clearing.
   - **Consequence a gateway must plan for:** setting `IntentPayload.condition`
     now bounds what may be imposed on your principal AND obliges the protocol to
     deliver it, so a condition your counterparty will not accept means **no fill**
     (the intent rolls to TTL and its bond is refunded) rather than a silent
     unconditioned settlement. That is the intended trade.
   - **The buyer's side is a per-class RELATION, not equality**, because a buyer
     cannot pre-commit a HASHLOCK `payload_hash`. Sign the bound you actually
     want: HASHLOCK `timeout_blocks` is a **MAXIMUM** you accept (a longer one
     keeps your payment locked longer before auto-refund); OPTIMISTIC
     `dispute_window_blocks` is a **MINIMUM** (the window is your time to inspect
     and dispute before funds release); ZK_DELIVERY is **exact** on
     `schema_commitment` + `vk_id`. A `payload_hash` you leave empty is
     unconstrained; one you set must match exactly.
   - **Still true:** nothing but the fallback attaches conditions on a live
     network today — no third-party solver reference exists yet (task **2.14**),
     so conditioned trades currently clear via the fallback path.

**Binding stability (D-084).** A grantee key's sigil binding cannot be
changed by any third party: re-issuing a mandate to an already-bound grantee
is accepted only from the **same sigil** (`ErrGranteeConflict` otherwise),
making `IssueMandate`'s REPLACE consistent with `AmendMandate`'s existing
"cannot change the sigil" guard. A used delegate key stays burned to its
sigil (D-017 posture). Without this guard the gateway's hot-key binding
would be destructible by any sigil controller; the conformance suite holds
it load-bearing.

### 1.2 Deployment models (same node surface, different principal)

- **Model A — gateway-as-principal** (the SPEC §9 / Phase-2 exit-demo
  shape). The gateway operator registers its own Sigil and issues mandates
  from its controller key to the gateway's hot key(s). External x402 payers
  never acquire on-chain identity — they pay the gateway over x402, and the
  gateway is the on-chain buyer of record. Blast radius of a full gateway
  compromise: the hot keys' mandate ceilings plus the hot wallet balances —
  never other agents' funds or identities.
- **Model B — per-agent delegation.** A Conclave-native agent's controller
  issues the gateway's key a mandate under the **agent's** sigil, with
  agent-chosen ceilings/allowlist/expiry; the gateway then submits as that
  agent, inside bounds the agent set and can revoke in one tx. Because the
  reverse index is single-valued, **one hot key serves exactly one
  principal**: a gateway serving N principals runs N keys (e.g. one HD
  derivation per principal). Issuing a mandate to a key that controls a
  sigil is rejected (`ErrGranteeConflict`), so hot keys must be fresh keys,
  never controller keys.

**Who pays at settlement (custody split — load-bearing for Model B).** A
matched intent's coin **give** legs settle from the **sigil's controller
address**, not the submitting gateway key (`x/session` conclude.go resolves
each leg's sigil → `ControllerAddress`). So the gateway's key fronts only
**gas and the intent bond** (from its own balance); the **settlement funds
come from the principal's account** (the sigil controller). Consequences:
in Model A the gateway *is* the controller and settles from its own float;
in **Model B the delegating agent must hold the coin-give funds** — a
gateway cannot spend a principal's counterparty obligation out of its own
wallet, and a principal with an empty settlement balance simply fails to
settle (the match rolls, then expires; bonds refund). The gateway's
custody exposure is therefore bounded to its gas + bond float even though
it authors the intent.

### 1.3 What the node does NOT bound (gateway-repo obligations)

- **Gas/fee spend is not window-ceilinged.** The mandate ceilings bound
  *msg spend intent* (`spend_commitment + bond`), not fees. The fee-payer's
  loss bound is its balance. Consequence: a `fee_sponsor` (see §4) is
  exposed to fee drain by a compromised grantee up to the sponsor balance —
  the sponsor model is for principals who accept that; the default gateway
  posture is paying fees from its own hot wallet float.
- **Payment-before-submission ordering.** The node cannot know whether the
  gateway collected the x402 `X-PAYMENT` before fronting bond+gas. Collect
  first, submit second; reconciliation of collected uusdc vs fronted
  bond/gas is gateway accounting.
- **x402-side replay.** The node dedups transactions (§3.2); it does not
  dedup x402 payment headers. The gateway's replay-nonce store is its own.
- **Submit-time preconditions are ante-enforced (G2, D-085).** A
  mandate-bound `MsgSubmitIntent` that would fail a veil msg-server
  precondition (insufficient/under-min bond, wrong sigil, bad envelope,
  undecryptable epoch, wrong chamber, unfundable bond) is rejected in the
  **ante** (`SubmitIntentPrevalidationDecorator`), BEFORE the §6.2 window
  charge — so a would-fail submit charges nothing and never strands the
  mandate's optimistic commitment. The rejection error is the mirrored veil
  error (e.g. `ErrSigilMismatch`, `ErrBondFundsInsufficient`), returned from
  the ante. The gateway should still `Simulate` before broadcast (to surface
  the rejection without spending gas) and size bonds from a fresh
  `veil.Params` read, but a mistake here is now a clean rejection, not
  consumed headroom.

---

## 2. Chain constants

| Constant | Value |
|---|---|
| Chain ID (dev) | `conclave-dev-1` |
| Bech32 prefix | `conc` (accounts), `concvaloper` (validators) |
| Native/gas/bond denom | `uclv` (10^6 uclv = 1 CLV) |
| Settlement denom | **parameter**, canonically "uusdc": the literal denom `uusdc` on dev/localnet; the ICS-20 voucher `ibc/<hash>` for real USDC (task 2.5, D-082). The gateway takes it as configuration; mandate ceilings and prices bind per-denom, so the configured string must match what ceilings/receipts use on that network. |
| Intent bond denom | `uclv` (`x/veil` params `min_bond`) — bonds are NOT fee-abstracted; the gateway holds a uclv float for bonds (§3.1) |

---

## 3. Transaction surface (inward: what the gateway submits)

All submission is `cosmos.tx.v1beta1.Service` (standard SDK gRPC):
`Simulate` (gas estimation), `BroadcastTx`, `GetTx` (by hash). The node's
gRPC server is the standard `start`-served one; no custom tx service exists.

### 3.1 `conclave.veil.v1.MsgSubmitIntent`

| Field | Contract |
|---|---|
| `sender` | The gateway hot key's address; the tx signer. Must resolve to `sender_sigil` (§1.1 step 2). |
| `sender_sigil` | The principal's sigil id (Model A: the gateway's own; Model B: the delegating agent's). |
| `ciphertext` | The `IntentPayload` bytes — plaintext in the epoch-0/stub window, IBE-encrypted to the epoch key otherwise (see `epoch_id`). Contains `give`/`want` legs, optional `condition` (D-075/D-076 consent applies), `counterparty_class`. |
| `envelope` | Serialized `IntentEnvelope` (always plaintext): `ttl_height`, `fee_bid` (veil-lane priority, Dec string), **`allow_partial`** (leave UNSET for fill-or-kill — it became load-bearing on the in-consensus fallback path in task 2.6; see §3.4), `chamber_id` (0), **`spend_commitment`** (Coins — the declared upper bound on give-leg value; the ante binds ceilings to it, and at the Reveal an actual give exceeding it in any denom is INVALID with a 50% bond slash, D-065 — the gateway derives it from the quoted price, in the settlement denom), `epoch_id` (0 = stub window; non-zero must be a currently-decryptable epoch queried from `conclave.epoch.v1.Query`). |
| `bond` | uclv; `≥ max(min_bond, bond_bps × faceValue(spend_commitment))` — both params from `conclave.veil.v1.Query/Params`. Escrowed from `sender`'s account. Refunded on CONCLUDED/EXPIRED/CANCELLED; 50% slashed on INVALID. |
| `chamber_id` | 0 until Phase 3. |

Response: `intent_id`. The submit also appears as event
`intent_submitted{intent_id, sender_sigil, owner, bond, ttl_height}`.

**Intent lifecycle the gateway observes** (`IntentState`):
`SUBMITTED → SEALED_IN_SESSION → OPEN → CONCLUDED` (success; bond refunded)
with the alternates `ROLLED` (unmatched, re-enters next session), `EXPIRED`
(ttl reached; bond refunded), `INVALID` (Reveal failed or commitment
violated; 50% bond slash), `CANCELLED` (owner-cancelled pre-OPEN or
session-cancel; bond refunded).

### 3.2 Replay/nonce model: unordered transactions

The gateway submits with `conclave.unordered.v1.UnorderedTxExtension` in
`TxBody.extension_options` (ADR-070 pattern, D-026): the tx carries
`timeout_timestamp` instead of relying on the account sequence; replay
protection is the app-level TTL'd dedup cache of tx hashes (TTL capped at
10 minutes). This is what lets **one hot key submit many txs in parallel in
one block** with no sequence contention — and it is the on-chain half of
double-settlement protection: an identical tx hash inside the TTL is
rejected. Standard sequence-based txs also work (e.g. for the controller's
mandate management), but the gateway's submission path should be unordered.

### 3.3 Other permitted msgs

- `conclave.veil.v1.MsgCancelIntent` — owner-only, pre-OPEN; refunds bond.
- `cosmos.bank.v1beta1.MsgSend` — direct uusdc transfers (ante values it as
  settled spend against the window ceiling).
- Mandate management (`conclave.mandate.v1.MsgIssueMandate` /
  `MsgRevokeMandate` / `MsgAmendMandate`) is **controller-side**, signed by
  the principal's controller key — never by (or allowlistable to) the
  gateway hot key.

A Model-A/B gateway mandate allowlist is therefore typically exactly:
`{/conclave.veil.v1.MsgSubmitIntent, /conclave.veil.v1.MsgCancelIntent,
/cosmos.bank.v1beta1.MsgSend}`.

### 3.4 Matcher behavior that became gateway-visible in task 2.6 (D-089)

Two payload/envelope fields the gateway may set changed from inert to
load-bearing in `aa21274`. Both default to safe; the risk is setting them
without knowing they now bite.

**(a) `IntentLeg.limit` is now ENFORCED.** Before 2.6 the §5.1 `limit` field
was signed and read by nobody — the matcher bounded quantities only, so a
gateway could set it and see no effect. It is now honored as a **rate** bound on
the party's own achieved terms, computed from settled flows alone (no price
source): a give leg bounds `given/recv ≤ limit`, a want leg bounds
`recv/given ≥ limit`, compared exactly by cross-multiplication. Two rejections
the gateway can now receive at settlement time:

- **`ErrLimitViolation`** (session `1416`) — the settled rate breaches a signed
  limit. The intent does not settle; it ROLLs or EXPIREs normally.
- **`ErrLimitRateUnsupported`** (session `1428`) — the limit's rate is **not
  evaluable for the match shape**, specifically when the intent's counter-side
  carries **more than one distinct asset** (no single counter-asset to divide
  by). The match is REJECTED rather than settled against an unhonored limit.

**Gateway posture:** a `limit` is now a real price bound and is the correct way
to express "do not fill me worse than my quote" — but only set it on intents
whose counter-side is a single asset, which is the normal x402 shape (pay
`uusdc`, receive one data/service leg). A quoted purchase is otherwise already
bounded by `spend_commitment` (§3.1), which is enforced at the Reveal.

**(b) `allow_partial` is now reachable through the IN-CONSENSUS fallback.**
Previously partial fills could only arise from solver-proposed solutions; the
fallback matcher always cleared at full give. It now tries FULL-GIVE first (so
nothing that cleared pre-2.6 changes) and then a **FLOOR-FILL** in which a party
that signed `allow_partial` hands over only what its counterparty's want-floor
needs and keeps the remainder. **For an x402 purchase, leave `allow_partial`
unset** — the default — so the buy is fill-or-kill: a partially-filled purchase
would collect full payment semantics for a partial good. The fill-or-kill
guarantee is enforced on-chain by §6.4 rule 3 (D-089). If you ever do set it,
read settled amounts from the receipt's Match pre-image, never from the intent
you submitted.

---

## 4. Fee/gas surface: fronting gas without holding it in CLV

`x/mandate`'s `FeeAbstractionDecorator` (SPEC §6.2) lets a mandate-bound fee
payer pay gas **directly in a listed fee denom** (the mandate's
`fee_denoms`, e.g. `["uusdc"]`), settled through the genesis-funded CLV fee
pool (no mint/burn). The contract:

- Abstraction applies iff the fee is a **single coin** of a listed non-native
  denom and the payer's mandate is live. The converted amount is deducted
  from **`fee_sponsor` if set, else the grantee** (§1.3 caveat), and the
  pool pays the fee collector in uclv.
- **Conversion rate**: the session TWAP (`conclave.session.v1.Query/Twap`,
  D-082) when the pair is warm; otherwise the deterministic fixed
  `fee_conversion_rate` param (`conclave.mandate.v1.Query/Params`). The
  gateway quotes gas in uusdc off the same `Twap` query (`rate` field =
  base-per-quote, the §6.2 orientation) with the fixed rate as its own
  cold fallback.
- A mandate-bound payer NOT using abstraction may pay fees **only in uclv**
  (`ErrFeeDenomNotPermitted` otherwise — D-022 deny-by-default).
- Intent **bonds are not fees**: they are escrowed uclv (§3.1). The
  gateway's float is therefore two-denom: uusdc (fees + settlement) and a
  small uclv reserve (bonds, refunded on the happy path).

---

## 5. Query surface (outward: what the gateway reads)

| Service / RPC | Request → Response | Gateway use |
|---|---|---|
| `conclave.veil.v1.Query/Intent` | `{id}` → `IntentRecord{intent, state, session_id, owner, charged_spend}` | poll lifecycle of a submitted intent |
| `conclave.veil.v1.Query/Params` | `{}` → `{min_bond, bond_bps, invalid_slash_bps, …}` | bond sizing before submit |
| `conclave.session.v1.Query/CurrentSession` | `{chamber_id}` → `Session` | session boundary/timing for quote TTLs |
| `conclave.session.v1.Query/Session` | `{id}` → `Session` | session an intent sealed into |
| `conclave.session.v1.Query/Twap` | `{base, quote}` → `{twap, rate, …}` (Dec strings; zero = cold) | uusdc↔uclv gas quoting; price sanity |
| `conclave.receipt.v1.Query/Receipts` | `{sigil_id, pagination}` → `[Receipt]` | enumerate settlements for the principal's sigil |
| `conclave.receipt.v1.Query/Receipt` | `{id}` → `Receipt{id, session_id, intent_id, sigil_id, counterparty_sigils, terms_hash, clearing_prices, condition_outcome, height}` | settlement confirmation payload. ⚠️ `condition_outcome` is currently ALWAYS `NONE` — never written for escrow outcomes, even on conditioned settlements; a receipt proves CONCLUDE, not delivery completion (D-087; fix owned by task 2.9 — see [7683_SURFACE.md](7683_SURFACE.md) §6.4) |
| `conclave.receipt.v1.Query/ReceiptProof` | `{id}` → `{receipt, proof (ICS-23 ProofOps), proof_height}` | the verifiable settlement evidence for `X-PAYMENT-RESPONSE` (verifiable against a Conclave consensus root by any IBC counterparty) |
| `conclave.mandate.v1.Query/MandateByGrantee` | `{grantee}` → `Mandate{…ceilings, window_spent, window_start_height, expiry_height, revoked, fee_denoms…}` | the gateway reads its own remaining headroom before quoting |
| `conclave.sigil.v1.Query/SigilByAddress` | `{addr}` → sigil | verify a key's binding (its own, or a counterparty's) |
| `conclave.epoch.v1.Query` (current epoch / pubkey) | — | fetch the epoch key + `epoch_id` to encrypt non-stub intents |
| `cosmos.auth.v1beta1.Query/Account`, `cosmos.bank.v1beta1.Query/Balance` | standard | account existence, float monitoring |
| `cosmos.tx.v1beta1.Service/GetTx` | `{hash}` → tx result | broadcast confirmation |

All queries are served over committed state; none is consensus-affecting.
Sealed payloads are opaque pre-Reveal by construction — `ciphertext` in a
queried `IntentRecord` reveals nothing the submitter didn't already know.

---

## 6. The x402 flow mapped to this surface (informative)

1. **Quote (402 challenge):** price the resource in uusdc; read
   `veil.Params` (bond), `session.Twap` (gas quote), and
   `MandateByGrantee` (headroom). Quote TTL ≤ session cadence.
2. **Collect:** verify `X-PAYMENT` (x402-side; gateway repo), collect
   uusdc. **Only then:**
3. **Submit:** build `IntentPayload` (want = the data/service leg;
   condition per the seller's published terms), `IntentEnvelope` with
   `spend_commitment` = quoted price, bond per §3.1, unordered extension
   per §3.2; `BroadcastTx`.
4. **Watch:** poll `veil.Query/Intent` until `CONCLUDED` (or a failure
   state → refund path in the gateway).
5. **Confirm:** locate the receipt (`Receipts` by the principal's sigil at
   `session_id`, or via the settlement events), fetch `ReceiptProof`.
6. **Respond:** `X-PAYMENT-RESPONSE` carrying receipt id + proof +
   `proof_height` (+ `clearing_prices` as the effective execution price).

Failure accounting: `EXPIRED`/`CANCELLED`/`ROLLED`-past-quote-TTL → bond
refunded on-chain, x402 payment refunded by the gateway; `INVALID` → 50%
bond slash is a **gateway defect** (it authored the payload) and must never
be passed to the payer.

**Conditioned purchases (D-087 caveat):** for a buy whose settlement leg
carried a delivery condition (e.g. the ZK data path), the receipt at step 5
proves the escrow was OPENED, not that delivery completed — and its
`condition_outcome` field reads `NONE` regardless (never updated on escrow
resolution; task 2.9). A gateway confirming conditioned purchases must
watch the `x/escrow` record to its terminal state before treating the
payment as delivered. Unconditioned coin/service settlements are final at
the receipt.

---

## 7. Conformance (what pins this contract in-tree)

- **`app/x402_surface_conformance_test.go`** (in `make test`) — the
  load-bearing auth proof, from a gateway-shaped fixture: a mandate-bound
  hot key CAN submit for its bound sigil (ceiling-bound, fee paid in uusdc
  through abstraction) and CANNOT submit as any other sigil
  (`ErrSigilMismatch`); an unbound key cannot submit for a sigil at all; an
  allowlist-less mandate is denied in ante; an over-ceiling commitment is
  denied; the D-084 binding-stability guard holds (cross-sigil re-issue
  rejected, `ErrGranteeConflict`).
- **`app/x402_surface_conformance_test.go`** also carries
  `TestX402FailedSubmitDoesNotStrand_G2Closed` — the D-085 proof that a
  would-fail submit is rejected in the ante and charges nothing (§1.3), with the
  conservation invariant asserted GREEN and a valid submit still landing and
  still charging. Watched red by commenting out the decorator in `app/ante.go`.
- **`x/session/keeper/limit_rate_test.go`** (in `make test`) — pins §3.4(a):
  give/want rate breaches rejected, the multi-counter-asset shape rejected with
  `ErrLimitRateUnsupported`, malformed/negative limits rejected.
- **`scripts/x402_surface_check.sh`** (localnet, not in `make test`) —
  exercises every endpoint named in §3–§5 once against a running node
  (PHASE2_DESIGN §2.3 acceptance).

*Authored task 2.3 (D-083/D-084/D-085). Updated at PHASE2_DESIGN Rev 3 (D-090)
with the task-2.6 matcher changes (§3.4) — the update task 2.6 owed this
document and did not make. The gateway repo pins the commit of this document it
builds against; changes land here first.*
