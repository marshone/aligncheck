# Makefile for aligncheck
# Copyright 2026 marshone
# Licensed under the Apache License, Version 2.0

help: ## Display this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_-]+:.*?## / {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.DEFAULT_GOAL := help
.PHONY: help build install test test-verbose demo fmt clean

GO            := go
GOFMT         := gofmt
BIN_DIR       := bin
CMD           := ./cmd/aligncheck

build: ## build: Build the aligncheck CLI into bin/
	$(GO) build -o $(BIN_DIR)/aligncheck $(CMD)

install: ## install: Install the aligncheck CLI into $(go env GOPATH)/bin
	$(GO) install $(CMD)

test: ## test: Run all unit tests (library, CLI, example self-test)
	$(GO) test -count=1 ./...

test-verbose: ## test-verbose: Run all unit tests with verbose logs
	$(GO) test -v -count=1 ./...

demo: ## demo: Show aligncheck failing on the example BrokenStruct (fails on purpose)
	ALIGNCHECK_DEMO=1 $(GO) test -v -count=1 -run="StructAlignments" ./example

fmt: ## fmt: Run gofmt on all Go files
	$(GOFMT) -s -w .

clean: ## clean: Remove bin/ and clean the Go test cache
	rm -rf $(BIN_DIR)
	$(GO) clean -testcache
