#!/usr/bin/env bash
#
# F-1 / D-063 determinism gate.
#
# Runs TestAppStateDeterminism over the committed seed vector TWICE, in two
# independent `go test` process invocations, and FAILS (non-zero exit) on:
#   - any SKIP                  (the harness must never skip itself — failOnSkipTB
#                                converts a sim SKIP to a failure, and we also
#                                guard against the package being skipped wholesale)
#   - any abort / non-zero rc   (invariant break, hard failure, build error)
#   - fewer hashes than seeds   (a seed silently dropped)
#   - any app-hash MISMATCH     within a run (asserted in-test) or ACROSS the two
#                                runs (cross-process reproducibility — checked here)
#
# Not part of `make test`: the simulation is -Enabled-gated (D-057), so this gate
# is invoked explicitly via `make sim-determinism`.
set -uo pipefail

cd "$(dirname "$0")/.."

SEEDS="${CONCLAVE_SIM_SEEDS:-1,2,7,12,42}"
NSEEDS=$(awk -F, '{print NF}' <<<"$SEEDS")

run_pass() {
	go test ./app -run '^TestAppStateDeterminism$' -v -timeout 30m \
		-Enabled -ConclaveSimSeeds="$SEEDS"
}

fail() {
	echo "SIM-DETERMINISM FAIL: $1" >&2
	exit 1
}

echo "== sim-determinism: $NSEEDS seeds [$SEEDS], two independent processes =="

echo "== pass 1 =="
OUT1="$(run_pass 2>&1)"
RC1=$?
echo "$OUT1"

echo "== pass 2 =="
OUT2="$(run_pass 2>&1)"
RC2=$?
echo "$OUT2"

[ "$RC1" -eq 0 ] || fail "pass 1 exited non-zero ($RC1) — abort / hard failure"
[ "$RC2" -eq 0 ] || fail "pass 2 exited non-zero ($RC2) — abort / hard failure"

# A SKIP must never appear (failOnSkipTB should have turned a sim skip into a
# failure; this also catches the whole package/test being skipped).
if grep -Eq -- '^--- SKIP|^=== SKIP|^ok .*\[no tests to run\]|no tests to run' <<<"$OUT1$OUT2"; then
	fail "a SKIP / no-tests-run appeared — the gate must never skip"
fi

H1="$(grep -oE 'DETERMINISM-HASH seed=[0-9-]+ hash=[0-9A-F]+' <<<"$OUT1" | sort)"
H2="$(grep -oE 'DETERMINISM-HASH seed=[0-9-]+ hash=[0-9A-F]+' <<<"$OUT2" | sort)"

NH1=$(grep -c . <<<"$H1")
[ "$NH1" -eq "$NSEEDS" ] || fail "pass 1 emitted $NH1 hashes, expected $NSEEDS (a seed was skipped/dropped)"

if [ "$H1" != "$H2" ]; then
	echo "--- pass 1 hashes ---"; echo "$H1"
	echo "--- pass 2 hashes ---"; echo "$H2"
	fail "cross-process app-hash MISMATCH between the two invocations"
fi

echo
echo "SIM-DETERMINISM PASS: $NSEEDS seeds byte-identical across two independent processes"
echo "$H1"
