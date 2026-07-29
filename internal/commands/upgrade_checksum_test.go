package commands

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Coverage for upgrade.go's checksum verification.
//
// Every branch here ends in a refusal to install. That is the point: the
// binary being verified is about to replace the one the user is running, so
// "could not verify" must never degrade into "install anyway".

// okResponse builds a 200 response carrying body.
func okResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestVerifyDownloadChecksum_FetchFailure(t *testing.T) {
	swapHTTP(t, func(string) (*http.Response, error) {
		return nil, fmt.Errorf("network is down")
	})

	err := verifyDownloadChecksum(writeTempBinary(t, "payload"), "v1.0.0", "gofasta_linux_amd64")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not fetch checksums")
}

func TestVerifyDownloadChecksum_BodyReadFailure(t *testing.T) {
	swapHTTP(t, func(string) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: errReader{}}, nil
	})

	err := verifyDownloadChecksum(writeTempBinary(t, "payload"), "v1.0.0", "gofasta_linux_amd64")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not read checksums.txt body")
}

// TestVerifyDownloadChecksum_EntryMissing covers the parse failure: the
// checksums file downloaded fine but carries no line for this binary, so there
// is nothing to compare against.
func TestVerifyDownloadChecksum_EntryMissing(t *testing.T) {
	swapHTTP(t, func(string) (*http.Response, error) {
		return okResponse("abc123  some_other_binary\n"), nil
	})

	err := verifyDownloadChecksum(writeTempBinary(t, "payload"), "v1.0.0", "gofasta_linux_amd64")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not resolve expected checksum")
}

// TestVerifyDownloadChecksum_HashFailure covers the branch where the
// downloaded file cannot be hashed.
func TestVerifyDownloadChecksum_HashFailure(t *testing.T) {
	swapHTTP(t, func(string) (*http.Response, error) {
		return okResponse("abc123  gofasta_linux_amd64\n"), nil
	})

	err := verifyDownloadChecksum(filepath.Join(t.TempDir(), "not-there"), "v1.0.0", "gofasta_linux_amd64")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not hash the downloaded binary")
}

// --- sha256File ---

// The happy path lives in upgrade_test.go's TestSha256File; only the two
// failure returns are covered here.

func TestSha256File_OpenFailure(t *testing.T) {
	_, err := sha256File(filepath.Join(t.TempDir(), "missing"))
	require.Error(t, err)
}

// TestSha256File_ReadFailure covers the io.Copy error return. Opening a
// directory succeeds; reading from it does not.
func TestSha256File_ReadFailure(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "adir")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	_, err := sha256File(dir)
	require.Error(t, err)
}

// writeTempBinary writes content to a temp file and returns its path.
func writeTempBinary(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gofasta-download")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}
