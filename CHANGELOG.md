# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
