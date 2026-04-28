# Gofasta CLI

[![CI](https://github.com/gofastadev/cli/actions/workflows/ci.yml/badge.svg)](https://github.com/gofastadev/cli/actions/workflows/ci.yml) [![CodeQL](https://github.com/gofastadev/cli/actions/workflows/codeql.yml/badge.svg)](https://github.com/gofastadev/cli/actions/workflows/codeql.yml) [![codecov](https://codecov.io/gh/gofastadev/cli/graph/badge.svg)](https://codecov.io/gh/gofastadev/cli) [![Go Reference](https://pkg.go.dev/badge/github.com/gofastadev/cli.svg)](https://pkg.go.dev/github.com/gofastadev/cli) [![Go Report Card](https://goreportcard.com/badge/github.com/gofastadev/cli)](https://goreportcard.com/report/github.com/gofastadev/cli) [![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT) [![Go Version](https://img.shields.io/github/go-mod/go-version/gofastadev/cli)](https://github.com/gofastadev/cli/blob/main/go.mod) [![Release](https://img.shields.io/github/v/release/gofastadev/cli)](https://github.com/gofastadev/cli/releases)

The command-line tool for [Gofasta](https://github.com/gofastadev/gofasta), a Go backend toolkit. The CLI is a standalone binary that creates new projects, generates code, and runs common development tasks. It does not import the gofasta library — it only manipulates files on disk.

## Install the CLI

The CLI lives in its own Go module (`github.com/gofastadev/cli`) with `main.go` at `cmd/gofasta/`. It is not the same as the `github.com/gofastadev/gofasta` library, which your generated projects import as a dependency. You install one, you import the other.

**Option A — `go install` (recommended for Go developers, requires Go 1.25.0+):**

```bash
go install github.com/gofastadev/cli/cmd/gofasta@latest
```

Compiles the CLI from source using your local Go toolchain and drops the `gofasta` binary into `$GOBIN` (or `$GOPATH/bin` if `GOBIN` is unset — usually `~/go/bin`).

**Option B — Pre-built binary via shell script (no Go toolchain needed):**

```bash
curl -fsSL https://raw.githubusercontent.com/gofastadev/cli/main/dist/install.sh | sh
```

Downloads the latest pre-built binary for your platform from [GitHub Releases](https://github.com/gofastadev/cli/releases) and installs it to `/usr/local/bin/gofasta`. Works on macOS and Linux for both `amd64` and `arm64`. The script detects your shell and prints exact `export PATH=…` instructions if the install directory isn't already on your `$PATH`.

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
| **Option A** — `go install` compiles a native binary with your exact toolchain and there's nothing to keep in sync. Upgrade with `go install …@latest`. | **Option B** — shell script. Downloads a pre-built binary from GitHub Releases. Upgrade with `gofasta upgrade`. |

Both end up as a `gofasta` binary in a standard directory. Use whichever matches your environment.

## Create a New Project

```bash
gofasta new myapp
```

Or with a full module path:

```bash
gofasta new github.com/myorg/myapp
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
│   ├── graphql/
│   │   ├── schema/         # GraphQL schema files (.gql)
│   │   └── resolvers/      # GraphQL resolvers
│   ├── validators/         # Input validation rules
│   ├── di/                 # Dependency injection (Google Wire)
│   └── jobs/               # Cron jobs
├── cmd/                    # CLI commands for your app
│   ├── root.go             # Cobra root command
│   ├── serve.go            # Starts the HTTP server
│   └── seed.go             # Runs database seeders
├── db/
│   ├── migrations/         # SQL migration files
│   └── seeds/              # Database seed functions
├── configs/                # RBAC policies, feature flags
├── deployments/            # Docker, Kubernetes, CI/CD, nginx, systemd
├── templates/emails/       # HTML email templates
├── locales/                # Translation files
├── config.yaml             # Application configuration
├── compose.yaml            # Docker Compose (app + PostgreSQL)
├── Dockerfile              # Production container image
├── Makefile                # Development shortcuts
└── gqlgen.yml              # GraphQL code generation config
```

The project imports `github.com/gofastadev/gofasta` as a library dependency. It does **not** contain any CLI or gofasta library internals — only your application code.

### What Happens During `gofasta new`

1. Creates the project directory
2. Runs `go mod init` with your module path
3. Copies ~78 template files, replacing placeholders with your project name
4. Runs `go get github.com/gofastadev/gofasta@latest` to pull the gofasta library as a project dependency, plus tool dependencies (Wire, gqlgen, Air, swag)
5. Runs `go mod tidy`
6. Generates Wire dependency injection code
7. Generates GraphQL resolver code
8. Initializes a git repository with an initial commit

## Start Developing

After creating a project:

```bash
cd myapp

# Option 1: Docker (recommended — starts app + PostgreSQL)
make up

# Option 2: Host machine (requires local PostgreSQL)
docker compose up db -d    # Start just the database
make dev                   # Run with hot reload
```

Your app is now running:
- REST API: `http://localhost:8080/api/v1/`
- GraphQL: `http://localhost:8080/graphql`
- GraphQL Playground: `http://localhost:8080/graphql-playground`
- Health check: `http://localhost:8080/health`

## Generate Code

The `generate` command (shorthand: `g`) creates boilerplate code for new resources. Every generated file is auto-wired into the dependency injection container, routes, and GraphQL schema.

### Scaffold a Full Resource

```bash
gofasta g scaffold Product name:string price:float
```

This single command creates **11 files** and patches 4 existing files:

| Created file | What it is |
|-------------|-----------|
| `app/models/product.model.go` | Database model with `name` and `price` fields |
| `db/migrations/000006_create_products.up.sql` | SQL to create the `products` table |
| `db/migrations/000006_create_products.down.sql` | SQL to drop the `products` table |
| `app/repositories/interfaces/product_repository.go` | Repository interface (contract) |
| `app/repositories/product.repository.go` | Repository implementation (GORM queries) |
| `app/services/interfaces/product_service.go` | Service interface (contract) |
| `app/services/product.service.go` | Service implementation (business logic) |
| `app/dtos/product.dtos.go` | Request/response DTOs with validation tags |
| `app/di/providers/product.go` | Wire dependency injection provider |
| `app/rest/controllers/product.controller.go` | REST controller with CRUD handlers |
| `app/rest/routes/product.routes.go` | Route definitions (GET, POST, PUT, DELETE) |

It also patches these files automatically:
- `app/di/container.go` — adds `ProductService` and `ProductController` fields
- `app/di/wire.go` — adds `ProductSet` to the Wire build
- `app/rest/routes/index.routes.go` — registers Product routes
- `cmd/serve.go` — wires `ProductController` into the route config

After scaffolding, run migrations and start coding your business logic:

```bash
gofasta migrate up
# Edit app/services/product.service.go — that's where your logic goes
```

### Add GraphQL Support

```bash
gofasta g scaffold Product name:string price:float --graphql
```

The `--graphql` flag additionally creates a `.gql` schema file and auto-wires a GraphQL resolver.

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

# Model + repository
gofasta g repository Product name:string price:float

# Model + repo + service + DTOs + Wire provider
gofasta g service Product name:string price:float

# Everything up to controller + routes
gofasta g controller Product name:string price:float

# Individual pieces
gofasta g dto Product name:string price:float     # DTOs only
gofasta g migration Product name:string            # SQL files only
gofasta g route Product                             # Route file only
gofasta g provider Product                          # Wire provider only
gofasta g resolver Product                          # Patch GraphQL resolver
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

This creates `.env` from `.env.example`, runs `go mod tidy`, generates Wire and GraphQL code, runs migrations, and verifies the build.

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

Every scaffolded project ships `AGENTS.md` at the root by default (the universal file every modern agent reads). For agent-specific configuration — permission allowlists, pre-commit hooks, slash commands, conventions files — opt in with one command:

```bash
gofasta ai claude       # .claude/ settings + hooks + slash commands
gofasta ai cursor       # .cursor/rules/gofasta.mdc
gofasta ai codex        # .codex/config.toml
gofasta ai aider        # .aider.conf.yml + .aider/CONVENTIONS.md
gofasta ai windsurf     # .windsurfrules
gofasta ai list         # supported agents
gofasta ai status       # what's currently installed in this project
```

Installs are idempotent, support `--dry-run`, and are tracked in `.gofasta/ai.json`.

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

## How It Works

The CLI is a standalone Go binary. It does **not** import the gofasta library — it only manipulates files on disk.

- `gofasta new` uses Go's `embed.FS` to carry project template files inside the binary. Templates are rendered with `text/template`, replacing `{{.ModulePath}}` with your project's module path and `{{.ProjectNameLower}}` with your project name.

- `gofasta g scaffold` reads Go template strings (for models, services, controllers, etc.) and renders them with your resource name and field definitions. It then patches existing files (container, wire, routes, serve) by finding insertion points via string matching.

- `gofasta dev`, `gofasta migrate`, `gofasta init` are thin wrappers that shell out to external tools (`air`, `migrate`, `go mod tidy`). They read your `config.yaml` to get database connection details.

- `gofasta serve` and `gofasta seed` delegate to your project's own binary (`go run ./app/main serve`) because they need to import your project's code.

## Project Layout

```
cli/
├── cmd/gofasta/main.go           # CLI entry point
├── internal/
│   ├── commands/                  # Cobra command definitions
│   │   ├── root.go               # Root command + subcommand registration
│   │   ├── new.go                # Project scaffolding
│   │   ├── dev.go                # Development server
│   │   ├── init_cmd.go           # Project initialization
│   │   ├── migrate.go            # Database migrations
│   │   ├── serve.go              # Passthrough to project serve
│   │   ├── seed.go               # Passthrough to project seed
│   │   ├── swagger.go            # Swagger generation
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
│   └── skeleton/                  # Embedded project templates
│       ├── embed.go              # //go:embed all:project
│       └── project/              # ~78 files that become a new project
├── dist/                          # CLI distribution files
│   └── install.sh                # curl-pipe-sh installer
├── go.mod
└── README.md
```

## License

MIT
