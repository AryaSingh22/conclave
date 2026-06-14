#!/usr/bin/env bash
# scripts/demo_e2e.sh — the SPEC §1.3 acceptance script (§10 row 0.10).
#
# Boots a single-node localnet, then drives the full §1.3 scenario end to end,
# CLEARED BY THE REFERENCE SOLVER (cmd/conclave-solver):
#
#   steps 1-3  conclave-demo setup            sigils, alice's mandate, the
#                                             over-ceiling ErrMandateExceeded reject
#   steps 4-6  conclave-demo cycle-and-assert the 3-party CYCLE is submitted as
#                                             intents; the bonded solver computes
#                                             the cycle match and submits it; ONE
#                                             Conclude settles the 3-leg cycle;
#                                             receipts queryable; reputation moved
#   step 7     kill the solver; conclave-demo fallback
#                                             a 2-party swap clears via the
#                                             in-consensus fallback (liveness),
#                                             while a fresh 3-party cycle stays
#                                             unmatched without a solver — proving
#                                             solver/cycles is load-bearing
#
# The cycle case is the proof: the naive pairwise matcher (and the in-consensus
# fallback) CANNOT clear a 3-party cycle, so only the solver's cycle finder
# clears step 4-6, and step 7 shows the cycle staying unmatched once it is gone.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO"

export PATH="$(go env GOPATH)/bin:$PATH"
export CONCLAVED_HOME="${CONCLAVED_HOME:-$HOME/.conclaved}"
export CONCLAVE_GRPC="${CONCLAVE_GRPC:-localhost:9090}"
export CONCLAVE_CHAIN_ID="${CONCLAVE_CHAIN_ID:-conclave-dev-1}"

NODE_LOG="$(mktemp -t conclave-node.XXXXXX.log)"
SOLVER_LOG="$(mktemp -t conclave-solver.XXXXXX.log)"
NODE_PID=""
SOLVER_PID=""

cleanup() {
  set +e
  [ -n "$SOLVER_PID" ] && kill "$SOLVER_PID" >/dev/null 2>&1
  [ -n "$NODE_PID" ] && kill "$NODE_PID" >/dev/null 2>&1
  wait >/dev/null 2>&1
}
trap cleanup EXIT

echo "== building conclaved + reference solver + demo driver =="
make install >/dev/null
go install ./cmd/conclave-solver ./cmd/conclave-demo

echo "== booting localnet (logs: $NODE_LOG) =="
bash ./scripts/localnet.sh >"$NODE_LOG" 2>&1 &
NODE_PID=$!

echo "== waiting for block production + gRPC =="
ready=0
for _ in $(seq 1 60); do
  if ! kill -0 "$NODE_PID" >/dev/null 2>&1; then
    echo "node exited early; log:" >&2; tail -n 40 "$NODE_LOG" >&2; exit 1
  fi
  h="$(conclaved status --home "$CONCLAVED_HOME" 2>/dev/null | jq -r '.sync_info.latest_block_height // empty' 2>/dev/null || true)"
  if [ -n "${h:-}" ] && [ "$h" -ge 3 ] 2>/dev/null; then ready=1; break; fi
  sleep 1
done
[ "$ready" = 1 ] || { echo "node did not produce blocks in time; log:" >&2; tail -n 40 "$NODE_LOG" >&2; exit 1; }
sleep 2 # gRPC settle

echo
echo "== §1.3 steps 1-3: setup (sigils, mandate, over-ceiling reject) =="
conclave-demo setup

echo
echo "== starting the reference solver as a fresh bonded solver (logs: $SOLVER_LOG) =="
conclave-solver -from solver2 -bond 100000000uclv >"$SOLVER_LOG" 2>&1 &
SOLVER_PID=$!

echo
echo "== §1.3 steps 4-6: the 3-party cycle cleared by the solver =="
conclave-demo cycle-and-assert

echo
echo "== §1.3 step 7: killing the solver, exercising the in-consensus fallback =="
kill "$SOLVER_PID" >/dev/null 2>&1 || true
wait "$SOLVER_PID" 2>/dev/null || true
SOLVER_PID=""
conclave-demo fallback

echo
echo "=========================================="
echo " demo_e2e.sh: SPEC §1.3 PASSED end-to-end "
echo "=========================================="
