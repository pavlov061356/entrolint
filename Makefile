.PHONY: help build test lint vuln mod-check fmt ci run pre-commit install-hooks tools clean

BINARY  := entrolint
MODULE  := github.com/pavlov061356/entrolint
# Strip leading "v" so local builds print the same version string as
# goreleaser-built binaries (goreleaser drops the prefix for {{ .Version }}).
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo "dev")
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT) \
	-X $(MODULE)/internal/version.Date=$(DATE)

help:  ## Show available targets
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build:  ## Build the binary (CGO off — match the release pipeline)
	CGO_ENABLED=0 go build -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/entrolint

test:  ## Run tests with race detector and coverage
	go test -race -coverprofile=coverage.out ./...

lint:  ## Run golangci-lint
	golangci-lint run ./...

vuln:  ## Scan dependencies for known CVEs (Go vuln DB)
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

mod-check:  ## Fail if go.mod / go.sum drift from `go mod tidy`
	go mod tidy
	git diff --exit-code -- go.mod go.sum \
		|| (echo "go.mod or go.sum is dirty after 'go mod tidy' — commit the fix" >&2; exit 1)

fmt:  ## Format code
	gofumpt -w .
	goimports -w .

ci: mod-check lint test vuln  ## Run the same checks as CI

run:  ## Run the binary; example: make run ARGS="scan ."
	go run ./cmd/entrolint $(ARGS)

pre-commit: fmt lint test  ## Run before committing

install-hooks:  ## Install git pre-commit hook calling `make pre-commit`
	@printf '#!/bin/sh\nmake pre-commit\n' > .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo "pre-commit hook installed at .git/hooks/pre-commit"

tools:  ## Install dev tools (golangci-lint pinned, others latest)
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
	go install mvdan.cc/gofumpt@latest
	go install golang.org/x/tools/cmd/goimports@latest

clean:  ## Remove build artifacts
	rm -f $(BINARY) coverage.out coverage.html
	rm -rf dist/
