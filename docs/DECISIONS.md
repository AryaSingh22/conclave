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
