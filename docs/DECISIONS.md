# Conclave Decisions Log

ADR-style log of field-level and behavioral decisions the SPEC does not fully
pin down. Every such decision is appended here **in the same task that makes
it**. Format: number, date, decision, rationale, SPEC section affected.
The SPEC remains the source of truth; entries here fill gaps, never override.

---

## D-001 — IntentRecord wrapper for intent lifecycle state

- **Date:** 2026-06-11 (task 0.2)
- **SPEC:** §5.1, §5.2
- **Decision:** Lifecycle `state` and `session_id` are persisted in a
  `veil.v1.IntentRecord` wrapper around `SealedIntent`, not as fields on
  `SealedIntent` itself.
- **Rationale:** §5.1's field list is authoritative ("implement exactly");
  §5.2 requires persisting state "with the intent record". The wrapper
  satisfies both without mutating the canonical message.

## D-002 — DataGrant / ServiceVoucher defined in veil/v1

- **Date:** 2026-06-11 (task 0.2)
- **SPEC:** §5.1, §6.4
- **Decision:** `DataGrant{content_hash, access_uri_commitment, schema_id}`
  and `ServiceVoucher{schema, units (math.Int)}` live in
  `proto/conclave/veil/v1/intent.proto`; `session.v1.SettleLeg` imports them.
- **Rationale:** §5.1 references both types without defining them. Veil is
  the lowest layer that uses them; dependency direction stays acyclic
  (session→veil→escrow, receipt→session).

## D-003 — chamber_id appended to every root entity

- **Date:** 2026-06-11 (task 0.2)
- **SPEC:** §1.2, §5.3
- **Decision:** Sigil, CredentialAnchor, Reputation, Mandate, Escrow,
  Session, Receipt all carry `uint32 chamber_id` (fixed 0). Where the SPEC
  pins its position (SealedIntent field 2) it is exact; elsewhere it is the
  last field.
- **Rationale:** §1.2 requires all IDs and protos chamber-aware from day one
  even where §6 state lists omit the field.

## D-004 — MandateTerms split from runtime accounting

- **Date:** 2026-06-11 (task 0.2)
- **SPEC:** §6.2
- **Decision:** `MsgIssueMandate`/`MsgAmendMandate` carry a `MandateTerms`
  message with only controller-specified fields; `window_spent`,
  `window_start_height`, `revoked` are keeper-managed and absent from msgs.
- **Rationale:** Makes §6.2's "full replacement semantics, never in-place
  mutation of ceilings" enforceable at the type level.

## D-005 — DeliveryCondition NONE = unset oneof

- **Date:** 2026-06-11 (task 0.2)
- **SPEC:** §6.5
- **Decision:** There is no explicit NONE condition class; an absent
  `DeliveryCondition.condition` oneof means NONE.
- **Rationale:** §6.5 lists only the three real classes. Phase-0 validation
  rejects any set condition with `ErrConditionsNotEnabled`.

## D-006 — SettleLeg / PricePoint / Solution shapes

- **Date:** 2026-06-11 (task 0.2)
- **SPEC:** §6.4
- **Decision:** `SettleLeg{from_sigil, to_sigil, asset oneof, condition}`;
  `PricePoint{base, quote, price (Dec)}` with non-coin assets priced as
  `data/{schema}` / `svc/{schema}`; `Solution` stores the protocol-computed
  `score`. Legs name sigils, resolved to addresses at Conclude time.
- **Rationale:** §6.4 names these types without field lists. Sigil-named
  legs keep the graph identity-centric; resolution happens at settlement.

## D-007 — Param fields invented from SPEC prose

- **Date:** 2026-06-11 (task 0.2)
- **SPEC:** §6.2, §6.3, §6.4
- **Decision:** veil `invalid_slash_bps` (default 5000 = §6.3's "50%");
  mandate `max_msg_nesting_depth` (8, §6.2 fuzz cap), `fee_conversion_rate`
  + `fee_pool` (§6.2 fee abstraction); session `invalid_solution_slash_bps`
  (500), `equivocation_slash_bps` (2000), `surplus_skim_bps` (§6.4 "small
  skim (param)"), `solver_fee_bps` (500 per D3 in SPEC §14).
- **Rationale:** SPEC names the behaviors and percentages in prose; bps
  params make them governable per §6.1's "params … governable" precedent.

## D-008 — MsgUpdateParams on sigil/mandate/veil/session

- **Date:** 2026-06-11 (task 0.2)
- **SPEC:** §6.1
- **Decision:** Standard v0.50 gov-authority `MsgUpdateParams` on the four
  modules with params; escrow/receipt have none yet.
- **Rationale:** §6.1 says params are governable; this is the v0.50
  mechanism. Never mandate-permittable (it is not in any allowed-msg list
  by default and the §6.2 guard applies to mandate msgs).

## D-009 — Sigil/credential id derivation scheme

- **Date:** 2026-06-12 (task 0.3)
- **SPEC:** §5.1, §6.1
- **Decision:** Ids are 32-byte sha256 digests rendered as 64-char hex:
  `sha256(domain_byte ‖ chamber_be32 ‖ payload ‖ global_seq_be64)` with
  domain 0x01 for sigils (payload = controller addr) and 0x02 for
  credentials (payload = issuer‖subject‖schema‖claim_hash). A module-global
  uint64 sequence salts every id.
- **Rationale:** Mirrors §5.1's hash-derived intent ids; fixed 32-byte width
  satisfies the §5.3 fixed-width key requirement; the sequence guarantees
  uniqueness without per-entity nonces.

## D-010 — One sigil per controller address per chamber

- **Date:** 2026-06-12 (task 0.3)
- **SPEC:** §6.1
- **Decision:** Registration and rotation reject an address that already
  controls a sigil in the chamber; the 0x04 reverse index is single-valued.
- **Rationale:** §6.1's invariant "every Mandate's agent key resolves to
  exactly one Sigil" requires single-valued address resolution; allowing
  multi-sigil keys would make it unsatisfiable.

## D-011 — x/sigil store layout beyond the SPEC's listed keys

- **Date:** 2026-06-12 (task 0.3)
- **SPEC:** §5.3, §6.1
- **Decision:** Reverse index keyed `0x04|chamber(4)|addr` (chamber first,
  per §5.3's "every keyed object" rule even though §6.1 writes it as plain
  addr→sigil_id); Params at `0x05`; global id sequence at `0x06`.
- **Rationale:** §5.3 conformance; chamber-0 lookups stay O(1).

## D-012 — Deterministic log10: digit count + linear interpolation

- **Date:** 2026-06-12 (task 0.3)
- **SPEC:** §6.1, §11.6
- **Decision:** `log10(v) ≈ d + (v/10^d − 1)/9` for v > 1, where d = decimal
  digit count of ⌊v⌋ minus 1; v ≤ 1 returns 0. All LegacyDec; no floats.
- **Rationale:** Exact at powers of 10, monotonic, max error ≈0.05 decades
  mid-decade — ample for value weighting. Values ≤1 USDC clamp to weight 0,
  matching min(1, log10(v)/4)'s behavior for the sub-1 range.

## D-013 — Lazy decay: whole periods, remainder carried, read paths pure

- **Date:** 2026-06-12 (task 0.3)
- **SPEC:** §6.1
- **Decision:** Decay exponent = whole elapsed periods (integer division);
  `last_decay_height` advances by exactly periods×period so remainder blocks
  carry. Queries return the decayed value without persisting; decay persists
  only on state-mutating paths (settlement, penalty).
- **Rationale:** LegacyDec.Power takes integer exponents; carrying the
  remainder gives drift-free determinism; pure reads keep queries
  side-effect free.

## D-014 — Reputation penalty floors at zero

- **Date:** 2026-06-12 (task 0.3)
- **SPEC:** §6.1
- **Decision:** `score -= penalty` clamps at 0 rather than erroring.
- **Rationale:** §6.1's invariant says reputation is never negative; the
  penalty must therefore saturate.

## D-015 — Invariant scope until dependent modules land

- **Date:** 2026-06-12 (task 0.3)
- **SPEC:** §6.1
- **Decision:** Invariant 1 currently asserts reverse-index ↔ sigil
  consistency (extended to mandate grantees at task 0.4); invariant 3
  asserts the `IsCredentialActive` accessor never reports a revoked
  credential active (CEL predicate evaluation gates on this accessor at
  task 0.8). Both carry CONCLAVE-TODO(phase-0) markers.
- **Rationale:** The full statements reference modules that don't exist
  yet; these are the strongest checkable forms today and the accessor
  contract pins the integration point.

## D-016 — Privilege-escalation guard extended beyond mandate msgs

- **Date:** 2026-06-12 (task 0.4)
- **SPEC:** §6.2 step 3
- **Decision:** `allowed_msg_types` may never contain, in addition to
  `MsgIssueMandate`/`MsgRevokeMandate` named by the spec:
  `MsgAmendMandate`, mandate `MsgUpdateParams`, authz `MsgGrant`, and
  feegrant `MsgGrantAllowance`. Enforced at issuance and amendment
  validation with `ErrPrivilegeEscalation`.
- **Rationale:** A mandate-bound key permitted to `MsgGrant` authz over its
  own account mints an unbound delegate key: that key's `MsgExec` txs are
  signed by the unbound key, step 1 passes the signer as a root key, and
  the inner msgs act as the grantee account — a full mandate bypass.
  `MsgGrantAllowance` is the feegrant analogue for fees. Amend and
  UpdateParams are mandate msgs in all but name.

## D-017 — Grantee index lifecycle: REPLACE, retention, controller conflict

- **Date:** 2026-06-12 (task 0.4)
- **SPEC:** §6.2 state, step 1; open decision D2
- **Decision:** (a) Issuing to a grantee that holds a mandate deletes the
  superseded record (REPLACE, one record per key). (b) The 0x02
  grantee-index entry is NEVER deleted on revocation or expiry — revoked
  and expired mandates are retained and keep rejecting in ante. (c) A key
  that controls a sigil can never be a grantee (`ErrGranteeConflict`).
  (d) Grantees enter x/sigil's 0x04 reverse index as delegate keys; a key
  whose mandate is defunct can be re-bound (to any sigil) by a controller
  reissue, which updates that index.
- **Rationale:** Step 1 treats "no index entry" as root-key passthrough, so
  deleting entries on revoke/expiry would silently re-promote session keys
  to unbounded root keys. Controller-grantees would either break D-010's
  single-valued index or brick the controller (D-016 forbids allowing
  `MsgRevokeMandate`, so a self-mandated controller could never manage
  mandates again).

## D-018 — Amendment: stable id, full term replacement, window reset

- **Date:** 2026-06-12 (task 0.4)
- **SPEC:** §6.2 Msgs
- **Decision:** `MsgAmendMandate` keeps the mandate id, replaces every term
  wholesale, and resets `window_spent`/`window_start_height`. Grantee,
  sigil, and chamber are immutable under amendment (rebinding = revoke +
  issue). Revoked mandates cannot be amended.
- **Rationale:** "Full replacement semantics, never in-place mutation of
  ceilings" at the record level; the controller could reach the same state
  via revoke+reissue, so the reset creates no bypass. Immutable grantee
  preserves D-017(b) retention for the old key.

## D-019 — Spend ceilings are deny-by-default per denom

- **Date:** 2026-06-12 (task 0.4)
- **SPEC:** §6.2 steps 4–5
- **Decision:** Ceiling checks use `Coins.IsAllLTE`: a spend-intent denom
  absent from `spend_ceiling_per_tx`/`spend_ceiling_window` has an implicit
  ceiling of zero. A mandate only authorizes spending denoms it lists;
  "unlimited" is expressed by an explicit large ceiling.
- **Rationale:** §1.1 goal 3 — a leaked session key must be bounded; an
  unlisted denom defaulting to unlimited would invert the safety posture.

## D-020 — Non-coin give legs value at zero under face-value pricing

- **Date:** 2026-06-12 (task 0.4)
- **SPEC:** §6.2 step 4
- **Decision:** `FaceValuePriceSource` (Phase 0) values DataGrant and
  ServiceVoucher give legs at zero: no clearing prices exist and they have
  no denom a Coins ceiling could bound. This is a known, accepted Phase-0
  exposure, marked CONCLAVE-TODO(phase-0) in code; task 0.8's session
  PriceSource values `data/{schema}` / `svc/{schema}` assets (D-006) and
  closes it.
- **Rationale:** The only alternatives are rejecting all data/service
  intents (kills the §1.3 demo economy) or inventing prices (violates
  §11.6 determinism-by-spec). The exposure is bounded: non-coin legs move
  escrowed grants/vouchers, not the grantee's coin balances.

## D-021 — Malformed intent payloads carry zero spend intent

- **Date:** 2026-06-12 (task 0.4)
- **SPEC:** §6.2 step 4, §6.3 bond rules, §11.6
- **Decision:** A `MsgSubmitIntent` whose ciphertext fails to unmarshal as
  `IntentPayload` is valued at zero in ante (behind recover — never
  panics). The deterrent is §6.3's INVALID path: the Reveal slashes 50% of
  its bond.
- **Rationale:** A malformed intent can never match, so it can never spend;
  the bond is the spec's anti-spam instrument for exactly this case.
  Phase 1 makes payloads opaque to ante anyway —
  CONCLAVE-TODO(phase-1): the encrypted Veil needs a spend commitment in
  the plaintext envelope for step 4 to bound encrypted intents.

## D-022 — Fee abstraction mechanics and fee-denom policing

- **Date:** 2026-06-12 (task 0.4)
- **SPEC:** §6.2 fee abstraction
- **Decision:** (a) One predicate (`FeeAbstractionMandate`: active mandate,
  fee = exactly one coin, denom listed and non-native) is shared by a
  skip-wrapper around the standard `DeductFeeDecorator` and by
  `FeeAbstractionDecorator`, so exactly one fee path handles any tx.
  (b) A mandate-bound fee payer may otherwise pay gas only in the native
  denom; anything else → `ErrFeeDenomNotPermitted`. (c) The pool is the
  `mandate` module account (blocked, no mint/burn perms), genesis-funded
  in localnet.sh; conversion truncates in the pool's favor; an exhausted
  pool rejects with `ErrFeePoolInsufficient`. (d) The native denom is read
  from `Params.fee_pool.denom`, never hardcoded. (e) Additive events:
  `mandate_revoked`, `mandate_amended`, `mandate_fee_converted`.
- **Rationale:** The split DeductFee/FeeAbstraction placement (§2 pipeline
  order) double-charges or leaks fees unless both sides share one
  predicate. fee_denoms is a capability list; deny-by-default matches
  D-019.

## D-023 — x/mandate store layout and id derivation

- **Date:** 2026-06-12 (task 0.4)
- **SPEC:** §5.3, §6.2 state
- **Decision:** Keys: `0x01|chamber(4)|len(1)|grantee|mandate_id(32)`
  record (grantee length-prefixed so addresses can never alias),
  `0x02|chamber(4)|grantee` → mandate_id (chamber-first per D-011),
  `0x03|chamber(4)|sigil_id(32)|mandate_id(32)` → grantee (serves the
  per-sigil query), `0x04` params, `0x05` id sequence. Mandate ids use the
  D-009 derivation with domain byte 0x03 and a module-local sequence.
- **Rationale:** §5.3 conformance; the 0x03 index keeps `Mandates(sigil)`
  off the hot path without scanning; D-009's domain-byte namespace extends
  naturally (0x01 sigil, 0x02 credential, 0x03 mandate).

## D-024 — §1.3 steps 2–3 demonstrated in two phases until task 0.7

- **Date:** 2026-06-12 (task 0.4)
- **SPEC:** §1.3 steps 2–3, §10 row 0.4
- **Decision:** baseapp v0.50 `runTx` rejects any msg lacking a registered
  handler BEFORE the ante handler runs, and x/veil's msg server lands at
  task 0.7 — so a `MsgSubmitIntent` cannot transit a block yet in any
  mode. The acceptance test therefore runs (1) the exact §1.3 intent flow
  through the production ante pipeline (`app.NewAnteHandler` over the
  app's keepers, real signed tx bytes, gas metered) and (2) the identical
  enforcement through full FinalizeBlock+Commit delivery using a routable
  msg (`MsgSend`) under the same ceilings. CONCLAVE-TODO(phase-0): fold
  into one full-block test at 0.7.
- **Rationale:** The §10 build order (mandate before veil) makes this
  unavoidable; the two phases together cover every §1.3 steps 2–3
  assertion, including "no gas beyond ante" on both paths.
- **RESOLVED (task 0.7):** x/veil's msg server is now routed, so
  `MsgSubmitIntent` transits real blocks. `app/mandate_e2e_test.go` is
  folded into ONE full-consensus flow: the over-ceiling intent is rejected
  with `ErrMandateExceeded` through FinalizeBlock (code, codespace, no gas
  beyond ante, nothing charged/escrowed), then an under-ceiling intent
  lands (bond escrowed, window charged), seals at the boundary EndBlock,
  opens at the next BeginBlock (order-then-reveal through consensus),
  expires with bond + window refunded — proven real by a follow-up intent
  that bounced against the consumed window now landing — and a real-block
  cancel refunds identically. The MsgSend surrogate phase is deleted.

## D-025 — Intent bonds count toward spend ceilings

- **Date:** 2026-06-12 (task 0.4, acceptance amendment)
- **SPEC:** §6.2 step 4, §6.3 bond rules
- **Decision:** `MsgSubmitIntent`'s spend intent is the per-denom max of
  give legs PLUS the intent bond, in both the per-tx and window
  aggregation. The bond charges even when the payload is malformed
  (zero give-leg spend per D-021). Consequence of D-019: a mandate
  permitting `MsgSubmitIntent` must list a native-denom ceiling that
  budgets bonds.
- **Rationale:** Bonds are slashable 50% on INVALID, so unbounded bond
  spend is an unbounded CLV burn for a stolen mandate-bound key — the
  exact "jailbroken agent" threat Mandates exist to cap. Reading D-019 and
  D-021 together without this left the burn bounded by nothing in the
  mandate. The same accounting carries into Phase 1, where ante binds
  `spend_commitment + bond` (SPEC §6.3 "Spend commitment", §8(e)).

## D-026 — Unordered flag encoded as a TxBody extension option

- **Date:** 2026-06-12 (task 0.5)
- **SPEC:** §6.7
- **Decision:** SDK v0.50's TxBody has no `unordered`/`timeout_timestamp`
  fields (they land upstream in v0.53), so a tx is unordered iff
  `TxBody.extension_options` carries
  `conclave.unordered.v1.UnorderedTxExtension{timeout_timestamp}`. The
  ExtensionOptionsDecorator accepts exactly this type URL and keeps
  rejecting everything else; a present-but-malformed extension is a hard
  error, never silently ordered.
- **Rationale:** Extension options are signed and critical (unknown ones
  reject), giving the ADR-070 semantics without forking tx encoding. The
  generated type lives under `app/unordered/types` — protocgen.sh now
  emits both `x/` and `app/` go_package trees.

## D-027 — Forked SigVerificationDecorator for the sequence bypass

- **Date:** 2026-06-12 (task 0.5)
- **SPEC:** §6.7, §14 D8
- **Decision:** `app/unordered/sigverify.go` forks SigVerificationDecorator
  from cosmos-sdk v0.50.15 `x/auth/ante/sigverify.go` with a two-point
  delta: the sequence-equality check is skipped iff the tx is unordered,
  and AMINO/TEXTUAL signer data uses the tx's own claimed sequence.
  Ordered txs are byte-identical to upstream. IncrementSequenceDecorator
  is wrapped with the same predicate (skip iff unordered). The file header
  is the provenance pin; the conformance suite is the behavioral pin; the
  fork is retired via the SPEC §14 D8 SDK v0.53 evaluation at the
  Phase-0→Phase-1 boundary (CONCLAVE-TODO(phase-1) at the fork site).
- **Rationale:** v0.50 offers no hook to disable the sequence check; the
  v0.53 bump is a larger dependency jump than task 0.5 warrants. Replay
  safety for unordered txs never derives from sequences — it is the
  dedup cache (D-028).

## D-028 — Dedup cache: key reconstruction, dual-mode writes

- **Date:** 2026-06-12 (task 0.5)
- **SPEC:** §6.7
- **Decision:** Dedup entries live at `0x0F|expiry_unix_nanos(8,BE)|`
  `txhash(32)` exactly as §6.7 writes (no chamber component — tx hashes
  are chain-global, app-level state). The expiry is the tx's own signed
  `timeout_timestamp`, so existence checks reconstruct the key from the
  tx and stay a single O(1) read — no secondary index. Entries are
  written in BOTH CheckTx and DeliverTx, after signature verification.
  The EndBlock sweep deletes `expiry <= block time`; it can never re-open
  a replay because the decorator requires `timeout > block time` — exact
  complements.
- **Rationale:** Expiry-first keys make the sweep one time-ordered prefix
  walk; the dual-mode write is the same optimistic pattern as the D-019
  window charge and bounds mempool-level replays before a block is
  proposed.

## D-029 — TTL cap is time-based: 10 minutes of wall-clock block time

- **Date:** 2026-06-12 (task 0.5)
- **SPEC:** §6.7 "Cap unordered TTL at 10 minutes of blocks"
- **Decision:** `MaxUnorderedTTL = 10 * time.Minute`, compared against
  `ctx.BlockTime()`. At the §12 dev cadence (1s blocks) this is ≈600
  blocks.
- **Rationale:** `timeout_timestamp` is a timestamp; converting the cap to
  a block count would re-introduce a blocks↔time assumption the spec's
  own field shape avoids. Block time is consensus state (BFT time), so
  the comparison is deterministic.

## D-030 — Unordered codespace 16xx; EndBlock sweep via a module shim

- **Date:** 2026-06-12 (task 0.5)
- **SPEC:** §6.7, §13
- **Decision:** Errors register under codespace `unordered` = 16xx,
  extending the §13 ladder. The sweep runs through a minimal
  EndBlocker-only AppModule shim (`app/unordered/module.go`) with no
  msgs, queries, genesis, or proto service surface — the practical
  reading of §6.7's "app-level, not a module" under the SDK's module
  manager, which owns EndBlock dispatch.
- **Rationale:** A shim keeps the sweep inside deterministic consensus
  flow without inventing a parallel EndBlock path; the absent user
  surface is what "not a module" protects.

## D-031 — App-side mempool pinned to NoOpMempool until Block SDK (0.6) — SUPERSEDED by D-032

- **Date:** 2026-06-12 (task 0.5)
- **SPEC:** §6.7, §10 rows 0.5–0.6
- **Decision:** `NewConclaveApp` appends `baseapp.SetMempool(NoOpMempool)`
  after all caller options, overriding the server default. Block
  proposals therefore fall through to CometBFT's FIFO mempool.
  Superseded at task 0.6 by the Block SDK laned mempool (SPEC §6.7
  lanes), which replaces the app-side mempool wholesale.
- **Rationale:** The server's DefaultBaseappOptions selects
  SenderNonceMempool whenever `mempool.max-txs >= 0` — the default
  app.toml value — and that mempool orders/gates same-sender txs by
  sequence BEFORE ante runs, serializing exactly the ≥50 parallel
  unordered txs §10 row 0.5 requires. NoOp is the honest interim choice;
  the localnet blast demonstrates 50-in-one-block against it.
- **SUPERSEDED (task 0.6):** replaced wholesale by the Block SDK laned
  mempool — see D-032. The SenderNonceMempool hazard is now avoided by
  setting the laned mempool AFTER appBuilder.Build (last write wins) and
  by D-034's synthetic nonces inside the lanes.

## D-032 — Block SDK pinned at skip-mev/block-sdk/v2 v2.1.5; custom ProcessProposal mode

- **Date:** 2026-06-12 (task 0.6)
- **SPEC:** §3 (lanes row), §6.7, §10 row 0.6
- **Decision:** Pin `github.com/skip-mev/block-sdk/v2 v2.1.5` (latest v2;
  the v2 line targets SDK v0.50.x). MVS verified empirically: block-sdk
  requires cosmos-sdk v0.50.3 / cometbft v0.38.5 / cosmossdk.io deps all
  BELOW our pins, so the build keeps cosmos-sdk v0.50.15, cometbft
  v0.38.23, ibc-go v8.8.0 (block-sdk has no ibc-go dependency) and the
  goleveldb replace. The proposal handler is constructed with
  `abci.New(..., useCustomProcessProposal=true)`: the library's
  recommended default wires a NO-OP ProcessProposal; the custom path is
  what validates lane structure (partition contiguity in priority order,
  intra-lane priority ordering via the lane comparator, per-lane
  byte/gas shares, per-tx ante verification with cache-accumulated
  state). The laned mempool and both handlers are set after
  appBuilder.Build, overriding the server's mempool selection.
- **Rationale:** §6.7 requires ProcessProposal to enforce the lane
  invariants; a no-op process handler would let a malicious proposer
  ignore lanes entirely. Verified against vendored v2.1.5 source, not
  docs.

## D-033 — Exclusive min-class lane predicates; top-level TypeURLs only; types registered early

- **Date:** 2026-06-12 (task 0.6)
- **SPEC:** §6.7 "lowest-priority matching lane"
- **Decision:** (a) Each msg classifies by its top-level TypeURL into
  veil/session/commerce/ibc/general (ibc = `/ibc.` prefix; anything
  unlisted, including every `MsgUpdateParams`, is general). A tx's lane is
  the lowest-priority class among its msgs, and each lane's match(tx)
  predicate is "tx class == this lane" — the five predicates therefore
  PARTITION the tx universe (every tx matches exactly one lane), making
  routing independent of lane registration order and making block-sdk's
  first-match insertion implement the spec rule exactly. (b) Wrapper msgs
  (authz MsgExec, gov) route as the wrapper, never their contents —
  routing stays a pure function of the outer TypeURL. (c)
  veil/session/escrow msg types (and their amino names) are registered in
  the app codec at 0.6, before their modules exist: the lanes route on
  these TypeURLs, so the tx decoder must resolve them for ProcessProposal
  lane verification. No msg server is routed, so execution keeps
  rejecting them (D-024 unchanged) and CheckTx still cannot admit them
  from the network.
- **Rationale:** Documented for client devs in docs/LANES.md (a §6.7
  requirement). Exclusive predicates also kill the failure mode where a
  reordered registry silently reroutes txs — pinned further by the
  registration-order guard test.

## D-034 — Unordered txs in lanes: synthetic hash-derived nonce + ctx.TxBytes injection

- **Date:** 2026-06-12 (task 0.6)
- **SPEC:** §6.7 (lanes × unordered txs), §10 row 0.5 regression gate
- **Decision:** (a) Every lane's SignerExtractionAdapter wraps the
  block-sdk default with one delta: a tx carrying the D-026 unordered
  extension reports sequence = BE uint64 of sha256(txBytes)[:8] instead
  of its claimed sequence (a malformed extension is a hard error,
  mirroring the ante). Lane mempools index and REPLACE txs by (sender,
  sequence), and the lane comparator hard-errors on equal same-sender
  sequences (rejecting the whole proposal) — without the synthetic nonce,
  two unordered txs claiming one sequence could never share a block.
  Replay protection never derives from this value; it remains the dedup
  cache (D-028). (b) The ante handler given to lanes is wrapped to inject
  `ctx.WithTxBytes(encode(tx))` when TxBytes is empty: block-sdk invokes
  the ante chain directly in PrepareProposal/ProcessProposal without
  setting TxBytes (baseapp's runTx always does), which gave every
  unordered tx in a proposal the sha256 of EMPTY bytes — the dedup cache
  then rejected all but the first as replays. Decoded txs re-encode to
  their original bytes, so the injected hash input equals what CheckTx
  and FinalizeBlock hash.
- **Rationale:** This was the predicted risk of 0.6 (unordered × lanes).
  Both halves are pinned by tests: same-claimed-sequence survival through
  CheckTx→lanes→Prepare→Process→FinalizeBlock, and the full 0.5
  conformance suite + localnet blast as the regression gate.

## D-035 — Veil lane priority: fee_bid as fixed-width Dec key; ties are NOT submit order

- **Date:** 2026-06-12 (task 0.6)
- **SPEC:** §6.3 "ordered by fee_bid then submit order", §11.6
- **Decision:** The veil lane's TxPriority parses envelope `fee_bid` as a
  LegacyDec (the proto calls it a decimal string) and encodes it as a
  60-digit zero-padded decimal key (the Dec's 10^18-scaled integer), so
  lexicographic order equals numeric order and the priority is a pure
  function of tx bytes — consensus-verifiable in ProcessProposal. A
  multi-msg tx takes the max bid across its MsgSubmitIntent envelopes;
  MsgCancelIntent counts zero; unparseable/empty/negative bids count zero
  (ordering metadata never rejects or panics; envelope validation is the
  0.7 msg server's job); bids ≥10^42 uclv clamp to the max key. The
  spec's "then submit order" tie-break is implemented as the priority
  index's deterministic (sender address, sequence) order, NOT
  proposer-local arrival order: arrival order is unobservable off the
  proposer and unverifiable in ProcessProposal. Same-sender txs always
  order by sequence (the lane comparator compares sequences before
  priorities for one signer). The trust model — fee_bid is congestion
  pricing, not a security boundary; uniform-price batch clearing (§6.4)
  makes intra-lane position worthless; Phase 1 fixes ordering before
  contents are readable (§6.3) — is documented in docs/LANES.md.
- **Rationale:** Strict fee_bid ordering IS enforced across nodes (the
  block-sdk lane comparator validates it in ProcessProposal); pretending
  the tie-break is "submit order" would claim a property no node can
  check. C must be a comparable map key in block-sdk's priority index, so
  the fixed-width string encodes Dec order safely (no floats, §11.6).

## D-036 — Lane over its share QUEUES; no cross-lane spill, no redistribution

- **Date:** 2026-06-12 (task 0.6)
- **SPEC:** §6.7, §10 row 0.6 ("spills correctly or queues — state which")
- **Decision:** Block SDK v2.1.5 gives QUEUE semantics and we keep them:
  PrepareProposal fills each lane up to `share × max_tx_bytes` (and gas);
  txs that don't fit stay in the lane's mempool for later blocks. Excess
  txs never spill into another lane's space, and space a lane leaves
  unused is NOT redistributed (every share is a hard cap; the shares sum
  to 1.0 with no zero-share unlimited lane). ProcessProposal rejects any
  proposal whose lane partition exceeds its share, so queueing is also
  consensus-enforced, not just proposer politeness.
- **Rationale:** Verified from vendored source (DefaultPrepareLaneHandler
  skips over-limit txs without removal; proposals.UpdateProposal errors
  on over-share partitions in both prepare and process). Pinned by the
  queue-then-drain integration test.

## D-037 — x/veil store layout: spec-literal 0x02 index, intent id domain, IntentRecord owner

- **Date:** 2026-06-13 (task 0.7)
- **SPEC:** §5.3, §6.3, §5.1
- **Decision:** Keys follow §5.3's veil example exactly:
  `0x01|chamber(4)|intent_id(32)` → IntentRecord;
  `0x02|state(1)|ttl(8,BE)|intent_id(32)` → state/expiry index (empty
  value). The 0x02 index deliberately carries NO chamber component — §5.3's
  own example omits it (unlike the convention sentence above it), and every
  sweep (boundary seal, Reveal, expiry) is state-scoped, not chamber-scoped,
  until Phase 3. Params at `0x03`, id sequence at `0x04`. Intent ids use the
  D-009 derivation with intent domain byte 0x04 (0x01 sigil, 0x02 cred,
  0x03 mandate, 0x04 intent) and payload = sender_sigil — exactly §5.1's
  `hash(sender_sigil, nonce, chamber)` with the module sequence as nonce.
  `IntentRecord` (D-001's wrapper, not §5.1-authoritative) gains
  `string owner = 4`: the §5.1 SealedIntent names only the sigil, which
  does not identify the paying key — owner is the bond-refund target, the
  window-refund grantee, and the MsgCancelIntent authorization identity.
- **Rationale:** §5.3 says "implement … exactly"; the example is the more
  specific instruction where it and the general convention diverge. Every
  transition flows through one `transitionIntent` chokepoint that rewrites
  the 0x02 entry, so index and records never drift. CONCLAVE-TODO(phase-3)
  at the index for chamber sharding.

## D-038 — SessionKeeper interface + Phase-0 stub; veil owns the sweeps until 0.8

- **Date:** 2026-06-13 (task 0.7)
- **SPEC:** §6.3 ABCI hooks, §5.2, §6.4 params
- **Decision:** The session-boundary hand-off goes behind a
  `SessionKeeper` interface: `SessionBoundary(ctx) (isBoundary bool,
  sessionID uint64)` and `IntakeRevealed(ctx, sessionID, intentIDs)`.
  `StubSessionKeeper{SessionLengthBlocks}` fires a boundary whenever
  `height % k == 0` and returns the **boundary height as the session id** —
  deterministic, unique per boundary, and it makes the order-then-reveal
  invariant directly checkable (the Reveal in BeginBlock(H) only touches
  records with `session_id < H`). The cadence is constructor-fixed (app
  wires the dev default 3), NOT a veil param: the governable
  `session_length_blocks` belongs to x/session per §6.4 and lands at 0.8 —
  parking it in veil params now would force a param migration. §5.2 says
  transitions happen "in x/session EndBlock", which does not exist yet, so
  x/veil owns the sweeps in the interim: BeginBlock = the Reveal; EndBlock =
  expiry sweep then boundary seal. CONCLAVE-TODO(phase-0): 0.8 supplies the
  real session keeper and moves sweep ownership under §6.4's EndBlock order.
- **Rationale:** A keeper interface with a deterministic stub lets the whole
  lifecycle and every test run against the true boundary semantics now,
  with Phase-0.8 swapping only the implementation — the same discipline
  §6.3 mandates for the ThresholdScheme.
- **EndBlock order consistency with §6.4 (pin for 0.8):** 0.7's veil
  EndBlock runs (1) the expiry sweep (with its mandate window refunds) then
  (2) the boundary seal; the Reveal is in BeginBlock per §6.3. This is a
  §6.4-consistent subsequence: §6.4's mandated EndBlock order is advance
  states → Reveal intake → scoring → conclude → fallback → rollover sweep →
  expiry sweep → mandate window refunds (steps 1–7). The veil expiry sweep
  + refund are §6.4 steps 6–7, run on the already-open intents BEFORE the
  boundary seal fixes the next session's ordering (the §6.3 boundary action
  that begins the next session). So 0.8 inserts x/session's steps 1–5
  (advance/intake/scoring/conclude/fallback/rollover) AHEAD of veil's expiry
  sweep, and the BeginBlock Reveal continues to feed IntakeRevealed — with
  NO reordering of veil's existing operations. The expiry-before-seal order
  is the load-bearing fact 0.8 relies on.

## D-039 — Cancel/boundary race resolved by ABCI phase ordering

- **Date:** 2026-06-13 (task 0.7)
- **SPEC:** §6.3 fuzz target "cancel/boundary races", §5.2
- **Decision:** No new mechanism — the race is settled by where each action
  runs in a block. A cancel is a DeliverTx msg; the boundary seal is in
  EndBlock; the Reveal is in the next BeginBlock. Therefore: (a) a cancel
  landing in the boundary block B executes before EndBlock(B) seals → the
  cancel WINS and the seal never sees the intent; (b) a cancel landing in
  B+1 executes after BeginBlock(B+1) has opened the intent → it is no
  longer pre-OPEN → the cancel LOSES with `ErrIntentNotCancellable`. The
  rule stated operationally: a cancel succeeds iff the intent is pre-OPEN
  at the instant its tx executes. Corollary: SEALED_IN_SESSION exists only
  between EndBlock(B) and BeginBlock(B+1) — a span in which no tx executes
  — so cancel-from-SEALED is legal per §5.2 ("any pre-OPEN state") and kept
  in the transition table/keeper, but unreachable in live block flow.
- **Rationale:** Phase ordering is already consensus-deterministic, so the
  race needs no tie-break invention. The rapid fuzz target drives random
  submit/cancel schedules against random cadences and asserts exactly this
  rule plus replay-determinism across two runs.

## D-040 — INVALID settlement: 50% to community pool, remainder refunded, NO window refund

- **Date:** 2026-06-13 (task 0.7)
- **SPEC:** §6.3 bond rules, §6.2 step 5, §6.4 step 7, D-007
- **Decision:** At the Reveal, a payload failing proto-unmarshal (behind
  `recover`, never a panic — §11.6) turns the intent INVALID:
  `invalid_slash_bps` (5000 = 50%) of the bond funds the community pool via
  `DistrKeeper.FundCommunityPool` from the veil module account, the
  remainder refunds to the owner, and the mandate window is deliberately
  NOT refunded. §6.2 step 5 and §6.4 step 7 name only *expired/cancelled*
  intents for window refunds; a malformed intent's ante charge was
  bond-only anyway (D-021), and keeping it charged preserves D-025's bound
  on the malformed-intent burn from a stolen key.
- **Rationale:** Refunding the window on INVALID would hand a stolen key
  free retries of the exact spam the slash exists to deter. EXPIRED and
  CANCELLED, by contrast, are honest non-matches and refund fully.

## D-041 — charged_spend persisted at submission; refunds never recompute; face-value scalar

- **Date:** 2026-06-13 (task 0.7)
- **SPEC:** §6.2 step 4, §6.3 bond rules, §11.6
- **Decision:** `IntentRecord` gains `repeated Coin charged_spend`,
  computed at MsgSubmitIntent execution exactly as the ante valued it
  (same PriceSource, same block: per-denom max of give legs + bond, D-025)
  and persisted. EVERY refund path (EXPIRED, CANCELLED now; CONCLUDED at
  0.8) refunds the STORED value via `MandateKeeper.RefundWindowSpend` —
  never a recomputation — so refunds are exact by construction and immune
  to the 0.8 price-source swap. The §6.3 bond formula's `face_value` is the
  Phase-0 scalar: the denom-blind sum of the per-denom max give-leg amounts
  in base units (no cross-denom prices exist pre-0.8; this is the only
  deterministic scalar and it scales the bond with intent size). The scalar
  is clamped at 10^50 so the bps multiplication can never overflow
  math.Int (§11.6: an over-large face value just demands an unpayable bond
  and rejects as ErrBondInsufficient). Malformed payload → zero give spend
  (D-021) → min_bond applies; the Reveal slash is the deterrent.
- **Rationale:** Replaces the plan's recompute approach (rejected by review
  amendment): persisting the charge makes the EXPIRED/CANCELLED/CONCLUDED
  window refunds exact and 0.8-proof. CONCLAVE-TODO(phase-0): 0.8's
  clearing-price source normalizes give legs to uusdc, replacing the scalar.
- **Refund matrix (charged_spend treatment per state — inherited by 0.8):**
  | terminal/transitional state | bond | mandate window (charged_spend) |
  |---|---|---|
  | EXPIRED   | full refund | full refund |
  | CANCELLED | full refund | full refund |
  | INVALID   | 50% community pool + 50% refund | NO refund — charge persists (D-040) |
  | ROLLED    | held (still live) | NO refund, NO re-charge — charge PERSISTS |
  | CONCLUDED | refund (0.8) | refund (0.8) |
  ROLLED is non-terminal: an unmatched-but-not-expired intent re-enters the
  next session still OPEN and was never un-spent, so its charge stays put —
  it is neither refunded nor charged again. 0.8 inherits this when it drives
  ROLLED re-entry (SPEC §6.4 step 5); CONCLUDED's refunds run inside 0.8's
  §6.4 Conclude cache-context. The window-accounting conservation fuzz pins
  the whole matrix: Σ(window_spent) == Σ(charged_spend of intents not in
  {EXPIRED, CANCELLED}).

## D-042 — Envelope validation rules; payload validity deferred to the Reveal

- **Date:** 2026-06-13 (task 0.7)
- **SPEC:** §6.3 "validates envelope", §5.1, §1.2, D-035
- **Decision:** MsgSubmitIntent validates ONLY the plaintext envelope: it
  must unmarshal as IntentEnvelope; `ttl_height` must be strictly beyond
  the submit height; `chamber_id` must be 0 (and equal `msg.chamber_id`);
  `fee_bid` must be empty or parse as a non-negative LegacyDec. The payload
  (ciphertext) is deliberately NOT inspected for validity at submission —
  malformedness is the Reveal's job even in stub mode (§6.3 INVALID path),
  and Phase 1 cannot inspect ciphertexts here at all. The sender must also
  resolve to `sender_sigil` through the x/sigil reverse index (which
  includes mandate-bound delegate keys, D-017(d)).
- **Rationale:** D-035 made veil-lane ordering tolerate garbage `fee_bid`
  precisely because envelope validation rejects it at execution; this is
  where that rejection lives. Validating the envelope (needed to index by
  ttl) while deferring payload validity keeps the Phase-0 and Phase-1 code
  paths identical — the only thing that changes in Phase 1 is that the
  payload becomes a real ciphertext.

## D-043 — Counterparty-class: deterministic predicate subset, not full CEL

- **Date:** 2026-06-13 (task 0.8, Half A)
- **SPEC:** §6.4 rule 5, §5.1 counterparty_class, §6.1 invariant 3, §11.6
- **Decision:** The SPEC §6.4 rule-5 "CEL predicate" is implemented in Phase 0
  as a minimal, fully deterministic subset (`x/session/types/predicate.go`),
  NOT google/cel-go. Grammar: `reputation <cmp> <decimal>`,
  `has_credential("schema")`, `&&`/`||`, parentheses; empty = always true.
  Evaluated against the counterparty's Sigil state via
  `GetReputation`/`HasActiveCredentialSchema` (the §6.1 invariant-3 active
  accessor). **Fail-closed + empty≠malformed:** an EMPTY predicate is always
  true; any non-empty predicate that fails to parse is MALFORMED and rejects
  the match (the parse error and a recover in the keeper both yield rejection).
  Parsing is length- and depth-bounded and never panics; the parser fuzz target
  asserts a deterministic parse-or-error outcome on arbitrary bytes. Evaluated
  at solution-validation height (≈ session-open; within solution_window blocks).
- **Rationale:** cel-go is a heavy consensus dependency with its own
  determinism surface for a Phase-0 need the spec illustrates only with simple
  comparisons. Resolves the 0.4 CONCLAVE-TODO (D-015/D-020 CEL gate).
  CONCLAVE-TODO(phase-2): swap in full cel-go if richer predicates are needed;
  snapshot reputation at the exact session-open height.

## D-044 — Ante/veil PriceSource swap (D-020) deferred to a dedicated post-0.8 task

- **Date:** 2026-06-13 (task 0.8, Half A)
- **SPEC:** §6.2 step 4, §6.4 scoring, D-020, D-041
- **Decision:** Task 0.8 does NOT swap `mandatetypes.FaceValuePriceSource` for a
  session clearing-price source in the ante handler or x/veil. Face-value
  pricing stays; the D-020 non-coin-give-leg exposure stays open. x/session
  scoring (Half B) normalizes surplus to uusdc using each solution's OWN
  `clearing_prices`, so it has no dependency on the ante PriceSource. The swap
  becomes its own task after 0.8.
- **Rationale:** A price-dependent ante introduces a new CheckTx/DeliverTx
  consistency surface (clearing prices read at both must agree) and is NOT
  behavior-preserving once any session has cleared — it would start drawing
  data/svc give legs against mandate ceilings mid-stream. It has zero 0.8
  acceptance dependency, so bundling it into 0.8 adds risk to the mandate
  chokepoint for no acceptance gain. CONCLAVE-TODO(phase-0) markers at
  `mandatetypes.FaceValuePriceSource`/`price_source.go` and the app/veil wiring
  carry the deferral.

## D-045 — Receipts and escrow stubs live in the x/session store

- **Date:** 2026-06-13 (task 0.8, Half A)
- **SPEC:** §6.4, §6.5, §6.6, §10 rows 0.8/0.9
- **Decision:** x/session is the writer of both §6.6 Receipts and the §6.4/§6.5
  DataGrant escrow stubs, and it writes them into its OWN store. The §6.6
  receipt key layout is pinned now exactly as the spec writes it —
  `0x07|chamber|sigil_id|height|receipt_id` (primary, per-agent chronological)
  and `0x08|receipt_id` (secondary) — even though receipts themselves are
  emitted in Half B; escrow stubs key `0x09|chamber|escrow_id` (state ACTIVE,
  no release logic). **Inheritance:** 0.9's x/receipt is a query/proof surface
  that READS these records from the x/session store (a cross-module read); it
  never migrates them. ICS-23 existence proofs (0.9 acceptance) work against the
  session store's IAVL subtree regardless.
- **Rationale:** §6.6 says "Receipts are written by x/session; this module is
  the query/proof surface" — the writer owning the store is the literal reading,
  and standing up x/receipt/x/escrow keepers now would pull 0.9/Phase-2 scope
  into 0.8. Pinning the byte layout now freezes the contract 0.9 reads against.
  CONCLAVE-TODO(phase-2): x/escrow takes ownership of the stub records.

## D-046 — SettleLeg sigils resolve to controller addresses; one sigil per match

- **Date:** 2026-06-13 (task 0.8, Half A)
- **SPEC:** §6.4, §6.1, §1.1 goal 3, D-006, D-010
- **Decision:** At Conclude, a SettleLeg's `from_sigil`/`to_sigil` resolve to the
  sigil's CONTROLLER address via `sigilKeeper.ControllerAddress` (the
  settlement-side counterpart of `SigilIDByAddress`, anticipated since 0.3).
  Coin legs move between controller treasuries; the intent OWNER (the paying
  key) still receives the bond refund and the mandate window refund (x/veil's
  `rec.Owner`). A match may contain at most one OPEN intent per `sender_sigil`
  (`ErrDuplicateSigil`). **Corrected rationale (vs the plan):** the distinct-
  sigil rule is NOT needed for address resolution — sigil→controller is a pure
  function regardless of how many intents share a sigil. It is required for
  per-intent LIMIT-SATISFACTION attribution (rule 3): legs name sigils, but
  give/want limits are per-intent, so a sigil must map to exactly one intent in
  the match for its aggregate net flow to attribute unambiguously.
- **Rationale:** Resolving to the controller treasury (not the session key) is
  the §1.1-goal-3 agent-safety model — a mandate on a session key bounds how
  much of the controller treasury that key can commit, while the bond/window
  (owner-charged) refund to the key. CONCLAVE-TODO(phase-2): allow multiple
  intents per sigil per match (a legitimate case — one agent clearing two
  intents in a batch) by tagging legs with their intent id.

## D-047 — Legs are bound to clearing prices (the binding half of rules 3/4)

- **Date:** 2026-06-13 (task 0.8, Half A — review Correction 1)
- **SPEC:** §6.4 rules 3 ("at the stated clearing prices") and 4, §11.6
- **Decision:** `validateSolution` runs `checkPriceBinding` after the quantity
  checks: for every match, valued at the match's `clearing_prices`, EACH party's
  give value must equal its receive value EXACTLY — i.e. Σ over the party's net
  flows of `price[asset] × net == 0` in the match numeraire (the common price
  quote; dev: uusdc). The numeraire's self-price is 1 (an explicit self-price
  ≠ 1 rejects); an asset that moves without a price rejects; mixed quotes
  reject. **Rounding rule: NONE (exact LegacyDec zero).** The solver controls
  both legs and prices and can always present an exact clearing, so any nonzero
  per-party residual is a mispriced/extractive solution → `ErrPriceMismatch`,
  5% slash. Runs AFTER `checkLimits` so a cap/floor breach still surfaces as
  `ErrLimitViolation`, not a price residual.
- **Rationale:** Without this, rule 3 bound only quantities and rule 4 only the
  price list's internal consistency — nothing forced transfers to respect the
  prices. Half B's surplus skim is a fraction of the margin vs each party's
  limit; if prices are not binding on transfers, "surplus" is measured against
  prices the legs need not honor and a solver can extract through the skim while
  passing every cap/floor. The skim cannot be built on a price-unenforcing
  settlement layer. (This closed the gap the Half-A review itself surfaced as
  Q3 caveat b.)

## D-048 — Conservation contract: three distinct per-asset-class invariants

- **Date:** 2026-06-13 (task 0.8, Half A — review Correction 2; amends D-045)
- **SPEC:** §6.4 rule 2, §6.5, §6.6
- **Decision:** `CheckConservation`'s per-asset graph net-zero is a BOOKKEEPING
  device, not a ledger-conservation proof. The ledger invariant it backs
  differs by asset class, and the code/comments/error taxonomy state each
  explicitly rather than calling all three "conservation":
  - **coin → value conservation:** `applyLeg` debits an EXISTING balance and
    routes it through the module account; graph-zero == on-chain supply
    unchanged + module nets zero.
  - **svc/{schema} → authorized mint:** `applyLeg` MINTS to the receiver (the
    giver is never debited; supply rises by design). Graph-zero means every
    minted unit is matched by a giver who took the −units side, whose offer is
    authorized by rule 3.
  - **data/{hash} → authorized creation:** `applyLeg` writes an escrow stub; NO
    ledger quantity exists. The per-leg amount of 1 is a pure accounting fiction
    so the graph balances; the real invariant is offered (give) AND wanted
    (want), enforced by rule 3.
  `ErrConservation`'s message is now asset-agnostic ("match graph does not
  balance"); it no longer implies value conservation for svc/data. The
  execution-property tests assert each invariant separately
  (`TestExecutionInvariant_Svc`, `TestExecutionInvariant_Data`) alongside the
  coin supply-invariance test.
- **Rationale:** The Half-A report (Q2/Q4) showed the single "conservation"
  framing conflated three different properties and that the execution proof
  covered coins only. Stating the real invariant per class prevents the economic
  layer from assuming a supply guarantee that svc/data do not have.

## D-049 — Phase-0 reads give/want quantity from the leg amount; §5.1 `limit` ignored

- **Date:** 2026-06-13 (task 0.8, Half A — review "ALSO LOG")
- **SPEC:** §5.1 IntentLeg.limit, §6.4 rule 3
- **Decision:** Rule-3 limit satisfaction reads each give/want quantity from the
  leg's ASSET AMOUNT (`coin.Amount` / `svc.Units` / 1 for data), exactly as the
  veil PriceSource and `legAssetKey` do. The §5.1 `IntentLeg.limit` decimal (a
  max-give/min-want RATE) is deliberately NOT honored in Phase 0 — a known,
  deliberate limitation, not silent. Marked `CONCLAVE-TODO(phase-2)` at
  `checkLimits`/`intentLegKeyAmount`.
- **Rationale:** Phase-0 intents express quantity through the asset amount (the
  existing veil valuation already does this); honoring the `limit` rate requires
  the clearing-price normalization deferred with D-044 and richer partial-fill
  semantics. Logged so the economic layer treats it as a known boundary.

## D-050 — Surplus skim base is the surplus margin, NOT §6.4's "settled value"

- **Date:** 2026-06-14 (task 0.8, Half B — STOP #1 approved)
- **SPEC:** §6.4 scoring ("a small skim (param) on settled value"), §14 D3, §11.6
- **DEVIATION FROM SPEC WORDING — recorded, not silent:** SPEC §6.4 frames the
  fee-pool skim as "a small skim on **settled value**." Half B instead bases the
  enforced skim on the **surplus margin** (specifically each party's realized
  numeraire RECEIVE-EXCESS, `recv_N − floor_N`), never on settled value.
- **Decision:** `computeMatchSkim` takes `skim_p = floor(surplus_skim_bps ×
  (recv_N − floor_N) / 10000)` numeraire base units from each party as a
  `party → @feepool` leg. Because the amount is bounded by receive-excess, the
  skim is **principal-safe by construction**: every party still ends ≥ its
  want-floor and (give legs untouched) ≤ its give-cap. The skim-inclusive graph
  is validated by LITERALLY re-running `CheckConservation` (each skim leg is an
  ordinary from→to leg, so the graph still nets zero) plus the new
  `checkSkimBinding`, which generalizes D-047's exact-zero price binding to
  `residual_p == −skim_p` with `0 ≤ skim_p ≤ surplus_p` and the fee pool as the
  sole sink. Re-running D-047's verbatim exact-zero check on the skim graph is
  provably impossible (extracting value v forces residual −v / pool +Σv — a
  conservation identity), so a generalized invariant is the correct contract.
  The solver fee composes on the same surplus base: `floor(solver_fee_bps ×
  total_surplus)`, paid from the pool and CAPPED at the pool balance (solvent by
  construction, never mints). Scoring (`score = Σ surplus_p`) measures the full
  both-sides surplus in the numeraire; only the skim/payout are numeraire-bounded.
- **Rationale (why deviate):** A settled-value skim can EXCEED a thin surplus and
  bite principal — violating the §1.1 agent-safety guarantee that a settlement
  never takes more than a party agreed to give. The approving review made
  principal-safety a hard build condition, so the base must be the surplus the
  party actually beat its limit by. With dev defaults (skim 50 bps, fee 500 bps)
  the fee always caps at the collected skim, so the solver receives the whole
  numeraire skim and the protocol retains zero — a faithful Phase-0 realization
  of §14 D3 ("surplus skim, solver_fee_bps=500"). `checkSkimBinding` is proven
  load-bearing by `skim_test.go` (it goes red if either bound is removed).
  CONCLAVE-TODO(phase-2): revisit whether a settled-value protocol skim (distinct
  from the solver fee) is wanted once real fee economics (D6) are modeled.

## D-051 — Phase-0 skim is numeraire-only; give-saved and non-numeraire surplus scored but not skimmed

- **Date:** 2026-06-14 (task 0.8, Half B — STOP #1 approved)
- **SPEC:** §6.4 scoring/skim, §6.5, D-048 (asset-class invariants), D-050
- **Decision:** Surplus for SCORING is the full economic margin — give-side
  saving (`cap − given`) plus receive-side excess (`recv − floor`), across ALL
  assets, valued in the numeraire (the winner-selection measure). The SKIM,
  however, is taken ONLY from realized numeraire (coin) receive-excess:
  - **give-saved surplus** (a party kept part of what it offered) is NOT skimmed
    — with Phase-0 fill-or-kill the give equals the cap, so this is usually zero,
    and skimming a non-moved asset would need an extra give leg;
  - **non-numeraire receive-excess** (extra svc/data received) is NOT skimmed —
    `svc/{schema}` is an authorized mint and `data/{hash}` has no ledger quantity
    (D-048), so neither is a clean, single-asset, rounding-free skim source;
  - a `data/` numeraire is never skimmable (no ledger quantity).
  Keeping the skim in one coin numeraire makes it in-kind, exact (no price
  round-trip, D-047), and the solver payout trivially payable from the pool.
- **Rationale:** The numeraire-only skim is the smallest construction that
  satisfies every STOP #1 constraint (principal-safe, conservation- and
  price-binding-consistent) without dragging svc-mint/data-creation semantics
  into the fee path. Marked `CONCLAVE-TODO(phase-2)` at `computeMatchSkim`:
  multi-asset in-kind skim + in-kind solver payout once §6.5 escrow and richer
  fee economics land.

## D-052 — x/receipt is a store-less query/proof surface; cross-module read via SessionReceiptReader

- **Date:** 2026-06-14 (task 0.9)
- **SPEC:** §6.6, §10 row 0.9, D-045
- **Decision:** x/receipt is registered as an `AppModule` with **no KVStoreKey**
  (it is absent from `app.RegisterStores`, `maccPerms`, the BeginBlocker and
  EndBlocker order lists; its `InitGenesis`/`ExportGenesis` are no-ops and its
  genesis `Validate` rejects any non-empty receipt list). It therefore CANNOT
  write consensus state — the §6.6 "query/proof surface" guarantee is enforced
  structurally, not by convention. It reads the §6.6 receipts x/session writes
  (D-045) through an **expected-keeper interface**, `receipttypes.SessionReceipt
  Reader` (`GetReceiptByID`, `GetReceiptsBySigil` with pagination,
  `ReceiptStoreName`, `ReceiptByIDStoreKey`), satisfied by `*sessionkeeper.
  Keeper` and wired in `app.go`. Pagination over the 0x07 per-agent index lives
  in x/session (where the store is); x/receipt is a thin facade. The dependency
  direction is receipt→session only — `x/receipt/types` imports no session
  package, so there is no cycle.
- **Rationale:** Chosen over a shared-store-key read so x/session stays the sole
  authority over its own byte layout (0x07/0x08 are its keys, pinned by D-045):
  x/receipt asks "give me the key/store-name to prove receipt X" rather than
  reconstructing `0x08‖IDBytes(id)` itself, so a future layout change is a
  single-module edit. The no-store-key form is the strong version of "this
  module writes nothing" — there is no store for a bug to write to.

## D-053 — ReceiptProof proves the 0x08 key as marshaled ProofOps; verification is height-bound

- **Date:** 2026-06-14 (task 0.9)
- **SPEC:** §6.6 (`ReceiptProof(id)`, "ICS-23 existence proofs come free"), §11.6
- **Decision:** `ReceiptProof(id)` proves the **0x08 secondary** key
  (`receipt_id`, derivable from the query input alone) against the committed
  session IAVL root. The proof is built by querying the app `CommitMultiStore`
  (a `*rootmulti.Store`, injected as `receipttypes.ProofQuerier`) with
  `Path:"/session/key", Prove:true`: rootmulti appends the multistore commit op,
  so the returned `crypto.ProofOps` chains value → session IAVL root → app hash.
  The response carries the **marshaled `ProofOps`** plus `ProofHeight` (the
  committed store version). x/receipt's consensus-path code thus depends only on
  CometBFT's crypto proto + `store/types` — **no ibc import in the module**;
  verification (ibc `ConvertProofs` → `MerkleProof.VerifyMembership` with
  `GetSDKSpecs`, path `["session", key]`) is a verifier-side concern (tests and
  IBC counterparties), where ibc is already a dependency.
- **Acceptance (the consequential deliverable):** `TestReceiptProofVerifies`
  proves the returned proof VERIFIES against `cms.LastCommitID().Hash`, and
  proves it REJECTS in two ways: (a) a byte-flipped value (sensitivity to the
  committed bytes), and (b) — per review — a **stale root**: the same proof,
  taken at ProofHeight H, fails to verify after another block advances the root,
  while still verifying against the correct root at H. The staleness case is
  what makes `ProofHeight` load-bearing rather than decorative — it is the crux
  of §6.6's "verify the proof, not the indexer", and
  `TestReputationMovedAndSameIntentProofVerifies` closes the loop: the same
  CONCLUDED intent whose §6.1 reputation moved (the 0.8 hook, unchanged) has a
  proof that cryptographically verifies. No consensus state is written by any of
  this (D-052).

## D-054 — Off-chain solver reimplements the matching IDEA but REUSES the chain's price-binding validator

- **Date:** 2026-06-14 (task 0.10)
- **SPEC:** §4 (solver/ layout), §6.4 (6-rule validation), §7, §1.3
- **Decision:** The reference solver (`solver/`, `cmd/conclave-solver`) is an
  off-chain, permissionless gRPC client, NOT consensus code. The MATCHING IDEA is
  reimplemented off-chain as separate packages — `solver/cycles` (cycle finder)
  and `solver/naive` (greedy pairwise) — that import ONLY proto message types
  (`x/veil/types`, `x/session/types`), never `x/session/keeper`. They are a
  distinct implementation from the in-consensus fallback (`buildFallbackMatches`
  is not imported). HOWEVER, the one rule that must not drift between solver and
  chain — price-binding representability (D-047) — is NOT reimplemented: it is
  reused verbatim. `keeper.checkPriceBinding` was EXPORTED as
  `keeper.CheckPriceBinding` (pure: `(Match, NetFlows) error`, no ctx/store; one
  internal caller updated, behavior byte-identical), joining the already-exported
  `keeper.CheckConservation`. The solver's pre-submission self-check
  (`solver/gate`) calls these exact functions, so the accept/reject verdict on a
  candidate Match is identical to the chain's by construction. `solver/gate` is
  the ONLY solver package importing `x/session/keeper`, and only the pure
  validation primitives — never the fallback matcher.
- **Rationale:** The bootstrap framing requires the binary not be coupled to the
  consensus fallback *matcher*; the build confirmation requires the price-binding
  *validator* be a faithful mirror that cannot drift. Both are satisfied by
  reimplementing the algorithm (the matcher) while sharing the validation
  primitive (the rule). Stateful §6.4 rules (OPEN-in-session, no-reuse,
  counterparty class) remain the chain's authority — the gate covers exactly the
  conservation + price-binding rules under the solver's control, which are what
  would otherwise cost it a 5% slash. CONCLAVE-TODO(phase-2): if the off-chain
  matcher ever needs richer §6.4 self-checks (limits/uniform-price), export those
  pure checks too rather than reimplementing them.

## D-055 — Cycle-finder prices: numeraire-anchored exact-LegacyDec propagation; representability decided by the shared gate

- **Date:** 2026-06-14 (task 0.10)
- **SPEC:** §6.4 rules 3/4, §4 (Bellman-Ford-style cycle finder), §11.6, D-047
- **Decision:** `solver/cycles` detects a trade cycle as a provider-chain over
  OPEN intents — the provider of intent A is an intent B whose give exactly fills
  A's want (`B.giveKey == A.wantKey`, `B.giveAmt == A.wantAmt`; Phase-0
  fill-or-kill, mirroring the fallback's full-give terms) — via bounded DFS in
  id order (deterministic). It constructs the n-party Match's clearing prices by
  anchoring a coin numeraire at price 1 and propagating around the cycle from the
  per-party identity `price[G_k]·giveAmt_k == price[G_{k+1}]·wantAmt_k`, each
  price an exact `LegacyDec` (`base.MulInt(giveAmt).Quo(LegacyNewDec(wantAmt))`,
  the same truncating quotient the solver will submit). The cycle finder does NOT
  itself decide whether the result is representable: it emits the candidate and
  the shared chain gate (D-054) is the SOLE arbiter — a cycle whose anchored
  prices are not exactly representable in LegacyDec (e.g. an implied 100/3) leaves
  a nonzero per-party residual, `keeper.CheckPriceBinding` rejects it, and
  `solver.Solve` drops it (its intents fall through to the pairwise pass). So the
  "which cycles are skippable" decision is literally the chain's rule, identical
  on both sides. **Acceptance:** `TestPriceBindingSelfCheckAgreesWithChain`
  asserts `gate.PriceBindingOK(m)` and `keeper.CheckPriceBinding` return the SAME
  verdict for the same Match, including the non-representable 100/3 cycle (reject)
  and the §1.3 cycle (accept); `TestSolveClearsTheCycleOnlyWhenRepresentable`
  pins the pipeline behavior.
- **Rationale:** This is the build-confirmation's load-bearing requirement: the
  solver must never believe a price-binding-valid solution the chain would reject
  (or skip one it would accept). Single-source-of-truth makes divergence
  structurally impossible; numeraire anchoring is the smallest exact construction
  that makes the §1.3 cycle clear (uusdc=1, data=100M, svc=10M — all exact
  integers). CONCLAVE-TODO(phase-2): try alternate numeraire anchorings when one
  is non-representable; partial-fill cycles (`B.giveAmt >= A.wantAmt`); retry
  intents freed from a gate-rejected cycle in a different cycle.

## D-056 — demo_e2e.sh: bash owns lifecycle, a Go driver owns chain logic; step 7 doubles as the cycle-finder proof

- **Date:** 2026-06-14 (task 0.10)
- **SPEC:** §1.3 (acceptance), §10 row 0.10, §12
- **Decision:** `scripts/demo_e2e.sh` owns PROCESS LIFECYCLE only (build, boot
  localnet, start/kill `conclave-solver`); all chain interaction and assertions
  live in a Go driver `cmd/conclave-demo` (reusing the unordered-blast
  gRPC/signing pattern), split into subcommands (`setup`, `cycle-and-assert`,
  `fallback`) so each phase is a fresh process re-reading on-chain state. Chosen
  because a SealedIntent's `ciphertext` is a serialized `IntentPayload` —
  impractical to build via autocli CLI flags. The solver runs as a FRESH bonded
  key `solver2` (provisioned + faucet-funded in `setup`), exercising
  `MsgRegisterSolver` end to end (§1.3 step 5), distinct from the genesis-bonded
  `solver1`. Cross-process state for alice's ephemeral session key is carried in
  the test keyring (`setup` creates `alice-session` via `NewMnemonic`; later
  phases load it). The 3 cyclic intents are submitted CONCURRENTLY (one block) so
  they seal into one session; `cycle-and-assert` asserts same-session before
  waiting. **Step 7 proves two things at once:** with the solver killed, (a) a
  mutually-fillable 2-party swap clears via the in-consensus fallback and the
  driver asserts its session carried ZERO solver solutions (so it was the
  fallback, not a solver), and (b) a fresh 3-party cycle stays unmatched (never
  CONCLUDED over ~12 blocks) — the fallback is pairwise-only, so the cycle is
  proof that `solver/cycles` is load-bearing.
- **Rationale:** The §1.3 "kill the solver" step requires a real long-running
  solver process the script can start and kill, so bash is the natural lifecycle
  owner and Go the natural chain-logic owner. Folding the cycle-finder load-
  bearing proof into step 7 means demo_e2e.sh failing-to-clear-the-cycle-without-
  the-finder and succeeding-with-it is asserted in the same run, per the build
  plan. The cycle CANNOT be cleared by the fallback, so steps 4-6 clearing it is
  itself proof the solver did it. CONCLAVE-TODO(phase-2): align concurrent intent
  submission to a fresh forming window if boundary-split flakiness ever appears.

## D-057 — SDK simulation design: operation set, genesis, the v0.50 persistence workaround, tolerant ops, measured coverage

- **Date:** 2026-06-15 (task 0.11)
- **SPEC:** §10 row 0.11, §11 (testing), §3 (SDK simulation row)
- **Decision:**
  - **Per-module operations.** Each stateful custom module
    (`x/{sigil,mandate,veil,session}`) implements `module.AppModuleSimulation`
    (`GenerateGenesisState`, `RegisterStoreDecoder`, `WeightedOperations`) in a
    `simulation/` subpackage; `x/escrow` (protos-only) and `x/receipt`
    (store-less, D-052) contribute none. The coverage ENGINE is x/veil's
    operations: coordinated MATCHABLE intent sets (2-party coin swaps the §6.4
    fallback clears, and §1.3 3-party data/svc cycles only `solver/cycles` can
    clear), plus window-charging intents from mandate-bound session keys
    (within-ceiling and deliberately over-ceiling), plus malformed-payload
    intents (INVALID slash path). x/session's `ProposeSolution` REUSES the
    reference solver (`solver.Solve` + `solver/gate`, D-054/D-055) to build valid
    solutions over a live AUCTION session, exercising the real scoring→Conclude
    path — not just the fallback. AppModule constructors gained sim-only keeper
    refs (account/bank/sigil/mandate/veil) used only by `WeightedOperations`.
  - **v0.50 SimulateFromSeed persistence workaround (load-bearing).** The v0.50
    sim loop runs operations via `SimDeliver` AFTER `FinalizeBlock` has already
    called `workingHash()` (which flushed the begin/end-block state to the commit
    store); `Commit()` never re-flushes `finalizeBlockState`, so the post-
    FinalizeBlock operation writes are stranded and dropped — state never
    accumulates and the per-block invariants observe nothing. The fix is a
    baseapp `SetPrecommiter` option (`simPrecommitFlush`) that `Write()`s the
    finalizeBlockState cache to the commit store just before `cms.Commit()` (the
    precommiter runs there). `Commit=true` is required so FinalizeBlock's height
    advances. Without this the sim is silently vacuous (diagnosed via
    accounts-with-seq>0 == 0 while mint inflation persisted).
  - **Full op set, wrapped TOLERANT.** Operations are the FULL
    `SimulationManager` set (standard SDK modules + the six custom ones), each
    wrapped so a delivery error becomes a no-op. Standard ops are required to
    maintain the validator set and keep blocks busy (excluding them empties the
    set and aborts the run); they are wrapped tolerant because a standard op may
    sign with a mandate-bound grantee key whose tx the MandateDecorator CORRECTLY
    rejects (the msg is not in the mandate allowlist) — which unwrapped SDK ops
    treat as fatal. Tolerating op errors does NOT weaken the bug-finding gate:
    that gate is the per-block crisis-invariant panic, independent of op success.
    Ops are built with the APP's own `TxConfig` (not `MakeTestTxConfig`) so the
    custom msg types decode.
  - **Genesis.** `simtestutil.AppStateFn` (randomizes all modules incl. the
    custom genesis) wrapped to add uclv+uusdc to every sim account (the base
    genesis funds only the staking "stake" denom; staking stays in "stake",
    Conclave economics run in uclv/uusdc — consistent with `bootConclaveApp`).
    `inv-check-period=1` so crisis asserts every invariant on EVERY block.
  - **Measured, not asserted coverage.** Like 0.10's branch-distribution proof,
    the harness prints the executed operation mix (a sim-only `simcoverage`
    atomic registry, observation-only so it never touches the app hash) AND the
    consensus-side OUTCOME tallies read from committed state (intents by state,
    sessions CLOSED, solvers, etc.) — a green sim with CLOSED==0/CONCLUDED==0
    is treated as a vacuous failure. Sim tests gate behind `-Enabled`
    (`simcli.FlagEnabledValue`) so they skip in `make test`.
- **Rationale:** SPEC §10 row 0.11 demands a 500-block run with no invariant
  break and no non-determinism that actually EXERCISES the modules. The
  persistence workaround was the difference between a vacuous green run and a
  real one; the tolerant-full-op set was the difference between a run that
  aborts on validator depletion and one that completes. CONCLAVE-TODO(phase-1):
  re-evaluate the persistence workaround if the SDK v0.53 bump (D8) changes the
  sim/FinalizeBlock lifecycle.

## D-058 — Invariant taxonomy: what is and is not a crisis invariant; crisis-runs-first ordering

- **Date:** 2026-06-15 (task 0.11)
- **SPEC:** §6.1 invariants, §6.4 rule 2, §11.3, §11.6, app_config EndBlock order
- **Decision:** The 0.11 sim checks, every block, the already-registered module
  invariants (sigil: address-index, reputation-non-negative, revoked-credential-
  inactive; mandate: grantee→sigil, grantee-index) PLUS three new app-level
  accounting invariants registered in `app/invariants.go` (they need bank
  balance access and/or span >1 custom module's store, so they live at app level
  rather than forcing a cross-module keeper edge):
  - **veil-bond-custody:** veil module-account balance == Σ bonds of live-state
    intents (resting consequence of §6.3 bond escrow).
  - **solver-bond-custody:** session module-account balance ≥ Σ active solver
    bonds (≥ because the same account is also the settlement-escrow + fee pool).
  - **mandate-commitment-conservation:** the D-041 property promoted to a
    per-block crisis invariant — after the D-059 split it is on the LIFETIME
    `outstanding_commitment` (see D-059), GLOBAL form, holding across every
    reset/amend/reissue by construction. It IS a valid crisis invariant because
    crisis runs FIRST in the EndBlock order (`crisis → … → session → veil`): an
    intent about to expire/conclude is still counted AND still committed at
    check time, and is uncounted AND decommitted after the session/veil sweep —
    equality holds at the crisis-check boundary.
  Plus the SDK bank total-supply invariant (registered by x/bank), the resting
  proxy for value conservation.
- **What CANNOT be a crisis invariant (stated in code + here):**
  - Conservation of value within a CONCLUDE (§6.4 rule 2) is a TRANSITION
    property, not a single-snapshot predicate; enforced at solution validation
    (`CheckConservation`) + the conservation fuzz, with the bank-supply +
    custody invariants as the resting proxy.
  - Limit-satisfaction / uniform-price / counterparty-class (rules 3/4/5) are
    per-solution transition properties with no resting form.
  - Fallback/scoring determinism (§11.3) is a cross-run property, covered by the
    same-seed `TestAppStateDeterminism`, not crisis.
- **Rationale:** 0.11 asked exactly which accounting properties can be promoted
  to crisis invariants and which cannot. The crisis-runs-first ordering is the
  load-bearing fact that makes commitment conservation checkable per block.

## D-059 — FOUND-AND-FIXED interaction bug: window_spent conflated a lifetime commitment with per-period spend; split into outstanding_commitment

- **Date:** 2026-06-15 (task 0.11 — the first found-and-fixed interaction bug of the project)
- **SPEC:** §6.2 steps 4–5, §1.1 goal 3, §6.4 step 7
- **SUPERSEDES** the single-window-counter assumption in **D-025**, the
  amend/reissue window-reset semantics of **D-018/D-017a**, and the D-041 refund
  matrix's treatment of the window (the bond/window split it describes).
- **The bug (surfaced by the 0.11 simulation, not by any prior hand-authored
  test).** With the crisis commitment-conservation invariant registered, the
  500-block sim broke DETERMINISTICALLY at block 2: a grantee's window counter
  was less than the sum of charged_spend over its still-live intents. Root cause:
  the single `window_spent` field CONFLATED two physically distinct quantities
  with two different conservation laws —
  - a **refundable intent COMMITMENT** (MsgSubmitIntent give-legs + bond): a
    LIFETIME outstanding balance that moves only on submit (charge) and on the
    intent's terminate (release), conserved across epochs; and
  - **non-refundable SETTLED spend** (bank sends, etc.): a PER-PERIOD consumption
    that resets each window by design (SPEC §6.2 step 5).
  A window rollover (§6.2 step 5), an amendment (D-018), or a reissue (D-017a
  REPLACE) zeroed `window_spent`, which ALSO zeroed the live intent commitments —
  (a) freeing ceiling capacity while the intent was still outstanding, an
  OVER-COMMITMENT that weakens the §1.1-goal-3 "a leaked key is bounded"
  guarantee across a reset, and (b) making the later terminate-time refund clamp
  at the reset counter and silently vanish (a reachable capacity/accounting leak
  for exactly the long-lived agents Conclave targets).
- **The fix (Option X — separate the two quantities; chosen over the lighter
  Option Y, epoch-aware refund).** `Mandate` gains a `outstanding_commitment`
  Coins field (proto field 16). `ChargeWindow` now takes `(commit, settled)`:
  the MandateDecorator classifies each msg (`MsgSubmitIntent` → commit; bank
  sends → settled) and routes intent charges to `outstanding_commitment` (a
  LIFETIME accumulator, NEVER reset by rollover/amend/reissue) and settled spend
  to `window_spent` (per-period, still reset by §6.2 step 5). The per-tx ceiling
  applies to the tx total; the **window ceiling bounds TOTAL live exposure**
  (`outstanding_commitment + window_spent`), so a leaked key can never commit +
  spend beyond `spend_ceiling_window` even across a reset. `RefundWindowSpend`
  became `DecommitIntent`, releasing the exact charge from
  `outstanding_commitment` (the lifetime balance always holds the live charge,
  so the release is exact and never clamp-loses). Amend/reissue reset
  `window_spent` (settled per-period budget) but CARRY `outstanding_commitment`
  (the in-flight intents are still live). The crisis invariant returns to its
  GLOBAL form: `Σ outstanding_commitment == Σ charged_spend(live intents)`,
  holding by construction across every reset.
- **Why X over Y.** Y (keep one counter, make the refund epoch-aware) would fix
  the clamp-loss but NOT the over-commitment: it frees the live commitment's
  ceiling capacity at the period boundary, which is correct for settled spend
  and WRONG for a still-live refundable commitment — re-opening the §1.1-goal-3
  weakening. The clamp-loss was a SYMPTOM; the conflation of a lifetime quantity
  with a per-period one was the root. X separates two things that are genuinely
  separate; the GLOBAL invariant then holds by construction (the strongest
  outcome) instead of needing a per-epoch narrowing.
- **Tests (load-bearing, all green):**
  `TestCommitmentSurvivesRolloverNoLeak` (commitment survives a rollover; the
  post-rollover decommit is exact, never clamps), `TestOverCommitmentRejected
  AcrossRollover` (a live commitment keeps bounding the ceiling across a
  rollover — the test that proves Y would have been wrong), the §6.2 `TestFuzz
  WindowResetBoundary` stays green UNCHANGED (settled-spend per-period reset
  preserved), and the 0.7 window-accounting conservation fuzz now pins the new
  `outstanding_commitment`. The 500-block sim completes with the GLOBAL
  invariant intact and reaches real clearing (CLOSED>0, CONCLUDED>0).
- **Rationale:** This is the canonical 0.11 success — a real interaction bug no
  prior task could reach, found by the sim and fixed at the root. The split
  makes the window's two jobs (bound lifetime in-flight commitment; budget
  per-period settled spend) explicit and independently conserved.
  CONCLAVE-TODO(phase-1): the Veil `spend_commitment` envelope (§6.3, §8(e))
  binds the encrypted-intent commitment to the same `outstanding_commitment`.

## D-060 — D8 resolved: stay on SDK v0.50 through Phase 1; sigverify fork retained

- **Date:** 2026-06-15 (Phase-0 → Phase-1 boundary planning session)
- **SPEC:** §14 D8, §3 (version pins), §8 (vote extensions)
- **RESOLVES** the SPEC §14 D8 open decision (slated for re-evaluation at exactly
  this boundary). See docs/PHASE1_DESIGN.md §1.
- **Decision:** Do NOT bump to SDK v0.53 for Phase 1. Keep cosmos-sdk v0.50.15,
  cometbft v0.38.23, ibc-go v8.8.0, block-sdk/v2 v2.1.5, the goleveldb replace
  pin, and the `app/unordered/sigverify.go` fork (D-027) plus the TxBody
  extension-option unordered encoding (D-026).
- **Decisive reasoning:** vote extensions — the entire Phase-1 consensus surface
  — are a CometBFT v0.38 / SDK v0.50 feature ALREADY available on the current
  pins, so the bump buys Phase 1 nothing it needs while re-baselining the whole
  dependency graph at the moment the largest new crypto/consensus surface lands.
  Isolate variables: do not perturb determinism (the D-057 sim workaround, §11.3
  gate) and the laned mempool while introducing threshold crypto + DKG + VE. The
  fork is byte-identical to upstream for ordered txs and behavior-pinned by the
  conformance suite — a low, bounded, tested cost.
- **Empirical spike (throwaway, Go module proxy; recorded so the flip-condition
  is accurate, not a stale memory assumption):**
  - `skip-mev/block-sdk/v2` latest = **v2.1.5** (our pin) → requires cosmos-sdk
    v0.50.3, cometbft v0.38.5. No newer v2 release; no `block-sdk/v3` module
    path. **block-sdk has no SDK-v0.53 line at all** — the gating dependency.
  - `cosmos/ibc-go/v8` latest = **v8.8.0**; the entire v8 line targets cosmos-sdk
    v0.50.x. SDK v0.53 would force an ibc-go MAJOR bump (v8 → the v0.53 line),
    not a point release.
  - cosmos-sdk v0.53 is mature (through v0.53.7) — the SDK is ready; its
    ecosystem (block-sdk, ibc-go v8) is not.
- **Flip-condition for the Phase-1 → Phase-2 re-evaluation:** bump only when ALL
  of (a) an MVS-clean block-sdk release targeting SDK v0.53 exists (today: none),
  (b) a v0.53-compatible IBC line is adopted (today an ibc-go major bump), and
  (c) the D-057 sim/FinalizeBlock lifecycle is confirmed intact or cheaply
  replaced. The Phase-2 wasmd integration (§10 task 2.2) forces a dependency
  revisit regardless — the natural re-evaluation point. CONCLAVE-TODO(phase-1) at
  the fork site is re-pointed to this Phase-2 boundary in task 1.2.

## D-061 — SPEC §8 "threshold encryption" resolved: kyber primitives + an authored encryption layer

- **Date:** 2026-06-15 (Phase-1 planning session)
- **SPEC:** §8, §6.3 ThresholdScheme, §14 D4
- **UNDER-SPECIFICATION RESOLVED — recorded, not silent:** SPEC §8 names BLS
  threshold encryption via the drand/kyber lineage, but those libraries ship
  threshold *signatures* (`tbls`) + DKG, NOT threshold *encryption*. See
  docs/PHASE1_DESIGN.md §3 (FLAG I).
- **Decision:** Use `go.dedis.ch/kyber/v3` + `drand/kyber-bls12381` for the DKG
  and threshold primitives (mature, drand-proven, the §8/D4-named lineage,
  MPL-2.0 file-level copyleft — OK for linking), and **author** the threshold-
  encryption layer (encrypt-to-`EpochPubKey`, verifiable partial decryption)
  on top, referencing drand `tlock`'s IBE/KEM construction. Do NOT build BLS or
  DKG from scratch. Because the encryption layer is authored, it has no prior
  audit and is therefore the heart of the §8(d) external audit (task 1.8).
- **Hard library criteria (a construction failing any is disqualified in 1.3):**
  (1) threshold *encryption*, not just signatures; (2) **verifiable partial
  decryption** — each `RevealShare` carries a pairing/NIZK proof it is a correct
  partial decryption under the validator's published key share, so one bad share
  cannot poison `Combine` undetectably; (3) **deterministic** on-chain `Combine`
  + verification (no fresh randomness — §11.6). DKG randomness is off-chain
  (a validator protocol), client `Encrypt` is off-chain — both acceptable.
- **Rationale:** §8 says do not implement DKG ad hoc; kyber supplies the audited-
  adjacent DKG + primitives. The encryption layer is the irreducible authored
  surface; naming it now scopes the 1.8 audit precisely.

## D-062 — ThresholdScheme.EpochSetup must be revised for a real DKG (not a pure implementation swap)

- **Date:** 2026-06-15 (Phase-1 planning session)
- **SPEC:** §6.3 ThresholdScheme, §8
- **CORRECTION TO A SPEC/PROMPT CLAIM — recorded loudly:** §6.3 frames Phase 1 as
  "swaps the scheme implementation only." That holds for the CONSUMING state
  machines — `Combine` (Reveal), client `Encrypt`, `Threshold(n)` keep their
  signatures — but NOT for key generation. See docs/PHASE1_DESIGN.md §3 (FLAG II).
- **Decision:** The current `EpochSetup(valset []Validator) (EpochPubKey,
  []KeyShare, error)` returns ALL key shares to a SINGLE caller, which a real
  distributed DKG never does (no node holds all shares — that defeats the
  threshold model). `EpochSetup` is a Phase-0 stub artifact and is REDESIGNED in
  task 1.3 into a multi-round DKG sub-protocol (per-validator round functions +
  a transcript/state object), with `KeyShare` staying local to each validator
  and never aggregated. `RevealShare` gains the D-061 validity proof. The
  consuming code (Reveal, client Encrypt) is unaffected.
- **Rationale:** Surfacing the interface limitation rather than quietly
  reshaping it keeps the §6.3 contract honest; the redesign is confined to key
  generation, where the stub could not model the real protocol.

## D-063 — Phase-1 Task 1.0: determinism-harness hardening (AUDIT F-1); the gate immediately surfaced a real mandate-commitment bug

- **Date:** 2026-06-17 (Phase-1 Task 1.0)
- **SPEC:** §11.3 determinism gate; AUDIT F-1; D-057 (sim design); D-059/D-058
  (the surfaced invariant); §1.1 goal 3
- **RESOLVES** AUDIT finding F-1 (the determinism HARNESS was flaky and so
  untrustworthy as a gate, even though the CHAIN is verified deterministic).
  This is the first Phase-1 build task — the gate must be dependable BEFORE
  Phase 1 adds vote extensions + DKG, the largest new nondeterminism surface.
- **Decision (harness changes only — `app/sim_test.go`, `app/testdata/
  sim_params.json`, `scripts/sim_determinism.sh`, `Makefile`):**
  - **Reproducible seeding.** The determinism/survival runs are driven by an
    explicit, committed seed vector (`-ConclaveSimSeeds`, default
    `1,2,7,12,42` — curated to completing seeds, see below); the un-seeded
    `rand.Int63()` self-seeding is gone. No un-seeded global RNG remains in the
    harness's control flow.
  - **Validator-set survivability via genesis tuning.** `testdata/
    sim_params.json` pins, through the SDK's `AppParams.GetOrGenerate` (so the
    full `AppStateFn` incl. its not-bonded-pool reconciliation is reused
    unchanged): `initially_bonded_validators=30` (vs the SDK's random 1..299,
    which lands at 1–3 on unlucky seeds and is then jailed/unbonded to zero) and
    `signed_blocks_window=1000` (widening the window is realistic, not
    suppression). **Slashing fractions stay at SDK defaults on purpose (floor-
    high, keep-jailing-realistic):** disabling downtime slashing would make the
    set survive by turning OFF the depleting mechanism, never exercising the
    jailing/unbonding/churn surface — load-bearing in Phase 1 (validators
    produce vote extensions and hold DKG shares; a leaver-mid-epoch-holding-a-
    share is a real case, PHASE1_DESIGN §2(b)). `TestValidatorSetSurvives500`
    reports the per-seed end-state composition (bonded/unbonding/unbonded/jailed)
    as evidence jailing/unbonding fire heavily (e.g. seed 1: 529 jail events,
    1624 unbondings over 500 blocks) while the active set stays above the floor —
    the "jailing live but floor holds" outcome, not the "nothing jails" mode.
  - **Curated to COMPLETING seeds (noise removal, not signal suppression).** The
    SDK sim op/evidence mix (equivocation-tombstoning at ~1 jail/block plus
    voluntary undelegation) drives the validator set to zero within 500 blocks
    for a large fraction of seeds — physically impossible on a real chain
    (unbonding takes weeks; mass equivocation is economically irrational).
    Empirically ~half of seeds deplete: {1,2,7,42} survive 500 blocks, while
    {3,5,8,11,99} deplete. **Genesis SIZING cannot fix this** — depletion is rate-
    driven, not size-driven: seed 99 depleted at floors 50/80/150, and seeds 5
    and 8 depleted even at floor=150/max=200. (Pinning a param also skips its
    `GetOrGenerate` RNG draw, so each floor/cap is a re-randomized run, not a
    monotonic search.) So the redirect's "raise the floor if it doesn't survive"
    fallback is empirically dead; floor stays at 30 and the vector is CURATED to
    completing seeds instead. Crucially, a non-completing run yields NO
    determinism information (no comparable cross-process hash), so it is not a
    data point being suppressed — it is noise being removed. **The fail-on-
    depletion conversion STAYS** (next bullet), so a committed seed that USED to
    complete and later mass-depletes still fails the gate — curation excludes a
    seed that never carried signal; it does not hide a future regression. This is
    sound curation, explicitly NOT the 0.11 vacuous-gate anti-pattern (which would
    be dropping a seed that *finds a bug* — see seed 7 below, which is KEPT).
  - **SKIP → hard FAILURE.** `SimulateFromSeed` skips on an empty validator set
    at genesis (`x/simulation/simulate.go` ~L109) and on mid-run depletion
    (~L251); `tb.Skip` there `runtime.Goexit`s and silently marks the test
    SKIPPED — a gate that can skip itself is not a gate. **Deviation from the
    approved plan, recorded:** a wrapping `testing.TB` cannot intercept this —
    the SDK type-asserts the CONCRETE `*testing.T` in `getTestingMode`
    (`x/simulation/util.go:24`), so a wrapper panics. The portable equivalent
    is used instead: run each sim in a subtest (the SDK gets a real
    `*testing.T`) and convert an observed `st.Skipped()` into a parent
    `t.Fatalf` (`runSimSeed`). Same guarantee. `stopEarly` (the no-proposer
    depletion path) is likewise a hard failure.
  - **CI target.** `make sim-determinism` runs the determinism sim over the
    vector TWICE in independent processes and fails on any SKIP, abort, or
    app-hash mismatch within OR across the two runs (cross-process
    reproducibility). Not part of `make test` (sim is `-Enabled`-gated, D-057).
  - **D-057 PRESERVED verbatim:** `simPrecommitFlush` and the tolerant full-op
    set (`simOperations`/`tolerantOp`) are unchanged; only seeding and genesis
    survivability change. The trajectory-sampling app replicates only
    `simPrecommitFlush`'s one-line cache flush (the named function is untouched).
- **Determinism gate runs inv-check=0 (orthogonal to invariant-checking).**
  Determinism is byte-identical app hashes; the per-block crisis invariant is a
  SEPARATE bug-finding gate (`TestFullAppSimulation`, inv-check=1). Crisis checks
  write no state, so inv-check on/off does not change the hashes for clean seeds;
  but with inv-check=1 a DETERMINISTIC invariant break aborts the run before a
  comparable hash is produced. Running the hash gate at inv-check=0 lets every
  COMPLETING seed yield a cross-process hash — including the bug-bearing seed 7,
  which is therefore KEPT in the vector (dropping it would be signal
  suppression). The determinism gate is GREEN across the curated vector.
- **THE GATE EARNED ITS KEEP ON ITS FIRST RUN — a found defect, recorded:** the
  first hardened run (inv-check=1) deterministically (byte-identical across two
  independent processes: same grantee, `5648205uclv`, mandate id) tripped the
  D-059 `mandate-commitment-conservation` crisis invariant at seed 7 —
  `outstanding_commitment=0` while a still-live intent carries charge. Root
  cause: `IssueMandate` (`x/mandate/keeper/mandate.go`) carries
  `outstanding_commitment` to the replacement mandate only when the superseded
  mandate is ACTIVE (`ActiveMandateByGrantee` found); a revoke-then-reissue (or
  expire-then-reissue) leaves the live intent's charge orphaned, breaking the
  conservation AND weakening the §1.1-goal-3 leaked-key bound. This is a real
  D-059-SIBLING accounting bug random seeding had masked — exactly what F-1
  exists to expose. The red state lives **behind inv-check** (the inv-check=1
  bug-finding path over seed 7) and is orthogonal to the determinism/survival
  vector curation; it is documented here, not papered over, and is the immediate
  next task.
- **The fix is a SEPARATE, dedicated task (next, fresh session) — see D-064
  when written.** It is a D-059-class commitment-ownership design decision, not
  an inline patch, and it interacts with D-017: the revoked mandate record is
  RETAINED deliberately (deleting its grantee-index entry would silently
  re-promote the session key to root passthrough under the ante's "no mandate
  entry = root key" rule). The fix must carry the commitment across
  revoke/reissue WITHOUT a record deletion that re-promotes the key — precisely
  the cross-decision interaction that needs its own plan-stop.
- **Rationale:** Fix the gate while it still guards known-good code, so any NEW
  nondeterminism in Phase 1 is unambiguously a real defect, not harness noise.
  Treating the surfaced mandate bug as its own task (not folding it into a
  harness task framed "no module code") gives a commitment-ownership decision
  the dedicated attention the last D-059-class decision needed (Option Y would
  have shipped a subtle hole).

## D-064 — FOUND-AND-FIXED (Task 1.0b): the gate's mandate-commitment break is the root→mandated transition, NOT revoke/reissue; FRESH issuance ADOPTS live root-era exposure

- **Date:** 2026-06-18 (Phase-1 Task 1.0b — the dedicated fix forward-referenced by D-063)
- **SPEC:** §6.2 steps 4–5, §6.3, §1.1 goal 3, §6.4 step 7
- **EXTENDS** D-059 (Option X: the lifetime `outstanding_commitment` vs per-period
  `window_spent` split) to the **mandate-issuance boundary**, and **LOUDLY
  SUPERSEDES D-063's stated root-cause hypothesis** (recorded as a correction, in
  the D-062 style — not silently reshaped).
- **D-063's hypothesis was a GUESS; the instrumented trace found the truth.**
  D-063 conjectured the seed-7 break was `IssueMandate` carrying
  `outstanding_commitment` "only when the superseded mandate is ACTIVE," i.e. a
  revoke/expire-then-reissue orphaning the carry. **That path is already correct:**
  `ActiveMandateByGrantee` returns `found=true` for revoked AND expired mandates
  (the 0x02 index is retained, D-017), so the carry fires on every reissue —
  verified statically and by the sim (55 revokes / 95 issues / 127 amends, none
  broke on that path). Targeted instrumentation of grantee
  `conc1twg…`/mandate `2c0b45…` (the gate working as intended) showed the REAL
  sequence: the grantee **submitted intents while UNMANDATED** (h=3/94/100 — a
  sigil **controller**, so it resolves through the sigil reverse index and may
  submit; the ante takes the root/controller passthrough, so **no `ChargeWindow`
  ever ran** and no commitment was recorded), its sigil binding was then removed
  (**controller rotation**, h≈112 — `SigilIDByAddress` now returns "no sigil"),
  making it **FRESH-mandate-eligible** (the D-017(c) conflict guard is skipped),
  and a FRESH mandate was issued (`oc=0`). The commitment-conservation invariant
  then **adopted** the grantee's still-live root-era intents (`charged_spend`
  persisted by veil for ALL intents, D-041) and found `oc=0 ≠ Σ charged_spend(live)
  = 5648205uclv`. So: **root-key-submit → later-FRESH-mandate**, not revoke/reissue.
- **The design question (the real fork, not "which field holds the truth"):** what
  happens to a live root-era intent's real exposure (`give+bond`) at the moment its
  key becomes mandated? Three options were analysed:
  - **(1) Zero it / leave uncounted** (faithful-`charged_spend`): conservation
    holds, but the key is **under-bounded across the transition** — it can commit
    fresh mandated intents to the FULL ceiling while the live root-era intent is
    still committing real funds. This is exactly the **D-059 Option-Y
    over-commitment hole through a different door**: §1.1-goal-3 must hold from the
    instant of issuance, and conservation-by-zeroing satisfies the INVARIANT but
    not the bounded-key GUARANTEE. Not defensible as "bounded elsewhere" — D-025
    (slashable bond burn) and D-059 (`give+bond` is the exposure `oc` exists to
    bound) already define this as exactly what the ceiling must cover. REJECTED.
  - **(2) ADOPT it into the new mandate at FRESH issuance** (CHOSEN): conservation
    holds AND the key is bounded from issuance. Cost: a mandate→veil read.
  - **(3) FORBID/defer issuance while live root-era intents exist:** same
    enumeration cost as (2) but more restrictive and safety-backwards (you most
    want to clamp a possibly-compromised key immediately, not wait ≤1 TTL for its
    root intents to drain), and it leaves those intents unbounded-as-root. REJECTED.
- **Commitment-OWNERSHIP model (the keystone):** `outstanding_commitment` is owned
  by the **mandate-lineage that charged the intent**. A root/controller intent was
  charged to **no mandate**; its `charged_spend` is persisted (D-041) but owned by
  nothing until — and unless — its key becomes mandated, at which point the FRESH
  mandate **adopts** the live exposure. `charged_spend` itself is left UNCHANGED
  (`give+bond`), which is the load-bearing detail: adoption sets `oc += give+bond`
  and `DecommitIntent` at terminate releases `oc -= give+bond` — **the same
  figure**, so adoption and release are exact inverses. (Zeroing `charged_spend`
  AND adopting would strand `oc` positive forever, breaking conservation the other
  way — which is why (1)-plus-adoption is incoherent and the chosen fix touches
  neither `charged_spend` nor `DecommitIntent`.)
- **DecommitIntent resolution trace (the easy-to-miss half):** the adopted intent
  keeps `charged_spend=give+bond`; at terminate, `settleRefund → DecommitIntent
  (owner, give+bond)` resolves `ActiveMandateByGrantee(owner)` to the SAME mandate
  that adopted it (or its carry-successor), releases `min(oc, give+bond)=give+bond`
  exactly → `oc -= give+bond`. The refund lands where the charge lives. Proven
  end-to-end (adopt → over-ceiling reject → terminate → `oc` drops by exactly the
  adopted figure → now-fitting charge succeeds) by
  `TestRootToMandatedAdoptionAndSelfCorrection`.
- **The fix (Construction B — adoption CONFINED to the FRESH branch):**
  `IssueMandate` seeds the new mandate's `outstanding_commitment`:
  REPLACE/reissue **inherits** the superseded balance (D-059, byte-identical to
  before); a **FRESH** issuance (`found==false`) **adopts** `Σ charged_spend` over
  the grantee's live intents. Orphans cannot exist on the REPLACE branch (a
  mandate-bound key cannot also submit unmandated root intents, D-017(c)), so
  every live intent since the FIRST mandate is already in the carried balance —
  the adoption is correct *only* on FRESH and is guarded there in code
  (`TestAdoptionConfinedToFreshBranch`: a refactor moving the scan out of
  `found==false` double-counts and fails the test, the 0.6 registration-order
  discipline).
- **The veil↔mandate cycle is broken by dependency inversion, NOT a new import.**
  `x/veil` already depends on `x/mandate` (`DecommitIntent`); a direct
  `x/mandate → x/veil` import would cycle and would re-introduce exactly the
  cross-module edge D-058 kept at app level. Instead the mandate keeper holds an
  injected `liveExposureFn`, wired in `app.go` (`MandateKeeper.SetLiveExposureFn(
  VeilKeeper.SumLiveChargedSpendByOwner)`) immediately after the veil keeper
  exists and BEFORE the keeper value is captured by module/ante wiring — the same
  post-construction pattern as `SetVeilKeeper` for veil↔session. `nil` ⇒ no
  adoption (mandate unit tests without veil). `SumLiveChargedSpendByOwner` defines
  "live" IDENTICALLY to the conservation invariant (every state except
  EXPIRED/CANCELLED/CONCLUDED) and is deterministic + cold-path (issuance only).
- **Both invariants hold simultaneously:** (1) **conservation** — `Σ oc ==
  Σ charged_spend(live)` by construction across the root→mandated transition;
  (2) **no re-promotion (D-017)** — the fix writes only the `oc` VALUE on the new
  record; it deletes no record and rewrites no index, so D-017's "index entry
  never deleted ⇒ key never silently regains root passthrough" property is
  preserved verbatim.
- **Severity (honest):** low practical reachability — it needs the
  controller-binding-loss path (a once-submitting key losing its sigil binding,
  then being mandated while holding live intents), which the sim reached unforced
  at seed 7 but which is a narrow key-lifecycle window (exposure self-limited to
  ≤1 intent TTL). Fixed completely anyway, because it is a genuine §1.1-goal-3
  weakening by the project's own accounting — the class D-059 took the harder
  Option X to avoid.
- **Tests (load-bearing, red-first verified):**
  `TestAdoptLiveExposureOnFreshIssuance` (FRESH adopts; bounded from issuance —
  RED without adoption: `oc=∅`, over-ceiling charge wrongly succeeds),
  `TestAdoptionConfinedToFreshBranch` (scan runs once on FRESH, never on
  REPLACE/amend), `TestRootToMandatedAdoptionAndSelfCorrection` (the seed-7 shape
  through real keepers incl. rotation + full adopt/release self-correction — RED
  without the fix), `TestSumLiveChargedSpendByOwner`. The determinism gate at
  inv-check=1 passes seed 7 and the full curated vector after the fix; the D-059
  suite stays green (REPLACE/carry untouched).
- **Rationale:** the gate (D-063) earned its keep again — it surfaced a real
  D-059-sibling hole that random seeding had masked, and the dedicated plan-stop
  caught that the smallest-diff fix (1) reopened the over-commitment gap. Adopting
  at issuance keeps the bound attached from the instant a key becomes mandated,
  which is the §1.1-goal-3 contract.
  CONCLAVE-TODO(phase-1): the Veil `spend_commitment` envelope (§6.3, §8(e)) binds
  the encrypted-intent commitment to the same `outstanding_commitment`; the
  root→mandated adoption applies identically to those commitments.

## D-065 — `spend_commitment` (Task 1.1): a UNIFORM ante valuation (never reads give legs), empty=zero, Reveal-slash as the SOLE enforcement; commitment-derived bond

- **Date:** 2026-06-19 (Phase-1 Task 1.1 — the load-bearing pre-crypto chokepoint task)
- **SPEC:** §6.3 "Spend commitment", §8(e), §6.2 steps 4–5, §1.1 goal 3, §11.6
- **RESOLVES** the SPEC §6.3 / §8(e) `spend_commitment` requirement and the
  CONCLAVE-TODO(phase-1) markers in D-021/D-025/D-040/D-041/D-059/D-064 and the
  ante's `intentSpend`. **SIMPLIFIES / SUPERSEDES** the encrypted-classification
  and reject-if-absent framing in the PHASE1_DESIGN §4 draft (recorded loudly as a
  correction, D-062/D-064 style — not silently reshaped).
- **The problem (why this lands before encryption, Task 1.6).** Once `ciphertext`
  is opaque, the `MandateDecorator` (§6.2, the agent-safety chokepoint) cannot read
  give legs to value spend intent. Without a plaintext commitment a stolen key
  could submit unbounded-value encrypted intents that pass the ceiling, breaking
  §1.1 goal 3. The fix is a plaintext `spend_commitment` on the always-plaintext
  `IntentEnvelope` (repeated Coin, `Coins`), built and proven in stub mode
  (`Encrypt` stays identity) so the chokepoint logic is exercised against the exact
  path that ships, while the plaintext payload still lets the Reveal cross-check.
- **Three pieces (built):**
  1. **Envelope field** `spend_commitment = 5`. `ValidateEnvelope` checks it
     **structurally only** (`Coins.Validate()`: sorted, no-dup, non-negative);
     **empty is a valid zero declaration** (extends D-042).
  2. **Uniform ante binding.** `MandateDecorator` values `MsgSubmitIntent` as
     `spend_commitment + bond` **always**, in stub AND encrypted mode, **never
     reading give legs**. Routes to `outstanding_commitment` (D-059); the per-tx
     and window-exposure (`outstanding_commitment + window_spent`) ceilings bind to
     it exactly as they bound `giveLegSpend + bond` before. The `PriceSource` is
     removed from `MandateDecorator` (now dead there); the veil keeper keeps its
     own for the Reveal.
  3. **`charged_spend = spend_commitment + bond`** persisted (D-041), and the
     **Reveal slash**: after decrypt, value the actual give legs (`giveLegValue`,
     behind recover, §11.6) and if any per-denom actual give > `spend_commitment`
     → INVALID + 50% bond slash to the community pool (D-040), no window refund
     (the charge persists, conservation intact). Over/exact/coin-give-free → OPEN.
- **Two decisive design forks (resolved with the architect, both pinned here):**
  - **No per-intent "encrypted" classification; the mechanism is UNIFORM.** There
    is no encrypted signal in stub mode (`StubScheme` is pure identity), and a
    stub-vs-encrypted split would make stub testing prove a *different* code path
    than Task 1.6 ships — defeating the build-in-stub strategy. So the ante's
    `intentSpend` give-leg read is DELETED and replaced unconditionally with the
    `spend_commitment` read. Identity across modes is exactly what makes stub
    testing prove the encrypted-mode chokepoint.
  - **No `ErrMissingSpendCommitment`; empty = zero; Reveal-slash is the SOLE
    enforcement.** The `repeated Coin` proto cannot distinguish "absent" from
    "zero", and a coin-give-free intent (data/svc-only, the F-3 boundary) has a
    legitimately-empty commitment — so an up-front reject cannot be implemented
    faithfully and is not needed. An empty commitment that hides a positive coin
    give is already deterred: the bond is charged and 50% slashed at the Reveal
    when actual give exceeds the (zero) commitment. Enforcement lives at the one
    place the give legs are readable.
- **Bond magnitude is now COMMITMENT-derived, not give-value-derived (a behavior
  change `RequiredBond` readers must know).** The §6.3 face value feeding the bond
  is `faceValueScalar(spend_commitment)`. Same uniformity argument as the ante: the
  msg server is blind to give legs in encrypted mode, so a give-derived bond would
  size differently in stub mode than the shipping path. Consequence:
  over-declaration costs a larger bond — correct self-penalizing alignment; an
  honest declaration sizes the bond exactly as the give-derived bond did before.
- **The comparison iterates the ACTUAL GIVE coins** against
  `spend_commitment.AmountOf(denom)` (NOT the commitment coins): a give in a denom
  the commitment omits compares against 0 and fires, so an undeclared-denom give is
  itself an under-declaration and cannot be dodged. `Coins.IsAnyGT` is deliberately
  NOT used — its `!amt.IsZero()` clause misses the empty-commitment case.
- **F-3 inherited unchanged.** Data/svc give legs value 0 under
  `FaceValuePriceSource`, so a coin commitment does not bound them and a
  coin-give-free intent honestly declares zero and opens (never slashes). The
  D-044 PriceSource swap is the proper fix and stays a separate, non-Phase-1 task;
  CONCLAVE-TODO(phase-0) marker carries it.
- **D-059/D-064 accounting untouched.** Ante charge, persisted `charged_spend`, and
  `DecommitIntent` release are all `commitment + bond`, so they agree and the
  commitment-conservation crisis invariant holds by construction — only the
  valuation *source* changed (give legs → declared commitment).
- **Tests (red-first verified on the slash path):**
  `TestRevealUnderDeclarationSlashes` (incl. the undeclared-denom variant
  `TestRevealUndeclaredDenomSlashes`) and `TestFuzzCommitmentDeclaration` were
  confirmed RED with the cross-check disabled, then green with it; `TestReveal{Over,
  Exact}DeclarationOpens` and `TestRevealDataOnlyEmptyCommitmentOpens` (the F-3
  inverse-pin) hold the OPEN side; the §6.2 ante suite, the 0.7/0.11 conservation
  fuzz, the 0.8 session clearing tests, the 0.9 receipt proofs, and the §1.3 e2e
  (over-ceiling intent now trips the ceiling via its declared commitment) all green
  after declaring commitments in their intent builders. `make lint test` EXIT=0;
  the determinism gate stays byte-identical cross-process (no golden hash). `Encrypt`
  stays identity.
- **Rationale:** uniform-across-modes valuation is the property that lets a
  stub-mode build de-risk the post-crypto chokepoint; pushing all enforcement to
  the Reveal is forced by the ciphertext being unreadable in ante and by the proto's
  inability to distinguish absent from zero. The result is strictly simpler than the
  §4 draft and provably the same code path that ships in Task 1.6.

## D-066 — Task 1.3: BLS threshold-encryption construction = tlock (IBE-over-threshold-BLS); library/version selection; `EpochSetup` redesigned to a DKG sub-protocol

- **Date:** 2026-06-19 (Phase-1 Task 1.3)
- **SPEC:** §8 (the Veil for real), §6.3 ThresholdScheme, §11.6 determinism;
  PHASE1_DESIGN §2/§3 (FLAGS I/II/III); resolves the construction left open by
  **D-061** and the interface revision mandated by **D-062**. OFFLINE task — no
  consensus wiring; the live chain keeps `reveal.StubScheme` until Task 1.6.
- **Construction (the §8(d)/Task-1.8 audit unit):** **drand `tlock` timelock
  encryption** (Gailly–Melissaris et al., a published, network-deployed scheme —
  adopted, NOT authored). Hybrid KEM/DEM: a random DEK encrypts the payload
  (AES-256-GCM, the DEM); the DEK is Boneh–Franklin-IBE-wrapped to a per-
  ciphertext identity (the KEM); the IBE decryption key for that identity is a
  **threshold BLS signature** on it. Per-share validity proof = the **intrinsic
  pairing check** `e(σ_i,g2)==e(H(id),PK_i)` (`tbls.VerifyPartial`) — no separate
  NIZK (FLAG III satisfied by construction; the share IS the proof).
  - **Group orientation:** signatures / IBE-decryption-keys on **G1**, public
    keys / epoch master on **G2** (the drand "sigs-on-G1" scheme). Matched pair:
    `ibe.EncryptCCAonG2(master∈G2, id, dek)` ⇄ `ibe.DecryptCCAonG2(σ∈G1, ·)`,
    where `σ = s·H_G1(id)` is exactly the BLS signature on `id` under
    `master = s·G2`. The recovered threshold signature on `id` IS the IBE
    decryption key. The SAME pairing suite drives IBE and tBLS so the G1
    hash-to-curve DST matches (the `id` bytes IBE hashes are the bytes tBLS signs).
- **Authored vs library boundary (held literally — the audit contract):**
  - **LIBRARY** (`github.com/drand/kyber` + `github.com/drand/kyber-bls12381`):
    pairing groups + hash-to-curve, secret sharing & Lagrange (`share`), threshold
    BLS sign/verify (`sign/tbls`), the Pedersen **DKG** (`share/dkg`), and the
    **BF-IBE** primitive (`encrypt/ibe`).
  - **AUTHORED** (`crypto/threshold`): (1) the KEM/DEM hybrid wrapper (stdlib
    AES-256-GCM, zero new dep); (2) the **identity-derivation** rule (see below);
    (3) the `reveal.ThresholdScheme` adapter; (4) the share-verification **gating**
    before recovery. No novel cryptographic primitive is authored.
- **Identity derivation (AUTHORED, security-relevant — a documented divergence
  from tlock):** `id = SHA-256(DST ‖ C_dem)`, `DST = "conclave/veil/ibe-identity/v1"`.
  tlock's identity is an EXTERNAL label (a future beacon round the client knows in
  advance, via `scheme.DigestBeacon`). Conclave clients **cannot predict their
  future ordering position** at encryption time, so the identity instead binds to
  the **DEM body** (fixed before KEM encapsulation — which also breaks the
  circularity, since the KEM output depends on the identity). This is forced by
  Conclave's architecture, not an improvisation of tlock's hashing: the identity's
  hash-to-curve is still performed inside the library IBE with its own domain
  separation; only the *preimage bytes* differ (DEM body vs. round), which they
  must. Per-ciphertext identity ⇒ revealing one ciphertext's shares cannot decrypt
  another. **Flagged for the Task-1.8 audit** as the one authored, security-bearing
  choice that diverges from the reference.
- **Two INDEPENDENT barriers (gate = liveness, IBE FO check = integrity),
  proven.** `Combine` runs `VerifyPartial` over every share and excludes any that
  fails BEFORE recovery; recovery uses the **raw, non-verifying** Lagrange
  primitive (`share.RecoverCommit`), NOT `tbls.Recover` (which self-filters bad
  shares and would mask the gate's role). Empirically, bypassing the gate makes
  `Combine` **ERROR** (`"invalid proof: rP check failed"`) on a forged-but-valid-
  point share — RED-without-gate, demonstrated not asserted. The precise division
  of labour: the **gate is the LIVENESS/ROBUSTNESS barrier** — it excludes
  adversarial shares in O(n) so recovery succeeds from ANY t honest shares;
  without it one bad share poisons the first-t Lagrange set and forces retry →
  CANCEL of a decryptable intent (a single-bad-validator DoS) — this robustness
  is what FLAG III buys. The **library IBE FO/CCA rP check is the INDEPENDENT
  INTEGRITY barrier** — a wrong recovered signature deterministically ERRORS,
  never silently corrupts into a plausible wrong plaintext. (An earlier draft
  called the gate the "sole guard against a poisoned recovery"; that overstated
  it — integrity rests on the audited library FO check, not the authored gate,
  which is the safer division and the corrected claim.)
- **t-1 boundary is information-theoretic, not an early reject.** At the raw
  primitive, t-1 shares make `share.RecoverCommit` ERROR ("not enough good public
  shares") — there is NO "wrong σ" to compute, because t-1 points underdetermine
  a degree-(t-1) polynomial (Shamir perfect secrecy: t-1 shares reveal zero about
  σ). The "not yet vs. rejected" liveness semantic is delivered at the `Combine`
  API: t-1 *valid* shares → `ErrInsufficientShares` (retry next block), distinct
  from the malformed/INVALID path; and because the gate runs FIRST, (t-1 genuine
  + 1 forged) collapses to t-1 valid → retry — a bad share cannot fake progress
  to t.
- **`EpochSetup` redesign (D-062 / FLAG II):** the Phase-0 single-caller
  `EpochSetup(valset) ([]KeyShare)` is REMOVED from `reveal.ThresholdScheme`
  (which trims to the pure consuming surface: Encrypt / PartialReveal / Combine /
  Threshold). Key generation becomes a multi-round **`DKGParticipant`** (wrapping
  `share/dkg`: `Round1Deal → Round2Response → Finalize`) producing node-local
  `EpochKeyMaterial` — the secret share NEVER aggregates at one node. The offline
  `RunDKGInProcess(n,t)` harness drives the rounds in-process passing only public
  bundles (Tasks 1.3/1.4); real transport is the Task-1.5 design sub-review.
  `StubScheme` keeps satisfying the trimmed interface; the chain is untouched.
- **Determinism (§11.6):** `Encrypt` is client-side/off-chain (`crypto/rand`).
  The on-chain paths take NO fresh randomness — a BLS signature share is
  deterministic (`σ_i = sk_i·H(id)`), the gate is a deterministic pairing check
  over a sorted/deduped share set (no map-iteration into output), and Lagrange +
  IBE/AEAD decryption are deterministic given `(shares, ct)`. Verified by
  `TestCombineDeterministic` (identical plaintext across orderings/repeats) and
  `TestThresholdExactBoundary` (t-1 genuinely cannot recover at the raw primitive;
  t can). The DKG's `crypto/rand` is off-chain (a validator protocol), per D-061.
- **Library/version selection + footprint (the D-061 evaluation, recorded):**
  - `github.com/drand/kyber v1.3.1` (the drand fork — it, NOT `go.dedis.ch/kyber/v3`,
    carries `encrypt/ibe`) and `github.com/drand/kyber-bls12381 v0.3.1`. Both go
    directive `1.18`. **License:** MPL-2.0 (file-level copyleft — fine for linking
    a Go binary, per D-061); verify on bump. **Maintenance/audit:** drand-
    production, externally-audited-adjacent threshold stack.
  - **`drand/tlock` is NOT imported.** Its IBE primitive lives in
    `drand/kyber/encrypt/ibe` (importable in isolation); tlock's ROOT package drags
    in `filippo.io/age` + `github.com/drand/drand/v2` (the live drand node) and is
    avoided. The DEM is **stdlib AES-256-GCM** (no `golang.org/x/crypto` dep).
  - **Footprint measured (Q1 condition):** pinning the pre-`v0.3.4` versions keeps
    the **go directive at 1.23 — NO toolchain bump** and **`golang.org/x/*`
    unchanged**; module count 761→~772 (kyber, kyber-bls12381, and small pure-Go
    transitives: circl, clockwork, edwards25519). `kyber-bls12381@latest` (v0.3.4)
    would have forced go 1.24 + x/* churn — hence the pin. Footprint is ACCEPTABLE;
    no hand-rolled BF-IBE KEM was needed (which would have required explicit
    sign-off).
- **Task-1.8 audit obligations (authored surface — scope made explicit, not
  left to be rediscovered):**
  - **(a) Composed KEM/DEM IND-CCA under attacker-CHOSEN identity.** Unlike tlock
    (identity = a fixed external beacon round), Conclave's identity is
    `id = H(DST‖C_dem)` — a hash of attacker-chosen ciphertext bytes. The
    mix-and-match closure argument (swap `C_dem` → the IBE FO `rP` check rejects;
    swap `C_kem` → the AES-256-GCM tag rejects; only a fresh self-consistent
    triple is constructible, i.e. "submit a new intent") is **INFORMAL**. A
    formal IND-CCA proof of the composed BF-IBE-KEM + AES-256-GCM-DEM under
    adversarial identity is an audit deliverable.
  - **(b) DST-uniqueness check.** Confirm `"conclave/veil/ibe-identity/v1"` is
    unique across the codebase's SHA-256 preimage tags (no collision with another
    hash/signing domain), and that the library G1 hash-to-curve DST is shared by
    the IBE and tBLS paths (holds by construction via one `newSuite()`, but
    verify under audit). **Pre-mainnet checklist item** (a simple grep + a suite-
    identity assertion).
  - **(c) The four authored pieces** (KEM/DEM wrapper, `deriveIdentity`, the
    `reveal.ThresholdScheme` adapter, the share-gating) are the ENTIRE authored
    surface. Everything else — pairing groups + hash-to-curve, `share` Lagrange,
    `sign/tbls`, `share/dkg`, and `encrypt/ibe` — is library code adopted from the
    published drand tlock construction, not authored here.
- **Acceptance (offline, all green):** `make lint test` EXIT=0 incl.
  `crypto/threshold`; round-trip `Encrypt→PartialReveal→Combine` recovers t-of-n
  (n∈{4,5,7,10}, t=⌊2n/3⌋+1); corrupt share deterministically rejected (RED
  without the gate, proven); t-1 cannot / t can (at the raw primitive); `Combine`
  deterministic; DKG shares node-local. SPIKES were throwaway and not committed;
  StubScheme remains the live scheme; no consensus wiring.

## D-067 — Task 1.4: offline adversarial harness for the threshold construction; kyber selection-rule probe; drain-before-rotate modeled as full rotation; three seams carried to 1.5

- **Date:** 2026-06-20 (Phase-1 Task 1.4)
- **SPEC:** §8 (the Veil for real), §6.3 ThresholdScheme, §11.6 determinism;
  PHASE1_DESIGN §2 (drain-before-rotate, CANCEL-as-drain-terminus, grace-window
  fallback) / §8 ("adversarial harness before consensus wiring"); builds on
  **D-066** (the 1.3 construction) and the barrier characterization it fixed (gate
  = LIVENESS/ROBUSTNESS; IBE FO check = INTEGRITY). OFFLINE — no consensus wiring;
  `StubScheme` stays the live chain scheme until Task 1.6.
- **What 1.4 is:** where the 1.3 unit tests ASSERTED the threshold/gate/determinism
  properties on happy-path inputs, 1.4 PROVES them under an adversary model and
  across the key-lifecycle events (churn, resharing) that 1.5/1.6 will drive. One
  test-only file, `crypto/threshold/adversarial_test.go` (8 tests: 7 property +
  1 selection-rule probe), reusing the 1.3 helpers. No source change; `threshold.go`
  byte-identical to HEAD; the two new imports (`kyber/share`, `kyber/util/random`)
  are confined to the `_test.go` file (`kyber/share` already a production dep,
  `kyber/util/random` already imported by `dkg.go`) — `go.mod` unchanged, nothing
  enters the consensus dependency graph, determinism vector provably untouched
  (not re-burned; the D-063 per-seed hashes are invariant by construction).
- **Adversary model (genuinely adversarial, not happy paths relabeled):**
  - **SECRECY below threshold.** A coalition of t-1 MALICIOUS validators pooling
    their REAL node-local secret shares (distinct per participant, from
    `RunDKGInProcess`) plus all public material and a target ciphertext they did
    not author cannot recover: `Combine` → `ErrInsufficientShares` (the "not yet"
    refuse-to-interpolate semantic, D-066/Q3 — NOT a wrong-σ), and at the raw
    primitive `share.RecoverCommit` errors (t-1 underdetermine the degree-(t-1)
    polynomial; no σ is computed — Shamir perfect secrecy). Fabricating a t-th
    forged share cannot fake quorum; a genuine t-th honest holder recovers ("t can").
  - **ROBUSTNESS = the property the gate actually guarantees (LIVENESS).** Forged
    shares are real G1 points that DESERIALIZE and reach the pairing check, then
    FAIL it (index-forge: a genuine value re-labelled to a foreign index;
    cross-ciphertext replay: a genuine share on a different ct). The load-bearing
    claim — *when ≥ t honest shares exist, no forged shares can force
    `ErrInsufficientShares`* — is proven with f∈{1, n-t} forged at LOW indices
    (f=1, a single bad validator, is the 1.6-liveness case): gated `Combine`
    recovers the true payload while the SAME set through the ungated path fails to
    reproduce it (two separate assertions, gate the sole difference). Proven
    RED-without-gate by temporary bypass (failure surfaced as the FO `rP check
    failed`, itself proof the forged share cleared deserialization — a
    parser-rejected share never reaches the FO check). The retry semantic is
    separately pinned: t-1 honest + forged (replay or flood) → `ErrInsufficientShares`
    (not an FO/decrypt error), so a forged share cannot fake progress to t.
  - **WITHHELD shares.** A withholding adversary chooses WHO is silent: recovery
    succeeds from arbitrary honest t-subsets including scattered-middle ones
    (nodes 2,4 silent), not only the lowest-indexed t.
- **Selection-rule probe (the corrupt-share premise made explicit, not assumed).**
  The corrupt-share strategy DEPENDS on kyber's `share.RecoverCommit` selecting the
  lowest-t shares BY INDEX (`poly.go` `xyCommit` sorts `byIndexPub`, breaks at
  `len==t`) — undocumented-in-contract behavior that would change, if ever, via a
  silent dependency bump, not our diff. Without a dedicated assertion, a future
  kyber that picked the honest subset would make the robustness test pass VACUOUSLY
  (forgery excluded by SELECTION, not by the gate) — "green for the wrong reason."
  `TestKyberRecoverCommitSelectsLowestIndex` is a payload-independent, gate-free,
  IBE-free probe: secret A at low indices, secret B at high, assert `RecoverCommit`
  over the union returns `commitA`. Verified load-bearing (flipping the expectation
  to `commitB` goes RED) so a changed selection rule fails BY NAME at the real
  premise rather than as a disguised gate/forgery failure.
- **CHURN / RESHARING (the 1.5/1.6 prerequisite).**
  - **drain-before-rotate, modeled as a FULL rotation (conservative choice).** Two
    independent in-process DKG runs = epochs e and e+1 with fully-independent key
    material (distinct epoch masters, asserted). An in-flight ciphertext sealed to
    epoch e's key REMAINS decryptable under epoch e's RETAINED shares after e+1
    exists, and is NOT decryptable under epoch e+1's (gate-passing) shares (the FO
    check errors on the wrong threshold signature) — cross-epoch isolation, why
    clients tag each ct with its epoch id and the Reveal selects the matching share
    set. This tests IN-FLIGHT decryptability across a rotation, not DKG
    repeatability. Full independence is the STRONGEST drain condition (no share
    overlap to lean on); partial-overlap resharing is a forward seam (below).
  - **Leaver mid-epoch.** The threshold tolerates exactly n-t missing shares for
    in-flight ciphertexts (n-t leavers recover; n-t+1 → `ErrInsufficientShares`).
  - **CANCEL-as-drain-terminus — crypto precondition proven, session-state
    deferred.** A permanently-undecryptable reveal (only t-1 shares will ever
    exist) yields a TERMINAL, DETERMINISTIC, PURE `ErrInsufficientShares` across N
    retries, and a sufficient `Combine` still succeeds afterward (no state mutation
    — Combine is a pure function of its inputs, which 1.6's retry/CANCEL counter
    relies on). This is the signal the 1.5/1.6 session state machine consumes to
    CANCEL and release rotation; the state-machine interaction itself is NOT in
    `crypto/threshold` and is carried forward.
- **THREE seams carried forward to Task 1.5/1.6 (the crypto-to-consensus boundary,
  recorded so 1.5 inherits them explicitly, not by rediscovery):**
  - **(a) CANCEL-releases-rotation session-state interaction.** 1.4 proves the
    crypto precondition (terminal deterministic signal); the assertion that a
    CANCELLED session does not block epoch rotation lives in `x/session` / `x/veil`
    and is Task 1.5/1.6.
  - **(b) Grace-window fallback mechanism.** PHASE1_DESIGN §2's named, not-default
    fallback (bounded retention of epoch e's material so e/e+1 overlap). Its crypto
    basis is proven (epoch-e material decrypts e's in-flight ct independently of
    e+1, the drain test); the WINDOW mechanism (bounded retention, overlap timing)
    is epoch-lifecycle state → Task 1.5.
  - **(c) Partial-overlap resharing.** 1.4 modeled rotation as FULL independence;
    real CometBFT validator-set updates typically produce PARTIAL overlap (some
    validators persist across the boundary). 1.5/1.6 must confirm the retained-share
    selection (which epoch's shares decrypt which in-flight ct) handles partial
    overlap — the full-rotation test is the conservative bound, not the complete
    membership-mechanics check.
- **Acceptance (offline, all green):** `make lint test` EXIT=0 incl.
  `crypto/threshold` (8 new + 5 existing = 13 tests PASS); the two load-bearing
  robustness/semantic tests proven RED-without-gate; the selection-rule probe
  proven RED-on-wrong-expectation; `gofmt` clean; determinism vector unaffected
  (test-only change, no source/sim touched). `StubScheme` remains the live scheme;
  no consensus wiring. Next: Task 1.5 (DKG epoch lifecycle) — transport design
  sub-review BEFORE code (STOP #1), inheriting seams (a)/(b)/(c) above.

## D-068 — Task 1.5: DKG epoch lifecycle — new `x/epoch` module; on-chain-tx DKG transport (Candidate A, STOP #1 approved); height-based boundary + staking-snapshot committee; drain-before-rotate; failed-DKG degradation

- **Date:** 2026-06-20 (Phase-1 Task 1.5)
- **SPEC:** §8 (the Veil for real), §6.3 ThresholdScheme, §11.6 determinism;
  PHASE1_DESIGN §2(b) (epoch boundary / EpochPubKey publication / drain-before-
  rotate / CANCEL-as-drain-terminus / grace-window fallback) and §5 (Task 1.5
  carries a transport design sub-review as its own stop); builds on **D-066**
  (the tlock construction + `DKGParticipant`) and **D-067** (the three seams
  (a)/(b)/(c) carried forward). The live x/veil Reveal stays on `StubScheme`
  until Task 1.6; 1.5 establishes the epoch KEY lifecycle + its queryable chain-
  state representation, NOT the per-block share transport (that is 1.6).
- **TWO MANDATORY STOPS (like 0.8).** STOP #1 reviewed the overall plan AND the
  DKG-message-TRANSPORT design in isolation BEFORE any transport code (the single
  most unspecified, highest-risk Phase-1 decision, the §2(b) FLAG); STOP #2 was
  the pre-commit review. Both passed.
- **TRANSPORT = on-chain transactions (Candidate A), APPROVED at STOP #1.**
  Committee validators submit DKG round bundles as txs; the chain is a
  DETERMINISTIC RELAY keyed by `(epoch id, dkg index)` and NEVER interprets a
  bundle — only validators (off-chain, a protocol, D-061) serialize/deserialize
  and run the kyber DKG, so consensus code stays crypto-free. Rejected
  alternatives: **vote-extension-injected** (pulls 1.6's VE surface forward into
  1.5, and VE's per-height one-shot model fits a windowed multi-round DKG
  poorly); **side channel** (needs a new p2p reactor — 1.7 infra — and does NOT
  remove the on-chain agreement step). The KEY INSIGHT recorded at STOP #1:
  Candidate A's deal-window/response-window/declare-FAILED IS the §2(b) grace-
  window/seam-(b) fallback in its natural form — transport and failure-handling
  are ONE mechanism, not two.
- **Module placement = a NEW `x/epoch` module** (codespace 16xx). Clean long-term
  seam: it owns boundary detection, the DKG msg transport, EpochPubKey
  publication to queryable state, and the staking→veil bridge; 1.6 sources epoch
  material from `x/epoch`, not veil internals. It holds NO module account (no coin
  custody) and writes NO secret material — per-validator secret shares are node-
  local (D-062).
- **The DKG transport, end to end (three windows, three msg types).** A FORMING
  epoch runs `deal → response → confirm` windows in sequence within the epoch:
  - `MsgSubmitDeal` / `MsgSubmitResponse` carry the kyber `DealBundle` /
    `ResponseBundle` as OPAQUE bytes (authored serde lives in
    `crypto/threshold/transport.go`, off-chain validator-side — kyber ships no
    bundle serializer; drand serializes in its own repo). Round-trip is byte-
    stable so a deserialized deal bundle's `Hash()` still verifies the dealer
    signature in `ProcessDeals`.
  - `MsgConfirmEpochKey` carries a node's LOCALLY finalized `(epoch_pub_key,
    commits)`. The chain CANNOT compute the public key itself (that needs node-
    local DKG state, and re-deriving it outside kyber would be authored crypto in
    consensus — rejected), so it AGREES by counting: finalize publishes the value
    that **≥ t committee members confirm byte-identically**, else FAILED. This is
    deterministic — counting over committed bytes, no crypto in BeginBlock/
    EndBlock.
  - **Division of labour + security basis (STOP #2 Q2, named explicitly).** Nodes
    run the kyber `Finalize` OFF-CHAIN; consensus only COUNTS byte-identical
    confirmation strings (a trivially deterministic op) — keeping kyber out of the
    consensus path entirely, the cleaner realization of "only public bundles
    transit state" than recompute-in-consensus would have been. The NEW security
    basis this introduces, named because it differs from recompute-by-
    construction: **the published key's correctness rests on the honest-threshold
    assumption** (≥ t honest nodes will not confirm a wrong key), NOT on
    consensus recomputation. This is the standard threshold assumption and is
    acceptable, but it is the basis — a t-of-n confirmation quorum, not a
    consensus-recomputed value. (Cross-node agreement on the SAME key is the
    DKG's own guarantee; the chain trusts the ≥ t identical confirmations.)
  - **Justification round DEFERRED (STOP #1 + #2 approved — do NOT pull
    forward), CONSEQUENCE + forward seam framed precisely:** 1.5 does NOT
    implement kyber's faulty-dealer justification round. The threat to close is
    **"a dealer submits a faulty deal repeatedly to force the epoch to FAILED"**;
    the justification round EXCLUDES that dealer (and proceeds with the remaining
    qualified dealers) rather than failing the whole epoch. Until then, such a
    dealer can DENY ROTATION (force the prior key to stay active each epoch) — a
    bounded, survivable liveness-grief (the chain never halts, encryption
    continues under the prior key; proven by `TestFailedDKGDegradesNoRotation`).
    Pulling the full justification round forward would stack two hard things on
    the phase's highest-risk task, so it is Task 1.6/1.7 work. Linked to seam (b).
- **Epoch boundary = HEIGHT-BASED + staking-snapshot committee (resolves the
  §2(b) underspecification).** The boundary fires at `height %
  epoch_length_blocks == 0` (mirroring the proven `x/session SessionBoundary`),
  and the committee is the bonded set snapshotted from staking, sorted by valoper
  ascending (dkg_index = sorted position). Reacting to raw ABCI `ValidatorUpdate`
  DIFFS would be nondeterministic to reconstruct; a height boundary with the
  staking-finalized bonded set is deterministic AND is exactly the set CometBFT
  will use — the honest reading of §2(b)'s "valset-update trigger." LOAD-BEARING
  ORDERING: `x/epoch`'s EndBlocker is registered AFTER staking's in
  `app_config.go` so the snapshot reads the set staking has committed for the
  block (asserted in code comments + `TestBoundaryDetectionDeterministic`).
  Mid-epoch joins wait (the committee is frozen for the epoch).
- **EpochPubKey publication.** The epoch master public key (G2) + the public
  `commits` (the verification polynomial 1.6/clients need) are published to
  queryable state on the ACTIVE epoch record (`Query/CurrentEpoch`, epoch-tagged)
  — the §2 perf FLAG (EpochPubKey distribution). Secret shares NEVER touch chain
  state.
- **Status machine + drain-before-rotate (seams a/c against real chain state).**
  `FORMING → ACTIVE → DRAINING → RETIRED` (success) | `FORMING → FAILED`
  (degraded). On DKG success the successor goes ACTIVE IMMEDIATELY (the new
  encryption target) and the prior ACTIVE epoch goes DRAINING — RETAINED, still
  reveal-capable — NOT dropped. Clients tag each ciphertext with its `epoch_id`
  (new `IntentEnvelope.epoch_id` field) so the Reveal selects the matching share
  set across the overlap. A DRAINING epoch RETIRES only once it has no in-flight
  (sealed-but-unrevealed) ciphertext tagged to it (`x/veil
  HasInFlightForEpoch`) — the "rotation release" gated on drain. **Seam (a)
  CANCEL-releases-rotation:** a stuck session that CANCELs (or OPENs-and-clears)
  leaves `SEALED_IN_SESSION`, so it stops counting as in-flight → the epoch can
  retire; it cannot stall retirement forever. **Seam (c) partial-overlap:** the
  retained-share selection keys by EPOCH id, not validator identity, so a
  committee that partially overlaps the prior one (1.4 only modeled FULL
  independence) still isolates — proven by `TestPartialOverlapResharing` (two
  chain-driven DKGs with overlapping committees yield independent keys; an
  epoch-e ct decrypts under epoch-e material and NOT under epoch-(e+1) material).
- **Epoch-tag validation = BOTH edges (D-068, STOP #1 confirmation).**
  `MsgSubmitIntent` rejects an `epoch_id` that is not CURRENTLY DECRYPTABLE
  (`IsDecryptableEpoch` ⇒ ACTIVE or DRAINING): a RETIRED epoch (PAST edge) AND a
  not-yet-published FORMING/FAILED/unknown epoch (FUTURE edge the client could
  not have fetched) both reject through the one gate. `epoch_id == 0` is the
  stub/unset path (always allowed); a nil `epochKeeper` (veil unit tests) skips
  the check so stub-mode behavior stays uniform with Phase 0. Chain-stamping at
  seal was REJECTED — the tag must reflect the key the client actually encrypted
  to, which a seal-time stamp could disagree with.
- **Failed-DKG degradation (chain never halts).** Every degraded path keeps the
  chain live and the prior key the encryption target: too few bonded validators
  (< `min_committee_size`) opens NO epoch; a sub-threshold DKG is FAILED with NO
  rotation; a stuck drain just keeps the epoch DRAINING. Proven by
  `TestFailedDKGDegradesNoRotation` and the boundary test's min-committee branch.
- **Determinism (§11.6).** All CHAIN-STATE transitions are deterministic
  functions of committed inputs — boundary detection, the sorted committee
  snapshot, opaque-bundle collection, confirmation tally (group by bytes over a
  SORTED key list — no map iteration into output), publication, rotation,
  retirement. The DKG COMPUTATION (dealing) uses `crypto/rand` locally per
  validator (off-chain, D-061), but its chain-visible output (published key,
  epoch id, status) is a deterministic function of the agreed committed bundles.
  `epoch_length_blocks` defaults to 17280 (~1 day) — far larger than the 500-
  block determinism sim — so `x/epoch` is INERT in the sim (genesis params only,
  no per-block writes, no boundary). The determinism vector stays REPRODUCIBLE
  (the D-063 gate is cross-process reproducibility, not a frozen golden hash; the
  new module legitimately changes the app hash but identically across runs — 5
  seeds byte-identical across two independent processes, re-verified at 1.5).
- **Determinism coverage of the new chain-state surface (STOP #2 verification —
  named, not assumed).** Because the 500-block app sim does NOT reach an epoch
  boundary, the new transitions (boundary detection, committee snapshot,
  transport collection, confirmation tally, publication, rotation, drain) are
  determinism-covered by: (1) **`TestEpochChainStateDeterministic`** — REPLAYS one
  fixed DKG transcript (identical committee + identical opaque bundle/confirmation
  bytes) into two INDEPENDENT keepers through identical block heights across two
  epoch boundaries + a rotation, and asserts the full epoch chain state is
  byte-identical (isolating deterministic chain state from the off-chain DKG
  randomness); (2) **`TestDKGLifecyclePublishesConsistentKey`** — cross-NODE
  agreement (every validator's locally finalized key is byte-identical and equals
  the chain-published key); (3) the boundary/rotation/tally unit tests, which are
  deterministic by construction (sorted committee, sorted-key tally — no map
  iteration into output). **NAMED FORWARD GAP:** no test drives a real epoch
  boundary inside the full multi-validator APP sim (epoch_length is realistically
  ~1 day); **Task 1.7's multi-node adversarial localnet must exercise a real
  boundary + rotation end-to-end** — recorded here so it is not silently assumed
  covered.
- **Epoch-0 genesis bootstrap = a RECORDED FORWARD SEAM to Task 1.6.** Genesis
  ships no ACTIVE epoch and no key (`current_epoch_id = 0`); since the live
  encryption path is `StubScheme` until 1.6, epoch-0 bootstrap (a genesis-
  provisioned key vs. first-successful DKG) is correctly deferred — recorded,
  not lost.
- **Acceptance (all green):** `make lint test` EXIT=0 (whole app builds + boots;
  module-manager ordering with `x/epoch` after staking in EndBlockers / in the
  InitGenesis order); `crypto/threshold` transport serde + DKG-via-transport
  consistency (`TestDealBundleSerdeRoundTrip`, `TestDKGViaTransportConsistentKey`);
  `x/epoch/keeper` lifecycle (`TestDKGLifecyclePublishesConsistentKey` — the
  multi-validator DKG completes through the on-chain transport and the published
  EpochPubKey is byte-consistent across all nodes AND decrypts a t-of-n reveal;
  `TestDrainBeforeRotateAndCancelReleases` — seam (a); `TestPartialOverlapResharing`
  — seam (c); `TestFailedDKGDegradesNoRotation`; `TestBoundaryDetectionDeterministic`;
  `TestIsDecryptableEpochBothEdges`); `x/veil` `TestSubmitIntentEpochTagBothEdges`;
  determinism vector reproducible over the new chain-state surface. NO
  Reveal/ExtendVote per-block share wiring (1.6); `StubScheme` remains the live
  scheme. Seam (b) grace-window: its crypto basis is proven (D-067) and its
  natural chain form (FAILED → no rotation → prior key stays) ships here; an
  explicit bounded-retention WINDOW knob remains available if 1.5/1.6 harnesses
  show liveness stalls. Next: Task 1.6 (vote-extension share transport + Reveal
  wiring) — sources epoch material from `x/epoch`, closes the justification-round
  liveness-grief, and handles epoch-0 bootstrap.
  - **LEDGER CORRECTION (recorded at Task 1.6, D-069):** this entry's "closes the
    justification-round liveness-grief" was OPTIMISTIC. Task-1.6 analysis showed
    closure is inherently MULTI-NODE (it needs the off-chain DKG daemon to detect a
    faulty deal, justify, and re-submit, exercised against a real faulty node) and
    CANNOT be done in an in-process task. It is RE-DEFERRED to Task 1.7; the grief
    stays bounded/survivable (`TestFailedDKGDegradesNoRotation`), which is the
    deferral bar. 1.6 closes epoch-0 bootstrap only.

## D-069 — Task 1.6: vote-extension share transport + Reveal wiring (AUDIT F-4 closed); StubScheme→real-threshold cutover keyed per-intent on epoch_id; ABCI++ off-by-one (reveal is B+2); retry→CANCEL; epoch-0 plaintext-bootstrap window; justification-grief re-deferred to 1.7

- **Date:** 2026-06-21 (Phase-1 Task 1.6)
- **SPEC:** §8 (the Veil for real), §6.3 (the Reveal / ThresholdScheme), §11.6
  (determinism); PHASE1_DESIGN §2 (order-then-reveal under real crypto, the VE
  mechanism, retry/CANCEL, the perf FLAGS); AUDIT **F-4** (the entry criterion this
  task CLOSES); builds on **D-066** (the tlock construction + the gate/FO barriers),
  **D-067** (the adversarial proof + CANCEL-as-drain-terminus), **D-068** (the
  `x/epoch` key lifecycle + the three D-068 seams). This is the CONVERGENCE task:
  1.3+1.4+1.5 come together so the LIVE Reveal uses real threshold decryption and
  order-then-reveal rests on vote-extension share availability, not state-gating.
- **TWO MANDATORY STOPS (like 0.8/1.5), both passed.** STOP #1 = full plan
  (5 decisions confirmed). STOP #2 = the consensus-critical share transport reviewed
  IN ISOLATION before the StubScheme cutover (the 0.8-Half-A pattern), with the
  injected-`Txs[0]` unforgeability elevated to a gating artifact.
- **THE MECHANISM (F-4 closed).** ABCI++ (CometBFT v0.38) delivers height-h vote
  extensions to exactly one place: `PrepareProposal(h+1).LocalLastCommit`. So:
  `ExtendVote(h)` enumerates the committed `SEALED_IN_SESSION` set tagged to a DKG
  epoch (`EnumerateInFlightForReveal`, the deterministic §6.3 0x02 index — the
  ordering↔share CONTRACT), `PartialReveal`s each with the node-local secret share,
  and packs them into the VE. The proposer of `h+1` INJECTS h's `ExtendedCommitInfo`
  at `Txs[0]` (`InjectedRevealVotes`); `ProcessProposal(h+1)` re-validates it with
  `baseapp.ValidateVoteExtensions` and STRIPS it; `PreBlocker(h+1)` collects the
  shares into a per-block `RevealShareCache`; `BeginBlock(h+1)` `RevealSealed`
  combines ≥t → OPEN. F-4 holds BY CONSTRUCTION: a valid share for a ciphertext
  cannot exist until the seal that fixed its ordering is committed (proven —
  `TestRevealRealSchemeEndToEnd` shows the intent does NOT open at B+1 with shares
  withheld even though ordering is fixed; `TestEnumerateInFlightForReveal` excludes
  SUBMITTED/epoch_id==0).
- **ABCI++ OFF-BY-ONE (the load-bearing find, STOP #2).** `ExtendVote(h)` runs
  BEFORE `FinalizeBlock(h)` and reads COMMITTED state through `h-1` (baseapp builds
  its ctx from `cms.CacheMultiStore()`). So PHASE1_DESIGN §2's "EndBlock(B) seal →
  ExtendVote(B)" diagram was off by one: a seal committed at block B is first seen by
  `ExtendVote(B+1)`, whose shares reach `B+2` — **reveal is 2 blocks post-seal, not
  1**. This STRENGTHENS F-4 (a share needs the seal COMMITTED, not merely decided).
  Consequence acted on: the retry counter FLOORS at B+2 (`height >= session_id+2`),
  so the structurally-impossible B+1 attempt never burns a retry.
- **INJECTED-`Txs[0]` UNFORGEABILITY (STOP #2 gating artifact, proven on 4 legs,
  tests not assertions).** The chain distinguishes "Txs[0] is injected commit-info"
  from "Txs[0] is a normal tx" by POSITION + HEIGHT, never by content-sniffing:
  (1) at injection-active heights (`h > VoteExtensionsEnableHeight`) ProcessProposal
  REQUIRES Txs[0] to decode as `InjectedRevealVotes` and pass ValidateVoteExtensions,
  else REJECT — a normal tx at index 0 cannot masquerade; (2) ValidateVoteExtensions
  authenticates every extension against the validator's CONSENSUS key + ≥2/3 power —
  a proposer cannot forge/alter another's shares; (3) Txs[0] is stripped before the
  Block-SDK handler and consumed in PreBlocker before baseapp's tx loop, which only
  `deliverTx`s on decode success (the `InjectedRevealVotes` blob → harmless
  `ErrTxDecode`, never executed; even a pathological dual-decode is neutralized by
  the ante's signature check); (4) share AUTHENTICITY rests independently on the
  BeginBlock pairing gate (D-066), a second barrier. The VE-signature-authenticates-
  SUBMITTER / pairing-gate-authenticates-CORRECTNESS split is the defense-in-depth.
  (`TestRevealVoteExtensionUnforgeable`.)
- **CUTOVER keyed per-intent on `epoch_id` (the F-4 flip).** `RevealSealed` branches
  per intent: `epoch_id==0` → `StubScheme` identity (the bootstrap window); `epoch_id
  !=0` → the real BLS threshold scheme `threshold.NewSchemeFromCommitBytes(commits,n)`
  built from the epoch's PUBLISHED commits (`EpochKeeper.GetEpochVerification`),
  decrypting from the vote-extension shares the PreBlocker collected this block
  (`RevealShareSource`). `<t` valid shares → `ErrInsufficientShares` → leave SEALED
  (retry); `≥t` but IBE/AEAD open fails → INVALID (the FO/CCA integrity barrier,
  D-066); success → OPEN. The recovered plaintext is PERSISTED into the record's
  `Ciphertext` at OPEN (post-Reveal the payload is public — the auction matches on it
  — and the VE shares are gone, so x/session reads the revealed plaintext via
  `GetOpenIntent`, never re-decrypts; for stub `epoch_id==0` plaintext==ciphertext, a
  no-op). The live `k.scheme` field STAYS `StubScheme` — the real scheme is built
  per-epoch from public commits, never held as the singleton.
- **RETRY → CANCEL (PHASE1_DESIGN §2(c), seam (a)).** Per-SESSION attempt counter
  (new `0x05` key), incremented only from the B+2 floor; at `>= max_reveal_retries`
  (default 3) the session's remaining SEALED intents CANCEL with full bond refund
  (`settleRefund`), which releases epoch rotation (they stop counting as in-flight —
  drain-before-rotate seam (a)). The chain NEVER halts. All inputs are committed
  state (collected shares, counter) ⇒ deterministic. (`TestRevealRetryExhaustionCancels`.)
- **VE SIZE BOUND (perf FLAG).** New veil param `max_intents_per_session` caps how
  many SUBMITTED intents seal into one session AT THE SEAL (`SealSubmitted`), in the
  deterministic 0x02 index order (now consensus state — which seal vs. roll cannot
  depend on map/insertion order); the overflow stays SUBMITTED for the next boundary
  (natural backpressure). 0 = unbounded (the Phase-0/stub default — epoch_id==0
  intents produce no shares). `VerifyVoteExtension` additionally rejects a VE with
  more shares than there are in-flight ciphertexts. New param `max_reveal_retries`
  (default 3).
- **EPOCH-0 BOOTSTRAP — RESOLVED (first-DKG-then-live + plaintext-stub window).**
  Genesis ships no ACTIVE epoch (`current_epoch_id=0`); until the first DKG reaches
  ACTIVE, clients submit `epoch_id==0` intents that reveal via `StubScheme` (identity).
  **HONEST CONFIDENTIALITY CLAIM (recorded loudly, not buried):** the Veil guarantee
  does NOT hold during the genesis → first-ACTIVE-epoch window; intents in that window
  reveal via StubScheme = **effectively plaintext**. This is the honest security claim
  for a fresh chain, not a silent gap — operators MUST NOT route sensitive flow before
  epoch 1 is ACTIVE. Chosen because a genesis-provisioned key would require shipping
  secret shares at genesis (defeats the DKG / D-062) or a trusted dealer.
- **JUSTIFICATION-GRIEF — RE-DEFERRED to Task 1.7 (D-068 seam, ledger corrected).**
  Closing the faulty-dealer-forces-FAILED grief needs the off-chain DKG daemon + a
  real faulty-node localnet (1.7 infra) and cannot be done in an in-process task; the
  grief is bounded/survivable today (chain never halts, prior key stays —
  `TestFailedDKGDegradesNoRotation`). Stacking it on the phase's highest-risk task was
  rejected. The D-068 ledger framing ("1.6 closes justification-grief") is corrected
  above.
- **NODE-LOCAL SHARE PROVIDER (the crypto→consensus seam).** `ShareProvider`
  (node-local, NEVER consensus state, D-062) supplies ExtendVote this node's DKG
  secret share for an epoch. 1.6 ships the interface + an in-memory `MapShareProvider`
  + the in-process harness that seeds it; **Task 1.7's DKG daemon** (watch FORMING
  epochs → run the kyber rounds → submit the deal/response/confirm txs → persist the
  share) populates it on a live network.
- **DETERMINISM (§11.6, the hard gate).** Cross-process determinism vector GREEN with
  the cutover live. The 500-block app sim is EPOCH-INERT (`epoch_length≈17280` ≫ 500,
  D-068), so `CurrentEpochID` stays 0, every intent is `epoch_id==0`, and the real-
  scheme/VE path is NEVER exercised in the vector — it traverses the StubScheme path
  exactly as before (hashes shifted from the pre-1.6 baseline only because the two new
  `Params` fields change genesis bytes; reproducible — the gate is cross-process
  reproducibility, not a frozen golden hash). The transport is inert on the sim path
  (sim keeps VEs disabled → injection/collect are pass-throughs; the cache is in-memory,
  never written to state). The REAL-path determinism is proven directly by
  `TestRevealRealSchemeDeterministic` (identical committed inputs → byte-identical
  reveal across two independent keepers). **NAMED FORWARD GAP (inherited from D-068):**
  no test drives a real epoch boundary + DKG + reveal inside the full multi-validator
  APP sim — **Task 1.7's multi-node adversarial localnet exercises a real boundary +
  encrypted reveal end-to-end with VEs enabled in a real genesis.**
- **VE ENABLING (A5).** Vote extensions ship enabled via `app.DefaultConsensusParams()`
  (height 1) and `scripts/localnet.sh` (dev genesis `vote_extensions_enable_height=1`);
  the determinism sim and keeper unit tests keep them OFF (independent genesis), so the
  vector is provably untouched. Production sets the genesis field; the full real-genesis
  VE + epoch-boundary flow is exercised in 1.7.
- **Proto/state added:** veil `Params.max_intents_per_session`/`max_reveal_retries`;
  `VoteExtensionShares`/`PartialRevealShare`/`InjectedRevealVotes` messages; key `0x05`
  reveal-attempt counter; errors `1314`/`1315`; events `reveal_retry`/
  `session_reveal_cancelled`. `threshold.NewSchemeFromCommitBytes` (the public-commits
  bridge). No change to the live `k.scheme` singleton (StubScheme).
- **Acceptance (all green):** `make lint test` EXIT=0; `x/veil/keeper` transport +
  cutover suite (`TestEnumerateInFlightForReveal`, `TestRevealRealSchemeEndToEnd`,
  `TestRevealRetryExhaustionCancels`, `TestRevealRealSchemeDeterministic`); `app`
  transport suite (`TestRevealTransportCollectAndDecrypt` incl. F-4 0/t-1-cannot,
  `TestRevealVoteExtensionUnforgeable`, `TestCollectIsDeterministicAndDropsGarbage`,
  `TestInjectedRoundTrip`); the 1.1 spend_commitment Reveal-slash and all prior suites
  still green under the cutover; determinism vector reproducible (5 seeds, two
  processes). Next: Task 1.7 (multi-node adversarial localnet) — real epoch boundary +
  encrypted reveal e2e, the DKG daemon, justification-grief closure, the Reveal-cost
  benchmark.

## D-070 — Task 1.7: 4-validator adversarial localnet — the off-chain DKG daemon; on-chain DKG-key registration round (A2); justification round CLOSED (B2); live real-network proofs (epoch ACTIVE, encrypted reveal+clear, F-4, faulty-dealer no-halt, app-hash agreement); Reveal-cost benchmark

- **Date:** 2026-06-21 (Phase-1 Task 1.7) — Phase-1 BUILD exit (1.8 = external audit).
- **SPEC:** §8 (the Veil for real), §6.3, §11.6; PHASE1_DESIGN Task 1.7 + the
  perf FLAGS (Reveal-cost benchmark, `max_intents_per_session`); closes the
  accumulated D-066–D-069 forward gaps: (i) the off-chain DKG daemon populating
  the node-local `ShareProvider`; (ii) real epoch-boundary + encrypted-reveal e2e
  on a 4-validator localnet with VEs in real genesis; (iii) justification-grief
  closure; (iv) the Reveal-cost benchmark; (v) the 1.5 determinism-boundary-in-sim
  gap. Builds on D-066 (tlock construction), D-067 (offline adversarial proof),
  D-068 (`x/epoch` lifecycle + transport), D-069 (VE share transport + cutover).
- **PLAN-STOP APPROVED (A2, B2, C1, D1).** The two load-bearing reversals (made
  under review pressure) were both adopted: Q1→**A2** (on-chain registration, the
  binding must be protocol-enforced now since 1.7 builds the real chain), Q2→**B2**
  (CLOSE justification — a single faulty dealer was a single-point VETO on the whole
  confidentiality guarantee for the first epoch, not a bounded grief). Q3→C1
  (scripted, no docker), Q4→D1 (in-process daemon).
- **THE DKG DAEMON (`app/dkgdaemon`, in-process, D1) — the missing client.** 1.5/1.6
  stubbed the share with a harness; 1.7 makes it real. Launched from `conclaved
  start --dkg-daemon` via the server `StartCmdOptions.PostSetup` hook (in-process
  only), it watches FORMING epochs, runs the kyber rounds locally, submits the
  register/deal/response/justify/confirm txs to its own node (signed by the
  validator account key, sequence filled fresh per tx — one tx per validator per
  window, spaced over blocks), and PERSISTS the finalized node-local secret share
  to a file-backed store. **D1 trust boundary:** the daemon holds the secret share
  AND signs with the validator key, so it is node-coupled; the share is written
  straight to `<home>/data/dkg_shares/epoch_<id>.share` (0600) and NEVER leaves the
  node process / enters consensus state / is gossiped (D-061/D-062). `app/dkgshare`
  is the leaf store both `app` (ExtendVote reads it) and the daemon (writes it)
  share — a node with a home dir gets the `FileProvider`; unit tests keep the
  in-memory `MapShareProvider`. The faulty-dealer adversary is a `--dkg-faulty-deal`
  flag (corrupt-but-authentic deal + refuse to justify).
- **A2 — on-chain DKG-key REGISTRATION round (the long-term-key/roster problem).**
  kyber needs every validator's long-term BLS12-381 G2 public key + the agreed
  roster + a shared nonce BEFORE round 1, none derivable from the ed25519 consensus
  key — the G2 public must be PUBLISHED. A2 adds a `register` window (before deal)
  + `MsgRegisterDKGKey(epoch, dkg_pub_key)`, keyed by the SIGNER's validator
  identity (msg_server derives valoper from the signer account), authorized against
  the bonded candidate set — the SAME account↔valoper binding the deal/response/
  confirm rounds use, so a key cannot be registered in another validator's slot
  (BUILD CONDITION 2, proven `TestRegisterBindingRejectsNonCandidate` /
  `TestRegisterBindingViaMsgServer`). The roster nonce is `DeriveNonce(epoch,
  sorted-roster)` — deterministic, agreed by all, no extra round. Rejected A1
  (genesis-registered keys): an unauthenticated key channel resting on genesis
  trust that does not generalize, and could not support dynamic membership.
- **A2 — the COMMITTEE is the REGISTERED set (no single-point veto).** A registration
  round that let a non-registering validator block the epoch would REINTRODUCE the
  veto B2 exists to kill. So `MaybeOpenEpoch` snapshots the bonded set only as
  CANDIDATES (committee `nil`, threshold 0); `FinalizeCommittee` runs once at the
  register-window close and sets the committee to the sorted REGISTRANTS with dense
  `dkg_index` and `t = floor(2m/3)+1` for the registered size m; a candidate that
  did not register is simply excluded (proven `TestNonRegisteringCandidateDoesNotVeto`
  — min=3, 3-of-4 register → ACTIVE). If m < `min_committee_size` the epoch FAILEDs
  (BUILD CONDITION 3 / the floor — `TestTooFewRegistrationsFailsEpoch`). The
  reveal-side n is the registered m, so the Combine threshold matches.
- **B2 — justification round CLOSED (the faulty-dealer veto).** The pre-1.7
  `DKGParticipant.Finalize` errored on ANY complaint, so ONE faulty dealer in the
  FIRST committee made the DKG collapse for every node → epoch 1 never activates →
  the Veil never leaves the epoch-0 PLAINTEXT bootstrap window: a single Byzantine
  node vetoing the entire confidentiality guarantee (not bounded liveness-grief —
  the first epoch has no prior key). `crypto/threshold` is restructured into the
  kyber-faithful phases (`Round1Deal → Round2Response → ProcessResponses →
  Finalize(justifications)`); a `justify` window + `MsgSubmitJustification` carries
  justification bundles. **KEY INSIGHT that kept the chain-side small:** excluding a
  faulty DEALER does NOT change n or the reveal threshold — all n holders still hold
  valid shares of the QUALified-set master secret; kyber's `ProcessJustifications`
  ABORTS if qualified dealers < t (the security floor is enforced INSIDE kyber, not
  re-derived in consensus); and the chain's EXISTING ≥t-identical-confirmation tally
  publishes the qualified-set key UNCHANGED (the 3 honest confirm the same key, the
  faulty one's deviating/absent confirmation just doesn't count). So B2 on-chain =
  a transport window only, no finalize surgery. Proven offline
  (`TestJustificationExcludesFaultyDealerAndCompletes` f=1 completes,
  `TestJustificationFailsBelowFloor` f=2 aborts) AND on chain
  (`TestFaultyDealerExcludedEpochActivates`: epoch ACTIVE, the published key decrypts
  a t-of-n reveal) — BUILD CONDITION 1.
- **FIVE-WINDOW DKG (register → deal → response → justify → confirm).** Boundaries
  derive from `formed_height`/`deal_deadline`/`confirm_deadline` + params (the
  recompute-from-params pattern of 1.5) — the **`Epoch` proto is UNCHANGED, no new
  fields** (`deal_deadline` already spans [register+deal], `confirm_deadline` spans
  the full DKG; register/response/justify boundaries recompute). `Params` gains
  `register_window_blocks` + `justify_window_blocks`. New store prefixes 0x09
  (registration by valoper), 0x0a (justify), 0x0b (candidate), 0x0c
  (committee-finalized flag). New queries `FormingEpoch`, `Registrations`,
  `DKGTranscript(round)` are the daemon's read surface. New errors 1611/1612.
- **LIVE 4-VALIDATOR NETWORK (`scripts/localnet4.sh`, scripted/C1) — every headline
  proven on the real network, not in-process:**
  - **The DKG daemon completed a real DKG across 4 validators → epoch 1 ACTIVE** with
    a consistent published `EpochPubKey` (committee n=4, t=3); shares stayed
    node-local. Multiple epochs rotated over a run (epoch_length=30, windows=5).
  - **App-hash AGREEMENT across all 4 nodes** at the boundary/post-rotation height
    (byte-identical `app_hash`; a divergent node would have halted consensus) —
    **closes the D-068/D-069 "real boundary in the full sim" gap (gap v)** directly.
  - **The headline e2e:** a client fetched the ACTIVE epoch key, ENCRYPTED a 2-party
    swap to it (ciphertext 238B ≠ plaintext 34B — confidentiality is real, not
    identity), submitted it `epoch_id`-tagged, and the intents revealed via REAL
    vote-extension threshold shares (B+2) and CLEARED through the in-consensus
    fallback — all on the 4-node net (`conclave-demo veil-e2e`).
  - **F-4 on a real network:** the `veil-f4-adversary` (a modified validator given
    its OWN node-local secret share + the published key + a target ciphertext)
    proved it STILL cannot decrypt — 0-share and single-share Combine both refuse
    (`ErrInsufficientShares`); it needs t=3 shares that only exist in vote extensions
    AFTER the seal. conclave-demo is dev-only tooling, never linked into the shipping
    `conclaved`, so this models a malicious node without modifying the real binary.
    **F-4 framing (stated for the auditor, not discovered):** the real-network F-4
    proof is a FULL-LOCAL-VIEW adversary with < t shares, NOT a forked node binary.
    It is handed a REAL node's secret share + the sealed ciphertext + all public
    material and still cannot decrypt — because the barrier is the CRYPTOGRAPHIC
    THRESHOLD, not node-binary integrity. Granting the adversary a real share + full
    local view is if anything a STRONGER test than a binary fork (it gives the
    adversary strictly more than a modified node would have); the guarantee rests on
    the threshold and the test exercises exactly that.
  - **Faulty-dealer no-halt (B2 live):** with `FAULTY_NODE` set, the justification
    round excludes the faulty dealer and the chain does NOT halt (the epoch still
    reaches ACTIVE under the 3 honest dealers; degraded paths keep the prior key).
- **REVEAL-COST BENCHMARK (D-069 gap iv) — a recorded FINDING.** `BenchmarkReveal
  CombineUnderCap` / `TestRevealCostWithinBound`: per-intent reveal is pairing-
  dominated (the D-066 share gate + IBE open) at **≈12ms/intent (n=4)**, so BeginBlock
  reveal cost is LINEAR in the sealed-intent count and the per-session cap is
  load-bearing. The recommended `max_intents_per_session` ≈ **25** for a ~300ms
  reveal budget in a 1s block. **The default 0 (unbounded) is safe ONLY on the
  epoch-0 stub path (no shares, no Combine); a live deployment (epoch ≥ 1) MUST set
  a finite cap** — the localnet genesis sets 25. The gate asserts the per-intent
  invariant (< 50ms) so a gate/IBE regression is caught. **CAP RECOMMENDATION IS
  n=4-SPECIFIC — re-derive for the real committee before mainnet (1.8 obligation,
  not a 1.7 blocker):** per-intent Combine cost SCALES WITH COMMITTEE SIZE n (more
  partial-decryption shares to gate + interpolate), so a larger audit/testnet
  committee RAISES per-intent cost and LOWERS the safe cap. The ≈25 figure must NOT
  harden into a mainnet default; it is re-derived at the production n.
- **Determinism (§11.6).** The in-process determinism vector stays REPRODUCIBLE
  (the localnet harness does not touch the sim path; `epoch_length` in the sim is
  still ≈1 day so x/epoch is inert there). The new chain-state surface (register/
  justify windows, committee finalization, 5-window boundaries) is determinism-
  covered by the rewritten `TestEpochChainStateDeterministic` (replays a fixed
  five-window transcript into two independent keepers) and the per-condition keeper
  tests; the REAL boundary is now also exercised live (app-hash agreement above).
- **Acceptance (all green):** `make lint test` EXIT=0 (incl. the rewritten epoch
  keeper suite over the 5-window flow, the crypto justification + reveal-cost tests,
  all prior suites under the daemon/registration changes); determinism vector
  reproducible; the live `scripts/localnet4.sh` end-to-end (epoch ACTIVE, app-hash
  agreement, encrypted reveal+clear, F-4 adversary, faulty-dealer no-halt). **All
  six 1.7 acceptance criteria met.** `StubScheme` remains the live scheme on the
  epoch-0 path; the real scheme is built per-epoch from public commits (D-069).
- **CARRIED TO 1.8 (external audit, the Phase-1 mainnet gate — D-066 obligations
  stand):** the authored tlock construction, the DKG (now incl. the registration +
  justification rounds + the daemon), the VE share transport, order-then-reveal, the
  retry/cancel paths, and the verifiable-share exclusion are the audited unit. NEW
  for the auditor's attention: the A2 registration binding, the B2 justification/
  qualified-set finalize, the daemon's tx-driving + sequence handling, and the
  honest-threshold confirmation-quorum basis (D-068). Phase-1 BUILD is COMPLETE.

## D-071 — Task 1.8: external cryptographic audit SCOPE PACKAGE — `docs/AUDIT_SCOPE_1.8.md`; collects the scattered D-066–070 1.8-obligation flags into one auditor-facing artifact

- **Date:** 2026-06-22 (Phase-1 Task 1.8). **NOT a build task** — the deliverable is
  a document (`docs/AUDIT_SCOPE_1.8.md`), no source code written; `make lint test`
  green and HEAD source byte-identical to `5f5feb8` (only docs + ledger changed).
- **SPEC:** §8(d) (the external audit is the **mainnet** gating milestone, not
  testnet); PHASE1_DESIGN §2/§3/§5. Collects the 1.8 obligations recorded across
  **D-066, D-067, D-068, D-069, D-070** — which were deliberately scattered as the
  build accreted them — into ONE auditor-facing scope, so the engagement does not
  re-derive them from five decisions.
- **WHAT "1.8 COMPLETE" MEANS (recorded loudly, not buried):** the SCOPE PACKAGE is
  delivered. The external cryptographic audit ITSELF is performed by an outside
  party, has NOT been run, and is the actual mainnet gate. The ledger `[x]` marks
  scope-delivered, NOT audit-passed. Findings, when they come, triage back here.
- **The scope's spine (the obligation collection — the point of the artifact):**
  - **O1** = D-066(a): IND-CCA of the composed BF-IBE-KEM + AES-256-GCM-DEM under an
    attacker-influenced identity `id=H(DST‖C_dem)` (the tlock divergence: tlock's
    identity is an external fixed beacon round). Mix-and-match closure is INFORMAL;
    a formal reduction is the deliverable.
  - **O2** = D-066(b): DST-uniqueness sweep + the IBE/tBLS shared-suite hash-to-curve
    DST (pre-mainnet checklist).
  - **O3** = D-066(c), **UPDATED**: the authored-surface inventory. D-066's "exactly
    four pieces" is **no longer true as stated** — 1.7 authored consensus-integration
    surface (A2 registration binding, B2 justification transport + qualified-set
    tally, VE share transport, the epoch-tag gate, the share custody/daemon). The
    scope's §2.2 is the corrected complete inventory as of `5f5feb8`; the doc states
    the correction explicitly so the auditor is not pointed away from what 1.7 added.
  - **O4** = D-070: re-derive `max_intents_per_session` at the production committee n
    (the ≈25 / ≈12 ms-per-intent figure is n=4-specific; per-intent Combine scales
    with n; default 0 is safe only on the epoch-0 stub path).
  - **Arguments to verify/refute** (presented as "here is our argument", not
    rebuilt): A-bind (transitive mix-and-match, 1.3/Q1b), **A-barriers** (gate =
    robustness / IBE FO = integrity — carrying the 1.3 self-correction that an earlier
    comment overstated the gate as the "sole guard"), A-floor (t−1 info-theoretic),
    A-F4 (order-then-reveal by construction + the full-local-view <t-share adversary,
    D-069/D-070(d)), A-inject (injected-`Txs[0]` unforgeability, D-069), A-reg (A2),
    A-just (B2).
  - **Trust assumptions** (named because they are where consensus USES crypto, not
    crypto properties): **T1** = D-068 honest-threshold confirmation-counting basis
    (the most easily overlooked); T2 = D-067 kyber `RecoverCommit` lowest-index
    selection premise; T3 = D-070 DKG-daemon D1 trust boundary (holds the secret
    share AND signs with the validator key; 0600 file store, never gossiped); T4 =
    D-066 dependency pin + MPL-2.0.
  - **Limitations:** L1 = D-069 epoch-0 plaintext-bootstrap window (Veil does NOT hold
    genesis→first-ACTIVE-epoch); L2 = the n=4-cap operational face of O4; L3 = D-066
    FLAG III (the share IS its own proof — no NIZK — framed as the replay-binding
    question that cross-references O1).
- **Evidence map = a WORKING-system pointer (1.7 shipped the live network).** 25
  property→test rows + the live `scripts/localnet4.sh` proofs. A discrete
  verification pass over the FINISHED doc confirmed **44/44 cited Test/Benchmark
  functions resolve at their paths** and every code-location citation resolves —
  which CAUGHT two file-attribution errors fixed before commit: `RevealSealed` +
  the `NewSchemeFromCommitBytes` call live in `x/veil/keeper/lifecycle.go` (not
  `reveal_transport.go`, which holds `EnumerateInFlightForReveal` + the retry/B+2
  counter), and the `ShareProvider` interface + in-memory `MapShareProvider` live in
  `app/vote_extensions.go` (only `FileProvider` is in `app/dkgshare/`).
- **Authored vs adopted boundary = the orientation section** (the scope's most
  important framing): ADOPTED = drand/kyber pairing+hash-to-curve, `share` Lagrange,
  `sign/tbls`, `share/dkg`, `encrypt/ibe`, stdlib AES-256-GCM (in scope only for
  correct USAGE); AUTHORED = the KEM/DEM wrapper + `deriveIdentity` + the
  `ThresholdScheme` adapter + the share-gating + the DKG round-wrapping/serde + the
  1.7 consensus integration. No novel primitive authored.
- **Acceptance:** `docs/AUDIT_SCOPE_1.8.md` exists, complete against the D-066–070
  obligations (cross-checked), accurate against `5f5feb8` (references verified to
  resolve), honest about what is unproven; `make lint test` green (no code). Phase-1
  is BUILD-complete and now SCOPE-complete; the external audit is the remaining
  mainnet gate.

## D-072 — Phase-2 design ledger authored (docs/PHASE2_DESIGN.md is the approved phase authority)

- **Date:** 2026-06-24 (Phase-1 → Phase-2 boundary planning session)
- **SPEC:** §10 (Phase-2 tasks 2.1–2.5), §6.5, §9, §11.2, §14 D5
- **Decision:** `docs/PHASE2_DESIGN.md` is authored and approved as the per-task
  design ledger for Phase 2 (the analog of `docs/PHASE1_DESIGN.md`). It is the
  reviewed phase authority each Phase-2 build session bootstraps from; SPEC §10 +
  this log remain the source of truth for detail. A docs-only task — no source,
  proto, or logic touched; `make lint test` green at baseline `39cb207`.
- **What it pins:**
  - **Per-task sections 2.1–2.5** (what/deps/acceptance/plan-stop decisions),
    plus a **numbered task 2.6** (matcher-richness hardening pass) that OWNS the
    five latent matcher TODOs so none floats.
  - **2.1 (`x/escrow`)** detailed most: the three condition classes as §11.2
    transition tables (HASHLOCK / OPTIMISTIC / ZK_DELIVERY — three sub-graphs over
    the shared `EscrowState` enum, illegal transitions enumerated per class); the
    `conclude.go:152` record-ownership handoff; the `PenalizeLostDispute` reputation
    hook wiring; **D5 ratification** (hash-mismatch + seller bond, subjective
    arbitration out of scope — to become a DECISIONS entry when 2.1 runs); and a
    recommended **2.1a (HASHLOCK+OPTIMISTIC) / 2.1b (ZK_DELIVERY)** session split.
  - **2.1b gnark-spike branch** (FLAG A): gnark's Go floor vs the D-066 kyber pin —
    (a) works on current toolchain → proceed; (b) needs a Go kyber tolerates →
    proceed-with-bump-recorded; (c) needs a Go that BREAKS the kyber pin → **STOP
    and decide** (different gnark / kyber-tolerating toolchain / defer ZK_DELIVERY).
  - **2.2 owns the D-060 v0.53 re-evaluation** (FLAG B): spike-then-decide — 2.2
    begins by re-checking D-060 (a)/(b)/(c) against THEN-CURRENT block-sdk/ibc-go/
    wasmd; the doc does NOT pre-bake the v0.50-vs-v0.53 outcome. 2.5 shares the
    IBC-line sub-question (b).
  - **2.3 / 2.4 are SEPARATE-REPO** edge services (FLAG C): off-chain gRPC clients;
    the in-repo deliverable is the node-side gRPC surface contract, not module code.
  - **2.5** IBC real USDC + session-TWAP fee (swaps the `window.go:138` fee seam).
  - **Latent-TODO disposition:** all ten `CONCLAVE-TODO(phase-2)` markers
    dispositioned — #1 (conclude.go:152)→2.1, #2 (reputation.go hook)→2.1,
    #3 (tx.pb.go:322 CVM bindings)→2.2, #4 (window.go:138 fee TWAP)→2.5,
    #5 (sigverify.go:16 ADR-070)→2.2, #6–10 (fallback/cycles/limit-rate/
    multi-intent/multi-asset-skim)→2.6; #10 carries a 2.5 coherence cross-ref.
  - **Phase-2 exit:** the §1.3 demo extended (external x402 agent buys ZK-verified
    data; receipt IBC-verified on a second chain) — spans 2.1+2.2+2.3+2.5; 2.4 is
    the parallel inbound rail, deliberately off the exit path.
- **2.1b forward design point (recorded for the 2.1b plan-stop, not in the doc):**
  the ZK_DELIVERY replay-rejection mechanism must bind to the escrow **INSTANCE**,
  not just the schema — a proof over `schema_commitment` alone could replay across
  two escrows sharing a schema. The proof/verification public inputs must
  incorporate something escrow-instance-unique (the escrow id or a per-escrow
  nonce). The §2.1 table correctly names replay illegal; 2.1b owns making the
  mechanism bind to THIS escrow.
- **Rationale:** PHASE1_DESIGN.md played exactly this role for Phase 1 (reviewed
  before any build task, per-task acceptance + plan-stop decisions). Authoring the
  Phase-2 analog up front flags every §10 under-specification and toolchain risk
  (gnark/kyber, wasmd/v0.53) before build, and the completeness gate guarantees no
  latent phase-2 TODO ships unowned.
- **Acceptance:** `docs/PHASE2_DESIGN.md` exists, mirrors PHASE1_DESIGN's shape,
  dispositions all ten phase-2 TODOs, holds v0.53 as decide-when-reached and the
  edges as separate-repo; `make lint test` green (no code) at `39cb207`. 2.1a
  begins the Phase-2 build in a fresh session on this approved doc.

## D-073 — Task 2.1a: `x/escrow` stood up — HASHLOCK + OPTIMISTIC condition classes as §11-rule-2 state machines; payment-into-escrow funding + seller-reveal dispute model; conclude.go ownership handoff; D5 ratified

- **Date:** 2026-06-25 (task 2.1a)
- **SPEC:** §6.5 (condition classes, escrow msgs), §6.4 (DataGrant→escrow), §11
  rule 2 (transition-table test), rule 5 (never panic), rule 6 (determinism);
  §14 D5; PHASE2_DESIGN §2.1 / §3; supersedes the conclude.go:152 TODO + the
  D-033 app-level escrow-registration shim.
- **Decision (the module).** `x/escrow` is now a real module (keeper, AppModule,
  msg server, query, genesis, EndBlock sweep) on codespace `15xx`. ZK_DELIVERY is
  **deferred to 2.1b** (gnark-spike-gated, FLAG A): `MsgSubmitDeliveryProof` and
  any ZK escrow creation return `ErrZkNotEnabled`. Every state change routes
  through ONE authoritative gate `nextState(class, from, event)` (a pure function
  rejecting every illegal tuple with a typed error and performing no side effect);
  data guards (height, preimage) layer on top in the callers. This is what makes
  the §11-rule-2 "illegal transitions impossible" assertion provable — the
  exhaustive `TestTransitionTable_Exhaustive` drives the full class×state×event
  product and is load-bearing (deleting any guard flips an illegal cell to legal
  and fails the test).
- **D5 ratified (not re-opened).** OPTIMISTIC dispute resolution is **hash-mismatch
  challenge with a seller bond only**; subjective arbitration is out of scope
  (SPEC §14 D5 default).
- **Funding model — payment-into-escrow.** A conditioned CONCLUDE routes the
  buyer's coin payment INTO the escrow module account (not to the seller);
  `payer = buyer` (data recipient, `SettleLeg.ToSigil`) funds + is refunded;
  `payee = seller` (data giver, `FromSigil`) is paid on release. HASHLOCK
  ACTIVE→RELEASED (valid pre-timeout preimage) pays the seller; ACTIVE→REFUNDED
  (timeout sweep) refunds the buyer. OPTIMISTIC ACTIVE→RELEASED (window elapsed,
  undisputed sweep) pays the seller; ACTIVE→DISPUTED (in-window `MsgRaiseDispute`,
  buyer-only); DISPUTED→RELEASED (seller reveals the content preimage); DISPUTED→
  REFUNDED (resolution sweep, seller failed to prove) refunds the buyer, slashes
  the bond to the buyer, and fires `PenalizeLostDispute` on the seller.
- **OPTIMISTIC seller-reveal model + soundness.** "Slash on proven non-delivery
  via hash mismatch" is mechanized as: a dispute forces the seller to reveal the
  content preimage of a **buyer-agreed committed hash**; failure to produce a
  hash-matching preimage within the resolution window (= a second dispute-window)
  IS the proven mismatch. The committed hash is the leg's `DataGrant.ContentHash`,
  which is **buyer-agreed**, not seller-supplied: SPEC has no `IntentLeg`
  condition field, but rule 3 (`solution.go` `checkLimits`, net-inflow-must-be-a-
  want-key) only lets the buyer receive `data/<ContentHash>` if the buyer's own
  signed want-leg named that exact hash, and conservation requires the seller's
  offered hash to be byte-identical. So `H(preimage)==ContentHash` proves the
  seller delivered **what the buyer asked for**, closing the paid-for-non-delivery
  hole. **Narrowed scope (recorded):** OPTIMISTIC is therefore "delivery of
  content hashing to a buyer-agreed commitment whose preimage the seller can
  re-reveal on-chain on demand" — (1) an honest seller who can't/won't re-reveal
  the preimage in-window (bulk/streamed/non-preimage-reducible content, or a lost
  preimage) is slashed despite honest delivery; (2) the reveal publishes the
  preimage **on-chain**, so a griefing buyer can force public disclosure — route
  content that must stay confidential after delivery through HASHLOCK (commit a
  small receipt-of-access token, not the bulk content) or ZK_DELIVERY (2.1b).
- **Seller bond source/sizing.** The bond is a SEPARATE stake posted by the
  seller at escrow creation, sized `SellerBondBps = 1000` (10%) of the escrowed
  payment per denom, slashed in full to the buyer on a lost dispute and returned
  to the seller on honest delivery (auto-release or successful reveal). An
  underfunded seller fails the conditioned CONCLUDE leg → match fails → fallback
  (`ErrInsufficientBond`). The bond is only ever debited from a seller who SIGNED
  the condition (see the consent binding next / **D-075**), so it is not a
  solver-manufactured liability.
- **Consent — the condition is bound to a signed field (D-075).** The
  `DeliveryCondition` lives only on the solver-built `SettleLeg`, which no party
  signs; an untrusted solver must not be able to bind a seller to a delivery
  obligation + bond/slash it never signed. But a signed consent field DOES exist:
  `IntentPayload.condition` (proto field 3), carried in the signed
  `MsgSubmitIntent.ciphertext`. 2.1a binds them — a conditioned `SettleLeg` is
  only honored when its condition (full params) EQUALS the seller (FromSigil)
  intent's signed `IntentPayload.condition`, enforced at BOTH solution validation
  (propose-time, slashes a cheating solver) and the settlement boundary
  (`openConditionedEscrows`, the authoritative guard no Conclude path can skip).
  A seller's default NONE rejects any imposed condition → the conditioned match
  fails closed → fallback (`ErrConditionNotConsented`). Full detail in **D-075**.
- **Hash + committed-hash storage.** Release is gated on
  `SHA-256(preimage) == committed` (raw-byte preimage, constant-time compare,
  fresh-randomness-free §11.6, never panics §11.5). HASHLOCK's committed hash is
  `HashlockCondition.payload_hash` (in the record); OPTIMISTIC's is the
  `DataGrant.ContentHash`, persisted in a `0x04|escrow_id` side-record at creation
  because `OptimisticCondition` carries no hash field — **logic-only, no proto
  change**. (Genesis limitation: that side-record is not in the genesis proto, so
  a mid-flight OPTIMISTIC escrow does not round-trip its commit across export/
  import — drain disputes before an upgrade, the drain-before-rotate discipline.)
- **Ownership handoff — writer-going-forward.** x/escrow owns its store
  (`0x01` record, `0x02` participant index, `0x03` sweep index, `0x04` OPTIMISTIC
  commit); `conclude.go` calls `escrowKeeper.CreateEscrow` inside the atomic
  Conclude cache instead of writing the old session-store `0x09`. No in-place
  migration is needed: it is a fresh chain with no live `0x09` state, and **the
  bootstrap finding that x/receipt never read the escrow stubs** (it reads
  receipts, `0x07`/`0x08`) means the handoff orphans no reader. Escrow ids are
  byte-identical to the Phase-0 derivation (`deriveID` over
  `session_id‖content_hash‖from‖to`). Records + participant indexes are retained
  through terminal states (queryable, spirit of D-017); only the sweep entry is
  deleted on a terminal transition. NONE-condition DataGrants remain record-only
  (no funds, no sweep), now written through x/escrow.
- **Deterministic timeout sweep.** EndBlock iterates the `0x03` index (keyed by
  the escrow's current sweep height = `timeout_height`) ascending up to the block
  height, ids collected then processed in sorted order — never map iteration
  (§11.6). Every sweep outcome is terminal, so the sweep cannot loop. A dispute
  moves the entry from the window-end to the resolution deadline. Ordered AFTER
  x/session in the EndBlocker list so same-block CONCLUDE-created escrows
  (timeout_height > current) are never swept before they exist. The sim is
  escrow-inert (no conditioned legs in the seed vector), so the determinism vector
  is byte-identical.
- **Reputation hook.** DISPUTED→REFUNDED calls the pre-authored
  `x/sigil` `PenalizeLostDispute(chamber, sellerSigil)` (task-0.3 hook for "escrow
  dispute Phase 2"), closing that wiring TODO.
- **Acceptance.** HASHLOCK + OPTIMISTIC transition tables implemented; EVERY
  illegal transition is a load-bearing REJECT test (wrong preimage, post-timeout
  release, dispute-on-HASHLOCK, double-release/post-terminal, dispute-post-window,
  release-while-DISPUTED, double-dispute, unauthorized dispute, wrong reveal,
  insufficient bond). conclude.go:152 closed; escrow owns its store; x/receipt
  reads unbroken. `PenalizeLostDispute` wired; seller-bond slash correct.
  Deterministic auto-refund sweep. End-to-end conditioned CONCLUDE escrows the
  payment + posts the bond (session integration test). NO ZK/gnark. `make lint
  test` + determinism vector green.

## D-074 — Task 2.1a: conditioned escrows require an unambiguous bilateral match (recorded limitation, spirit of D-049)

- **Date:** 2026-06-25 (task 2.1a; constrains D-073)
- **SPEC:** §6.4 (matcher rules), §6.5; PHASE2_DESIGN §2.1.
- **Decision.** A match carrying any HASHLOCK/OPTIMISTIC condition must be an
  **unambiguous bilateral exchange**: exactly one conditioned leg, a DataGrant
  from seller→buyer, with the only other legs being coin payment legs
  buyer→seller, across exactly two distinct sigils. `validateSolution`
  (`validateConditionedMatch`) rejects anything else with
  `ErrConditionUnsupportedMatch` (session `1426`): multi-party conditioned matches,
  more than one conditioned leg, a conditioned non-DataGrant leg, or a non-coin
  counter-payment (e.g. data-for-svc under a condition). ZK_DELIVERY is rejected
  earlier, at the conservation gate (2.1b).
- **Rationale.** The payment-into-escrow funding (D-073) needs a single,
  unambiguous coin payment to escrow against the conditioned deliverable;
  multi-party or non-coin-paid conditioned settlement needs a richer escrow-
  funding model than 2.1a builds. This mirrors D-049's discipline (a deliberate,
  greppable, recorded matcher limitation rather than a silent rejection or an
  over-broad enablement), and keeps 2.1a bounded. It is liftable when richer
  conditioned-settlement funding lands.
- **Acceptance.** `validateConditionedMatch` ACCEPTS a well-formed bilateral
  data-for-coin conditioned match (unit + end-to-end CONCLUDE tests) and REJECTS
  the non-bilateral / wrong-class / multi-conditioned shapes; `make lint test`
  green.

## D-075 — Task 2.1a: a delivery condition must be SIGNED by the seller (SettleLeg.condition bound to IntentPayload.condition) — closing the solver-attached-condition consent gap

- **Date:** 2026-06-25 (task 2.1a; closes the D-073 consent gap; review-driven)
- **SPEC:** §1.1 (solvers are untrusted), §6.4 (solution validation), §6.5; the
  signed field is `IntentPayload.condition` (`intent.proto` field 3).
- **Problem (raised at the 2.1a review).** The `DeliveryCondition` attaches to the
  solver-built `SettleLeg` (`session.proto`), and `IntentLeg` has no condition
  field — so as first built, 2.1a let an UNTRUSTED solver bind a seller to a
  delivery obligation and the OPTIMISTIC dispute/slash state machine the seller
  never signed. The seller-bond "bounded grief" framing understated it: on the
  can't-reveal path (D-073 narrowed scope) a NON-CONSENTING seller takes a real
  10% slash. This is the mandate-chokepoint principle from the other side — an
  agent must not take slashable exposure via a leg attribute no signature covers.
- **Consent-provenance finding (with cites).** A signed consent field already
  exists: `IntentPayload.condition` (proto field 3), part of the signed
  `MsgSubmitIntent.ciphertext`. But the matcher read it NOWHERE — `grep` of
  `.Condition` across `x/veil` + `x/session` non-test code showed zero binding;
  `SettleLeg.condition` was never compared to the seller's signed payload. So the
  signed field existed but was ignored → effectively solver-attached.
- **Decision.** Bind them, no proto change: a conditioned `SettleLeg` is honored
  ONLY when its condition — including full parameters (HASHLOCK payload_hash +
  timeout_blocks; OPTIMISTIC dispute_window_blocks) — EQUALS the seller (the
  conditioned DataGrant leg's `FromSigil`) intent's signed
  `IntentPayload.condition`. Enforced at TWO layers: (1) **solution validation**
  (`validateConditionConsent` in `validateSolution`) so a solver proposing an
  unsigned/mismatched condition has its solution rejected as invalid and is
  slashed (D-024 incentive); (2) the **settlement boundary**
  (`openConditionedEscrows` in `conclude.go`, via `intentConditionForSigil`) — the
  AUTHORITATIVE guard, since `Conclude` does not re-run `validateSolution`, so no
  path to escrow creation can skip the consent check. Mismatch / no-signed-
  condition → `ErrConditionNotConsented` (session `1427`) → the conditioned match
  fails closed → fallback. Default (seller signed NONE) rejects every imposed
  condition. `conditionEqual` compares class + all params (a solver may not alter
  the timeout, payload hash, or dispute window the seller signed).
- **Scope of the consent requirement.** Bound to the SELLER (the `FromSigil` that
  bears the delivery obligation + bond/slash exposure — the party the untrusted
  solver could manufacture liability for). 2.1a does NOT yet bind the buyer's
  signed condition (see the forward items + the buyer-exposure determination
  below).
- **Buyer-side exposure — DETERMINED (yes, bounded).** The buyer (the payer/
  `ToSigil`) never faces a slash or loss: there is no buyer bond and its escrowed
  payment is ALWAYS either released to the seller or refunded to the buyer. But
  the buyer IS exposed to two UNSIGNED impositions, the symmetric form of the
  same disease: (a) a solver-attached condition escrows the buyer's payment under
  a dispute/timeout TIMELINE the buyer did not sign (HASHLOCK locks the payment
  until a preimage or the timeout; OPTIMISTIC delays finality by the dispute
  window) — a liquidity/finality imposition, not a loss; and (b) a solver can
  DROP or ALTER a condition the buyer DID sign (the signed→NONE/other
  "downgrade"), denying the buyer protection it asked for. Both are milder than
  the seller's slash but are real unconsented impositions on a non-signing party.
  The proper closure is symmetric buyer-side consent, with a genuine design
  nuance: the buyer cannot pre-commit to a HASHLOCK `payload_hash` (the
  receipt-of-access preimage is generated post-delivery, by the seller's
  delivery), so buyer consent needs a CLASS/window-level form, not the seller's
  exact-param equality. Recorded, not silently deferred.
- **Forward items (named, not floating — the §2.6 "nothing floats behind a
  phrase" discipline).** Owned, greppable, to be picked up as real work:
  - **D-075-FW-1 — buyer-side consent (the symmetric hole).** Bind the buyer's
    signed condition to the SettleLeg too, closing the imposed-timeline (a) and
    downgrade (b) exposures above. Design nuance: class/window-level consent for
    HASHLOCK (buyer can't pre-commit the `payload_hash`). Target: a consent-
    completeness pass (own task) once the per-leg field (FW-2) exists, or sooner
    at payload level if buyer downgrade is judged pressing.
  - **D-075-FW-2 — per-leg signed `IntentLeg.condition` (proto refinement).** Add
    a condition field to `IntentLeg` so a multi-leg intent consents per data leg
    (the payload-level `IntentPayload.condition` suffices only for the bilateral
    single-conditioned-leg 2.1a shape, D-074). Proto change → its own task (2.1b-
    adjacent or later); re-validate against the kyber pin per FLAG D if it touches
    the toolchain.
  - **D-075-FW-3 — signed→NONE/other downgrade.** A solver dropping or weakening a
    condition a party signed (subsumed by FW-1's buyer binding + the existing
    seller binding, but named so it is not lost): the seller binding already
    rejects a seller-signed condition settled as a different one; FW-1 extends the
    same to the buyer.
- **Acceptance.** Load-bearing consent tests: a solver attaching an OPTIMISTIC
  condition the seller signed NONE for is REJECTED with `ErrConditionNotConsented`
  at BOTH settlement (`TestConcludeConditioned_UnsignedConditionRejected` — no
  escrow opened, no bond debited) and validation
  (`TestProposeConditioned_UnsignedRejectedAtValidation` — invalid solution); a
  params-mismatch (signed window 50, settled 999) is REJECTED
  (`TestConcludeConditioned_MismatchedConditionRejected`); the CONSENTED path
  (seller signs the exact condition) opens the funded escrow + posts the bond
  (`TestConcludeConditioned_Hashlock/_Optimistic`). `make lint test` + determinism
  vector green.

## D-076 — Task 2.1b: ZK_DELIVERY — gnark groth16 verifier on the current toolchain (spike outcome (a)); compiled-in vk_id registry; instance-bound replay defense; constant ZK timeout; buyer-side consent gate

- **Date:** 2026-06-26 (task 2.1b)
- **SPEC:** §6.5 (ZK_DELIVERY condition class, `MsgSubmitDeliveryProof`), §11 rule 2
  (transition-table test), rule 5 (never panic), rule 6 (determinism); PHASE2_DESIGN
  §2.1 criterion 4 + §2.1b (FLAG A spike branch); builds on **D-066** (the kyber pin
  + the go-1.24-bump avoidance the spike tests against), **D-072** (the 2.1b
  instance-binding design point), **D-073/074/075** (the escrow module + bilateral
  pairing + signed consent ZK now reuses).
- **Spike outcome = (a): gnark works on the CURRENT toolchain (go 1.23), kyber pin
  intact → PROCEED.** Achieved by PINNING **`github.com/consensys/gnark v0.13.0` +
  `gnark-crypto v0.18.0`**, whose go directives are exactly `1.23.0` — satisfied by
  the repo's `go 1.23`. This was a deliberate version choice, the lever §2.1b/D-072
  named ("pin a different gnark version"): gnark **latest v0.15.0 requires go ≥
  1.25.7** (and drags `golang.org/x/crypto` 0.21→0.48) and **v0.14.0 requires go
  1.24.0** (the bump D-066 avoided) — both would perturb the crypto foundation. v0.13.0
  is the highest gnark whose floor stays ≤ 1.23. Evidence: a throwaway isolated module
  forcing kyber v1.3.1/v0.3.1 + gnark v0.13.0 into one graph → go directive stays 1.23,
  effective toolchain `go1.23.12`, `go build` exit 0, the kyber BLS12-381 suite RUNS
  (pin intact at runtime), a groth16 round trip works, and the instance-binding replay
  rejection holds at the gnark level. **No Go bump.** Ride-along (indirect, no toolchain
  change): `golang.org/x/crypto` 0.33→0.39, `x/sys`→0.33, `x/sync`→0.15; the directive
  normalized `go 1.23`→`go 1.23.0` + a `toolchain go1.23.12` line (still 1.23 minor, NOT
  the 1.24 bump). The determinism vector was re-run RIGHT AFTER the dep add (before any
  ZK logic) and is **byte-identical** — confirming the gnark dep graph leaves the
  threshold/Veil path unperturbed (the (a)/(b) safety re-confirmation, done even though
  (a) carries no toolchain bump).
- **The circuit (AUTHORED, `crypto/zkdelivery`).** `DeliveryCircuit` over BN254 with
  the gnark `std/hash/mimc` gadget proves, WITHOUT revealing the payload:
  `MiMC(Payload) == SchemaCommitment` (the "payload matches the committed schema/hash"
  core, §6.5) AND binds `EscrowID` (a public input absorbed into a MiMC so it is a
  constrained wire). **Authored vs library:** gnark/gnark-crypto (the R1CS frontend,
  groth16 backend, BN254 pairing + MiMC) is LIBRARY; authored = the circuit definition,
  the `Verify` wrapper (deserialize + reconstruct public witness + recover→INVALID), the
  compiled-in vk registry, and the `FieldFromBytes` encoding shared by prover and
  verifier. No novel primitive is authored.
- **THE replay/instance binding (D-072, the load-bearing security property).** The
  `MsgSubmitDeliveryProof` carries ONLY the proof bytes. `ReleaseZk` reconstructs the
  public witness — `EscrowID` from the escrow record's id, `SchemaCommitment` from the
  seller-signed condition — **never from the submitter**. groth16 binds every public
  input into the verification equation, so a proof generated for escrow A's id is
  REJECTED when the verifier supplies escrow B's id, even when A and B share a schema and
  vk. This is the ZK analog of Phase-1's per-ciphertext identity binding (D-066): bind to
  the INSTANCE, not the type. Watched by `TestZk_ReplayCrossInstance` (keeper) +
  `TestVerify_InstanceBinding_ReplayRejected` (crypto): if `EscrowID` is dropped from the
  reconstructed witness, the replay succeeds and both tests fail red. Same-instance replay
  (re-submitting a spent proof) is caught by the terminal-state gate (`ErrTerminalState`).
- **vk_id registry decision = COMPILED-IN (the §2.1 plan-stop decision deferred from
  2.1a).** Verifying keys are embedded in the binary (`//go:embed vk/delivery_v1.vk`,
  396 bytes) behind a `vk_id → VerifyingKey` map; a proof names its key via the SIGNED
  `ZkDeliveryCondition.vk_id` (`delivery/v1`, versioned for a future circuit revision).
  Chosen over a genesis-param map or an on-chain registration msg because it matches
  x/escrow's established **compile-time-constant, non-governance posture** (D-073: no
  Params message, no `MsgUpdateParams`), is deterministic (same binary → byte-identical
  keys, no consensus state), and keeps the trusted setup inside the audited binary. An
  unknown vk_id fails CLOSED at escrow creation (`ErrUnknownVk` → the conditioned CONCLUDE
  leg falls back). The matching **proving key (116 KB) lives ONLY in the test-only
  `crypto/zkdelivery/zktest` package** (`//go:embed`, imported only by tests) and NEVER
  enters the consensus binary.
- **ZK_DELIVERY transition table (criterion 4), one authoritative gate.** Two new events
  in `nextState`: `evtZkRelease` (ACTIVE→RELEASED, guard = groth16 verifies) and
  `evtZkTimeout` (ACTIVE→REFUNDED, the sweep). Funds like HASHLOCK — buyer payment in, **no
  seller bond** (release is proof-gated, not bond-gated). Every illegal path is a
  load-bearing REJECT: release-without-proof / invalid-proof → `ErrInvalidProof`;
  cross-instance replay → instance binding rejects; same-instance replay → `ErrTerminalState`;
  a `MsgReleaseEscrow` (preimage path) on a ZK escrow → `ErrWrongClass` (ZK releases ONLY via
  `MsgSubmitDeliveryProof`); a dispute on a ZK escrow → `ErrNoDisputePath` (like HASHLOCK).
  The exhaustive `TestTransitionTable_Exhaustive` now drives `classZk` across the full
  state×event product.
- **Auto-refund timeout = a COMPILE-TIME CONSTANT** `ZkDeliveryTimeoutBlocks = 14400`
  (~1 day at 6 s blocks). `ZkDeliveryCondition` carries no timeout field, and 2.1b
  deliberately did NOT add one: gnark is already the single hard thing, so a constant
  (no proto regen) mirrors 2.1a's side-record move (D-073) and keeps the consent surface
  unchanged (the timeout is protocol policy, not a solver-imposed value). A per-condition
  ZK timeout (a `ZkDeliveryCondition.timeout_blocks` proto field) is a recorded forward
  item.
- **Determinism (§11.6) + safety (§11.5).** On-chain the ONLY ZK operation is
  `groth16.Verify` — a fixed pairing computation over a fixed embedded VK and a public
  witness reconstructed from state, with NO fresh randomness; `zkdelivery.Verify` wraps
  the whole path (proof deserialization of attacker-controlled bytes, witness build,
  verify) in `recover → ErrInvalidProof` so the chain never panics on a malformed proof.
  The trusted-setup randomness ("toxic waste") is OFF-CHAIN / build-time (the
  `crypto/zkdelivery/gen` tool), the analog of the DKG's off-chain `crypto/rand` (D-066).
  The sim is escrow-inert (no conditioned legs in the seed vector, D-073) so the
  determinism vector is byte-identical; real-path verification determinism is asserted by
  a unit test.
- **Consent — BOTH parties sign the ZK condition (review-driven, the buyer-side gate).**
  ZK routes through the SAME bilateral (D-074) + signed-consent (D-075) gate as
  HASHLOCK/OPTIMISTIC (a ZK leg is a DataGrant; `conditionEqual` compares
  `schema_commitment + vk_id`), so the seller signs the exact schema + circuit. **But the
  2.1a consent gate was SELLER-only (`FromSigil`), and a review trace found the buyer-side
  mirror of the seller hole D-075 closed: because ZK release is SELLER-DRIVEN (the seller
  submits the proof) on a `schema_commitment` NOT tied to the buyer's want-key, a NONE-buyer
  could be matched into a ZK escrow whose payment releases on a seller-chosen schema** (the
  pre-fix `TestConcludeConditioned_Zk` demonstrated exactly this — a `submitOpen` plain buyer
  concluded into a ZK escrow). This is a fail-OPEN consent hole at the same chokepoint 2.1a
  closed seller-side, not a documentable limitation. **Fix:** for ZK_DELIVERY the BUYER
  (`ToSigil`) must ALSO have signed the exact condition, enforced at BOTH chokepoints
  (`validateConditionConsent` propose-time + `openConditionedEscrows` settlement-boundary),
  fail-closed → fallback (`ErrConditionNotConsented`). Exact-param buyer consent is feasible
  for ZK (the "committed schema/hash" of SPEC §6.5 is advance-known, unlike HASHLOCK's
  post-delivery receipt token) and closes BOTH the consent-to-conditioned-delivery hole AND
  the schema-not-tied-to-the-buyer's-want concern at once: buyer-signed = seller-signed =
  imposed, so the proof attests delivery of the tri-agreed schema. **ZK-only:** HASHLOCK's
  `payload_hash` is a post-delivery token the buyer cannot pre-commit, and OPTIMISTIC's
  committed hash IS the buyer-tied `DataGrant.ContentHash` (D-073) — so their buyer consent
  needs the class/window-level form of D-075-FW-1 and stays deferred; ZK is the one class
  whose release is seller-driven on an advance-committed, want-untethered schema. The
  Phase-0 ZK rejection at the conservation gate (`conservation.go`, `ErrConditionsNotEnabled`)
  is LIFTED — all three classes are enabled and conservation no longer policies conditions.
- **Scope discipline (deliberate, per the task's FW guard).** **FW-2 NOT folded in;** the
  buyer-side gate is the FW-1 mirror-fix for ZK, NOT FW-2's proto work. 2.1b does not touch
  the intent proto — the buyer-consent gate reuses the existing signed `IntentPayload.condition`
  field + `conditionEqual` at the chokepoint (the same justification 2.1a used to close the
  seller hole without FW-2). D-075-FW-2 (per-leg `IntentLeg.condition`) and the
  HASHLOCK/OPTIMISTIC half of FW-1 (their class/window-level buyer consent) remain their own
  task. **Circuit scope narrowed (recorded, spirit of D-073's OPTIMISTIC narrowing):** the
  circuit proves knowledge of a single committed field-element payload bound to the instance;
  a richer typed-schema-structure circuit (proving structural conformance, multi-field/Merkle
  payloads) is a forward item.
- **Carried to the 1.8-style audit (new-surface obligations).** (a) **Trusted setup:** the
  embedded VK comes from a single-party dev `groth16.Setup`; a production deployment MUST
  replace it with an MPC ceremony — a mainnet gate, the analog of D-066's external-audit
  gate. (b) **Circuit soundness/completeness:** the MiMC commitment-opening + the
  EscrowID-as-public-input binding argument is an audit unit (the binding rests on
  groth16's public-input mechanism, asserted by the watched replay tests but not formally
  proven). (c) **New consensus dependency:** gnark v0.13.0 verification runs in a Msg
  handler; audit the verifier integration + the recover-never-panic guarantee on
  adversarial proof bytes.
- **Forward item — flake-hardening (named, not floating; the test-harness analog of
  the §11.6 determinism discipline).** `x/mandate/keeper` `TestIssueMandateReplace`
  flaked once during this task (`MandatesBySigil` returned 0 of 1 expected at
  `keeper_test.go:266`), then passed **20/20 on isolated re-run** and the next full
  `make lint test` was clean. It is **NOT a 2.1b regression** — the mandate test binary
  links no gnark/escrow/zkdelivery, and the mandate-heavy determinism vector is
  byte-identical to baseline. Root cause is a `randAddr()`-seeded fixture
  nondeterminism (`secp256k1.GenPrivKey()` per call, `keeper_test.go:101`). A test that
  flakes on a random seed is owned, not a loose note: the fix (seed the fixture
  deterministically / remove the random-address nondeterminism) is a flake-hardening
  forward item, a candidate for a determinism-harness / 2.6-adjacent pass.
- **Acceptance.** Spike outcome (a) reported + handled (gnark v0.13.0 pin, no bump,
  determinism re-confirmed). ZK_DELIVERY transition table implemented; release-without-
  proof / invalid-proof / replayed-proof each a load-bearing REJECT. Replay-binding proven
  instance-specific (proof for A rejected against B, watched at crypto + keeper level).
  vk_id registry decided (compiled-in) + implemented. End-to-end conditioned ZK CONCLUDE
  opens a funded escrow (both parties signing) and a bound proof releases it
  (`TestConcludeConditioned_Zk`). **Buyer-side consent gate watched:** a NONE-buyer ZK
  match is REJECTED at both settlement (`TestConcludeConditioned_Zk_BuyerNotConsented`, no
  escrow) and validation (`TestProposeConditioned_Zk_BuyerNotConsentedAtValidation`), and a
  buyer-schema mismatch is REJECTED (`TestConcludeConditioned_Zk_BuyerMismatch`). `make lint
  test` + determinism vector green; 2.1a's HASHLOCK/OPTIMISTIC suites still green.

## D-077 — Task 2.2 D-060 RE-EVALUATION (UPDATES D-060): stay on SDK v0.50 for Phase 2; wasmd integrates on the v0.50 line — the answer is unchanged but the binding reason has MOVED

- **Date:** 2026-06-26 (task 2.2 dependency spike, STOP #1; approved)
- **SPEC:** §14 D8, §3 (version pins), §0 (CVM contract runtime); PHASE2_DESIGN
  §2.2 (FLAG B spike-then-decide) + FLAG A (toolchain/kyber-pin risk).
- **UPDATES D-060** (the Phase-1 D8 resolution) with task-time current-release
  evidence, exactly as PHASE2_DESIGN §2.2 criterion 1 requires ("Record the result
  as a new DECISIONS entry updating D-060 — the doc does not pre-bake the
  outcome"). Builds on **D-066** (the kyber-bls12381 v0.3.1 pin + the go-1.24-bump
  avoidance the toolchain axis tests against) and is the dependency baseline the
  **D-072–076** escrow surface + the 2.2 CVM bindings build on.
- **Decision: do NOT bump to SDK v0.53 for Phase 2. Keep cosmos-sdk v0.50.x,
  block-sdk/v2 v2.1.5 (the laned mempool), ibc-go v8, the goleveldb replace pin,
  go 1.23.x, and the `app/unordered/sigverify.go` fork (D-027) + the D-026 TxBody
  extension-option unordered encoding. Integrate wasmd on the v0.50 line.** The
  outcome matches D-060, but D-060's flip-rule ("bump only when ALL of (a)(b)(c)")
  still fails on (a), and the DECISIVE constraints have shifted — recorded loudly
  rather than rubber-stamped.
- **Empirical spike (throwaway, Go module proxy / current tags — the same method
  as D-060's Phase-1 spike, not a stale memory assumption). Our pins: cosmos-sdk
  v0.50.15, go 1.23 (`go 1.23.0` + `toolchain go1.23.12`). Kyber ceiling (D-066):
  must not cross into go 1.24 (forces kyber-bls12381 v0.3.4).**
  | Dependency | v0.50 line | v0.53 line | Go floor |
  |---|---|---|---|
  | `skip-mev/block-sdk/v2` | **v2.1.5** (our pin, SDK v0.50.3) | **none** — frozen, no v2.1.6 / v2.2.0 / `/v3` | 1.22.4 |
  | `cosmos/ibc-go` | v8.8.0 / v9.0.2 (both SDK v0.50.9) | v10.3.0+ → SDK v0.53 (v10.7.0→v0.53.4) | 1.23.8 |
  | `CosmWasm/wasmd` | v0.54.x (SDK v0.50.11, ibc-go v8); v0.55.1 (SDK v0.50.12, ibc-go v10.0.0) | v0.61.0 (SDK v0.53.0, ibc-go v10.3.0) | v0.50: 1.23.1–1.23.6; v0.53: v0.61.0=1.23.8 but v0.61.14=**1.24.0**, v0.70=1.25.9 |
  | `cosmos-sdk` | v0.50.15 (ours) | v0.53.7 (mature) | v0.50→1.22; v0.53→1.23.2 |
  (wasmd v1.0.0 is a 2021 ghost tag → SDK v0.36 / go 1.12; ignored.)
- **Flip-condition re-check (a)–(c) + the 2.2-forced axes (d)–(e):**
  - **(a) MVS-clean block-sdk on v0.53 — STILL NO, and decisive.** block-sdk is
    frozen at v2.1.5 (v0.50-only); no v2 successor, no `/v3`, effectively
    unmaintained. Bumping to v0.53 means LOSING the Block-SDK laned mempool — a
    core §6.7 feature wired in task 0.6 — and rebuilding/replacing it. D-060's
    rule required ALL of (a)(b)(c); (a) fails. UNCHANGED from Phase-1.
  - **(b) v0.53-compatible ibc-go — NOW YES (the one condition that FLIPPED).**
    ibc-go v10.3.0+ targets SDK v0.53 (v10.7.0 → v0.53.4). But it is a v8→v10
    MAJOR bump (API churn) and is MOOT without (a). (Note for 2.5: ibc-go v8 is
    still maintained and serves IBC-USDC; the v10 line is NOT required.)
  - **(c) D-057 sim/FinalizeBlock lifecycle — trivially intact by staying v0.50**
    (no change); would need re-verification only on a bump.
  - **(d) wasmd does NOT force v0.53.** A MAINTAINED wasmd line targets SDK v0.50:
    v0.54.x (ibc-go v8 — matches our current stack exactly) and v0.55.1 (ibc-go
    v10.0.0, still v0.50). The 2.2 deliverable (CVM bindings for CONCLUDE/escrow)
    is fully achievable on v0.50. **The forcing function does not force the bump.**
  - **(e) toolchain vs the kyber pin — v0.50 is SAFE; v0.53 is FRAGILE against the
    foundation.** v0.50 + wasmd v0.54/v0.55 floors at go 1.23.1–1.23.6
    (kyber-safe). v0.53 is kyber-safe ONLY pinned to wasmd v0.61.0 (go 1.23.8) —
    but that line's current patch v0.61.14 already requires **go 1.24** (the exact
    D-066 collision), and the next SDK line (v0.54 / wasmd v0.70) needs go 1.25. So
    v0.53 puts us one wasmd patch away from the toolchain collision 2.1b's spike
    (D-076) was built to avoid.
- **Why the answer is the same but the REASON moved (the recorded point).** At
  Phase-1 close (D-060) the blocker was "the whole v0.53 ecosystem isn't ready" —
  now partly obsolete: ibc-go v10 and wasmd v0.61 ARE ready. Today's binding
  constraints are different: **(a)** block-sdk is abandoned with no v0.53 path
  (bumping drops the laned mempool), and **(e)** the v0.53 wasmd line's current
  patches collide with the kyber pin (go 1.24). Bumping would mean rebuilding the
  mempool + a v8→v10 IBC major + a determinism re-baseline + a fragile sub-latest
  wasmd pin — WHILE introducing wasmd — violating D-060's own "isolate variables"
  discipline at the moment the largest new (CVM) surface lands.
- **The bump's only real 2.2 prize is not needed by the deliverable.** Native
  ADR-070 retiring the sigverify fork (`app/unordered/sigverify.go:16`) + the D-026
  encoding is irrelevant to the CVM-bindings work; the fork is conformance-pinned
  (a low, bounded, tested cost). The `sigverify.go:16` CONCLAVE-TODO re-points
  FORWARD again (next v0.53 re-evaluation = 2.5's IBC-line check or a later boundary).
- **2.2 proceeds on v0.50:** add wasmd (recommended line **v0.54.x** — SDK v0.50.x
  + ibc-go v8, matching our stack with minimal re-baseline; v0.55.1 only if its
  fixes are wanted at the cost of a v8→v10 IBC bump — a STOP #2 detail), keep
  block-sdk + the sigverify fork, stay at go 1.23.x (kyber pin intact), build the
  CVM CONCLUDE/escrow bindings.
- **Rationale.** Re-deriving rather than inheriting keeps the §14-D8 contract
  honest: the spike confirms the v0.50 choice on data current at task time, names
  the one flipped input (b) so the record is not stale, and isolates the wasmd
  integration from a mempool rebuild + IBC major + toolchain collision it does not
  need. The v0.53 re-evaluation remains live for a future boundary (2.5 / Phase 3),
  re-checked the same empirical way.

## D-078 — PHASE2_DESIGN.md Rev 2: as-built reconciliation after 2.1a/2.1b + the 2.2 STOP-#1 spike; FLAGs A/B recorded RESOLVED; NEW numbered task 2.7 (escrow consent completeness — FW-1 + FW-2); the two-pin go-1.23.x toolchain ceiling named

- **Date:** 2026-07-02 (doc-maintenance session; docs-only — no source/proto/
  logic touched; the uncommitted 2.2 working tree left intact)
- **SPEC:** §10 (phase plan); PHASE2_DESIGN.md (the D-072 phase authority);
  builds on D-073–D-077.
- **Why a revision.** The phase authority was authored (Rev 1, D-072) before
  any Phase-2 build. Three sessions later its risk register was stale — FLAG A
  (gnark/kyber) and FLAG B (v0.53 flip) are RESOLVED with recorded outcomes
  (D-076 outcome (a); D-077), §2.1 described as future work what is now built
  (with plan-vs-built deltas the next tasks must not mis-inherit), and the
  D-075/D-076 FW seams had no numbered owner. A stale authority silently
  misleads the next session's bootstrap.
- **What Rev 2 changes (doc-level decisions made here):**
  - **Task 2.7 created** (escrow consent completeness): owns D-075-FW-1
    (HASHLOCK/OPTIMISTIC class/window-level buyer consent), D-075-FW-2
    (per-leg `IntentLeg.condition` proto refinement, incl. the
    does-it-relax-D-074 plan-stop question), and the timeouts→params
    promotion (`ZkDeliveryTimeoutBlocks` et al.) as a plan-stop decision.
    Sequenced after 2.6, off the exit path — the same
    "a ledger entry, not a floating phrase" discipline that created 2.6.
  - **Toolchain ceiling named as an invariant (§4):** stay on go 1.23.x until
    BOTH pins — kyber-bls12381 v0.3.1 (D-066) AND gnark v0.13.0 (D-076) — are
    re-validated in ONE recorded decision; any dependency whose floor crosses
    go 1.24 is a STOP, not a bump.
  - **2.2 resume checklist made load-bearing:** determinism re-baseline on the
    already-dep-bumped tree (iavl 1.2.2→1.2.4 is on the state-commitment path;
    math v1.5.0, log v1.6.1 — MVS pre-pulls, wasmd itself NOT yet added)
    BEFORE wasmd wiring, so any vector drift is attributable to the bumps in
    isolation; then wasmd v0.54.x pin → bindings STOP #2. The throwaway
    `scripts/_baseline.sh` / `_d060_spike*.sh` are named for deletion before
    the 2.2 commit.
  - **Consent invariants elevated:** D-075/D-076 recorded as constraints on
    2.2 (bindings must not let contract-originated flows bypass the signed-
    consent chokepoints) and 2.6 (matcher richness must not settle a condition
    against a non-signer; a D-074 collision routes to 2.7's FW-2).
  - **Rev 1 corrections recorded:** the "x/receipt reads escrow records"
    dependency claim was false (x/receipt never read escrows — D-073);
    ownership handoff = writer-going-forward, not migration; TODO #5
    (sigverify fork) re-pointed off 2.2 to 2.5's v0.53 checkpoint per D-077;
    solution.go marker drift (141→143, 330→349) refreshed by live grep.
  - **Edge deliverables sharpened to named artifacts:**
    `docs/edge/X402_SURFACE.md` / `docs/edge/7683_SURFACE.md` (2.3/2.4), each
    exercised once by script against a running node — "done" is checkable.
  - **Exit-demo leg-readiness table + mainnet-gate sidebar:** the ZK leg is
    DONE; the demo can run on the dev VK, mainnet cannot (MPC ceremony +
    circuit-soundness audit + external audit stay mainnet gates, not
    Phase-2 tasks).
- **Also fixed (working-tree hygiene):** the uncommitted D-077 draft had been
  inserted MID-D-076 (before its "Carried to the 1.8-style audit" /
  flake-hardening / "Acceptance" bullets, stranding them under the wrong
  header). Moved D-077 after the end of D-076 so each entry is contiguous;
  no text altered in either entry.
- **Rationale.** Result-oriented means the authority states what IS, what
  remains, and the exact next action (resume 2.2 at checklist step 1) — with
  every resolved risk carrying its citation instead of its original warning.

## D-079 — Task 2.2: wasmd v0.54.9 integrated (D-077 line); CVM bindings = escrow-msg allowlist + read-only CONCLUDE; three-layer no-forge authority model; permissioned upload; wasm sim-inert; first CGO dependency

- **Date:** 2026-07-02 (task 2.2 build, STOP #2 approved: wasmd v0.54.x /
  read-only CONCLUDE / permissioned gov-only upload)
- **SPEC:** §0 (CVM naming), §6.4 (all-or-nothing CONCLUDE), §6.5 (escrow
  msgs), §6.7 (lanes), §11.5/§11.6 (safety/determinism); PHASE2_DESIGN §2.2
  Rev 2 (D-078 resume checklist); builds on **D-077** (the v0.50 line),
  **D-073–076** (the escrow gates the bindings ride), **D-066/D-076** (the
  go-1.23.x two-pin ceiling the wasmd pin respects).
- **Dependency pins.** `wasmd v0.54.9` (go directive 1.23.2; cosmos-sdk
  v0.50.15 and ibc-go v8.8.0 EXACT matches to ours) + `wasmvm/v2 v2.2.8`
  (go 1.22). Repo directive moved `go 1.23.0`→`go 1.23.2`, toolchain stays
  `go1.23.12` — INSIDE the §4 two-pin ceiling (kyber v1.3.1/v0.3.1 and gnark
  v0.13.0 untouched). Ride-alongs: `iavl 1.2.2→1.2.4` (state-commitment
  path — vector-validated below), `cosmossdk.io/math 1.4.0→1.5.0`,
  `log 1.4.1→1.6.1`, `go-getter 1.7.4→1.7.5`. block-sdk v2.1.5, cometbft
  v0.38.23, goleveldb replace: unchanged.
- **FIRST CGO DEPENDENCY — recorded loudly.** wasmvm links `libwasmvm` (C
  FFI); everything before 2.2 (kyber, gnark included) was pure Go. Build/
  test/vector now require `CGO_ENABLED=1` + a C toolchain (default on the WSL
  builder; verified by an isolated spike: `LibwasmvmVersion()=2.2.8`, exit 0).
  Ops note for later packaging: static/musl builds need `libwasmvm_muslc`.
- **Wiring (app.go / app_config.go / ante.go / wasm.go).** wasm store key +
  keeper (ICS4Wrapper = ChannelKeeper — no fee middleware in this stack), wasm
  IBC route added and `SetRouter` DEFERRED to after wasm keeper construction
  (the CVM plugins need the Conclave keepers, so the wasm section sits after
  the Conclave modules); module registered via `permissionedWasmModule` (a
  DefaultGenesis override — see posture below); state-sync snapshotter
  registered when a snapshot manager exists; ante gains wasmd's four
  decorators (LimitSimulationGas, CountTX on the wasm store, GasRegister,
  TxContracts) directly after SetUpContext — the fork'd SigVerification
  pipeline is untouched and the unordered conformance suite stays green.
  **Lanes (the step-3 plan-stop): wasm msgs ride the GENERAL lane** — the
  §6.7 partition routes unlisted TypeURLs to general; no new lane, no
  msgClasses change, lane suites green.
- **wasmvm dir = node-local disk cache, NEVER consensus state.** Real nodes:
  `<home>/wasm` (same appOpts pattern as the DKG share store). Tests pass no
  home → each app instance gets a FRESH temp dir, because wasmvm holds an
  EXCLUSIVE file lock on its dir and in-process suites construct several apps.
- **Permissioned posture (STOP #2 decision).** Default genesis ships
  `CodeUploadAccess=Nobody` + `InstantiateDefaultPermission=Nobody` via the
  `permissionedWasmModule` DefaultGenesis override; only governance
  (wasmd's GovAuthorizationPolicy bypasses access config for authority msgs)
  can store/instantiate or loosen params. Watched by the e2e (non-gov store
  rejected; gov store/instantiate succeeds).
- **CVM bindings (`app/cvm`) — the authority model, three layers:**
  1. **ALLOWLIST (encoder):** custom msgs = exactly
     {release_escrow, submit_delivery_proof, raise_dispute}; strict JSON
     (unknown fields rejected, exactly one branch). NO Conclude encoding
     exists — settlement authorship stays module-authority-only (EndBlock
     Half-B + authority-gated MsgConclude); the contract-actionable pieces of
     settlement ARE the escrow legs, each gated by the 2.1 keeper rules
     (knowledge-gated release/proof, payer-identity-gated dispute).
  2. **NO PASSTHROUGH:** the Any/Stargate ENCODER is replaced with a hard
     rejection (wasmd default would encode arbitrary protobuf); Stargate/gRPC
     QUERIES stay on wasmd's default rejection. Rejections deliberately do
     NOT wrap wasmd's ErrUnknownMsg (the handler chain treats that as
     "try next handler" and swallows provenance) — they hard-fail with CVM-
     attributed errors.
  3. **SIGNER STAMPING:** every encoded msg carries the CALLING CONTRACT's
     address as signer (schema has no signer field; wasmd's dispatch
     independently verifies every signer IS the contract). A forged
     MsgConclude would need authority==contract (dispatch check) AND
     authority==gov (keeper check) — structurally impossible.
  **Consent gates unreachable from CVM (the Rev-2 step-4 constraint):** the
  allowlist contains NO intent/solution submission — a contract cannot attach
  a condition at all, so the D-075/D-076 signed-consent chokepoints cannot be
  bypassed by contract-originated flows; the escrow msgs a contract CAN send
  act on escrows whose consent was established at creation.
- **Read surface.** Query plugins expose three BOUNDED point lookups —
  session, receipt, escrow — as proto-JSON over committed state (the
  read-only face of CONCLUDE: contracts observe settlement, never author it).
  No iteration a contract can widen.
- **Gas.** Contract execution meters wasm gas via wasmd's default GasRegister
  (ante GasRegisterDecorator); CVM→native EXECUTE dispatches on the same ctx
  gas meter (native escrow work costs SDK gas exactly as a direct tx);
  CVM queries are metered through the store reads they perform. Watched by
  the e2e gas assertion.
- **Sim/determinism.** x/wasm implements AppModuleSimulation (random contract
  stores) — EXCLUDED from the simulation manager (custom-only-ops posture,
  D-057/D-063); its sim genesis stays the permissioned default, so the vector
  is wasm-inert. Vector run on the wired tree: byte-identical across
  two independent processes × 5 seeds — validating the iavl 1.2.4 / math
  1.5.0 ride-alongs on the state-commitment path. (Deviation from the D-078
  step-2 ordering surfaced at review: the vector ran AFTER wiring, not on the
  dep-bumps-only tree; it was byte-identical so no bisection was needed.)
- **Load-bearing tests.** `app/cvm` units pin the allowlist (a "conclude"
  spelling never encodes), the stamping invariant (naming a signer is
  rejected), exactly-one-branch, and the Any rejection. The app-level e2e
  (`app/cvm_bindings_test.go`) runs a REAL contract (wasmd's reflect, via
  wasmvm) end to end: a contract-emitted custom msg lands in and is rejected
  by the CVM allowlist WITH its provenance; contract-emitted
  Any(MsgConclude) is rejected by layer 2; MsgConclude under a contract
  authority is rejected by the session keeper (layer 3); a contract RELEASES
  a HASHLOCK escrow with the preimage (positive path: state→RELEASED, payee
  paid, gas consumed) and CANNOT dispute an escrow whose payer it is not;
  the CVM querier serves the bounded escrow read and rejects unknown shapes;
  stargate queries stay rejected; the permissioned posture holds. Dispatch
  subtests replicate wasmd's SDKMessageHandler steps exactly (production
  encoder → signer check → real MsgServiceRouter) because reflect's Custom
  msg/query are TYPED (debug|raw / ping|capitalized) and cannot spell
  Conclave's schema — which also means:
- **DEVIATION (Rev-2 step 5) — localnet contract-release smoke deferred to
  the exit demo.** A live-network smoke needs a contract BINARY that emits
  CVM custom msgs; reflect cannot, and authoring a CVM-schema contract needs
  a Rust/wasm32 toolchain (not on the builder). The in-process e2e covers the
  full wasm→binding→keeper path through real wasmvm execution; the live
  smoke rides the §6 exit-demo task, whose purpose-built CVM demo contract
  is its natural vehicle. Surfaced, not silently skipped.
- **Seams closed/re-pointed.** `session tx.pb.go:322` + `tx.proto` seam
  comment RESOLVED read-only (comment-only edit, both files in sync, no proto
  regen); `app/unordered/sigverify.go` TODO re-pointed to 2.5-or-later per
  D-077.
- **Rationale.** The bindings inherit the phase's consent/authority
  discipline instead of inventing a parallel permission system: a contract is
  just another account-shaped principal, every existing keeper gate applies
  to it unchanged, and the one msg no principal but the module authority may
  send remains exactly that.

## D-080 — Phase-3 plan + methodology authored: PHASE3_PLAN.md (PROPOSED forward authority, three-class labeling) + METHODOLOGY.md (standalone transferable discipline)

- **Date:** 2026-07-10 (planning/research session at `9722912`, mid-Phase-2 —
  2.3–2.7 + the §6 exit demo still open; docs-only, no source/proto/logic
  touched; baseline verified this session: lint exit 0, test exit 0,
  sim-determinism PASS — 5 seeds `[1,2,7,12,42]` byte-identical across two
  independent processes).
- **SPEC:** §10 (Phase 3 = Chambers ALONE — the plan's authority anchor),
  §8(d) (external audit = mainnet gate), §14 D6 (pre-testnet) / D7
  (pre-announce); AUDIT_SCOPE_1.8 (O1–O4, L1–L3, T1–T3); PHASE2_DESIGN §4
  (two-pin ceiling); AUDIT F-2/F-3/F-5/F-6.
- **Deliverable 1 — `docs/PHASE3_PLAN.md` (PROPOSED, not yet phase
  authority).** SPEC §10's Phase 3 is quoted as ONE task (Chambers); every
  section header carries one of three authority classes — [SPEC §10 PHASE 3]
  (3.11 Chambers only), [SPEC-MANDATED, NOT PHASE-NUMBERED] (3.1 audit, 3.2
  VK ceremony, 3.9 D6, the D7 gate row), [PROPOSED — BEYOND CURRENT SPEC]
  (3.3–3.8, 3.10, 3.12). The plan becomes authority only at task 3.0 (a
  Rev-at-Phase-2-boundary reconciliation, the D-078 pattern) — surfaced as a
  deviation from D-072's authored-at-the-boundary precedent, forced by the
  research findings below. Complete debts disposition (§5.1 — every O/L/T/F/
  FW item, every SPEC §14 open decision → a task or a reasoned deferral) and
  §5.2 advisories to the remaining Phase-2 tasks.
- **Deliverable 2 — `docs/METHODOLOGY.md`.** The working discipline codified
  project-independently: 10 rules, each with a mechanical procedure + a real
  repo instance (the failure cases load-bearing: the 2.2 fake-stop and its
  bisection cost, the D-044 phrase-not-task that returned as audit finding
  F-3, the 2.2 got-lucky sequencing deviation surfaced-not-normalized), plus
  the per-task execution checklist. Referenced from the plan; Phase-3
  sessions bootstrap from both.
- **Research findings that moved recorded positions (searched 2026-07-10;
  re-verify at task time):** (1) block-sdk **ARCHIVED 2025-01-31**, upstream
  recommends fork-and-maintain — stronger than D-077's "frozen"; (2) go 1.23
  **EOL 2025-08-31**; (3) cosmos-sdk **v0.50.x EOL** — active release
  families are 2025.1 (SDK 0.53/CometBFT 0.38/ibc-go v10) and 2026.1 (SDK
  0.54/0.39/v11); (4) **ibc-go v8 in no active family** — D-077's "v8 still
  maintained and suffices" is STALE for 2.5 (recorded as a §5.2 advisory,
  resolved at 2.5's plan-stop); (5) CometBFT v1/v2 discontinued upstream,
  work concentrates on v0.38→v0.39 — the VE surface is not stranded; (6) the
  two gnark Groth16 advisories affect ≤v0.10.0 only — the v0.13.0 pin is
  unexposed, so the toolchain crossing is a **support-horizon decision, not
  an exploit fire**; (7) no audited threshold-mempool construction covers the
  authored surface (Fairblock/FairyRing uses externally-FIXED identities —
  exactly the divergence Conclave made; no public audit of its threshold-IBE
  found), so adoption does NOT close O1; the batched-threshold-decryption
  literature + Aptos validate a **session-scoped-identity re-derivation** as
  a middle path; (8) x402 foundation targets spec v1.0 Q3 2026 (advisory to
  2.3); ERC-7683 production-dominant (confirms 2.4); AP2 also names its core
  primitive "Mandate" (held against the D7 gate).
- **Key structural decisions of the plan:** the audit is the GATING
  DEPENDENCY of the whole forward path, opening with an adopt-vs-audit STOP —
  (a) audit as-built / (b) re-derive to a session-scoped identity (O1 shrinks
  toward the tlock reference, O4 collapses per-intent→per-session; the
  boundary-race defeater recorded) / (c) adopt batched-TE, rejected-by-default
  (FLAG P3-A); the platform task 3.3 is a MIGRATION decision (family choice +
  block-sdk disposition + both crypto pins re-validated together) and **gates
  the public testnet 3.8** — the kyber pin (D-066) is itself what holds the
  project on the EOL toolchain, an ACTIVE conflict resolved at 3.3's STOP #1
  (FLAG P3-B); the VK ceremony binds the circuit, so 3.12's fate is fixed at
  3.2's STOP (FLAG P3-G); Chambers honors the spec's design-doc-first
  guardrail with implementation explicitly gated post-review (FLAG P3-C).
- **Rationale.** Mirrors the D-071/D-072 pattern — a completeness-gated
  outline reviewed at a real stop before prose, every known debt in a
  disposition row — extended with the authority-vs-proposal separation so the
  plan cannot be read as "the spec's Phase 3 has 12 tasks": it has one, and
  the plan says so first.

## D-081 — Task 2.5 IBC-line decision (UPDATES D-060/D-077): stay on ibc-go v8.8.0 for real USDC; v8-EOL usage is a recorded POSTURE under P3-B, not new risk; the v0.53 checkpoint re-fails on block-sdk

- **Date:** 2026-07-10 (task 2.5 STOP #1, reviewed in isolation and approved
  before any integration plan or code; baseline verified this session at
  `b2f633b`: lint exit 0, test exit 0, sim-determinism exit 0 — 5 seeds
  byte-identical across two independent processes).
- **SPEC:** §3 (version pins: ibc-go v8.x), §14 D8; PHASE2_DESIGN §2.5
  (dependencies + the "confirm the v8 patch current at task time" clause,
  now superseded in its "v8 still maintained" premise); PHASE3_PLAN §3.3 +
  FLAG P3-B (the platform-migration boundary this decision must not cross);
  METHODOLOGY Rules 2 (two-stop) and 5 (a dependency conflict is a decision).
- **UPDATES D-060/D-077:** D-077's parenthetical "ibc-go v8 is still
  maintained and serves IBC-USDC" was true at 2026-06-26 and is now STALE in
  its first half (D-080 research finding 4). This entry re-derives the IBC
  line for 2.5 from task-time evidence rather than inheriting it.
- **Decision: 2.5 integrates real USDC over IBC on ibc-go v8.8.0 — the pin
  already in the tree. No IBC major, no SDK family bump, no wasmd move. The
  family migration (SDK + IBC + wasmd + block-sdk disposition + both crypto
  pins) remains 3.3's scoped two-stop task, gated before the public testnet.**
- **Empirical spike (Go module proxy, 2026-07-10 — the D-060/D-077 method):**
  | Line | Latest | Tagged | SDK target | Go floor | Signal |
  |---|---|---|---|---|---|
  | ibc-go v8 (ours) | v8.8.0 | 2025-12-18 | v0.50.9 | 1.21 | no tag in ~7 months; no active family |
  | ibc-go v9 | v9.0.2 | 2024-12-20 | v0.50.9 | — | dead 18+ months |
  | ibc-go v10 ≤ v10.1.1 | v10.1.1 | 2025-04-08 | v0.50.12/13 | 1.23.6 | the ONLY v0.50-compatible v10 sub-line — dead 15 months (v10.2.0, 2025-04-30, moved to SDK v0.53.0) |
  | ibc-go v10 current | v10.7.0 | 2026-05-20 | v0.53.4 | 1.23.8 | active |
  | ibc-go v11 | v11.1.0 | 2026-05-20 | v0.54.0 | 1.25.9 | active, newest family |
  | block-sdk/v2 | v2.1.5 | 2024-07-02 | v0.50 only | — | still frozen; no v2.1.6, no /v3 |
  | wasmd v0.55.1 | — | 2025-05-15 | v0.50.12 + ibc-go v10.0.0 | 1.23.6 | the ibc-v10-on-v0.50 wasmd; same dead sub-line |
- **Why (a) and not (b)/(c):**
  - **(a) stay v8.8.0 — CHOSEN.** Real USDC is ICS-20 transfer; the transfer
    app + `ibc/<hash>` voucher denoms need nothing newer than v8, and the
    v8.8.0 core + transfer + capability stack is ALREADY WIRED in app.go with
    wasmd v0.54.9 pinning ibc-go v8 exactly. Zero dependency change, go floor
    1.21 (the D-078 two-pin go-1.23.x ceiling untouched), no dep-driven
    determinism re-baseline.
  - **(b) bump to v10-on-v0.50 (v10.1.1 + wasmd v0.55.1) — REJECTED on the
    merits.** The only v0.50-compatible v10 sub-line is MORE unmaintained
    than v8.8.0 (dead since 2025-04/05 vs a Dec-2025 v8 backport), and the
    move costs a v8→v10 major (capability module removed → app.go churn), a
    forced wasmd bump, and a determinism re-baseline — for zero USDC
    functional gain. Newer-numbered is not better-maintained; the Rule-5
    "never bump a pin in passing" case.
  - **(c) family bump now (SDK v0.53.7 + ibc-go v10.7.0 + wasmd v0.61.x) —
    REJECTED for 2.5.** Both D-077 decisive constraints re-verified today:
    block-sdk still frozen at v2.1.5 with no v0.53 path (bumping = losing/
    rebuilding the §6.7 laned mempool inside a fee task), and the v0.53
    wasmd line's current patches need go ≥1.24 (the D-066 kyber collision;
    a STOP per the D-078 ceiling). This is exactly 3.3's migration; folding
    it into 2.5 would commit the project to a bigger migration than 2.5
    needs, against PHASE3_PLAN's explicit gating.
- **The posture, stated loudly for the future reader/auditor: staying on an
  EOL ibc-go v8 is NOT 2.5 accepting new risk — it is deepening usage of a
  pin the project already carries, under the posture PHASE3_PLAN already
  ratified (FLAG P3-B: the entire v0.50 cluster is off the maintained path;
  a private/invite localnet may run on the current pins; NOTHING PUBLIC may;
  3.3 owns the exit and gates the public testnet on it).** 2.5 adds no new
  CLASS of exposure — the EOL-v8 usage is a recorded posture-decision with a
  named owner (3.3), not drift.
- **The v0.53 checkpoint 2.5 owns (D-077 / PHASE2_DESIGN TODO #5): re-run
  and CLOSED — no bump.** block-sdk is still v2.1.5/frozen, so D-060's
  flip-condition (a) still fails; the sigverify fork stays; the next
  re-evaluation is 3.3's STOP #1.
- **New fact recorded as a 3.3 input:** ibc-go v10.7.0 itself floors at go
  1.23.8 — INSIDE the current toolchain ceiling. On the v0.53 path the
  toolchain collision now comes from wasmd (v0.61.14+ → go 1.24), not from
  ibc-go. Changes nothing for 2.5; feeds 3.3's STOP #1 table.

## D-082 — Task 2.5 BUILD: session TWAP behind §6.2 fee conversion (the `window.go` TODO closes); ibc/<hash> USDC proven through ceilings + price source with the F-2/F-3 boundary PINNED, not moved; determinism vector re-baselined (single cause)

- **Date:** 2026-07-11 (task 2.5 build, after the approved STOP #2 plan;
  builds on D-081's IBC-line decision — no dependency changes of any kind in
  this task, which is what makes the re-baseline single-cause).
- **SPEC:** §6.2 (fee abstraction), §6.4/§6.6 (clearing prices on receipts),
  §11.6 (determinism); PHASE2_DESIGN §2.5 (the contract); AUDIT F-2/F-3 +
  D-020/D-044 (the boundary carried); D-051 (skim), D-058/D-064 (the
  cross-module seam pattern reused); METHODOLOGY Rules 3/6/7/8.
- **What shipped.** (1) `x/session` price index: every settled match's
  `PricePoint`s are folded into a per-pair `TwapRecord` (store `0x0B`,
  length-prefixed pair key) INSIDE the Conclude cache with the receipts —
  observations commit atomically with the settlement that produced them
  (`economics.go` after `writeMatchReceipts`). Pruned to the height window
  on write; TWAP recomputed at write; genesis export/import + validation;
  gRPC `Twap` query (base/quote as query params — denoms contain `/`).
  (2) `x/mandate` seam: `TwapSource` (nil-safe, injected via `SetTwapSource`
  — the SetLiveExposureFn pattern, set before ante capture in app.go);
  `ExecuteFeeAbstraction` converts at the TWAP when the pair is WARM, else
  the fixed `fee_conversion_rate`, whose meaning is now "deterministic
  cold-start fallback" (field unchanged — no mandate proto change).
  (3) ibctesting surface (`app/ibctesting.go` getters) + a two-chain ICS-20
  e2e: real channel, USDC minted on the counterparty, `ibc/<hash>` voucher
  received and driven through face-value pricing, mandate ceilings
  (ErrMandateExceeded binds in the voucher denom), and cold→warm fee
  conversion on the live chain store.
- **Build decisions (the spec did not pin; recorded here, Rule 6):**
  - **Aggregation = per-height mean, then mean across heights** (approved
    plan option B): a block stuffed with many cleared sessions counts as ONE
    height — intra-block spam damping for ~10 lines. Golden Dec outputs
    pinned in tests (`1.333333333333333333`) so a silent rounding change
    fails by name.
  - **Orientation + single rounding:** observations stored as quoted
    (quote-per-base, the PricePoint orientation). The fee path divides once
    — `fee/price → TruncateInt` — rather than pre-inverting, so there is ONE
    rounding and truncation still favors the pool. The event attribute's
    `conversion_rate` (native-per-fee-denom) is informational.
  - **Staleness/re-cold semantics:** the stored TWAP reflects the window as
    of the LAST settlement of the pair; it is served while its newest
    observation is inside the current window; a whole window with no
    settlement → cold → fallback (boundary exact, tested at ±1 block).
    `twap_min_observations` is evaluated against the write-time (pruned)
    set. Consequence: the ante path is a pure O(1) stored-value read —
    no iteration, no Dec math on CheckTx.
  - **Params:** `twap_window_blocks=1200`, `twap_min_observations=10` (dev)
    in **x/session** (the module that stores/prunes/computes); `0` window =
    index disabled (recording no-ops, consumers permanently on fallback) —
    valid but loud. Self-pairs and non-positive prices are never recorded.
  - **NOT the D-044 problem — the containment argument:** the TWAP has
    exactly ONE consumer (the fee seam). It is not offered to ceiling
    valuation; `FaceValuePriceSource` stays wired in x/veil untouched. A
    manipulated/wrong rate mis-prices gas conversion against a genesis-
    funded pool with pool-favoring truncation — it cannot move mandate-
    ceiling value. The D-044/F-3 swap remains Phase-3 task 3.4, which will
    REUSE this index under its own review.
  - **F-2 PINNED (more live now, deliberately unmoved):** real IBC vouchers
    make the unvalued-`MsgTransfer` exposure carry real value. 2.5 does not
    touch the `spendIntent` switch; instead `TestSpendIntentF2BoundaryPin`
    asserts MsgTransfer values at zero BY NAME, so 3.4 flips the boundary
    via a deliberate red, never by drift.
  - **Skim coherence (§2.5 criterion 5):** the D-051 numeraire-only skim is
    denom-generic (`plan.numeraire` end to end); an ibc/<hash>-quoted match
    skims in that denom into the same module account. No TWAP interaction —
    the skim reads flows, the TWAP reads prices. Confirmed by direct read;
    the voucher-quoted path is exercised in the e2e via RecordClearingPrices.
    (Pre-existing quirk, noted not fixed: reputation's whole-USDC scaling
    `economics.go` divides ANY numeraire by 1e6 — correct for 6-decimal
    USDC, inherited for others; 2.6/3.4 surface if it matters.)
  - **The live localnet + relayer USDC demo is the §6 exit demo's scope** —
    decided at the plan stop (not a D-079-style late deviation); the in-repo
    acceptance is the ibctesting e2e above.
- **Determinism vector — RE-BASELINE (Rule 7; the ADR the gate requires):**
  - Run #1 (proto regen + READ-ONLY plumbing; nothing writes): **byte-
    identical** to the b2f633b baseline, all 5 seeds (e.g. seed 1
    `168ED7BF…`, seed 7 `B0B8C6EA…`). DEVIATION NOTE (Rule 8): the plan
    labeled run #1 "regen alone", but the regenerated QueryServer interface
    requires the `Twap` method to compile, so the read-only query surface
    rode along — surfaced here; it writes nothing and the hashes prove it.
  - Run #2 (write path: params defaults on the InitGenesis path + `0x0B`
    observation writes at Conclude + the seam): internal two-pass
    byte-identity PASS, exit 0, NEW baseline hashes —
    seed 1 `245EF17739F97E7E2F27D162A8EBD6A3CB36CEED23B1D287A8AADA323967CC93`,
    seed 2 `B48BDA6D29294A4F12F50FDE835233ACB51E513855D0882E3A7BE21CE2954194`,
    seed 7 `FA562B1DE5CBD34AA925B2A76AB345E44A9860261C33C90F55799C6127B95B76`,
    seed 12 `E491FB48472320B33E07EA63999F355A27B4B93F3C606BC38B5F03C2F80EA3E9`,
    seed 42 `6596CCA106CA4A3616C9F4C6B8E92FF9DC75D362FA83AF95D261D88F77259325`.
    Cause: exactly the feature writes above — no dependency changed in the
    whole task, so the drift is single-cause by construction (the 2.2
    lesson, D-079, applied).
- **Forward item (named owner, Rule 6): the fee-TWAP manipulation surface →
  3.1 audit input** (PHASE3_PLAN §3.1 engagement scope updated this task).
  A winning solver sets clearing prices, so the fee rate is steerable at the
  cost of bonds/skim/gas; windowing + per-height damping + fee-only blast
  radius bound it — an argument, not a proof; the auditor prices it, and an
  outlier-clipping hardening task is created only if the bound fails review.
- **Load-bearing tests (Rule 3):** the seam proof (warm TWAP conversion is
  UNCHANGED when the fixed param is cranked 7x — red if the seam still reads
  the param; cold reverts to the fallback — red if the cold gate is
  bypassed); cold-start min-observations; staleness and prune boundaries
  exact at ±1 block; golden Dec pins; the F-2 boundary pin; genesis
  round-trip; the ibc-denom pair through the index; the two-chain e2e.
  Red-without-the-guard runs recorded in the task record.
