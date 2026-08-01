# Engineering Methodology — The Transferable Discipline

**What this is.** The working method that carried Conclave through Phases 0–2,
codified as **project-independent rules** so that any executor — including a
less-capable model, a new contributor, or a future session with no memory of
this history — can produce excellent results by following them mechanically.
The rules stand alone; each carries a **real instance** from this repo's
history as evidence (including the failures that taught it). Nothing here is
Conclave-specific except the instances.

**How to use it.** Read the rules before starting any task. Run the §11
checklist at task start and task end. When a rule and your judgment conflict,
the rule wins until you have written down why it should not — that written
"why" is itself Rule 6.

---

## Rule 1 — One task per session, bootstrapped from written authority

**Rule.** Each working session executes exactly one ledger task. Every session
starts by reading the project's written authority (conventions doc → spec →
decision log → the current phase plan) before touching anything, even when the
task "looks obvious." The docs are the project's memory; a session's memory is
worthless to the next session unless written down.

**Why.** Multi-task sessions blur review boundaries and produce commits nobody
can reason about. Sessions that skip the bootstrap re-derive stale context and
contradict recorded decisions without knowing it.

**Mechanical procedure.**
1. Read, in order: conventions (CLAUDE.md-equivalent) → spec sections the task
   cites → the decision log entries the task cites → the phase plan's task
   section.
2. Verify the green baseline (the project's full check suite) BEFORE changing
   anything, and record the HEAD commit you started from.
3. If the task's premise contradicts what you read: STOP and surface it. Do
   not "fix" the premise silently.
4. Do not start the next ledger task, however small it looks.

**Instance (Conclave).** Every 0.x–2.x task ran this way; the one standing
CLAUDE.md instruction is "one SPEC §10 task per session, in order." When Task
1.0's hardened gate immediately exposed a real bug, the bug became its OWN
task (1.0b) in a fresh session — not a rider on 1.0.

---

## Rule 2 — Plan-stop before code; two stops when the foundation is hard to reverse; a stop is a REAL wait

**Rule.** No consensus-critical or architecture-bearing code before a written
plan is reviewed at a stop. Tasks whose first step is hard to reverse —
dependency selection, protocol/wire decisions, consensus wiring — get TWO
stops: one for the foundation decision, one for the build on top of it. A stop
means **ending the turn and waiting for a typed go-ahead**. Bundling "here's
my plan, and here's the code, approve both" — or presenting options while
already building the favorite — is not a stop.

**Why.** The foundation decision is the expensive one; reviewing it after the
build exists reviews a fait accompli. A rubber-stamp option-prompt gets
rubber-stamp review.

**Mechanical procedure.**
1. Classify the task: does any step commit the project to something a later
   session cannot cheaply undo (a dependency major, a proto field, a stored
   key layout, an interface another module consumes)? If yes → two-stop.
2. Write the plan with the decision points EXPLICIT (options, costs,
   recommendation).
3. End the turn. Do nothing to the repo while waiting. Resume only on an
   explicit go-ahead, and record the approved decision before building.
4. If mid-build you discover the approved foundation was wrong: that is a new
   stop, not a silent pivot.

**Instance (Conclave).** Task 1.5's DKG-transport decision and 1.6's
transport-then-cutover were clean two-stops. The lesson case is Task 2.2: an
early draft presented the wasmd STOP as an inline option-prompt bundled with
in-progress wiring; review pushed back, the stop was re-run in isolation, and
the phase ledger now records the rule explicitly. The cost of a fake stop is
not hypothetical — 2.2's determinism re-baseline had to be bisected after the
fact precisely because wiring had begun before the checkpoint (D-078/D-079
record the deviation and its remediation).

---

## Rule 3 — Load-bearing tests: watched RED without the thing

**Rule.** Every guard, invariant, and security property gets a test that has
been **watched to fail when the guard is removed**. "The test passes" is the
weak signal — it also passes when the test is vacuous, wired to a stub, or
asserting the wrong thing. "The test goes red the moment the guard is
deleted" is the real signal, and you must SEE the red, not assume it.

**Why.** A test suite full of green vacuous tests is worse than no suite: it
manufactures false confidence exactly where the project needs true signal.

**Mechanical procedure.**
1. Write the guard and its test; see green.
2. Remove or bypass the guard (comment out the check, skip the gate, unbind
   the validation); run the test; **see red**; note the failure message.
3. Restore the guard; see green again.
4. Record "watched red-without-X" in the task notes/ADR. A reviewer should be
   able to repeat the removal and get the same red.
5. For state machines: an exhaustive transition-table test where every ILLEGAL
   transition is an asserted rejection — illegal-by-omission is not tested,
   illegal-by-assertion is.
6. **PREDICT which guard reds before you remove it, then check the
   prediction.** Write down "removing X should turn test T red" *first*. A red
   that lands somewhere other than predicted is not a pass — it means your model
   of what-guards-what was wrong, and the model is what you will reason with
   next time.
7. **DOSE the watched-red against whatever randomizes its path.** If ordering,
   ids, addresses, or map traversal can vary the code path, a single red run is
   an accident. Repeat until the red is the rule, not a draw.

**Corollary — a flaky test may be a correctness ORACLE at low duty cycle.**
Before you stabilize a flake, *reproduce and count it*. The failure RATE is
evidence: rates that land near a power-of-two boundary (1/256, 1/128) point at a
byte/boundary bug, not at a fixture. **"Make the fixture deterministic" applied
first would silence the oracle and ship the defect permanently green** — the
most dangerous outcome available, because the suite then reports success
forever. Investigate first, stabilize second; the ordering is part of the rule.

**Instance (the corollary — Conclave Task 2.11, D-088).** A design doc carried a
flake-hardening item with a prescribed fix: *"`TestIssueMandateReplace` flaked
once on a `randAddr()`-seeded fixture; fix = deterministic fixture seeding."*
The session reproduced it BEFORE touching it: `-count=2000` → **8 failures
(0.40% ≈ 1/256)**. That rate is the signature of a one-byte range boundary, and
it was: `store.Iterator(prefix, append(prefix, 0xFF))` silently excludes every
key whose first post-prefix byte is `0xFF`, across **8 sites, 2 LIVE-broken** —
one of them on a consensus path (the counterparty-class credential predicate).
The prescribed fix would have pinned the sampler to a passing draw and hidden
all of it. Correct order, load-bearing: fix the sites → forced-`0xFF` watched
red → *only then* fixture determinism.

**Instance (predict-the-red — Conclave Task 2.6, D-089).** The session assumed
the solver-side `allow_partial` candidate gates were what guaranteed a
fill-or-kill intent is never partially filled. Removing them left the test
**GREEN 25/25** — the prediction was wrong. Removing §6.4 rule 3's clause as
well turned it **RED 25/25**. The guarantee was rule 3 all along, and rule 3 is
reached on the new partial-fill path; the candidate gates are an efficiency
guard. Had the red not been predicted-then-checked, the session would have
shipped believing a non-guarantee was the guarantee. **And the 25 is the dosing
rule:** the pair's roles follow content-derived intent-id ordering, so a
one-shot red would have exercised one side by accident.

**Instances (Conclave).** The escrow module's single `nextState` gate backs an
exhaustive transition-table test — every illegal transition is a load-bearing
REJECT, proven red when the gate is loosened (D-073). The F-4 property test
proves an intent CANNOT open pre-ordering even with shares withheld — and the
corrupt-share gate has a dedicated red-without-gate proof
(`TestCorruptShareGateIsLoadBearing`): removing the authored share gate flips
named tests red, which is precisely what made the gate's role auditable. The
ZK instance-binding test fails red if the escrow-id binding is removed
(D-076) — that red run is why replay defense is a fact, not a claim.

---

## Rule 4 — Existence is not enforcement

**Rule.** A field that is signed but never read is functionally absent. A
gate that is wired but never reached is functionally absent. A test vector
that never executes a code path does not test it. For every artifact ask:
**who reads this, where is it enforced, and which test goes red if nobody
does?** Check HONORED/COVERED, never merely PRESENT.

**Why.** This is the highest-yield review question in the project's history.
Presence is what a diff shows; enforcement is what an adversary meets. The gap
between them is where real vulnerabilities lived.

**Mechanical procedure.**
1. For every new field/flag/param: name the exact code line that READS it and
   the behavior that changes when it differs. No reader → you have added
   decoration, not function; either wire it or remove it.
2. For every consent/authorization artifact: trace who ATTACHES it vs who
   AGREED to it. If an untrusted party can attach it, a signature elsewhere
   does not make it consented.
3. For every coverage claim ("the sim covers X", "the fuzzer hits Y"):
   sabotage X and confirm the gate fails. A gate that cannot be made to fail
   is not covering anything.
4. Apply the same question to defaults: a safe-looking default that is only
   safe in one mode (dev, stub) is an unsafe default.
5. **A security claim verified against ONE code path is UNVERIFIED.** Enumerate
   every path that reaches the claim, then check each. The question is never
   "is this guarded?" but "how many ways in are there, and is each one
   guarded?" Paths multiply where you are not looking: an ante decorator and a
   msg server; a solver-proposed settlement and a fallback settlement; a CREATE
   branch and a REPLACE branch; a full fill and a partial fill.

**Instances (Conclave).** Three, escalating: (1) delivery conditions were
solver-attached — the seller SIGNED a condition but validation never checked
the attached condition against the signed one, so an untrusted solver could
impose unsigned obligations; found and closed fail-closed at both chokepoints
(D-075). (2) The mirror-image hole: ZK release was seller-driven against a
commitment untethered to the buyer's want — the buyer's consent existed as a
concept but was enforced nowhere; closed by requiring the buyer's signature on
the exact condition (D-076). (3) The determinism vector is green while the
REAL reveal path is never exercised (the sim is epoch-inert) — coverage that
exists but does not enforce; carried as an explicit debt with an owner rather
than left as false comfort (D-069 → the Phase-3 plan). The rule's slogan
version: **a signed-but-unread consent field is no consent; a vector that
doesn't reach the path doesn't test it.**

**Instances (the path-enumeration sharpening — three, each a gap found only by
asking "which OTHER path?"):** (1) **2.3/D-085** — the submit preconditions were
enforced in the veil msg server, and that was true; the ante was a *second* path
that charged before reaching it, so a submit could pass the ante and fail the
msg server, stranding the charge and corrupting the conservation invariant on
demand. The fix is structural precisely because it collapses the two paths into
one shared function. (2) **2.4/D-086** — the `terms_hash` pre-image chain was
verified on solver-cleared settlements and was genuinely fine there; the
**fallback** path (the no-solver default — i.e. every localnet settlement) never
persisted its Solution, so the pre-image was unfetchable. One path checked, the
common one missed. (3) **2.3/D-084** — `AmendMandate` correctly refused to
change a grantee's sigil; `IssueMandate`'s **REPLACE** branch was the other way
in and never inherited the check. In all three the guarded path was real and the
claim was true *of it* — the defect lived entirely in the unenumerated sibling.

---

## Rule 5 — The authored-vs-adopted boundary is literal; a dependency conflict is a DECISION, not a detour

**Rule.** Maintain an explicit inventory: which security-bearing code the
project AUTHORED (its proof burden) vs ADOPTED (its usage burden). Never blur
it in either direction — do not re-implement what a maintained, audited
library provides; do not smuggle authored glue into the "library" column.
When a wanted dependency conflicts with the crypto/toolchain foundation, that
conflict is surfaced as a recorded decision with options — never routed
around with a quiet version bump, vendored patch, or "temporary" fork.

**Why.** The authored surface is the audit bill and the attack surface; its
inventory must be exact or the audit reviews the wrong thing. And unmanaged
dependency drift is how a pinned, validated foundation silently rots.

**Mechanical procedure.**
1. Keep a written two-column inventory (authored / adopted, with versions and
   licenses). Update it in the same task that changes it.
2. Name the project's toolchain/dependency ceiling explicitly (versions,
   go/rust floor). Any dependency whose floor crosses the ceiling = STOP with
   options, costs, and a single recorded re-validation if crossed.
3. Never bump one foundation pin "in passing." Pins that were validated
   together are re-validated together.
4. When authoring on library primitives, document the exact divergence from
   the library's reference construction and WHY — that divergence paragraph is
   the seed of the future audit scope.

**Instances (Conclave).** The threshold layer adopts drand/kyber primitives
and authors only composition/derivation/gating — with the divergence from
tlock's reference (the identity derivation) documented at authoring time
(D-066), which later became audit obligation O1 verbatim. The gnark spike
found the wanted version required a toolchain bump that would invalidate the
kyber pin: the answer was a recorded pin (gnark v0.13.0) and a NAMED two-pin
ceiling, not a quiet Go bump (D-076). The wasmd task hit the same ceiling and
resolved it the same way — spike, table, decision (D-077). The counterexample
that proves the rule's value: block-sdk was adopted, upstream then died; the
recorded pin + re-evaluation checkpoints meant the project KNEW its exposure
at every boundary instead of discovering it in production.

---

## Rule 6 — Every underspecified decision becomes a same-task ADR; every forward item becomes a numbered task

**Rule.** When the spec does not pin a behavior down, the implementation
session decides it AND records it as an ADR-style entry in the same task —
never in a later "docs pass." When work is deliberately deferred, it becomes a
**numbered task in a ledger** with an owner, not a phrase ("handle later",
"post-X cleanup") floating in a comment or commit message.

**Why.** Undocumented decisions get re-litigated or silently contradicted.
Floating phrases are where debts go to be forgotten — and they resurface as
audit findings.

**Mechanical procedure.**
1. During implementation, keep a running list of every choice the spec did
   not force. At task close, each list item is an ADR entry (context →
   decision → consequences) appended to the decision log.
2. Anything you consciously did NOT do: give it a task number in the phase
   ledger, with acceptance criteria, in the same session that deferred it.
3. Periodically grep for the project's TODO marker; every hit must map to a
   ledger task. A TODO without an owner is a defect in the ledger. **A marker
   whose named task has COMPLETED is the worst case** — it reads as pending
   while being unowned, and it is invisible to "is anything unowned?" checks.
   Retire it or re-point it; never leave it naming a closed task.
4. When a later task changes an earlier decision, the new ADR names and
   UPDATES the old one — the log stays navigable.
5. **A FINDINGS LIST NEEDS THE SAME OWNER-PER-ROW DISCIPLINE AS A LEDGER, OR IT
   GOES WRITE-ONLY.** Audit findings, review comments, and post-mortem actions
   are forward items too. Give every row a disposition column that is never
   blank — a task number, or an explicit "closed by X" — and re-verify it at
   every boundary. A findings list is *especially* prone to this failure because
   producing it feels like completing something.
6. **A one-way pointer is not ownership.** Writing "re-pointed to task N" in the
   *deferring* task is half the work; if task N's own acceptance criteria do not
   list the item, it is owned by nobody — and it will be invisible at exactly
   the moment task N is planned. Write the item INTO the receiving task.

**Instances (Conclave).** The pattern's successes: 90 ADRs in
`docs/DECISIONS.md`; the escrow consent seams were deferred with discipline —
FW-1/FW-2 became numbered task 2.7 the moment they were deferred (D-078), and
the five matcher TODOs became task 2.6 rather than staying comments. The
**counterexample that taught the rule:** the ante price-source swap was
deferred early (D-044) to "a dedicated post-0.8 task" — a phrase, not a task
number. It floated unowned across two phases and came back as external audit
finding F-3. The failure mode is precisely "deferred correctly, tracked
nowhere."

**The counterexample RECURRING — which is why sub-rules 5 and 6 exist
(D-090).** A reconciliation audit found the same failure reproduced *on the
findings list that recorded F-3*. The Phase-0 audit filed eight findings;
F-1…F-6 were picked up by later plans, but **F-7 and F-8 were given no owner
anywhere** — a repo-wide grep found zero mentions of either outside the audit
document. F-7 had in fact been closed by task 2.6, which closed it under a
different name (D-049) and never recorded it against F-7: closed-in-fact,
open-on-paper. F-8 stayed genuinely open for two phases and was
**independently rediscovered** by a marker sweep that did not know F-8 existed —
and that rediscovery is the proof it was invisible. Separately, two items were
"re-pointed to task 2.7" by the deferring task while 2.7's own criteria listed
neither (sub-rule 6), and `timeouts→params` was recorded as owned by **two**
tasks at once. **A list you write and never re-verify is a list you have
filed, not a list you are tracking.**

---

## Rule 7 — The invariant gate is hard, versioned, and re-baselined only by recorded decision

**Rule.** The project's global correctness gate (for a consensus system:
byte-identical determinism over a committed seed vector, run in independent
processes) is (a) run before declaring ANY task complete, (b) reproducible —
fixed seeds, no self-seeding, no skippable preconditions, (c) allowed to
change its expected output only via an explicit recorded re-baseline that
names the cause. A gate that can SKIP itself is not a gate. Changes to
dependencies on the state path get the gate run in ISOLATION before further
changes stack on top, so drift is attributable.

**Why.** The gate is the one signal that catches whole-system regressions no
unit test names. Its authority depends entirely on it being un-dodgeable and
its history being explainable.

**Mechanical procedure.**
1. The gate runs from committed inputs (seed vector in-tree); two independent
   process invocations; any SKIP, abort, or mismatch is a hard failure.
2. Sequence risky changes so the gate runs BETWEEN causes: dependency bumps
   in one run, feature wiring in the next. If you must combine, plan the
   bisection before you need it.
3. Every re-baseline is an ADR: what changed, why the new baseline is
   correct, what was compared.
4. When the gate fires unexpectedly: treat it as a real defect until proven
   otherwise. The gate's false-positive rate must be driven to zero (fix the
   harness as its own task) or the team learns to ignore it.
5. **Know the gate's COVERAGE SHADOW, and name the covering gate separately.**
   A global gate is generated by *something* — a workload generator, a seed
   vector, a fixture set — and whatever that generator does not emit is
   **structurally unreachable**, not merely untested. More seeds do not help;
   only a different generator does. So a passing global gate licenses exactly
   one conclusion: **"unperturbed on what the generator exercises."** It never
   licenses "inert in production." Every time the gate comes back clean on a
   change, write down *which* test actually covers the change — and if the
   answer is "none", you have no coverage, only a green light.

**Instance (the coverage shadow — Conclave, four times, which is why it is a
property and not an observation).** The determinism vector's intent generator
emits thin, one-sided, single-asset flow: one leg per side, `limit` and
`allow_partial` never set, one coin give-asset in its only cycle. Consequently
the vector came back **byte-identical** while (a) **D-069** the real Reveal path
was never exercised at all (the sim is epoch-inert, so the cryptographic Veil
the vector exists to guard was untouched by it), (b) **D-086** the new
fallback-Solution write was never reached (the sim generates no mutually
fillable pair), (c) **D-088** no `0xFF`-prefixed id was ever populated in five
seeds, so a live 1-in-256 consensus bug passed the gate, and (d) **D-089**
none of the three new matcher branches were entered. Four byte-identical greens,
four times covering nothing. The rule that came out of it, now binding: **the
vector is a whole-system REGRESSION backstop, not a coverage instrument** —
byte-identical on a matcher change is evidence of ADDITIVITY, never of
correctness — and where the generator cannot reach, **hand-built adversarial
fixtures are the primary gate**, with the vector demoted to backstop. Widening
the generator is the alternative, and it is a task in its own right, not an
assumption.

**Instances (Conclave).** The Phase-0 audit found the chain deterministic but
the HARNESS flaky (un-seeded self-seeding, a SKIP-on-empty-valset escape
hatch) — F-1. Task 1.0 hardened the gate FIRST, before the largest
nondeterminism surface in the project's history (vote extensions + DKG) was
built — and the hardened gate immediately caught a real, pre-existing
mandate-accounting bug at seed 7 (D-063→D-064): the gate paid for itself
before the feature it was hardened for existed. The 2.2 session violated the
run-in-isolation sequencing (vector after wiring instead of after dep-bumps)
— it got lucky (byte-identical), and the deviation was surfaced and recorded
rather than normalized (D-079).

---

## Rule 8 — Report honestly: deviations surfaced, failures shown, self-corrections kept

**Rule.** When execution deviates from the approved plan, say so at the
moment of deviation — in the task record, not buried. When a claim was wrong
and later corrected, keep the correction VISIBLE (do not rewrite history to
look right the first time). "Done" claims come with the output that proves
them; a skipped step is reported as skipped, with why.

**Why.** Reviewers and auditors calibrate trust on the whole record. One
discovered silent deviation poisons every past "green." Conversely, a visible
self-correction is evidence the process catches its own errors — it BUILDS
trust.

**Mechanical procedure.**
1. Deviation from plan → a named DEVIATION entry in the task's record, with
   the remediation or the accepted consequence.
2. Never claim a check ran without its output. Never summarize a failure as
   a pass "because the failure is understood."
3. When you discover an earlier claim overstated something, write the
   correction next to the claim's record and propagate it to any document
   that repeated it.

**Instances (Conclave).** The audit scope deliberately SURFACES its own
earlier overstatement — the share gate had been called "the sole guard
against poisoned recovery"; internal review corrected it to the two-barrier
split, and the scope document presents the correction to the auditor
up-front rather than letting them rediscover it (A-barriers). The 2.2
localnet-smoke gap was recorded as a named DEVIATION with a home (D-079)
instead of quietly narrowing the acceptance criteria. Determinism hashes,
lint/test exit codes, and bench numbers appear in task records as output,
not adjectives.

---

## Rule 9 — Security-bearing code gets a direct read; inventories are verified, not trusted

**Rule.** Anything that touches key material, share/proof verification,
ciphertext structure, ordering guarantees, or authorization is reviewed by
READING THE CODE, never a summary of it — including (especially) summaries
the author wrote. Inventories of "what is security-bearing" go stale the
moment new integration code lands; re-verify the inventory at every boundary
(phase close, audit scope, major integration).

**Why.** Summaries encode the author's model of the code, and the bugs live
exactly where the model and the code diverge. Stale inventories steer
reviewers AWAY from the newest (riskiest) surface.

**Mechanical procedure.**
1. Before any boundary review, regenerate the security-surface inventory from
   the tree (grep the crypto imports, the key-material types, the
   authorization chokepoints) and DIFF it against the last recorded
   inventory. Every addition gets a direct read.
2. In review, open the actual files at the actual lines; check cited
   tests/locations RESOLVE (a citation that doesn't compile is a finding).
3. Treat "the library handles that" as a claim to verify: find the library
   call and confirm it is the call you think it is, at the pinned version.
4. **FINGERPRINT the tree before and after every gate run** — record the HEAD
   commit and a hash of the working diff (`git diff | sha256sum`, plus the
   staged diff and the porcelain status). Attach both to the task record with
   the gate output. This makes **"the gate ran on the shipping tree"
   CHECKABLE rather than asserted**: identical fingerprints prove no
   uncommitted experiment, no left-behind sabotage from a watched-red run, and
   no post-gate edit slipped in between green and commit. It costs one command
   and it converts the single most load-bearing claim in the whole checklist
   from trust into evidence.

   **TWO fingerprints, and they answer different questions — use both.**
   - **FULL-TREE, during the run** (`git diff | sha256sum` before and after):
     the gate ran on exactly this tree. Non-negotiable, and it has caught real
     process errors twice.
   - **SOURCE-ONLY, at close-out** (`git diff -- ':(exclude)*.md' | sha256sum`,
     or the project's equivalent exclusion): **this is how a post-gate
     documentation edit is JUSTIFIED rather than merely confessed.** Long-form
     gates are expensive, and the honest sequencing — source and gate first,
     docs after, so drift stays attributable — guarantees the shipping tree
     differs from the gated one. If the source-only hash of the SHIPPING tree
     equals the FULL-diff fingerprint recorded at the gate, then not one source
     or test byte moved after the gate, and that is a proof rather than a
     promise. Re-running a multi-minute consensus gate to bless a prose edit is
     theatre; saying nothing is worse. One command replaces both.

   **Instance.** The pattern's predecessor is the honest version of the same
   thing done in prose: a task recorded that its shipping fingerprint "differs
   by exactly two Markdown files" and asked the reader to trust the sentence.
   The next task to hit the identical situation printed the source-only hash
   instead and it matched the gate-time fingerprint byte for byte — same
   discipline, now falsifiable by a reader who has only the repository.
5. **Re-derive the security-surface inventory at every boundary — and schedule
   it, do not merely intend it.** A hand-maintained inventory decays silently
   between boundaries; the greps in step 1 are the source of truth, the
   document is a cache. Run the re-derivation at each phase boundary AND
   immediately before any external review is contracted.

**Instances (Conclave).** The audit scope's O3 exists because the original
"the four authored pieces are the ENTIRE authored surface" claim (D-066) went
stale two tasks later — Task 1.7 authored consensus-integration surface
(registration binding, justification transport, VE injection, share custody)
that the four-piece list would have steered an auditor away from; the scope
corrects the inventory explicitly and says so. The 1.8 scope package
verified all 44 cited test references resolved in-tree at the pinned commit
before delivery — citations are checked, not assumed.

**The instance that turned step 5 from advice into a gate:** that same
inventory went stale **a second time**. Pinned at the Phase-1 commit, it could
not cover what tasks 2.1–2.6 authored — the ZK delivery circuit and its
instance-binding verifier, the two consent chokepoints, the escrow transition
gate, a grantee-rebind guard, an ante-chain decorator protecting the
conservation invariant, the CVM no-forge model, and a new EndBlock `recover()`
boundary. None of it was hidden; nobody re-derived. Twice is a pattern, and a
pattern in a *process* is fixed by scheduling, not by resolving to be careful:
the re-derivation is now a named step in the phase-boundary task and a
precondition of contracting the audit (D-090). **A stale inventory is worse
than no inventory — it steers the reviewer away from the newest, least-reviewed,
riskiest code with the authority of a document.**

---

## Rule 10 — Long-form deliverables pass a completeness-gate OUTLINE first

**Rule.** Any long document that will function as authority (a phase plan, an
audit scope, a design doc) is preceded by an outline reviewed at a stop: the
outline must demonstrate COMPLETENESS (every input accounted for — every
debt, every finding, every open decision has a row) before prose is written.
Prose polish cannot compensate for a missing row; a complete outline makes
the full document mechanical.

**Why.** Long documents hide omissions in fluency. An outline exposes the
inventory: reviewers can check "is everything here?" in minutes, which is the
one question that matters.

**Mechanical procedure.**
1. Enumerate the document's inputs first (ledgers, findings, obligations,
   forward items). Build the disposition table BEFORE the sections: every
   input row → a section, a task, or an explicit reasoned deferral.
2. Present outline + disposition table at a stop (Rule 2 applies — a real
   wait).
3. Write the full document only after the outline is approved; if writing
   reveals a missing input, that is a surfaced deviation (Rule 8), not a
   silent addition.

**Instances (Conclave).** PHASE2_DESIGN passed a completeness-gate review
before prose (D-072); the 1.8 audit scope collected every scattered
obligation flag from five ADRs into one auditor-facing inventory before being
written (D-071); the Phase-3 plan was outlined — with its full
debts-disposition table — and approved at a stop before authoring, and the
approval materially changed the document (the authority framing and two
calibrations), which is the gate doing its job.

---

## 11. The per-task execution checklist (mechanical)

**At task start:**
- [ ] Read conventions → spec sections cited → decision-log entries cited →
      phase-plan task section (Rule 1).
- [ ] Verify the green baseline; record HEAD (Rule 1).
- [ ] Classify: hard-to-reverse foundation? → two-stop (Rule 2).
- [ ] Write the plan with explicit decision points; STOP; wait (Rule 2).

**During the task:**
- [ ] Running list of every spec-unpinned choice (→ ADRs at close) (Rule 6).
- [ ] Every new guard: watched red-without-it, then green (Rule 3).
- [ ] Every new field/flag/param: name its reader; no reader → not done
      (Rule 4).
- [ ] Dependency floor crosses the ceiling? → STOP, options, recorded
      decision (Rule 5).
- [ ] Deviating from the approved plan? → surface it NOW (Rule 8).

**At task close:**
- [ ] Tree fingerprinted BEFORE the gate run (HEAD + `git diff | sha256sum`)
      (Rule 9).
- [ ] Full check suite green — output shown, not asserted (Rules 7, 8).
- [ ] Invariant gate run; any re-baseline recorded as an ADR (Rule 7).
- [ ] Tree fingerprinted AFTER the gate run and compared — identical, or the
      difference explained (Rule 9).
- [ ] Docs edited after the gate? → SOURCE-ONLY fingerprint of the shipping tree
      printed and shown to equal the gate-time fingerprint (Rule 9 step 4).
- [ ] The gate came back clean? → name the test that ACTUALLY covers this
      change. "None" means no coverage, not a pass (Rule 7).
- [ ] Every decision from the running list → an ADR in this task (Rule 6).
- [ ] Every deferred item → a numbered ledger task with acceptance criteria,
      **written into the RECEIVING task, not only into this one** (Rule 6).
- [ ] Security-bearing lines touched? → direct-read pass + inventory diff
      (Rule 9).
- [ ] STOP for review before commit; commit only after go-ahead (Rules 1, 2).

**At every boundary (phase close, audit scope, major integration):**
- [ ] Security-surface inventory RE-DERIVED from the tree by grep and diffed
      against the recorded one; every addition directly read (Rule 9).
- [ ] Every findings list and forward-item table re-verified row by row; no
      blank dispositions, no owner-less rows, no item owned twice (Rule 6).
- [ ] Every marker's named task still open; markers naming closed tasks
      retired or re-pointed (Rule 6).
- [ ] Every cited test name and file:line in the authority documents still
      resolves in the tree (Rule 9).

---

*Distilled 2026-07-10 from the Conclave build record (Phases 0–2, D-001
through D-079); amended 2026-07-28 by the D-090 reconciliation audit, which
added the Rule-3 flake-oracle corollary and predict/dose procedure, Rule 4's
path-enumeration sharpening, Rule 6's findings-list and one-way-pointer
sub-rules, Rule 7's coverage-shadow property, and Rule 9's tree-fingerprint and
scheduled-inventory steps (D-001 through D-090); amended again 2026-07-30 by
task 2.7 (D-094), which promoted the **two-fingerprint close-out standard** into
Rule 9 step 4. That last amendment is the one exception to the pattern below: it
came not from a rule failing but from a rule being *followed* and found to end
in prose where it could end in evidence. **Every other amendment came from a rule
failing in practice — which is the meta-rule: if a rule fails you, the failure is
itself an instance. Record it and amend the rule.** The 2.7 case adds its
converse: if following a rule leaves you asking the reader to trust a sentence,
the rule has one more step in it.*
