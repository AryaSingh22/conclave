#!/usr/bin/env bash
# scripts/protocgen.sh — generate Go code from the Conclave protos (SPEC §4).
# Idempotent: plugin versions are pinned; running twice produces no diff
# (SPEC §10 row 0.2 acceptance).
set -euo pipefail
cd "$(dirname "$0")/.."

# pinned codegen plugins (versions matched to go.mod)
GOCOSMOS_VERSION=v1.7.0
GATEWAY_VERSION=v1.16.0

command -v protoc-gen-gocosmos >/dev/null 2>&1 ||
  go install "github.com/cosmos/gogoproto/protoc-gen-gocosmos@${GOCOSMOS_VERSION}"
command -v protoc-gen-grpc-gateway >/dev/null 2>&1 ||
  go install "github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway@${GATEWAY_VERSION}"

echo "Generating gogo proto code..."
(
  cd proto
  buf generate --template buf.gen.gogo.yaml
)

# move generated code into the repo tree (go_package is
# github.com/AryaSingh22/conclave/{x,app}/... — x/ for modules, app/ for
# app-level types like the unordered tx extension, SPEC §6.7)
for dir in x app; do
  if [ -d "github.com/AryaSingh22/conclave/$dir" ]; then
    mkdir -p "$dir"
    cp -r "github.com/AryaSingh22/conclave/$dir/"* "$dir/"
  fi
done
rm -rf github.com

echo "proto-gen done"
