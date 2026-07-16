
# Makefile for aligncheck
# Copyright 2026 marshone
# Licensed under the Apache License, Version 2.0



help: ## Display this help
		@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_-]+:.*?## / {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.DEFAULT_GOAL := help
GO            := go
GOFMT         := gofmt

test:  ## test: Run library unit tests
	$(GO) test -count=1 ./...

test-verbose: ## test-verbose: Run library unit tests with verbose logs
	$(GO) test -v -count=1 ./...

alignment: ## alignment validation
	${GO} test -count=1 -run="StructAlignments" ./...

alignment-verbose: ## alignment-verbose validation
	${GO} test -v -count=1 -run="StructAlignments" ./...

fmt: ## fmt: Run gofmt on all Go files
	$(GOFMT) -s -w .

clean: ## clean: Clean up the Go build and test cache
	$(GO) clean -testcache
