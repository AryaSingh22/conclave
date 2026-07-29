#!/usr/bin/make -f

COMMIT  := $(shell git log -1 --format='%H' 2>/dev/null || echo unknown)
VERSION := $(shell git describe --tags --always 2>/dev/null || echo v0.0.0-dev)

ldflags = -X github.com/cosmos/cosmos-sdk/version.Name=conclave \
	-X github.com/cosmos/cosmos-sdk/version.AppName=conclaved \
	-X github.com/cosmos/cosmos-sdk/version.Version=$(VERSION) \
	-X github.com/cosmos/cosmos-sdk/version.Commit=$(COMMIT)

BUILD_FLAGS := -ldflags '$(ldflags)'

all: lint test install

# install builds conclaved into $GOPATH/bin (SPEC §12)
install:
	go install $(BUILD_FLAGS) ./cmd/conclaved

build:
	go build $(BUILD_FLAGS) -o build/conclaved ./cmd/conclaved

test:
	go test -count=1 ./...

# sim-determinism is the F-1 / D-063 determinism gate: it runs the determinism
# sim over the committed seed vector in two independent processes and fails on
# any SKIP, abort, or app-hash mismatch (within or across runs). It is NOT part
# of `make test` because the simulation is -Enabled-gated (D-057).
sim-determinism:
	bash scripts/sim_determinism.sh

lint:
	golangci-lint run

lint-fix:
	golangci-lint run --fix

localnet:
	./scripts/localnet.sh

# demo-contract rebuilds the §6 exit demo's CVM-schema CosmWasm contract and
# refreshes the COMMITTED artifact. Needs a Rust toolchain with the wasm32
# target (`rustup target add wasm32-unknown-unknown`). Deliberately NOT a
# prerequisite of build/test/install: the artifact is committed so the demo runs
# without Rust, and nothing this builds enters conclaved (dev tooling).
demo-contract:
	cd demo/cvm-contract && cargo build --release --target wasm32-unknown-unknown
	cp demo/cvm-contract/target/wasm32-unknown-unknown/release/conclave_demo_escrow.wasm \
		demo/cvm-contract/artifacts/conclave_demo_escrow.wasm
	@echo "artifact: demo/cvm-contract/artifacts/conclave_demo_escrow.wasm"

# proto-gen regenerates Go code from proto/ (idempotent — SPEC §10 row 0.2)
proto-gen:
	./scripts/protocgen.sh

proto-lint:
	cd proto && buf lint

.PHONY: all install build test sim-determinism lint lint-fix localnet demo-contract proto-gen proto-lint
