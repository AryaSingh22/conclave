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
  slashed (~~D-024~~ **D-007** incentive — mis-citation corrected at task 2.17,
  D-098; this is the line the wrong number propagated from, and until 2.17 the
  slash it names did not actually land); (2) the **settlement boundary**
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

## D-083 — Task 2.3: x402 gateway surface contract — the node-side surface is COMPLETE; in-repo deliverable = docs/edge/X402_SURFACE.md + app-level conformance + live sweep; the gateway itself is a separate repo

- **Date:** 2026-07-11 (task 2.3; STOP #1 approved the plan and the D-084
  fix as option (a))
- **SPEC:** §9 (edge services), §6.2 (mandate ante + fee abstraction), §6.3
  (MsgSubmitIntent), §6.6 (receipts/proofs); PHASE2_DESIGN §2.3 (FLAG C);
  PHASE3_PLAN §5.2 R4 advisory (x402 Foundation targets spec v1.0 Q3 2026).
- **Surface verdict (the honest answer, per the §2.3 acceptance).** Every
  endpoint SPEC §9 / PHASE2_DESIGN §2.3 name EXISTS at `dccd607`: tx
  submission (`cosmos.tx.v1beta1.Service` + the D-026 unordered extension =
  the replay model), the veil/session/receipt/mandate/sigil/epoch query
  services incl. `Twap` (D-082) and `ReceiptProof` (ICS-23), and the §6.2
  fee abstraction (`fee_denoms` + TWAP conversion, D-082). Task 2.3
  therefore adds NO node gRPC. The in-repo deliverable is the CONTRACT and
  its proof:
  - **`docs/edge/X402_SURFACE.md`** — the binding contract: endpoints and
    shapes (§3/§5), the auth model (§1), the replay model (§3.2), the
    fee/gas model (§4 — gas fronts in uusdc; bonds are uclv and NOT
    fee-abstracted), settlement confirmation (§6), the settlement denom as
    a PARAMETER (`uusdc` literal on dev; `ibc/<hash>` per D-082), and the
    x402 **v1.0-RC version posture**: the node surface depends on no x402
    protocol detail, so pre-1.0 breakage risk is owned entirely by the
    gateway repo (the R4 advisory discharged — PHASE2_DESIGN §2.3 predated
    that finding).
  - **`app/x402_surface_conformance_test.go`** (in `make test`) — the
    load-bearing auth proof from a gateway-shaped fixture (below).
  - **`scripts/x402_surface_check.sh` + `conclave-demo x402-check`** —
    every named endpoint exercised once against a LIVE localnet,
    gateway-shaped (sigil/mandate setup, submit with a real uusdc fee
    through fee abstraction, fallback clearing, Receipts→Receipt→
    ReceiptProof, pre-OPEN cancel, and the live auth negatives incl. the
    D-084 rebind rejection). Dev tooling, not in `make test`.
- **The auth model (the plan-stop crux, confirmed by direct read — Rule 9).**
  Submission authority is **"hold a key + hold a mandate"**; the node
  exposes NO server-side signing and no submit-on-behalf-of RPC. The
  chokepoint chain: SDK signature verification → the single-valued sigil
  reverse index (`ErrSigilMismatch`, `x/veil` SubmitIntent; includes
  mandate-bound delegate keys, D-017(d)) → the §6.2 mandate ante (TypeURL
  allowlist deny-by-default with recursive unwrap; per-tx + window ceilings
  bound to `spend_commitment + bond`, D-065; optimistic CheckTx charge =
  the stolen-key parallel bound; expiry/revocation; no self-escalation) →
  the D-075/D-076 consent gates (a gateway's condition consent is delegated
  consent, bounded by the mandate; it cannot impose conditions on
  counterparties). Deployment models documented: (A) gateway-as-principal
  (the §6 exit-demo shape; x402 payers never touch the chain) and (B)
  per-agent delegation; **one key ↔ one sigil** (single-valued index) ⇒ a
  gateway runs one hot key per principal, and hot keys are never controller
  keys (`ErrGranteeConflict`). Documented non-bounds (§1.3): fee spend is
  NOT window-ceilinged (the `fee_sponsor` caveat);
  payment-before-submission ordering and the x402-side nonce store are
  gateway-repo obligations.
- **Conformance suite (Rule 3/4).** `TestX402SurfaceConformance`: the bound
  key submits for its sigil with the fee in uusdc (pool +fee, collector
  +converted uclv, window charged commitment+bond, then released on
  cancel); an allowlist-less mandate → `ErrMsgNotPermitted` (nothing
  charged); over-ceiling → `ErrMandateExceeded` (nothing charged); an
  unbound key → `ErrSigilMismatch` for every sigil; the bound key claiming
  a foreign sigil → `ErrSigilMismatch`; a cross-sigil rebind →
  `ErrGranteeConflict` with the victim binding intact and functional
  (D-084). Pre-existing guards' red-proofs live in their owning tasks; the
  NEW guard's red-first run is recorded in D-084.
- **Separate repo (unchanged posture, FLAG C).** `conclave-x402-gateway` =
  x402 HTTP facade (402 → X-PAYMENT → verify → X-PAYMENT-RESPONSE carrying
  receipt id + ICS-23 proof), hot wallet (one mandate-bound key per
  principal), replay-nonce store, unordered-tx submitter, settlement
  watcher, gas-fronting reconciliation. Builds against X402_SURFACE.md at a
  pinned commit; its code/tests are NOT in `make test`; its plan-stops
  (hot-wallet model, nonce store, fee reconciliation) belong to that repo.
- **G3 — found in-task, fixed in-task (dev tooling only, surfaced per
  Rule 8): `scripts/demo_e2e.sh` was silently stale, TWICE over.**
  (a) `conclave-demo`'s `buildIntent` predated D-065 and declared no
  `spend_commitment`: the §1.3-step-3 over-ceiling reject could no longer
  fire (an empty commitment values at bond-only) and every coin-give demo
  intent would slash INVALID at the Reveal — stale since task 1.1. Fixed:
  `buildIntent` declares commitment = its coin give legs. (b) The driver's
  gRPC connection used grpc-go's DEFAULT codec, which decodes through
  protobuf-go reflection and PANICS on gogoproto customtype fields
  (`invalid Go type math.LegacyDec`) — first hit live by x402-check's
  `Twap` read; `cycle-and-assert`'s sigil `Reputation` read has the same
  shape. Fixed: the connection forces the SDK's gogo-aware
  `ProtoCodec.GRPCCodec()`. Both breaks lived in an acceptance script that
  nothing re-ran — the same lesson as the epoch-inert vector (D-069):
  an acceptance artifact that is not exercised is not covering anything.
  No consensus code; the demo driver is a gRPC client.
- **Settlement-custody clarification (surfaced live, folded into the
  contract — X402_SURFACE.md §1.2).** Building the live sweep surfaced a
  correct-but-non-obvious model fact: a matched intent's coin **give** legs
  settle from the **sigil's controller address** (`conclude.go`
  resolve→`ControllerAddress`), NOT the submitting gateway key. So a
  mandate-bound gateway fronts only **gas + bond** from its own key; the
  **settlement funds come from the principal's account**. In Model A the
  gateway is the controller (settles from its own float); in Model B the
  delegating agent must hold the coin-give funds, and the gateway's custody
  exposure is bounded to its gas+bond float even though it authors the
  intent. Not a defect — this is the correct agent-owns-the-funds model; it
  is now stated in the contract because it is exactly the "whose funds"
  authority question the surface must answer.
- **Gates (output shown in the task record):** `make lint test` EXIT=0 on
  the full tree; determinism vector run #1 (docs+tests+tooling only, pre-
  D-084): **byte-identical** to the D-082 baseline, all 5 seeds; run #2 in
  isolation after D-084: **byte-identical** again (the sim generates no
  cross-sigil reissue) — no re-baseline; live sweep
  `scripts/x402_surface_check.sh`: **PASS** end to end (setup, the §5 query
  sweep, uusdc gas fronting via §4 fee abstraction with the pool delta
  asserted, pre-OPEN `MsgCancelIntent`, a 2-party swap CONCLUDED via the
  in-consensus fallback, `Session`/`CurrentSession` read-back, `Receipt` +
  ICS-23 `ReceiptProof` as the X-PAYMENT-RESPONSE evidence, and all four
  live auth negatives — foreign-sigil, unbound-key, over-ceiling,
  cross-sigil rebind).

## D-084 — Task 2.3 (G1): IssueMandate REPLACE is SAME-SIGIL only — closing the cross-sigil grantee-rebind hole; UPDATES D-064's REPLACE semantics

- **Date:** 2026-07-11 (task 2.3; the fix approved at STOP #1 as option (a))
- **SPEC:** §6.1 (reverse index invariant), §6.2 (MsgIssueMandate); builds
  on D-010 (single-valued index), D-017 (revoked-record retention), D-059/
  D-064 (outstanding_commitment conservation + REPLACE carry).
- **The hole (found tracing the 2.3 auth surface).** The REPLACE branch
  (D2: one active mandate per key) never compared the existing mandate's
  sigil to the new terms'. ANY sigil controller — sigil registration is
  unprivileged — could issue a mandate to a grantee already bound to a
  DIFFERENT sigil, which (a) **deleted the victim sigil's mandate** without
  its controller's consent (in tension with D-017 retention), (b) **carried
  the victim's `outstanding_commitment` cross-principal** into the
  attacker's mandate — the D-059/D-064 conservation split made violable by
  an unrelated third party, and (c) **overwrote the reverse index**,
  bricking the key's submissions for its real principal
  (`ErrSigilMismatch` on every subsequent submit). This is exactly the key
  class edge gateways run (X402_SURFACE.md §1.1 "binding stability").
  `GranteeResolvesToSigilInvariant` could not catch it: the deletion keeps
  post-attack state invariant-clean.
- **Unconsidered, not intended:** `AmendMandate` already enforces the same
  invariant ("amendment cannot change the sigil"); REPLACE simply never
  inherited it, and `TestIssueMandateReplace` covered only same-controller/
  same-sigil reissue.
- **Decision.** REPLACE requires `existing.SigilId == terms.SigilId`, else
  `ErrGranteeConflict`. The D-064 same-sigil carry and the revoke→reissue
  re-enable path are untouched. Because revoked records are retained
  (D-017) and found by the REPLACE lookup, a used grantee key stays
  **burned to its sigil even after revocation** — cross-sigil key migration
  is deliberately impossible; edge operators use a fresh key per principal
  (X402_SURFACE.md §1.2).
- **Watched red-first (Rule 3):** `TestIssueMandateCrossSigilRebindRejected`
  ran RED against the unguarded code (the rebind SUCCEEDED — "Target error
  should be in err chain … in chain: <empty>", keeper_test.go:292), then
  GREEN with the guard; it also pins the intact victim binding, the
  post-revocation burn, and the surviving same-sigil re-enable.
  `TestX402SurfaceConformance` asserts the rejection at the app level with
  the gateway still functional; `x402-check` asserts it against a live
  node.
- **Determinism (Rule 7):** consensus-affecting (a msg handler gains a
  rejection), so the vector ran in ISOLATION on this change: byte-identical
  to the D-082 baseline (the sim's mandate ops never produce cross-sigil
  reissue) — no re-baseline.

## D-085 — Task 2.3 finding G2: a mandate-bound MsgSubmitIntent that fails in the MSG SERVER stranded its ante charge (an on-demand conservation break) — FOUND, severity-graded, and MITIGATED in-task by ante pre-validation; the STRUCTURAL design fork is task 2.8

- **Date:** 2026-07-11 (found while building the conformance suite);
  **mitigated 2026-07-12** (STOP: severity determination → MITIGATE-NOW
  approved). Rules 3/4/6/8/9.
- **The defect.** The §6.2 ante charges the window optimistically in CheckTx
  AND DeliverTx (D-059). cosmos-sdk v0.50 `baseapp.runTx` commits ante writes
  to the deliver state BEFORE `runMsgs` and discards the msg cache on failure
  with no ante rollback (a refund-on-failure postHandler is structurally
  impossible — its writes land in the same discarded cache; verified in
  `baseapp.go`). Consequence: a mandate-bound `MsgSubmitIntent` that passed
  the ante but failed a veil msg-server precondition (`ErrSigilMismatch`,
  bond `< required`, `ErrInvalidEnvelope`, `ErrEpochNotDecryptable`,
  `ErrInvalidChamber`, unfundable-bond escrow) permanently stranded
  `spend_commitment + bond` in `outstanding_commitment`: no intent record, so
  `DecommitIntent` never fires and rollover/amend/reissue CARRY the balance.
  Each strand breaks the app-level mandate-commitment-conservation invariant
  (grantee with outstanding_commitment, zero live intents) — the D-059/D-064
  accounting backbone of §1.1-goal-3.
- **Severity (graded at the STOP, cites verified — Rule 9).**
  - The invariant IS registered with the LIVE crisis module
    (`app/invariants.go:50` → `app/app.go:525`), not test-only.
  - **NOT a permissionless instant halt:** `MsgVerifyInvariant` is
    permissionless but its broken-invariant `panic(res)` is CAUGHT by
    baseapp's default recovery middleware (`recovery.go` `newDefaultRecovery
    Middleware`) → an `ErrPanic` tx failure, not a halt. The SDK's "chain
    halts here" comment is stale for the DeliverTx path in v0.50.15.
  - **Auto-halt is operator-conditional:** the crisis EndBlock check runs
    only if `--inv-check-period > 0` (`crisis/abci.go:17`; default 0 via
    `crisis/module.go:200`). If set (the prudent-validator config), a broken
    invariant panics in EndBlock (not recovered) → that node halts.
  - **Attacker-reachable ON DEMAND** (the deciding exposure): permissionless
    setup (register sigil, issue a mandate to a fresh grantee), then one
    crafted submit strands the charge — proven by the pin test (bond
    `< required`) and reachable via bogus `sender_sigil` etc. So the GLOBAL
    conservation invariant is corruptible by an attacker using only their own
    mandate, repeatably. Plus a gateway self-DoS: a gateway key whose error
    path ever produces a msg-server-failing submit progressively bricks its
    own window with phantom charges (the exact "contract claims stable
    authority, error path degrades it" class as G1).
  - Disposition: attacker-reachable corruption of THE conservation invariant
    + operator-conditional halt is disqualifying for a silent deferral, even
    without a permissionless instant halt. **Mitigate now.**
- **The mitigation (shipped; the CLEAN FIX, not a stopgap).**
  `x/veil` keeper `ValidateSubmitIntentPreconditions(ctx, msg) → (env, owner,
  err)` is the SINGLE SOURCE OF TRUTH for the payload-independent submit
  preconditions — chamber, envelope, epoch-tag, sender-parse, sender→sigil,
  bond valid/denom/≥required, and **bond-fundable** (new `ErrBondFunds
  Insufficient`, mirroring the escrow via `bankKeeper.SpendableCoin`, added to
  the `BankKeeper` interface). BOTH callers use it: the veil msg server
  (`SubmitIntent` now calls it first — the direct path) AND a new
  `SubmitIntentPrevalidationDecorator` (`x/mandate/ante`) placed BEFORE
  `MandateDecorator` in the ante chain. The decorator walks nested msgs with
  the SAME `nestedAnys`/`max_msg_nesting_depth` the mandate charge uses, so it
  covers every submit the mandate would charge (incl. `MsgExec`-wrapped). Net
  effect: **no ante-passing submit can fail the veil msg server**, so the
  strand vector CLOSES — the hole is closed, not masked. (Gating the
  invariant's halt was REJECTED: it would hide a real D-059/D-064-shape break;
  you never disable the detector.)
- **One-source-of-truth is structural (no divergence).** Because the ante and
  the msg server both call `ValidateSubmitIntentPreconditions`, a new payload-
  independent precondition added to the msg server is added to BOTH callers by
  construction; the function comment states this obligation.
- **Load-bearing proof (Rule 3, watched-red RECORDED).**
  `TestX402FailedSubmitDoesNotStrand_G2Closed` drives each failure mode
  through the full app (ante+msg) and asserts rejection + `outstanding_
  commitment` ZERO + the conservation invariant GREEN, and that a VALID submit
  still lands AND still charges. Watched-red: commenting out the decorator in
  `app/ante.go` flips it RED at `x402_surface_conformance_test.go:421`
  ("foreign sigil: … charges nothing in the ante" — the strand returns);
  restored to green. `x/veil/keeper` `TestValidateSubmitIntentPreconditions`
  covers every mode one-for-one incl. the unfundable-bond
  `ErrBondFundsInsufficient` (only provable where the mock balance is
  controllable).
- **Determinism (Rule 7).** Consensus-affecting (a new ante decorator changes
  what state a would-fail submit leaves — a strand → nothing). Scoped to
  mandate-charged txs only (root-key submits skip prevalidation — a lane-
  routing test surfaced this and it is the correct scope: only a charged
  submit can strand), so the sim's behavior is unchanged by construction (the
  vector is green ⇒ the sim generates no would-fail mandate-bound submit).
  Vector run in ISOLATION on the mitigation: **byte-identical** to the D-082
  baseline, all 5 seeds (seed 1 `245EF177…`, seed 7 `FA562B1D…`,
  seed 12 `E491FB48…`, seed 2 `B48BDA6D…`, seed 42 `6596CCA1…`), two
  independent processes — no re-baseline.
- **Owner of the STRUCTURAL fork: task 2.8** (Phase-2 ledger). 2.8 is no
  longer the SECURITY fix (shipped here); it owns the design QUESTION —
  ante-prevalidation-as-permanent-design vs a v0.50 mode-split charging
  (CheckTx-only ante charge; DeliverTx charge inside the msg server,
  revisiting D-059's "zero mode branching") — plus the timeouts→params
  promotion. A plan-stop, off the exit path.

## D-086 — Task 2.4: 7683 resolver surface contract — translation authority is the resolver's own (no on-chain order binding, by design); fallback clearings now PERSIST their winning Solution (the terms_hash pre-image was unfetchable for the no-solver default — found by the new conformance test, fixed as a stop-approved mid-build deviation)

- **Date:** 2026-07-13 (task 2.4; STOP #1 approved the plan + decisions ①
  filename `7683_SURFACE.md` per FLAG C, ② D-087 disposition (b), ③ repo
  split; a mid-build DEVIATION STOP approved the fallback-Solution fix)
- **SPEC:** §9 (edge services), §6.3 (MsgSubmitIntent), §6.4 (sessions/
  fallback), §6.6 (receipts/proofs); PHASE2_DESIGN §2.4 (FLAG C);
  PHASE3_PLAN §5.2 R4 (ERC-7683 production-dominant — Across 88%, OIF 30+
  teams; OIF = the resolver repo's reference frame).
- **Surface verdict (the honest answer).** Everything 2.3 verified carries
  over unchanged (tx service + D-026 unordered ext; veil/session/receipt/
  mandate/sigil/epoch queries; §6.2 fee abstraction; `ReceiptProof`
  ICS-23, D-053). 2.4's ADDITIONAL requirement — the `terms_hash`
  PRE-IMAGE chain `Session → winning_solution_id → Solutions → Match` —
  was TRUE for solver-cleared sessions and **FALSE for fallback-cleared
  sessions** (the no-solver default: every localnet settlement and 2.3's
  live swap). With that one gap closed (below), the node surface is
  COMPLETE for the resolver; **no new gRPC**.
- **The DEVIATION (Rules 2.4/8) and its fix.** The approved plan said "no
  node changes; vector byte-identical expected." The conformance test then
  walked the FALLBACK path and the plan's own §2 claim broke:
  `runFallback` applied `buildFallbackMatches` output via
  `concludeInner(…, nil)` WITHOUT persisting it, and only `concludeWinner`
  ever set `session.winning_solution_id` — so a fallback settlement's
  receipts committed (terms_hash) to Match bytes NOTHING could fetch.
  Surfaced at discovery, STOPPED, option (A) approved: `runFallback`'s
  success branch now stores a synthetic
  `Solution{id: deriveID(0x12‖chamber‖u64be(session_id)), solver: "",
  score: 0, matches}` and sets `winning_solution_id` before CLOSED
  (x/session endblock.go; `domainSolution=0x12`, keeper.go).
  - **Uniformity is the acceptance property** (review requirement): the
    stored matches are the SAME values the receipts hashed, so the QUERIED
    pre-image recomputes to the receipt's terms_hash byte-for-byte — the
    conformance test asserts exactly this on FALLBACK settlements.
  - **Id discipline:** session-derived ⇒ consensus-deterministic and
    reproducible cross-node (§11.6: no clock, no divergent counter);
    domain-separated from tx-hash-derived solver ids (collision = sha256
    collision); one fallback per session (FALLBACK is entered once and the
    session closes in the same call) ⇒ unique. A conclude_failed session
    holds both its solver solutions AND the fallback record;
    `winning_solution_id` names what actually settled.
  - **Rejected:** event-carried pre-image (indexer-dependent, not
    state-provable); reconstruct-by-reimplementation (couples the evidence
    chain to matcher internals task 2.6 is scheduled to change);
    solver-cleared-only rail (fails the common case).
- **Translation authority (the plan-stop crux, by direct read).** A 7683
  order becomes a SealedIntent under the RESOLVER'S OWN key+mandate —
  X402_SURFACE §1.1's chokepoint chain, unchanged. NOTHING binds an
  intent to an origin-chain order: a compromised resolver can mint
  order-less intents, bounded by its mandate (allowlist, per-tx + window
  ceilings, expiry, revocation) and its controller-account funds; it
  cannot forge other sigils' intents. The conformance suite PINS the
  non-binding honestly (an order-less fill settles; if that assert ever
  reds, §1.2 of the contract must be rewritten). **An `origin_order_id`
  field was considered and REJECTED as decoration (Rule 4):** it would be
  resolver-attached and the chain cannot verify EVM order authenticity,
  so the bridge must verify the settled legs against the order regardless
  — the field would add nothing an attacker respects. Future proposers of
  that field: this line is the answer.
- **`spend_commitment` under-declaration (D-065, resolver-shaped).** A
  resolver CAN under-declare (the ante never reads give legs — uniform)
  and an under-declared fill CANNOT SETTLE: Reveal cross-check → INVALID
  before matching, 50% bond slash, **window charge retained**.
  `TestERC7683UnderDeclaredFillNeverSettles` proves the full arc through
  real blocks (ante passes → INVALID → no receipt → outstanding_commitment
  keeps commitment+bond). Boundary carried honestly: coin legs only (F-2,
  pinned elsewhere by `TestSpendIntentF2BoundaryPin`).
- **Fill-back evidence semantics (contract §6).** `ReceiptProof` proves
  the exact Receipt bytes under `0x08|id` against Conclave's app hash at
  `ProofHeight` (D-053). Empirically pinned in-test: a tip query proves at
  the last HEADER-ANCHORED version (`ProofHeight = LastCommitID.Version −
  1`); the verifying root is the app hash AT ProofHeight = the `AppHash`
  of header ProofHeight+1 (what a light client supplies); a receipt
  written at H is provable once header H+1 exists. What the proof does
  NOT prove is stated with a MANDATED bridge check each: root authenticity
  (per-origin Conclave light client), terms authenticity (recompute
  terms_hash from the queried Match), **order↔fill correspondence (the
  leg check — the binding lives bridge-side)**, first-claim uniqueness
  (receipt-id dedup). The conformance suite demonstrates each gap
  PERMANENTLY via skip-variant acceptance (the forged/replayed claim is
  ACCEPTED with the check skipped — in-tree reds for the "does not prove"
  claims).
- **Deliverables.** `docs/edge/7683_SURFACE.md` (FLAG C artifact);
  `app/erc7683_surface_conformance_test.go` (in `make test`; **watched RED
  against pre-fix HEAD** for the fallback pre-image gap — the red was
  banked before the D-086 node change existed);
  `scripts/7683_surface_check.sh` + `conclave-demo 7683-check` (live
  sweep, dev tooling); X402_SURFACE.md D-087 disclosures (Rule 8
  propagation); the separate repo `conclave-7683-resolver` scoped in the
  contract (order ingestion/translation/bridge = its plan-stops,
  PHASE2_DESIGN §2.4).
- **Gates (Rules 3/7/8 — output shown in the task record).**
  - `make lint test` EXIT=0 on the full tree at close.
  - Determinism vector run in **ISOLATION** on the fallback-Solution change
    (the only consensus change of the task): **byte-identical** to the
    D-082 baseline, all 5 seeds (seed 1 `245EF177…`, seed 2 `B48BDA6D…`,
    seed 7 `FA562B1D…`, seed 12 `E491FB48…`, seed 42 `6596CCA1…`), two
    independent processes — **no re-baseline**. Honest coverage note
    (D-069 class): the sim generates no mutually-fillable pair, so the
    vector never EXERCISES the new fallback-Solution write; the covering
    gate is the conformance test, which was watched RED against pre-fix
    HEAD.
  - **Watched-red, D-065 cross-check (Rule 3):** with `exceeds = false`
    forced after `giveExceedsCommitment` in `x/veil/keeper/lifecycle.go`,
    `TestERC7683UnderDeclaredFillNeverSettles` goes RED — the
    under-declared fill reaches **CONCLUDED (4)** instead of INVALID (7)
    ("did not reach INTENT_STATE_INVALID within 10 blocks"): it settles
    past its declaration, the exact escape the guard kills. Bypass
    removed; `git diff` on lifecycle.go empty (byte-identical restore);
    suite green.
  - **Skip-variant reds are PERMANENT in-tree:** the wrong-settlement
    claim is ACCEPTED with the leg check skipped, and the cross-order
    replay is ACCEPTED with dedup skipped — each "does not prove" claim
    stays demonstrated on every `make test` run, not just in a one-time
    watch.
  - **Live sweep `scripts/7683_surface_check.sh`: PASS end to end** —
    fill CONCLUDED via the fallback on a real node; the D-086
    `winning_solution_id` non-empty LIVE; the evidence bundle verified
    against the header-anchored `AppHash` fetched from
    `GetBlockByHeight(ProofHeight+1)` (the actual light-client flow);
    §6.3 checks 1–4; live negatives (under-declared → INVALID at the
    Reveal with the ante passing; over-ceiling `ErrMandateExceeded` in
    CheckTx; foreign-sigil `ErrSigilMismatch` in CheckTx via the D-085
    prevalidation; unbound-key `ErrSigilMismatch` in DeliverTx — the
    prevalidation is correctly scoped to mandate-bound signers). The
    sweep's first run also re-derived §6.2 empirically: gathering
    evidence in the settlement block returns a NONEXISTENCE proof
    (receipt at H, tip proof at H−1) — the driver now advances one block
    first, and the contract documents the rule.

## D-087 — Task 2.4 finding G1(2.4): `Receipt.condition_outcome` is decorative — always `NONE`, never updated on escrow resolution; disposition (b) defer-with-teeth (contract discloses + mandates the bridge-side leg-condition check); the fix is NEW task 2.9

- **Date:** 2026-07-13 (task 2.4; disposition approved at STOP #1)
- **SPEC:** §6.6 (Receipt.condition_outcome), §6.5 (conditions); D-073
  (escrow handoff), D-053 (the proof surface that makes the field
  externally consumable).
- **Finding (Rule 4: existence ≠ enforcement).** The receipt proto
  documents `RELEASED/REFUNDED/DISPUTED` as escrow outcomes and `NONE` as
  "the settlement carried no delivery condition" — but the ONLY write site
  in the tree (`x/session/keeper/receipt.go` writeMatchReceipts) stamps
  `CONDITION_OUTCOME_NONE` unconditionally, INCLUDING for matches whose
  legs carry HASHLOCK/OPTIMISTIC/ZK conditions (real settlements since
  2.1), and nothing ever updates a receipt when its escrow resolves
  (repo-wide grep: zero non-generated writers of any other value). A
  conditioned settlement's ICS-23-provable receipt therefore asserts, to
  external verifiers §6.6 explicitly invites, that it carried no
  condition — and receipt-existence gets read as delivery-completion when
  the funds may still REFUND from escrow.
- **Severity + the G2 distinction (why defer is right here and fix-now was
  right there).** No on-chain funds-at-risk and NO current consumer (the
  origin bridge this contract defines is what WOULD have consumed it) —
  an external-verifier integrity defect. D-085's G2 was an
  attacker-reachable consensus-invariant corruption (a live vector →
  disqualifying → fixed in-task). Disposition (b) does not ship the
  falsehood; it NEUTRALIZES it: the contract (7683_SURFACE §6.4)
  discloses the true semantics AND MANDATES the leg-condition check
  (§6.3 check 3 — accept only unconditioned coin legs, verified from the
  terms pre-image the bridge already checks), which makes the v1
  coin-fill rail safe BY CONSTRUCTION with zero node changes. Disclosure
  is deliberately redundant (three places in 7683_SURFACE: the §6.1
  schema warning, §6.3 check 3, §6.4; plus X402_SURFACE §5 receipt row +
  §6 conditioned-purchases caveat — the x402 exit-demo ZK buy hits the
  same semantics).
- **The fix is task 2.9 (new Phase-2 ledger row, owner + acceptance
  recorded there):** additive `CONDITION_OUTCOME_PENDING` enum value +
  faithful stamp at CONCLUDE (NONE only when truly unconditioned) +
  escrow-resolution write-back to the receipt record + the
  RELEASED/REFUNDED/DISPUTED transitions; proto + cross-module
  consensus-path work ⇒ its own session, determinism vector re-baseline
  expected, Rule-3 watched-red on the new write-back. Option (a) fix-now
  was REJECTED for 2.4 scope discipline (Rule 7 isolation).

## D-088 — Task 2.11: store-iterator range-end truncation (`prefix‖0xFF`) in x/mandate and x/sigil — 1-in-256 records invisible; surfaced by 2.6's "flake" item and split out as its own task BEFORE the matcher work

- **Date:** 2026-07-28 (task 2.11; split out of 2.6 at a STOP and landed BEFORE
  the matcher enrichments — Rule 1's own 1.0/1.0b precedent: when a gate
  surfaces a real pre-existing bug, the bug becomes its OWN task, not a rider)
- **SPEC:** §5.3 (store key layout), §6.1 (sigil/credential/reputation),
  §6.2 (mandate), §6.4 rule 5 (counterparty-class predicate), §11.6
- **Discovery — and why the prescribed fix would have HIDDEN the bug.**
  PHASE2_DESIGN §2.6 (Rev 2) carried a flake-hardening item: `TestIssueMandate
  Replace` "flaked once" on a `randAddr()`-seeded fixture, and the recorded
  disposition was *"fix = deterministic fixture seeding."* The flake was
  REPRODUCED BEFORE being fixed: `-count=2000` → **8 failures (0.40%)**, always
  `MandatesBySigil` returning 0 mandates where 1 exists. **0.40% ≈ 1/256** — the
  signature of a one-byte range boundary, not a fixture problem. Making the
  fixtures deterministic first would have pinned the sampler to a passing draw
  and shipped the defect permanently green. **The flakiness WAS a correctness
  oracle running at a low duty cycle** — that is the generalizable lesson, and
  it is why the ordering below is load-bearing.
- **The defect.** Eight iterators built their range end as
  `store.Iterator(prefix, append(prefix, 0xFF))`. A range end must be an
  EXCLUSIVE upper bound over every key sharing `prefix`; `prefix‖0xFF` is
  itself only a 1-byte extension, so any longer key beginning `prefix‖0xFF`
  sorts AFTER it and is silently excluded. Every record whose first
  post-prefix byte is `0xFF` — uniformly 1 in 256 — was invisible to the walk.
  No error, no log: a short read that looks exactly like "no such record."
- **The fix (Rule 5 — adopt, do not author).** The range end is now
  `storetypes.PrefixEndBytes` from `cosmossdk.io/store@v1.1.1/types/utils.go`,
  the SDK's own already-adopted helper (it increments the last non-`0xFF` byte
  and truncates). `x/session` already had a correct local `PrefixEnd`
  (`x/session/types/keys.go:226`); `x/mandate` and `x/sigil` never got it. No
  new authored surface. `x/session`'s local copy is deliberately left untouched
  in this task so 2.6's matcher runs stay single-cause (consolidation noted as
  a cosmetic follow-up, not a defect).
- **The eight sites, classified honestly — 2 LIVE, 6 LATENT.** Which sites
  actually dropped records depends on what byte FOLLOWS the prefix:

  | Site | Post-prefix byte | Status |
  |---|---|---|
  | `MandatesBySigil` (`x/mandate/keeper/mandate.go`) | `mandate_id[0]` (high-entropy) | **LIVE-BROKEN** |
  | `IterateCredentials` (`x/sigil/keeper/credential.go`) | `cred_id[0]` (high-entropy) | **LIVE-BROKEN** |
  | `IterateMandates`, `IterateGranteeIndex`, `IterateAllCredentials`, `IterateReputations`, `IterateSigils`, `IterateAddressIndex` | `chamber_id` high byte = `0x00` | **LATENT** |

  The six latent sites were correct **only by the accident** that `chamber_id`
  is `0x00000000` until Phase 3. **Phase-3 Chambers re-arms every one of them.**
  All eight are fixed uniformly so no site depends on that accident, and
  `TestIterateMandatesFindsHighByteChamber` pins the latent case by asserting
  a `0xFF`-high-byte chamber is walked.
- **CORRECTION of this task's own first severity report (Rule 8 — the
  correction stays VISIBLE).** The discovery message claimed three
  consequences that are **WRONG** and are retracted here:
  1. ~~"a grindable UNREVOCABLE mandate"~~ — `RevokeMandate` → `MandateByID` →
     `IterateMandates` uses the BARE-prefix walk, whose next byte is the zero
     chamber id. Nothing was dropped; no mandate was ever unrevocable.
  2. ~~"genesis export silently drops ~1/256 of mandates/sigils/credentials/
     reputations"~~ — every genesis-export iterator is a bare-prefix walk.
     Not firing.
  3. ~~"the D-059/D-064 conservation invariant is computed over an incomplete
     set; spurious fire → D-085-shaped operator-conditional halt"~~ — same
     reason. Not firing.
  The error was **reasoning from the shared code pattern without reading each
  key layout**, then extrapolating severity from the one reproduced site. The
  layouts were subsequently read directly (Rule 9) and the extrapolation did
  not survive. Recorded because the process failure is the reusable lesson: a
  systemic-looking pattern still requires a per-site read before its blast
  radius is asserted.
- **Severity as it ACTUALLY stands.**
  - `MandatesBySigil` → the gRPC `Mandates(sigil_id)` query omitted 1/256 of a
    sigil's mandates. **Query-surface only** (sole caller: `grpc_query.go`).
  - `IterateCredentials` → **CONSENSUS path.** It backs
    `HasActiveCredentialSchema`, which backs `predEnv.HasCredential`, which
    backs the SPEC §6.4 **rule-5 counterparty-class predicate**
    `has_credential(schema)`. A credential the walk cannot see is a credential
    the matcher denies: a genuinely credentialed party is wrongly rejected and
    an otherwise-valid match fails. Every node computes the SAME wrong answer
    (the walk is deterministic), so there is **no fork and no halt** — this is
    a silent correctness/liveness loss, NOT a safety break and NOT value
    corruption.
  - **Grindability is self-harm, not an attack.** `cred_id =
    deriveID(0x02, chamber, issuer‖subject‖schema‖claim_hash, seq)`. An issuer
    picks `claim_hash` and can retry (~256 attempts, each a tx) to land a
    `0xFF`-prefixed id — but the only reachable effect is making its OWN
    credential invisible. A third party cannot grind a VICTIM's credential id:
    the victim's issuer controls `claim_hash` and the global `seq` is not
    attacker-controlled. No victim-targeting path exists.
- **Watched-red evidence (Rule 3 — the red was SEEN, not assumed).** Four new
  tests in `x/mandate/keeper/prefixend_test.go` and
  `x/sigil/keeper/prefixend_test.go`. With the four source files stashed
  (`git stash push`), ALL FOUR fail against pre-fix HEAD:
  - `TestMandatesBySigilFindsHighByteMandateID` — "mandate with id 0xFF… is
    missing from the 0x03 index walk"
  - `TestIterateMandatesFindsHighByteChamber` — expected 1, actual 0
  - `TestHasActiveCredentialSchemaFindsHighByteCredID` — the rule-5 predicate
    wrongly denies a credentialed party
  - `TestIterateCredentialsFindsHighByteCredID` — expected 2, actual 1
  Fix restored → all green. Each test carries a low-byte CONTROL so it proves
  the walk truncates SELECTIVELY rather than being broken outright. The
  adversarial key is CONSTRUCTED deterministically (an id literal beginning
  `ff`), never ground for — grinding would have made the regression test itself
  probabilistic, the very defect this task exists to remove.
- **Ordering is part of the decision (do not reorder on a future re-read).**
  (1) fix the eight sites → (2) forced-`0xFF` watched-red regression → (3)
  ONLY THEN `randAddr()` determinism. Step 3 before step 1 silences the oracle.
  `randAddr()` now derives addresses from a package counter (`fixtureAddrSeq`,
  `encoding/binary`) and draws no randomness; the name is kept so call sites
  read unchanged, with the reason in a doc comment at both definitions. These
  keeper tests never verify signatures, so a derived address suffices.
  Post-fix: `TestIssueMandateReplace -count=2000` → **0 failures** (was 8).
- **Determinism vector: BYTE-IDENTICAL, no re-baseline.** Run in ISOLATION
  (Rule 7) with this fix as the only cause; all 5 seeds match the D-082
  baseline exactly, two independent processes:
  `seed=1 245EF177…67CC93 · seed=2 B48BDA6D…954194 · seed=7 FA562B1D…B95B76 ·
  seed=12 E491FB48…0EA3E9 · seed=42 6596CCA1…259325`.
  **Honest reading of that green (the D-069/D-086 coverage-shadow shape):**
  byte-identical does NOT mean the fix is behaviour-neutral — it means these
  five seeds never populated a `0xFF`-prefixed id (1/256 per record, and the
  sim's credential/mandate population is small). **The vector does not reach
  this path; the four watched-red tests are the covering gate, not the vector.**
- **Consequences.** No proto, no state-machine, no migration: the fix widens a
  read range that was wrongly narrow, so previously-written records become
  visible again — there is no stored state to migrate and no upgrade handler.
  Any operator who queried `Mandates(sigil_id)` before this fix may have seen a
  short list; re-querying now returns the complete set.

## D-089 — Task 2.6: matcher-richness hardening — §5.1 limit RATE honored (closes D-049), partial-fill + alternate-anchor cycles, 2-party partial-fill fallback; all three strictly ADDITIVE, vector byte-identical three times over

- **Date:** 2026-07-28 (Phase-2 task 2.6; the plan-stop was approved before the
  session; 2.11/D-088 had already been split out and landed first)
- **SPEC:** §5.1 (`IntentLeg.limit`), §6.4 rules 3/4 + the in-consensus fallback,
  §11.6 (determinism); PHASE2_DESIGN §2.6.
- **CLOSES D-049** (the §5.1 `limit` rate deliberately unhonored in Phase 0) and
  the three `CONCLAVE-TODO(phase-2)` markers at `solution.go:349`,
  `cycles.go:89`, `fallback.go:35`. **Constrained by** D-048 (the three
  per-asset-class invariants), D-047/D-055 (exact price representability, single
  source of truth), D-065 (uniform ante valuation), D-074/D-075/D-076 (bilateral
  + signed consent), D-086 (fallback-Solution pre-image uniformity), D-082 (the
  determinism baseline).

### The organising principle: every enrichment is STRICTLY ADDITIVE

Each of the three items can only ever ADD a rejection or ADD a candidate that
the pre-2.6 code would have dropped. None of them can change the outcome of a
match that cleared before. That is not an observation about the test data — it
is a property of how each item is wired, and it is why the determinism vector
came back byte-identical to the D-082 baseline **three separate times**, once
per item, each run in ISOLATION (Rule 7):

| item | wiring that makes it additive |
|---|---|
| 8 — limit rate | the clause returns before touching anything unless a leg carries a non-empty `limit`; the quantity guards above it are unchanged lines |
| 7 — cycles | the alternate anchorings are tried AFTER the pre-2.6 default anchor, and the partial-fill provider rule only ADMITS pairs the exact-fill rule rejected |
| 6a — fallback | the FULL-GIVE candidate is built and validated FIRST; the floor-fill candidate is reached only when the full-give one fails |

### Item 8 — §5.1 limit-rate honoring (`x/session/keeper/solution.go`)

- **The gap.** `IntentLeg.limit` was SIGNED by the agent (it rides inside the
  `IntentPayload` ciphertext) and read by NOBODY — the Rule-4 disease in its
  purest form: an agent could believe it had bounded its exchange rate while the
  matcher bounded only its quantities. A repo-wide grep re-confirmed at build
  time what the plan-stop found: **zero writers** of the field anywhere (the only
  `Limit:` hits are `query.PageRequest.Limit`), including fixtures, the demo
  driver, the conformance suites and the sim's intent generator.
- **The rule.** `checkLimitRate`, a SEPARATE clause called last from
  `checkLimits` (the quantity checks above it are byte-identical to pre-2.6):
  - a GIVE leg of asset X bounds `given_X / recv_Y ≤ limit` (max-give);
  - a WANT leg of asset Y bounds `recv_Y / given_X ≥ limit` (min-want);
  where Y (resp. X) is the intent's single counter-side asset.
- **Computed FROM FLOWS ALONE — and why that IS §6.4's "at the stated clearing
  prices".** No external price source is consulted (the D-044/F-3 ante
  price-source swap stays task 3.4 per D-081/D-082), and the solver-stated
  clearing prices are not consulted either. They do not need to be: because
  `CheckPriceBinding` forces every party's price-valued net to EXACTLY zero
  (`p_X·given_X == p_Y·recv_Y`), a two-asset party's flow ratio and its
  stated-price ratio are the SAME NUMBER. Reading the flows therefore honors the
  spec's wording without letting a solver's choice of prices define the bound a
  party signed — a strictly stronger position than pricing the check.
- **Exact-or-reject, by cross-multiplication.** The comparison is
  `given ⋛ limit×recv`, never `LegacyDec.Quo`, which rounds at 18 places and
  could admit a rate the party never signed. A zero denominator needs no special
  case: on a give leg the bound becomes 0, so giving something for nothing
  breaches; on a want leg it becomes ≥ 0, trivially met (pinned by
  `TestRateWithinBound_ZeroDenominator`).
- **Overflow fails CLOSED, and this one is NEW.** `LegacyDec.MulInt` PANICS
  ("Int overflow") once a product leaves ±2^256·10^18, and the fallback matcher
  runs `checkLimits` inside EndBlock, where §11.6 forbids a panic on user input.
  The existing price×flow products are bounded by construction (the fallback
  builds prices that cancel against its own amounts); **nothing bounds the
  magnitude of an agent-signed limit**, so `rateWithinBound` carries a
  `recover()` that fails closed — the same Decision-A posture
  `counterpartySatisfied` uses, and deterministic because the operands are pure
  signed-payload and match data.
- **RECORDED LIMITATION — the shape whose rate is not computable from flows
  alone (surfaced, not routed around).** A give leg's rate is undefined when the
  intent wants more than ONE distinct asset (and symmetrically for a want leg):
  there is no single counter-asset to divide by, and valuing the basket would
  need exactly the price lens this check refuses. Such a match is **REJECTED**
  (`ErrLimitRateUnsupported`, session `1428`) rather than settled against an
  unhonored limit. The task's hard constraint was "do not reach for the D-044
  price source" — the resolution needs no price source at all: an unevaluable
  rate is a rejection, so the signed field is honored in every shape, either by
  enforcement or by refusal. Malformed and negative limits fail the same way.
  This mirrors D-049's own discipline (a deliberate, greppable, recorded matcher
  limitation) and D-074's (`ErrConditionUnsupportedMatch`); it is liftable when a
  per-leg counter-asset exists — D-075-FW-2's per-leg field (task 2.7) is the
  natural home.
- **Conservation analysis (which D-048 invariant could break).** None can: the
  clause is a pure read of already-computed `NetFlows` and returns only an error.
  It cannot add, remove, or re-amount a leg, so coin value-conservation, svc
  authorized-mint and data authorized-creation are all untouched. Its only
  effect is to make a previously-valid solution INVALID.
- **Solver-side drift: none, by scope.** `solver/gate` deliberately covers rule 2
  and the binding half of rules 3/4 only — rule 3's limit satisfaction was never
  in the gate (nor is counterparty class), so adding the rate half creates no
  new divergence between chain and solver. A solver that ignores a signed limit
  builds a solution the chain rejects and pays the 5% invalid-solution slash,
  exactly as it does today for the quantity half.

### Item 7 — partial-fill cycles + alternate numeraire anchoring (`solver/cycles`)

Off-chain only. Every proposal is still re-validated on-chain by
`validateSolution` / `CheckConservation` / `CheckPriceBinding`, so the blast
radius is bounded to *which* matches get proposed.

- **Alternate anchoring.** `Find` now returns `[][]Match` — one GROUP per cycle,
  holding the same cycle priced under each of its coin give-assets, in sorted
  anchor order, the FIRST being the pre-2.6 default. Anchoring changes which
  clearing prices are exactly representable, so a cycle the default anchor cannot
  price may still clear under another. **`cycles` does not decide which** — that
  would have re-implemented the D-055 rule; `Solve` admits the first alternative
  the CHAIN gate accepts and skips the rest, so the arbiter is unchanged and the
  package keeps its documented purity (no `x/session/keeper` import).
- **Partial fills.** A provider fills a consumer exactly, or with a remainder it
  keeps when its own intent signed `allow_partial` (`node.canFill`); the
  settlement leg carries the FILLED amount via the new
  `match.LegToSettleAmount`. A DataGrant is exact-fill only — its conservation
  amount is the indivisible bookkeeping 1 (D-048). Price propagation generalises
  to `price[G_{k+1}] = price[G_k] × delivered_k / wantAmt_k`, which reduces to
  the pre-2.6 formula on exact fills.
- **Conservation analysis.** Each asset still appears on exactly one directed
  leg per cycle, so the graph nets zero per asset by construction; the remainder
  is simply never moved. The generalised price formula is exactly the
  equal-value condition `CheckPriceBinding` enforces, so a mis-derived price
  cannot settle — it is rejected, not absorbed.
- **Honest scope note.** `gate.Accept` does NOT cover rule 3, so `canFill`'s
  `allow_partial` condition is the SOLVER's protection against proposing a
  slashable solution — the party's fill-or-kill GUARANTEE is enforced on-chain by
  rule 3, proven load-bearing under item 6a below.

### Item 6a — 2-party partial-fill fallback (`x/session/keeper/fallback.go`)

- **Two candidates, fixed order.** `tryFallbackPair` now builds a deterministic
  candidate list and returns the first that passes the full `validateSolution`:
  1. **FULL-GIVE** — the pre-2.6 degraded terms, tried FIRST, so every pair that
     cleared before 2.6 clears at byte-identical terms;
  2. **FLOOR-FILL** — a party that signed `allow_partial` hands over only what
     its counterparty's want-floor needs and KEEPS THE REMAINDER.
  The second candidate earns its keep because the two terms differ in PRICE and
  in RATE: a pair whose full-give ratio is not exactly representable (D-047 — the
  matcher never rounds), or whose full-give rate breaches a signed §5.1 limit
  (item 8), can still clear exactly at the floor terms. The acceptance fixture is
  exactly that pair (7 uaaa against 3 uusdc prices uusdc at 7/3 and is dropped;
  the floor fill of 6 against 3 prices it at 2 and clears).
- **In-consensus cycle-aware fallback is NOT in this item** — it is task 2.10
  (see below). 6a is 2-party only, as the approved scope cut requires.
- **Conservation analysis — D-048 invariant (i), and it is the load-bearing
  one.** A partially-filled coin leg moves exactly the filled amount from giver
  to receiver through the module account, so the denom nets zero and supply is
  unchanged; the remainder is not a leg at all and never leaves the giver's
  wallet. Mis-attributing it is structurally unsettleable: emitting the full
  offer while pricing the partial makes the party's price-valued residual
  non-zero, so `CheckPriceBinding` rejects the graph (watched red S2 below), and
  emitting mismatched leg amounts breaks `CheckConservation`. **Double-counting**
  is prevented on the other side: `applyMatches` marks a partially-filled intent
  CONCLUDED like any other, so its remainder is never re-offered into a later
  session (watched red S3 below).
- **D-086 pre-image uniformity, extended.** A partial fill produces a Match whose
  leg amount is NOT the intent's offered quantity — a genuinely new pre-image
  shape the 2.4 conformance test (full fills only) does not cover.
  `TestFallbackPartialFill_PreImageUniformity` asserts the persisted fallback
  Solution carries the FILLED amount and that
  `sha256(Marshal(queried Match)) == receipt.terms_hash` for BOTH parties, so the
  `Session → winning_solution_id → Solutions → Match` evidence chain stays
  uniform across full and partial fills.
- **Consent/bilateral gates unaffected.** A conditioned leg is a DataGrant
  (D-074), and a DataGrant is exact-fill only in both new matchers, so no partial
  fill can arise on a conditioned leg — the D-074 bilateral shape and the
  D-075/D-076 signed-consent chokepoints are reached exactly as before (escrow +
  consent suites green).

### Watched-red evidence (Rule 3 — every red was SEEN)

- **Item 8**, `return checkLimitRate(...)` → `return nil`: 4 tests RED —
  `GiveRateBreachRejected`, `WantRateBreachRejected`,
  `ProposeSolutionRejectsAndSlashes`, and — the important one —
  `FallbackDeclinesRateViolatingPair`, which reports the intent reaching state
  **4 (CONCLUDED)**: without the clause the IN-CONSENSUS matcher settles the
  party against a rate it never signed. Restored → green.
- **Item 7**, `allow_partial` removed from `node.canFill`:
  `TestCyclesPartialFillRequiresAllowPartial` RED — the finder splits a
  fill-or-kill offer. Restored → green.
- **Item 6a, S1** (floor-fill candidate removed from `fallbackTerms`): 3 tests
  RED — the enrichment is what those tests measure, not a pre-existing path.
- **Item 6a, S2** (`intentLegToSettle` ignores the partial amount, i.e. the
  remainder is mis-attributed): 3 tests RED — a settlement that debits more than
  it credits cannot clear its own prices.
- **Item 6a, S3** (`ConcludeIntent` skipped, i.e. the filled intent stays
  re-offerable): `RemainderConservation` RED at its no-double-count assertion.
- **Item 6a, fill-or-kill — a TWO-STAGE red that corrects a plausible
  assumption.** Removing only the solver-side `allow_partial` candidate gates
  left `FillOrKillNeverPartiallyFilled` **GREEN over 25 runs**: the guarantee
  does not rest there. Removing §6.4 rule 3's fill-or-kill clause AS WELL turned
  it **RED 25/25**. So the authoritative guarantee is rule 3, and it is REACHED
  on the new partial-fill path; the candidate gates are an efficiency guard that
  avoids proposing a match that must fail. (The 25-run form is deliberate: the
  pair's `a`/`b` roles follow content-derived intent-id order, so a single run
  would have exercised only one side — a one-shot red here would have been an
  accident of ordering.)

### Determinism vector (Rule 7) — three isolated runs, no re-baseline

Run in ISOLATION after EACH item, two independent processes per run, exit 0, all
five seeds **byte-identical to the D-082 baseline** every time:
`seed=1 245EF177…67CC93 · seed=2 B48BDA6D…954194 · seed=7 FA562B1D…B95B76 ·
seed=12 E491FB48…0EA3E9 · seed=42 6596CCA1…259325`.

**Honest reading of that green (the D-069/D-086/D-088 coverage-shadow shape).**
Byte-identical does not mean the three items are behaviour-neutral in general —
it means the sim never reaches their new branches, and that is exactly what the
additive wiring predicts:
- nothing in-tree writes `IntentLeg.limit`, so item 8's clause is never entered;
- nothing in-tree sets `allow_partial`, so no partial fill is ever built;
- the sim's coordinated cycle has exactly ONE coin give-asset, so it has one
  anchoring, and every pair whose default anchoring already cleared still clears
  through it first.
**The vector does not cover these paths; the unit and end-to-end tests named
above are the covering gate.** Recorded here rather than presented as
confirmation the enrichments are inert in production.

**PROMOTED (review-driven): this is the FOURTH instance, so it is no longer an
observation — it is a STRUCTURAL PROPERTY of the gate.** D-069 (the sim is
epoch-inert, so the real Reveal path is never exercised), D-086 (the sim
generates no mutually-fillable pair, so the fallback-Solution write is never
reached), D-088 (the five seeds never populate a `0xFF` id), and now D-089
(nothing sets `limit` or `allow_partial`, and the sim's cycle has one coin
give-asset). The common cause is the sim's INTENT GENERATOR, not the seed
vector: it emits thin, one-sided, single-asset flow, so the matcher's rich paths
are structurally unreachable however many seeds are added. Two consequences,
both binding going forward:
- **The vector is a whole-system REGRESSION backstop, not a coverage instrument
  for matcher richness.** A byte-identical result on a matcher change is
  evidence of ADDITIVITY, never of correctness; the covering gate must be named
  separately every time (as it is above).
- **Task 2.10 inherits this as a stated design input** (written into its ledger
  row, not left to be rediscovered): an in-consensus cycle-finder whose
  determinism the vector cannot exercise must be gated PRIMARILY by hand-built
  adversarial fixtures — cycle shapes, tie orderings, enumeration-bound
  boundaries, node-identical traversal — each watched red, with the vector as a
  backstop only. Widening the sim's generator until the vector reaches these
  paths is the alternative; it is a legitimate task in its own right and is NOT
  assumed here.

### Forward items (Rule 6 — numbered, not floating)

- **Task 2.10 — in-consensus cycle-aware fallback** (the second half of the old
  `fallback.go:35` TODO). 6a delivers 2-party partial fills; an N-party cycle in
  consensus is a materially larger determinism surface (cycle enumeration must be
  bounded and node-identical) and was split out at the approved plan-stop.
- **Task 2.12 — multi-asset in-kind skim + in-kind solver payout**
  (`skim.go:37`). Split out because it is a FEE-MODEL question (D-050/D-051), not
  a matcher enrichment: it asks whether svc-mint and data-creation surplus should
  be skimmable at all, which D-048's asset-class split answers in the negative
  today.
- **TODO 9 — multi-intent-per-sigil** (`solution.go:143`) folded into **2.7 /
  FW-2**: it needs a `SettleLeg` intent-tag proto field so rule 3 can attribute a
  leg to one of a sigil's several intents, and it collides with D-074's
  two-distinct-sigil bilateral constraint — precisely the FW-2 plan-stop
  question, not an ad-hoc 2.6 carve-out (PHASE2_DESIGN §2.6 says so explicitly).

## D-090 — Phase-2 state-reconciliation audit: docs-only corrections; PHASE2_DESIGN Rev 3; AUDIT F-7/F-8 dispositioned (the findings-list-goes-write-only lesson); the authored-surface inventory re-derivation promoted to a scheduled gate; mirror .git foot-gun FIXED

- **Date:** 2026-07-28 (reconciliation audit at `aa21274`, immediately after
  task 2.6. NOT a build task: no feature code, no proto, no consensus logic.
  The deliverables are a reconciliation report and docs-only corrections; the
  standing instruction was that a CODE defect found here becomes its own
  numbered task rather than a fix in this session — **none was found.**)
- **SPEC:** §10 (the phase ledgers being reconciled), §13 (the greppable-marker
  convention); AUDIT F-1..F-8; AUDIT_SCOPE_1.8 O1–O4/T1–T4/L1–L3/A-*;
  PHASE2_DESIGN Rev 2 (D-078); PHASE3_PLAN §5.1 (D-080); METHODOLOGY Rules 4,
  6, 8, 9 (which governed the session) and 3, 7 (which it amends).
- **Why now.** Phase 2 had landed 2.0–2.6 plus 2.11 while forward items were
  spread across CLAUDE.md, PHASE2_DESIGN, PHASE3_PLAN, DECISIONS and
  AUDIT_SCOPE_1.8. Drift between what the docs claimed and what the tree held
  was the risk, and everything below was verified AGAINST THE TREE — not
  against any prior summary, commit message, or conversational account.

### Verdict: no code defect; five of eight checks found docs DRIFT

**PASS — citations.** All **59** distinct `Test*`/`Benchmark*` identifiers cited
across X402_SURFACE, 7683_SURFACE, AUDIT_SCOPE_1.8 and D-080..D-089 resolve in
the tree, as do all cited file paths and the AUDIT_SCOPE §2.2 function-level
citations. The 1.8 "44/44" discipline still holds, at 59/59.

**PASS — gate health.** `make lint` exit 0; `make test` exit 0 (21 packages ok,
0 FAIL, 0 SKIP); `make sim-determinism` exit 0, all five seeds **byte-identical
to the D-082 baseline** across two independent processes. Tree fingerprinted
before AND after each gate run (`git diff | sha256sum` =
`e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`, the
empty-input hash, both times) — the Rule-9 addition this ADR then writes into
METHODOLOGY, applied to itself.

**PASS — three high-value claims re-verified by direct read (Rule 9).**
(a) `IntentLeg.limit` has **zero non-test writers** repo-wide; its only readers
are `checkLimitRate`/`intentSignsLimit`. D-089's phrasing "zero writers
anywhere … including the conformance suites" is now imprecise — 2.6's own
`limit_rate_test.go:28` writes it — but the substantive premise is verified
INDEPENDENTLY and more strongly: `x/veil/simulation/operations.go` builds
exactly ONE leg per side, never sets `limit` or `allow_partial`, and its
3-party cycle is usdc→data→svc→usdc (one coin give-asset). The Rule-7
structural-property claim is code-true, not merely observed.
(b) **x/receipt never reads escrow records** — zero escrow references in
non-test x/receipt code; the only hits construct an escrow keeper to satisfy
`sessionkeeper.NewKeeper`'s signature in a test harness. D-073's correction of
Rev 1 stands.
(c) **`Receipt.condition_outcome` has exactly ONE non-generated writer** —
`x/session/keeper/receipt.go:51`, stamping `CONDITION_OUTCOME_NONE`
unconditionally; no `PENDING` value exists yet. D-087's premise holds, and 2.9
remains correctly scoped.
Also verified: **D-086 pre-image uniformity extends to the partial shape** —
`TestFallbackPartialFill_PreImageUniformity` asserts the stored Match carries
the FILLED 6 (not the offered 7) and recomputes `terms_hash` for both parties'
receipts — with the honest residual that the **app-level** conformance chain
(query → marshal → ICS-23) still covers full fills only; recorded in
7683_SURFACE §7 rather than left implicit. And the **D-082 baseline is still the
sole fixed point**: those five hashes appear only in DECISIONS.md and are cited
identically by D-084/085/086/088/089; no competing baseline exists anywhere.

### DRIFT-1 — PHASE2_DESIGN was five tasks stale → **Rev 3**

The approved phase authority — the document CLAUDE.md requires every Phase-2
session to bootstrap from (Rule 1) — still called **2.3, 2.4 and 2.6 "not
started"** when all three had shipped, had **no rows at all** for 2.8–2.12,
described the in-flight 2.2 working tree as current, and carried §5 rows
saying the CVM marker was "in flight" (2.2 closed it) and the `window.go:138`
TODO was "open" (2.5 closed it). 2.6's commit had touched only §5.
**This is the most consequential finding of the audit and the reason the
corrections were sequenced with Rev 3 first:** a stale scoreboard does not
merely misinform, it would lead a *discipline-faithful* session to re-plan
committed work — the bootstrap document lying to the discipline that depends on
it. Rev 3 rewrites §0/§1/§2.3/§2.4/§2.5/§2.6/§5/§6 and the FLAGS summary to
as-built, adds the missing task rows, discharges FLAG C, and records the exit
demo as UNBLOCKED.

### DRIFT-2 — ownership defects (two shapes, both now rules)

- **Double ownership:** `timeouts→params` was assigned to **both** 2.7
  (PHASE2_DESIGN §0/§2.7, CLAUDE.md) and 2.8 (D-085's closing paragraph).
  **RESOLVED to 2.8** — it is already the params/charging-shape task, and 2.7
  is already carrying a proto change.
- **One-way pointers:** D-089 re-pointed **TODO 9** (multi-intent-per-sigil)
  and the **`ErrLimitRateUnsupported` multi-counter-asset lift** into 2.7/FW-2,
  and said so in the *deferring* task — but 2.7's own acceptance criteria
  listed neither. An item pointed at a task but not written into it is owned by
  nobody, and is invisible at exactly the moment that task is planned. Both are
  now IN 2.7's criteria, which additionally require the FW-2 plan-stop to
  return an explicit verdict on each (implemented, or re-deferred to a named
  task) so neither can lapse silently again. → METHODOLOGY Rule 6 sub-rules 5/6.

### DRIFT-3 — AUDIT F-7 and F-8 were unowned; the findings list had gone write-only

A repo-wide grep found **zero mentions of F-7 or F-8 outside `docs/AUDIT.md`**.
- **F-7** (§5.1 limit rate ignored, D-049) was **closed in fact by task 2.6**,
  which closed it under the name "D-049" and never mentioned F-7:
  closed-in-fact, open-on-paper. Recorded now. Note its original rationale
  ("safe under Phase-0 fill-or-kill") was **half wrong** — 2.6's watched red
  showed the in-consensus fallback settling a party at an unsigned rate once
  the clause was removed.
- **F-8** (stale phase-0 markers, filed "cosmetic") was **genuinely open for
  two phases** and was **independently rediscovered** by this audit's marker
  sweep, which did not know F-8 existed. That rediscovery is the proof it was
  invisible.

> **The lesson, recorded because it is the transferable part: A FINDINGS LIST
> NEEDS THE SAME OWNER-PER-ROW DISCIPLINE AS A LEDGER, OR IT GOES WRITE-ONLY.**
> This is METHODOLOGY Rule 6's own counterexample — D-044's "dedicated post-0.8
> task", a phrase rather than a task number, which floated two phases and came
> back as **F-3** — reproduced *on the very list that recorded F-3*. A findings
> list is especially prone to it because producing the list feels like
> completing something. `docs/AUDIT.md` now carries a disposition row per
> finding that must never be blank, and Rule 6 says so explicitly.

### DRIFT-4 — the marker sweep (closes F-8)

22 markers audited against Rule 6 ("every hit maps to a ledger task"); **15
remain, every one owned**, zero phase-0, zero phase-1:
- **3 re-tagged phase-3 → task 3.4** — the F-3/D-044 price-source debt
  (`price_source.go` ×2, `lifecycle.go:664`), plus `faceValueScalar`; all had
  named task **0.8**, complete since Phase 0.
- **2 given owners** — session-tx FinalizeBlock coverage → **3.5**; per-lane
  fee priority → **3.3** (its answer moves with the block-sdk disposition).
- **4 retired as completed work** — 0.8's session-keeper swap, the EndBlock
  hand-off, and the counterparty-class predicate wiring (which DID land; the
  one wrong word was "CEL" — the evaluator is a Go predicate environment, no
  CEL library is imported anywhere); and `crypto/reveal/stub.go`'s phase-1
  marker, where the most interesting finding sits: **1.3/1.6 did replace the
  stub, and `StubScheme` now persists DELIBERATELY as the epoch-0 bootstrap
  window (L1, D-069) — a recorded design decision that had been wearing a
  TODO's clothing for two phases.** That is how greppable-marker discipline
  decays into noise, and it is why F-8 was not cosmetic.
- **6 phase-3 Chambers markers now name task 3.11** instead of relying on
  inference, and the two index-sharding markers carry D-088's re-arming warning
  (activating `chamber_id > 0` re-arms the truncated-iterator class).
- **Method note (Rule 8):** the first sweep pass QUOTED the retired marker
  token verbatim in its explanatory prose, so a marker grep still hit five
  retired sites — the sweep briefly defeated the discipline it was restoring.
  Caught by re-running the census, fixed by writing "the former phase-N marker"
  instead. Recorded rather than quietly corrected.
- Four **code comments contradicting shipped behavior** also fixed: three in
  `app/app.go` and one in `crypto/threshold/threshold.go` still said the
  StubScheme was the live Reveal scheme "until Task 1.6" and that the reveal
  share cache was "populated but not yet consumed" — untrue since 1.6 (D-069).
  Not markers, so Rule 6 did not catch them; Rule 9's "summaries encode the
  author's model, and bugs live where model and code diverge" did.

### DRIFT-5 — METHODOLOGY was missing six promised additions

All six were promised in review and absent from the document. Added with their
worked examples, each from a rule failing in practice:
- **Rule 3 corollary — a flaky test may be a correctness ORACLE at low duty
  cycle.** Reproduce and COUNT before stabilizing; a rate near a power-of-two
  boundary points at a byte bug. Instance: task 2.11, where the prescribed
  "make the fixture deterministic" fix would have shipped a live 1-in-256
  store-iterator bug permanently green.
- **Rule 3 refinement — PREDICT which guard reds, then check the prediction.**
  A red landing elsewhere means your model of what-guards-what was wrong.
  Instance: 2.6, where `allow_partial` candidate gates were assumed to be the
  fill-or-kill guarantee and §6.4 rule 3 actually was.
- **Rule 3 dosing — dose the watched-red against whatever randomizes its
  path.** Instance: 2.6's 25 runs, because content-derived id ordering made a
  one-shot red an accident.
- **Rule 4 sharpening — a security claim verified against ONE code path is
  UNVERIFIED; enumerate the paths.** Three instances: 2.3's ante-vs-msg-server
  divergence, 2.4's solver-vs-fallback pre-image gap, 2.3's
  AmendMandate-vs-IssueMandate-REPLACE gap.
- **Rule 7 structural property — the gate's COVERAGE SHADOW.** Four instances
  (D-069/086/088/089) make it a property, not an observation: byte-identical
  means "unperturbed on what the generator exercises", never "inert in
  production", and the covering gate must be named separately every time.
- **Rule 9 addition — fingerprint the tree before and after a gate run**, so
  "the gate ran on the shipping tree" is checkable rather than asserted.

Plus Rule 6's two new sub-rules (findings lists; one-way pointers), a
boundary checklist, and the stale counts fixed (79 ADRs → 90; D-079 → D-090).

### DECISION — the authored-surface inventory re-derivation becomes a SCHEDULED GATE

The audit confirmed the reviewer's concern: **O3 has now gone stale TWICE.**
First D-066's "the four authored pieces are the ENTIRE authored surface" missed
what task 1.7 authored (O3 exists to record that correction); then
AUDIT_SCOPE_1.8, pinned at `5f5feb8`, could not cover what 2.1–2.6 authored —
and PHASE3_PLAN §3.1's "extended with" list, which is what actually scopes the
engagement, named only the D-076 ZK units, T1–T3, the Pedersen probe and the
fee-TWAP surface. Omitted: `crypto/zkdelivery`'s compiled-in VK registry and
record-reconstructed public witness, the **D-075/D-076 consent chokepoints**
(arguably the highest-value authored security surface Phase 2 produced, and
both found as Rule-4 holes in review rather than by test), the escrow
`nextState` gate, D-084's rebind guard, D-085's ante decorator protecting the
conservation invariant, `app/cvm`'s no-forge model, D-088's iterator range
ends, and **D-089's `checkLimitRate` — a NEW EndBlock panic surface behind a
`recover()`, on unbounded agent-signed input, added after the scope was
written.**

Twice is a pattern, and a pattern in a process is fixed by scheduling, not by
resolving to be careful. **PHASE3_PLAN §3.1.1 now enumerates that surface, and
§3.1.2 records a STANDING OBLIGATION: the authored-surface inventory is
RE-DERIVED FROM THE TREE — never re-read — at every phase boundary AND before
the audit engagement is contracted, diffed against the last recorded inventory,
with every addition directly read.** A grep-based procedure is written out
(crypto imports, authorization chokepoints, key-material types, consensus-path
`recover()` sites, ante-chain composition) so it is executable rather than
aspirational; task 3.0 runs it first and 3.1 step 2 runs it again immediately
before contracting. This is METHODOLOGY Rule 9's inventory-diff promoted from
advice to a gate. Without it §3.1.1 goes stale during Phase 3 exactly as §2.2
went stale during Phase 2.

### DECISION — the mirror .git foot-gun is FIXED, not documented

`scripts/dev-mirror-sync.sh` rsyncs with `--exclude .git`, which means it never
WRITES a `.git` into the Windows mirror — but also never UPDATES or REMOVES one
already there. The mirror carried a `.git` frozen at **`b2f633b`, five commits
behind**, while its working files tracked HEAD: `git log` there reported a
stale HEAD and `git status` reported **40+ phantom "modified" files**. The
hazard is not the misreport but the recovery reflex — `git checkout .` or
`git stash` run there from habit would "discard" the phantom changes and
silently revert the mirror to the frozen commit's content. **A doc protects the
reader; the person who gets bitten is someone typing `git status` from habit —
which happened during this very audit.** Content visibility is the mirror's
only job, so a half-stale `.git` is pure downside. The script now **deletes any
`.git` in the mirror unconditionally on every sync**, drops a root sentinel
(`MIRROR-READ-ONLY-DO-NOT-PUSH.txt`) so the hazard is visible in a plain
directory listing, states in the README that this is deliberately not a
repository, and reports the canonical HEAD the mirror reflects.
**Also removed: a dead `git rm` + `git commit` block** at the top of the script
(targeting `scripts/dev-localnet-debug.sh`, deleted at `46170d2` in Phase 0). It
was a no-op behind an `[ -f ]` guard, but a sync script that can create a commit
on the canonical repo as a side effect is a Rule-1/Rule-8 hazard regardless.

### DECISION — the §6 exit demo is next, with a HARD condition

All four exit legs are green for the first time (2.1 ZK escrow, 2.2 CVM, 2.3
x402 ingress, 2.5 IBC USDC + receipt), and **every one of 2.7–2.12 declares
itself off the exit path**, so the demo is the only remaining phase-closing
item and the trigger for PHASE3_PLAN task 3.0. It is also the sole owner of two
obligations deferred into it: the D-079 localnet contract-release smoke (needs
a purpose-built CVM-schema contract binary — a Rust-toolchain dependency, so
the session is larger than its leg count suggests) and the D-082 live relayer
USDC demo.

**The condition, recorded as a condition and not a note:** the demo's x402 leg
is the conditioned ZK buy, so its receipt reads `condition_outcome = NONE` for
a conditioned settlement (D-087). That is acceptable for an **internal
acceptance run** — both edge contracts disclose the true semantics in three
places, so no consumer is misled — but **if the demo output is ever repurposed
as EXTERNAL-FACING evidence, task 2.9 becomes a prerequisite.** Sequencing
rationale: pulling 2.9 first would put a proto change and a determinism
re-baseline on the critical path of the thing that validates Phase 2, and given
2.3's and 2.4's track record the demo is likely to surface integration gaps
worth finding before spending a session on receipt semantics.

### Other forward items given owners (nothing floats)

- **Sim intent-generator widening** (D-089 called it "a legitimate task in its
  own right" and gave it no number) → **3.5**, as a named sub-item: widen the
  generator, or ratify hand-built adversarial fixtures as the permanent primary
  gate for matcher richness — and record which.
- **L3**, **T4**, and the seven **A-*** security arguments had no PHASE3_PLAN
  §5.1 rows; each is owned in substance by 3.1/3.3 but was unnamed, risking an
  engagement scoped to O1–O4 alone. Rows added.
- **Reputation whole-USDC 1e6 scaling quirk** (D-082: "noted not fixed …
  2.6/3.4 surface if it matters"; 2.6 did not) → **3.4**, same
  valuation-provenance question as the D-044 swap.
- **Task 2.6's undischarged edge-doc obligation.** `7683_SURFACE.md` carried a
  *"Warning owned by task 2.6"* and 2.6 changed the fallback matcher without
  touching either edge contract — even though it added two consumer-visible
  rejections and made fallback partial fills reachable. Discharged here: both
  contracts gained a matcher-changes section (`limit` now enforced,
  `ErrLimitRateUnsupported` and the multi-output-order trap it sets for a 7683
  resolver, `allow_partial` now load-bearing on the in-consensus path), the
  partial-shape uniformity proof and its keeper-vs-app-level coverage boundary
  are stated, and both lineages advanced to `aa21274`.

### Consequences

- **Docs, one script, and code COMMENTS only.** No proto, no consensus logic,
  no test logic. The determinism vector is expected byte-identical and was
  re-run after the edits to prove it (hashes in the task record).
- Ledger numbering verified: 2.0–2.12 each defined exactly once, no collisions,
  no gaps; 2.11 moved into numeric order in CLAUDE.md with its
  built-before-2.6 resequencing explained in place, and commit hashes added to
  every Phase-2 row (the Phase-1 convention, which Phase 2 had dropped).
- Two execution-order facts recorded that had never been written down: **2.5
  landed before 2.3 and 2.4** (design-consistent — PHASE2_DESIGN §1 makes them
  parallel siblings — but CLAUDE.md says "in order", so the deviation is stated
  per Rule 8), and **`b2f633b` (the D-080 planning session) carries no Phase-2
  task number**, now noted so the commit maps to something.
- **No code defect was found**, so no new numbered task was created by this
  audit. Every correction above is a docs, tooling, or comment change.

## D-091 — Phase-2 §6 EXIT DEMO part A (§6a): the CVM demo contract + the first LIVE conditioned ZK settlement; D-079 discharged; the condition-attaching writer is DEMO tooling; NEW task 2.13

- **Date:** 2026-07-29 (§6a build, after the approved plan-stop; §6b — the IBC
  leg — is a separate session by that same approved split).
- **SPEC:** §6.2 (fee abstraction), §6.4 (solutions/CONCLUDE), §6.5 (escrow
  classes), §6.6 (receipts), §9 (edges), §10 (Phase-2 exit); PHASE2_DESIGN §6;
  builds on **D-075/D-076** (the consent gates), **D-079** (CVM bindings + the
  deferred localnet smoke), **D-083** (x402 surface), **D-086/D-087** (evidence
  contract + the condition_outcome disclosure), **D-090** (the internal-run
  condition); METHODOLOGY Rules 1/2/3/4/5/7/8/9.
- **Plan-stop decisions (approved, recorded before building):** D1 SPLIT into
  §6a/§6b — the exit criterion is asserted at §6b, only the build is split;
  D2 chain B = a second `conclaved` (`conclave-dev-2`), not gaiad; D3 chain A =
  single-node, with the epoch-0 plaintext-Veil boundary NAMED rather than
  implied; D4 the condition-attaching path lives in `cmd/conclave-demo`, NOT in
  `solver/` (which `x/session/simulation` imports — Rule 7); D5 the built wasm
  artifact is COMMITTED (the committed-VK precedent); D6 **INTERNAL acceptance
  run** (D-090's hard condition; task 2.9 is a prerequisite for any
  external-facing use, and the demo prints this in its own banner).

### The finding the task opened with, which SURVIVES the task

`SettleLeg.Condition` had **no live-network writer**. Verified by grep before any
code was written: `x/session/keeper/fallback.go` never sets it, non-test
`solver/` never sets it, and escrows are created ONLY by CONCLUDE (there is no
`MsgCreateEscrow`). Consequence: **every conditioned-escrow proof in the tree was
a keeper-level hand-built `Match`** — the entire escrow condition system had
never been exercised by a path a real network could take. That is precisely what
this demo existed to discover, and it surfaced at hour zero.

`cmd/conclave-demo` is now that writer — **and it is dev tooling**, so the
statement is still true of production paths. This is recorded loudly in three
places (the demo's package comment, EXIT_DEMO.md, here) because "the demo can do
it now" must not be read as "the network can do it now". Whether the gap is
INTENDED (conditions are solver-attached by design, third-party solvers attach
them, and the chain's job is to verify consent — which §6a proves it does, live)
or a REAL GAP (no reference implementation shows a solver how) is the open
question given to **task 2.13**.

### What shipped

- **`demo/cvm-contract/`** — a purpose-built CosmWasm contract in the CVM schema
  (Rust, `cosmwasm-std` EXACT-pinned `=2.2.8` to match the node's `wasmvm
  v2.2.8`; a caret range floats to 2.3.x, a different VM line). It is a
  transparent conduit: each `ExecuteMsg` emits exactly one CVM custom message and
  the contract holds no state, so everything the demo observes is attributable to
  the BINDINGS, not to contract logic. Its `conclude` branch is deliberately
  present and deliberately not allowlisted. An in-crate test pins the exact JSON
  the Go encoder must accept, so a rename on either side fails at `cargo test`.
  **The built artifact is committed** (~204 KB) so the demo runs without Rust;
  `make demo-contract` rebuilds it.
- **`cmd/conclave-demo/exit_demo.go` + the `exit-demo` subcommand** — seven
  stages (see docs/EXIT_DEMO.md), including the condition-attaching proposer.
- **`scripts/exit_demo.sh`** + two default-OFF genesis knobs in `localnet.sh`
  (`SESSION_LENGTH_BLOCKS` / `SOLUTION_WINDOW_BLOCKS`). The SPEC §6.4 dev
  defaults are 3/2 blocks; on a 1s-block node that is an auction window the
  driver can LOSE to the fallback matcher, which would clear the pair
  UNCONDITIONED and open no escrow. Demo timing only — no code path changes, and
  every existing script keeps the defaults.
- **`docs/EXIT_DEMO.md`** — the acceptance record, its boundaries, and findings.

### D-079 DISCHARGED — the live contract-release smoke

A real contract, uploaded through a **passing governance proposal** (a direct
`MsgStoreCode` is rejected `ErrUnauthorized` first — the permissioned posture
proven live, not assumed), released a **real ZK escrow** with a **real groth16
proof** on a live node: escrow `ACTIVE → RELEASED`, seller paid `250000000uusdc`.
Three negatives ran first, from the same contract: a proof bound to ANOTHER
escrow is rejected `ErrInvalidProof` (D-076 instance binding, live), a preimage
release on a ZK escrow is `ErrWrongClass` (the binding reaches the real keeper
gate), and a contract-emitted `conclude` is rejected by the CVM encoder (layer 1,
from a contract that genuinely emitted it — which `reflect` could never do, and
which is exactly why D-079 deferred this). Ordering is load-bearing: the
wrong-instance proof runs BEFORE the release, because afterwards the escrow is
terminal and the same attempt would fail as `ErrTerminalState`, proving nothing.

### The zktest proving-key constraint — VERIFIED, not restated

`crypto/zkdelivery/zktest` says "imported only by tests, so the proving key never
enters the consensus binary"; the demo driver now imports it, so the constraint
was RE-VERIFIED rather than re-asserted, at two levels:

- **import graph:** `go list -deps ./cmd/conclaved | grep -c zkdelivery/zktest`
  → **0** (the verifier `crypto/zkdelivery` + `circuit` are present, as they must
  be); the same grep on `./cmd/conclave-demo` → present.
- **byte level:** the first 64 bytes of `delivery_v1.pk` appear in the built
  `conclave-demo` binary and are **absent** from the built `conclaved`.

Both checks are one command each and should be re-run at any boundary that moves
this import. The load-bearing property is "never in `conclaved`", and that is now
measured rather than asserted; the package doc's "imported only by tests" is
widened in fact to "tests and dev tooling".

### FINDINGS

- **F-1 (NEW TASK 2.13) — the registered error code is lost on the wire.**
  `ProposeSolution` returns `errors.Join(types.ErrInvalidSolution, ruleErr)`;
  `cosmossdk.io/errors` cannot walk a multi-error `Unwrap() []error`, so ABCI
  code extraction falls back to internal code **1** instead of session **1412**.
  The reason text survives, so the gate is diagnosable by string but not by code:
  every invalid solution looks like an internal error to an off-chain solver, and
  no client can dispatch on the documented codespace. **NOT fixed here** — tx
  result codes are consensus data (they enter `LastResultsHash`), so changing one
  is a state-machine-visible change that must run the determinism gate in
  isolation under its own plan-stop. Found by the live consent negative; pinned
  BY NAME in the demo (the `TestSpendIntentF2BoundaryPin` pattern) so 2.13's fix
  turns it red deliberately rather than drifting.
- **F-2 — Rust ≥1.82 emits wasm `reference-types`, wasmvm v2.2.8 rejects it.**
  Watched in this order: (1) Rust 1.85, no flags → the gov proposal PASSED its
  vote and then FAILED on execution, `"reference-types not enabled ... at offset
  0x6ff"`; (2) `-C target-feature=-reference-types` → **still failed**, at offset
  `0x1c94d`, because the flag governs this crate's codegen and not the
  precompiled `rust-std` it links; (3) **Rust 1.81.0 → accepted**. So the
  toolchain pin (`rust-toolchain.toml`) is the mechanism and the rustflag was
  tried and REMOVED rather than left in as cargo-cult. 1.81's Cargo predates
  `edition2024`, so four transitive deps are held below latest in the COMMITTED
  `Cargo.lock` (zeroize 1.8.1, zeroize_derive 1.4.2, rmp-serde 1.3.0, rmp
  0.8.14) — the lock is what makes the build reproducible rather than
  resolution-date dependent. Contained entirely in dev tooling: no Go dependency
  moved, the §4 two-pin ceiling is untouched.
- **F-3 (self-correction, Rule 8) — my own check passed for the wrong reason.**
  Stage 4's permissioned-upload assertion originally required only `code != 0`,
  and the first run "passed" it on code **11 — out of gas on tx size**, which
  says nothing about upload permission. Tightened to require `ErrUnauthorized`
  specifically, at which point it became a real posture proof. Recorded because
  this is exactly the vacuous-green failure Rule 4 exists to catch, caught in the
  session's own new code.

### Load-bearing negatives (Rule 3), all watched on a live node

The demo's stage 5 negatives (a)(b)(c) and stage 7 are the guards. **Stage 7 is
the one that matters most for what this task ADDS:** since `cmd/conclave-demo` is
now the first live writer of `SettleLeg.Condition`, the question is whether that
writer can IMPOSE a condition. It cannot — with the seller signing the ZK
condition and the buyer signing nothing, the solution is rejected specifically
for *"buyer … did not sign this ZK condition"* (D-076) and the solver is slashed
5%. That is the D-076 gate proven reached from the LIVE path, where previously
only keeper tests reached it. Stage 7 runs LAST because the 5% slash takes
solver1's bond below `solver_min_bond`.

### Gates

`make lint` + `make test` green; determinism vector **byte-identical to the
D-082 baseline** across two independent processes, run before and after with the
tree fingerprinted both times (Rule 9). **Named covering gate (Rule 7):** the
vector covers NOTHING in this task — no consensus code changed, and the sim's
generator reaches neither escrow conditions nor wasm (x/wasm is sim-excluded,
D-079). The covering gate is `scripts/exit_demo.sh` itself, whose every stage is
an assertion, plus the pre-existing `TestConcludeConditioned_Zk` and
`app/cvm_bindings_test.go` suites at the in-process level.

### Forward items

- **NEW task 2.13 — solver-side condition attachment + the F-1 error-code loss.**
  Two items, one owner, both surfaced by this demo: (a) decide and record whether
  a PRODUCTION path should be able to attach delivery conditions, and if so ship
  it (the reference solver is the natural home; note it is imported by
  `x/session/simulation`, so any change there runs the determinism vector in
  isolation); (b) fix F-1 so `ErrInvalidSolution`'s registered code survives to
  the wire, with the determinism gate run in isolation because tx result codes
  are consensus data. Acceptance: a solution rejected for an unsigned condition
  returns session `1412` with the rule error still readable, and §6a's stage-7
  pin is retired in the same task.
- **§6b (the remaining exit work)** owns D-082's live relayer USDC demo, the
  second-chain light-client verification, and re-running §6a's flow on the
  `ibc/<hash>` voucher. The exit criterion is asserted there, not here.

## D-092 — Phase-2 §6 EXIT DEMO part B (§6b): the live-relayer IBC leg and second-chain receipt verification; **the SPEC §10 exit criterion is ASSERTED**; D-082 discharged

- **Date:** 2026-07-29 (§6b build, after the approved plan-stop; completes the
  §6a/§6b split D-091 recorded).
- **SPEC:** §6.2 (fee abstraction), §6.4 (solutions/CONCLUDE), §6.5 (escrow
  classes), §6.6 (receipts + `ReceiptProof`), §9 (edges), §10 (Phase-2 exit);
  PHASE2_DESIGN §6; builds on **D-082** (the live-relayer obligation, the
  session-TWAP fee seam, the ibc/<hash> ceiling work), **D-053** (the ICS-23
  proof surface and its height binding), **D-086/D-087** (the fill-back evidence
  contract and the condition_outcome disclosure), **D-091** (§6a), **D-090**
  (the internal-run condition); METHODOLOGY Rules 1/2/3/4/5/7/8/9.

### Plan-stop decisions (approved, recorded before building)

- **E1 — the relayer is `cosmos/relayer` v2.6.0**, installed with
  `go install github.com/cosmos/relayer/v2@v2.6.0`. Rule 5 framing: this is a
  TOOL in `GOPATH/bin`, not a module dependency — `go install pkg@version` builds
  in its own module, and conclave's `go.mod`/`go.sum` were hashed before and
  after to prove it (`ffe1e90c…` / `687b3f8a…`, unchanged). The §4 two-pin
  go-1.23.x ceiling is untouched. Its own pins (SDK v0.50.11, ibc-go v8.2.0,
  CometBFT v0.38.12) sit on our major line. Alternatives were unavailable, not
  merely unattractive: no relayer binary was installed, the docker daemon was
  down, and D-082 explicitly forbids reusing the in-process ibctesting harness.
  (Note the package path: the module root is the main package —
  `.../relayer/v2/cmd/rly` does not exist — and the installed binary is named
  `relayer`, not `rly`.)
- **E2 — LIGHT-CLIENT-HEIGHT-FIRST** (the decision that carries the stage; below).
- **E3 — parametrize §6a's flow, do not fork it:** `exitState.settleDenom` is
  read by every stage, so §6a and §6b are ONE code path and
  `scripts/exit_demo.sh` becomes a live regression test of the parametrization.
- **E4 — default-OFF knobs** in `localnet.sh` (chain id, RPC/P2P/gRPC/API/pprof
  ports, pruning), defaulting to the exact historical values so every existing
  caller is unaffected; plus a `dialChain` split of `newCLI` so ONE in-process
  `ConclaveApp` serves both chain connections (a second would contend for the
  wasm VM directory — the task-2.2 lock).
- **E5 — the run stays INTERNAL** (D-090's hard condition; task 2.9 remains a
  prerequisite for any external-facing use, and both banners say so).

### E2 — the height inversion, which is the load-bearing design choice

A light client's `ConsensusState` at height `H` stores
`Root = A.header(H).AppHash`, and a header's app hash commits the **previous**
block's state — so a proof checked against that root must be taken at store
version **`H−1`**. And `x/receipt`'s `ReceiptProof` proves at the **TIP**: it
calls `q.proofs.Query(&RequestQuery{Path, Data, Prove: true})` with `Height`
unset, so rootmulti serves latest−1 and the product surface cannot be asked for
a chosen height.

The stage therefore **reads the height chain B's client actually holds, then
asks chain A for a proof at the version that root commits to** — over the
generic ABCI route 7683_SURFACE §6.2 already documents (`/store/session/key`,
`prove=true`, explicit height), reached through
`cosmos.base.tendermint.v1beta1.Service/ABCIQuery` (which accepts `store` paths
with `Height`+`Prove`; verified in the pinned SDK at
`client/grpc/cmtservice/service.go:226`). The rejected alternative — fetch the
tip proof, then hope the relayer lands a client update at exactly
`ProofHeight+1` — is a race over a header height we do not choose. The module
surface's tip proof is still fetched and cross-checked to prove **byte-identical
receipt bytes**, so the product surface stays exercised rather than bypassed.

Real A→B traffic (a token return transfer) advances chain B's client — with
traffic, not a privileged command.

### THE EXIT CRITERION — ASSERTED

One run, `scripts/exit_demo_ibc.sh`: an external x402 agent (mandate-bound hot
key, gas fronted through §6.2) bought ZK-verified data from a Conclave agent,
**settling in real IBC USDC** — `ibc/8E27BA2D…` received over a **live
`cosmos/relayer` process**, trace resolving to chain B's `uusdc` — with a real
CosmWasm contract releasing a real ZK escrow on a real groth16 proof, and the
settlement receipt **ICS-23-verified against the root held by chain B's on-chain
light client of chain A**. **D-082 DISCHARGED.**

### The honest-claim boundary (stated because it is easy to overstate)

- **B→A is verified by a state machine, in consensus.** The voucher can only
  exist because chain A executed `MsgRecvPacket`, whose `OnRecvPacket`
  ICS-23-verifies chain B's packet commitment against chain A's on-chain light
  client of B. Claimed as such.
- **A→B is verified in the DEMO DRIVER, against chain B's root.** The §6.3
  checks run in `cmd/conclave-demo` because the settler is separate-repo scope
  (7683_SURFACE §1.2/§6.3). What makes it a *second-chain* verification is the
  **provenance of the root**: read out of chain B's stored `ConsensusState`,
  established by a live relayer and advanced by real `MsgUpdateClient` txs on B.
- **NOT claimed: chain B's state machine verified the receipt.** That needs the
  verification bridge deployed on B, which is resolver-repo scope.
- **Provenance made CHECKABLE, not merely structural.** A faithful light client
  holds exactly the bytes chain A's own header carries, so a value check cannot
  by itself prove where a root came from. The stage therefore asserts the
  **agreement**: chain B's stored root for height `H` must equal chain A's own
  `header(H).AppHash`. That turns "the client is tracking chain A" into a
  measured fact and would catch a client fed something else.

### Load-bearing negatives (Rule 3), all live, each asserting a SPECIFIC failure

Four, and the message each must produce is pinned by name — never "an error":

| Negative | Required failure |
|---|---|
| one byte of the receipt flipped | `chained membership proof failed to verify membership of value` |
| the **nearest other** root chain B holds | `proof did not commit to expected root` |
| **the off-by-one**: proof at version `H` vs the root for `H` | `proof did not commit to expected root` |
| §6.3 check 3 skipped | the conditioned leg IS admitted (proving check 3 load-bearing) |

The first two fail with **different** messages, which is what proves they test
different things rather than each other. "Nearest, not any" is deliberate: the
first implementation compared against whatever stored height came first (93 vs
12) — a distant root rejects trivially and would let an adjacent-height
confusion pass unnoticed. And the off-by-one negative is the mistake a bridge
implementer actually makes: both artifacts are internally valid and both name
"height H".

### FINDINGS

- **F-1 (fixed in-task) — §6a's D-076 negative asserted only `code != 0`.** The
  wrong-instance-proof negative in stage 5a — *the* live proof of D-076 instance
  binding — checked only that the code was non-zero. A rejection for any other
  reason (wrong class, terminal state, out of gas) would have "passed" it while
  proving nothing about instance binding. This is the same shape as §6a's own
  finding F-3, in a negative guarding a security property, and it was found by
  re-reading the negative rather than by it failing. Tightened to require
  `ErrInvalidProof` (1510) specifically; the §6a script re-run confirms it.
  Fixed rather than deferred because it is an assertion-strength gap in dev
  tooling, not a chain defect — F-3 is the precedent. Recorded loudly because it
  means **§6a's D-076 live claim rested on a weaker check than its record
  implied**.
- **F-2 — the relayer cannot identify itself.** Installed via `go install` it
  carries no ldflags, so `rly version` prints `version: ""`. The pin lives in
  `scripts/exit_demo_ibc.sh` and in this ADR; those are the only sources of
  truth for what ran.
- **The watch that did NOT fire (a negative result worth keeping).** The plan
  named task 1.6's proposer-injected `Txs[0]` (the vote-extension transport) as
  the most likely place for a third-party relayer to break, with a *silent*
  non-relay as the expected failure mode, and pre-committed to treating it as a
  finding about Conclave's compatibility with standard IBC tooling rather than a
  demo-config problem. It did not happen: `cosmos/relayer` v2.6.0 relayed both
  directions with no special configuration. Recorded because no test in the tree
  asserts this, and a future ABCI change could break it quietly.

### A constraint a real bridge operator inherits (contract-relevant)

Stage 5 fetches a proof at the height the counterparty's light client holds, and
**a pruned version cannot serve one**. Any node serving light-client-driven
fill-back must retain the versions its counterparty's client tracks. The demo
nodes run `pruning=nothing` (a new default-OFF `localnet.sh` knob) and the
driver fails with that explanation rather than a bare "no proof". Stated in
`docs/EXIT_DEMO.md` because it is a property of the rail, not of the demo.

### Gates

`make lint` + `make test` green; determinism vector **byte-identical to the
D-082 baseline** across two independent processes, run before and after with the
tree fingerprinted both times (Rule 9). **Named covering gate (Rule 7): the
vector covers NOTHING in this task** — no consensus code changed (the diff is
`cmd/conclave-demo`, `scripts/`, `docs/`), and the sim's generator reaches
neither IBC nor escrow conditions. The covering gates are
**`scripts/exit_demo_ibc.sh`** (every stage an assertion) and
**`scripts/exit_demo.sh`** re-run as the parametrization regression.

### Forward items

- No new task. **Task 2.13 is unchanged by §6b**: the condition-attaching writer
  is still demo tooling, and §6b's stage 6 re-proves the same F-1 wire-code
  boundary pin §6a recorded.
- **Phase-2's ledger is now clear of exit-path work.** 2.7–2.10, 2.12 and 2.13
  remain open and are all off the exit path, so PHASE3_PLAN task **3.0** (the
  Phase-2 boundary re-validation) is now triggered — its trigger was the §6 exit
  demo.

## D-093 — Task 2.13: the `ErrInvalidSolution` wire-code loss FIXED (D-091 F-1); and the condition-attachment verdict = **GAP** — a signed delivery condition is a PERMISSION CEILING, never a REQUIREMENT

- **Date:** 2026-07-29 (task 2.13, both halves, after the approved plan-stop).
- **SPEC:** §6.4 (solution validation), §6.5 (escrow classes), §13 (stable
  per-module error codespaces), §11.6 (determinism); builds on **D-075/D-076**
  (the consent gates), **D-085** (the path-enumeration lesson), **D-089** (the
  coverage shadow), **D-090** (the findings-list discipline), **D-091**
  (finding F-1 and the condition-writer finding), **D-092** (§6b);
  METHODOLOGY Rules 1/2/3/4/6/7/8/9.
- **Plan-stop decisions (approved, recorded before building):** S1 as the fix
  shape (below); GAP as the (a) verdict; one session, strictly sequenced
  (source + gate first, docs after, so vector attribution is clean by
  construction); the closure split enforcement→2.7 / reference→**2.14**; a
  single commit.

### Three premise corrections made AT the plan-stop, before any code (Rule 1 step 3)

The kickoff brief carried three claims that did not survive reading the tree.
They are recorded because a fix justified by a claim that does not hold is
unauditable later.

1. **"The determinism vector is required because result codes enter
   `LastResultsHash`."** True of a real network — CometBFT v0.38 hashes
   `deterministicExecTxResult{Code, Data, GasWanted, GasUsed}`, so a tx result
   code IS consensus data and this change IS state-machine-visible on chain.
   But the project's vector cannot see it: `DETERMINISM-HASH` is `appHashes[i]`,
   the SIM's store-commit hash (`app/sim_test.go:477`), and `SimDeliver` never
   builds a CometBFT block. So a byte-identical vector here **licenses
   nothing**. Run in isolation anyway (attributable absence-of-drift is worth
   having); see the coverage-shadow entry under Gates.
2. **"Both edge docs document the codespace 2.13(b) restores."** They do not.
   Neither `X402_SURFACE.md` nor `7683_SURFACE.md` mentions `MsgProposeSolution`
   at all — a gateway and a resolver submit INTENTS; only a solver proposes
   solutions. Their `1416`/`1428` tables name error IDENTITIES for outcomes that
   surface in EndBlock, not tx result codes. The contract 2.13(b) restores is
   **SPEC §13 / CLAUDE.md** ("stable codespaces … `session`=14xx"); the only
   document stating the BROKEN behaviour was `EXIT_DEMO.md`'s F-1 row plus the
   stage-7 pin. So (b) is a SPEC-§13 conformance fix, not an edge-doc retrofit,
   and no edge-doc change was needed for (b). (Both edge docs ARE changed by
   (a) — for a different reason.)
3. **`ErrInvalidSolution` is not the only registered code in play.** Every §6.4
   rule error is registered too, and **18 in-tree assertions** depend on
   `errors.Is` finding BOTH the sentinel and the rule error. That is why
   `errors.Join` was chosen in the first place, and it is the constraint that
   selected the fix shape.

---

## (b) The registered code now reaches the wire

### Root cause, read from the pinned library

`cosmossdk.io/errors@v1.0.1` `abci.go` extracts the ABCI code by walking exactly
two interfaces — `coder{ABCICode() uint32}`, then `causer{Cause() error}` — and
falls through to `internalABCICode = 1` / `UndefinedCodespace` otherwise.
`errors.Join`'s value implements neither (it exposes `Unwrap() []error`), so
`ProposeSolution`'s rejection arrived as **code 1, codespace `""`**: every
invalid solution looked like an internal error, and no client could dispatch on
the documented codespace.

### The shape decision (S1), and the two alternatives REJECTED

| | ABCI code | `Is(…, ErrInvalidSolution)` | `Is(…, ruleErr)` | cost |
|---|---|---|---|---|
| as built (`errors.Join`) | **1** / `""` | yes | yes | the defect |
| **S1 — `types.InvalidSolution(rule)`** | **1412** / `session` | yes | yes | ~25 authored lines |
| S2 — `errorsmod.Wrapf(ErrInvalidSolution, "%s", rule)` | 1412 / `session` | yes | **no** | 10 assertions degrade to string matching |
| S3 — `errorsmod.Wrapf(rule, "%s", ErrInvalidSolution)` | rule code | **no** | yes | 8 assertions break; retires 1412 |

**S2 rejected:** it buys the wire code by deleting ten
`require.ErrorIs(t, err, tc.err)` assertions in favour of `ErrorContains` —
trading one vacuity for ten, directly against the write-time rule that a
negative must assert the SPECIFIC error.

**S3 rejected on an ASYMMETRY, not on taste.** Surfacing the rule's own code
(1416/1427/1428…) is genuinely richer for a solver and is what a future sub-code
scheme should do. But per-rule sub-codes can be added later WITHOUT breaking a
client that dispatches on 1412, and 1412 could not be restored later without
breaking clients that had come to depend on the rule codes. S1 preserves the
option S3 forecloses. The code deliberately stays the MESSAGE-level 1412; the
rule identity remains in the log text and via `errors.Is`.

**S1 as built:** `types.InvalidSolution(rule)` returns an `invalidSolutionError`
implementing `ABCICode()` and `Codespace()` (the library's `coder`/`codespacer`,
implemented explicitly rather than relying on `causer` traversal, so the ABCI
mapping is legible at the point of definition) plus stdlib `Unwrap() []error`.
One call site — `solution.go:117` was the ONLY `errors.Join` in non-test code,
tree-wide. Determinism: a pure function of the rule error; no map iteration, no
clock, no float (§11.6).

### Load-bearing tests (Rule 3) — PREDICTED, then watched

Two sabotages, each predicted in writing before running:

- **Revert the CALL SITE to `errors.Join`.** Predicted: only
  `TestProposeSolution_RejectionCarriesRegisteredABCICode` reds, with
  "expected 1412, got 1"; `x/session/types` stays green. **Observed exactly
  that** — `expected: 0x584, actual: 0x1`, one test.
- **Make `types.InvalidSolution` itself return `errors.Join`.** Predicted: BOTH
  new gates red (all five subcases of the types table plus the keeper site),
  while `TestErrorsJoinLosesTheRegisteredCode` and
  `TestInvalidSolutionDoesNotMatchUnrelatedErrors` stay green. **Observed
  exactly that.**

New tests: `x/session/types/errors_test.go` —
`TestInvalidSolutionSurfacesRegisteredCode` over five rule-error shapes;
`TestInvalidSolutionDoesNotMatchUnrelatedErrors`, guarding the other direction
(without it, a value that matched everything would pass); and
`TestErrorsJoinLosesTheRegisteredCode`, which pins the LIBRARY behaviour in
executable form so a future edit reaching for `errors.Join` — it reads like the
natural composition — learns what it costs. In `x/session/keeper`,
`TestProposeSolution_RejectionCarriesRegisteredABCICode` asserts the wire code
on the value the msg server actually propagates, and also that the fix does NOT
blanket-stamp 1412 over `ProposeSolution`'s other rejections (equivocation stays
1410).

`errorsmod.ABCIInfo` is not a proxy for the wire path — it IS the wire path:
`sdkerrors.ResponseExecTxResultWithEvents`, which baseapp calls to build a
failed tx result, is exactly `space, code, log := errorsmod.ABCIInfo(err, debug)`
(cosmos-sdk v0.50.15 `types/errors/abci.go`).

**The finding inside the finding:** all 18 pre-existing `ErrorIs` assertions
pass under BOTH the broken and the fixed shape. That is precisely why a suite
full of green assertions never caught this, and it is Rule 4 in miniature — the
sentinel was PRESENT in the error chain and simply never READ by the layer that
mattered.

### The §6a stage-7 pin: watched RED on a live node, then replaced

D-091 built the pin to self-announce, and it did. With the fix applied and the
OLD pin still in place, `scripts/exit_demo.sh` failed at stage 7 with D-091's
own words — *"wire code is now 1412 — D-091's F-1 appears FIXED; retire this pin
and close task 2.13"* — exit code **1**. The pin was then replaced by assertions
on the (codespace, code) PAIR, and the demo re-run **green**:

```
wire identity: codespace "session" code 1412 (ErrInvalidSolution) — an off-chain solver
can dispatch on the documented SPEC §13 codespace, and the rule text survives ✓
```

The pair matters: a bare code is ambiguous across modules, and a bare codespace
does not distinguish this rejection from any other session error. Carrying the
codespace needed `broadcastAndWaitFull` in the demo driver (the existing helper
dropped it); `broadcastAndWait` is now a thin wrapper over it, so no other call
site changed.

**A measurement self-correction (Rule 8).** The first red run was reported by my
own tooling as `RC=0`, which would have meant `scripts/exit_demo.sh` exits 0 on
failure — a gate that cannot fail, which would have been a finding in its own
right. Before recording that, I built a minimal repro of the script's `set -e` +
EXIT-trap structure: it exited **1**, as did four narrowed variants. The `RC=0`
was an artefact of nested `$?` expansion through PowerShell → `wsl` →
`bash -lc` (the same quoting pitfall left a loop variable unexpanded in the same
output). Re-measured from a file-based runner: **`EXIT_RC=1` on the red run,
`EXIT_RC=0` on the green run.** The gate is sound; there was no defect. Recorded
because a phantom defect reported as real costs a later session a day, and
because the faulty measurement was mine.

---

## (a) VERDICT: **GAP** — and structurally worse than "no reference implementation"

D-091 left the question open: is the absence of a production writer for
`SettleLeg.Condition` INTENDED (solver-attached by design, chain verifies
consent) or a GAP? Read from the code, three findings:

1. **Consent is enforced in ONE direction only.** Every guard in the tree proves
   `attached ⇒ signed` — a solver cannot IMPOSE a condition (D-075 seller,
   D-076 buyer, at validation AND at settlement). **Nothing anywhere enforces
   `signed ⇒ attached`:** both chokepoints skip a NONE leg
   (`validateConditionConsent` and `openConditionedEscrows` each `continue`
   before ever reaching the comparison). The party bearing none of the delivery
   risk — the solver — is the only party who can opt the system in, and it is
   neither obliged nor rewarded for doing so.
2. **The IN-CONSENSUS FALLBACK performs the downgrade, so this is a protocol
   property, not a solver-behaviour question.** `buildFallbackMatch` emits
   `SettleLeg`s with no `Condition` set at all, and `validateSolution` accepts
   them. The fallback is the DEFAULT clearing path for every session no solver
   wins. Two parties who BOTH signed the same ZK condition are therefore cleared
   UNCONDITIONED **by the state machine itself** — proven, not argued, by part
   (2) of the boundary pin below, which drives the real `buildFallbackMatches`
   and gets back a match whose DataGrant leg has a nil condition.
3. **No reference implementation, and no incentive to write one.** Non-test
   `solver/` never reads `IntentPayload.Condition` (zero hits). Nothing shows a
   third-party solver the required shape — exact-param equality, the D-074
   bilateral constraint, the buyer signature for ZK — and attaching a condition
   costs a solver shape constraints and rejection risk for no score benefit.

**The honest statement: a signed condition is a permission ceiling, never a
requirement, and no document says so.** A party can bound what may be imposed on
it; it cannot obtain what it asked for. The only live-network writer of
`SettleLeg.Condition` remains `cmd/conclave-demo`.

### What the downgrade actually costs — THE HEADLINE, and worse than "the condition is dropped"

Writing the boundary pin corrected my own model, and the correction is kept
visible (Rule 8) because it changes the SEVERITY, not just the wording. I
predicted "no escrow is opened". The truth:

**Every** DataGrant leg opens an `x/escrow` entry (`conclude.go` `applyLeg`).
For a NONE condition that entry is a **record-only, zero-amount shell** with no
release gate — **while the buyer's payment goes STRAIGHT to the seller** through
the ordinary coin leg. So the downgrade does not merely remove a protection: it
converts a signed escrowed-delivery trade into **buyer-pays-first with no
recourse**, and leaves behind an empty escrow record that exists to be mistaken
for the protection that was removed. The buyer has paid in full, up front, for a
delivery it can no longer withhold payment against — nothing to release, nothing
to refund, nothing to dispute.

Combine that with finding 2 — the fallback is the DEFAULT clearing path — and
the statement is: **two parties who both signed a ZK delivery condition are
cleared into buyer-pays-first-with-no-recourse BY CONSENSUS.** Not by an
untrusted solver; by the state machine, on the path taken whenever no solver
wins the auction. That is not a nicety for 2.7 to weigh among its other seams —
it is why 2.7 has a DOWNGRADE row and why the verdict is GAP.

The boundary pin asserts that exact shape — nil condition, **zero amount**, and
the seller's balance up by the **full payment** — rather than the weaker thing I
first wrote.

### The verdict recorded in EXECUTABLE form

`TestSignedConditionIsPermissionNotRequirement_BoundaryPin` (x/session/keeper)
pins what is TRUE TODAY, by name, in the F-2 / D-091-F-1 idiom, so the fix turns
it red DELIBERATELY instead of the verdict quietly decaying in a document. It
asserts both paths: (1) a solver omitting the condition is accepted, concludes,
and is not slashed; (2) the in-consensus fallback produces the nil-condition
match. Its doc comment names task 2.7 as the owner and says: when 2.7 lands this
test SHOULD fail — replace it with the enforcement assertions, do not relax it.

### TWO RECORD CORRECTIONS (Rule 6 / Rule 8) — both loud

- **D-075-FW-3's disposition is wrong for the case that matters.** It reads
  *"the seller binding already rejects a seller-signed condition settled as a
  different one"* and dispositions FW-3 as subsumed. That is **true for a
  DIFFERENT condition and false for a DROPPED one** — the loop `continue`s
  before ever reaching `conditionEqual`. FW-3 has therefore been reading CLOSED
  while its principal case is OPEN. Corrected here; the drop-to-NONE case is
  written into 2.7 below.
- **2.7's narrowed FW-1 leaves the ZK downgrade owned by NOBODY.** 2.7's row
  scopes FW-1 to "HASHLOCK/OPTIMISTIC class/window-level buyer-side consent;
  ZK's exact-param gate shipped in 2.1b" — accurate about the ATTACH direction,
  but it means no task owns the ZK DOWNGRADE. A buyer who signed a ZK condition
  can still be settled unconditioned, and no row anywhere said who fixes that.

**The recurrence is the observation worth naming.** This is exactly the Rule-6
one-way-pointer failure that D-090's reconciliation audit found, wrote into
METHODOLOGY as sub-rules 5 and 6, and closed — reproduced in the SAME ledger
weeks later, in rows written after the lesson. Twice was a pattern; three times
says the discipline does not survive contact with a narrowing edit: **when a
receiving task narrows its own scope, the item that was pointed at it is
silently orphaned, and nothing in the process re-checks the pointer.** The
sub-rules say to write the item INTO the receiving task; they do not say to
re-verify it when that task's wording later changes. Recorded here as a
candidate METHODOLOGY amendment rather than applied unilaterally.

### Closure, scoped and OWNED (not built here)

- **Enforcement half → written INTO task 2.7's acceptance criteria** as its own
  row (D-075-FW-1(b) + FW-3, including the ZK case and what the FALLBACK must do
  with a condition-signing intent — reject the pair, attach, or an explicit
  documented "permission not requirement" posture). Written into the receiving
  task, not pointed at it: that IS the fix for the failure found above.
- **Reference half → NEW task 2.14** (a solver-side attachment path plus the
  solver-facing contract doc). Separate because `solver/` is imported by
  `x/session/simulation`, so any change there runs the determinism vector in
  isolation, and because it is reference/tooling work rather than consensus work.

### Consumer-visible disclosure

Both edge docs stated only the imposition direction, which is half the story.
`X402_SURFACE.md` §1.4 and `7683_SURFACE.md` §3.1 now also state that a signed
condition is not a guarantee it will be attached, and that nothing on a live
network attaches one today.

---

### Gates

`make lint` + `make test` green. Determinism vector run **in ISOLATION on the
(b) source change, before any doc edit** — two independent processes, five
committed seeds, **byte-identical to the D-082 baseline**, no re-baseline; tree
fingerprinted before and after the run and **identical** (Rule 9). A final
confirming run was made on the shipping tree.

**Named covering gate (Rule 7) — the vector covers NOTHING here, and this is a
coverage shadow of a SECOND, DISTINCT CATEGORY.** The distinction is not
pedantry: the two categories have different remediations, and conflating them
would send a future session to fix this one with a tool that cannot reach it.

- **Category 1 — GENERATOR gaps (D-069, D-086, D-088, D-089).** The gate could
  observe the behaviour; the sim's intent generator never produces the input
  that reaches it. Remediation: **a wider generator**, which is real work and is
  already scoped (PHASE3_PLAN 3.5).
- **Category 2 — OBSERVABLE-SURFACE gaps (this task, first instance).** The
  quantity the gate hashes is the sim's store-commit app hash, and `SimDeliver`
  never builds a CometBFT block, so **tx result codes are outside what the gate
  can observe at all** — for any input, under any generator. A wider generator
  buys exactly nothing here. Remediation: **a DIFFERENT GATE.**

For this change that different gate is `scripts/exit_demo.sh` **stage 7 on a
live node** — the only gate in the project that exercises the real CometBFT
result path end to end — backed in `make test` by the two `ABCIInfo` tests (the
literal wire extractor) and the boundary pin. Naming which gate actually covers
a change, every time, is precisely what Rule 7's covering-gate step is for; this
instance shows the step has to name the gate's *observable surface*, not just
its inputs.

**Deviation surfaced (Rule 8), stated so the fingerprint claim is not quietly
broken.** `cmd/conclave-demo` and `docs/` were edited AFTER the ISOLATED vector
run, by the approved sequencing; neither can affect it (`go list -deps ./app`
and `go list -deps ./x/session/...` each contain zero `conclave-demo` packages),
and the final confirming run covers that tree. After the FINAL run, one further
round of edits landed at the review stop — **`docs/DECISIONS.md` and `CLAUDE.md`
only, prose, no source and no test** (this severity framing, the two-category
coverage-shadow split, and this paragraph). The shipping fingerprint therefore
differs from the one recorded at the final gate by exactly those two documents.
Re-running a 292-second consensus gate to bless a prose edit would be theatre;
saying nothing would be worse, because "the gate ran on the shipping tree" is
the single most load-bearing claim in the checklist (Rule 9 step 4) and it is
now true modulo two Markdown files, which is what this sentence exists to make
checkable.

---

## D-094 — Task 2.7: a signed delivery condition is a REQUIREMENT, not a permission ceiling — `signed ⇒ attached` ENFORCED, the in-consensus fallback made an ATTACHER, and FW-1/FW-2 landed

- **Date:** 2026-07-30 (task 2.7, after two approved stops: the semantic in
  isolation, then the build plan).
- **SPEC:** §5.1 (`IntentPayload.condition`, `IntentLeg`), §6.4 (solution
  validation, the atomic Conclude, the in-consensus fallback), §6.5 (condition
  classes), §11.6 (determinism), §13 (stable codespaces); **UPDATES D-075**
  (its FW-1 and FW-3 forward items) and **D-076** (extends the buyer gate from
  ZK to all three classes); closes the DOWNGRADE row **D-093** wrote into this
  task; builds on **D-073/D-074** (escrow funding + the bilateral shape),
  **D-085** (the one-source-of-truth lesson), **D-089** (the two re-points),
  **D-090** (the ownership discipline); METHODOLOGY Rules 1/2/3/4/6/7/8/9.

### The decision, and the argument that actually carried it

**A signed delivery condition is now a REQUIREMENT.** A settlement that drops
one is invalid; the fallback attaches or leaves the pair unmatched.

Three options were weighed at STOP #1 — (i) enforce, (ii) an explicit
documented permission-ceiling posture, (iii) an opt-in `require_condition`
flag. The decisive argument was **not** the severity of the exposure, which is
the argument the ledger led with. It was **internal inconsistency in a single
signed message**:

| signed field of `IntentPayload` | enforced as | where |
|---|---|---|
| `give`/`want` quantities | REQUIREMENT | `checkLimits` |
| `IntentLeg.limit` rate | REQUIREMENT (since 2.6) | `checkLimitRate` |
| `counterparty_class` | REQUIREMENT | rule 5 |
| `condition` | **permission ceiling only** | both chokepoints `continue`d |

Every other field of the same object is a requirement; `condition` alone was a
permission, and nothing in the tree or the SPEC said so. That basis is stronger
than severity because it does not depend on how bad the exposure is. And
**D-049/F-7 is this exact shape one task ago for the neighbouring field** —
`limit` was "SIGNED by the agent and read by nobody, the worst of both worlds"
— so deciding the smaller case (rate) one way and the larger case (principal,
no recourse) the other way was indefensible.

**Option (ii) died on its mitigation not surviving contact with the tree.** Its
safety half was "do not create the record-only zero-amount shell, so nothing can
be mistaken for protection". That shell is **SPEC-MANDATED** (§6.4: "DataGrant
legs instantiate `x/escrow` entries instead of transferring", implemented at
`conclude.go` `applyLeg`) and is the ONLY on-chain trace that an unconditioned
data grant happened — data carries no ledger balance. Removing it needs a
SPEC-deviation ADR and deletes the delivery record; MARKING it is a proto change
to `Escrow`, which is task 2.9's territory. What remained of (ii) was "document
it", already nearly shipped in both edge docs by D-093 — 2.7 would have closed
as a docs task over a live economic exposure.

**Option (iii) died on Rule 4 step 4.** An agent that signs a condition and
forgets the flag gets buyer-pays-first-with-no-recourse: the unsafe reading as
the default, silently and totally. Two artifacts to express one intention is
also a consent-surface smell — the signature IS the intent, and a second field
asserting "I meant it" makes the first one's meaning-without-it the question,
whose honest answer is (ii).

**The liveness objection, re-priced.** Nothing on a live network attaches
conditions (task 2.14 is the reference), so enforcement means a condition-signing
intent stops clearing. But the cost is **NO FILL, not stuck funds**: the intent
rolls to TTL, reaches `INTENT_STATE_EXPIRED`, and its bond is refunded. Against
that, what "clearing" meant for the same intent before 2.7 was clearing WRONGLY
and irreversibly. Not clearing beats clearing wrongly when the wrong clearing
cannot be undone — and fail-closed is this project's posture at every
neighbouring boundary (D-075, D-076, `counterpartySatisfied`'s Decision A,
`parseSignedLimit`'s "a garbage rate is never read as no rate").

### R2 — the fallback ATTACHES (the decision that removes the liveness cost)

The ledger permitted 2.7 to answer reject / attach / documented-posture for the
fallback. Reject-only would have converted a correctness fix into a liveness
cliff on the DEFAULT clearing path, so `buildFallbackMatch` now carries each
giver's signed condition onto its own DataGrant leg. It was cheap because the
matcher already loads the full payload and discarded everything but
`give[0]`/`want[0]`, and because the pair it builds is already exactly D-074's
conditioned shape — `pickNumeraire` rejects data-for-data and data-for-svc, so a
conditioned fallback match is always data-for-coin, across two distinct sigils.

The counterparty's consent is deliberately NOT consulted at attach time: the
candidate faces the unchanged `validateSolution` bar, which runs the same
predicate as every other clearing path. Keeping that decision in one place is
what stops the fallback becoming a second, weaker definition of consent.

**Consequence worth stating: the in-consensus fallback is now the FIRST REAL
ATTACHER in the tree.** The rule ships with a live path that satisfies it rather
than a rule nothing can meet.

### R3 — the per-class buyer relation (the substantive design work)

Buyer consent could not be equality: a buyer cannot pre-commit a HASHLOCK
`payload_hash`, the post-delivery receipt token D-075 recorded as FW-1's whole
reason for existing. The relation is derived from **what each timer protects**,
not by analogy:

| role / class | relation | derivation |
|---|---|---|
| seller, any class | EXACT (`conditionEqual`) | it generates the deliverable and knows the schema; D-075 unchanged |
| buyer, ZK_DELIVERY | EXACT | `schema_commitment`/`vk_id` are advance-known and release is seller-driven against them; D-076 unchanged |
| buyer, HASHLOCK | attached `timeout_blocks` **≤** signed | the timeout is when the buyer's LOCKED payment auto-refunds, so longer is worse: signed value is a **MAXIMUM** |
| buyer, OPTIMISTIC | attached `dispute_window_blocks` **≥** signed | the window IS the buyer's protection — time to inspect and dispute before funds release — so shorter is worse: signed value is a **MINIMUM** |

**The HASHLOCK and OPTIMISTIC bounds run in OPPOSITE directions, and that is the
substance rather than an accident.** A single "the window must match" rule would
be wrong for both; a single "at least as large" rule would be wrong for HASHLOCK.
Each bound is the direction that is monotone-SAFE for the party carrying the
risk. `payload_hash`: empty = UNCONSTRAINED, non-empty = exact — a buyer that
knows it out of band may pin it, and pinning can only TIGHTEN.

This lands **FW-1(a) and FW-1(b) in one mechanism**: requiring the buyer's
consent to ATTACH (previously ZK-only, so a solver could impose a HASHLOCK or
OPTIMISTIC timeline on a non-signing buyer) and forbidding the DROP.

### One predicate, two quantifiers — and the loop bound is the actual fix

Both directions are the same question — "does the attached condition satisfy
what this party signed?" — differing only in quantifier: the attach direction
asks it of the attached condition, the requirement direction asks whether SOME
such condition was attached. `conditionSatisfies` is that one function, so the
rule cannot be implemented for one direction, one role, or one class and not the
others.

**But the predicate alone would have closed nothing.** Before 2.7 both
chokepoints iterated only legs that ALREADY carried a condition, so the single
case they could never observe was the case that mattered. **Widening the loop to
every DataGrant leg is the fix**; the predicate is what it then asks. That
distinction is load-bearing and was proven as such (below).

Error split, chosen so the two directions stay distinguishable by a test that
can only fail one way: **`ErrConditionNotHonored` (session `1429`, NEW) fires for
EXACTLY ONE case — attached is NONE while signed is not.** Every other rejection
stays `ErrConditionNotConsented` (`1427`), whose registered text was corrected
here (it said "the seller" and now covers both parties). No existing negative
changed its error identity, including the live demo's stage-7 assertion.

### FW-2 — the per-leg `IntentLeg.condition` field, with a reader

`IntentLeg.condition = 5`. It REFINES the payload-level field: a matching signed
leg that sets one is governed by it, one that leaves it unset falls back — so an
intent written before the field existed resolves exactly as it did pre-2.7.

**Its reader is `effectiveCondition`**, which resolves the condition governing
one settlement leg for one party and feeds the predicate at BOTH chokepoints and
in the fallback attacher. Rule 4 satisfied at landing: removing the per-leg
branch turns named tests red (watched, below), so the field is load-bearing and
not decoration.

**No migration.** `IntentPayload` lives inside `SealedIntent.ciphertext` as
marshaled bytes, not as its own store entry; a proto3 field addition does not
change the encoding of existing payloads. No store migration, no genesis change.
(The enforcement RULE is consensus-behaviour-changing and would need a
coordinated upgrade on a live network — recorded, moot pre-testnet.)

**Spec-unpinned choice, decided here and fail-CLOSED:** a per-leg field that is
PRESENT but EMPTY does not exempt a leg from a payload-level condition; it falls
back like an absent one. proto3 can distinguish the two, so per-leg exemption
stays expressible later — but reading "empty submessage" as "this leg needs no
protection" is a fail-OPEN default a client emitting empty messages could trip
into silently. Refinement may only ADD a condition, never remove one.

**D-074 VERDICT — kept, not relaxed.** FW-2's plan-stop question was whether the
per-leg field relaxes the two-distinct-sigil bilateral constraint. It does not,
and the feared head-on collision **dissolves**: the two are orthogonal. D-074
constrains the SHAPE of a conditioned match (escrow funding needs one
unambiguous coin payment against one deliverable); per-leg tagging is about
WHICH signed condition governs which leg. Richer conditioned-settlement funding
is what lifts D-074, and 2.7 does not build it.

### The two D-089 re-points — EXPLICIT VERDICTS (neither left silently open)

- **TODO 9 (multi-intent-per-sigil) — RE-DEFERRED to new task 2.15, collision
  RESOLVED.** It is blocked by SCOPE, not by D-074: it needs a `SettleLeg`
  intent-tag field plus a rewrite of flow attribution in
  `CheckConservation`/`checkLimits` from per-SIGIL to per-INTENT — 2.10-scale
  consensus surface, which Rule 7 step 2 forbids stacking on a task already
  carrying a semantic change and a proto change.
- **`ErrLimitRateUnsupported` (session `1428`) — NOT LIFTED; the rejection
  STANDS.** PHASE2_DESIGN §2.7 anticipated this outcome and required it be
  recorded rather than left greppable-and-unowned. The reason is concrete: FW-2's
  field is a `DeliveryCondition`, and 1428 needs a **counter-asset designator** —
  nothing about a delivery condition tells `checkLimitRate` which asset a give
  leg's rate is denominated against. Different field, different question. → also
  **2.15**.

Both are written INTO 2.15's acceptance criteria, not pointed at it.

### `solver/` — left wholly to 2.14, with the consequence recorded

`solver/` is import-linked into `x/session/simulation`, so touching it would make
2.7 carry a second vector-attribution cause for code the sim provably cannot
exercise (zero `Condition` occurrences in `solver/` or `x/session/simulation`,
test or non-test). **Consequence, stated rather than hidden: until 2.14 lands,
the in-tree reference solver will propose a slashable solution the first time it
meets a condition-signing intent.** Pre-testnet, no production solvers,
acceptable — and written INTO 2.14's row.

### Load-bearing tests (Rule 3) — PREDICTED, watched, and one prediction WRONG

**The loop-bound sabotage, run because the predicate could otherwise be proven
on legs the loop happens to visit.** Reverting ONLY the loop bound (predicate
untouched), one chokepoint at a time:

- validation bound only → all 7 `TestDowngradeRejected_EveryClassEverySigner`
  cases red at the VALIDATION assertion (line 82) plus
  `TestSignedConditionIsRequirement_Enforced`; the settlement-only tests
  (`TestPerLegEmptyConditionDoesNotExemptALeg`, `TestConcludeConditioned*`)
  stayed **green**.
- settlement bound only → the mirror image: the same cases red at the SETTLEMENT
  assertion (line 88, so line 82 had passed), plus the settlement-only test.

That pair is the proof the WIDENING is what closed the hole, and that the two
chokepoints are independently load-bearing.

**A PREDICTION THAT WAS WRONG, kept visible (Rule 8).** Flipping HASHLOCK's bound
direction was predicted to red the two ceiling subtests "and nothing else". It
red **four** tests: `TestConcludeConditioned_Hashlock` and
`TestPerLegConditionOverridesPayloadLevel` also carry attached-under-ceiling
shapes. The mechanism was right and the inventory of consumers was not — the
guard has more consumers than I named, which is a stronger position than
predicted, not a weaker one.

**The two flips red DISJOINT test sets** — HASHLOCK's flip touched no OPTIMISTIC
test and vice versa. That disjointness is the evidence the opposite directions
are independently load-bearing, which is the claim R3 rests on.

Other watched reds, each predicted then observed: attacher removed → the
`_Enforced` fallback half + the affordability test; `fallbackCanFund` bypassed →
only the affordability test; `effectiveCondition`'s per-leg branch removed → only
the two FW-2 subtests.

**DOSED at `-count=25`** (D-089's lesson — the fallback's seller/buyer roles
follow content-derived intent-id ordering, so a one-shot result exercises one
side by accident): the suite green **25/25**, and the two fallback sabotages red
**25/25**. The red is the rule, not a draw.

**A fixture migration that is a semantic change, not plumbing.**
`TestConcludeConditioned_Hashlock`/`_Optimistic` used to pass with the buyer
signing NONE; under FW-1(a) that exact shape is now a 1427 rejection. Both moved
to `setupBilateralBoth` with EVERY assertion preserved, and the behaviour that
changed is pinned by two NEW mirror negatives
(`..._Hashlock_BuyerNotConsented`, `..._Optimistic_BuyerNotConsented`), so the
migration deletes no coverage.

`TestUnconditionedPairStillClears` guards the other direction — a requirement
rule that rejected everything would pass every downgrade test — by proving a
pair who signed nothing settles exactly as before.

### The boundary pin, REPLACED

`TestSignedConditionIsPermissionNotRequirement_BoundaryPin` (D-093) instructed
its owner to replace it with the enforcement assertions rather than relax it.
`TestSignedConditionIsRequirement_Enforced` is that replacement: the same two
clearing paths, the same observable facts, every assertion inverted — the solver
omission now REJECTED (1429) and SLASHED with no escrow and no payment to the
seller, and the fallback now ATTACHING and settling into a funded escrow that
HOLDS the payment.

### Gates

Sequenced so drift is attributable (Rule 7 step 2), each run in ISOLATION with
the tree fingerprinted before and after (Rule 9 step 4):

- **Baseline** at `0d7f167`, clean tree: `make lint test` EXIT=0.
- **Cause A — proto + regen ALONE:** 5 seeds × 3 runs, **byte-identical to the
  D-082 baseline** (seed 1 `245EF177…`, seed 2 `B48BDA6D…`, seed 7 `FA562B1D…`,
  seed 12 `E491FB48…`, seed 42 `6596CCA1…`), EXIT=0, fingerprint `049cf647…`
  identical before and after. **No re-baseline.**
- **Cause B — the semantic + fallback wiring:** same 5 hashes, EXIT=0,
  fingerprint `84e17012…` identical before and after. **No re-baseline.**
- `make lint` + `make test` green on the shipping source.
- **`scripts/exit_demo.sh` on a live node: `EXIT_RC=0`, PASSED, with ZERO edits
  to the demo.** Its staying green was recorded as a PREDICTION before the run
  (both parties sign at `exit_demo.go:313-316`; stage 7's negative keeps its 1427
  identity because attaching to a NONE-signing buyer is still the attach
  direction) — a red there would have been a finding about the semantic's reach,
  not a fixture to adjust. Stage 7 printed its consent-gate and wire-identity
  assertions unchanged.

**Named covering gate (Rule 7 step 5) — the vector covers NOTHING here.** This is
a **Category-1 (GENERATOR)** shadow throughout: the sim's intent generator never
sets `IntentPayload.condition` or the new `IntentLeg.condition` (zero occurrences
in `x/session/simulation` or `solver/`, test or non-test), so every path this
task adds is structurally unreachable by the vector under any seed count. More
seeds buy nothing; only a wider generator would (PHASE3_PLAN 3.5). Byte-identical
across both causes is therefore evidence of **ADDITIVITY, never of correctness**.
The primary gates are the hand-built fixtures in
`x/session/keeper/condition_binding_test.go` plus the replaced boundary pin, with
`scripts/exit_demo.sh` covering the live attach path and the vector demoted to
regression backstop.

### Forward items, written INTO their receiving tasks (Rule 6 sub-rule 6)

- **2.15 (NEW)** — §6.4 rule-3 per-leg attribution: the TODO-9 intent tag and the
  1428 counter-asset designator, with D-074 recorded as unaffected.
- **2.14** — the reference-solver row now carries the slashable-reference-solver
  consequence above.
- **2.9** — now carries the consequence that enforcement makes conditioned
  settlements MORE common, so its unconditional `condition_outcome = NONE` stamp
  is wrong more often. Accepted there, not pointed at it.

### Deviation surfaced (Rule 8)

`docs/`, `CLAUDE.md` and `docs/METHODOLOGY.md` were edited AFTER the isolated
cause-B vector run, by the approved sequencing (source + gate first, so
attribution is clean by construction). The shipping fingerprint is therefore
`f6bb6692…` while the gate ran at `84e17012…`.

**That difference is PROVEN Markdown-only, not asserted to be** — which is what
Rule 9 step 4 exists for, and which D-093 could only state in prose:

```
$ git diff -- ':(exclude)*.md' | sha256sum
84e170121a45f112d7ff8af66f84d7f37fcfbb8b32d0e2187490623d221d9499
```

The shipping tree's source-only diff hashes to **exactly** the full-diff
fingerprint recorded at the gate, so not one source or test byte changed after
the vector ran. "The gate ran on the shipping tree" is the single most
load-bearing claim in the checklist; here it is re-checkable with one command.

**This is now the close-out standard, not a one-off.** Promoted at review into
METHODOLOGY Rule 9 step 4 as a TWO-fingerprint discipline — full-tree DURING the
run (it has caught real process errors twice), source-only at CLOSE-OUT to
justify the post-gate documentation edit that honest sequencing makes
inevitable. The amendment is also the first in METHODOLOGY's history that came
from a rule being FOLLOWED rather than failing: D-093 obeyed Rule 9 and still had
to end in a sentence asking the reader to trust it, which is the signal a rule
has one more step in it.

## D-095 — Task 2.16 Phase-2 boundary record reconciliation: 20 post-2.7 stale statements corrected under a recorded load-bearingness rule; the §6 exit criterion re-verified LIVE at HEAD (inference → evidence); PHASE2_DESIGN Rev 4; **PHASE 2 DECLARED EXITED** with a six-task tail under one uniform rule

- **Date:** 2026-08-02 (boundary reconciliation at `7283b11`. Numbered as a
  ledger task, unlike D-090's unnumbered reconciliation — see "Numbering"
  below. One banner's string literals are the only behavioural change; the
  standing rule that a CODE defect found here becomes its own numbered task
  rather than an inline fix was carried, and **none was found.**)
- **SPEC:** §10 (the Phase-2 paragraph whose completion is being determined),
  §9 (edge services off-chain — FLAG C), §13 (the greppable-marker convention);
  METHODOLOGY Rules 6, 7, 8, 9 + the §11 boundary checklist; PHASE2_DESIGN
  Rev 3 (D-090); D-091/092/093/094; PHASE3_PLAN §3.0/§3.1.1/§3.1.2/§5.1.

### The determination that opened the task

Phase 2's BUILD was finished; its RECORD was not. All five SPEC §10 tasks are
complete against their own acceptance criteria (2.5's criterion 5 — the skim
coherence re-check owed to 2.6 — was individually verified as discharged at
D-082, not assumed), and the §6 criterion was asserted at §6b. **Nothing open
was exit-blocking.**

**The exit test, recorded so it is re-applied rather than re-argued:** a task is
EXIT-BLOCKING iff SPEC §10's Phase-2 paragraph — the five named tasks plus its
one-sentence exit criterion — cannot be truthfully asserted without it.
Importance drives SEQUENCE; only the SPEC's text drives the GATE. Applied to
2.8/2.9/2.10/2.12/2.14/2.15 it **CONFIRMS** each "off the exit path" row rather
than inheriting it — which is what makes the boundary a determination rather
than a restatement.

### The finding: a correction that propagated four-fifths of the way

Task 2.7 (`7283b11`, D-094) inverted a security semantic — a signed delivery
condition became a REQUIREMENT rather than a permission ceiling — and correctly
propagated the correction into CLAUDE.md, both edge contracts, and
PHASE2_DESIGN's §0 row and §2.7 section. **It stopped there.** Twenty
present-tense statements across five files still asserted the pre-2.7 world:

| File | What it still asserted |
|---|---|
| `PHASE2_DESIGN.md` | the §0 callout listing 2.7 as open (and omitting 2.15); §2.1's `IntentLeg` clause; §5 row 9 (owner 2.7, "collides with D-074"); §6's leg table ("§6b still open"), its GAP block, and FLAG 7 ("the REQUIREMENT direction is not enforced at all"); the closing "Next action: the §6 exit demo" |
| `PHASE3_PLAN.md` | **§3.1.1, the AUDITOR-FACING scope**: "`IntentLeg` has no condition field", plus a one-directional description of the consent model |
| `EXIT_DEMO.md` | the timing-knob rationale; boundary 3 ("reachable live ONLY through demo tooling"); the whole condition-attaching-proposer section and its GAP verdict |
| `cmd/conclave-demo/exit_demo.go` | package doc: three false clauses + a citation to a DELETED test |
| `cmd/conclave-demo/exit_demo_ibc.go` | **a banner the exit-criterion run PRINTS**: "the network still has no such writer. Verdict: GAP; enforcement = task 2.7" |

**Every one of them UNDER-claims** — describing a protection as absent when it
is present. Harmless to a user; **actively harmful to an auditor**, because
§3.1.1 steered a reviewer away from `x/session/keeper/condition_binding.go`,
297 lines of the newest and least-reviewed consent code in the tree. That is
METHODOLOGY Rule 9's own warning realised: *a stale inventory is worse than no
inventory.*

### DECISION 1 — the load-bearingness rule (so the next session applies a test, not a taste)

Three options were weighed: correct-in-place everywhere (cheapest, but it would
destroy the record of the 2.13 GAP finding, one of the project's most
instructive results and the thing D-094's design rests on); keep-and-mark
everywhere (faithful, but it puts struck blocks in three documents for
statements nobody would re-derive a decision from); or **split by
load-bearingness — ADOPTED**:

> **A false statement someone could re-derive a DECISION from is KEPT, MARKED,
> and corrected. Incidental description is corrected IN PLACE.**
>
> **Every keep-and-mark block must state WHAT SUPERSEDED IT AND WHERE** — not
> merely "~~pre-2.7~~" but a forward pointer naming the ADR and the code. **A
> struck block without a forward pointer still reads as CURRENT to a skimmer**,
> which defeats the purpose of retaining it.

Kept-and-marked (4): PHASE2_DESIGN §6's GAP block and FLAG 7, EXIT_DEMO's
proposer section, `exit_demo.go`'s package doc. Corrected in place (15).

**An unplanned consequence that turned out to be load-bearing.** The convention
chosen for keep-and-mark — a Markdown blockquote — is precisely what makes the
new citation sweep DISCRIMINATING, since a superseded block legitimately quotes
a deleted test. The Rule-8 formatting convention and the anti-staleness check
reinforce each other. That was not designed, and it is recorded so it is not
accidentally undone.

### DECISION 2 — 2.16 writes the §5.1 rows; 3.0 re-verifies them

PHASE3_PLAN §5.1 is headed "The table (nothing floats)" and had **no row** for
2.8's structural fork, 2.9, 2.13, 2.14 or 2.15 — four of them open. 3.0's
acceptance is a row-by-row re-verification, which **structurally cannot surface
a row that is missing.** That is AUDIT F-8's mechanism reproduced inside the
receiving document. The rows were therefore written at 2.16, BEFORE 3.0 runs,
and 3.0's criteria now require re-deriving the row SET from CLAUDE.md's ledger
and the marker census rather than verifying whichever rows it happens to find.

### DECISION 3 — the citation sweep, and the honest tradeoff inside it

METHODOLOGY §11's boundary checklist carries "every cited test name and
file:line still resolves" as a MANUAL item — so it was reproducible only by
whoever reinvented the command, and this audit duly found a citation to
`TestSignedConditionIsPermissionNotRequirement_BoundaryPin`, which 2.7 DELETED,
live in three places. The sweep is now a documented command in PHASE3_PLAN
§3.1.2's fenced block. **Markdown only; no `scripts/` artifact** — that is
deliberately 3.0's call once it has run the block by hand and knows what it
wants to freeze.

**The check was red-forever in its first form, and that was FIXED rather than
tolerated** (Rule 7 step 4: a gate whose false-positive rate is not driven to
zero is one the team learns to ignore). Two mechanisms keep it discriminating: a
`grep -v '^>'` filter for prose documents, and a **named allowlist** for the
CLAUDE.md ledger rows, whose keep-and-mark convention is
strikethrough-plus-forward-pointer because a blockquote cannot nest cleanly in a
checklist item. **Widening the filter until the check went green would have
blinded it to real re-citations**; naming the exception — with its four
legitimate locations and the explicit note that a fifth IS a finding — keeps it
sharp. The cost is a list that must be maintained, the same cost every findings
list carries (Rule 6), stated rather than hidden.

### DECISION 4 — 2.16 declares Phase 2 EXITED

Making 3.0 adjudicate whether the phase it is revving has ended is the wrong
division of labour. 2.16 records the exit, the tail rule and the roster; 3.0
plans from a settled boundary.

### DECISION 5 — the tail is ONE UNIFORM RULE, not six exceptions

**2.8, 2.9, 2.10, 2.12, 2.14, 2.15** remain open, none exit-blocking. They keep
their `2.x` numbers: **a number is an identifier, not a schedule**, and
renumbering would break roughly fifteen `DECISIONS.md` cross-references plus the
in-tree markers.

**2.12 — a premise corrected on the record (Rule 8).** The task arrived framed
as *"2.12's ledger row says it is gated on the D6 fee-economics model, and D6 is
PHASE3_PLAN 3.9, so 2.12 cannot close in Phase 2."* **No document creates that
gate.** An exhaustive grep for `D6` returns SPEC §14's tokenomics decision
(→ 3.9) and one occurrence in CLAUDE.md's §6a row where "D6" is that task's
INTERNAL plan-stop decision label (the same series as its "D1" split decision).
2.12's row calls it a fee-MODEL question in the sense of **D-050/D-051** —
Conclave ADRs already decided — and its plan-stop question (*is an authorized
`svc/{schema}` mint or a quantity-less `data/{hash}` grant skimmable at all*) is
answerable from **D-048** with no tokenomics model. **2.12 is self-gated and
could close in Phase 2.**

The requester identified the error's mechanism themselves and asked for it on
the record: **a plausible cross-reference generated from adjacent context** —
the third instance in one conversation of a tree-fact asserted that was not in
the tree, each time by the same mechanism. It is kept here because the mechanism
generalises well beyond this instance, and because it is exactly what "verify
against the tree, not against narrative" exists to catch.

**The disposition adopted is neither of the two offered** (exit with 2.12
OPEN-AND-BLOCKED, or re-home it to Phase 3): both are special cases, and **a
special case is how a ledger acquires exceptions nobody can re-derive.** 2.12
takes the uniform tail rule plus an explicit new **SEQUENCING DECISION**:
*sequenced after 3.9 by decision, because its plan-stop question is an economics
question 3.9 answers.* That is checkable. **"Blocked on D6" would have been a
floating phrase asserting a dependency no document creates — precisely the
failure that produced external audit finding F-3**, on the very table built to
prevent it.

### The §6 exit criterion: inference → EVIDENCE

§6b's last recorded run was `1aecfbf`. `0d7f167` then edited its driver and
`7283b11` inverted the consent semantic; **both tasks ran `exit_demo.sh` (§6a)
only.** So "the criterion holds at HEAD" had come to rest on §6a-green-at-HEAD
plus an additivity argument — while EXIT_DEMO.md's own closing line claimed
"both scripts are run at every close."

**The claim was repaired by making it TRUE, not by softening it.**
`scripts/exit_demo_ibc.sh` was re-run at HEAD: **EXIT_RC=0** — two chains plus a
live `cosmos/relayer` v2.6.0, the buyer holding `ibc/8E27BA2D…`, the ZK escrow
holding and paying out the voucher, and §6.3 check 1 verifying the receipt
against chain B's stored root `895E6F71CA1E84B0…` read from B's on-chain
`ConsensusState`, with the off-by-one negative failing the root check
specifically. The corrected banner printed. EXIT_DEMO.md now states both the
lapse and the rule: `exit_demo.sh` keeps §6a honest; **`exit_demo_ibc.sh` keeps
the EXIT CRITERION honest**, and any future task touching `cmd/conclave-demo`,
`x/session/keeper` or `x/escrow` that runs only the former repeats this lapse.

### Marker census — Rule 6 sub-rule 3's worst case, one task after the rule was written

Re-derived from the tree: **15 markers** (2 `phase-2`, 13 `phase-3`). Rev 3's
phase-3 sweep held — all 13 still name a live owner. **One was unowned:**
`solution.go:143` named **task 2.7, which had CLOSED**, and its text asserted a
D-074 collision D-094 had already ruled non-existent and recorded as "must not
be re-litigated." Retargeted to **2.15(a)**.

METHODOLOGY Rule 6 sub-rule 3 names this exact case — *"a marker whose named
task has COMPLETED is the worst case: it reads as pending while being unowned,
and it is invisible to 'is anything unowned?' checks"* — and it reproduced **one
task after the rule was written.** The lesson taken is not "be more careful":
the census is now a scheduled sweep in §3.1.2, run from the tree at every
boundary rather than trusted from a table.

### SCOPE ADDITION, surfaced mid-build (Rule 8)

`solution.go:154` documented §6.4 rule 6 as **"NONE conditions only"** — false
since **2.1a** (D-073/D-075), older than 2.7 and outside the planned S-list
fence. Corrected in place rather than left standing four lines from a comment
being fixed, and surfaced at the moment of deviation rather than at close. Same
defect class, zero behavioural reach.

### The two scope facts the checkboxes did not show

Recorded in PHASE2_DESIGN §6 because a reader of the ledger would not see them.
(a) **Neither edge service exists in any repo** — SPEC §10 rows 2.3/2.4 are
satisfied by their in-repo deliverable, the recorded FLAG-C narrowing grounded
in SPEC §9, which is legitimate; but 2.3's acceptance clause *"is the interface
the separate repo builds against"* is **unverifiable today**, and no document
said so. (b) Consequently the criterion's "external x402 agent" is
`cmd/conclave-demo` in the Model-A gateway shape — the word "external" is
carried by an emulation of the node-side auth model, not by an x402 HTTP
conversation.

### Gates

Sequenced source → gate → docs → live → sweeps, so attribution is free.

- **Baseline** at `7283b11`, clean tree: lint 0 / test 0 / vector 0; five seeds
  byte-identical to the D-082 baseline.
- **Source stage in ISOLATION** (`solution.go`, `exit_demo.go`,
  `exit_demo_ibc.go`): `make lint` **0**, `make test` **0** (0 FAIL),
  `make sim-determinism` **0** — seeds `245EF177…`, `B48BDA6D…`, `FA562B1D…`,
  `E491FB48…`, `6596CCA1…`: **byte-identical to the D-082 baseline, no
  re-baseline.** Fingerprint `206eece7…` identical before and after.
- **`scripts/exit_demo_ibc.sh` at HEAD: EXIT_RC=0** (detail above).
- **Five acceptance sweeps, all pass**: marker ownership (15/15 owned by open
  tasks), citation resolution (0 findings after the named allowlist),
  forward-item union (6/6 tail tasks have a §5.1 row), hash traceability (14/14
  Phase-2 commits cited), superseded-claim scan (0 unmarked).

**Rule 3 does not apply, and saying so beats a ceremonial red:** this task adds
no guard, so there is nothing to sabotage. **The honest analog is that all five
sweeps were RED at HEAD before the task — that is how the findings were
produced — and are green after.**

**Named covering gate (Rule 7 step 5) — the vector covers NOTHING here.**
Comments and demo strings cannot reach consensus state. The vector ran as a
regression backstop and because Rule 7 step 1 admits no exceptions; the gates
that actually cover this change are `scripts/exit_demo_ibc.sh` at HEAD and the
five sweeps.

**Two-fingerprint close-out (Rule 9 step 4).** Docs were edited after the
isolated source gate, by the approved sequencing. The shipping tree's
source-only diff:

```
$ git diff -- ':(exclude)*.md' | sha256sum
206eece717f2547fdf5f997e29b0384b5f662d4b502162b0a0d20e935a70ba85
```

equals the full-diff fingerprint recorded at the gate **exactly**, so not one
source or test byte moved after the vector ran.

*The shipping FULL-diff hash is deliberately not recorded here: writing it into
this ADR would change it. That self-reference is precisely why the source-only
hash is the one that carries the proof — it is invariant under any Markdown
edit, including this one.*

### Numbering: a deliberate divergence from D-090's precedent

D-090 was the same shape — a reconciliation touching docs plus marker re-tags
across 27 files — and deliberately carried **no** task number. 2.16 diverges and
takes one. **Declining to number the task that fixes work-gone-missing, when
"unnumbered work goes missing" is D-090's own headline lesson, would be an
unforced irony**; and unlike D-090 this task carries a live gate (§6b) and a
source component that a docs-pass framing would under-weight.

### Forward items, written INTO their receiving tasks (Rule 6 sub-rule 6)

- **3.0** — four obligations accepted into its criteria, not pointed at it: run
  §3.1.2's re-derivation from the tree; **read §3.1.1 as a CORRECTION TARGET,
  not only a diff base** (a grep-diff surfaces additions and cannot surface that
  existing prose went FALSE — exactly how §3.1.1 came to misdescribe the consent
  model eight days after FW-2 landed); re-verify the §5.1 row **SET**, enumerated
  from the ledger; and treat a task's NARROWING edits as the trigger to re-check
  every item pointed at it (D-093's input — the Rule-6 one-way-pointer failure
  has now recurred a fourth time).
- **2.15(a)** — receives `solution.go:143`'s marker, with D-074 recorded as
  orthogonal and not to be re-litigated.
- **2.14** — carries the only live functional defect in the tail: non-test
  `solver/` contains **zero** reads of `Condition`, so the reference solver would
  propose a slashable (5%) solution against a condition-signing intent.
- **2.9** — remains the hard prerequisite for external-facing use of the §6
  evidence, widened by 2.7.
- **§5.2's three advisories** marked SPENT: 2.3/2.4/2.5 have all closed.

### The transferable lesson

**A correction propagates to the documents its author is holding, and stops.**
2.7 did the hard part — inverting a security semantic with watched reds and a
replaced pin — and still left the auditor-facing scope telling a reviewer the
opposite. The failure is not diligence; it is that **nothing enumerated the
consumers of the claim being corrected.** The remedy is mechanical and now
scheduled: the citation sweep, the marker census and the row-SET re-derivation
all run from the TREE at every boundary. **Candidate METHODOLOGY amendment, for
3.0 to rule on: Rule 8 step 3 ("propagate the correction to any document that
repeated it") needs the sub-rule Rule 6 already learned — enumerate the
consumers FIRST, because a correction propagated from memory reaches exactly the
documents you remember.**

**Its MECHANICAL FORM, added at the review stop, because the mechanism is what
makes an amendment fire and a principle without one is advice:**

> After a task that STRENGTHENS a guarantee, grep for assertions of the OLD
> guarantee's **NEGATION** — `"not enforced"`, `"never attaches"`, `"has no X
> field"`, `"only through"`, `"is a … ceiling"` — **NOT for the new mechanism's
> name.** Stale text by definition does not mention the new thing, so searching
> for `condition_binding.go` or `attachFallbackCondition` returns precisely the
> documents already corrected. The negation is what the stale text actually says.

**THE MECHANISM WAS TESTED RATHER THAN ASSERTED, AND IT FOUND ONE THE MANUAL
PASS MISSED.** A proposed rule that is never run is a hypothesis, so the negation
grep was executed against `7283b11` (the pre-2.16 tree) with `git grep`:

| Run | Result |
|---|---|
| **(A) NEGATION grep at `7283b11`** — the proposed mechanism | hits **all five** stale files *plus* `PHASE3_PLAN.md` — every stale statement, **including §3.1.1**, the highest-value one |
| **(B) NEW-MECHANISM-NAME grep at `7283b11`** — the intuitive search | returns `CLAUDE.md` + the two edge docs: **exactly the three documents 2.7 had already corrected, and zero stale ones** |

(B) is the asymmetry proven rather than argued. Searching for
`condition_binding.go` / `attachFallbackCondition` returns precisely the
already-fixed set, because **stale text by definition does not mention the new
thing.** The intuitive search is not merely weaker — it is *anti-correlated* with
what you are looking for.

**And the sweep then caught a TWENTIETH statement the manual enumeration had
missed** — this ADR's own title was corrected from nineteen to twenty (Rule 8).
`EXIT_DEMO.md`'s §6b carry-over line read: *"the **INTERNAL** classification, the
epoch-0 plaintext Veil, and 'conditioned escrows are reachable live only through
demo tooling' (task 2.13) all carry over from §6a unchanged."* The §6a boundary
list had been corrected; this §6b line **re-asserted the same claim 190 lines
later**, and a careful reading of the document walked straight past it. Fixed
in-task.

That is the whole argument for the amendment, demonstrated on itself: a careful
manual pass by the person holding all the context missed one, and a one-line
command did not. It is also a second instance of the session's governing
finding — **the manual pass reached the documents I remembered.**

**And the STANDARD to rule it by, which generalises past this amendment: a rule
that depends on REMEMBERING does not fire; a rule with a RUNNABLE STEP does.**
Two independent data points from this one session say the same thing — Rule 6
sub-rule 3's explicitly-named worst case recurred **one task after the rule was
written**, and what fixed it was a SCHEDULED SWEEP rather than a resolution to be
careful. Advice has a measured failure rate in this repo; commands do not. Both
the amendment and the standard are written into **3.0's criteria as obligation
5**, not pointed at it.

---

## D-096 — Task 3.0: the Phase-3 plan Rev at the Phase-2 boundary. A toolchain spike run BEFORE any planning REFUTED FLAG P3-B's constraint half at the artifact — the platform was never trapped; 3.3 SPLITS; all six written-in obligations discharged; PHASE3_PLAN becomes **Rev 1, the phase authority**

**Status.** ACCEPTED. Docs-only — **no pin and no source byte changed.**
Baseline `15c2470` (clean tree); `make lint` 0, `make test` 0 (17/17 packages),
determinism vector PASS on 5 seeds byte-identical to the D-082 baseline (verified
line-by-line against `DECISIONS.md:3300-3304`, not asserted), tree fingerprint
`e3b0c44…b855` (empty) before and after.

### Context

`PHASE3_PLAN.md` was authored mid-Phase-2 (D-080) as a PROPOSED plan and became
executable only after (a) the Phase-2 ledger closed and (b) a Rev-at-boundary
reconciliation re-validated it against as-built state. (a) landed at task 2.16
(D-095). This is (b). Task 3.0 carried **six obligations written INTO its
acceptance criteria** rather than pointed at it, plus a reviewer-directed
instruction to **run a toolchain spike before any planning**, on the reasoning
that the plan written before the spike would be the plan the spike invalidated.

### The spike, and why it changed the phase rather than informing it

Run on a throwaway branch off `15c2470`; the only tracked edit was `go.mod`'s two
directive lines; everything else ran in `/tmp` copies (`git archive`, no `.git`).
Two-fingerprint discipline was carried INSIDE the spike — the working-diff hash
was recorded before and after the vector run (`9ebb6df8…`, identical). Branch
destroyed; post-teardown fingerprint equals the pre-spike baseline.

1. **Bumping only the `go` directive to `go 1.25.0`/`toolchain go1.25.12`, with
   every pin untouched:** build 0, vet 0, `go test ./...` 0 (17/17), crypto 24/24,
   **determinism vector byte-identical to D-082** across two processes.
2. **The 2025.1 stack** (sdk v0.53.8 + cometbft v0.38.24 + wasmd v0.60.8 + ibc-go
   v10.7.0) resolves **MVS-clean**, `go mod tidy` 0, directive settling at exactly
   **`go 1.23.8`**, and the census of modules declaring above 1.23.8 came back
   **EMPTY** — the transitives nobody had checked.
3. **What actually binds:** at 1.25, nothing. At 1.26, **`bytedance/sonic
   v1.14.0`** — `undefined: GoMapIterator`, a `//go:linkname` into runtime
   internals removed by Go 1.26's map rework, reaching us through
   `conclave/app → cosmossdk.io/log → sonic`, with `sonic/internal/rt` inside
   `conclaved`'s own import graph. **Not kyber. Not block-sdk.**
4. **The pinned assembly-bearing `kilic/bls12-381 v0.1.0`** produces a
   **byte-identical KAT (`6d98cd34…`) under go1.23.12 ≡ go1.25.12 ≡ go1.26.5**,
   and — the stronger result — **byte-identical under `-tags generic`** against its
   own pure-Go fallback. `TestKyberRecoverCommitSelectsLowestIndex` (the T2
   premise) passed **50/50 per toolchain**, dosed per Rule 3 step 7.

### Decision 1 — FLAG P3-B's constraint half is REFUTED, and the correction runs in the expensive direction

> ~~"the kyber pin (D-066) is itself what holds the project on the EOL toolchain
> … an ACTIVE conflict, not a future one."~~

**A `go` directive is a MINIMUM, never a maximum.** `kyber-bls12381 v0.3.1`
declares `go 1.18`; `kilic/bls12-381 v0.1.0` declares `go 1.13`. Neither imposes
any ceiling. D-066's pin genuinely avoided a **FORCED** bump and was correct in
Phase 1 — it never prevented a **VOLUNTARY** one, and that inference is what was
wrong. The flag had been carried, cited and built on across multiple documents
for three weeks **and had never once been tested against the artifact it
described.**

The other half of P3-B is CONFIRMED and worse than written: `govulncheck -mode
binary` finds **16 reachable-symbol vulnerabilities** in the shipping binary, so
the original *"support-horizon decision, not an exploit fire"* is FALSE
stack-wide and true only of gnark.

**The recorded block-sdk hypothesis is also REFUTED**, and is held to the same
standard rather than quietly dropped: block-sdk v2.1.5 compiles clean under both
1.25.12 and 1.26.5, and against SDK v0.53.8. Stated precisely — **compiling is
not behaving**; lanes and `PrepareProposal` remain 3.3b STOP #2's evidence list.
But the inherited framing asserted a compile/MVS *impossibility*, and that is
false.

### Decision 2 — 3.3 SPLITS, and 3.3a is pulled ahead of the Phase-2 tail

The spike **decoupled** two axes the original 3.3 fused. Security patching
(toolchain, CometBFT v0.38.25, the reachable advisories, golangci-lint, the
F-5/F-6/EOL CI guards, iavl-in-isolation) is now **separable** from the
SDK-family migration. Fusing them would hold 16 reachable advisories hostage to a
multi-week migration.

- **3.3a** — single-stop; effectively zero dependency churn; risk **measured**
  (green on the full suite, vector-identical). Sequenced **ahead of the Phase-2
  tail except 2.14**, because a security-patch vehicle does not queue behind
  reference-solver ergonomics.
- **3.3b** — two-stop, unchanged shape, **still gates 3.8**. A patched toolchain
  on an EOL SDK is still an EOL SDK. The spike did **not** retire 3.3b's risk:
  its evidence is **compile-only**.

**Honest limits, binding on every row that leans on them:** full gates ran on
**1.25.12 only**; under 1.26.5 only build/vet + crypto + KAT + T2 ran, so *"1.26
is green"* is NOT established — only *"1.26 compiles once sonic moves, and the
crypto path is bit-identical."* The 2025.1 build was compile-only: no tests, no
vector, no CGO/libwasmvm packaging, no lane/VE/reveal behaviour.

### Decision 3 — the six obligations, discharged with evidence

1. **§3.1.2 re-derivation run from the tree, all seven steps.** Findings F-10…F-13.
2. **§3.1.1 read as a CORRECTION TARGET.** Every existing bullet re-verified
   against code. **One correction: the D-089 `recover()` is in `rateWithinBound`
   (`solution.go:617`), NOT in `checkLimitRate`** — an auditor sent to the named
   function would find no recover there (**F-11**). One sharpening: D-088's
   invariant now has **three** implementations (**F-12**).
3. **§5.1 row SET re-derived** by enumerating from the ledger, not by reading the
   table. All six open tail tasks have rows (2.16's work confirmed); **ten items
   had no row at all** and were added.
4. **Narrowing edits re-checked.** 2.7→2.15 and 2.13→2.14 both carry their items
   **written into the receiving task**. Rule 6 sub-rule 6 satisfied; the fourth
   recurrence did not happen.
5. **The D-095 amendment: ADOPTED**, mechanical form verbatim (below).
6. **The `6d6b730` calibration: obligation 3's mechanism is PROVEN.**

### Decision 4 — the calibration result, recorded because its value is diagnostic

`6d6b730` appears exactly twice in the corpus, **both inside §3.0's own
description of the omission**; it is still absent from CLAUDE.md's 2.16 row and
PHASE2_DESIGN's scoreboard. A row-by-row pass could never have surfaced it. The
pass that did enumerated from an **independent source** (`git log`) and asked
whether the documents covered it — and **also surfaced something nobody seeded:
HEAD `15c2470` is cited nowhere.** That confirms the plan's own prediction that a
follow-up commit *relocates* the gap. **Obligation 3 needs no sharpening; the
recurring-by-construction hygiene gap becomes F-13 with a runnable check.**

### Decision 5 — the METHODOLOGY amendment is ADOPTED, plus a companion

Ruled against the stated standard — *a rule that depends on REMEMBERING does not
fire; a rule with a RUNNABLE STEP does* — and it passes: it is a command, not
advice. Mechanical form carried verbatim into PHASE3_PLAN §8.1.

Rev 1 adds a **third data point, the strongest of the three**: P3-B's refutation
came from running `govulncheck` and reading four `go.mod` files, not from anyone
re-reading the flag. Hence a **companion rule**:

> **A FLAG is a CLAIM, and flags accumulate authority precisely because nobody
> re-checks them.** At every boundary, ask of each flag: *what artifact would
> falsify this, and has anyone run it?* **Flags asserting a CONSTRAINT get
> priority** — they delete options, and their cost is invisible because nobody
> plans the work a constraint forbade.

**Neither edit is made here.** Amending METHODOLOGY inside a plan Rev would be
precisely the silent cross-document edit Rule 8 step 3 is about. That is **F-14**.

**The adopted rule immediately caught this Rev's own stale text.** Splitting 3.3
left **eight §5.1 rows and one §2.1 line** routing to a task that no longer
existed as one unit — surfaced by running the negation sweep against the Rev's
own corrections, not by re-reading it. All nine were re-routed.

**That instance WIDENS the rule's trigger class, and the generalisation is worth
more than the data point.** D-095 stated the amendment for a **semantic
strengthening**: *"after a task that STRENGTHENS a guarantee, grep for the OLD
guarantee's negation."* This Rev's case was **not** a strengthening — it was a
**structural rename/split**, one referent (`3.3`) becoming two (`3.3a`, `3.3b`) —
and the mechanism fired anyway. Generalised:

> **Any change that RENAMES or SPLITS a referent leaves the old referent behind in
> every document that pointed at it, and the intuitive search is systematically
> ANTI-CORRELATED with finding them.** Searching for the NEW name (`3.3a`,
> `3.3b`) returns exactly the sites already updated — **by construction**, since
> stale text cannot mention a name that did not exist when it was written. The
> stale sites are reachable only by searching for **what they still say**: the OLD
> name, or the OLD guarantee's negation. **Two distinct triggers, one mechanism.**

**Consequence for F-14:** the METHODOLOGY amendment owed under Rule 8 step 3 must
be written to cover **both** trigger classes — *strengthened a guarantee* **and**
*renamed or split a referent*. Writing it narrowly to the semantic case D-095
happened to observe would leave the structural case unowned, which is the same
failure the amendment exists to fix, one level up.

### Consequences

- `PHASE3_PLAN.md` is **Rev 1** and **the phase authority**; §3.0 is CLOSED.
- **Cross-document corrections propagated under D-095's load-bearingness rule**
  (kept + marked + forward pointer, because a session could re-derive a *decision*
  from them): **PHASE2_DESIGN §4's invariant** — which survives only in its
  literal form, as a statement about adopting `v0.3.4`/`gnark v0.14+`, never as a
  constraint on the `go` directive — plus **§FLAG D** and **CLAUDE.md's 2.2 row**,
  whose "kyber collision" reason was wrong even though 2.2's *decision* stands on
  block-sdk alone.
- **D-077's and D-081's dependency tables both skipped the `wasmd v0.60` line**
  (v0.60.8 → SDK v0.53.6 + ibc-go v10.5.0 at floor `go 1.23.8`). Recorded here
  rather than by rewriting those ADRs.
- **D-089's own text calls `checkLimitRate` the panic surface** — same imprecision
  as F-11, corrected here rather than by editing the historical ADR.
- New: **G-1** relayer ops · **G-2** gas/DoS pricing (ranked highest; a
  pairing-bound Reveal in BeginBlock paid for by nobody, on an exchange chain, now
  with a first cost datum of **5.3 ms/intent**) · **G-3** validator key custody ·
  **G-4** load testing · **F-9…F-14**.
- **F-9 is two defects in one test**: `TestRevealCostWithinBound` printed *"the
  share gate / IBE open regressed"* — **a diagnosis it had not established** — and
  its `recommendedCap` derives an **O4 consensus parameter** from an unguarded
  wall-clock measurement that swung **5 vs ~56** on machine load alone. Routed as
  **G-4 evidence**, not as a test cleanup.
- **O1 re-scoped** via the CHK/BCHK placement — carried as the auditor's
  **STARTING POINT phrased as a task**, explicitly NOT an established premise,
  because an auditor handed a settled framing may let it do all the work. And
  `AUDIT_SCOPE §7`'s *"confidentiality holds against any coalition of < t"* is
  **FLAGGED for explicit re-examination** against PKC-2026's non-monotone
  corruption result — flagged, not withdrawn.
- **§3.5 widened** from one shadow to a per-shadow verdict over all five.
- **All 8 `recover()` sites** enumerated in §3.1.1 with per-site auditor notes,
  against the one the section previously named.

## D-097 — Task 2.14: the reference solver's delivery-condition attachment path — the last live functional defect of the Phase-2 tail CLOSED; and the SPEC §6.4 bond slash found not to land on the tx path (NEW task 2.17)

- **Date:** 2026-08-03 (task 2.14, after an approved plan-stop with two ratified
  decisions).
- **SPEC:** §4 (`solver/`), §6.4 (solution validation, slashing, the fallback),
  §6.5 (condition classes), §11.6 (determinism); builds on **D-054/D-055** (the
  solver reuses the chain's validation primitives verbatim), **D-074** (the
  bilateral conditioned shape), **D-075/D-076** (the consent gates), **D-085**
  (one source of truth), **D-086** (the fallback's synthetic Solution),
  **D-089** (candidate gates are not guarantees), **D-093** (the GAP verdict and
  the wire-code fix), **D-094** (a signed condition is a REQUIREMENT);
  METHODOLOGY Rules 1/2/3/4/6/7/8/9.

### The defect, re-derived from the tree rather than inherited

Non-test `solver/` contained **zero** reads of `Condition`. `match.DecodeOpenIntent`
discarded `IntentPayload.condition` outright, so `naive.buildPair` built a
mutually-fillable data-for-coin pair with a BARE DataGrant leg. That match passes
`gate.Accept` (conservation and price binding are indifferent to conditions), so
the pre-2.14 pipeline **admitted and proposed it** — and since D-094 the chain
rejects exactly that as `ErrConditionNotHonored` (session `1429`).

Confirmed reachable end to end before any code was written, and reproduced after
the fact by sabotage S6b (below), which returns `solver/` to its pre-2.14 shape
and makes the acceptance test fail on a nil condition.

### STOP decisions (both ratified before building)

**(1) Placement: in `solver/`.** Option (iii) — a boundary preserving the
simulation's import graph — was **established as impossible rather than argued
down**: `x/session/simulation` imports root `solver`, which imports
`cycles`/`gate`/`naive`, so anything `Solve` can reach is already in that graph.
Option (ii) (cmd/-side, the D-091 D4 precedent) was rejected because the defect
IS `solver.Solve`: an invalid constructor plus a repair step elsewhere is two
places to be wrong, and leaves the library entry point broken. **D-091's D4 was
read correctly and does not apply** — it excluded the demo's LIVE-NETWORK WRITER
(signing, broadcast, gRPC polling) from a pure matching library, not attachment
logic.

**(2) The consent predicate is CALLED, not mirrored.** `solver/match`'s standing
contract is that the MATCHING idea is reimplemented off-chain while the rules
that must not drift are reused verbatim (D-054/D-055). The condition binding is
a validation rule, and the asymmetry decides it: a mirrored predicate that
drifts one bit costs a third-party solver 5% of its bond, silently, on a path no
test of its own would show. `solver/gate`'s charter is verbatim "the rules under
the solver's control that would otherwise cost it a 5% slash" — a call site, not
a copy site. **D-085's lesson at a new boundary: one implementation, two
callers.**

### What was built

**Cause A — `x/session/keeper`, a pure extraction.** `CheckConditionBinding(m,
payloadFor)` is the exported, ctx-free walk that `validateConditionBinding` now
delegates to, supplying only a lookup closure; `EffectiveSellerCondition`
exports the FW-2 resolver. Behaviour, error identities, message text and check
order are unchanged. The buyer-side resolver is deliberately NOT exported —
a solver never needs it, and a function with no reader is decoration (Rule 4).

**Cause B — `solver/`.** `match.OpenIntent` gains `Condition` + `Payload()`;
new package **`solver/condition`** holds `Attach` (per DataGrant leg, the
GIVER's resolved condition, mirroring `attachFallbackCondition`),
`SignsDataCondition`, `CycleEligible`, and the participant/payload plumbing;
`gate` gains `AcceptConsent` and `AcceptConditionShape`; `Solve` attaches, then
gates, then admits.

### The mid-build finding that changed the design (Rule 8, surfaced at the moment)

Writing the cycle-exclusion test exposed a hole in the approved plan:
`AcceptConsent` covers 1427/1429 but **not D-074's shape rule** (`1426`). Under
the plan as approved, a conditioned leg inside an n-party cycle would attach,
pass conservation/price/consent, and be PROPOSED — with `CycleEligible` as the
only thing preventing a slashable proposal. That contradicts the very principle
the plan-stop endorsed: *the guarantee must not depend on the attacher being
exhaustive*. Closed structurally by exporting `CheckConditionedMatchShape` and
adding `gate.AcceptConditionShape`. **This added one keeper export beyond the
plan's "solver/ only" Cause B; recorded rather than absorbed.**

### CycleEligible is an EFFICIENCY guard, not the guarantee — stated, not discovered later

D-089's lesson applied to this session's own code rather than inherited: with the
shape gate in place, removing `CycleEligible` changes nothing observable
(sabotage S4a, green). What it actually buys is liveness — `cycles.Find` marks
intents used inside its own traversal, so a doomed conditioned cycle can consume
intents that would otherwise have formed a valid one. The guarantee is
`gate.AcceptConditionShape`. Saying so in the doc comment is the point: a
candidate gate whose removal is unobservable is an optimisation, and calling it a
guarantee is how a project comes to trust the wrong line.

### Load-bearing tests — PREDICTED, watched, and three predictions WRONG

Six sabotages, each predicted in writing before removal:

| | Sabotage | Predicted | Observed |
|---|---|---|---|
| S1 | `condition.Attach` removed | attach tests + the 5 "clears" + both PerLeg | **as predicted** (+ the leg-order test, added after the prediction) |
| S2 | `gate.AcceptConsent` removed | the 4 "dropped" + buyer-signed-seller-not | **as predicted** |
| S3 | resolver mirrored, not called | `TestPerLegConditionRefinesPayloadLevel` only | **as predicted, on the second attempt** |
| S4a | `CycleEligible` removed | nothing | **as predicted** |
| S4b | shape-gate call site removed | `BothMoves/ATTACHING` | **WRONG — nothing red** |
| S4c | both removed | `ConditionedIntentNeverEntersACycle` | **WRONG — nothing red** |

**S1 and S2 red DISJOINT subtest sets**, which is the evidence the attacher and
the gate are independently load-bearing (the D-094 loop-bound/predicate pattern).

**The three misses, kept visible:**

1. **S3 first reported "(no failures)" — and the harness was lying.** The
   sabotage replaced `EffectiveSellerCondition(giver.Payload(), key)` with
   `giver.Condition`, which leaves `key` and the keeper import unused: it did not
   COMPILE. The harness greped only for `--- FAIL`, so a build failure printed as
   "(no failures)" and **silence was indistinguishable from success**. Had the
   sabotage's plausibility not been re-checked against the code, this session
   would have recorded "the resolver call is not load-bearing" — the exact
   opposite of the truth, about the very decision STOP #2 turned on. Harness
   fixed to fail loudly on a non-building sabotage; re-run reds exactly one test,
   as predicted. **This is a METHODOLOGY amendment candidate (below).**
2. **S4b** was predicted wrong because removing a CALL SITE cannot red a test
   that calls the FUNCTION directly. The model, not the code, was wrong.
3. **S4c** was predicted wrong for a substantive reason: the obvious fixture has
   only the SELLER signing, so a conditioned cycle is rejected by CONSENT
   (`1427`) before the shape rule is ever the reason — the fixture could not
   isolate the rule it claimed to test. Fixed by
   `bothSigningConditionedCycle`, where both parties sign identically so every
   consent question answers YES and D-074 stands alone; S4c then reds as it
   should.

**Dosing.** D-089's `-count=25` rule was examined rather than applied by rote:
this suite's intent ids are literals, so repetition pins the SAME
`naive.Find` sort order every time and proves nothing about role assignment. The
fixture was varied instead (`TestAttachmentIsIndependentOfLegOrder`, seller
sorting first vs the default buyer-first). The app-level tests were still run
`-count=25`, green 25/25.

### FINDING F-1 — the SPEC §6.4 bond slash does not land on the tx path (→ NEW task 2.17) — **CLOSED by D-098; and the heading itself is OVER-BROAD, see the correction below**

`x/session/keeper/solution.go` calls `slashSolver` for an invalid solution (5%,
line 114) and for equivocation (20%, line 97) — **and then returns an error**, so
baseapp discards the failing message's cache context and the slash write is
reverted with it. Through the msg server a rejected proposal costs a solver
**gas only**. The comment at `msg_server.go:54` ("the slash commits with the
failing tx") is false.

**Verified on both paths, not inferred:** `1412` (invalid solution) and `1410`
(equivocation, branch confirmed reached by its own message) each leave
`SlashedCount = 0` and the bond untouched through real `FinalizeBlock` txs.

**Why a green suite never caught it:** every in-tree slash assertion calls
`k.ProposeSolution` on a RAW keeper context with no tx cache-wrap
(`x/session/keeper/solution_test.go:53`), where the write does persist. One path
verified, the sibling never enumerated — **METHODOLOGY Rule 4 step 5, and the
D-093 shape exactly** (18 assertions that passed under both shapes).

Severity: this is the D-024 incentive that makes "solvers are untrusted"
economically safe, and `max_solutions_per_session` does not bound invalid
proposals because they are never stored. Pre-testnet, no production solvers.

> **CORRECTION (task 2.17, D-098): "D-024" is a MIS-CITATION.** D-024 is "§1.3
> steps 2–3 demonstrated in two phases until task 0.7"; the slash bps are
> **D-007**'s invented params (`invalid_solution_slash_bps` 500,
> `equivocation_slash_bps` 2000). Corrected in place with the original kept,
> because this line is where the mis-citation propagated from — it reached
> CLAUDE.md's 2.17 row, PHASE3_PLAN §5.1, SOLVER_SURFACE §6.3 and a test
> comment. **Also over-broad, and this is the substantive half:** the finding
> below is about the two PROPOSAL-TIME slashes; the conclude-time
> `conclude_failed` slash runs in EndBlock on the parent ctx and was never
> affected (verified through a real block at 2.17).
**NOT fixed in this task** — a consensus-path charging change is 2.8-shaped work
and Rule 7 step 2 forbids stacking it on a task already carrying two vector
causes. Both halves are pinned in executable form
(`TestDroppingTheConditionIsRejectedAtTheWire`,
`TestEquivocationSlashAlsoDoesNotPersist`), each instructing its own replacement
when 2.17 lands.

### Gates

Sequenced so drift is attributable (Rule 7 step 2), fingerprinted before and
after each (Rule 9 step 4):

- **Baseline** at `42ac490`, clean tree: `make lint test` EXIT=0.
- **Cause A — the keeper extraction ALONE:** `make lint test` EXIT=0; vector
  **byte-identical to the D-082 baseline** (seed 1 `245EF177…`, seed 2
  `B48BDA6D…`, seed 7 `FA562B1D…`, seed 12 `E491FB48…`, seed 42 `6596CCA1…`),
  two independent processes; fingerprint `44d7163e…` identical before and after.
  **No re-baseline.**
- **Cause B — `solver/` + the shape export:** `make lint test` EXIT=0; the same
  five hashes; fingerprint `3f88b1e9…` identical before and after. **No
  re-baseline.**

**A DEVIATION, self-caught and reported (Rule 8).** Cause B's gate was started
BEFORE the D-095 negation-grep was run, and that grep then found a stale
statement in SOURCE — `cmd/conclave-demo/exit_demo_ibc.go` prints "the reference
solver … never reads IntentPayload.condition" live to an operator. Editing it
mid-gate would have broken the fingerprint proof, so the run was killed and
re-run from scratch after the edit. (The first run had also failed lint on an
unused helper, so a re-run was owed regardless — but the ordering error was
independent of that and is recorded on its own terms.) The unused helper was not
deleted but given a reader: it is what `TestEquivocationSlashAlsoDoesNotPersist`
needs to put two txs in one block, which is what moved F-1's second half from a
throwaway run to in-tree evidence.

**Named covering gate (Rule 7 step 5) — the vector covers NOTHING here.** A
**Category-1 (GENERATOR)** shadow, proven at the generator rather than by hit
count: `x/veil/simulation/operations.go` builds `IntentPayload{Give, Want}` and
never sets `condition`, and its `coinLeg`/`dataLeg`/`svcLeg` helpers never set
`IntentLeg.condition`. Every path this task adds is gated on a non-nil
condition, hence structurally unreachable under any seed count. Byte-identical
across both causes is evidence of **ADDITIVITY, never of correctness**. The
primary gates are `solver/condition_attachment_test.go` and
`app/solver_conformance_test.go`.

### The trap the acceptance test had to avoid, recorded because it nearly worked

Since D-094 the in-consensus fallback ATTACHES conditions. So an acceptance test
asserting only "a conditioned escrow exists" would have passed **with the defect
fully present**: the solver's downgrade is rejected 1429, zero valid solutions
remain, the session drops to FALLBACK, and the fallback settles it correctly. The
test would be green, the reference solver still broken, and nothing would say so.
The load-bearing observables are therefore the PROPOSAL's fate — tx code, the
solver's bond and `slashed_count`, and **whose** solution won
(`winning_solution_id`, distinguishing the solver's from D-086's synthetic
fallback Solution). Rule 4's "passes for the wrong reason", in this task's exact
shape.

### Deliverable

`docs/edge/SOLVER_SURFACE.md` — the solver-facing contract, in the
X402/7683 shape. Unlike its siblings it names no separate repo (none exists);
the in-repo reference is what it documents. §5 is the attachment contract
(requirement semantics, FW-2 resolution order, the per-class relations with
their derivations, the D-074 shape, the algorithm, the do-not-fill rule, and the
rejection table). §6 is the honest-limits section, and its load-bearing line is
**§6.1: no in-tree LIVE path reaches HASHLOCK or OPTIMISTIC** — the §6 exit demo
is ZK_DELIVERY only, so those two rows are specified and unit-proven, not
field-proven. §6.3 discloses F-1 with its owner.

### Forward items, written INTO their receiving tasks (Rule 6 sub-rule 6)

- **2.17 (NEW)** — the F-1 slash-persistence defect, both paths, with the
  boundary pins named that must go red when it lands.
- **2.18 (NEW)** — the METHODOLOGY amendment backlog, because **F-14 is NOT
  discharged**: PHASE3_PLAN assigns its two amendments to "the session that
  commits D-096" — a phrase, not a numbered task — that session was 3.0
  (`42ac490`), and neither amendment is in `docs/METHODOLOGY.md` (grep: zero
  hits for both). It went unowned the moment that session ended: **the F-3
  floating-owner failure, recurring, in the very document that catalogues it.**
  Not fixed here, because F-14 itself argues that amending METHODOLOGY from
  inside another task's session is the silent cross-document edit Rule 8 exists
  to prevent. 2.18 also receives this session's own candidate: **a watched-red
  harness must fail loudly when the sabotage does not COMPILE** — silence and
  success are indistinguishable otherwise, and Rule 3's "see the red" is
  satisfiable by a harness that can only ever see one.

## D-098 — Task 2.17: the SPEC §6.4 proposal-time bond slashes are applied in the ANTE, the only v0.50 store branch that outlives a failing message; the untrusted-solver model's economics exist for the first time

- **Date:** 2026-08-04 (task 2.17, after an approved plan-stop with six ratified
  decisions).
- **SPEC:** §6.4 (solution validation, slashing), §13 (codespaces), §11.6
  (determinism); builds on **D-007** (the ADR that PRICES the slashes —
  `invalid_solution_slash_bps` 500, `equivocation_slash_bps` 2000), **D-085**
  (one implementation, two callers; and the v0.50 postHandler finding),
  **D-059** (zero mode branching), **D-093** (the wire identities that must not
  regress), **D-097** (finding F-1, which opened this task); METHODOLOGY Rules
  1/2/3/4/6/7/8/9.

### The defect, re-derived at the artifact rather than inherited

`cosmos-sdk v0.50.15`, `baseapp/baseapp.go`, read directly:

- the **ante** branch is written at **line 933**, the moment the ante returns
  nil — before any message runs, and regardless of what the messages then do;
- the **message** branch is written at **line 989**, only when `runMsgs`
  returns no error;
- a **postHandler** runs on a context derived from that same message branch
  (**line 972**) — which is why D-085 found a postHandler refund structurally
  impossible, and equally rules out a postHandler charge.

`x/session/keeper/solution.go` called `slashSolver` and then returned an error,
so the slash was written into the branch the rejection threw away. Through the
msg server a rejected proposal cost a solver **gas only**.

### The over-broad inheritance, corrected — the most important record change here

D-097 filed this as *"the SPEC §6.4 bond slash does not land on the tx path"*,
and it was being read as *"the §6.4 slash does not land"*. **x/session slashes
in THREE places and only two were broken.** The third — `conclude.go`'s
`conclude_failed` 5%, when a WINNING solution fails at settlement — runs in
EndBlock on the parent context, where no per-message branch exists to discard
it. It was never broken.

Verified through a real `FinalizeBlock` rather than by reading the code
(`TestConcludeFailedSlashPersistsThroughARealBlock`), because METHODOLOGY Rule 4
step 5 applies to this session's own claim as much as to the one it corrects.
**A task that corrects an over-claim is the likeliest place to produce the
mirror over-claim.** The test asserts the slash EVENT REASON, not a count: the
session concludes in the very block the proposal lands in, so no
"not-slashed-then-slashed" discriminator exists in time — only the reason
attribute distinguishes the third site from the two just fixed.

### The model decision: CLOSE the gap (not adopt gas-only deterrence)

Gas-only deterrence is a coherent model and was rejected on a reason, not by
default. Adopting it would (a) require a SPEC §6.4 amendment and a D-007
supersession rather than a code fix, and (b) **pick an unvalidated deterrent
over a specified one** — PHASE3_PLAN's **G-2** records that nothing prices the
authored surface's consensus cost at all, and ranks that highest of four gaps.
Choosing "gas is the deterrent" while gas pricing is itself an open Phase-3 row
would rest the untrusted-solver model on the weaker of the two.

### Mechanism: the ante, and it is the option space rather than a preference

`ProposeSolutionSlashDecorator` (`x/mandate/ante/propose_slash.go`) applies the
slash and returns **nil**, precisely so the ante branch commits and the message
can then fail. Rejection stays in the msg server, so every D-093 wire identity
is untouched: `("session", 1412)` with the specific rule reachable by
`errors.Is`, and `("session", 1410)`.

**ONE evaluator, two callers** — `keeper.EvaluateProposal` returns a
`ProposalVerdict{Err, SlashBps, Reason}`; the ante turns it into the price, the
msg server into the rejection. D-085's rule at a new boundary, and it is the
structural answer to how this defect was possible: the deterrent and the
rejection had drifted *completely apart* while both looked present.

Rejected alternatives, recorded so they are not re-litigated:

- **message succeeds, verdict in the response/events** — trivially persistent,
  but inverts D-093 one task after it landed, breaks SOLVER_SURFACE §5.7's
  published `(codespace, code)` dispatch contract and 18 `errors.Is`
  assertions, and makes a rejected proposal read as success to every generic
  Cosmos client;
- **postHandler** — impossible, see above;
- **ante REJECTS the proposal** — worse than the defect: a tx failing the ante
  is rolled back entirely, so the solver would not even pay gas.

### The sole-message rule (`ErrProposalNotSole`, session 1430)

`MsgProposeSolution` must be the SOLE top-level message of its tx and must not
be nested in a wrapper. Without it the ante prices against pre-tx state while
the msg server judges against post-sibling state, and **both directions are
defects**: a sibling that turns an invalid proposal VALID slashes a solver whose
solution then succeeds; one that turns a valid proposal INVALID is a free way to
propose garbage. With it, both evaluations read identical state and run the same
function — agreement is a property of the code, not of the tx the solver chose
to build. **The guarantee must not depend on the attacker's tx shape** (D-097's
own endorsed principle, applied to a different mechanism).

The nested walk reuses `nestedAnys` and `max_msg_nesting_depth` — the same
unwrap the mandate chokepoint and D-085's prevalidation decorator use — because
a top-level-only rule would leave authz `MsgExec` as the bypass. Proven: S4
below reds ONLY the wrapper case.

### Exec-mode: NO branch

Runs identically in CheckTx, DeliverTx and simulation, preserving D-059's
zero-mode-branching posture. A CheckTx-side write lands in the mempool's own
branch and is reset at the next Commit; simulation discards everything it
writes, and running the same code there keeps gas estimation honest. **If task
2.8 introduces a mode split it breaks this posture chain-wide** — that cost is
written into 2.8's criteria.

### The 2.8 coupling: SEQUENCE, not fold — and the discriminator generalises

PHASE3_PLAN §5.3 proposed folding 2.17 into 2.8 ("doing them apart risks two
answers to one problem"). Overruled at the plan-stop on a test worth reusing:
**fold when neither problem constrains the other first; sequence when one
does.** 2.17 had to establish what survives a failing tx before 2.8's
ante-vs-msg-server question could be answered on anything but intuition — a
one-directional dependency. Folding would also have (a) repeated the shape that
nearly overloaded 2.7 (two hard things arriving together because they are
RELATED, which is a different justification from CHEAP TOGETHER), and (b) fused
two vector causes into one unattributable hash movement. 2.17's re-baseline is
attributable precisely because 2.8's params work was not stacked on it.

**Five findings are written INTO 2.8's acceptance criteria** (Rule 6 sub-rule
6), not pointed at it: the v0.50 branch facts with line numbers, the
one-evaluator-two-callers precedent, the exec-mode ruling and its chain-wide
cost, the tx-shape hazard any ante/msg-server split inherits, and the new vector
baseline to run against.

### Load-bearing tests — PREDICTED, watched, and two predictions WRONG

Both boundary pins were watched RED before replacement, at their own
self-announcing messages (`expected: 0x0, actual: 0x1`), then replaced with
`SlashedCount 1` + bond `95_000_000` / `80_000_000` — replaced, not relaxed.

| | Sabotage | Predicted | Observed |
|---|---|---|---|
| S1 | ante slash call removed | the two pins | **as predicted** |
| S2 | sole-message rejection removed | both sole-message subtests | **as predicted** — after a compile failure, below |
| S3 | `SlashBps == 0` guard removed | `TestUnpricedRejectionDoesNotSlash` | **WRONG — four tests red** |
| S4 | nested walk disabled | only the `MsgExec` subtest | **as predicted** |

**S1/S2 and the two halves of S4 red DISJOINT sets**, which is the evidence the
attacher, the rule, and its nested walk are independently load-bearing.

**The two misses, kept visible:**

1. **S3 was wrong in an informative direction.** Removing the guard reds FOUR
   tests, not one, because `slashSolver` increments `SlashedCount`
   unconditionally — a zero-bps call is **not** a no-op. So the guard is the only
   thing preventing every VALID proposal from being recorded as a slash event.
   The model, not the code, was wrong, and the corrected model is worth carrying:
   *"slash of zero" is not "no slash"*.
2. **A red prediction earlier in the task was also wrong**, in a way that
   mattered more than S3: removing the keeper-side slash was predicted to red
   three tests and red **five**. All five asserted the slash on a RAW keeper
   context. D-097 wrote that "every in-tree slash assertion" had this shape; the
   number is **five test functions**, so the false-confidence surface was wider
   than the finding implied.

**The harness caught a non-compiling sabotage — 2.18's candidate amendment (c),
firing on this session's own run.** S2's first form left `errorsmod` unused and
did not build. The run reported `FAIL ... [build failed]` and was recognised
ONLY because the grep pattern included `build failed` alongside `FAIL:`. An
earlier command in this same session had the very defect 2.14 recorded — `go vet
... | head -5 && echo BUILD-OK`, where `head` masks the exit code so the OK
prints unconditionally. **The amendment is not yet in METHODOLOGY (it is
2.18's), and it earned its keep before being written down.**

### The determinism vector: RE-BASELINED, and the movement was PREDICTED

**A pre-build measurement, run on a throwaway instrumented tree** (reverted, and
the tree fingerprinted back to the empty-diff hash): across the five committed
seeds the sim invokes `SimulateMsgProposeSolution` **2872** times, enters the
keeper's `ProposeSolution` **870** times, and reaches the slash branches
**126 times — all equivocation, zero invalid-solution, zero conclude-failed**.
Hashes with the probes in place were byte-identical to D-082, confirming the
instrumentation was stderr-only and perturbed nothing.

So the movement was predicted, with its cause and its half named in advance:
**126 equivocation slashes now persist.** This is the vector's FIRST re-baseline
since D-082.

**New baseline** (two independent processes, byte-identical):

| seed | D-082 baseline | D-098 baseline |
|---|---|---|
| 1 | `245EF177…` | `E9C4A6827487B65283A9A91CD5147AAE28C416E0B9255D33914685D92C74BD0F` |
| 2 | `B48BDA6D…` | `C1EE3B5F577EFE934AF35B867E306E0D93CC5B9EE857A0AE0BB493BDA615B576` |
| 7 | `FA562B1D…` | `DA743B6A0B72642CA231CE40C95035C71A2817ACFC229CDBBACA41E3D17BE0EE` |
| 12 | `E491FB48…` | `B183307D1FD9A2BF4EAA5ED48746589B060B87638F87555B245ED0EC1CE383AA` |
| 42 | `6596CCA1…` | `36661093B93AB53254FD3B751304AAE8E57CB825CDAB904A4DDE49918A03D819` |

**Named covering gate (Rule 7 step 5), and note the SPLIT.** The vector covers
the **equivocation** half only, and covers it for the first time — this is not
another Category-1 shadow. It covers the **invalid-solution** half NOT AT ALL
(zero hits, measured), and the sole-message rule not at all. The covering gates
are therefore `app/solver_conformance_test.go` (both pins) and
`app/proposal_slash_test.go` (unpriced rejection, sole-message both shapes,
conclude-failed), plus `scripts/exit_demo.sh` stage 7 on a live node.

**The D-093 asymmetry, restated because it cuts both ways here:** a result-code
change CANNOT move this vector (it hashes the sim's store-commit app hash;
`SimDeliver` builds no CometBFT block), so byte-identical would have licensed
nothing about the new 1430 rejection either. The movement observed is a STATE
movement and only a state movement.

### The live gate

`scripts/exit_demo.sh` re-run end to end on a live node, **PASSED**. Stage 7's
banner used to print `solution rejected and the solver slashed 5% ✓` with
nothing behind it — the positive form of the vacuous negative, and it was
**false every time it printed**. It now reads the bond back and checks the
GOVERNED rate (from params, not a hardcoded 5%, since D-007/D-008 make it
governable):

```
  and the solver's bond REALLY fell: 100000000uclv → 95000000uclv at 500 bps ✓
```

**Three predictions about stage-7 ordering, written before the run, all held:**
the 5% lands; no EARLIER stage loses bond (the bond is still `100000000uclv`
when stage 7 begins, which the delta assertion proves); and stage 7 running last
remains sufficient. **The ordering rationale is now load-bearing rather than
cautious** — until this task the slash was reverted, so the stage could have
been moved anywhere and no run would have noticed. Recorded in the stage's own
doc comment so a future reorder cannot silently break it.

### The record sweep — method first, then results

Ran the D-095 negation sweep for BOTH trigger classes plus two more, on the
claim's negation rather than the mechanism's name:

- **Class A — statements FALSE today** that the fix makes true:
  `grep -rn "commits with the failing tx"` → 3 sites, plus one in 2.14's own
  acceptance-test doc comment.
- **Class B — statements TRUE today** that the fix makes false:
  `grep -rni "gas only|reverted with the failing|does not persist|never lands|not applied on the proposal"`
  → ~10 real sites across 6 files (3 unrelated "pay gas only" false positives
  discarded).
- **Class C — the referent, not the guarantee:** every `5%` / `20%` /
  `slashable` / `is slashed` claim re-read against WHICH of the three sites it
  means. This is the class the two-trigger rule does not name, and where the
  over-broad inheritance lived.
- **Class D — citation resolution (Rule 9 step 2):** **`D-024` is a
  mis-citation in 5 places.** D-024 is *"§1.3 steps 2–3 demonstrated in two
  phases until task 0.7"*; the slash bps are **D-007**'s. It propagated from a
  loose "D-024 incentive" phrase into D-097, CLAUDE.md's 2.17 row, PHASE3_PLAN
  §5.1, SOLVER_SURFACE §6.3 and a test comment.

**21 statements corrected.** The one worth naming: `cmd/conclave-demo/exit_demo.go`
still asserted *"the reference solver in solver/ never reads
IntentPayload.condition"* — stale since 2.14 closed it. **2.14's own negation
grep corrected the sibling statement in `exit_demo_ibc.go` and missed this
one**, because that sweep looked at printed banner strings and this is a comment.
Recorded as evidence for a sharpening of the D-095 rule: the sweep's surface is
*every* statement, not the ones that happen to be user-visible.

### Deviations (Rule 8)

1. **The plan's vector prediction was wrong, then right.** The plan said the
   vector was "likely to MOVE"; the first measurement reported zero slash hits,
   which would have made it another coverage shadow; that measurement was
   INVALID because `go test` discards a passing binary's output without `-v`,
   and the control run that proved the probe worked had used `-v` while the sim
   runs had not. Re-run correctly, the original prediction was right. **The
   false negative and its cause are recorded rather than smoothed over: it is
   the same silence-reads-as-success failure as 2.14's S3, in a measurement
   harness this time.**
2. **Documentation was edited CONCURRENTLY with the gate**, not after it. The
   effect is identical to the post-gate case Rule 9 step 4 anticipates, and the
   proof is the same: the shipping tree's SOURCE-ONLY diff hash equals the
   FULL-diff fingerprint recorded at gate time, `dbf0aa6a…`, byte for byte — not
   one source or test byte moved after the gate.

### Forward items

**None.** 2.17 produced no new task — the first tail head to close without
generating another, which is §5.3's termination condition reached rather than
asserted. The keeper-level slash assertions were migrated (not deleted) into a
fixture that models the ante+msg-server sequence, with a doc comment stating
plainly that it REPRODUCES the sequence and is not evidence the chain performs
it — that evidence is the app-level tests, and the distinction is exactly what
five green keeper tests failed to make for two phases.
