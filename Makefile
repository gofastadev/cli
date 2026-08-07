.PHONY: fmt fmt-check vet lint lint-install test coverage build integration deploy-e2e clean ci preflight docs-sync docs-check

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
# -timeout 20m: internal/commands is large and its -race run sits near Go's
# 10-minute default, which fails as a timeout rather than a test failure.
	go test -race -shuffle=on -timeout 20m ./...

## Run tests with coverage report
coverage:
	go test -race -timeout 20m -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html

## Build the CLI binary
build:
	go build -o bin/gofasta ./cmd/gofasta/

## Run integration test (build CLI → scaffold project → run scaffold's preflight)
##
## `go build ./...` alone is too weak a check: a template change can break
## the scaffold's gofmt-cleanliness, lint compliance, or race-test suite
## without breaking compilation. Running the scaffold's own `make preflight`
## here catches those regressions locally before they hit CI on a
## downstream user's project.
##
## The scaffold runs `go get github.com/gofastadev/gofasta@latest`, which
## pulls whatever is currently published on the framework module proxy.
## A scaffold that fails here usually means the framework's most recent
## release isn't yet indexed by sum.golang.org (the proxy and sum DB are
## eventually-consistent); wait a few minutes and retry, or temporarily
## pin GOPROXY=direct,off if the sum DB is the hold-up.
integration: build
	rm -rf /tmp/gofasta-integration-test
	./bin/gofasta new /tmp/gofasta-integration-test
	@# Exercise the generators against the fresh scaffold so a regression
	@# in `gofasta g <X>` can't slide past local preflight. Each command
	@# is a representative of its category — scaffold (full CRUD stack),
	@# job (cron), task (async queue handler). If one of these breaks
	@# compilation or lint, scaffold's preflight below catches it.
	cd /tmp/gofasta-integration-test && \
		$(CURDIR)/bin/gofasta g scaffold Product name:string price:float description:text active:bool owner_id:uuid released_at:time && \
		$(CURDIR)/bin/gofasta g job cleanup-tokens "0 0 0 * * *" && \
		$(CURDIR)/bin/gofasta g task send-welcome
	cd /tmp/gofasta-integration-test && make preflight
	@# Also exercise `gofasta test --coverage` inside the scaffold —
	@# `make preflight` runs `go test -race ./...` directly, which
	@# bypasses the wrapper. Scaffolded projects have `tool` directives
	@# in go.mod (wire/gqlgen/swag/air), and Go's per-package coverage
	@# merge looks up the `covdata` tool in that list before falling
	@# back to the stdlib helper. Without -coverpkg=./... (which
	@# `gofasta test --coverage` adds), every package without test files
	@# emits a `go: no such tool "covdata"` warning + non-zero exit.
	@# Running this here catches a regression in the --coverage flag
	@# shape locally before it ships.
	cd /tmp/gofasta-integration-test && $(CURDIR)/bin/gofasta test --coverage
	@# The --graphql variant is a SEPARATE scaffold because its breakages are
	@# invisible to the run above: `gofasta new --graphql` invokes gqlgen,
	@# which rewrites app/graphql/resolvers/*.resolvers.go from the schema and
	@# relocates any non-resolver declaration into a commented-out block. That
	@# silently commented out the shared error helpers and shipped a project
	@# that would not compile — for as long as this target only built the
	@# non-GraphQL variant, nothing caught it.
	rm -rf /tmp/gofasta-integration-test-gql
	./bin/gofasta new /tmp/gofasta-integration-test-gql --graphql
	@# Scaffold a resource inside the GraphQL project WITHOUT --graphql:
	@# gqlgen.yml auto-detection must kick in and produce the schema
	@# fragment + fully-implemented resolver file. The project's own
	@# preflight then compiles the resolvers and runs the generated
	@# tests — a panic("not implemented") stub or a broken binding
	@# fails right here.
	cd /tmp/gofasta-integration-test-gql && \
		$(CURDIR)/bin/gofasta g scaffold Order title:string qty:int
	@# The resolver file must exist and must not contain gqlgen's stock
	@# panic stubs.
	@if ! test -f /tmp/gofasta-integration-test-gql/app/graphql/resolvers/order.resolvers.go; then \
		echo "integration: order.resolvers.go was not generated"; exit 1; fi
	@if grep -q "not implemented" /tmp/gofasta-integration-test-gql/app/graphql/resolvers/order.resolvers.go; then \
		echo "integration: order.resolvers.go contains unimplemented stubs"; exit 1; fi
	cd /tmp/gofasta-integration-test-gql && make preflight

## End-to-end deploy test: scaffolds a project and runs real
## `gofasta deploy setup/deploy/status/rollback` (both methods, repeat
## deploys, induced-failure auto-rollback) against a disposable Docker
## "VPS" container (systemd + sshd + Docker + PostgreSQL). Requires
## Docker and network access. Runs in CI as the deploy-e2e job.
deploy-e2e: build
	test/e2e-deploy/run.sh

## Remove build artifacts
clean:
	rm -rf bin/ coverage.out coverage.html

## Regenerate the README's marker-delimited blocks from `gofasta facts`
docs-sync: build
	./bin/gofasta --no-banner facts sync --repo .

## Verify docs match the facts document: README generated blocks, inline
## fact annotations, every `gofasta …` invocation in code fences (README,
## skeleton README template, ../.claude/docs when present), the release
## platform matrix vs .goreleaser.yaml, and the golangci-lint version
## parity between this Makefile and ci.yml.
docs-check: build
	./bin/gofasta --no-banner facts check --repo .

## Run all checks (what CI runs)
# The PR-level checks (what ci.yml's lint + test jobs run). `make
# preflight` is the full local gate — it adds the integration scaffolds.
ci: fmt-check vet lint test build docs-check

## Preflight — the full set of checks that MUST pass locally before any
## task is considered complete. Intended to be run before every commit and
## before reporting a task done to the user. Runs the exact same linter
## version CI uses, so a green preflight predicts a green CI run.
##
## Order matters: fmt-check is first (cheapest, catches the most common
## slip), then vet, then lint (includes errcheck + staticcheck + revive +
## the rest), then tests with -race, then a build, then docs-check (cheap,
## catches documentation drift before the expensive steps), then the
## integration smoke test that scaffolds a project and compiles it, then
## the deploy end-to-end test against a disposable Docker VPS (mirrors the
## CI deploy-e2e job — preflight runs everything CI runs).
preflight: fmt-check vet lint test build docs-check integration deploy-e2e
	@echo ""
	@echo "  ✓ preflight green — safe to commit."
