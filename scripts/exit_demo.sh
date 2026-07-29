#!/usr/bin/env bash
# scripts/exit_demo.sh — the Phase-2 §6 EXIT DEMO, part A (§6a).
#
# Boots a single-node localnet and runs `conclave-demo exit-demo`, which drives
# three of the four exit legs TOGETHER on a live node for the first time:
#
#   x402 ingress (2.3)  a Model-A gateway principal submits a mandate-bound buy
#                       with gas fronted in the settlement denom (§6.2)
#   ZK escrow (2.1b)    the buy is a CONDITIONED ZK_DELIVERY purchase; both
#                       parties sign the same condition and CONCLUDE routes the
#                       payment into a funded escrow
#   CVM release (2.2)   the purpose-built CVM-schema contract, uploaded through
#                       GOVERNANCE, submits the groth16 proof and releases it —
#                       D-079's deferred localnet contract-release smoke
#
# The fourth leg (real IBC USDC over a live relayer + receipt verification
# against a second chain's light client) is §6b's.
#
# INTERNAL ACCEPTANCE RUN (D-090). The settlement is conditioned, so its receipt
# reads condition_outcome = NONE (D-087). The demo asserts that boundary rather
# than avoiding it. Task 2.9 is a PREREQUISITE before this output may be used as
# external-facing evidence.
#
# Dev tooling — not part of `make test`.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO"

export PATH="$(go env GOPATH)/bin:$PATH"
export CONCLAVED_HOME="${CONCLAVED_HOME:-$HOME/.conclaved}"
export CONCLAVE_GRPC="${CONCLAVE_GRPC:-localhost:9090}"
export CONCLAVE_CHAIN_ID="${CONCLAVE_CHAIN_ID:-conclave-dev-1}"

# The driver must win the auction with its conditioned solution; the SPEC §6.4
# dev defaults (3/2 blocks) are a race it can lose to the fallback matcher on a
# 1s-block node. Demo timing only — no code path changes. See localnet.sh.
export SESSION_LENGTH_BLOCKS="${SESSION_LENGTH_BLOCKS:-12}"
export SOLUTION_WINDOW_BLOCKS="${SOLUTION_WINDOW_BLOCKS:-8}"

CONTRACT="demo/cvm-contract/artifacts/conclave_demo_escrow.wasm"
if [ ! -f "$CONTRACT" ]; then
  echo "error: $CONTRACT missing — run 'make demo-contract' (needs the Rust wasm32 target)" >&2
  exit 1
fi

NODE_LOG="$(mktemp -t conclave-exit-node.XXXXXX.log)"
NODE_PID=""

cleanup() {
  set +e
  [ -n "$NODE_PID" ] && kill "$NODE_PID" >/dev/null 2>&1
  wait >/dev/null 2>&1
}
trap cleanup EXIT

echo "== building conclaved + demo driver =="
make install >/dev/null
go install ./cmd/conclave-demo

echo "== booting localnet (session=${SESSION_LENGTH_BLOCKS}b window=${SOLUTION_WINDOW_BLOCKS}b; logs: $NODE_LOG) =="
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
conclave-demo exit-demo

echo
echo "======================================================"
echo " exit_demo.sh: PHASE-2 §6 EXIT DEMO (part A) PASSED "
echo "======================================================"
