package ai

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestJoinShort covers both inline and "(+N more)" overflow branches.
func TestJoinShort(t *testing.T) {
	assert.Equal(t, "", joinShort(nil))
	assert.Equal(t, "a", joinShort([]string{"a"}))
	assert.Equal(t, "a, b, c, d", joinShort([]string{"a", "b", "c", "d"}))
	assert.Equal(t, "a, b, c, d (+2 more)",
		joinShort([]string{"a", "b", "c", "d", "e", "f"}))
}

// TestExtractModulePath — parses `module ...` lines out of go.mod text.
func TestExtractModulePath(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"simple", "module myapp\n\ngo 1.25.0\n", "myapp"},
		{"namespaced", "module github.com/acme/myapp\n\ngo 1.25.0\n", "github.com/acme/myapp"},
		{"leading whitespace", "\nmodule  example.com/x\ngo 1.25\n", "example.com/x"},
		{"missing", "go 1.25.0\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, extractModulePath(tc.in))
		})
	}
}

func TestModuleName(t *testing.T) {
	assert.Equal(t, "myapp", moduleName("myapp"))
	assert.Equal(t, "myapp", moduleName("github.com/acme/myapp"))
	assert.Equal(t, "", moduleName(""))
}

// TestCmdRunE_NoArgs — Cmd.RunE with zero args delegates to cmd.Help().
func TestCmdRunE_NoArgs(t *testing.T) {
	Cmd.SetOut(os.Stderr)
	Cmd.SetErr(os.Stderr)
	require.NoError(t, Cmd.RunE(Cmd, nil))
}

// TestCmdRunE_WithArg — Cmd.RunE with an unknown agent name returns
// the UNKNOWN_AGENT clierr via runInstall.
func TestCmdRunE_WithArg(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
	err := Cmd.RunE(Cmd, []string{"nonexistent-agent"})
	require.Error(t, err)
}

// TestListCmdRunE — listCmd.RunE delegates to runList.
func TestListCmdRunE(t *testing.T) {
	_ = captureStdout(t, func() {
		require.NoError(t, listCmd.RunE(listCmd, nil))
	})
}

// TestStatusCmdRunE — statusCmd.RunE delegates to runStatus; outside
// a Go module it returns an error.
func TestStatusCmdRunE(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
	err := statusCmd.RunE(statusCmd, nil)
	require.Error(t, err)
}

// TestAgentConflictError_DiffListsRemoveAndAdd — install a previous
// agent, attempt to install another without --switch. The conflict
// error must list the files the previous agent will remove and the
// files the new agent will add.
func TestAgentConflictError_DiffListsRemoveAndAdd(t *testing.T) {
	scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})
	t.Cleanup(func() { installSwitch = false })

	err := runInstall("codex", false, false)
	require.Error(t, err)
	b, _ := json.Marshal(err)
	assert.Contains(t, string(b), "AI_AGENT_CONFLICT")
	assert.Contains(t, err.Error(), "remove")
	assert.Contains(t, err.Error(), "add")
}

// TestAgentConflictError_PrevUnknownAgent — manifest references an
// agent the running CLI doesn't know about. agentConflictError should
// still produce a useful error using the recorded key as the name.
func TestAgentConflictError_PrevUnknownAgent(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	m, err := LoadManifest(dir)
	require.NoError(t, err)
	m.ActiveAgent = "legacyx"
	require.NoError(t, m.Save(dir))
	t.Cleanup(func() { installSwitch = false })

	err = runInstall("claude", false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "legacyx is currently installed")
}

// TestAgentConflictError_NoDiff — prev is an unknown agent (no remove
// diff), target has no templates (synthetic Agent pointing at a
// nonexistent template dir). Diff stays empty and we hit the fallback
// message that includes the `--switch` hint.
func TestAgentConflictError_NoDiff(t *testing.T) {
	m := &Manifest{ActiveAgent: "legacyx", Installed: map[string]InstallRecord{}}
	target := &Agent{Key: "synthetic", Name: "Synthetic", TemplateDir: "templates/nonexistent"}
	err := agentConflictError(m, target)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Re-run with `--switch`")
	assert.Contains(t, err.Error(), "Synthetic")
}

// TestRunInstall_SwitchPrevAgentUnknown — manifest references an
// unknown agent under --switch. switchUninstall clears ActiveAgent
// and returns nil so the new install proceeds.
func TestRunInstall_SwitchPrevAgentUnknown(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	m, err := LoadManifest(dir)
	require.NoError(t, err)
	m.ActiveAgent = "legacyx"
	require.NoError(t, m.Save(dir))

	installSwitch = true
	t.Cleanup(func() { installSwitch = false })

	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("codex", false, false))
	})
	m2, err := LoadManifest(dir)
	require.NoError(t, err)
	assert.Equal(t, "codex", m2.ActiveAgent)
}

// TestRunInstall_SwitchPrevAgentNoRecord — ActiveAgent points to a
// known agent with NO install record (manifest got out of sync).
// switchUninstall clears ActiveAgent and returns nil.
func TestRunInstall_SwitchPrevAgentNoRecord(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	m, err := LoadManifest(dir)
	require.NoError(t, err)
	m.ActiveAgent = "aider"
	require.NoError(t, m.Save(dir))

	installSwitch = true
	t.Cleanup(func() { installSwitch = false })

	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("codex", false, false))
	})
}

// TestRunInstall_SwitchDryRunDoesNotUpdateManifest — --switch +
// --dry-run runs the uninstall in dry-run mode AND skips
// RecordUninstall. The original agent is still recorded after the
// dry-run switch attempt.
func TestRunInstall_SwitchDryRunDoesNotUpdateManifest(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("codex", false, false))
	})
	mBefore, err := LoadManifest(dir)
	require.NoError(t, err)
	require.Contains(t, mBefore.Installed, "codex")

	installSwitch = true
	t.Cleanup(func() { installSwitch = false })
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", true, false))
	})

	mAfter, err := LoadManifest(dir)
	require.NoError(t, err)
	assert.Contains(t, mAfter.Installed, "codex")
	assert.Equal(t, "codex", mAfter.ActiveAgent)
}

// TestSwitchUninstall_BuildInstallDataError — call switchUninstall
// directly so buildInstallData fails specifically inside it.
func TestSwitchUninstall_BuildInstallDataError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod read denial")
	}
	dir := scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("codex", false, false))
	})
	m, err := LoadManifest(dir)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(filepath.Join(dir, "go.mod"), 0o000))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "go.mod"), 0o644) })

	err = switchUninstall(m, dir, true)
	require.Error(t, err)
}

// TestSwitchUninstall_UninstallError — make Uninstall fail inside
// switchUninstall by chmod'ing a parent dir of a recorded file so
// os.Remove returns EACCES. Hits the `err != nil` branch of the
// Uninstall call.
func TestSwitchUninstall_UninstallError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod denial")
	}
	dir := scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})
	m, err := LoadManifest(dir)
	require.NoError(t, err)

	// Make .claude/commands read-only so os.Remove on a file in it fails.
	cmds := filepath.Join(dir, ".claude", "commands")
	require.NoError(t, os.Chmod(cmds, 0o555))
	t.Cleanup(func() { _ = os.Chmod(cmds, 0o755) })

	err = switchUninstall(m, dir, false)
	require.Error(t, err)
}

// TestRunInstall_SwitchUninstallError — runInstall with --switch
// where switchUninstall fails: chmod a parent dir read-only so the
// inner Uninstall errors. Surfaces the line `if err := switchUninstall(...)
// ... return err` branch inside runInstall.
func TestRunInstall_SwitchUninstallError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod denial")
	}
	dir := scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})

	cmds := filepath.Join(dir, ".claude", "commands")
	require.NoError(t, os.Chmod(cmds, 0o555))
	t.Cleanup(func() { _ = os.Chmod(cmds, 0o755) })

	installSwitch = true
	t.Cleanup(func() { installSwitch = false })

	err := runInstall("aider", false, false)
	require.Error(t, err)
}

// TestAgentOwnedFiles_HappyPath — non-empty slice for an agent with
// templates.
func TestAgentOwnedFiles_HappyPath(t *testing.T) {
	agent := AgentByKey("claude")
	require.NotNil(t, agent)
	files, err := agentOwnedFiles(agent)
	require.NoError(t, err)
	assert.NotEmpty(t, files)
}

// TestAgentOwnedFiles_EmptyForAgentWithoutTemplates — a synthetic
// agent pointing at a nonexistent template dir returns an empty slice
// and no error. Every shipping agent now has templates; this branch
// stays exercised for safety because future agents may register before
// their template tree is authored.
func TestAgentOwnedFiles_EmptyForAgentWithoutTemplates(t *testing.T) {
	agent := &Agent{Key: "synthetic", TemplateDir: "templates/nonexistent"}
	files, err := agentOwnedFiles(agent)
	require.NoError(t, err)
	assert.Empty(t, files)
}

// TestAiCmd_NoArgsShowsHelp — `gofasta ai` with no agent argument
// prints help and exits 0 instead of erroring.
func TestAiCmd_NoArgsShowsHelp(t *testing.T) {
	_ = captureStdout(t, func() {
		require.NoError(t, Cmd.RunE(Cmd, nil))
	})
}

// TestAgentOwnedFiles_WalkErrorPropagates — fsWalkDir failure
// surfaces through agentOwnedFiles as a CodeAIInstallFailed clierr.
func TestAgentOwnedFiles_WalkErrorPropagates(t *testing.T) {
	orig := fsWalkDir
	fsWalkDir = func(_ fs.FS, _ string, _ fs.WalkDirFunc) error {
		return assertError("synthetic walk failure")
	}
	t.Cleanup(func() { fsWalkDir = orig })

	_, err := agentOwnedFiles(AgentByKey("claude"))
	require.Error(t, err)
}

// TestRunInstall_AgentOwnedFilesError — fsWalkDir failure: the
// agentOwnedFiles call inside runInstall returns an error after the
// install succeeded, so runInstall propagates it before saving the
// manifest.
func TestRunInstall_AgentOwnedFilesError(t *testing.T) {
	scaffoldFakeProject(t, "example.com/app")

	// Install succeeds, but the post-install ownedFiles lookup fails.
	// To trigger this ordering: let TemplateFiles succeed once (for
	// Install's own use) and fail on the second call (agentOwnedFiles).
	orig := fsWalkDir
	calls := 0
	fsWalkDir = func(fsys fs.FS, root string, fn fs.WalkDirFunc) error {
		calls++
		if calls >= 2 {
			return assertError("synthetic walk failure on second call")
		}
		return fs.WalkDir(fsys, root, fn)
	}
	t.Cleanup(func() { fsWalkDir = orig })

	_ = captureStdout(t, func() {
		err := runInstall("claude", false, false)
		require.Error(t, err)
	})
}

// TestPrintNextSteps_MentionsNewFamilies — calls printNextSteps for
// each agent and asserts the output mentions the new families we
// added (hooks for claude/codex, slash/workflow counts for
// claude/cursor/windsurf). Catches future drift between the templates
// and the post-install help text.
func TestPrintNextSteps_MentionsNewFamilies(t *testing.T) {
	cases := []struct {
		agent string
		wants []string
	}{
		{"claude", []string{"/status", "/g-method", "/seed-memory", "Hooks", "jq"}},
		{"cursor", []string{"/status", "/g-method", ".cursor/commands", "Hooks", "afterFileEdit"}},
		{"codex", []string{"Hooks", ".codex/hooks", "/hooks", "prompts", "symlink"}},
		{"windsurf", []string{"/status", "/g-method", ".windsurf/workflows", "Hooks", "post_write_code"}},
	}
	for _, tc := range cases {
		t.Run(tc.agent, func(t *testing.T) {
			agent := AgentByKey(tc.agent)
			require.NotNil(t, agent)
			var buf bytes.Buffer
			printNextSteps(&buf, agent)
			out := buf.String()
			for _, want := range tc.wants {
				assert.Contains(t, out, want,
					"printNextSteps(%s) should mention %q", tc.agent, want)
			}
		})
	}
}

func TestFindProjectRoot_AtRoot(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	got, err := findProjectRoot()
	require.NoError(t, err)
	// Resolve both paths to handle macOS /var/private symlink quirks.
	gotResolved, _ := filepath.EvalSymlinks(got)
	wantResolved, _ := filepath.EvalSymlinks(dir)
	assert.Equal(t, wantResolved, gotResolved)
}

// TestFindProjectRoot_WalksUp — starting from a subdirectory still
// finds the go.mod above.
func TestFindProjectRoot_WalksUp(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	sub := filepath.Join(dir, "app", "models")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	require.NoError(t, os.Chdir(sub))
	got, err := findProjectRoot()
	require.NoError(t, err)
	gotResolved, _ := filepath.EvalSymlinks(got)
	wantResolved, _ := filepath.EvalSymlinks(dir)
	assert.Equal(t, wantResolved, gotResolved)
}

// TestFindProjectRoot_NotInsideModule — no go.mod anywhere →
// CodeNotGofastaProject.
func TestFindProjectRoot_NotInsideModule(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
	_, err := findProjectRoot()
	require.Error(t, err)
	b, _ := json.Marshal(err)
	assert.Contains(t, string(b), "NOT_GOFASTA_PROJECT")
}

func TestBuildInstallData_HappyPath(t *testing.T) {
	dir := scaffoldFakeProject(t, "github.com/acme/myapp")
	data, err := buildInstallData(dir)
	require.NoError(t, err)
	assert.Equal(t, "github.com/acme/myapp", data.ModulePath)
	assert.Equal(t, "myapp", data.ProjectName)
	assert.Equal(t, "myapp", data.ProjectNameLower)
	assert.Equal(t, "MYAPP", data.ProjectNameUpper)
	// Default when no version resolver is registered.
	assert.Equal(t, "dev", data.CLIVersion)
}

func TestBuildInstallData_VersionResolver(t *testing.T) {
	dir := scaffoldFakeProject(t, "github.com/acme/myapp")
	SetVersionResolver(func() string { return "v1.2.3" })
	t.Cleanup(func() { SetVersionResolver(func() string { return "" }) })
	data, err := buildInstallData(dir)
	require.NoError(t, err)
	assert.Equal(t, "v1.2.3", data.CLIVersion)
}

func TestBuildInstallData_MissingGoMod(t *testing.T) {
	dir := t.TempDir()
	_, err := buildInstallData(dir)
	require.Error(t, err)
}

// TestSetVersionResolver_NilKeepsCurrent — passing nil must not wipe
// the existing resolver (defensive against mistaken init order).
func TestSetVersionResolver_NilKeepsCurrent(t *testing.T) {
	SetVersionResolver(func() string { return "stable" })
	SetVersionResolver(nil)
	t.Cleanup(func() { SetVersionResolver(func() string { return "" }) })
	assert.Equal(t, "stable", rootCmdVersion())
}

func TestRunList_WritesTable(t *testing.T) {
	// runList emits via cliout.Print → os.Stdout. Verify by swapping
	// stdout to a pipe for the duration of the call.
	out := captureStdout(t, func() {
		require.NoError(t, runList())
	})
	assert.Contains(t, out, "KEY")
	for _, a := range Agents {
		assert.Contains(t, out, a.Key)
	}
}

func TestRunStatus_EmptyProject(t *testing.T) {
	scaffoldFakeProject(t, "example.com/app")
	out := captureStdout(t, func() {
		require.NoError(t, runStatus())
	})
	assert.Contains(t, out, "No AI agents installed")
}

func TestRunStatus_WithInstalledManifest(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	m, err := LoadManifest(dir)
	require.NoError(t, err)
	m.RecordInstall("claude", "v1.0.0", []string{".claude/settings.json"})
	require.NoError(t, m.Save(dir))

	out := captureStdout(t, func() {
		require.NoError(t, runStatus())
	})
	assert.Contains(t, out, "claude")
	assert.Contains(t, out, "v1.0.0")
}

func TestRunInstall_UnknownAgent(t *testing.T) {
	scaffoldFakeProject(t, "example.com/app")
	err := runInstall("nonexistent", false, false)
	require.Error(t, err)
	b, _ := json.Marshal(err)
	assert.Contains(t, string(b), "UNKNOWN_AGENT")
}

func TestRunInstall_DryRunDoesNotWriteFiles(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	// Capture stdout so the result table doesn't pollute test output.
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", true, false))
	})
	// In dry-run mode the .claude directory should NOT exist.
	_, err := os.Stat(filepath.Join(dir, ".claude"))
	assert.True(t, os.IsNotExist(err), "claude dir should not exist after dry-run")
	// The manifest should also not be updated.
	m, _ := LoadManifest(dir)
	assert.Empty(t, m.Installed)
}

func TestRunInstall_RealRunCreatesFiles(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})
	// Claude templates render into .claude/.
	_, err := os.Stat(filepath.Join(dir, ".claude"))
	require.NoError(t, err)
	// Manifest recorded the install.
	m, _ := LoadManifest(dir)
	assert.Contains(t, m.Installed, "claude")
}

func TestRunInstall_IdempotentSecondRun(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})
	// Second run should succeed without --force — every file is
	// byte-identical so Install records them as Skipped.
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})
	_ = dir
}

func TestPrintNextSteps_EachAgent(t *testing.T) {
	for _, a := range Agents {
		t.Run(a.Key, func(t *testing.T) {
			var buf bytes.Buffer
			printNextSteps(&buf, &a)
			assert.Contains(t, buf.String(), "Next steps")
		})
	}
}

// TestRunInstall_FindProjectRootError — outside any Go module,
// runInstall returns the error from findProjectRoot without trying
// to install.
func TestRunInstall_FindProjectRootError(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
	// t.TempDir is under /var which has no go.mod.
	err := runInstall("claude", false, false)
	require.Error(t, err)
}

// TestRunInstall_ManifestSaveError — after successful install+load,
// Save fails because .gofasta is read-only.
func TestRunInstall_ManifestSaveError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod denial")
	}
	dir := scaffoldFakeProject(t, "example.com/app")
	gofastaDir := filepath.Join(dir, ".gofasta")
	require.NoError(t, os.MkdirAll(gofastaDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(gofastaDir, 0o755) })
	_ = captureStdout(t, func() {
		err := runInstall("claude", false, false)
		require.Error(t, err)
	})
}

// TestRunInstall_BuildInstallDataError — unreadable go.mod causes
// buildInstallData to fail after findProjectRoot succeeded.
func TestRunInstall_BuildInstallDataError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod read denial")
	}
	dir := scaffoldFakeProject(t, "example.com/app")
	require.NoError(t, os.Chmod(filepath.Join(dir, "go.mod"), 0o000))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "go.mod"), 0o644) })
	err := runInstall("claude", false, false)
	require.Error(t, err)
}

// TestRunStatus_FindProjectRootError — runStatus outside a Go module.
func TestRunStatus_FindProjectRootError(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
	err := runStatus()
	require.Error(t, err)
}

// TestFindProjectRoot_GetwdError — forces the os.Getwd branch via
// the getwd seam.
func TestFindProjectRoot_GetwdError(t *testing.T) {
	orig := getwd
	getwd = func() (string, error) { return "", assertError("boom") }
	t.Cleanup(func() { getwd = orig })
	_, err := findProjectRoot()
	require.Error(t, err)
}

// TestRunInstall_InstallError — a conflicting destination file with
// differing content triggers Install to return an error, which
// runInstall propagates.
func TestRunInstall_InstallError(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	agent := AgentByKey("claude")
	require.NotNil(t, agent)
	files, err := TemplateFiles(agent)
	require.NoError(t, err)
	// Pre-populate the first destination with conflicting bytes.
	dst := filepath.Join(dir, files[0].DestPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(dst, []byte("conflict"), 0o644))
	err = runInstall("claude", false, false)
	require.Error(t, err)
}

// TestRunInstall_BlocksWhenOtherAgentActive — install codex first,
// then attempt to install claude without --switch. Must error with
// CodeAIAgentConflict and the error body should describe the diff.
func TestRunInstall_BlocksWhenOtherAgentActive(t *testing.T) {
	scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("codex", false, false))
	})
	t.Cleanup(func() { installSwitch = false })
	err := runInstall("claude", false, false)
	require.Error(t, err)
	b, _ := json.Marshal(err)
	assert.Contains(t, string(b), "AI_AGENT_CONFLICT")
	assert.Contains(t, err.Error(), "currently installed")
	assert.Contains(t, err.Error(), "--switch")
}

// TestRunInstall_SwitchReplacesActiveAgent — install aider, then
// install claude with --switch. Verify the swap: aider files all gone,
// claude files present, manifest.ActiveAgent == "claude".
func TestRunInstall_SwitchReplacesActiveAgent(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")

	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("aider", false, false))
	})
	// Aider installed: .aider.conf.yml exists.
	_, err := os.Stat(filepath.Join(dir, ".aider.conf.yml"))
	require.NoError(t, err)

	// Now switch to claude.
	installSwitch = true
	t.Cleanup(func() { installSwitch = false })
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})

	// Aider files removed; claude files installed.
	_, err = os.Stat(filepath.Join(dir, ".aider.conf.yml"))
	assert.True(t, os.IsNotExist(err), ".aider.conf.yml should be removed")
	_, err = os.Stat(filepath.Join(dir, ".claude", "settings.json"))
	require.NoError(t, err)

	m, err := LoadManifest(dir)
	require.NoError(t, err)
	assert.Equal(t, "claude", m.ActiveAgent)
	_, present := m.Installed["aider"]
	assert.False(t, present, "aider should be removed from manifest after switch")
}

// TestRunInstall_SameAgentReinstall — re-running the same agent does
// NOT trigger the conflict guard (it's idempotent).
func TestRunInstall_SameAgentReinstall(t *testing.T) {
	scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("codex", false, false))
	})
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("codex", false, false))
	})
}

// TestRunUninstall_FindProjectRootError — outside any Go module.
func TestRunUninstall_FindProjectRootError(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
	err := runUninstall("claude", false)
	require.Error(t, err)
}

// TestRunUninstall_BuildInstallDataError — go.mod unreadable so the
// inner buildInstallData call inside runUninstall fails.
func TestRunUninstall_BuildInstallDataError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod read denial")
	}
	dir := scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})
	require.NoError(t, os.Chmod(filepath.Join(dir, "go.mod"), 0o000))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "go.mod"), 0o644) })

	err := runUninstall("claude", false)
	require.Error(t, err)
}
