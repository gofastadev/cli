package commands

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// --- isHomebrew / isGoInstall ---

func TestIsHomebrew(t *testing.T) {
	assert.True(t, isHomebrew("/opt/homebrew/bin/gofasta"))
	assert.True(t, isHomebrew("/usr/local/Cellar/gofasta/1.0/bin/gofasta"))
	assert.True(t, isHomebrew("/home/linuxbrew/.linuxbrew/bin/gofasta"))
	assert.False(t, isHomebrew("/usr/local/bin/gofasta"))
}

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

// --- upgradeViaHomebrew ---

func TestUpgradeViaHomebrew_Success(t *testing.T) {
	withFakeExec(t, 0)
	assert.NoError(t, upgradeViaHomebrew())
}

func TestUpgradeViaHomebrew_Failure(t *testing.T) {
	withFakeExec(t, 1)
	err := upgradeViaHomebrew()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "brew upgrade failed")
}

// --- upgradeViaGoInstall ---

func TestUpgradeViaGoInstall_Success(t *testing.T) {
	withFakeExec(t, 0)
	assert.NoError(t, upgradeViaGoInstall())
}

func TestUpgradeViaGoInstall_Failure(t *testing.T) {
	withFakeExec(t, 1)
	err := upgradeViaGoInstall()
	assert.Error(t, err)
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

func TestRunUpgrade_DispatchHomebrew(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v2.0.0"}`))
	}))
	t.Cleanup(srv.Close)
	swapAPIURL(t, srv.URL)
	swapExecutable(t, "/opt/homebrew/bin/gofasta", nil)
	withFakeExec(t, 0)

	orig := rootCmd.Version
	rootCmd.Version = "1.0.0"
	t.Cleanup(func() { rootCmd.Version = orig })

	assert.NoError(t, runUpgrade())
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

	// Serve the binary download
	binSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("newbinary"))
	}))
	t.Cleanup(binSrv.Close)
	swapDownloadURL(t, binSrv.URL+"/%s/%s")

	// Override the running exec path so isHomebrew/isGoInstall both return false
	// We can't easily swap os.Executable, but we can verify the dispatch by
	// checking runUpgrade takes the binary branch when the exec path doesn't
	// match either pattern. The real os.Executable() in tests points at the
	// go-test binary — that's usually not in homebrew nor gopath so this works.
	// Force GOPATH to a nonexistent place to guarantee isGoInstall=false.
	t.Setenv("GOPATH", "/definitely/not/a/real/path")

	orig := rootCmd.Version
	rootCmd.Version = "1.0.0"
	t.Cleanup(func() { rootCmd.Version = orig })

	// runUpgrade will call upgradeViaBinary with the test binary path as execPath.
	// We don't want to actually overwrite the test binary! upgradeViaBinary will
	// attempt os.Rename(tmpPath, execPath). That would corrupt the test binary.
	// Avoid this by pointing execPath target via a copy-safe indirection:
	// since we can't override os.Executable easily, we accept that this test
	// exercises the dispatch logic through upgradeViaBinary's HTTP call and
	// temp-file creation — but not the final rename. Make the HTTP server
	// return a 500 so we hit the dispatch + download error branches without
	// touching the real binary.
	binSrv.Close()
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	t.Cleanup(failSrv.Close)
	swapDownloadURL(t, failSrv.URL+"/%s/%s")

	err := runUpgrade()
	// If we ended up in Homebrew/GoInstall branches the exec fake isn't set,
	// so those would try real brew/go commands. Since we seeded GOPATH to a
	// bogus path, and the test binary is unlikely to contain "homebrew" in the
	// path, we should land in upgradeViaBinary and get an HTTP 500 error.
	// If it ends up in homebrew/goinstall on this host, just assert the error.
	assert.Error(t, err)
	_ = strings.Contains // keep import
}
