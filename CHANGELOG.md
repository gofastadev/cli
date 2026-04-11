# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
- `gofasta upgrade` — self-update to the latest version (detects Homebrew, go install, or binary)
- `gofasta version` — print detailed version, Go, and OS/arch information
- `gofasta --version` — print the installed CLI version
- `gofasta doctor` — check system prerequisites and project health
- `gofasta routes` — display all registered API routes
- `gofasta db reset` — drop all tables, re-migrate, and seed
- `gofasta console` — start an interactive Go REPL (via yaegi)
- Homebrew formula and shell installer for distribution
