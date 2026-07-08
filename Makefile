# depdog developer tasks. `make install` rebuilds the binary into $GOPATH/bin
# (which is on your PATH) with the current git version stamped in, so
# `depdog --version` reports something real instead of the 0.0.0-dev default.

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/matterpale/depdog/internal/cli.Version=$(VERSION)

.DEFAULT_GOAL := build
.PHONY: build install test check lint

## build: compile everything
build:
	go build ./...

## install: rebuild and replace the depdog binary on your PATH ($GOPATH/bin)
install:
	go install -ldflags '$(LDFLAGS)' ./cmd/depdog
	@echo "installed depdog $(VERSION) -> $$(command -v depdog)"

## test: run unit, fixture and golden e2e tests
test:
	go test ./...

## check: run depdog's own architecture self-check
check:
	go run ./cmd/depdog check

## lint: run golangci-lint (as CI does)
lint:
	golangci-lint run
