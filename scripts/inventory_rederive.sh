#!/usr/bin/env bash
#
# scripts/inventory_rederive.sh — PHASE3_PLAN §3.1.2's standing obligation, made
# executable (AUDIT/Rev-1 finding F-13, built at task 3.3a — D-099).
#
# WHY IT IS A COMMITTED ARTIFACT AND NOT A BLOCK OF PROSE. The authored-surface
# inventory has gone stale TWICE (D-066's four-piece claim vs task 1.7's
# consensus-integration surface; AUDIT_SCOPE §2.2 vs tasks 2.1–2.6), each time
# because a hand-maintained document was trusted instead of the tree. A grep
# block living in a plan is reproducible only by whoever retypes it. F-13 adds
# the sharper reason: the commit-citation gap RELOCATES TO THE NEWEST COMMIT
# EVERY TIME, so fixing it by hand is a treadmill and only a check ends it.
#
# Steps 1–7 are §3.1.2's block verbatim in behaviour. Step 8 is NEW: F-13's
# "enumerate commits from git log and assert every one is cited somewhere",
# tolerating exactly the HEAD-cannot-cite-itself case and no other.
#
# Exit status: 0 only if the ASSERTING steps (6, 7, 8) are clean. Steps 1–5 are
# INVENTORY steps — they print a surface to be diffed by a human against
# AUDIT_SCOPE_1.8.md §2.2 and PHASE3_PLAN §3.1.1, and cannot self-assert.
set -uo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO"
RC=0
hdr() { echo; echo "=============== $* ==============="; }

hdr "1. crypto surface: what imports a crypto primitive"
grep -rln 'drand/kyber\|consensys/gnark\|groth16\|encrypt/ibe\|crypto/aes\|crypto/rand' \
     --include='*.go' . | grep -v '_test.go' | sort

hdr "2. authorization chokepoints (keyed on the REGISTRATION SITE, not a name list)"
grep -rn 'errorsmod\.Register' --include='*.go' x/ app/ | grep -v '_test.go'
ERRS=$(grep -rhoE '^\s*(Err[A-Za-z0-9_]+)\s*=\s*errorsmod\.Register' \
       --include='*.go' x/ app/ | grep -oE 'Err[A-Za-z0-9_]+' | sort -u | paste -sd'|')
echo "--- referenced at: ---"
grep -rnE "($ERRS)" --include='*.go' x/ app/ | grep -v '_test.go' | wc -l
echo "    (count only; run the grep by hand when diffing the inventory)"

hdr "3. key material + share custody"
grep -rln 'PrivKey\|SecretShare\|KeyShare\|ShareProvider\|dkg_shares' \
     --include='*.go' . | grep -v '_test.go' | sort

hdr "4. consensus-path recover() sites (each a fail-closed panic boundary)"
grep -rn 'recover()' --include='*.go' x/ app/ crypto/ | grep -v '_test.go'
echo "--- count: $(grep -rn 'recover()' --include='*.go' x/ app/ crypto/ | grep -vc '_test.go') ---"

hdr "5. ante-chain composition (order is security-bearing)"
grep -n 'Decorator' app/ante.go

hdr "6. CITATION RESOLUTION — every Test* cited by an authority doc must resolve"
DOCS="docs/EXIT_DEMO.md docs/PHASE2_DESIGN.md docs/PHASE3_PLAN.md \
      docs/edge/X402_SURFACE.md docs/edge/7683_SURFACE.md docs/AUDIT.md \
      docs/AUDIT_SCOPE_1.8.md CLAUDE.md docs/METHODOLOGY.md"
# grep -v '^>' drops keep-and-mark blocks: a SUPERSEDED block QUOTES an old
# claim and may legitimately cite a deleted test. That convention is what makes
# this sweep discriminating rather than red forever (Rule 7 step 4).
UNRESOLVED=$(comm -23 \
  <(grep -rhE 'Test[A-Za-z0-9_]+' $DOCS | grep -v '^\s*>' | grep -oE 'Test[A-Za-z0-9_]+' | sort -u) \
  <(grep -rhoE '^func (Test[A-Za-z0-9_]+)' --include='*_test.go' . | sed 's/^func //' | sort -u))
# KNOWN NON-FINDINGS — anything NOT here is a finding. Each entry names why.
#   TestKyberRecoverCommit  — a doc WRAPS ...SelectsLowestIndex across two lines
#   Testnet                 — an English word, not a citation
#   TestSignedConditionIsPermissionNotRequirement_BoundaryPin — DELETED by 2.7
#     (D-094); survives only inside marked supersession blocks + this allowlist
#   TestEquivocationSlashAlsoDoesNotPersist — DELETED by 2.17 (D-098), BY ITS OWN
#     INSTRUCTION. Added to this allowlist at task 3.3a (D-099), and the reason
#     matters more than the entry: the surviving citation is in CLAUDE.md's
#     ledger row for task 2.17 ITSELF, where it is not a claim about the tree but
#     a record of what 2.17 was REQUIRED to do — "MUST go red and be replaced
#     with the slash assertions, not relaxed". The task did it; the replacement
#     is TestEquivocationSlashPersists (app/solver_conformance_test.go:441).
#     A satisfied instruction inside a CLOSED row is exactly the case §3.1.2
#     predicts this sweep cannot filter by format, because ledger rows mark
#     supersession with strikethrough rather than a blockquote. Allowlisting is
#     the designed answer; SILENCING THE CHECK IS NOT. If this name ever appears
#     outside 2.17's row, it IS a finding.
ALLOW='^(TestKyberRecoverCommit|Testnet|TestSignedConditionIsPermissionNotRequirement_BoundaryPin|TestEquivocationSlashAlsoDoesNotPersist)$'
REAL=$(echo "$UNRESOLVED" | grep -vE "$ALLOW" | grep -v '^$' || true)
if [ -n "$REAL" ]; then
  echo "FINDING — cited but unresolved:"; echo "$REAL" | sed 's/^/    /'; RC=1
else
  echo "clean (allowlisted-only)"
fi

hdr "7. MARKER OWNERSHIP — every CONCLAVE-TODO must name an OPEN task"
grep -rn 'CONCLAVE-TODO' --include='*.go' --include='*.rs' --include='*.sh' \
     --include='*.proto' . | grep -oE 'CONCLAVE-TODO\([a-z0-9+.-]*\)' | sort | uniq -c
echo "--- each marker: the task it names, and whether that task is still open ---"
# THREE THINGS THIS CHECK GOT WRONG BEFORE IT WORKED, all recorded because each
# is a reusable trap for anyone writing a comment-scanning check (D-099):
#
#  1 SCOPE. v1 grepped whole FILES for `task N.N`, which conflates a marker's
#    OWNER with any historical mention in surrounding prose ("closed by 2.7",
#    "RE-POINTED at 3.3a"). It reported five false owners — two of them written
#    by the very fix that introduced them. Scope is the MARKER BLOCK.
#  2 WRAPPING. Comment blocks wrap, so "owned" can end one line and "by task
#    3.4" begin the next. The window must be JOINED before matching — the same
#    wrapped-referent hazard §3.1.2 documents for test names.
#  3 THE COMMENT PREFIX. Joining is not enough: the join yields "owned // by
#    task 3.4", so the `//` must be STRIPPED per line first. This one produced
#    a false UNOWNED on a marker that names its owner perfectly clearly.
#
# And a design correction that matters more than any of them: the pattern is
# deliberately BROAD (any `task N.N` inside the block), not the "owned by"
# phrasing. The repo uses several spellings — `owned by task 3.4`, `RE-POINTED
# … to **task 2.12**` — and a check that recognises only the house style
# reports correctly-owned markers as unowned. Breadth was never the bug; FILE
# SCOPE was. A check that cries wolf is one the team stops reading (Rule 7
# step 4).
#
# This file is excluded from its own sweep: it contains the marker string in
# its own greps, so it always matches itself — the §3.1.2 allowlist
# self-reference, recurring inside the artifact built to mechanise §3.1.2.
REFPAT='task[ *]*[0-9]+\.[0-9]+[ab]?(\([a-z]\))?'
UNOWNED=0
while IFS=: read -r file line _; do
  [ -z "$file" ] && continue
  case "$file" in *inventory_rederive.sh) continue ;; esac
  block=$(sed -n "${line},$((line+14))p" "$file" 2>/dev/null \
          | sed -E 's@^[[:space:]]*(//|#)[[:space:]]?@@' | tr '\n' ' ')
  refs=$(echo "$block" | grep -ioE "$REFPAT" | grep -oE '[0-9]+\.[0-9]+[ab]?' | sort -u)
  if [ -z "$refs" ]; then
    echo "    UNOWNED: $file:$line — names no task at all"; UNOWNED=$((UNOWNED+1)); RC=1
    continue
  fi
  for ref in $refs; do
    # A task is CLOSED when the ledger check-marks it: `- [x] **<ref> ...`
    if grep -qE "^- \[x\] \*\*$ref " CLAUDE.md 2>/dev/null; then
      echo "    ***CLOSED OWNER*** $file:$line -> task $ref — retire or re-point it"; RC=1
    else
      echo "    ok $file:$line -> task $ref"
    fi
  done
done < <(grep -rn 'CONCLAVE-TODO(' --include='*.go' --include='*.rs' --include='*.sh' \
         --include='*.proto' x/ app/ crypto/ solver/ scripts/ proto/ demo/ 2>/dev/null)
[ "$UNOWNED" -eq 0 ] && echo "    every marker names at least one task"

hdr "8. COMMIT CITATION — every commit must be cited somewhere (F-13)"
# The gap relocates to the newest commit every time, so it is checked, not fixed.
# HEAD is the ONE tolerated exception: a commit cannot cite its own hash.
HEADSHA=$(git rev-parse --short=7 HEAD)
MISSING=""
while read -r sha; do
  [ "$sha" = "$HEADSHA" ] && continue
  if ! grep -rqF "$sha" docs/ CLAUDE.md 2>/dev/null; then
    MISSING="$MISSING $sha"
  fi
done < <(git log --format='%h' --abbrev=7)
if [ -n "$MISSING" ]; then
  echo "FINDING — commits cited nowhere:$MISSING"; RC=1
else
  echo "clean — every commit except HEAD ($HEADSHA, cannot cite itself) is cited"
fi

echo
if [ "$RC" -eq 0 ]; then
  echo "inventory_rederive: asserting steps (6,7,8) CLEAN"
else
  echo "inventory_rederive: FINDINGS above — each must get an owner (Rule 6)" >&2
fi
exit "$RC"
