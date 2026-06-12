#!/usr/bin/env bash
# scripts/dev-unordered-blast.sh — SPEC §10 row 0.5 acceptance against a
# REAL localnet: 50 parallel unordered txs from one key land in one block,
# then the 0.4×0.5 mandate-intersection variant (exactly the under-ceiling
# prefix lands; the rest bounce in CheckTx).
#
# Boots a fresh localnet in the background, runs both blast modes, tears
# the node down. Dev tooling — not part of `make test`.
set -euo pipefail
cd "$(dirname "$0")/.."

HOME_DIR="${CONCLAVED_HOME:-$HOME/.conclaved}"
LOG="$(mktemp -t conclave-blast-node.XXXXXX.log)"

echo "Booting localnet (log: $LOG)..."
CONCLAVED_HOME="$HOME_DIR" ./scripts/localnet.sh >"$LOG" 2>&1 &
NODE_PID=$!
trap 'kill "$NODE_PID" 2>/dev/null || true; wait "$NODE_PID" 2>/dev/null || true' EXIT

# wait for the node to produce blocks (gRPC up + height > 2)
for i in $(seq 1 60); do
  if go run ./cmd/unordered-blast -count 1 -from alice -home "$HOME_DIR" >/dev/null 2>&1; then
    break
  fi
  if ! kill -0 "$NODE_PID" 2>/dev/null; then
    echo "node died during startup; tail of log:" >&2
    tail -20 "$LOG" >&2
    exit 1
  fi
  sleep 1
done

echo "== blast: 50 unordered txs, one key, wrong sequences =="
go run ./cmd/unordered-blast -count 50 -from alice -home "$HOME_DIR"

echo "== blast: mandate intersection (window admits 10 of 50) =="
go run ./cmd/unordered-blast -count 50 -mandate -from operator -home "$HOME_DIR"

echo "dev-unordered-blast: ALL PASS"
