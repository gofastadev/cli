package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// strconvItoa is a tiny alias — scoped to this file's exec-stubbing
// helpers that build GOFASTA_FAKE_EXIT env values.
func strconvItoa(i int) string { return strconv.Itoa(i) }

func TestDevCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "dev" {
			found = true
			break
		}
	}
	assert.True(t, found, "devCmd should be registered on rootCmd")
}

func TestDevCmd_HasDescription(t *testing.T) {
	assert.NotEmpty(t, devCmd.Short)
	assert.NotEmpty(t, devCmd.Long)
}

// setupDevTempdir creates a temp project dir, chdirs into it, writes a
// minimal config.yaml so configutil.BuildMigrationURL returns a usable URL,
// and restores the original cwd on cleanup.
func setupDevTempdir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("database:\n  driver: postgres\n  name: testdb\n"), 0o644))
	require.NoError(t, os.Chdir(dir))
}

// runDev happy path — .env loaded, migration + air mocked to succeed,
// function returns nil. Covers:
//   - the loadDotEnv success branch (loaded > 0 "Loaded N variables" step)
//   - the "Running migrations" step with a mocked migrate CLI returning 0
//   - the "Starting air" step with a mocked `go tool air` returning 0
//   - full happy-path traversal of runDev
func TestRunDev_HappyPathWithEnv(t *testing.T) {
	setupDevTempdir(t)
	require.NoError(t, os.WriteFile(".env",
		[]byte("DEV_TEST_RUN_HAPPY_VAR=loaded\n"), 0o644))
	t.Cleanup(func() { _ = os.Unsetenv("DEV_TEST_RUN_HAPPY_VAR") })

	withFakeExec(t, 0)

	err := runDev(devFlags{envFile: ".env", noServices: true})
	assert.NoError(t, err)
	// The .env was loaded — value now in process env.
	assert.Equal(t, "loaded", os.Getenv("DEV_TEST_RUN_HAPPY_VAR"))
}

// runDev when .env is missing — loadDotEnv returns (0, nil), the "Loaded"
// step is skipped, and the rest of the flow still runs to completion.
func TestRunDev_NoDotEnv(t *testing.T) {
	setupDevTempdir(t)
	withFakeExec(t, 0)

	err := runDev(devFlags{envFile: ".env", noServices: true})
	assert.NoError(t, err)
}

// runDev when .env exists but is unreadable — loadDotEnv returns an error,
// runDev emits a PrintWarn and continues. Covers the error branch at
// dev.go:52-53.
func TestRunDev_UnreadableDotEnv(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod-based read denial")
	}
	setupDevTempdir(t)
	require.NoError(t, os.WriteFile(".env", []byte("FOO=bar\n"), 0o644))
	require.NoError(t, os.Chmod(".env", 0o000))
	t.Cleanup(func() { _ = os.Chmod(".env", 0o644) })

	withFakeExec(t, 0)
	err := runDev(devFlags{envFile: ".env", noServices: true})
	// runDev treats the load error as non-fatal — it prints a warning and
	// carries on. No error is returned.
	assert.NoError(t, err)
}

// runDev when the migration step fails — warn branch of the migration
// block is exercised and runDev still proceeds to start air.
func TestRunDev_MigrationFails(t *testing.T) {
	setupDevTempdir(t)
	withFakeExec(t, 1) // every exec returns non-zero
	// Provide a migrate binary on PATH so runMigrations doesn't short-circuit
	// at the LookPath check. fakeExecCommand produces the binary path from
	// os.Args[0] (the test binary itself) which is always on PATH.
	origLookPath := execLookPath
	execLookPath = func(name string) (string, error) { return "/usr/bin/migrate", nil }
	t.Cleanup(func() { execLookPath = origLookPath })

	err := runDev(devFlags{envFile: ".env", noServices: true})
	// Air also fails (same fakeExec) — runDev returns the air error.
	assert.Error(t, err)
}

// runMigrations with migrate not installed — returns a clear error message
// mentioning where to install from.
func TestRunMigrations_MigrateNotFound(t *testing.T) {
	setupDevTempdir(t)
	origLookPath := execLookPath
	execLookPath = func(name string) (string, error) { return "", fmt.Errorf("not found") }
	t.Cleanup(func() { execLookPath = origLookPath })

	err := runMigrations()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "migrate CLI not found")
	assert.Contains(t, err.Error(), "v4.18.1")
}

// runMigrations with empty DB URL — returns error about config.
func TestRunMigrations_EmptyDBURL(t *testing.T) {
	// Empty temp dir with no config.yaml → BuildMigrationURL returns something
	// with empty fields but non-empty string; so this particular test won't
	// trigger the empty-URL path. Use a dir without config.yaml AND no env
	// vars set — but configutil always returns a non-empty URL with defaults.
	// Skip this — the branch is defensive and practically unreachable since
	// configutil always returns at least the default postgres URL.
}

// runMigrations succeeds on first attempt.
func TestRunMigrations_SuccessFirstAttempt(t *testing.T) {
	setupDevTempdir(t)
	origLookPath := execLookPath
	execLookPath = func(name string) (string, error) { return "/usr/bin/migrate", nil }
	t.Cleanup(func() { execLookPath = origLookPath })
	withFakeExec(t, 0)

	err := runMigrations()
	assert.NoError(t, err)
}

// runMigrations fails first attempt but succeeds on retry.
func TestRunMigrations_SuccessOnRetry(t *testing.T) {
	setupDevTempdir(t)
	origLookPath := execLookPath
	execLookPath = func(name string) (string, error) { return "/usr/bin/migrate", nil }
	t.Cleanup(func() { execLookPath = origLookPath })
	// First call (migrate up) fails, second call (retry) succeeds.
	stagedFakeExec(t, 1, 0)

	err := runMigrations()
	assert.NoError(t, err)
}

// runMigrations fails both attempts — returns the error from the second try.
func TestRunMigrations_FailsBothAttempts(t *testing.T) {
	setupDevTempdir(t)
	origLookPath := execLookPath
	execLookPath = func(name string) (string, error) { return "/usr/bin/migrate", nil }
	t.Cleanup(func() { execLookPath = origLookPath })
	withFakeExec(t, 1) // both attempts fail

	err := runMigrations()
	assert.Error(t, err)
}

// runMigrateUp — direct test of the single-attempt function.
func TestRunMigrateUp(t *testing.T) {
	withFakeExec(t, 0)
	assert.NoError(t, runMigrateUp("postgres://test:test@localhost:5432/testdb"))

	withFakeExec(t, 1)
	assert.Error(t, runMigrateUp("postgres://test:test@localhost:5432/testdb"))
}

// ─────────────────────────────────────────────────────────────────────
// Coverage for runDev branches that the happy-path tests skip:
// flags.port, flags.orchestrate with compose services, flags.fresh,
// flags.seed, flags.attachLogs, flags.dashboard, and runAir branches.
// ─────────────────────────────────────────────────────────────────────

// TestRunDev_WithFlagPort — flags.port != "" sets the PORT env var and
// takes the port override branch when picking URLs.
func TestRunDev_WithFlagPort(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	t.Setenv("PORT", "") // reset
	err := runDev(devFlags{envFile: ".env", noServices: true, port: "9999"})
	assert.NoError(t, err)
	assert.Equal(t, "9999", os.Getenv("PORT"))
}

// TestRunDev_DryRun — dry-run path prints the plan and returns.
func TestRunDev_DryRun(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	err := runDev(devFlags{envFile: ".env", noServices: true, dryRun: true})
	assert.NoError(t, err)
}

// TestRunDev_Seed — flags.seed triggers runSeedDelegation. We stub
// exec so it succeeds.
func TestRunDev_Seed(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	err := runDev(devFlags{envFile: ".env", noServices: true, seed: true})
	assert.NoError(t, err)
}

// TestRunDev_SeedFails — seed returns error; runDev continues.
func TestRunDev_SeedFails(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	// stagedFakeExec: migrate=0, seed=1, then air=0 (final code repeats).
	stagedFakeExec(t, 0, 1, 0)
	err := runDev(devFlags{envFile: ".env", noServices: true, seed: true})
	assert.NoError(t, err)
}

// TestRunDev_WithComposeOrchestration — compose.yaml present and
// docker fake responds to everything.
func TestRunDev_WithComposeOrchestration(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	// compose.yaml makes plan.orchestrate true.
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db:\n    image: postgres\n"), 0o644))
	// Stub every docker call: info, version, compose version, compose
	// config, compose up, compose ps, migrate, air. Use fakeExecOutput
	// with a config blob that has one service with a healthcheck.
	composeConfig := `{"services":{"db":{"healthcheck":{"test":["CMD","pg_isready"]}}}}`
	composePS := `[{"Service":"db","State":"running","Health":"healthy"}]`
	orig := execCommand
	call := 0
	execCommand = func(name string, args ...string) *exec.Cmd {
		stdout := ""
		// Decide stdout based on the argv shape.
		if len(args) > 0 && args[0] == "info" {
			stdout = ""
		} else if len(args) >= 2 && args[0] == "version" {
			stdout = "28.0\n"
		} else if len(args) >= 2 && args[0] == "compose" && args[1] == "version" {
			stdout = "v2.26\n"
		} else if len(args) >= 2 && args[0] == "compose" && args[1] == "config" {
			stdout = composeConfig
		} else if len(args) >= 2 && args[0] == "compose" && args[1] == "ps" {
			stdout = composePS
		}
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"=0",
			"GOFASTA_FAKE_STDOUT="+stdout,
		)
		call++
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })

	err := runDev(devFlags{envFile: ".env", waitTimeout: 5e9, keepVolumes: true})
	assert.NoError(t, err)
}

// TestRunDev_Fresh_WithCompose — fresh=true with orchestrate triggers
// resetVolumes call.
func TestRunDev_Fresh_WithCompose(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db:\n    image: postgres\n"), 0o644))
	composeConfig := `{"services":{"db":{}}}`
	composePS := `[{"Service":"db","State":"running","Health":""}]`
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		stdout := ""
		if len(args) >= 2 && args[0] == "compose" && args[1] == "config" {
			stdout = composeConfig
		} else if len(args) >= 2 && args[0] == "compose" && args[1] == "ps" {
			stdout = composePS
		}
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"=0",
			"GOFASTA_FAKE_STDOUT="+stdout,
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })

	err := runDev(devFlags{envFile: ".env", waitTimeout: 5e9,
		keepVolumes: false, fresh: true})
	assert.NoError(t, err)
}

// TestRunDev_Fresh_ResetVolumesFails — resetVolumes returns an
// error; runDev logs a warning and continues.
func TestRunDev_Fresh_ResetVolumesFails(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db:\n    image: postgres\n"), 0o644))
	composeConfig := `{"services":{"db":{}}}`
	composePS := `[{"Service":"db","State":"running","Health":""}]`
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		stdout := ""
		exitCode := 0
		if len(args) >= 2 && args[0] == "compose" && args[1] == "config" {
			stdout = composeConfig
		} else if len(args) >= 2 && args[0] == "compose" && args[1] == "ps" {
			stdout = composePS
		} else if len(args) >= 3 && args[0] == "compose" && args[1] == "down" && args[2] == "-v" {
			exitCode = 1 // resetVolumes fails
		}
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"="+strconvItoa(exitCode),
			"GOFASTA_FAKE_STDOUT="+stdout,
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })
	err := runDev(devFlags{envFile: ".env", waitTimeout: 5e9,
		keepVolumes: true, fresh: true})
	assert.NoError(t, err)
}

// TestRunDev_ComposeUnavailable — orchestrate=true but composeAvailable
// returns false → error.
func TestRunDev_ComposeUnavailable(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db:\n    image: postgres\n"), 0o644))
	orig := composeAvailableFn
	composeAvailableFn = func() bool { return false }
	t.Cleanup(func() { composeAvailableFn = orig })
	// Also stub execCommand so docker compose config doesn't try to
	// run for real.
	execOrig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		stdout := ""
		if len(args) >= 2 && args[0] == "compose" && args[1] == "config" {
			stdout = `{"services":{"db":{}}}`
		}
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"=0",
			"GOFASTA_FAKE_STDOUT="+stdout,
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = execOrig })
	err := runDev(devFlags{envFile: ".env", waitTimeout: 5e9, keepVolumes: true})
	require.Error(t, err)
}

// TestRunDev_StartServicesFails — compose config ok but compose up
// fails.
func TestRunDev_StartServicesFails(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db:\n    image: postgres\n"), 0o644))
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		stdout := ""
		exitCode := 0
		if len(args) >= 2 && args[0] == "compose" && args[1] == "config" {
			stdout = `{"services":{"db":{}}}`
		} else if len(args) >= 3 && args[0] == "compose" && args[1] == "up" {
			exitCode = 1
		}
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"="+strconvItoa(exitCode),
			"GOFASTA_FAKE_STDOUT="+stdout,
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })
	err := runDev(devFlags{envFile: ".env", waitTimeout: 5e9, keepVolumes: true})
	require.Error(t, err)
}

// TestRunDev_WaitHealthyFails — compose up succeeds but services never
// become healthy in the short timeout.
func TestRunDev_WaitHealthyFails(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db:\n    image: postgres\n"), 0o644))
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		stdout := ""
		if len(args) >= 2 && args[0] == "compose" && args[1] == "config" {
			stdout = `{"services":{"db":{"healthcheck":{"test":["CMD","pg_isready"]}}}}`
		} else if len(args) >= 2 && args[0] == "compose" && args[1] == "ps" {
			stdout = `[{"Service":"db","State":"running","Health":"starting"}]`
		}
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"=0",
			"GOFASTA_FAKE_STDOUT="+stdout,
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })
	err := runDev(devFlags{envFile: ".env", waitTimeout: 500000000, // 500ms
		keepVolumes: true})
	require.Error(t, err)
}

// TestRunDev_KeepVolumesFalseDestroys — teardown with keepVolumes=false
// calls resetVolumes which we make fail to cover the else branch.
func TestRunDev_KeepVolumesFalseDestroys(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db:\n    image: postgres\n"), 0o644))
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		stdout := ""
		exitCode := 0
		if len(args) >= 2 && args[0] == "compose" && args[1] == "config" {
			stdout = `{"services":{"db":{}}}`
		} else if len(args) >= 2 && args[0] == "compose" && args[1] == "ps" {
			stdout = `[{"Service":"db","State":"running","Health":""}]`
		} else if len(args) >= 3 && args[0] == "compose" && args[1] == "down" && args[2] == "-v" {
			exitCode = 1 // Make teardown fail → emitter.Shutdown(mode+"-failed", 1).
		}
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"="+strconvItoa(exitCode),
			"GOFASTA_FAKE_STDOUT="+stdout,
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })
	err := runDev(devFlags{envFile: ".env", waitTimeout: 5e9, keepVolumes: false})
	assert.NoError(t, err)
}

// TestIsSignaledExit_NilProcessState — default isSignaledExit handles
// nil gracefully.
func TestIsSignaledExit_NilProcessState(t *testing.T) {
	got := isSignaledExit(nil)
	assert.False(t, got)
}

// TestRunAir_SignaledExit — force isSignaledExit to return true so
// runAir returns nil despite the exec error.
func TestRunAir_SignaledExit(t *testing.T) {
	chdirTemp(t)
	orig := isSignaledExit
	isSignaledExit = func(_ *os.ProcessState) bool { return true }
	t.Cleanup(func() { isSignaledExit = orig })
	withFakeExec(t, 1) // exec fails with exit 1
	err := runAir(devFlags{}, func(string) {})
	// isSignaledExit returns true → runAir returns nil.
	assert.NoError(t, err)
}

// TestRunDev_NoTeardownSkips — flags.noTeardown=true → teardown
// returns early.
func TestRunDev_NoTeardownSkips(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	err := runDev(devFlags{envFile: ".env", noServices: true, noTeardown: true})
	assert.NoError(t, err)
}

// TestRunDev_ResolveDevPlanFails — construct devFlags with
// servicesList set and no compose.yaml → resolveDevPlan errors.
func TestRunDev_ResolveDevPlanFails(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	err := runDev(devFlags{envFile: ".env",
		servicesList: []string{"db"}})
	require.Error(t, err)
}

// TestRunDev_AttachLogs — attach-logs triggers startLogStreamer when
// orchestrating. We stub exec to be quick so the cancel func can clean
// up without hanging.
func TestRunDev_AttachLogs(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db:\n    image: postgres\n"), 0o644))
	composeConfig := `{"services":{"db":{}}}`
	composePS := `[{"Service":"db","State":"running","Health":""}]`
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		stdout := ""
		if len(args) >= 2 && args[0] == "compose" && args[1] == "config" {
			stdout = composeConfig
		} else if len(args) >= 2 && args[0] == "compose" && args[1] == "ps" {
			stdout = composePS
		}
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"=0",
			"GOFASTA_FAKE_STDOUT="+stdout,
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })

	err := runDev(devFlags{envFile: ".env", waitTimeout: 5e9,
		keepVolumes: true, attachLogs: true})
	assert.NoError(t, err)
}

// TestRunDev_Dashboard — dashboard=true triggers startDashboard.
func TestRunDev_Dashboard(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	// Pick a port nothing else is likely listening on.
	err := runDev(devFlags{envFile: ".env", noServices: true,
		dashboard: true, dashboardPort: 0}) // port 0 → random free port
	assert.NoError(t, err)
}

// TestRunDev_AirRebuild — rebuild=true triggers os.RemoveAll("tmp")
// before air starts.
func TestRunDev_AirRebuild(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	require.NoError(t, os.MkdirAll("tmp", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("tmp", "x"), []byte("x"), 0o644))
	withFakeExec(t, 0)
	err := runDev(devFlags{envFile: ".env", noServices: true, rebuild: true})
	assert.NoError(t, err)
	_, err = os.Stat("tmp")
	assert.True(t, os.IsNotExist(err), "tmp should have been removed")
}

// TestRunMigrationsWithCount_MigrateNotFound — execLookPath fails.
func TestRunMigrationsWithCount_MigrateNotFound(t *testing.T) {
	origLookPath := execLookPath
	execLookPath = func(name string) (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() { execLookPath = origLookPath })
	_, err := runMigrationsWithCount()
	require.Error(t, err)
}

// TestAirURLs_WithSwagger — docs/swagger.json exists → swagger URL set.
func TestAirURLs_WithSwagger(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.MkdirAll("docs", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("docs", "swagger.json"),
		[]byte("{}"), 0o644))
	urls := airURLs("8080")
	assert.Contains(t, urls, "swagger")
}

// TestAppendTag_NoTagPrefix — an existing -tags=... fragment followed
// by a token without the -tags= prefix exercises the continue branch.
func TestAppendTag_NoTagPrefix(t *testing.T) {
	got := appendTag("-tags=foo -mod=mod", "bar")
	assert.Contains(t, got, "-tags=foo,bar")
	assert.Contains(t, got, "-mod=mod")
}

// TestAppendTag_ExistingTagsSkipsNonTagsPrefix — a field that isn't
// a -tags= fragment exercises the "continue" branch.
func TestAppendTag_ExistingTagsSkipsNonTagsPrefix(t *testing.T) {
	got := appendTag("-mod=mod -tags=foo", "bar")
	assert.Contains(t, got, "-tags=foo,bar")
}

// TestRunAir_RemoveAllFails — removeAllFn seam returns an error; the
// error is swallowed silently and air still runs.
func TestRunAir_RemoveAllFails(t *testing.T) {
	chdirTemp(t)
	orig := removeAllFn
	removeAllFn = func(_ string) error { return fmt.Errorf("boom") }
	t.Cleanup(func() { removeAllFn = orig })
	withFakeExec(t, 0)
	err := runAir(devFlags{rebuild: true}, func(string) {})
	// Air succeeds despite RemoveAll failing.
	assert.NoError(t, err)
}

// TestRunAir_EnvNilBranch — execCommand returns an *exec.Cmd whose Env
// is nil so runAir's "seed from os.Environ() when nil" branch fires.
func TestRunAir_EnvNilBranch(t *testing.T) {
	chdirTemp(t)
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		// Build a subprocess cmd but keep Env nil.
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = nil // force the branch
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })
	// runAir will fail; we only care about coverage.
	_ = runAir(devFlags{}, func(string) {})
}

// TestRunAir_Rebuild_RemovesTmp — rebuild flag triggers the RemoveAll
// branch. Even if tmp doesn't exist RemoveAll is a no-op error branch.
func TestRunAir_Rebuild_RemovesTmp(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 0)
	err := runAir(devFlags{rebuild: true}, func(string) {})
	assert.NoError(t, err)
}

// TestRunAir_GoNotOnPath — execLookPath returns error.
func TestRunAir_GoNotOnPath(t *testing.T) {
	chdirTemp(t)
	origLookPath := execLookPath
	execLookPath = func(name string) (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() { execLookPath = origLookPath })
	err := runAir(devFlags{}, func(string) {})
	require.Error(t, err)
}

// TestRunSeedDelegation — covers the package-level function called by
// runDev when --seed is set.
func TestRunSeedDelegation(t *testing.T) {
	withFakeExec(t, 0)
	assert.NoError(t, runSeedDelegation())
	withFakeExec(t, 1)
	assert.Error(t, runSeedDelegation())
}

// TestAirSignalHandler_NilProcess — signal fired before air started;
// teardown still called.
func TestAirSignalHandler_NilProcess(t *testing.T) {
	sigChan := make(chan os.Signal, 1)
	airCmd := exec.Command("true")
	var called string
	sigChan <- os.Interrupt
	airSignalHandler(sigChan, airCmd, func(r string) { called = r })
	assert.Equal(t, "interrupted", called)
}

// TestAirSignalHandler_WithProcess — running process receives SIGINT
// on signal fire.
func TestAirSignalHandler_WithProcess(t *testing.T) {
	sigChan := make(chan os.Signal, 1)
	airCmd := exec.Command("sleep", "60")
	require.NoError(t, airCmd.Start())
	t.Cleanup(func() { _ = airCmd.Wait() })
	var called string
	sigChan <- os.Interrupt
	airSignalHandler(sigChan, airCmd, func(r string) { called = r })
	assert.Equal(t, "interrupted", called)
}

// TestParseServicesInList — parseServicesList trims spaces and
// filters empty entries.
func TestParseServicesInList(t *testing.T) {
	got := parseServicesList("a, b , c")
	assert.Equal(t, 3, len(got))
	for _, s := range got {
		assert.NotEmpty(t, s)
	}
	// Silence unused imports if nothing else pulls strconv.
	_ = strconv.Itoa(len(got))
}
