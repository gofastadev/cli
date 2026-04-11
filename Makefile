.PHONY: fmt vet lint lint-install test coverage build integration clean ci preflight

## Pinned golangci-lint version. MUST match .github/workflows/ci.yml so a
## green local run predicts a green CI run.
GOLANGCI_LINT_VERSION := v2.11.4
GOLANGCI_LINT := $(shell go env GOPATH)/bin/golangci-lint

## Format all Go files (gofmt + goimports semantics via gofmt -s)
fmt:
	gofmt -s -w .

## Verify gofmt has nothing to change (fails if formatting is off).
## Uses `gofmt -l` which prints any file that needs formatting — a non-empty
## output means the tree is dirty. Portable across /bin/sh and bash.
fmt-check:
	@out=$$(gofmt -s -l .); \
	if [ -n "$$out" ]; then \
		echo "gofmt issues in:"; echo "$$out"; \
		echo "run 'make fmt' to fix"; \
		exit 1; \
	fi

## Run go vet across every package
vet:
	go vet ./...

## Install golangci-lint locally at the version pinned above. Idempotent —
## re-installs only if the version differs from what's already on $PATH.
lint-install:
	@if ! $(GOLANGCI_LINT) --version 2>/dev/null | grep -q "$(GOLANGCI_LINT_VERSION:v%=%)"; then \
		echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION)..."; \
		go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION); \
	fi

## Run golangci-lint with the same version CI uses
lint: lint-install
	$(GOLANGCI_LINT) run

## Run tests with the race detector
test:
	go test -race ./...

## Run tests with coverage report
coverage:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html

## Build the CLI binary
build:
	go build -o bin/gofasta ./cmd/gofasta/

## Run integration test (build + scaffold + compile)
integration: build
	rm -rf /tmp/gofasta-integration-test
	./bin/gofasta new /tmp/gofasta-integration-test
	cd /tmp/gofasta-integration-test && go build ./...

## Remove build artifacts
clean:
	rm -rf bin/ coverage.out coverage.html

## Run all checks (what CI runs)
ci: lint test build

## Preflight — the full set of checks that MUST pass locally before any
## task is considered complete. Intended to be run before every commit and
## before reporting a task done to the user. Runs the exact same linter
## version CI uses, so a green preflight predicts a green CI run.
##
## Order matters: fmt-check is first (cheapest, catches the most common
## slip), then vet, then lint (includes errcheck + staticcheck + revive +
## the rest), then tests with -race, then a build, then the integration
## smoke test that scaffolds a project and compiles it.
preflight: fmt-check vet lint test build integration
	@echo ""
	@echo "  ✓ preflight green — safe to commit."
