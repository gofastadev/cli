package cliout

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// capture redirects os.Stdout and os.Stderr to in-memory buffers for the
// duration of fn, returning the captured bytes. Each test does its own
// capture so cases stay independent.
func capture(t *testing.T, fn func()) (stdout, stderr []byte) {
	t.Helper()
	origOut, origErr := os.Stdout, os.Stderr
	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	os.Stdout = outW
	os.Stderr = errW
	defer func() {
		os.Stdout = origOut
		os.Stderr = origErr
	}()

	fn()
	_ = outW.Close()
	_ = errW.Close()

	var outBuf, errBuf bytes.Buffer
	_, _ = io.Copy(&outBuf, outR)
	_, _ = io.Copy(&errBuf, errR)
	return outBuf.Bytes(), errBuf.Bytes()
}

func TestPrint_TextModeCallsTextFn(t *testing.T) {
	SetJSONMode(false)
	stdout, _ := capture(t, func() {
		Print(nil, func(w io.Writer) {
			_, _ = io.WriteString(w, "hello human")
		})
	})
	if string(stdout) != "hello human" {
		t.Errorf("stdout = %q, want %q", stdout, "hello human")
	}
}

func TestPrint_JSONModeWritesJSON(t *testing.T) {
	SetJSONMode(true)
	t.Cleanup(func() { SetJSONMode(false) })

	payload := map[string]string{"hello": "agent"}
	stdout, _ := capture(t, func() {
		Print(payload, func(w io.Writer) {
			_, _ = io.WriteString(w, "should not appear")
		})
	})
	var got map[string]string
	if err := json.Unmarshal(stdout, &got); err != nil {
		t.Fatalf("stdout did not parse as JSON: %v\n%s", err, stdout)
	}
	if got["hello"] != "agent" {
		t.Errorf("payload round-trip lost content: %+v", got)
	}
}

func TestPrintError_TextMode(t *testing.T) {
	SetJSONMode(false)
	_, stderr := capture(t, func() {
		PrintError(errors.New("oops"))
	})
	if string(stderr) != "oops\n" {
		t.Errorf("stderr = %q", stderr)
	}
}

// errorWithMarshalJSON mimics clierr.Error's JSON shape so we can verify
// PrintError invokes MarshalJSON in JSON mode.
type errorWithMarshalJSON struct {
	Code string
	Msg  string
}

func (e *errorWithMarshalJSON) Error() string { return e.Msg }

func (e *errorWithMarshalJSON) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"code": e.Code, "message": e.Msg})
}

func TestPrintError_JSONModeUsesMarshalJSON(t *testing.T) {
	SetJSONMode(true)
	t.Cleanup(func() { SetJSONMode(false) })

	e := &errorWithMarshalJSON{Code: "FOO", Msg: "something bad"}
	_, stderr := capture(t, func() {
		PrintError(e)
	})
	var got map[string]string
	if err := json.Unmarshal(stderr, &got); err != nil {
		t.Fatalf("stderr not JSON: %v\n%s", err, stderr)
	}
	if got["code"] != "FOO" || got["message"] != "something bad" {
		t.Errorf("JSON lost structure: %+v", got)
	}
}

func TestPrintError_NilIsNoop(t *testing.T) {
	SetJSONMode(false)
	_, stderr := capture(t, func() { PrintError(nil) })
	if len(stderr) != 0 {
		t.Errorf("expected no output for nil error, got %q", stderr)
	}
}

func TestJSONFlag_DefaultIsFalse(t *testing.T) {
	SetJSONMode(false)
	if JSON() {
		t.Error("JSON() should be false after SetJSONMode(false)")
	}
}

func TestJSONFlag_Toggles(t *testing.T) {
	SetJSONMode(true)
	t.Cleanup(func() { SetJSONMode(false) })
	if !JSON() {
		t.Error("JSON() should be true after SetJSONMode(true)")
	}
}

// TestStep_JSONModeWritesToStderr — the regression driver for the
// "--json must keep stdout clean" promise. Step (and every progress
// helper) routes to stderr in JSON mode so an agent piping stdout to
// jq doesn't see human-readable chatter mixed with the JSON document.
func TestStep_JSONModeWritesToStderr(t *testing.T) {
	withJSONMode(t)
	out, errOut := withStdouterr(t, func() { Step("hello %s", "world") })
	assert.Empty(t, out, "JSON mode must not touch stdout")
	assert.Contains(t, errOut, "hello world")
}

// withJSONMode flips cliout into JSON mode and restores on cleanup.
func withJSONMode(t *testing.T) {
	t.Helper()
	SetJSONMode(true)
	t.Cleanup(func() { SetJSONMode(false) })
}
