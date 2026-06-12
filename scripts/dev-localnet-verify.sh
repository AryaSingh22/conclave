#!/usr/bin/env bash
# Internal helper used during scaffolding: task 0.1 acceptance check
# (SPEC §10: node produces blocks; accounts funded incl. uusdc).
set -uo pipefail
export PATH="$PATH:/usr/local/go/bin:$HOME/go/bin"
cd "$HOME/conclave"

echo "=== make install ==="
if ! make install 2>&1 | tail -3; then
  echo "install failed"; exit 1
fi

pkill -f "conclaved start" 2>/dev/null
sleep 1

LOG=/tmp/conclave-localnet.log
nohup bash scripts/localnet.sh > "$LOG" 2>&1 &
echo "localnet starting (log: $LOG)"

h=""
for i in $(seq 1 90); do
  h=$(curl -s localhost:26657/status 2>/dev/null | jq -r '.result.sync_info.latest_block_height' 2>/dev/null)
  if [ -n "${h:-}" ] && [ "$h" != "null" ] && [ "$h" -gt 3 ] 2>/dev/null; then
    echo "reached height $h after ~${i}s"
    break
  fi
  sleep 1
done

if [ -z "${h:-}" ] || [ "$h" = "null" ] || ! [ "$h" -gt 3 ] 2>/dev/null; then
  echo "FAILED: node did not pass height 3. Log tail:"
  tail -40 "$LOG"
  pkill -f "conclaved start" 2>/dev/null
  exit 1
fi

echo "=== node status ==="
curl -s localhost:26657/status | jq '{chain_id: .result.node_info.network, height: .result.sync_info.latest_block_height, moniker: .result.node_info.moniker}'

ALICE=$(conclaved keys show alice -a --keyring-backend test --home "$HOME/.conclaved")
echo "=== alice ($ALICE) balances ==="
conclaved q bank balances "$ALICE" --output json | jq '.balances'

echo "=== solver1 + faucet spot check ==="
SOLVER=$(conclaved keys show solver1 -a --keyring-backend test --home "$HOME/.conclaved")
conclaved q bank balances "$SOLVER" --output json | jq -c '.balances'
FAUCET=$(conclaved keys show faucet -a --keyring-backend test --home "$HOME/.conclaved")
conclaved q bank balances "$FAUCET" --output json | jq -c '.balances'

echo "=== stopping node (restart anytime with scripts/localnet.sh) ==="
pkill -f "conclaved start"
exit 0
