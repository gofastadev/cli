package clierr

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Completion coverage for clierr — the Error()/Unwrap() nil
// branches, Wrapf, and the code registry.
// ─────────────────────────────────────────────────────────────────────

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

// TestRegistry_NonEmpty — the code registry enumerates every declared
// code and must include at least the canonical codes.
func TestRegistry_NonEmpty(t *testing.T) {
	require.NotEmpty(t, registry)
	_, foundInternal := registry[CodeInternal]
	_, foundWire := registry[CodeWireMissingProvider]
	assert.True(t, foundInternal, "CodeInternal missing from registry")
	assert.True(t, foundWire, "CodeWireMissingProvider missing from registry")
}
