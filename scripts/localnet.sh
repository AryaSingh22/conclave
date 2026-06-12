#!/usr/bin/env bash
# scripts/localnet.sh — init + start a single-node Conclave devnet (SPEC §12).
#
# Genesis per SPEC §12:
#   - 1 validator, self-delegated 1M CLV
#   - test accounts operator, alice, bob, carol, solver1: 10k CLV + 10k mock USDC each
#   - faucet account holding a top-up pool
#   - fast blocks (timeout_commit = 1s), minimum-gas-prices = "0uclv", API + gRPC enabled
set -euo pipefail

CHAIN_ID="conclave-dev-1"
DENOM="uclv"
HOME_DIR="${CONCLAVED_HOME:-$HOME/.conclaved}"
MONIKER="conclave-dev"
BIN="${CONCLAVED_BIN:-conclaved}"
KEYRING=(--keyring-backend test)

# amounts (10^6 uclv = 1 CLV per SPEC §0)
VALIDATOR_SUPPLY="1100000000000${DENOM}"            # 1.1M CLV (1M self-delegation + fee float)
SELF_DELEGATION="1000000000000${DENOM}"             # 1M CLV
ACCOUNT_FUNDS="10000000000${DENOM},10000000000uusdc" # 10k CLV + 10k mock USDC each
FAUCET_FUNDS="1000000000000${DENOM},1000000000000uusdc" # 1M CLV + 1M USDC top-up pool

if ! command -v "$BIN" >/dev/null 2>&1; then
  echo "error: $BIN not found in PATH — run 'make install' first" >&2
  exit 1
fi

if [ -d "$HOME_DIR" ]; then
  echo "Removing existing localnet home at $HOME_DIR"
  rm -rf "$HOME_DIR"
fi

$BIN init "$MONIKER" --chain-id "$CHAIN_ID" --default-denom "$DENOM" --home "$HOME_DIR"

for name in validator operator alice bob carol solver1 faucet; do
  $BIN keys add "$name" "${KEYRING[@]}" --home "$HOME_DIR"
done

$BIN genesis add-genesis-account validator "$VALIDATOR_SUPPLY" "${KEYRING[@]}" --home "$HOME_DIR"
for name in operator alice bob carol solver1; do
  $BIN genesis add-genesis-account "$name" "$ACCOUNT_FUNDS" "${KEYRING[@]}" --home "$HOME_DIR"
done
$BIN genesis add-genesis-account faucet "$FAUCET_FUNDS" "${KEYRING[@]}" --home "$HOME_DIR"

# CONCLAVE-TODO(phase-0): genesis solver registration for solver1 (SPEC §12:
# "Genesis includes one pre-registered solver so the demo can also exercise
# the registered-solver path before testing fallback") — lands with x/session
# at task 0.8.

$BIN genesis gentx validator "$SELF_DELEGATION" --chain-id "$CHAIN_ID" "${KEYRING[@]}" --home "$HOME_DIR"
$BIN genesis collect-gentxs --home "$HOME_DIR"

GENESIS="$HOME_DIR/config/genesis.json"
TMP="$GENESIS.tmp"

# bank denom metadata: 10^6 uclv = 1 CLV (SPEC §0); mock uusdc (SPEC §3)
jq '.app_state.bank.denom_metadata = [
  {
    "description": "Native staking/gas token of Conclave.",
    "denom_units": [
      { "denom": "uclv", "exponent": 0 },
      { "denom": "clv", "exponent": 6 }
    ],
    "base": "uclv", "display": "clv", "name": "CLV", "symbol": "CLV"
  },
  {
    "description": "Mock USDC settlement asset for the local devnet (SPEC §3).",
    "denom_units": [
      { "denom": "uusdc", "exponent": 0 },
      { "denom": "usdc", "exponent": 6 }
    ],
    "base": "uusdc", "display": "usdc", "name": "Mock USDC", "symbol": "USDC"
  }
]' "$GENESIS" > "$TMP" && mv "$TMP" "$GENESIS"

# fast dev governance
jq '.app_state.gov.params.voting_period = "30s"
  | .app_state.gov.params.expedited_voting_period = "15s"' "$GENESIS" > "$TMP" && mv "$TMP" "$GENESIS"

# fund the x/mandate fee-conversion pool (SPEC §6.2: "the module holds a CLV
# float funded at genesis"). The balance is written directly into bank
# genesis — never via add-genesis-account, which would create a BaseAccount
# at the module address and break GetModuleAccount; x/mandate InitGenesis
# materializes the proper module account before any fee abstraction runs.
FEE_POOL_AMOUNT="1000000000" # 1000 CLV, matching x/mandate DefaultParams fee_pool
MANDATE_ADDR_HEX=$(echo -n "mandate" | sha256sum | cut -c1-40)
MANDATE_ADDR=$($BIN debug addr "$MANDATE_ADDR_HEX" | awk '/Bech32 Acc:/ {print $3}')
jq --arg addr "$MANDATE_ADDR" --arg amt "$FEE_POOL_AMOUNT" --arg denom "$DENOM" '
  .app_state.bank.balances += [{ "address": $addr, "coins": [{ "denom": $denom, "amount": $amt }] }]
  | .app_state.bank.supply = (.app_state.bank.supply | map(
      if .denom == $denom then .amount = ((.amount | tonumber) + ($amt | tonumber) | tostring) else . end
    ))' "$GENESIS" > "$TMP" && mv "$TMP" "$GENESIS"

$BIN genesis validate --home "$HOME_DIR"

APP_TOML="$HOME_DIR/config/app.toml"
CONFIG_TOML="$HOME_DIR/config/config.toml"

# SPEC §12: zero min gas price, API + gRPC enabled, 1s blocks
sed -i 's/^minimum-gas-prices = .*/minimum-gas-prices = "0uclv"/' "$APP_TOML"
sed -i '/^\[api\]/,/^\[/ s/^enable = .*/enable = true/' "$APP_TOML"
sed -i '/^\[api\]/,/^\[/ s/^swagger = .*/swagger = true/' "$APP_TOML"
sed -i '/^\[grpc\]/,/^\[/ s/^enable = .*/enable = true/' "$APP_TOML"
sed -i 's/^timeout_commit = .*/timeout_commit = "1s"/' "$CONFIG_TOML"

echo
echo "Localnet initialized: chain-id=$CHAIN_ID home=$HOME_DIR"
echo "Starting conclaved..."
exec $BIN start --home "$HOME_DIR"
