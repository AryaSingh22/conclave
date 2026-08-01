# 7683_SURFACE.md — the node-side contract for `conclave-7683-resolver`

**What this is.** The binding surface contract between the Conclave node and
the `conclave-7683-resolver` edge service (SPEC §9; PHASE2_DESIGN §2.4,
FLAG C; D-086/D-087). The resolver is a **separate repo**: an off-chain
filler that consumes ERC-7683 cross-chain orders from EVM origin chains,
translates them into `SealedIntent`s filled through Conclave sessions, and
posts the fills back to each origin chain with a receipt proof through a
per-origin verification bridge. Its code and tests live in its own
repository and are **not** part of this repo's build. This document is what
that repository builds against: every endpoint, shape, and authority rule
the resolver may rely on — and, load-bearingly, what the settlement
evidence does **not** prove (§6.3).

**ERC-7683 version posture (R4, PHASE3_PLAN §5.2).** ERC-7683 is the
production-dominant cross-chain intent standard (Across — 88% of its
volume, UniswapX, CoW, Eco; the Open Intents Framework spans 30+ teams).
The **OIF is the reference integration frame for the resolver repo.** The
node-side surface in this document depends on **no ERC-7683 protocol
detail** — it is plain Conclave gRPC — so 7683 spec evolution never
invalidates this contract, only the resolver's origin-chain facade. The
same argument, and the same division of breakage risk, as
[X402_SURFACE.md](X402_SURFACE.md)'s x402 posture.

**Contract stability.** Everything named here lives in `conclave.*.v1`
proto packages at pinned commit lineage `8756575`→`aa21274`. Fields are
never renumbered; additive evolution only. If a future task changes a named
behavior, that task updates this document in the same session (CLAUDE.md
working method).

**The task-2.6 warning is now DISCHARGED (D-090).** This contract used to warn
that the fallback matcher's internals (pairing rules, pricing) were *scheduled*
to change. **Task 2.6 changed them** (`aa21274`, D-089), and the warning's
substantive claim held: nothing in this contract depends on matcher internals,
because the evidence chain reads *stored* state (§5) and never re-derives
matcher output — the `terms_hash` pre-image is still fetched, not recomputed.
Two consequences ARE resolver-visible, though, and 2.6 did not record them here;
they are §3.2 below. (2.6 shipped without updating either edge contract; the
omission was caught by the D-090 reconciliation audit and is fixed here rather
than left for the resolver repo to discover at integration time.)

---

## 1. The authority model

### 1.1 The chokepoint chain (normative by reference)

Submission authority is **"hold a key + hold a mandate," and nothing
else** — the node exposes no server-side signing and no
submit-on-behalf-of RPC. The full chokepoint chain (SDK signature
verification → the single-valued sigil reverse index, `ErrSigilMismatch` →
the §6.2 mandate ante with allowlist/ceilings bound to
`spend_commitment + bond` → the D-075/D-076 consent gates), the D-084
binding-stability guard, and the D-085 ante pre-validation are specified in
**[X402_SURFACE.md](X402_SURFACE.md) §1.1**, which is normative for this
contract too. The resolver is just another mandate-bound client of the same
chain.

### 1.2 Translation authority — the honest claim

**A foreign ERC-7683 order becomes an on-chain SealedIntent under the
RESOLVER'S OWN authority, and nothing else.** There is no on-chain binding
between an intent and an origin-chain order:

- **Nothing on Conclave attests that a submitted fill corresponds to a real
  order.** A malicious or compromised resolver can mint intents that
  correspond to no order at all. Its blast radius is exactly its mandate —
  TypeURL allowlist, per-tx and rolling-window ceilings, expiry,
  revocation — plus its own settlement funds (§1.3): give legs settle from
  the sigil controller's account. It **cannot** forge other agents' orders
  or intents; that requires their keys and mandates (`ErrSigilMismatch`;
  the D-084 rebind guard keeps hot-key bindings stable). The conformance
  suite pins this honestly: an order-less fill settles normally, bounded
  only by ceiling and funds (§7).
- **Deliberately absent: an `origin_order_id` field.** Adding one would be
  decoration, not binding (METHODOLOGY Rule 4): it would be
  resolver-attached, and the chain cannot verify EVM order authenticity,
  so a malicious resolver could stamp any order's id on any settlement.
  The bridge must verify the settled legs against the order regardless
  (§6.3 check 3) — at which point the field adds nothing. Do not propose
  it back; the reasoning is recorded in D-086.

**Deployment models.** **Model A — resolver-as-principal — is the
canonical ERC-7683 filler shape**: the resolver operator registers its own
Sigil, mandates its hot key(s), fronts its own destination liquidity into
fills, and is reimbursed on the origin chain by the settler contract after
proving the fill. Model B (per-agent delegation, one hot key per principal)
exists identically to X402_SURFACE.md §1.2 but is secondary for 7683.

**Custody split (X402_SURFACE.md §1.2, load-bearing here too).** The hot
key fronts only **gas + the intent bond**; a fill's coin give legs settle
from the **sigil controller's account**. In Model A the resolver operator's
account is therefore the destination-liquidity float, and a full hot-key
compromise is bounded by the mandate ceilings — never the whole float
directly.

### 1.3 What the node does NOT bound (resolver-repo obligations)

All of X402_SURFACE.md §1.3 applies (fee spend not window-ceilinged;
node-side dedup ≠ origin-side replay protection; ante pre-validation per
D-085). Translation-specific additions:

- **Fill-first risk is the resolver's.** The origin settler holds the
  user's deposit; the resolver fills on Conclave first and claims
  reimbursement second. Order-expiry/reorg handling on the origin chain is
  resolver accounting, invisible to Conclave.
- **The order→intent translation itself** (which order shapes to accept,
  counterparty class, TTL vs `fillDeadline` mapping) is a resolver-repo
  plan-stop (PHASE2_DESIGN §2.4). This contract constrains only its
  enforcement boundary (§3).

---

## 2. Chain constants

Identical to **[X402_SURFACE.md](X402_SURFACE.md) §2** (chain-id, bech32
`conc`, `uclv` gas/bond denom, the settlement denom as a parameter —
literal `uusdc` on dev, `ibc/<hash>` for real USDC per D-082).

---

## 3. Transaction surface (inward: what the resolver submits)

Submission is `cosmos.tx.v1beta1.Service` with the D-026 unordered
extension — the replay/nonce model of X402_SURFACE.md §3.2. The
recommended resolver mandate allowlist is exactly
`{/conclave.veil.v1.MsgSubmitIntent, /conclave.veil.v1.MsgCancelIntent}` —
a fill resolver has no `MsgSend` business.

### 3.1 The translated fill (`conclave.veil.v1.MsgSubmitIntent`)

Field contract as X402_SURFACE.md §3.1; the 7683-specific rules:

- **Shape.** `give` = the destination outputs the resolver delivers (coin
  legs, from its principal's float); `want` = what the fill takes back out
  of the session (the Conclave-native counterpart). The fill clears as an
  ordinary sealed-bid session match — 7683 standardizes how the resolver
  CONSUMES orders, not the settlement venue (SPEC §9).
- **`spend_commitment` derivation (D-065).** Declare **at least the coin
  give legs, per denom** — the order's Conclave-side max-spend equivalent.
  The ante binds ceilings to `spend_commitment + bond` and never reads give
  legs, so a resolver **can** under-declare — but an under-declared fill
  **can never settle**: at the Reveal, an actual coin give exceeding the
  commitment in any denom turns the intent INVALID *before matching*
  (`x/veil` lifecycle, D-065 cross-check), slashes 50% of the bond, and the
  mandate window **retains** the charged commitment+bond (conservation
  posture). Under-declaration is self-punishing, not a ceiling escape —
  pinned end-to-end by `TestERC7683UnderDeclaredFillNeverSettles` (§7).
  Boundary carried from AUDIT F-2: the cross-check bounds **coin** legs
  only; data/service give legs are un-commitment-valued
  (`TestSpendIntentF2BoundaryPin`; task 3.4 flips it). The fill direction
  is coin-give, so the bound is real where this contract needs it.
- **Conditions on translated intents (PHASE2_DESIGN §2.4 consent rule).**
  A delivery condition inside the resolver's signed payload is *delegated
  consent*, bounded by its mandate (D-075/D-076) — the resolver signs what
  its principal authorized, or submits unconditioned. **For the v1
  fill-back rail, submit unconditioned**: a conditioned leg's funds sit in
  escrow after CONCLUDE and may later refund, so its receipt is not
  delivery evidence (§6.4), and the mandated bridge check rejects it
  (§6.3 check 3).
  ⚠️ **The reason this recommendation costs you nothing has CHANGED; the
  recommendation has not** (D-093 → D-094, task 2.7). This paragraph used to say
  a signed condition was not a guarantee it would be attached — accurate for the
  tree it described, and **no longer true**. Since 2.7, `signed ⇒ attached` is
  enforced at solution validation AND at the settlement boundary
  (`ErrConditionNotHonored`, codespace `session` code `1429`, solver slashed 5%),
  and the in-consensus fallback matcher ATTACHES a signed condition instead of
  dropping it. **The correction changes the cost, and a resolver should read it
  that way: signing a condition now costs you FILLS, not protection.** A
  condition the counterparty has not consented to leaves the pair UNMATCHED — the
  intent rolls to TTL and its bond is refunded — rather than clearing
  unconditioned with the payment going straight to the seller. So "submit
  unconditioned for the v1 fill-back rail" still stands: now because a
  conditioned intent may simply not clear, and still because a conditioned leg's
  funds sit in escrow after CONCLUDE and its receipt is not delivery evidence.
  A reference solver-side attachment path remains task **2.14**.
- **Bond sizing, lifecycle states, cancel:** X402_SURFACE.md §3.1/§3.3.

### 3.2 Matcher behavior that became resolver-visible in task 2.6 (D-089)

Two changes in `aa21274` are visible to a filler. Neither breaks this
contract's evidence chain (§6), but both change what a submitted fill can DO.

**(a) `IntentLeg.limit` is now ENFORCED — and it can reject your fill.** Before
2.6 the §5.1 `limit` field was signed by the agent and read by nobody; the
matcher bounded quantities only. It is now honored as a **rate** bound on each
party's own achieved terms, computed from settled flows alone (no external price
source): a give leg bounds `given/recv ≤ limit`, a want leg bounds
`recv/given ≥ limit`. Comparison is exact by cross-multiplication — nothing
rounds. Two new rejections a resolver can now hit:

| Error | Codespace | When |
|---|---|---|
| `ErrLimitViolation` | session `1416` | the settled rate breaches a limit some party signed |
| `ErrLimitRateUnsupported` | session `1428` | the limit's rate is **not evaluable** for the match shape |

**`ErrLimitRateUnsupported` is the one that will surprise a 7683 resolver**,
because multi-output orders walk straight into it. A leg's rate is undefined
when the intent's counter-side carries **more than one distinct asset** — there
is no single counter-asset to divide by, and valuing a basket would need exactly
the price lens this check refuses. Such a match is **REJECTED rather than
settled against an unhonored limit**. Concretely: if you translate a
multi-output order into a fill that gives two or more distinct denoms and you
put a `limit` on a want leg, the match is rejected. **Recommended posture for
the v1 fill-back rail: leave `limit` unset on translated fills** and bound your
economics origin-side, where you control them. If you do set it, set it only on
fills whose counter-side is a single asset. (Malformed and negative limits fail
the same way. Lifting the multi-asset restriction is task 2.7/FW-2's per-leg
field.)

**(b) Partial fills are now reachable through the IN-CONSENSUS fallback.**
Before 2.6 the fallback matcher only ever cleared a pair at full give; partial
fills existed solely in solver-proposed solutions. `tryFallbackPair` now tries
FULL-GIVE first — so anything that cleared before 2.6 still clears at identical
terms — and falls back to a **FLOOR-FILL** in which a party that signed
`allow_partial` hands over only what its counterparty's want-floor needs and
**keeps the remainder**. Consequences for a resolver:

- **`allow_partial` in your envelope is now load-bearing.** Setting it means a
  fill may settle for less than the full give you offered. Leave it **unset**
  (the default) for fill-or-kill semantics — that is the correct posture when an
  origin-chain order must be filled exactly or not at all. The fill-or-kill
  guarantee is enforced on-chain by §6.4 rule 3, proven load-bearing in D-089.
- **A settled `Match` leg amount may be less than the intent's offered
  quantity.** Read fill amounts from the **stored Match** (§5), never from your
  own submitted intent, when constructing the fill-back claim. §6.3 check 3 —
  "some leg pays the order's recipient at least `minReceived`" — already reads
  the verified Match, so a bridge implementing this contract correctly is
  unaffected; a bridge that shortcuts by trusting the submitted intent is not.
- A partially-filled intent is marked CONCLUDED like any other; its remainder is
  never re-offered into a later session.
- **The evidence chain stays uniform across full and partial fills** — see §6.1.

---

## 4. Fee/gas surface

Identical to **[X402_SURFACE.md](X402_SURFACE.md) §4** (fee abstraction in
listed `fee_denoms` through the CLV pool; TWAP-quoted; bonds are uclv and
never fee-abstracted).

---

## 5. Query surface (outward: what the resolver reads)

The full X402_SURFACE.md §5 table applies. The rows this contract
additionally leans on:

| Surface | Contract |
|---|---|
| `conclave.session.v1.Query/Session` `{id}` | `winning_solution_id` names the settled clearing — **set for BOTH clearing paths**: solver-won sessions since 0.8, and **fallback-cleared sessions since D-086** (this task added the persistence; it was previously `""` for fallback). |
| `conclave.session.v1.Query/Solutions` `{session_id}` | Returns the stored solutions, including the winning one. **This is the `terms_hash` pre-image source** (§6). For a fallback clearing the record is synthetic: `solver = ""`, `score = 0`, `id = hex(sha256(0x12 ‖ chamber_be4 ‖ session_id_be8))` — derivable offline. Sessions and solutions are never pruned. |
| `receipt_emitted` event | `{receipt_id, intent_id, sigil_id}` at the CONCLUDE block — the fastest receipt locator. |
| Receipt id, offline | `hex(sha256(0x11 ‖ chamber_be4 ‖ session_id_be8 ‖ intent_id_utf8))` — the resolver can derive its receipt id from `(session_id, intent_id)` without any query. |
| `conclave.receipt.v1.Query/ReceiptProof` `{id}` | The fill-back evidence (§6). |
| `conclave.escrow.v1.Query/Escrow`/`Escrows` | *Informative:* terminal state of conditioned settlements — outside the v1 fill-back rail (§6.4). |

---

## 6. The fill-back evidence contract (the core of this surface)

### 6.1 What `ReceiptProof` proves

An ICS-23 existence proof that the **exact marshaled `Receipt` bytes** sat
under the `0x08|receipt_id` key of the session store at `ProofHeight`,
chained value → session IAVL root → **Conclave app hash** (D-053). The
proven value pins every receipt field:

`Receipt{ id, session_id, intent_id, sigil_id, counterparty_sigils,
terms_hash, clearing_prices, condition_outcome, height, chamber_id }`

- `terms_hash = sha256(Match bytes)` — a commitment to the **full settled
  terms**: the matched intent set, every directed leg (from/to sigils,
  asset, **per-leg condition**), and the clearing prices. The pre-image is
  the winning `Match` inside the stored winning `Solution` (§5), byte-
  reproducible via deterministic proto marshaling.
- **Uniformity holds across full AND partial fills (D-089).** A partial fill
  produces a Match whose leg amount is the FILLED quantity, not the intent's
  offered quantity — a genuinely new pre-image shape. The persisted fallback
  Solution carries the filled amount, and `sha256(Marshal(queried Match))`
  recomputes to each party's `receipt.terms_hash` for that shape too, so the
  `Session → winning_solution_id → Solutions → Match` chain needs no special
  case. Pinned by `TestFallbackPartialFill_PreImageUniformity` (§7).
- ⚠️ **`condition_outcome` is currently decorative — never read it**
  (D-087, task 2.9): the only writer stamps `CONDITION_OUTCOME_NONE`
  unconditionally, including for settlements whose legs DID carry
  conditions; `RELEASED/REFUNDED/DISPUTED` are never written. §6.4.

### 6.2 Height semantics (normative for the bridge)

- A height-0 (tip) `ReceiptProof` query is served at the **last
  header-anchored version**: `ProofHeight = latest_committed − 1`. The
  verifying root is Conclave's app hash **at `ProofHeight`**, which is the
  `AppHash` field of the **block header at `ProofHeight + 1`** (CometBFT
  convention) — that is what a light client supplies.
- A receipt written at height `H` is therefore provable from the moment
  header `H+1` exists — one block after settlement.
- Proofs are also obtainable at historical heights via the generic ABCI
  query (`path=/store/session/key, prove=true, height=H`) on any unpruned
  node; since receipts persist, re-fetching a *fresh* proof at the current
  tip is always available and is the recommended posture when the origin
  light client has advanced.

### 6.3 What the proof does NOT prove — and the MANDATED bridge checks

The proof proves a settlement **happened on Conclave**. Each property below
is NOT provided by the proof; the bridge (origin settler) MUST implement
the paired check. The conformance suite demonstrates each gap by showing
the forged/replayed claim is **accepted when the check is skipped** (§7).

1. **Root authenticity.** The proof chains to *an* app hash; only a
   trusted **Conclave light client per origin chain** (the verification
   bridge's trust root — resolver-repo scope) makes it *Conclave's* app
   hash at `ProofHeight`. Verify: ICS-23 `ConvertProofs` →
   `VerifyMembership(GetSDKSpecs, root@ProofHeight, ["session", 0x08‖id],
   receipt_bytes)`.
2. **Terms authenticity.** The receipt commits to terms by hash only.
   Fetch the winning `Match` (§5), recompute
   `sha256(match_bytes) == receipt.terms_hash`, and check
   `receipt.intent_id ∈ match.intent_ids`.
3. **Order ↔ fill correspondence — the binding lives HERE, not on
   chain (§1.2).** Some leg of the verified `Match` must pay the order's
   recipient sigil at least the order's `minReceived` of the order's denom,
   **with `condition` unset** — a conditioned leg's funds are escrowed and
   may refund (§6.4), so it is not delivery. Without this check, **any
   genuine receipt claims any order** (conformance: the wrong-settlement
   claim is accepted with the leg check skipped).
4. **First-claim uniqueness.** The proof is a fact, replayable verbatim.
   The settler MUST enforce **one-claim-per-`receipt_id`**. Without it, one
   fill reimburses unboundedly many identical orders (conformance: the
   replay is accepted with dedup skipped). This is a bridge obligation, not
   a node property — the node's tx dedup (X402_SURFACE.md §3.2) is
   unrelated.

### 6.4 `condition_outcome` — the D-087 disclosure (read this)

`Receipt.condition_outcome` documents escrow outcomes
(`RELEASED/REFUNDED/DISPUTED`) but **no code path has ever written any
value except `CONDITION_OUTCOME_NONE`** — including for settlements whose
legs carried HASHLOCK/OPTIMISTIC/ZK conditions (real since task 2.1), and
nothing updates a receipt when its escrow later resolves. Consequences:

- A receipt is evidence that **CONCLUDE happened**, not that delivery
  completed. For a conditioned leg, the funds sit in `x/escrow` and may
  REFUND after the receipt exists, with the receipt unchanged.
- A bridge that reads `condition_outcome == NONE` as "unconditional final
  settlement" will reimburse fills whose funds later refunded.
- **v1 rail rule:** accept only fills whose relevant legs are
  **unconditioned coin legs**, checked from the terms pre-image the bridge
  already verifies (§6.3 check 3). Conditioned-delivery fill-back (via the
  escrow query surface or a fixed `condition_outcome`) is out of scope
  until **task 2.9** wires the receipt's outcome to escrow resolution.

### 6.5 The fill flow mapped to this surface (informative)

1. **Ingest:** watch origin settlers for opened orders (OIF frame;
   resolver repo).
2. **Translate:** order outputs → `give`, counterpart → `want`;
   `spend_commitment` ≥ coin give (§3.1); unconditioned; bond from
   `veil.Params`; unordered extension.
3. **Submit** via `BroadcastTx`; watch `veil.Query/Intent` to
   `CONCLUDED` (or handle `ROLLED`/`EXPIRED`/`INVALID` — an INVALID is a
   resolver defect: it authored the declaration).
4. **Gather evidence:** locate the receipt (event, `Receipts(sigil)`, or
   the offline id derivation), fetch `ReceiptProof` one block after
   settlement, fetch `Session → winning_solution_id → Solutions` for the
   Match pre-image.
5. **Present** `{receipt bytes, proof, proof_height, match bytes}` to the
   origin settler, which runs §6.3's four checks against its Conclave
   light client and pays out once.

---

## 7. Conformance (what pins this contract in-tree)

- **`app/erc7683_surface_conformance_test.go`** (in `make test`) — the
  resolver-shaped proof of §1/§3/§6 through real `FinalizeBlock+Commit`
  blocks:
  - the full evidence chain end-to-end on **fallback-cleared** settlements
    (the no-solver default): receipt → ICS-23 verify against the recorded
    app root at `ProofHeight` → `terms_hash` recomputed from the *queried*
    winning Solution — the D-086 **uniformity proof** (this test was
    watched RED against pre-D-086 HEAD, where the fallback stored no
    Solution and `winning_solution_id` was empty);
  - the §6.3 skip-variant demonstrations, permanent and in-tree: the
    wrong-settlement claim fails **the leg check specifically** and is
    ACCEPTED with the leg check skipped; the cross-order replay fails
    **dedup specifically** and is ACCEPTED with dedup skipped; stale-root
    evidence fails the proof check (D-053 height binding re-checked at app
    level);
  - the §1.2 non-binding pin: an order-less fill settles (if this ever
    goes red, an order-binding was added and §1.2 must be rewritten);
  - the ante chokepoints from the resolver fixture (over-ceiling
    `ErrMandateExceeded`, foreign-sigil and unbound-key
    `ErrSigilMismatch`);
  - `TestERC7683UnderDeclaredFillNeverSettles` — the §3.1 D-065 boundary:
    under-declared fill passes the ante, INVALID at the Reveal, 50% bond
    slash, no receipt, window charge retained (watched RED with the Reveal
    cross-check bypassed; run recorded in D-086).
- **`x/session/keeper/partial_fill_test.go`** (in `make test`) —
  `TestFallbackPartialFill_PreImageUniformity` pins §6.1's uniformity claim for
  the **partial** shape: it queries the persisted fallback Solution, asserts the
  stored leg carries the FILLED amount (not the offered one), and recomputes
  `terms_hash` against BOTH parties' receipts. **Coverage boundary, stated
  honestly:** this runs at the keeper level. The app-level chain in
  `erc7683_surface_conformance_test.go` — query → marshal → ICS-23 verify
  against a real header-anchored root — still exercises **full fills only**. So
  the partial shape's pre-image identity is proven, but not yet through the
  full external evidence path a bridge actually walks. A bridge implementing
  §6.3 as written is unaffected (it reads the stored Match either way); closing
  the asymmetry is a candidate for the §6 exit demo or task 2.10.
- **`x/session/keeper/limit_rate_test.go`** (in `make test`) — pins §3.2(a):
  give/want rate breaches rejected, the multi-counter-asset shape rejected with
  `ErrLimitRateUnsupported`, malformed/negative limits rejected, and the
  zero-denominator boundary.
- **`scripts/7683_surface_check.sh` + `conclave-demo 7683-check`**
  (localnet; dev tooling, not in `make test`) — the live sweep: every
  endpoint named in §3/§5 against a running node, the evidence chain
  gathered and verified exactly as §6.5, and the live negatives.

*Authored task 2.4 (D-086 surface + fallback-solution persistence; D-087
condition_outcome disclosure). Updated at PHASE2_DESIGN Rev 3 (D-090) with the
task-2.6 matcher changes §3.2 — the update task 2.6 owed this document and did
not make. The resolver repo pins the commit of this document it builds against;
changes land here first.*
