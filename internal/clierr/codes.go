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
	CodeGofastaReplace  Code = "GOFASTA_REPLACE_INVALID"
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
		Hint: "wait 5–30 minutes for sum.golang.org to index a freshly-published release and retry, or set GOFASTA_REPLACE=/path/to/local/gofasta to bypass the proxy entirely",
		Docs: "https://gofasta.dev/docs/cli-reference/new",
	},
	CodeGofastaReplace: {
		Hint: "GOFASTA_REPLACE must point to a directory containing a valid gofasta checkout (go.mod present)",
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
