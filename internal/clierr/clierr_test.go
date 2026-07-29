package clierr

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestError_StringWithoutCause(t *testing.T) {
	e := New(CodeConfigInvalid, "bad value for database.driver")
	got := e.Error()
	want := "bad value for database.driver"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestError_StringWithCause(t *testing.T) {
	cause := errors.New("eof")
	e := Wrap(CodeFileIO, cause, "failed to read config.yaml")
	got := e.Error()
	want := "failed to read config.yaml: eof"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestError_UnwrapReturnsCause(t *testing.T) {
	cause := errors.New("root cause")
	e := Wrap(CodeInternal, cause, "wrapper")
	if !errors.Is(e, cause) {
		t.Error("errors.Is did not traverse through clierr.Error to the cause")
	}
}

func TestError_MarshalJSON(t *testing.T) {
	e := New(CodeDeployHostRequired, "deploy host is required")
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	var got struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Hint    string `json:"hint"`
		Docs    string `json:"docs"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("result JSON did not round-trip: %v", err)
	}
	if got.Code != string(CodeDeployHostRequired) {
		t.Errorf("Code = %q", got.Code)
	}
	if got.Hint == "" {
		t.Error("Hint not present in JSON output")
	}
	if got.Docs == "" {
		t.Error("Docs not present in JSON output")
	}
}

func TestError_MarshalJSONFoldsCauseIntoMessage(t *testing.T) {
	cause := errors.New("permission denied")
	e := Wrap(CodeFileIO, cause, "cannot read config")
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var got struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(b, &got)
	want := "cannot read config: permission denied"
	if got.Message != want {
		t.Errorf("Message = %q, want %q", got.Message, want)
	}
}

func TestAs_ReturnsFalseForNonClierr(t *testing.T) {
	_, ok := As(errors.New("plain"))
	if ok {
		t.Error("As returned true for a plain error")
	}
}

func TestAs_ReturnsTrueForWrapped(t *testing.T) {
	inner := New(CodeConfigInvalid, "bad")
	got, ok := As(inner)
	if !ok || got != inner {
		t.Error("As did not return the inner *Error")
	}
}

func TestNewf_FormatsMessage(t *testing.T) {
	e := Newf(CodeInvalidName, "name %q is not a valid module path", "My App")
	if e.Message != `name "My App" is not a valid module path` {
		t.Errorf("Message = %q", e.Message)
	}
}

// TestError_Nil — nil receiver returns empty string rather than
// panicking. Defensive branch that error-chain traversal relies on.
func TestError_Nil(t *testing.T) {
	var e *Error
	assert.Empty(t, e.Error())
}

// TestError_WithoutCause — a structured error with no wrapped
// cause renders just the message.
func TestError_WithoutCause(t *testing.T) {
	e := New(CodeInternal, "boom")
	assert.Equal(t, "boom", e.Error())
}

// TestError_WithCause — renders "message: cause".
func TestError_WithCause(t *testing.T) {
	e := Wrap(CodeInternal, errors.New("underlying"), "wrapper")
	assert.Equal(t, "wrapper: underlying", e.Error())
}

// TestUnwrap_Nil — nil receiver returns nil.
func TestUnwrap_Nil(t *testing.T) {
	var e *Error
	assert.Nil(t, e.Unwrap())
}

// TestUnwrap_NoCause — structured error without cause → nil.
func TestUnwrap_NoCause(t *testing.T) {
	e := New(CodeInternal, "x")
	assert.Nil(t, e.Unwrap())
}

// TestUnwrap_WithCause — structured error wrapping a sentinel; the
// sentinel is recoverable via errors.Is.
func TestUnwrap_WithCause(t *testing.T) {
	sentinel := errors.New("sentinel")
	e := Wrap(CodeInternal, sentinel, "wrapper")
	assert.True(t, errors.Is(e, sentinel))
}

// TestWrapf_FormatsMessage — Wrapf renders the format arguments into
// the message field.
func TestWrapf_FormatsMessage(t *testing.T) {
	e := Wrapf(CodeInternal, errors.New("c"), "count=%d", 42)
	assert.Equal(t, "count=42: c", e.Error())
}
