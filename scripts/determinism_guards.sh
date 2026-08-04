#!/usr/bin/env bash
#
# scripts/determinism_guards.sh — the machine-enforced half of SPEC §11.6, plus
# the dependency-pin guards. Run by `make guards`; run by CI on every push.
#
# WHY A SCRIPT AND NOT JUST THE LINTER (task 3.3a, D-099 — read this before
# deleting anything here as redundant). `.golangci.yml` carries forbidigo rules
# for the same two bans, and they are a SECOND layer, not the enforcement:
# forbidigo was measured by sabotage to fire on `t := time.Now()` and
# `var f float64` while MISSING `return time.Now()...`, struct fields, function
# parameters and `float64(x)` conversions — identically on golangci-lint v1.64.8
# and v2.12.2, so this is a long-standing property of the tool, not a migration
# regression. A ban with positional blind spots is not a ban. These greps have
# none.
#
# Guards:
#   G1  AUDIT F-5 — no float types anywhere in consensus code (was review-only
#       for two phases while this repo's lint config HEADER claimed otherwise)
#   G2  SPEC §11.6 — no wall-clock time in consensus code (closes the forbidigo
#       hole above; the ban itself is Phase-0's)
#   G3  AUDIT F-6 — the goleveldb `replace` pin has no regression guard, so a
#       careless `go get -u` could silently drop it (D-032)
#   G4  dependency-EOL watch — the platform pins are asserted against a manifest
#       kept HERE, so drift is a build failure rather than a discovery
#
# Every guard prints what it checked, not merely that it passed: a guard whose
# output is indistinguishable from a no-op is how F-5 survived two phases.
set -uo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO"

RC=0
fail() { echo "  ✗ FAIL: $*" >&2; RC=1; }
pass() { echo "  ✓ $*"; }

# Consensus surface = the code that runs inside a block. Tests, generated
# protobuf, the CLI, and the SDK simulation shims are deliberately outside it,
# exactly as .golangci.yml's exclusions define them.
consensus_files() {
  find x app crypto solver -name '*.go' -type f 2>/dev/null \
    | grep -v '_test\.go$' \
    | grep -v '\.pb\.go$' \
    | grep -v '\.pb\.gw\.go$' \
    | grep -v '/simulation/' \
    | sort
}

NFILES=$(consensus_files | wc -l)
echo "== determinism guards over $NFILES consensus files =="

# ---------------------------------------------------------------- G1: floats
echo "-- G1: float ban (AUDIT F-5, SPEC §11.6)"
G1HITS=$(consensus_files | xargs grep -nE '\bfloat(32|64)\b' 2>/dev/null || true)
if [ -n "$G1HITS" ]; then
  fail "float types in consensus code — use math.LegacyDec / math.Int"
  echo "$G1HITS" | sed 's/^/      /' >&2
else
  pass "no float32/float64 in consensus code"
fi

# ------------------------------------------------------------ G2: wall clock
echo "-- G2: wall-clock ban (SPEC §11.6)"
G2HITS=$(consensus_files | xargs grep -nE '\btime\.(Now|Since)\(' 2>/dev/null || true)
if [ -n "$G2HITS" ]; then
  fail "wall-clock time in consensus code — use ctx.BlockTime()/BlockHeight()"
  echo "$G2HITS" | sed 's/^/      /' >&2
else
  pass "no time.Now()/time.Since() in consensus code"
fi

# -------------------------------------------------------------- G3: goleveldb
echo "-- G3: goleveldb replace pin (AUDIT F-6, D-032)"
GOLEVELDB_PIN='github.com/syndtr/goleveldb v1.0.1-0.20210819022825-2ae1ddf74ef7'
if grep -qF "=> $GOLEVELDB_PIN" go.mod; then
  pass "replace directive present and exact: $GOLEVELDB_PIN"
else
  fail "the goleveldb replace pin is missing or changed (expected '=> $GOLEVELDB_PIN')"
  grep -n 'goleveldb' go.mod | sed 's/^/      /' >&2
fi
# The pin only matters if it is what actually RESOLVES — a replace can be
# present and overridden. Check the resolved build list too (Rule 4: existence
# is not enforcement).
if command -v go >/dev/null 2>&1; then
  RESOLVED=$(go list -m github.com/syndtr/goleveldb 2>/dev/null || echo "UNRESOLVED")
  case "$RESOLVED" in
    *"2ae1ddf74ef7"*) pass "resolves to the pinned revision: $RESOLVED" ;;
    *)                fail "goleveldb RESOLVES to '$RESOLVED', not the pinned revision" ;;
  esac
fi

# ------------------------------------------------------- G4: dependency watch
# The manifest is the DECLARED platform. Any drift — deliberate or from a stray
# `go get -u` — fails here and must be re-decided, which is the whole point:
# this is the class of rot task 3.3 exists to end. Update this list ONLY in a
# task that records the version change as an ADR.
echo "-- G4: platform pin manifest (dependency-EOL watch)"
while read -r mod want; do
  [ -z "$mod" ] && continue
  got=$(go list -m -f '{{.Version}}' "$mod" 2>/dev/null || echo "MISSING")
  if [ "$got" = "$want" ]; then
    pass "$mod $got"
  else
    fail "$mod is $got, manifest says $want — re-decide it in an ADR, do not edit this line casually"
  fi
done <<'MANIFEST'
github.com/cosmos/cosmos-sdk v0.50.15
github.com/cometbft/cometbft v0.38.25
github.com/cosmos/ibc-go/v8 v8.8.0
github.com/CosmWasm/wasmd v0.54.9
github.com/skip-mev/block-sdk/v2 v2.1.5
github.com/cosmos/iavl v1.2.8
github.com/drand/kyber v1.3.1
github.com/drand/kyber-bls12381 v0.3.1
github.com/consensys/gnark v0.13.0
github.com/consensys/gnark-crypto v0.18.1
MANIFEST

# The `go` directive is part of the platform and has its own EOL clock.
echo "-- G4b: go directive"
GODIR=$(grep -E '^go ' go.mod | awk '{print $2}')
GOTC=$(grep -E '^toolchain ' go.mod | awk '{print $2}')
EXPECT_GODIR="1.26.0"
EXPECT_GOTC="go1.26.5"
if [ "$GODIR" = "$EXPECT_GODIR" ] && [ "$GOTC" = "$EXPECT_GOTC" ]; then
  pass "go $GODIR / toolchain $GOTC"
else
  fail "go directive is '$GODIR'/'$GOTC', manifest says '$EXPECT_GODIR'/'$EXPECT_GOTC'"
fi

echo
if [ "$RC" -eq 0 ]; then
  echo "=================================="
  echo " determinism guards: ALL PASSED   "
  echo "=================================="
else
  echo "determinism guards: FAILED" >&2
fi
exit "$RC"
