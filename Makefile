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

lint:
	golangci-lint run

lint-fix:
	golangci-lint run --fix

localnet:
	./scripts/localnet.sh

# CONCLAVE-TODO(phase-0): proto-gen target lands with task 0.2 (buf generate
# via scripts/protocgen.sh; acceptance: `make proto-gen` idempotent).

.PHONY: all install build test lint lint-fix localnet
