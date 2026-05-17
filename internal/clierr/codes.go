package clierr

// Code is a stable machine-readable identifier for an error class. Codes
// MUST NOT be renamed once shipped — AI agents, CI tooling, and custom
// automation rely on them for programmatic handling. Deprecate with a
// successor code rather than rename.
type Code string

// Error codes. Each has an entry in registry below with a remediation
// hint and a documentation URL. Keep the two lists in sync.
const (
	// CodeInternal is reserved for unexpected failures that indicate a
	// bug in the CLI itself, not user error.
	CodeInternal Code = "INTERNAL"

	// --- Project lifecycle ---

	CodeNotGofastaProject Code = "NOT_GOFASTA_PROJECT"
	CodeProjectDirExists  Code = "PROJECT_DIR_EXISTS"
	CodeInvalidName       Code = "INVALID_NAME"

	// --- go / go.mod ---

	CodeGoModInitFailed Code = "GO_MOD_INIT_FAILED"
	CodeGoModTidyFailed Code = "GO_MOD_TIDY_FAILED"
	CodeGofastaInstall  Code = "GOFASTA_INSTALL_FAILED"
	CodeGoBuildFailed   Code = "GO_BUILD_FAILED"
	CodeGoTestFailed    Code = "GO_TEST_FAILED"
	CodeGoVetFailed     Code = "GO_VET_FAILED"
	CodeGoFmtFailed     Code = "GO_FMT_FAILED"
	CodeGoLintFailed    Code = "GO_LINT_FAILED"

	// --- Wire / codegen ---

	CodeWireMissingProvider Code = "WIRE_MISSING_PROVIDER"
	CodeWireFailed          Code = "WIRE_GENERATION_FAILED"
	CodeGeneratorFailed     Code = "GENERATOR_FAILED"
	CodePatcherFailed       Code = "PATCHER_FAILED"
	CodeSwaggerFailed       Code = "SWAGGER_GENERATION_FAILED"
	CodeGqlgenFailed        Code = "GQLGEN_GENERATION_FAILED"

	// --- Database / migrations ---

	CodeMigrationFailed  Code = "MIGRATION_FAILED"
	CodeMigrationMissing Code = "MIGRATION_DIR_MISSING"
	CodeSeedFailed       Code = "SEED_FAILED"
	CodeDBUnreachable    Code = "DATABASE_UNREACHABLE"
	CodeDBResetFailed    Code = "DATABASE_RESET_FAILED"

	// --- Deploy ---

	CodeDeployHostRequired Code = "DEPLOY_HOST_REQUIRED"
	CodeDeployConfig       Code = "DEPLOY_CONFIG_INVALID"
	CodeSSHFailed          Code = "SSH_FAILED"
	CodeHealthCheckFailed  Code = "HEALTH_CHECK_FAILED"
	CodeDockerFailed       Code = "DOCKER_COMMAND_FAILED"
	CodeRollbackFailed     Code = "ROLLBACK_FAILED"

	// --- Introspection / utility ---

	CodeRoutesDirMissing Code = "ROUTES_DIR_MISSING"
	CodeConfigInvalid    Code = "CONFIG_INVALID"
	CodeConfigNotFound   Code = "CONFIG_NOT_FOUND"
	CodeFileIO           Code = "FILE_IO"

	// --- Verify / preflight ---

	CodeVerifyFailed Code = "VERIFY_FAILED"

	// --- AI installer ---

	CodeUnknownAgent    Code = "UNKNOWN_AGENT"
	CodeAIManifestIO    Code = "AI_MANIFEST_IO"
	CodeAIInstallFailed Code = "AI_INSTALL_FAILED"
	CodeAIAgentConflict Code = "AI_AGENT_CONFLICT"

	// --- Debug (gofasta debug) ---
	//
	// The debug command family talks to a running app's /debug/* JSON
	// endpoints. Failures split into reachability (app not running) and
	// capability (devtools tag off) so agents can branch cleanly.
	CodeDebugAppUnreachable     Code = "DEBUG_APP_UNREACHABLE"
	CodeDebugDevtoolsOff        Code = "DEBUG_DEVTOOLS_OFF"
	CodeDebugTraceNotFound      Code = "DEBUG_TRACE_NOT_FOUND"
	CodeDebugBadFilter          Code = "DEBUG_BAD_FILTER"
	CodeDebugBadDuration        Code = "DEBUG_BAD_DURATION"
	CodeDebugProfileUnsupported Code = "DEBUG_PROFILE_UNSUPPORTED"
	CodeDebugExplainFailed      Code = "DEBUG_EXPLAIN_FAILED"

	// --- Dev server (gofasta dev) ---
	//
	// The dev orchestrator is a multi-step pipeline (preflight → service
	// orchestration → migrations → Air) so each failure class gets its
	// own code. Agents can branch on the exact step that broke without
	// string-matching log output.
	CodeDevDockerUnavailable Code = "DEV_DOCKER_UNAVAILABLE"
	CodeDevComposeNotFound   Code = "DEV_COMPOSE_NOT_FOUND"
	CodeDevServiceUnhealthy  Code = "DEV_SERVICE_UNHEALTHY"
	CodeDevMigrationFailed   Code = "DEV_MIGRATION_FAILED"
	CodeDevAirNotInstalled   Code = "DEV_AIR_NOT_INSTALLED"
	CodeDevPortInUse         Code = "DEV_PORT_IN_USE"
	CodeDevFlagConflict      Code = "DEV_FLAG_CONFLICT"
	CodeDevLocalReplace      Code = "DEV_LOCAL_REPLACE"
	CodeDevServiceUnknown    Code = "DEV_SERVICE_UNKNOWN"
	CodeDevPreflightCancel   Code = "DEV_PREFLIGHT_CANCELED"

	// CodeInteractiveOnly is returned when a command that REQUIRES an
	// interactive terminal (REPL, TUI, etc.) is invoked with --json.
	// Agents and CI runners can pattern-match on the code and refuse
	// to call interactive commands programmatically rather than getting
	// a hung process or garbled output.
	CodeInteractiveOnly Code = "INTERACTIVE_ONLY"

	// --- Cross-resource impact analysis (gofasta xrefs / impact) ---
	//
	// Type-aware symbol lookup uses golang.org/x/tools/go/packages, so
	// failures split between "module won't load" (PACKAGE_LOAD_FAILED) and
	// "symbol not present after load" (SYMBOL_NOT_FOUND).
	CodeSymbolNotFound     Code = "SYMBOL_NOT_FOUND"
	CodeTypeAnalysisFailed Code = "TYPE_ANALYSIS_FAILED"
	CodePackageLoadFailed  Code = "PACKAGE_LOAD_FAILED"
	CodeAmbiguousSymbol    Code = "AMBIGUOUS_SYMBOL"

	// --- Change-scoped verify (--since / --changed) ---
	//
	// Three failure modes: not a git repo at all, the diff command failed
	// (permissions, malformed ref syntax), or the ref does not resolve.
	CodeGitNotAvailable Code = "GIT_NOT_AVAILABLE"
	CodeGitDiffFailed   Code = "GIT_DIFF_FAILED"
	CodeGitRefNotFound  Code = "GIT_REF_NOT_FOUND"

	// --- Modify-aware generators (g method / g field / g endpoint / ...) ---
	//
	// AST-based generators that edit existing resources. Idempotency checks
	// surface RESOURCE_NOT_FOUND / *_ALREADY_EXISTS so agents can branch on
	// "is this a fresh add or a no-op?" without re-parsing the source tree.
	CodeResourceNotFound    Code = "RESOURCE_NOT_FOUND"
	CodeMethodAlreadyExists Code = "METHOD_ALREADY_EXISTS"
	CodeFieldAlreadyExists  Code = "FIELD_ALREADY_EXISTS"
	CodeRouteAlreadyExists  Code = "ROUTE_ALREADY_EXISTS"
	CodeASTParseFailed      Code = "AST_PARSE_FAILED"
	CodeASTPatchFailed      Code = "AST_PATCH_FAILED"

	// --- Migration safety preview (gofasta migrate up --explain) ---
	CodeMigrationLintFailed  Code = "MIGRATION_LINT_FAILED"
	CodeMigrationParseFailed Code = "MIGRATION_PARSE_FAILED"

	// --- Mock regeneration (gofasta g mock) ---
	//
	// MOCK_DRIFT is the --check exit code: the on-disk mock does not match
	// the generated output. Use in CI to gate "mocks are stale" PRs.
	CodeInterfaceNotFound Code = "INTERFACE_NOT_FOUND"
	CodeMockGenFailed     Code = "MOCK_GEN_FAILED"
	CodeMockDrift         Code = "MOCK_DRIFT"

	// --- Job / task introspection (gofasta inspect-jobs / inspect-tasks) ---
	CodeJobsDirMissing  Code = "JOBS_DIR_MISSING"
	CodeTasksDirMissing Code = "TASKS_DIR_MISSING"

	// --- Debug replay (gofasta debug replay) ---
	//
	// SSRF-guarded re-fire of a captured request. UNSAFE fires when the
	// override would target a host other than the configured app URL.
	CodeDebugReplayNotFound Code = "DEBUG_REPLAY_NOT_FOUND"
	CodeDebugReplayFailed   Code = "DEBUG_REPLAY_FAILED"
	CodeDebugReplayUnsafe   Code = "DEBUG_REPLAY_UNSAFE"

	// --- Debug stack resolver (gofasta debug stack) ---
	CodeDebugStackParseFailed  Code = "DEBUG_STACK_PARSE_FAILED"
	CodeDebugSourceUnavailable Code = "DEBUG_SOURCE_UNAVAILABLE"
)

// meta carries the remediation hint and docs URL for a code. Looked up
// at Error construction time by New / Wrap / From.
type meta struct {
	Hint string
	Docs string
}

var registry = map[Code]meta{
	CodeInternal: {
		Hint: "file a bug at https://github.com/gofastadev/cli/issues with the full command output",
		Docs: "",
	},

	CodeNotGofastaProject: {
		Hint: "run this command from the root of a gofasta project (directory containing go.mod plus the scaffolded app/ directory)",
		Docs: "https://gofasta.dev/docs/getting-started/project-structure",
	},
	CodeProjectDirExists: {
		Hint: "choose a different project name or remove the existing directory",
		Docs: "https://gofasta.dev/docs/cli-reference/new",
	},
	CodeInvalidName: {
		Hint: "project names must be a valid Go module path (lowercase letters, digits, dots, slashes, hyphens)",
		Docs: "https://gofasta.dev/docs/cli-reference/new",
	},

	CodeGoModInitFailed: {
		Hint: "make sure Go 1.25.0 or later is installed and on $PATH; run `go version` to check",
		Docs: "https://gofasta.dev/docs/getting-started/installation",
	},
	CodeGoModTidyFailed: {
		Hint: "run `go mod tidy` manually and inspect the output; a transitive dep may be unavailable or the module proxy may be unreachable",
		Docs: "https://gofasta.dev/docs/getting-started/installation",
	},
	CodeGofastaInstall: {
		Hint: "wait 5–30 minutes for sum.golang.org to index a freshly-published release and retry, or run `go get github.com/gofastadev/gofasta@latest` inside the generated project once the sum DB catches up",
		Docs: "https://gofasta.dev/docs/cli-reference/new",
	},
	CodeGoBuildFailed: {
		Hint: "the generated or edited Go code does not compile; fix the error above and re-run",
		Docs: "",
	},
	CodeGoTestFailed: {
		Hint: "one or more tests failed; inspect the output above for the specific failure",
		Docs: "https://gofasta.dev/docs/guides/testing",
	},
	CodeGoVetFailed: {
		Hint: "`go vet` flagged a static issue; address the warnings above and re-run",
		Docs: "",
	},
	CodeGoFmtFailed: {
		Hint: "run `gofmt -s -w .` to apply formatting",
		Docs: "",
	},
	CodeGoLintFailed: {
		Hint: "`golangci-lint` reported issues; run `golangci-lint run` for full output",
		Docs: "",
	},

	CodeWireMissingProvider: {
		Hint: "add the provider to a provider set in app/di/providers/, then run `gofasta wire` to regenerate",
		Docs: "https://gofasta.dev/docs/cli-reference/wire",
	},
	CodeWireFailed: {
		Hint: "Wire failed to generate — inspect the error above; common causes are a missing provider, a type mismatch, or a circular dependency",
		Docs: "https://gofasta.dev/docs/cli-reference/wire",
	},
	CodeGeneratorFailed: {
		Hint: "the generator could not complete; inspect the error above and verify the project layout is intact",
		Docs: "https://gofasta.dev/docs/cli-reference/generate/scaffold",
	},
	CodePatcherFailed: {
		Hint: "the patcher could not locate an expected marker in a target file; verify you have not heavily modified the generated scaffold files",
		Docs: "https://gofasta.dev/docs/cli-reference/generate/scaffold",
	},
	CodeSwaggerFailed: {
		Hint: "run `gofasta swagger` manually to inspect the error; usually caused by malformed Swagger annotations on controller methods",
		Docs: "https://gofasta.dev/docs/cli-reference/swagger",
	},
	CodeGqlgenFailed: {
		Hint: "run `go tool gqlgen generate` manually to inspect the error; usually caused by a malformed .gql schema file",
		Docs: "https://gofasta.dev/docs/guides/graphql",
	},

	CodeMigrationFailed: {
		Hint: "inspect the SQL error above; ensure the database is reachable and the migration file is valid",
		Docs: "https://gofasta.dev/docs/cli-reference/migrate",
	},
	CodeMigrationMissing: {
		Hint: "create db/migrations/ or generate a migration with `gofasta g migration`",
		Docs: "https://gofasta.dev/docs/cli-reference/generate/migration",
	},
	CodeSeedFailed: {
		Hint: "a seeder returned an error; inspect the output above",
		Docs: "https://gofasta.dev/docs/cli-reference/seed",
	},
	CodeDBUnreachable: {
		Hint: "verify the database is running and the `database` section of config.yaml matches; test with `gofasta doctor`",
		Docs: "https://gofasta.dev/docs/guides/database-and-migrations",
	},
	CodeDBResetFailed: {
		Hint: "`gofasta db reset` could not complete; inspect the step that failed above",
		Docs: "https://gofasta.dev/docs/cli-reference/db",
	},

	CodeDeployHostRequired: {
		Hint: "set `deploy.host` in config.yaml or pass --host user@server",
		Docs: "https://gofasta.dev/docs/cli-reference/deploy",
	},
	CodeDeployConfig: {
		Hint: "the deploy configuration is invalid; run `gofasta doctor` or check config.yaml against the schema",
		Docs: "https://gofasta.dev/docs/cli-reference/deploy",
	},
	CodeSSHFailed: {
		Hint: "verify your SSH key is authorized on the server and the host/port are reachable — test with `ssh -p <port> user@server echo ok`",
		Docs: "https://gofasta.dev/docs/cli-reference/deploy",
	},
	CodeHealthCheckFailed: {
		Hint: "the deployed app did not respond at the health endpoint within the timeout; the previous release is still active — inspect logs with `gofasta deploy logs`",
		Docs: "https://gofasta.dev/docs/cli-reference/deploy",
	},
	CodeDockerFailed: {
		Hint: "a Docker command failed; check that Docker is running locally and on the remote host (run `gofasta deploy setup` to install it remotely)",
		Docs: "https://gofasta.dev/docs/cli-reference/deploy",
	},
	CodeRollbackFailed: {
		Hint: "rollback could not complete; inspect the step that failed above — the current release is unchanged",
		Docs: "https://gofasta.dev/docs/cli-reference/deploy",
	},

	CodeRoutesDirMissing: {
		Hint: "app/rest/routes/ was not found — run this command from the root of a gofasta project",
		Docs: "https://gofasta.dev/docs/getting-started/project-structure",
	},
	CodeConfigInvalid: {
		Hint: "config.yaml is malformed; validate it against the schema emitted by `gofasta config schema`",
		Docs: "https://gofasta.dev/docs/guides/configuration",
	},
	CodeConfigNotFound: {
		Hint: "config.yaml not found in the current directory",
		Docs: "https://gofasta.dev/docs/guides/configuration",
	},
	CodeFileIO: {
		Hint: "could not read or write a file; check filesystem permissions",
		Docs: "",
	},

	CodeVerifyFailed: {
		Hint: "`gofasta verify` reported a failing check above; fix the first failure and re-run",
		Docs: "",
	},

	CodeUnknownAgent: {
		Hint: "run `gofasta ai list` to see supported agents",
		Docs: "",
	},
	CodeAIManifestIO: {
		Hint: "could not read or write .gofasta/ai.json; check filesystem permissions",
		Docs: "",
	},
	CodeAIInstallFailed: {
		Hint: "one or more agent configuration files could not be written; inspect the error above",
		Docs: "",
	},
	CodeAIAgentConflict: {
		Hint: "another AI agent is already installed in this project; re-run with `--switch` to replace it, or `gofasta ai uninstall <agent>` to remove it first",
		Docs: "",
	},

	CodeDevDockerUnavailable: {
		Hint: "install Docker Desktop (or Docker Engine + docker compose plugin) and make sure the daemon is running — test with `docker info`",
		Docs: "https://gofasta.dev/docs/cli-reference/dev",
	},
	CodeDevComposeNotFound: {
		Hint: "a compose.yaml is required for service orchestration; re-run with `--no-services` to skip Docker and run Air against an externally-managed database",
		Docs: "https://gofasta.dev/docs/cli-reference/dev",
	},
	CodeDevServiceUnhealthy: {
		Hint: "a compose service did not become healthy within the timeout; tail its logs with `docker compose logs <service>`, or raise `--wait-timeout`",
		Docs: "https://gofasta.dev/docs/cli-reference/dev",
	},
	CodeDevMigrationFailed: {
		Hint: "`migrate up` returned a non-zero exit; inspect the SQL error above or re-run with `--no-migrate` to skip and investigate the DB state manually",
		Docs: "https://gofasta.dev/docs/cli-reference/dev",
	},
	CodeDevAirNotInstalled: {
		Hint: "Air is not registered on the project toolchain; run `go get github.com/air-verse/air@latest && go mod edit -tool github.com/air-verse/air`",
		Docs: "https://gofasta.dev/docs/cli-reference/dev",
	},
	CodeDevPortInUse: {
		Hint: "another process is already bound to the configured PORT; stop it, pick a different port with `--port`, or update `server.port` in config.yaml",
		Docs: "https://gofasta.dev/docs/cli-reference/dev",
	},
	CodeDevFlagConflict: {
		Hint: "two flags requested incompatible behavior — see the message above; run `gofasta dev --help` for the flag matrix",
		Docs: "https://gofasta.dev/docs/cli-reference/dev",
	},
	CodeDevLocalReplace: {
		Hint: "filesystem-path replaces (e.g. `replace ... => ../foo`) only resolve on the host — the docker build context cannot see paths outside the project. Either run without --all-in-docker (host mode handles local replaces fine), drop the replace and `go get` a published version, or vendor with `go mod vendor` so the replaced module is bundled into the build context.",
		Docs: "https://gofasta.dev/docs/cli-reference/dev",
	},
	CodeDevServiceUnknown: {
		Hint: "the name passed to --services is not declared in compose.yaml; check `docker compose config --services` for the list of valid names",
		Docs: "https://gofasta.dev/docs/cli-reference/dev",
	},
	CodeDevPreflightCancel: {
		Hint: "preflight was canceled by the user (menu option [4]) or aborted on a non-TTY session — resolve the unreachable dependency manually, then re-run `gofasta dev`",
		Docs: "https://gofasta.dev/docs/cli-reference/dev",
	},
	CodeInteractiveOnly: {
		Hint: "this command requires an interactive terminal and cannot run in --json / headless mode; drop --json or invoke a non-interactive equivalent",
		Docs: "https://gofasta.dev/docs/cli-reference",
	},

	CodeDebugAppUnreachable: {
		Hint: "the target app is not reachable at the resolved URL — start it with `gofasta dev` or pass `--app-url=http://host:port` if it runs on a different address",
		Docs: "https://gofasta.dev/docs/cli-reference/debug",
	},
	CodeDebugDevtoolsOff: {
		Hint: "the app is running without the `devtools` build tag — rebuild under `gofasta dev` (which sets GOFLAGS=-tags=devtools) so /debug/* endpoints become available",
		Docs: "https://gofasta.dev/docs/guides/debugging",
	},
	CodeDebugTraceNotFound: {
		Hint: "the requested trace is not in the ring — it may have been evicted (rings hold at most 50 traces); re-issue the request you want to inspect and try again",
		Docs: "https://gofasta.dev/docs/guides/debugging",
	},
	CodeDebugBadFilter: {
		Hint: "a filter value was rejected; see the command's --help for accepted syntax",
		Docs: "https://gofasta.dev/docs/cli-reference/debug",
	},
	CodeDebugBadDuration: {
		Hint: "duration values use Go's time.ParseDuration syntax — e.g. `100ms`, `2s`, `1m30s`",
		Docs: "https://gofasta.dev/docs/cli-reference/debug",
	},
	CodeDebugProfileUnsupported: {
		Hint: "supported profile kinds: cpu, heap, goroutine, mutex, block, allocs, threadcreate, trace",
		Docs: "https://gofasta.dev/docs/cli-reference/debug",
	},
	CodeDebugExplainFailed: {
		Hint: "EXPLAIN is SELECT-only and requires the app to have registered its *gorm.DB via devtools.RegisterDB — verify the app was built with the devtools tag",
		Docs: "https://gofasta.dev/docs/guides/debugging",
	},

	CodeSymbolNotFound: {
		Hint: "the symbol was not found in the current module — check the spelling and package qualifier (e.g. `pkg.Func` or `pkg.Type.Method`)",
		Docs: "https://gofasta.dev/docs/cli-reference/xrefs",
	},
	CodeTypeAnalysisFailed: {
		Hint: "go/packages could not type-check the module; run `go build ./...` to surface the underlying compile error",
		Docs: "https://gofasta.dev/docs/cli-reference/xrefs",
	},
	CodePackageLoadFailed: {
		Hint: "one or more packages failed to load — fix the build error above before running impact analysis",
		Docs: "https://gofasta.dev/docs/cli-reference/impact",
	},
	CodeAmbiguousSymbol: {
		Hint: "the unqualified symbol matches definitions in multiple packages; pass the fully qualified name (e.g. `irodata/app/services.OrderService.Archive`)",
		Docs: "https://gofasta.dev/docs/cli-reference/xrefs",
	},

	CodeGitNotAvailable: {
		Hint: "this directory is not a git repository — run `git init` or drop the --since/--changed flag",
		Docs: "https://gofasta.dev/docs/cli-reference/verify",
	},
	CodeGitDiffFailed: {
		Hint: "`git diff` returned an error; check that the ref exists locally (`git fetch` may be needed) and that you have read access to the repo",
		Docs: "https://gofasta.dev/docs/cli-reference/verify",
	},
	CodeGitRefNotFound: {
		Hint: "the supplied git ref does not resolve — try `git fetch origin` or pass a known commit / branch / tag",
		Docs: "https://gofasta.dev/docs/cli-reference/verify",
	},

	CodeResourceNotFound: {
		Hint: "no files match the given resource name — check spelling (PascalCase, singular) or run `gofasta g scaffold <Name>` to create it first",
		Docs: "https://gofasta.dev/docs/cli-reference/generate/method",
	},
	CodeMethodAlreadyExists: {
		Hint: "a method with that name already exists on the target interface — pick a different name or skip this generator",
		Docs: "https://gofasta.dev/docs/cli-reference/generate/method",
	},
	CodeFieldAlreadyExists: {
		Hint: "the model already has a field with that name — pick a different name or remove the existing field first",
		Docs: "https://gofasta.dev/docs/cli-reference/generate/field",
	},
	CodeRouteAlreadyExists: {
		Hint: "that METHOD + path combination is already registered — pick a different path or use `gofasta g middleware` to attach behavior",
		Docs: "https://gofasta.dev/docs/cli-reference/generate/endpoint",
	},
	CodeASTParseFailed: {
		Hint: "the target Go file has a syntax error and cannot be parsed — fix the error above and re-run",
		Docs: "https://gofasta.dev/docs/cli-reference/generate/method",
	},
	CodeASTPatchFailed: {
		Hint: "could not locate the AST insertion target (e.g. interface or struct named for the resource) — the file may have been heavily restructured; inspect manually",
		Docs: "https://gofasta.dev/docs/cli-reference/generate/method",
	},

	CodeMigrationLintFailed: {
		Hint: "static SQL analysis errored on a pending migration — inspect the file for malformed SQL or unsupported syntax",
		Docs: "https://gofasta.dev/docs/cli-reference/migrate",
	},
	CodeMigrationParseFailed: {
		Hint: "could not split the migration into statements — check for unmatched `$$` dollar-quote blocks or stray string literals",
		Docs: "https://gofasta.dev/docs/cli-reference/migrate",
	},

	CodeInterfaceNotFound: {
		Hint: "no interface with that name was found under app/services/interfaces/ or app/repositories/interfaces/ — check spelling or pass --all to refresh every mock",
		Docs: "https://gofasta.dev/docs/cli-reference/generate/mock",
	},
	CodeMockGenFailed: {
		Hint: "the mock template failed to render — inspect the error above; often caused by an interface that uses unsupported features (generics, embedded external interfaces)",
		Docs: "https://gofasta.dev/docs/cli-reference/generate/mock",
	},
	CodeMockDrift: {
		Hint: "the on-disk mock no longer matches the interface — run `gofasta g mock --all` to regenerate, then commit the result",
		Docs: "https://gofasta.dev/docs/cli-reference/generate/mock",
	},

	CodeJobsDirMissing: {
		Hint: "app/jobs/ was not found — generate a job with `gofasta g job <name> \"<cron>\"` first, or run this command from the project root",
		Docs: "https://gofasta.dev/docs/cli-reference/inspect-jobs",
	},
	CodeTasksDirMissing: {
		Hint: "app/tasks/ was not found — generate a task with `gofasta g task <name>` first, or run this command from the project root",
		Docs: "https://gofasta.dev/docs/cli-reference/inspect-tasks",
	},

	CodeDebugReplayNotFound: {
		Hint: "the request id is not in the capture ring — it may have been evicted (rings hold at most 200 requests); re-issue the request you want to replay",
		Docs: "https://gofasta.dev/docs/cli-reference/debug",
	},
	CodeDebugReplayFailed: {
		Hint: "the replayed request failed at the target app — inspect the response payload above or check the app logs",
		Docs: "https://gofasta.dev/docs/cli-reference/debug",
	},
	CodeDebugReplayUnsafe: {
		Hint: "the replay override was rejected by the SSRF guard — overrides may change path / headers / body but cannot change scheme, host, or port",
		Docs: "https://gofasta.dev/docs/cli-reference/debug",
	},

	CodeDebugStackParseFailed: {
		Hint: "the stack frame does not match the expected `file:line function` format — verify the source is a gofasta-captured stack (TraceSpan.Stack or ExceptionEntry.Stack)",
		Docs: "https://gofasta.dev/docs/cli-reference/debug",
	},
	CodeDebugSourceUnavailable: {
		Hint: "the source file referenced in the stack frame is not present on disk (deleted, vendored, or outside the current module) — the frame is still resolvable but without source context",
		Docs: "https://gofasta.dev/docs/cli-reference/debug",
	},
}

// lookup returns the metadata for code, or an empty meta{} if code is not
// registered. Unregistered codes still produce usable errors — just without
// a hint or docs URL.
func lookup(code Code) meta {
	if m, ok := registry[code]; ok {
		return m
	}
	return meta{}
}

// AllCodes returns every code present in the registry, sorted in the order
// they are declared above. Intended for tests that want to assert all codes
// have non-empty hint strings.
func AllCodes() []Code {
	codes := make([]Code, 0, len(registry))
	for code := range registry {
		codes = append(codes, code)
	}
	return codes
}
