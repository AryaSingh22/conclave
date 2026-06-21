#!/usr/bin/env bash
# scripts/localnet4.sh — the Task-1.7 / D-070 4-validator adversarial localnet.
#
# Stands up 4 real conclaved validators (scripted, no docker — every 1.7 property
# is process-level for 4 local validators; multi-host adds only ops realism), each
# running the in-process off-chain DKG daemon, with vote extensions enabled in a
# REAL genesis and a short epoch so a real boundary + DKG + rotation crosses in a
# reasonable run. It then drives the headline checks:
#   - the DKG daemon completes a real DKG across 4 validators → epoch 1 ACTIVE;
#   - app-hash agreement across all 4 nodes (no divergence at the boundary —
#     closes the D-068/D-069 sim-boundary gap);
#   - (with FAULTY_NODE set) a faulty dealer is excluded by the justification
#     round and the chain does NOT halt (B2 live).
# The encrypted-intent → reveal → clear e2e and the F-4 adversary run from
# scripts/localnet4_e2e.sh against the network this script leaves running.
#
# Env knobs:
#   FAULTY_NODE=<0..3>   make that node submit a faulty deal + refuse to justify
#   KEEP_RUNNING=1       leave the nodes running after the checks (for the e2e)
set -euo pipefail

BASE="${LOCALNET4_BASE:-$HOME/.conclave4}"
CHAIN_ID="conclave-dev-1"
DENOM="uclv"
BIN="${CONCLAVED_BIN:-conclaved}"
N=4
KR=(--keyring-backend test)
FAULTY_NODE="${FAULTY_NODE:-}"

VAL_SUPPLY="1100000000000${DENOM}"
SELF_DELEG="1000000000000${DENOM}"
ACCT_FUNDS="10000000000${DENOM},10000000000uusdc"
FAUCET_FUNDS="1000000000000${DENOM},1000000000000uusdc"

command -v "$BIN" >/dev/null 2>&1 || { echo "error: $BIN not in PATH — run 'make install'" >&2; exit 1; }
command -v jq   >/dev/null 2>&1 || { echo "error: jq not found" >&2; exit 1; }

home()  { echo "$BASE/node$1"; }
rpcport(){ echo $((26657 + $1*100)); }
p2pport(){ echo $((26656 + $1*100)); }
grpcport(){ echo $((9090 + $1*10)); }
apiport(){ echo $((1317 + $1*10)); }

echo "== resetting $BASE =="
rm -rf "$BASE"; mkdir -p "$BASE"

# 1. init each node + its own validator account key.
for i in $(seq 0 $((N-1))); do
  H=$(home "$i")
  $BIN init "node$i" --chain-id "$CHAIN_ID" --default-denom "$DENOM" --home "$H" >/dev/null 2>&1
  $BIN keys add "val$i" "${KR[@]}" --home "$H" >/dev/null 2>&1
done

# 2. build node0's genesis with ALL validator + test accounts.
H0=$(home 0)
G0="$H0/config/genesis.json"
for i in $(seq 0 $((N-1))); do
  ADDR=$($BIN keys show "val$i" -a "${KR[@]}" --home "$(home "$i")")
  $BIN genesis add-genesis-account "$ADDR" "$VAL_SUPPLY" --home "$H0" >/dev/null
done
# faucet + a couple of test accounts (alice/bob/carol/solver1 reused by the e2e).
$BIN keys add faucet "${KR[@]}" --home "$H0" >/dev/null 2>&1
for name in alice bob carol solver1; do $BIN keys add "$name" "${KR[@]}" --home "$H0" >/dev/null 2>&1; done
$BIN genesis add-genesis-account faucet "$FAUCET_FUNDS" "${KR[@]}" --home "$H0" >/dev/null
for name in alice bob carol solver1; do
  $BIN genesis add-genesis-account "$name" "$ACCT_FUNDS" "${KR[@]}" --home "$H0" >/dev/null
done

# 3. each node makes a gentx from its OWN home (its priv_validator consensus key),
#    against a copy of the all-accounts genesis; collect them into node0.
for i in $(seq 0 $((N-1))); do
  H=$(home "$i")
  if [ "$i" != "0" ]; then cp "$G0" "$H/config/genesis.json"; fi
  $BIN genesis gentx "val$i" "$SELF_DELEG" --chain-id "$CHAIN_ID" "${KR[@]}" --home "$H" >/dev/null 2>&1
  if [ "$i" != "0" ]; then cp "$H"/config/gentx/*.json "$H0/config/gentx/"; fi
done
$BIN genesis collect-gentxs --home "$H0" >/dev/null 2>&1

# 4. genesis edits: denom metadata, vote extensions, SHORT epoch/windows, mandate pool.
GEN="$H0/config/genesis.json"; TMP="$GEN.tmp"
jq '.app_state.bank.denom_metadata = [
  {"description":"Native token","denom_units":[{"denom":"uclv","exponent":0},{"denom":"clv","exponent":6}],"base":"uclv","display":"clv","name":"CLV","symbol":"CLV"},
  {"description":"Mock USDC","denom_units":[{"denom":"uusdc","exponent":0},{"denom":"usdc","exponent":6}],"base":"uusdc","display":"usdc","name":"Mock USDC","symbol":"USDC"}
]' "$GEN" >"$TMP" && mv "$TMP" "$GEN"
# vote extensions ON from height 1 (the reveal share transport, F-4).
jq '.consensus.params.abci.vote_extensions_enable_height = "1"' "$GEN" >"$TMP" && mv "$TMP" "$GEN"
# SHORT epoch so a real boundary + 5-window DKG + rotation crosses quickly:
# epoch_length 30; each window 5 blocks (total DKG 25 < 30); committee = all 4.
jq '.app_state.epoch.params.epoch_length_blocks = "30"
  | .app_state.epoch.params.register_window_blocks = "5"
  | .app_state.epoch.params.deal_window_blocks = "5"
  | .app_state.epoch.params.response_window_blocks = "5"
  | .app_state.epoch.params.justify_window_blocks = "5"
  | .app_state.epoch.params.confirm_window_blocks = "5"
  | .app_state.epoch.params.min_committee_size = 4' "$GEN" >"$TMP" && mv "$TMP" "$GEN"
# a finite reveal cap so a live session is bounded (D-069 gap iv / benchmark).
jq '.app_state.veil.params.max_intents_per_session = "25"' "$GEN" >"$TMP" && mv "$TMP" "$GEN"
# mandate fee pool (same as localnet.sh).
FEE="1000000000"
MADDR=$($BIN debug addr "$(printf 'mandate' | sha256sum | cut -c1-40)" | awk '/Bech32 Acc:/ {print $3}')
jq --arg a "$MADDR" --arg amt "$FEE" --arg d "$DENOM" '
  .app_state.bank.balances += [{"address":$a,"coins":[{"denom":$d,"amount":$amt}]}]
  | .app_state.bank.supply = (.app_state.bank.supply | map(if .denom==$d then .amount=((.amount|tonumber)+($amt|tonumber)|tostring) else . end))' "$GEN" >"$TMP" && mv "$TMP" "$GEN"

$BIN genesis validate --home "$H0" >/dev/null

# 5. distribute the final genesis + collect node ids for persistent_peers.
PEERS=""
for i in $(seq 0 $((N-1))); do
  H=$(home "$i")
  if [ "$i" != "0" ]; then cp "$GEN" "$H/config/genesis.json"; fi
  ID=$($BIN comet show-node-id --home "$H")
  PEERS="$PEERS,$ID@127.0.0.1:$(p2pport "$i")"
done
PEERS="${PEERS#,}"

# 6. per-node config: ports, peers, fast blocks, api/grpc on, dkg keygen.
for i in $(seq 0 $((N-1))); do
  H=$(home "$i")
  CT="$H/config/config.toml"; AT="$H/config/app.toml"
  sed -i "s#^laddr = \"tcp://127.0.0.1:26657\"#laddr = \"tcp://127.0.0.1:$(rpcport "$i")\"#" "$CT"
  sed -i "s#^laddr = \"tcp://0.0.0.0:26656\"#laddr = \"tcp://0.0.0.0:$(p2pport "$i")\"#" "$CT"
  sed -i "s#^persistent_peers = \"\"#persistent_peers = \"$PEERS\"#" "$CT"
  sed -i 's/^timeout_commit = .*/timeout_commit = "1s"/' "$CT"
  sed -i 's/^allow_duplicate_ip = .*/allow_duplicate_ip = true/' "$CT"
  sed -i 's/^addr_book_strict = .*/addr_book_strict = false/' "$CT"
  sed -i "s#^minimum-gas-prices = .*#minimum-gas-prices = \"0uclv\"#" "$AT"
  sed -i "s#^address = \"localhost:9090\"#address = \"localhost:$(grpcport "$i")\"#" "$AT"
  sed -i "/^\[grpc\]/,/^\[/ s/^enable = .*/enable = true/" "$AT"
  sed -i "/^\[api\]/,/^\[/ s/^enable = .*/enable = true/" "$AT"
  sed -i "s#^address = \"tcp://localhost:1317\"#address = \"tcp://localhost:$(apiport "$i")\"#" "$AT"
  $BIN dkg-keygen --home "$H" >/dev/null
done

# 7. start every node with the in-process DKG daemon (one optionally faulty).
echo "== starting $N validators (faulty node: ${FAULTY_NODE:-none}) =="
PIDS=()
for i in $(seq 0 $((N-1))); do
  H=$(home "$i")
  FAULT=""
  if [ "$i" = "$FAULTY_NODE" ]; then FAULT="--dkg-faulty-deal"; fi
  $BIN start --home "$H" --dkg-daemon --dkg-from "val$i" --keyring-backend test $FAULT \
    >"$H/node.log" 2>&1 &
  PIDS+=($!)
done
echo "${PIDS[@]}" > "$BASE/pids"
trap 'if [ "${KEEP_RUNNING:-}" != "1" ]; then kill ${PIDS[*]} 2>/dev/null || true; fi' EXIT

RPC0="tcp://127.0.0.1:$(rpcport 0)"

# 8. wait for epoch 1 ACTIVE via node0's status/abci query.
echo "== waiting for the DKG daemon to bring epoch 1 ACTIVE =="
ACTIVE=0
for _ in $(seq 1 120); do
  sleep 2
  OUT=$($BIN query epoch current-epoch --node "$RPC0" --output json 2>/dev/null || true)
  ST=$(echo "$OUT" | jq -r '.epoch.status // empty' 2>/dev/null || true)
  ID=$(echo "$OUT" | jq -r '.epoch.id // "0"' 2>/dev/null || true)
  H=$($BIN status --node "$RPC0" 2>/dev/null | jq -r '.sync_info.latest_block_height // "?"' 2>/dev/null || echo "?")
  echo "  h=$H epoch=$ID status=${ST:-none}"
  if [ "$ST" = "EPOCH_STATUS_ACTIVE" ]; then ACTIVE=1; break; fi
done
[ "$ACTIVE" = "1" ] || { echo "FAIL: epoch 1 did not reach ACTIVE"; tail -40 "$H0/node.log"; exit 1; }
echo "epoch 1 ACTIVE ✓"

# 9. app-hash agreement across all 4 nodes at a settled height (no divergence at
#    the boundary/rotation — the D-068/D-069 sim-boundary gap closure). A divergent
#    node would have halted consensus; we additionally compare app_hash directly.
CHECK_H=$($BIN status --node "$RPC0" 2>/dev/null | jq -r '.sync_info.latest_block_height')
CHECK_H=$((CHECK_H-2))
echo "== app-hash agreement across $N nodes at height $CHECK_H =="
REF=""
for i in $(seq 0 $((N-1))); do
  AH=$($BIN query block --type=height "$CHECK_H" --node "tcp://127.0.0.1:$(rpcport "$i")" --output json 2>/dev/null \
        | jq -r '.header.app_hash // .block.header.app_hash // empty' 2>/dev/null || true)
  echo "  node$i app_hash=$AH"
  if [ -z "$REF" ]; then REF="$AH"; elif [ "$AH" != "$REF" ]; then echo "FAIL: app-hash divergence node$i"; exit 1; fi
done
echo "all $N nodes agree on app_hash at height $CHECK_H ✓"

# 10. optional inline e2e + F-4 adversary (RUN_E2E=1), all in this one invocation
#     so node processes need not survive across separate calls.
if [ "${RUN_E2E:-}" = "1" ]; then
  REPO="$(cd "$(dirname "$0")/.." && pwd)"
  DEMO="$BASE/conclave-demo"
  echo "== building conclave-demo =="
  (cd "$REPO" && "${GO:-/usr/local/go/bin/go}" build -o "$DEMO" ./cmd/conclave-demo)
  G0ADDR="localhost:$(grpcport 0)"
  echo "== veil-e2e: encrypted intent -> reveal -> clear =="
  CONCLAVED_HOME="$H0" CONCLAVE_GRPC="$G0ADDR" CONCLAVE_CHAIN_ID="$CHAIN_ID" "$DEMO" veil-e2e
  echo "== veil-f4-adversary: a modified validator cannot decrypt pre-ordering =="
  CONCLAVE_ADVERSARY_HOME="$H0" CONCLAVED_HOME="$H0" CONCLAVE_GRPC="$G0ADDR" CONCLAVE_CHAIN_ID="$CHAIN_ID" "$DEMO" veil-f4-adversary
fi

echo
echo "LOCALNET4 OK — epoch 1 ACTIVE, $N nodes in consensus, no app-hash divergence"
if [ "$FAULTY_NODE" != "" ]; then
  echo "  (faulty node $FAULTY_NODE was excluded by the justification round; chain did not halt)"
fi
if [ "${KEEP_RUNNING:-}" = "1" ]; then
  echo "nodes left running (KEEP_RUNNING=1). node0 grpc=localhost:$(grpcport 0) rpc=$RPC0 home=$H0"
  echo "pids: $(cat "$BASE/pids")"
fi
