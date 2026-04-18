// Package clierr defines the structured error type emitted by the CLI at
// the command boundary. Every returning error wrapped with clierr.New or
// clierr.Wrap carries a stable machine-readable code, a human-readable
// message, a remediation hint, and a documentation URL — so AI agents and
// CI systems can act on failures programmatically instead of regex-parsing
// English prose.
//
// The Error type satisfies the standard error interface, so it composes
// with errors.Is / errors.As and wrapping helpers unchanged. When the root
// command runs with --json the Execute handler renders the error via its
// MarshalJSON method; otherwise the Error() string (message plus optional
// cause) is written to stderr, identical to the pre-clierr behavior.
package clierr

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Error is a structured CLI error. Construct with New / Wrap / From — the
// helpers look up Hint and Docs from the code registry so callers don't
// have to repeat remediation text at every call site.
type Error struct {
	// Code is a stable machine-readable identifier. Agents and integrations
	// rely on codes for programmatic handling, so once shipped a code must
	// not be renamed — only deprecated.
	Code string `json:"code"`

	// Message is a one-line human-readable summary of what failed. Follows
	// staticcheck ST1005: lowercase, no trailing punctuation.
	Message string `json:"message"`

	// Hint is a short sentence describing how to recover. Looked up from
	// the registry at construction time.
	Hint string `json:"hint,omitempty"`

	// Docs is a URL to the most relevant documentation page.
	Docs string `json:"docs,omitempty"`

	// Cause holds the underlying error so errors.Unwrap / errors.Is / errors.As
	// work across the structured-error boundary. Not serialized directly —
	// its text is folded into Message during JSON rendering via MarshalJSON.
	Cause error `json:"-"`
}

// Error returns a human-readable rendering: "message" by itself, or
// "message: cause" when a cause is set. Kept short so chained errors do
// not accumulate redundant prose.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

// Unwrap exposes the wrapped cause so errors.Is / errors.As traverse
// through the structured layer.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// MarshalJSON serializes the error as {code, message, hint, docs}. The
// message includes the cause's text if one is set, so consumers reading
// the JSON do not need a separate "cause" field.
func (e *Error) MarshalJSON() ([]byte, error) {
	type alias struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Hint    string `json:"hint,omitempty"`
		Docs    string `json:"docs,omitempty"`
	}
	a := alias{
		Code:    e.Code,
		Message: e.Error(),
		Hint:    e.Hint,
		Docs:    e.Docs,
	}
	return json.Marshal(a)
}

// New constructs a new structured error for code with message. Hint and
// Docs are looked up from the registry; callers that need to override
// either can set the field after construction.
func New(code Code, message string) *Error {
	meta := lookup(code)
	return &Error{
		Code:    string(code),
		Message: message,
		Hint:    meta.Hint,
		Docs:    meta.Docs,
	}
}

// Newf is New with a format string for the message.
func Newf(code Code, format string, args ...any) *Error {
	return New(code, fmt.Sprintf(format, args...))
}

// Wrap wraps cause with a structured error carrying code and message.
// Use this at every `return err` site where a structured code adds
// context agents or CI can act on.
func Wrap(code Code, cause error, message string) *Error {
	e := New(code, message)
	e.Cause = cause
	return e
}

// Wrapf is Wrap with a format string for the message.
func Wrapf(code Code, cause error, format string, args ...any) *Error {
	return Wrap(code, cause, fmt.Sprintf(format, args...))
}

// From returns err as a *Error when it already is one (pass-through) or
// wraps it with code and the err's own text otherwise. Intended for use
// at command boundaries that receive an arbitrary error from a helper.
func From(code Code, err error) *Error {
	if err == nil {
		return nil
	}
	var structured *Error
	if errors.As(err, &structured) {
		return structured
	}
	return Wrap(code, err, err.Error())
}

// As is a convenience wrapper around errors.As for *clierr.Error so
// callers can unwrap without importing the errors package just for
// the assertion.
func As(err error) (*Error, bool) {
	var structured *Error
	if errors.As(err, &structured) {
		return structured, true
	}
	return nil, false
}
