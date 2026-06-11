#!/usr/bin/env bash
# Internal helper: debug the "version does not exist" query failure.
set -uo pipefail
export PATH="$PATH:/usr/local/go/bin:$HOME/go/bin"
cd "$HOME/conclave"

pkill -f "conclaved start" 2>/dev/null
sleep 1

LOG=/tmp/conclave-localnet.log
nohup bash scripts/localnet.sh > "$LOG" 2>&1 &

h=""
for i in $(seq 1 60); do
  h=$(curl -s localhost:26657/status 2>/dev/null | jq -r '.result.sync_info.latest_block_height' 2>/dev/null)
  if [ -n "${h:-}" ] && [ "$h" != "null" ] && [ "$h" -gt 3 ] 2>/dev/null; then break; fi
  sleep 1
done
echo "latest height: ${h:-none}"

ALICE=$(conclaved keys show alice -a --keyring-backend test --home "$HOME/.conclaved")

echo "=== query at latest (expect maybe fail) ==="
conclaved q bank balances "$ALICE" --output json 2>&1 | head -3

echo "=== query at explicit height 2 ==="
conclaved q bank balances "$ALICE" --height 2 --output json 2>&1 | head -3

echo "=== query at explicit height 1 ==="
conclaved q bank balances "$ALICE" --height 1 --output json 2>&1 | head -3

echo "=== abci_info ==="
curl -s localhost:26657/abci_info | jq -c '.result.response | {last_block_height, last_block_app_hash}'

echo "=== staking validators query ==="
conclaved q staking validators --output json 2>&1 | head -3

echo "=== node log: store/commit related lines ==="
grep -i -e prune -e "version" -e "store" "$LOG" | head -20

echo "=== node log tail ==="
tail -15 "$LOG"

pkill -f "conclaved start" 2>/dev/null
exit 0
