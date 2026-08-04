#!/usr/bin/make -f

COMMIT  := $(shell git log -1 --format='%H' 2>/dev/null || echo unknown)
VERSION := $(shell git describe --tags --always 2>/dev/null || echo v0.0.0-dev)

ldflags = -X github.com/cosmos/cosmos-sdk/version.Name=conclave \
	-X github.com/cosmos/cosmos-sdk/version.AppName=conclaved \
	-X github.com/cosmos/cosmos-sdk/version.Version=$(VERSION) \
	-X github.com/cosmos/cosmos-sdk/version.Commit=$(COMMIT)

BUILD_FLAGS := -ldflags '$(ldflags)'

# GOLANGCI_LINT_VERSION is PINNED ALONGSIDE THE TOOLCHAIN ON PURPOSE (task 3.3a,
# D-099). golangci-lint hard-fails on any `go` directive newer than the Go the
# linter binary was built with, so the linter version and go.mod's directive are
# a COUPLED PAIR, not two independent choices. Installing from source with this
# repo's own toolchain makes the constraint hold BY CONSTRUCTION rather than by
# a version table someone has to keep in step. The absolute path matters too: a
# stale distro-installed golangci-lint in /usr/local/bin shadows $(GOPATH)/bin
# on PATH, and `make lint` would silently run the wrong linter.
GOLANGCI_LINT_VERSION := v2.12.2
GOLANGCI_LINT := $(shell go env GOPATH)/bin/golangci-lint

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

# lint-tools installs the PINNED linter if the pinned version is not the one
# present. It is a prerequisite of lint so a fresh checkout, or a machine whose
# PATH linter is stale, cannot lint against the wrong binary.
lint-tools:
	@"$(GOLANGCI_LINT)" --version 2>/dev/null | grep -q "version $(GOLANGCI_LINT_VERSION:v%=%) " || \
		(echo "installing golangci-lint $(GOLANGCI_LINT_VERSION) with $$(go env GOVERSION)" && \
		 go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION))

lint: lint-tools
	"$(GOLANGCI_LINT)" run

# guards is the machine-enforced half of SPEC §11.6 plus the dependency-pin
# guards (AUDIT F-5 float ban, F-6 goleveldb pin, the dependency-EOL manifest).
# It is SEPARATE from `lint` because the linter has measured positional blind
# spots — see the header of .golangci.yml and D-099. Part of CI on every push.
guards:
	bash scripts/determinism_guards.sh

# inventory-rederive is PHASE3_PLAN §3.1.2's standing obligation as an artifact
# (finding F-13): the authored-surface greps, the test-citation sweep, marker
# ownership, and the commit-citation check. Run at every boundary and before any
# external review is contracted.
inventory-rederive:
	bash scripts/inventory_rederive.sh

lint-fix: lint-tools
	"$(GOLANGCI_LINT)" run --fix

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

.PHONY: all install build test sim-determinism lint lint-tools lint-fix guards inventory-rederive localnet demo-contract proto-gen proto-lint
