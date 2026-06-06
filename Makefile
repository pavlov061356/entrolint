.PHONY: help build test lint vuln fmt ci run pre-commit install-hooks tools clean

BINARY  := entrolint
MODULE  := github.com/pavlov061356/entrolint
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT) \
	-X $(MODULE)/internal/version.Date=$(DATE)

help:  ## Show available targets
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build:  ## Build the binary
	go build -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/entrolint

test:  ## Run tests with race detector and coverage
	go test -race -coverprofile=coverage.out ./...

lint:  ## Run golangci-lint
	golangci-lint run ./...

vuln:  ## Scan dependencies for known CVEs (Go vuln DB)
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

fmt:  ## Format code
	gofumpt -w .
	goimports -w .

ci: lint test vuln  ## Run the same checks as CI

run:  ## Run the binary; example: make run ARGS="scan ."
	go run ./cmd/entrolint $(ARGS)

pre-commit: fmt lint test  ## Run before committing

install-hooks:  ## Install git pre-commit hook calling `make pre-commit`
	@printf '#!/bin/sh\nmake pre-commit\n' > .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo "pre-commit hook installed at .git/hooks/pre-commit"

tools:  ## Install dev tools (golangci-lint pinned, others latest)
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8
	go install mvdan.cc/gofumpt@latest
	go install golang.org/x/tools/cmd/goimports@latest

clean:  ## Remove build artifacts
	rm -f $(BINARY) coverage.out coverage.html
	rm -rf dist/
