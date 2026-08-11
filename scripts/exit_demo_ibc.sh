#!/usr/bin/env bash
# scripts/exit_demo_ibc.sh — the Phase-2 §6 EXIT DEMO, part B (§6b).
#
# THIS IS WHERE THE SPEC §10 EXIT CRITERION IS ASSERTED.
#
# Boots TWO Conclave nodes and a LIVE RELAYER process, then runs
# `conclave-demo exit-demo-ibc`:
#
#   chain A  conclave-dev-1 — the settlement chain (§6a's whole flow re-runs here)
#   chain B  conclave-dev-2 — the ICS-20 uusdc source, and the host of the
#                             on-chain light client OF CHAIN A that stage 5
#                             verifies the settlement receipt against
#   relayer  cosmos/relayer, pinned below — a real process moving real packets.
#            D-082's obligation is explicitly a LIVE relayer, not the
#            in-process ibctesting harness task 2.5 used.
#
# CONDITION_OUTCOME IS TRUE SINCE TASK 2.9 (D-100). The settlement is
# conditioned and its receipt now reports the escrow's real outcome, so D-090's
# condition on external-facing use is DISCHARGED for this field. ~~Task 2.9 is a
# PREREQUISITE before this output may be used as external-facing evidence~~ —
# what remains is the VERIFIER side (a Conclave light client per origin chain,
# and the check-4 dedup ledger), a bridge obligation and not a node one.
#
# HONEST LIMIT (task 2.9): §6b was NOT re-run at 2.9 — it needs two chains and a
# live relayer, and the only 2.9 change reaching it is this banner plus the
# shared stage-6 assertion, which §6a DID re-run. Stated rather than implied.
#
# Dev tooling — not part of `make test`.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO"

export PATH="$(go env GOPATH)/bin:$PATH"

# The relayer is a TOOL, not a dependency of the chain: `go install pkg@version`
# builds it in its own module and never touches conclave's go.mod/go.sum. Its
# own pins (SDK v0.50.11, ibc-go v8.2.0, CometBFT v0.38.12) sit on the same
# major line as ours.
#
# NOTE: installed this way the binary reports an EMPTY version string
# (`rly version` prints version: "") because `go install` sets no ldflags — so
# THIS LINE is the pin of record. It cannot be read back from the binary.
RLY_VERSION="v2.6.0"
RLY_PKG="github.com/cosmos/relayer/v2@${RLY_VERSION}"
RLY_BIN="$(go env GOPATH)/bin/relayer" # module is /v2, so the binary is "relayer"

CHAIN_A="conclave-dev-1"
CHAIN_B="conclave-dev-2"
HOME_A="${CONCLAVED_HOME:-$HOME/.conclaved}"
HOME_B="${CONCLAVED_HOME_B:-$HOME/.conclaved-b}"
RLY_HOME="${RLY_HOME:-$HOME/.relayer-conclave-demo}"
PATH_NAME="conclave-demo"

# chain B's ports, offset so both nodes coexist on one box
B_RPC=26757; B_P2P=26756; B_GRPC=9190; B_API=1417; B_PPROF=6160

export CONCLAVE_GRPC="localhost:9090"
export CONCLAVE_CHAIN_ID="$CHAIN_A"
export CONCLAVE_GRPC_B="localhost:${B_GRPC}"
export CONCLAVED_HOME_B="$HOME_B"
export CONCLAVE_CHAIN_ID_B="$CHAIN_B"

CONTRACT="demo/cvm-contract/artifacts/conclave_demo_escrow.wasm"
if [ ! -f "$CONTRACT" ]; then
  echo "error: $CONTRACT missing — run 'make demo-contract' (needs the Rust wasm32 target)" >&2
  exit 1
fi

LOG_A="$(mktemp -t conclave-exit-a.XXXXXX.log)"
LOG_B="$(mktemp -t conclave-exit-b.XXXXXX.log)"
LOG_RLY="$(mktemp -t conclave-exit-rly.XXXXXX.log)"
PID_A=""; PID_B=""; PID_RLY=""

cleanup() {
  set +e
  [ -n "$PID_RLY" ] && kill "$PID_RLY" >/dev/null 2>&1
  [ -n "$PID_A" ] && kill "$PID_A" >/dev/null 2>&1
  [ -n "$PID_B" ] && kill "$PID_B" >/dev/null 2>&1
  wait >/dev/null 2>&1
}
trap cleanup EXIT

echo "== building conclaved + demo driver =="
make install >/dev/null
go install ./cmd/conclave-demo

if [ ! -x "$RLY_BIN" ]; then
  echo "== installing the relayer ($RLY_PKG) =="
  go install "$RLY_PKG"
fi
echo "== relayer: $RLY_BIN (pinned $RLY_VERSION; its own version string is empty by design) =="

# --- boot both chains -------------------------------------------------------
# Chain A widens the session/auction window exactly as §6a does: the driver must
# WIN the auction with its conditioned solution. Demo timing only.
# Both chains keep every version (pruning=nothing): stage 5 fetches an ICS-23
# proof at the height chain B's light client holds, and a pruned version cannot
# serve one.
echo "== booting chain A ($CHAIN_A; logs: $LOG_A) =="
env CONCLAVED_HOME="$HOME_A" CHAIN_ID="$CHAIN_A" MONIKER="conclave-a" PRUNING="nothing" \
    SESSION_LENGTH_BLOCKS=12 SOLUTION_WINDOW_BLOCKS=8 \
    bash ./scripts/localnet.sh >"$LOG_A" 2>&1 &
PID_A=$!

echo "== booting chain B ($CHAIN_B; logs: $LOG_B) =="
env CONCLAVED_HOME="$HOME_B" CHAIN_ID="$CHAIN_B" MONIKER="conclave-b" PRUNING="nothing" \
    RPC_PORT="$B_RPC" P2P_PORT="$B_P2P" GRPC_PORT="$B_GRPC" API_PORT="$B_API" PPROF_PORT="$B_PPROF" \
    bash ./scripts/localnet.sh >"$LOG_B" 2>&1 &
PID_B=$!

wait_for_chain() { # name pid home rpcport log
  local name="$1" pid="$2" home="$3" port="$4" log="$5" h
  for _ in $(seq 1 90); do
    if ! kill -0 "$pid" >/dev/null 2>&1; then
      echo "$name exited early; log:" >&2; tail -n 40 "$log" >&2; exit 1
    fi
    h="$(conclaved status --home "$home" --node "tcp://localhost:${port}" 2>/dev/null \
         | jq -r '.sync_info.latest_block_height // empty' 2>/dev/null || true)"
    if [ -n "${h:-}" ] && [ "$h" -ge 3 ] 2>/dev/null; then return 0; fi
    sleep 1
  done
  echo "$name did not produce blocks in time; log:" >&2; tail -n 40 "$log" >&2; exit 1
}
echo "== waiting for block production on both chains =="
wait_for_chain "chain A" "$PID_A" "$HOME_A" 26657 "$LOG_A"
wait_for_chain "chain B" "$PID_B" "$HOME_B" "$B_RPC" "$LOG_B"
sleep 2 # gRPC settle

# --- configure the relayer --------------------------------------------------
echo "== configuring the relayer (home: $RLY_HOME) =="
rm -rf "$RLY_HOME"
"$RLY_BIN" --home "$RLY_HOME" config init

chain_json() { # chain-id rpc-port key
  cat <<EOF
{"type":"cosmos","value":{
  "key":"$3","chain-id":"$1","rpc-addr":"http://localhost:$2",
  "account-prefix":"conc","keyring-backend":"test","gas-adjustment":1.5,
  "gas-prices":"0uclv","min-gas-amount":0,"debug":true,"timeout":"20s",
  "output-format":"json","sign-mode":"direct","coin-type":118,
  "broadcast-mode":"sync"}}
EOF
}
chain_json "$CHAIN_A" 26657 "rly-a" > "$RLY_HOME/chain-a.json"
chain_json "$CHAIN_B" "$B_RPC" "rly-b" > "$RLY_HOME/chain-b.json"
"$RLY_BIN" --home "$RLY_HOME" chains add --file "$RLY_HOME/chain-a.json" "$CHAIN_A"
"$RLY_BIN" --home "$RLY_HOME" chains add --file "$RLY_HOME/chain-b.json" "$CHAIN_B"

ADDR_A="$("$RLY_BIN" --home "$RLY_HOME" keys add "$CHAIN_A" rly-a | jq -r '.address')"
ADDR_B="$("$RLY_BIN" --home "$RLY_HOME" keys add "$CHAIN_B" rly-b | jq -r '.address')"
echo "relayer keys: A=$ADDR_A  B=$ADDR_B"

# The relayer signs its own txs, so its accounts must exist and hold gas.
fund() { # home rpcport addr chain-id
  conclaved tx bank send faucet "$3" 100000000uclv \
    --home "$1" --node "tcp://localhost:$2" --chain-id "$4" \
    --keyring-backend test --gas 300000 -y >/dev/null
}
fund "$HOME_A" 26657 "$ADDR_A" "$CHAIN_A"
fund "$HOME_B" "$B_RPC" "$ADDR_B" "$CHAIN_B"
sleep 3

echo "== opening clients + connection + ICS-20 channel (rly tx link) =="
"$RLY_BIN" --home "$RLY_HOME" paths new "$CHAIN_A" "$CHAIN_B" "$PATH_NAME"
"$RLY_BIN" --home "$RLY_HOME" tx link "$PATH_NAME" \
  --src-port transfer --dst-port transfer --version ics20-1 --timeout 30s 2>&1 | tail -n 20

echo "== starting the live relayer (logs: $LOG_RLY) =="
"$RLY_BIN" --home "$RLY_HOME" start "$PATH_NAME" --debug >"$LOG_RLY" 2>&1 &
PID_RLY=$!
sleep 5
if ! kill -0 "$PID_RLY" >/dev/null 2>&1; then
  echo "the relayer exited immediately; log:" >&2; tail -n 40 "$LOG_RLY" >&2; exit 1
fi

echo
conclave-demo exit-demo-ibc || {
  rc=$?
  echo >&2
  echo "== relayer log tail (packet discovery lives here) ==" >&2
  tail -n 60 "$LOG_RLY" >&2
  exit $rc
}

echo
echo "==========================================================="
echo " exit_demo_ibc.sh: PHASE-2 §6 EXIT CRITERION ASSERTED "
echo "==========================================================="
