#!/usr/bin/env bash
# scripts/7683_surface_check.sh — the task-2.4 live acceptance
# (docs/edge/7683_SURFACE.md §7, PHASE2_DESIGN §2.4, D-086/D-087): boots a
# fresh single-node localnet and runs `conclave-demo 7683-check`, the
# resolver-shaped sweep that exercises the fill-back evidence chain the
# contract names — mandate-bound translated-fill submission, fallback
# clearing, the D-086 winning-Solution pre-image fetch, ICS-23 verification
# against the header-anchored app hash (§6.2), the §6.3 bridge checks
# (root/terms/leg/dedup), and the live negatives (under-declared fill →
# INVALID at the Reveal; over-ceiling; foreign-sigil; unbound-key).
#
# Dev tooling — not part of `make test` (the claims are proven in-tree by
# app/erc7683_surface_conformance_test.go; this proves a real node serves
# the same surface over real gRPC).
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO"

export PATH="$(go env GOPATH)/bin:$PATH"
export CONCLAVED_HOME="${CONCLAVED_HOME:-$HOME/.conclaved}"
export CONCLAVE_GRPC="${CONCLAVE_GRPC:-localhost:9090}"
export CONCLAVE_CHAIN_ID="${CONCLAVE_CHAIN_ID:-conclave-dev-1}"

NODE_LOG="$(mktemp -t conclave-7683-node.XXXXXX.log)"
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
echo "== 7683 surface sweep (docs/edge/7683_SURFACE.md §3–§6) =="
conclave-demo 7683-check

echo
echo "================================================"
echo " 7683_surface_check.sh: SURFACE CONTRACT PASSED "
echo "================================================"
