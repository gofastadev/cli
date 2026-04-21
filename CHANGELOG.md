# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **`gofasta verify`** — runs the full preflight gauntlet (gofmt, vet, golangci-lint, tests with the race detector, build, Wire drift, routes sanity) in one command. Fail-fast by default with `--keep-going` to report every result. Per-check structured JSON output via the global `--json` flag.
- **`gofasta status`** — offline project-drift report. Detects Wire-derived code out of sync with inputs, stale Swagger docs, pending migrations, uncommitted generated files, and `go.sum` freshness. Non-zero exit on any drift.
- **`gofasta inspect <Resource>`** — AST-parses a resource's model, DTOs, service interface, controller, and routes; emits a structured report so agents planning a modification see the full picture from one command instead of opening six files.
- **`gofasta config schema`** — emits a Draft-7 JSON Schema describing `config.yaml`. Shells out to the project-local `cmd/schema/` helper so the schema always matches the `gofasta` version pinned in the project's `go.mod`. Feed to VS Code YAML, JetBrains editors, or CI validators.
- **`gofasta do <workflow>`** — named development workflows chaining multiple gofasta commands: `new-rest-endpoint`, `rebuild`, `fresh-start`, `clean-slate`, `health-check`. Includes `--dry-run` for previewing chains without execution.
- **`gofasta ai <agent>`** — opt-in installer for AI coding agent configuration. Supports Claude Code, Cursor, OpenAI Codex, Aider, and Windsurf. Idempotent; `--dry-run` / `--force` supported; install history tracked in `.gofasta/ai.json`. Sub-commands: `gofasta ai list`, `gofasta ai status`.
- **Structured errors** — every CLI error now carries `{code, message, hint, docs}`. 38 stable error codes. Agents pattern-match on the code instead of regex-parsing English.
- **Global `--json` flag** — every structured-output command honors it, producing a single-line JSON document for agent consumption.
- **Post-generation auto-verify** — `gofasta g scaffold` automatically runs `go build ./...` after generation so template regressions surface immediately. Disable with `--no-verify`.
- **Generator `--dry-run`** — `gofasta g scaffold --dry-run` shows every file it would create and every patch it would apply without touching disk.
- **Per-resource controller test scaffolding** — `gofasta g scaffold` now emits a starter `<name>.controller_test.go` with smoke tests + a TODO placeholder, so generated resources are green on `go test` out of the box.
- **`AGENTS.md` in every scaffolded project** — comprehensive agent briefing (project overview, tech stack, every command, conventions, Wire gotcha walkthrough, "do not do" list, pre-commit self-check). Read automatically by Claude Code, OpenAI Codex, Cursor, Aider, and other MCP-aware agents.
- **Scaffold ships `cmd/schema/main.go`** — the 10-line helper binary that `gofasta config schema` shells out to. Also callable directly as `go run ./cmd/schema` for CI or IDE extensions.

### Fixed
- `gofasta --version` now reports the real module version for users who install via `go install`. Previously it always printed `dev` because `go install` does not apply build-time `-ldflags`. The CLI now falls back to `runtime/debug.ReadBuildInfo()` at startup to read the module version Go stamped into the binary. Pre-built binaries shipped via GitHub Releases are unaffected — they still use the `-X main.Version=<tag>` ldflag set by the release workflow.

### Improved
- `dist/install.sh` now detects whether the install directory is on the user's `$PATH` and prints exact, shell-specific `export PATH=…` instructions (zsh / bash / fish) when it isn't — preventing first-run `command not found` errors.
- Installation documentation in README and the website now includes a dedicated troubleshooting section covering the `$GOPATH/bin` not-on-`$PATH` issue, with copy-paste fixes for every major shell.

## [0.1.0] - 2026-04-09

### Added
- Initial public release of the Gofasta CLI
- `gofasta new` — scaffold a complete, ready-to-run Go web project
- `gofasta g scaffold` — generate a full resource (model, repo, service, controller, routes, DTOs, Wire provider)
- `gofasta g model` — generate a model and migration only
- `gofasta g repository` — generate model + repository layer
- `gofasta g service` — generate through the service layer
- `gofasta g controller` — generate through the controller layer
- `gofasta g dto` — generate DTOs only
- `gofasta g migration` — generate SQL migration files only
- `gofasta g route` — generate route file only
- `gofasta g provider` — generate Wire provider only
- `gofasta g resolver` — patch GraphQL resolver
- `gofasta g job` — generate a scheduled cron job
- `gofasta g task` — generate an async background task
- `gofasta g email-template` — generate an HTML email template
- `gofasta dev` — run the development server with hot reload (via Air)
- `gofasta migrate up/down` — run or roll back database migrations
- `gofasta seed` — run database seeders
- `gofasta init` — initialize a cloned project (env, deps, codegen, migrations)
- `gofasta swagger` — generate OpenAPI/Swagger documentation
- `gofasta wire` — regenerate Wire dependency injection code
- `gofasta upgrade` — self-update to the latest version (detects `go install` vs. pre-built binary)
- `gofasta version` — print detailed version, Go, and OS/arch information
- `gofasta --version` — print the installed CLI version
- `gofasta doctor` — check system prerequisites and project health
- `gofasta routes` — display all registered API routes
- `gofasta db reset` — drop all tables, re-migrate, and seed
- `gofasta console` — start an interactive Go REPL (via yaegi)
- Shell-script installer for distribution (`curl | sh`)
