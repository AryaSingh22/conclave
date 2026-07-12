#!/usr/bin/env bash
# scripts/x402_surface_check.sh — the task-2.3 live acceptance
# (docs/edge/X402_SURFACE.md §7, PHASE2_DESIGN §2.3, D-083): boots a fresh
# single-node localnet and runs `conclave-demo x402-check`, the
# gateway-shaped sweep that exercises every node-side endpoint the
# X402_SURFACE.md contract names — sigil/mandate setup, MsgSubmitIntent
# with the gas fee fronted in uusdc (§4 fee abstraction), the fallback
# clearing of a 2-party swap, Receipts/Receipt/ReceiptProof settlement
# evidence, MsgCancelIntent, and the live auth chokepoints (foreign-sigil,
# unbound-key, over-ceiling, cross-sigil rebind — D-084).
#
# Dev tooling — not part of `make test` (the auth model is proven in-tree
# by app/x402_surface_conformance_test.go; this proves a real node serves
# the same surface over real gRPC).
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO"

export PATH="$(go env GOPATH)/bin:$PATH"
export CONCLAVED_HOME="${CONCLAVED_HOME:-$HOME/.conclaved}"
export CONCLAVE_GRPC="${CONCLAVE_GRPC:-localhost:9090}"
export CONCLAVE_CHAIN_ID="${CONCLAVE_CHAIN_ID:-conclave-dev-1}"

NODE_LOG="$(mktemp -t conclave-x402-node.XXXXXX.log)"
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
echo "== x402 surface sweep (docs/edge/X402_SURFACE.md §3–§5) =="
conclave-demo x402-check

echo
echo "================================================"
echo " x402_surface_check.sh: SURFACE CONTRACT PASSED "
echo "================================================"
