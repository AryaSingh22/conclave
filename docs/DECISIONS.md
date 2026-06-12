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
