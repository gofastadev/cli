# Gofasta CLI

[![CI](https://github.com/gofastadev/cli/actions/workflows/ci.yml/badge.svg)](https://github.com/gofastadev/cli/actions/workflows/ci.yml) [![CodeQL](https://github.com/gofastadev/cli/actions/workflows/codeql.yml/badge.svg)](https://github.com/gofastadev/cli/actions/workflows/codeql.yml) [![codecov](https://codecov.io/gh/gofastadev/cli/graph/badge.svg)](https://codecov.io/gh/gofastadev/cli) [![Go Reference](https://pkg.go.dev/badge/github.com/gofastadev/cli.svg)](https://pkg.go.dev/github.com/gofastadev/cli) [![Go Report Card](https://goreportcard.com/badge/github.com/gofastadev/cli)](https://goreportcard.com/report/github.com/gofastadev/cli) [![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT) [![Go Version](https://img.shields.io/github/go-mod/go-version/gofastadev/cli)](https://github.com/gofastadev/cli/blob/main/go.mod) [![Release](https://img.shields.io/github/v/release/gofastadev/cli)](https://github.com/gofastadev/cli/releases)

The `gofasta` binary — the command-line tool for [Gofasta](https://gofasta.dev), a Go backend toolkit. It creates new projects, generates code, runs the development loop (`gofasta dev`), and ships first-class agent integration. It is a standalone Go binary that does **not** import the gofasta library at runtime — it only manipulates files on disk.

## The Gofasta project

Gofasta is split across three independent repositories. Each has its own release cycle and `go.mod` / `package.json`.

| Repo | Role |
|------|------|
| [`gofastadev/cli`](https://github.com/gofastadev/cli) | **You are here.** The `gofasta` binary — `gofasta new`, code generation, and the dev loop. |
| [`gofastadev/gofasta`](https://github.com/gofastadev/gofasta) | The library your project imports — every package under `pkg/*`. |
| [`gofastadev/website`](https://github.com/gofastadev/website) | The docs site at **[gofasta.dev](https://gofasta.dev)**. |

For full documentation — every command's flags, every package's API, every guide — visit **[gofasta.dev](https://gofasta.dev)**. This README covers CLI installation, scaffolding, and the most common workflows; everything else is on the website.

## Install the CLI

The CLI lives in its own Go module (`github.com/gofastadev/cli`) with `main.go` at `cmd/gofasta/`. It is not the same as the `github.com/gofastadev/gofasta` library, which your generated projects import as a dependency. You install one, you import the other.

**Option A — `go install` (recommended for Go developers, requires Go 1.25.0+, install Go: <https://go.dev/doc/install>):**

```bash
go install github.com/gofastadev/cli/cmd/gofasta@latest
```

Compiles the CLI from source using your local Go toolchain and drops the `gofasta` binary into `$GOBIN` (or `$GOPATH/bin` if `GOBIN` is unset — usually `~/go/bin`).

**Option B — Pre-built binary (no Go toolchain needed):**

Download the archive for your platform from [GitHub Releases](https://github.com/gofastadev/cli/releases) (macOS, Linux, and Windows, `amd64` and `arm64`), then unpack it onto your `PATH`. On macOS/Linux:

```bash
tar -xzf gofasta_*_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz
sudo install -m 0755 gofasta /usr/local/bin/gofasta
```

On Windows, download the `.zip` archive, extract `gofasta.exe`, and place it in a directory on your `%PATH%`.

Verify the installation:

```bash
gofasta --help
```

### Troubleshooting

#### `command not found: gofasta` after `go install`

This is the single most common first-time issue with any `go install`-distributed CLI, not specific to gofasta. Go installed the binary into `$GOPATH/bin` (typically `~/go/bin`), but that directory isn't on your shell's `$PATH`.

**Verify the binary is actually there:**

```bash
ls -l "$(go env GOPATH)/bin/gofasta"
```

If you see the binary, the install worked — you just need to make the directory reachable. Add one line to your shell config and reload:

```bash
# zsh (default on macOS)
echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> ~/.zshrc
source ~/.zshrc

# bash
echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> ~/.bashrc
source ~/.bashrc

# fish
fish_add_path (go env GOPATH)/bin
```

After that, `gofasta --help` should work in any new terminal session. This is a one-time setup — every future `go install` lands in the same directory.

#### `gofasta --version` says `dev` (pre-v0.1.3 only)

Earlier releases of the CLI shipped with a hardcoded `Version = "dev"` default that only got overridden for pre-built binaries. `go install` never applies build-time flags, so installs via Option A always reported `dev` regardless of the actual version. This was fixed in **v0.1.3**: the CLI now reads the installed module version via [`runtime/debug.ReadBuildInfo()`](https://pkg.go.dev/runtime/debug#ReadBuildInfo) at startup, so `go install` users see the correct tag.

If you installed before v0.1.3, re-run `go install github.com/gofastadev/cli/cmd/gofasta@latest` to pick up the fix.

#### Which install method should I use?

| You have Go installed | You don't have Go installed |
|---|---|
| **Option A** — `go install` compiles a native binary with your exact toolchain and there's nothing to keep in sync. Upgrade with `go install …@latest`. | **Option B** — pre-built binary from GitHub Releases. Upgrade with `gofasta upgrade`. |

Both end up as a `gofasta` binary in a standard directory. Use whichever matches your environment.

## Create a New Project

```bash
gofasta new myapp
```

Or with a full module path:

```bash
gofasta new github.com/myorg/myapp
```

Pick a database driver (default `postgres`) and a project layout
(default `layered`) at scaffold time:

```bash
gofasta new myapp --driver mysql          # postgres | mysql | sqlite | sqlserver | clickhouse
gofasta new myapp --layout feature        # layered | feature (per-resource packages)
gofasta new myapp --graphql               # add a GraphQL API alongside REST
```

This creates a complete, ready-to-run project:

```
myapp/
├── app/                    # Your application code
│   ├── main/main.go       # Entry point
│   ├── models/             # Database models (User starter included)
│   ├── dtos/               # Request/response types
│   ├── repositories/       # Data access layer
│   ├── services/           # Business logic
│   ├── rest/
│   │   ├── controllers/    # HTTP handlers
│   │   └── routes/         # Route definitions
│   ├── graphql/            # (--graphql only)
│   │   ├── schema/         # GraphQL schema files (.gql)
│   │   └── resolvers/      # GraphQL resolvers
│   ├── validators/         # Input validation rules
│   ├── di/                 # Dependency injection (Google Wire)
│   ├── devtools/           # Build-tagged /debug/* endpoints for `gofasta debug`
│   └── jobs/               # Cron jobs
├── cmd/                    # CLI commands for your app
│   ├── root.go             # Cobra root command
│   ├── serve.go            # Starts the HTTP server
│   ├── seed.go             # Runs database seeders
│   ├── migrate.go          # In-app migration runner
│   └── schema/             # Emits the config.yaml JSON Schema (`gofasta config schema`)
├── db/
│   ├── migrations/         # SQL migration files (foundational set matches your --driver)
│   └── seeds/              # Database seed functions
├── configs/                # RBAC policies, feature flags
├── deployments/            # Docker, CI/CD, nginx, systemd
├── docs/                   # Generated Swagger/OpenAPI output
├── templates/emails/       # HTML email templates
├── locales/                # Translation files
├── testutil/mocks/         # Generated testify mocks
├── config.yaml             # Application configuration
├── compose.yaml            # Docker Compose (app + your chosen database)
├── Dockerfile              # Production container image
├── Makefile                # Development shortcuts
├── air.toml                # Hot-reload config (used by `gofasta dev`)
└── gqlgen.yml              # GraphQL codegen config (--graphql only)
```

The project imports `github.com/gofastadev/gofasta` as a library dependency. It does **not** contain any CLI or gofasta library internals — only your application code.

### What Happens During `gofasta new`

1. Creates the project directory
2. Runs `go mod init` with your module path
3. Copies ~78 template files (including a starter User resource), replacing placeholders with your project name, plus the foundational migrations for your chosen `--driver`
4. Runs `go get github.com/gofastadev/gofasta@<pinned version>` to pull the gofasta library as a project dependency — the CLI pins the library release its templates were tested against (`toolVersionGofasta` in `internal/commands/new.go`) — plus tool dependencies. The tool deps are recorded as `go.mod` tools (Go 1.24+) and fetched on first `go mod tidy` — no separate install required by the user. They are: [Wire](https://github.com/google/wire) (DI codegen), [gqlgen](https://gqlgen.com/getting-started/) (GraphQL codegen, `--graphql` only), [Air](https://github.com/air-verse/air) (hot reload), and [swag](https://github.com/swaggo/swag) (Swagger generator).
5. Runs `go mod tidy`
6. Generates Wire dependency injection code
7. Generates GraphQL resolver code (`--graphql` only)
8. Generates Swagger/OpenAPI docs
9. Initializes a git repository with an initial commit

## Start Developing

`gofasta dev` is the one-command dev loop. It runs **host-first** by default: your app runs on the host with Air hot reload, and a preflight probes your database / cache / queue connections — if a dependency is unreachable, an interactive menu offers to start it in Docker or continue without it. Docker (install: <https://docs.docker.com/get-docker/>) is only needed when you let `dev` run services in containers.

```bash
cd myapp

# Default — app on host with hot reload, preflight + interactive recovery
gofasta dev

# Bring up the database in Docker explicitly, app stays on host
gofasta dev --services db

# Full stack in Docker (app + db + cache + queue)
gofasta dev --services all
```

Useful extras: `--dashboard` starts a live dev dashboard on `http://localhost:9090`, `--fresh` drops compose volumes for a clean DB, `--dry-run` prints the resolved plan, and the interactive keyboard layer supports `r` (restart) and `q` (quit).

Your app is now running:
- REST API: `http://localhost:8080/api/v1/`
- GraphQL: `http://localhost:8080/graphql`
- GraphQL Playground: `http://localhost:8080/graphql-playground`
- Health check: `http://localhost:8080/health`
- Prometheus metrics: `http://localhost:8080/metrics`
- Swagger UI: `http://localhost:8080/swagger/index.html`

## Generate Code

The `generate` command (shorthand: `g`) creates boilerplate code for new resources. Every generated file is auto-wired into the dependency injection container, routes, and GraphQL schema.

### Scaffold a Full Resource

```bash
gofasta g scaffold Product name:string price:float
```

This single command creates **18 files** (tests included) and patches 4 existing files:

| Created file | What it is |
|-------------|-----------|
| `app/models/product.model.go` | Database model with `name` and `price` fields |
| `db/migrations/000006_create_products.up.sql` | SQL to create the `products` table |
| `db/migrations/000006_create_products.down.sql` | SQL to drop the `products` table |
| `app/repositories/interfaces/product_repository.go` | Repository interface (contract) |
| `app/repositories/product.repository.go` | Repository implementation (GORM queries) |
| `app/repositories/product.repository_test.go` | Repository tests |
| `app/services/interfaces/product_service.go` | Service interface (contract) |
| `app/services/product.service.go` | Service implementation (business logic) |
| `app/services/product.service_test.go` | Service tests |
| `app/services/product_errors.go` | Per-resource sentinel errors |
| `app/services/product_inputs.go` | Domain input types |
| `app/services/product_inputs_test.go` | Domain input tests |
| `app/dtos/product.dtos.go` | Request/response DTOs with validation tags |
| `app/dtos/product.dtos_test.go` | DTO tests |
| `app/di/providers/product.go` | Wire dependency injection provider |
| `app/rest/controllers/product.controller.go` | REST controller with CRUD handlers |
| `app/rest/controllers/product.controller_test.go` | Controller tests |
| `app/rest/routes/product.routes.go` | Route definitions (GET, POST, PUT, DELETE) |

(With `--layout feature`, the same files land in per-resource packages under `app/<resource>/` instead.)

It also patches these files automatically:
- `app/di/container.go` — adds `ProductService` and `ProductController` fields
- `app/di/wire.go` — adds `ProductSet` to the Wire build
- `app/rest/routes/index.routes.go` — registers Product routes
- `cmd/serve.go` — wires `ProductController` into the route config

Wire code is regenerated for you at the end, and a post-generation `go build ./...` verifies the result (skip with `--no-verify`; preview everything with `--dry-run`).

After scaffolding, run migrations and start coding your business logic:

```bash
gofasta migrate up
# Edit app/services/product.service.go — that's where your logic goes
```

### Add GraphQL Support

```bash
gofasta g scaffold Product name:string price:float --graphql
```

The `--graphql` flag additionally creates a `.gql` schema file and a resolver file, patches the resolver struct and the gqlgen autobind list, and regenerates gqlgen code.

### Supported Field Types

| Type | Go type | SQL type (Postgres) | GraphQL type |
|------|---------|-------------------|-------------|
| `string` | `string` | `VARCHAR(255)` | `String` |
| `text` | `string` | `TEXT` | `String` |
| `int` | `int` | `INTEGER` | `Int` |
| `float` | `float64` | `DECIMAL(10,2)` | `Float` |
| `bool` | `bool` | `BOOLEAN` | `Boolean` |
| `uuid` | `uuid.UUID` | `UUID` | `ID` |
| `time` | `time.Time` | `TIMESTAMP` | `DateTime` |

SQL types are automatically adapted for MySQL, SQLite, SQL Server, and ClickHouse based on the `database.driver` in your `config.yaml`.

### Generate Individual Layers

You don't have to scaffold everything at once. Generate only what you need:

```bash
# Just the model + migration
gofasta g model Product name:string price:float

# Model + migration + repository interface, implementation, and test
gofasta g repository Product name:string price:float

# Everything below the controller: model, repo, service, errors, inputs, DTOs, tests, Wire provider
gofasta g service Product name:string price:float

# The full stack — identical to `g scaffold`
gofasta g controller Product name:string price:float

# Individual pieces
gofasta g dto Product name:string price:float     # DTOs only
gofasta g migration Product name:string            # SQL files only
gofasta g route Product                             # Route file only
gofasta g provider Product                          # Wire provider only
gofasta g resolver Product                          # Patch GraphQL resolver
```

### Modify Existing Resources

The modify-aware generators patch code you already have (AST surgery,
idempotent, `--dry-run` supported):

```bash
gofasta g field Product weight:float                # model + DTOs + inputs + allowlists + SDL + migration
gofasta g method Product Recalculate                # service interface + impl stub
gofasta g repo-method Product FindBySlug --returns "*models.Product, error"
gofasta g endpoint Product POST /products/{id}/archive
gofasta g middleware GET /products middleware.Throttle(20)
gofasta g relation Order belongs_to Product         # association + FK migration
gofasta g rename Product.Title Name                 # preview; --apply to write
gofasta g mock --all                                # refresh testify mocks
```

### Background Jobs

```bash
# Cron job (runs on a schedule)
gofasta g job cleanup-tokens "0 0 0 * * *"     # Daily at midnight
gofasta g job send-reports "0 0 9 * * 1"       # Every Monday at 9am
gofasta g job sync-data                         # Defaults to every hour

# Async task (enqueued and processed in background)
gofasta g task send-welcome-email
gofasta g task process-payment
```

### Email Templates

```bash
gofasta g email-template order-confirmation
# Creates templates/emails/order-confirmation.html
```

> **Per-command reference.** Every `gofasta` command has a dedicated page on [gofasta.dev/docs/cli-reference](https://gofasta.dev/docs/cli-reference) with all its flags, error codes, and recipes. The README below is a tour of the most common workflows; the website is the source of truth for flag-level detail.

## Other Commands

### Database Migrations

```bash
gofasta migrate up     # Apply all pending migrations
gofasta migrate down   # Rollback the last migration
```

### Database Seeding

```bash
gofasta seed           # Run all seed functions
gofasta seed --fresh   # Drop all tables, re-migrate, then seed
```

### Project Initialization

Run this once after cloning an existing gofasta project:

```bash
gofasta init
```

This creates `.env` from `.env.example`, runs `go mod tidy`, generates Wire, GraphQL, and Swagger code, runs migrations, and verifies the build.

### Swagger Documentation

```bash
gofasta swagger
```

Generates OpenAPI/Swagger docs from code annotations.

### Regenerate Wire DI

```bash
gofasta wire
```

Regenerates the Wire dependency injection code after manual changes to providers.

## Agent-friendly commands

Gofasta ships first-class integration with AI coding agents. Every command below honors the global `--json` flag for machine-parseable output, and every error carries a stable code + remediation hint + docs link.

### `gofasta verify` — one "am I done?" check

Runs the full preflight gauntlet (gofmt, vet, golangci-lint, tests with the race detector, build, Wire drift, routes) in one command. Fails fast on the first failing step; pass `--keep-going` to report every result.

```bash
gofasta verify               # human table
gofasta verify --json        # structured per-check JSON
gofasta verify --no-lint     # skip golangci-lint on machines that don't have it
```

### `gofasta status` — project drift report

Reports whether derived artifacts (Wire, Swagger), generated files, and module state are in sync with their inputs. Complementary to `verify` — `verify` is about quality gates; `status` is about drift.

```bash
gofasta status               # text table
gofasta status --json        # one JSON object per check
```

### `gofasta inspect <Resource>` — resource composition at a glance

AST-parses a resource's model, DTOs, service interface, controller, and routes; emits a single structured report. Replaces opening six files by hand.

```bash
gofasta inspect User
gofasta inspect User --json | jq '.service_methods[].name'
```

### `gofasta config schema` — JSON Schema for `config.yaml`

Emits a Draft-7 JSON Schema describing `config.yaml`. Feed it to VS Code / JetBrains YAML extensions for autocomplete and inline validation, or to CI for pre-deploy checks. Shells out to a project-local `cmd/schema` helper so the schema always matches the `gofasta` version pinned in your `go.mod`.

```bash
gofasta config schema > config.schema.json

# Then, at the top of config.yaml:
#   # yaml-language-server: $schema=./config.schema.json
```

### `gofasta do <workflow>` — named command chains

Pre-defined sequences of gofasta commands that together accomplish one higher-level task. Transparent (no hidden logic, each step is a command you could run by hand) but save agent round-trips and keystrokes:

```bash
gofasta do new-rest-endpoint Invoice total:float    # scaffold + migrate up + swagger
gofasta do rebuild                                  # wire + swagger
gofasta do fresh-start                              # init + migrate up + seed
gofasta do clean-slate                              # db reset + seed
gofasta do health-check                             # verify + status
gofasta do list                                     # every supported workflow
```

Pass `--dry-run` to preview the chain.

### `gofasta ai <agent>` — install agent-specific configuration

A brand-new `gofasta new` project ships **zero** agent-related files. Agent setup is fully opt-in, per agent. Running `gofasta ai <key>` installs a self-contained tree at the locations that agent reads natively — no rename of pre-existing files, every file lands at its final path on first install and is removed wholesale on uninstall.

```bash
gofasta ai claude       # CLAUDE.md + .claude/{settings,hooks,commands,rules}/
gofasta ai cursor       # .cursor/rules/*.mdc (6 topic rules, no root briefing)
gofasta ai codex        # AGENTS.md + .codex/{config.toml, docs/*.md}
gofasta ai aider        # CONVENTIONS.md + .aider.conf.yml + .aider/docs/*.md
gofasta ai windsurf     # .windsurf/rules/*.md (6 topic rules, no root briefing)
gofasta ai list         # supported agents
gofasta ai status       # what's currently installed in this project
```

Every install ships a slim root briefing (where the agent expects one) plus six topic chunks — `conventions`, `overview`, `workflow`, `commands`, `debugging`, `docs-index` — wrapped with the right activation metadata for that agent (Claude's `paths:`, Cursor's `alwaysApply` / `globs` / `description`, Windsurf's `trigger:`, etc.). Each chunk stays under the agent's per-file size cap (Claude 200-line target, Codex 32 KiB, Windsurf 12 KB).

Installs are idempotent, support `--dry-run`, and are tracked in `.gofasta/ai.json`. Only one agent can be active at a time — pass `--switch` to atomically uninstall the previous one and install the new one.

### `--json` on every command

Every command that emits structured output honors the global `--json` flag, producing a single-line JSON document suitable for agent parsing, `jq` filtering, or CI consumption.

```bash
gofasta routes --json | jq '.[] | select(.method == "POST")'
gofasta --json verify | jq '.checks[] | select(.status == "fail")'
gofasta ai list --json
```

### Structured errors

Every CLI error carries `{code, message, hint, docs}` — agents pattern-match on the stable code and read the hint for the remediation. No regex-parsing English error strings.

```bash
$ gofasta --json g scaffold 2>&1 >/dev/null | jq .
{
  "code": "INVALID_NAME",
  "message": "missing resource name",
  "hint": "pass a PascalCase resource name — e.g. `gofasta g scaffold Product`",
  "docs": "https://gofasta.dev/docs/cli-reference/generate/scaffold"
}
```

## Command Index

Grouped as `gofasta --help` prints them. Flag-level detail lives at
[gofasta.dev/docs/cli-reference](https://gofasta.dev/docs/cli-reference).

| Group | Commands |
|---|---|
| Project lifecycle | `new`, `init`, `doctor`, `upgrade`, `version`, `ai` (`list` / `status` / `uninstall`) |
| Development workflow | `dev`, `serve`, `routes`, `swagger`, `wire`, `console`, `do`, `verify`, `status`, `config schema`, `debug` (18 subcommands: `requests`, `sql`, `traces`, `trace`, `logs`, `errors`, `last-error`, `last-slow-request`, `n-plus-one`, `explain`, `cache`, `goroutines`, `stack`, `profile`, `replay`, `har`, `health`, `watch`) |
| Database | `migrate up` / `migrate down` / `migrate repair`, `seed`, `db reset` |
| Code generation | `g scaffold`, `g model`, `g repository`, `g service`, `g controller`, `g dto`, `g migration`, `g route`, `g provider`, `g resolver`, `g job`, `g task`, `g email-template`, `g mock` — plus the modify-aware set: `g field`, `g method`, `g repo-method`, `g endpoint`, `g middleware`, `g relation`, `g rename` |
| Deployment | `deploy`, `deploy setup`, `deploy status`, `deploy logs`, `deploy rollback` |
| Introspection | `inspect`, `inspect-jobs`, `inspect-tasks`, `impact`, `xrefs`, `refactor` (`feature` / `layered` / `status`), `test` |
| Shell | `completion` (bash / zsh / fish / powershell) |

Global flags: `--json` (machine-parseable single-line JSON, banner suppressed) and `--no-banner` (also via `GOFASTA_NO_BANNER=1`).

## How It Works

The CLI is a standalone Go binary. It does **not** import the gofasta library — it only manipulates files on disk.

- `gofasta new` uses Go's `embed.FS` to carry project template files inside the binary. Templates are rendered with `text/template`, replacing placeholders like `{{.ModulePath}}`, `{{.ProjectNameLower}}`, `{{.DBDriver}}`, and `{{.GraphQL}}`. The binary embeds two filesystems: the project tree, and one set of foundational migrations per database driver — `gofasta new --driver X` copies the matching set into your project's `db/migrations/`.

- `gofasta g scaffold` reads Go template strings (for models, services, controllers, etc.) and renders them with your resource name and field definitions. It then patches existing files (container, wire, routes, serve) by finding insertion points via string matching.

- `gofasta dev` orchestrates the whole dev loop: it resolves a plan, starts any requested Docker compose services, waits for them to become healthy, runs a connectivity preflight (with an interactive recovery menu), applies migrations, then supervises Air on the host — or runs the full stack in Docker with `--services all` — and tears everything down on exit.

- `gofasta migrate` and `gofasta init` shell out to external tools (`migrate`, `go mod tidy`). They read your `config.yaml` to get database connection details.

- `gofasta serve` and `gofasta seed` delegate to your project's own binary (`go run ./app/main serve`) because they need to import your project's code.

## Project Layout

```
cli/
├── cmd/gofasta/main.go           # CLI entry point
├── internal/
│   ├── commands/                  # Cobra command definitions
│   │   ├── root.go               # Root command + subcommand registration
│   │   ├── new.go                # Project scaffolding
│   │   ├── dev*.go               # The dev loop (plan, services, preflight, Air supervisor)
│   │   ├── init_cmd.go           # Project initialization
│   │   ├── migrate.go            # Database migrations
│   │   ├── serve.go              # Passthrough to project serve
│   │   ├── seed.go               # Passthrough to project seed
│   │   ├── swagger.go            # Swagger generation
│   │   ├── ai/                   # `gofasta ai` — agent config installer
│   │   └── configutil/           # Reads config.yaml without importing the gofasta library
│   ├── generate/                  # Code generation engine
│   │   ├── commands.go           # Generate subcommands and step chains
│   │   ├── types.go              # ScaffoldData, Field, Step types
│   │   ├── scaffold_data.go      # Builds template data from CLI args
│   │   ├── writer.go             # Renders templates to files
│   │   ├── patcher.go            # Patches existing files (adds imports, fields)
│   │   ├── runner.go             # Runs Wire and gqlgen after generation
│   │   ├── fieldparse.go         # Parses "name:string" field definitions
│   │   ├── stringutil.go         # Case conversion, pluralization
│   │   ├── gen_*.go              # Individual generators (model, service, etc.)
│   │   └── templates/            # Go template strings for generated code
│   ├── skeleton/                  # Embedded project templates
│   │   ├── embed.go              # //go:embed all:project + per-driver migrations
│   │   ├── project/              # ~78 files that become a new project
│   │   └── migrations/           # Foundational migrations, one directory per driver
│   ├── layout/                    # Layered vs feature layout — every generated file path
│   ├── deploy/                    # `gofasta deploy` — SSH deploys (docker + binary methods)
│   ├── clierr/                    # Structured error codes ({code, message, hint, docs})
│   ├── cliout/                    # Single console-output surface (--json routing)
│   └── termcolor/                 # ANSI color helpers
├── go.mod
└── README.md
```

## Maintenance and sustainability

Gofasta is currently maintained by one person; sustainability planning — release cadence, security SLOs, the solo-to-team transition, and the automation arc that retires manual steps as the project matures — is documented in the [release coordination repo](https://github.com/gofastadev/release), specifically in [`CADENCE.md`](https://github.com/gofastadev/release/blob/main/CADENCE.md), [`RELEASING.md`](https://github.com/gofastadev/release/blob/main/RELEASING.md), and [`COMMUNITY.md`](https://github.com/gofastadev/release/blob/main/COMMUNITY.md). Read those three together for the full picture.

## Contributing

Contributions are welcome — see [CONTRIBUTING.md](./CONTRIBUTING.md).

## License

MIT
