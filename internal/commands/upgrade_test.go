package commands

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errReader is an io.ReadCloser that always errors — used to simulate a
// network body that fails partway through io.Copy.
type errReader struct{}

func (errReader) Read(_ []byte) (int, error) { return 0, fmt.Errorf("simulated read error") }
func (errReader) Close() error               { return nil }

// swapHTTP replaces httpGet and restores at cleanup.
func swapHTTP(t *testing.T, fn func(url string) (*http.Response, error)) {
	t.Helper()
	orig := httpGet
	httpGet = fn
	t.Cleanup(func() { httpGet = orig })
}

// swapAPIURL replaces githubAPIURL and restores at cleanup.
func swapAPIURL(t *testing.T, url string) {
	t.Helper()
	orig := githubAPIURL
	githubAPIURL = url
	t.Cleanup(func() { githubAPIURL = orig })
}

// swapDownloadURL replaces githubDownloadURLFmt.
func swapDownloadURL(t *testing.T, fmtStr string) {
	t.Helper()
	orig := githubDownloadURLFmt
	githubDownloadURLFmt = fmtStr
	t.Cleanup(func() { githubDownloadURLFmt = orig })
}

// --- fetchLatestVersion ---

func TestFetchLatestVersion_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name":"v1.2.3"}`))
	}))
	t.Cleanup(srv.Close)
	swapAPIURL(t, srv.URL)

	tag, err := fetchLatestVersion()
	assert.NoError(t, err)
	assert.Equal(t, "v1.2.3", tag)
}

func TestFetchLatestVersion_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	swapAPIURL(t, srv.URL)

	_, err := fetchLatestVersion()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestFetchLatestVersion_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not-json`))
	}))
	t.Cleanup(srv.Close)
	swapAPIURL(t, srv.URL)

	_, err := fetchLatestVersion()
	assert.Error(t, err)
}

func TestFetchLatestVersion_EmptyTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":""}`))
	}))
	t.Cleanup(srv.Close)
	swapAPIURL(t, srv.URL)

	_, err := fetchLatestVersion()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no releases")
}

func TestFetchLatestVersion_HTTPError(t *testing.T) {
	swapHTTP(t, func(url string) (*http.Response, error) {
		return nil, fmt.Errorf("dial failed")
	})
	_, err := fetchLatestVersion()
	assert.Error(t, err)
}

// --- isGoInstall ---

func TestIsGoInstall(t *testing.T) {
	t.Setenv("GOPATH", "/my/go")
	assert.True(t, isGoInstall("/my/go/bin/gofasta"))
	assert.False(t, isGoInstall("/other/bin/gofasta"))
}

func TestIsGoInstall_DefaultGopath(t *testing.T) {
	t.Setenv("GOPATH", "")
	home, _ := os.UserHomeDir()
	assert.True(t, isGoInstall(home+"/go/bin/gofasta"))
}

// --- normalizeVersion ---

func TestNormalizeVersion(t *testing.T) {
	assert.Equal(t, "1.2.3", normalizeVersion("v1.2.3"))
	assert.Equal(t, "1.2.3", normalizeVersion("1.2.3"))
	assert.Equal(t, "", normalizeVersion(""))
	assert.Equal(t, "0.1.3-0.20260411-abcdef", normalizeVersion("v0.1.3-0.20260411-abcdef"))
}

// --- goInstallTargetPath ---

func TestGoInstallTargetPath_GOBIN(t *testing.T) {
	t.Setenv("GOBIN", "/custom/gobin")
	p, err := goInstallTargetPath()
	assert.NoError(t, err)
	assert.Equal(t, "/custom/gobin/gofasta", p)
}

func TestGoInstallTargetPath_GOPATH(t *testing.T) {
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "/my/gopath")
	p, err := goInstallTargetPath()
	assert.NoError(t, err)
	assert.Equal(t, "/my/gopath/bin/gofasta", p)
}

func TestGoInstallTargetPath_DefaultHome(t *testing.T) {
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")
	p, err := goInstallTargetPath()
	assert.NoError(t, err)
	home, _ := os.UserHomeDir()
	assert.Equal(t, filepath.Join(home, "go", "bin", "gofasta"), p)
}

func TestGoInstallTargetPath_HomeError(t *testing.T) {
	// On unix, os.UserHomeDir returns an error when $HOME is empty and there
	// is no passwd fallback. Windows uses %USERPROFILE% instead — skip there.
	if runtime.GOOS == "windows" {
		t.Skip("UserHomeDir error path is unix-specific")
	}
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")
	t.Setenv("HOME", "")
	_, err := goInstallTargetPath()
	assert.Error(t, err)
}

// --- readBinaryVersion ---

func TestReadBinaryVersion_Success(t *testing.T) {
	withFakeExecVersion(t, 0, "v1.2.3")
	v, err := readBinaryVersion("/fake/gofasta")
	assert.NoError(t, err)
	assert.Equal(t, "v1.2.3", v)
}

func TestReadBinaryVersion_ExecError(t *testing.T) {
	withFakeExec(t, 1)
	_, err := readBinaryVersion("/fake/gofasta")
	assert.Error(t, err)
}

// --- upgradeViaGoInstall ---

func TestUpgradeViaGoInstall_Success(t *testing.T) {
	t.Setenv("GOBIN", "/fake/gobin")
	withFakeExecVersion(t, 0, "v2.0.0")
	assert.NoError(t, upgradeViaGoInstall("2.0.0"))
}

func TestUpgradeViaGoInstall_InstallFailure(t *testing.T) {
	withFakeExec(t, 1)
	err := upgradeViaGoInstall("2.0.0")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "go install failed")
}

func TestUpgradeViaGoInstall_VersionMismatch(t *testing.T) {
	t.Setenv("GOBIN", "/fake/gobin")
	withFakeExecVersion(t, 0, "v1.0.0")
	err := upgradeViaGoInstall("2.0.0")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reports version")
}

func TestUpgradeViaGoInstall_TargetPathError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("UserHomeDir error path is unix-specific")
	}
	// Clear every env var goInstallTargetPath consults so it falls through
	// to os.UserHomeDir, which then errors because HOME is empty.
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")
	t.Setenv("HOME", "")
	withFakeExec(t, 0)
	// upgradeViaGoInstall swallows the goInstallTargetPath error and prints
	// a warning rather than returning it.
	assert.NoError(t, upgradeViaGoInstall("2.0.0"))
}

func TestUpgradeViaGoInstall_VerifyReadFails(t *testing.T) {
	t.Setenv("GOBIN", "/fake/gobin")
	// First call (go install) succeeds, second call (--version) also succeeds
	// but without a version env var → readBinaryVersion returns the arg count
	// fallback. Use an empty version so printed output has no trailing token.
	//
	// Simpler: use exit code 0 with no version → helper exits 0 with no output,
	// which makes strings.Fields empty and triggers the parse error path.
	withFakeExec(t, 0)
	// upgradeViaGoInstall swallows the readBinaryVersion error and prints a
	// warning rather than failing. Assert no error.
	assert.NoError(t, upgradeViaGoInstall("2.0.0"))
}

// --- upgradeViaBinary ---

func TestUpgradeViaBinary_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("fake-binary-bytes"))
	}))
	t.Cleanup(srv.Close)

	// redirect download URL format to our server
	swapDownloadURL(t, srv.URL+"/%s/%s")

	dir := t.TempDir()
	execPath := filepath.Join(dir, "gofasta")
	// Seed the target file so the rename has a destination
	require.NoError(t, os.WriteFile(execPath, []byte("old"), 0755))

	err := upgradeViaBinary(execPath, "v1.0.0")
	assert.NoError(t, err)
	content, _ := os.ReadFile(execPath)
	assert.Equal(t, "fake-binary-bytes", string(content))
}

func TestUpgradeViaBinary_HTTPError(t *testing.T) {
	swapHTTP(t, func(url string) (*http.Response, error) {
		return nil, fmt.Errorf("network fail")
	})
	err := upgradeViaBinary("/tmp/nope", "v1.0.0")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "download failed")
}

func TestUpgradeViaBinary_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	swapDownloadURL(t, srv.URL+"/%s/%s")

	err := upgradeViaBinary("/tmp/nope", "v1.0.0")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 404")
}

func TestUpgradeViaBinary_CopyError(t *testing.T) {
	// Return a response whose Body fails on the first read, forcing the
	// io.Copy path inside upgradeViaBinary to error out.
	swapHTTP(t, func(url string) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       errReader{},
			Header:     make(http.Header),
		}, nil
	})
	err := upgradeViaBinary(filepath.Join(t.TempDir(), "gofasta"), "v1.0.0")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "download failed")
}

func TestUpgradeViaBinary_CreateTempError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("TMPDIR is unix-specific")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "bin")
	}))
	t.Cleanup(srv.Close)
	swapDownloadURL(t, srv.URL+"/%s/%s")

	// Capture a working tempdir BEFORE redirecting TMPDIR — t.TempDir itself
	// reads TMPDIR and would fail if we pointed it at a nonexistent path.
	execPath := filepath.Join(t.TempDir(), "gofasta")
	// Now point os.TempDir at a nonexistent path so os.CreateTemp fails.
	t.Setenv("TMPDIR", "/definitely/does/not/exist/gofasta-xyz")
	err := upgradeViaBinary(execPath, "v1.0.0")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "temp file")
}

func TestUpgradeViaBinary_RenameFallback(t *testing.T) {
	// Rename across filesystems can fail; force fallback to replaceViaCopy by
	// pointing destination to a path that is writable but rename would cross
	// a different dir. Rename within the same dir normally succeeds — so force
	// a rename error by making the target path a non-existent parent dir.
	// Instead, force the fallback by pointing execPath to a dir that exists so
	// os.Rename fails with "is a directory".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("newbin"))
	}))
	t.Cleanup(srv.Close)
	swapDownloadURL(t, srv.URL+"/%s/%s")

	dir := t.TempDir()
	// execPath is a nested path that doesn't exist — rename will succeed. Use
	// a different strategy: make execPath target a directory so rename fails.
	targetDir := filepath.Join(dir, "targetdir")
	require.NoError(t, os.MkdirAll(targetDir, 0755))

	// Call upgradeViaBinary with execPath = targetDir (a directory). os.Rename
	// from a file to an existing directory fails, triggering replaceViaCopy
	// which then also fails because os.WriteFile on a directory fails.
	err := upgradeViaBinary(targetDir, "v1.0.0")
	assert.Error(t, err)
}

// --- replaceViaCopy ---

func TestReplaceViaCopy_Success(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	dst := filepath.Join(t.TempDir(), "dst")
	require.NoError(t, os.WriteFile(src, []byte("hello"), 0644))
	assert.NoError(t, replaceViaCopy(src, dst))
	b, _ := os.ReadFile(dst)
	assert.Equal(t, "hello", string(b))
}

func TestReplaceViaCopy_SourceMissing(t *testing.T) {
	err := replaceViaCopy("/nonexistent/src", "/tmp/dst")
	assert.Error(t, err)
}

func TestReplaceViaCopy_DestUnwritable(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	require.NoError(t, os.WriteFile(src, []byte("hi"), 0644))
	// Target a dir that can't be written (on unix, root-owned)
	err := replaceViaCopy(src, "/nonexistent-dir/dst")
	assert.Error(t, err)
}

// --- runUpgrade ---

func TestRunUpgrade_AlreadyUpToDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v9.9.9"}`))
	}))
	t.Cleanup(srv.Close)
	swapAPIURL(t, srv.URL)

	// Set rootCmd.Version to match
	orig := rootCmd.Version
	rootCmd.Version = "9.9.9"
	t.Cleanup(func() { rootCmd.Version = orig })

	assert.NoError(t, runUpgrade())
}

func TestRunUpgrade_FetchError(t *testing.T) {
	swapHTTP(t, func(url string) (*http.Response, error) { return nil, fmt.Errorf("nope") })
	err := runUpgrade()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to check for updates")
}

// Helper to swap osExecutable.
func swapExecutable(t *testing.T, path string, err error) {
	t.Helper()
	orig := osExecutable
	osExecutable = func() (string, error) { return path, err }
	t.Cleanup(func() { osExecutable = orig })
}

func TestRunUpgrade_DispatchGoInstall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v2.0.0"}`))
	}))
	t.Cleanup(srv.Close)
	swapAPIURL(t, srv.URL)
	t.Setenv("GOPATH", "/fake/gopath")
	swapExecutable(t, "/fake/gopath/bin/gofasta", nil)
	withFakeExec(t, 0)

	orig := rootCmd.Version
	rootCmd.Version = "1.0.0"
	t.Cleanup(func() { rootCmd.Version = orig })

	assert.NoError(t, runUpgrade())
}

func TestRunUpgrade_ExecutableError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v2.0.0"}`))
	}))
	t.Cleanup(srv.Close)
	swapAPIURL(t, srv.URL)
	swapExecutable(t, "", fmt.Errorf("no executable"))

	orig := rootCmd.Version
	rootCmd.Version = "1.0.0"
	t.Cleanup(func() { rootCmd.Version = orig })

	err := runUpgrade()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "executable path")
}

func TestRunUpgrade_DispatchBinary(t *testing.T) {
	// Serve the release API
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v2.0.0"}`))
	}))
	t.Cleanup(apiSrv.Close)
	swapAPIURL(t, apiSrv.URL)

	// Force isGoInstall to return false so runUpgrade falls through to the
	// binary-replacement path.
	t.Setenv("GOPATH", "/definitely/not/a/real/path")

	orig := rootCmd.Version
	rootCmd.Version = "1.0.0"
	t.Cleanup(func() { rootCmd.Version = orig })

	// runUpgrade will call upgradeViaBinary with the test binary path as execPath.
	// To avoid actually overwriting the test binary, make the download fail with
	// HTTP 500 — we only need to verify runUpgrade reached the binary branch.
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	t.Cleanup(failSrv.Close)
	swapDownloadURL(t, failSrv.URL+"/%s/%s")

	err := runUpgrade()
	assert.Error(t, err)
	_ = strings.Contains // keep import
}
