// Package cliout centralizes CLI output formatting so every command
// emits either human-friendly text or agent-friendly JSON based on the
// single --json persistent flag defined on rootCmd.
//
// Callers never call fmt.Println / fmt.Printf directly for structured
// results — they call cliout.Print with a payload value and a text
// renderer, and the package routes to the right sink. This keeps every
// command consistent: text by default, strict JSON under --json, no
// mixed modes.
package cliout

import (
	"encoding/json"
	"io"
	"os"
	"sync/atomic"
)

// jsonMode is toggled by SetJSONMode at startup (before any subcommand
// runs) and read by Print / JSON throughout the CLI. Stored as an int32
// so concurrent subcommands can read it without racing.
var jsonMode atomic.Bool

// SetJSONMode sets whether subsequent output should be JSON-encoded.
// Intended to be called once at process start from the root command's
// persistent flag handler.
func SetJSONMode(enabled bool) {
	jsonMode.Store(enabled)
}

// JSON reports whether the CLI is currently emitting JSON output.
func JSON() bool {
	return jsonMode.Load()
}

// Print writes a structured payload to stdout. In JSON mode the payload
// is marshaled and written as a single line. In text mode the supplied
// textFn renders a human-friendly representation to the same writer.
//
// Callers should NOT assume the text and JSON modes produce the same
// bytes — the text representation is optimized for readability; the
// JSON representation is the stable machine contract.
func Print(payload any, textFn func(w io.Writer)) {
	if JSON() {
		writeJSON(os.Stdout, payload)
		return
	}
	if textFn != nil {
		textFn(os.Stdout)
	}
}

// PrintJSON always writes payload as JSON to stdout, regardless of mode.
// Use this for subcommands that have their own --json flag with more
// specific semantics than the global one.
func PrintJSON(payload any) {
	writeJSON(os.Stdout, payload)
}

// PrintError writes an error payload to stderr. Used by the root command
// when a subcommand returns an error — in JSON mode the error is
// serialized via its MarshalJSON method (clierr.Error implements this);
// in text mode the err.Error() string is written.
func PrintError(err error) {
	if err == nil {
		return
	}
	if JSON() {
		writeJSON(os.Stderr, err)
		return
	}
	_, _ = os.Stderr.WriteString(err.Error())
	_, _ = os.Stderr.WriteString("\n")
}

// writeJSON encodes payload as a single-line JSON document followed by
// a newline. Write errors are swallowed — stdout / stderr going away
// mid-command is not actionable.
func writeJSON(w io.Writer, payload any) {
	enc := json.NewEncoder(w)
	// One-line-per-result is the shell-friendly convention (agents can
	// pipe through `jq -c` or parse line-by-line). Indented output is
	// available via PrintJSONIndented for humans.
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}

// PrintJSONIndented is the same as PrintJSON but with two-space
// indentation. Useful for commands whose output a human is likely to
// inspect directly (e.g., `gofasta inspect User --json`).
func PrintJSONIndented(payload any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(payload)
}
