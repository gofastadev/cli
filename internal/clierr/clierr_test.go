package clierr

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestNew_PopulatesHintAndDocsFromRegistry(t *testing.T) {
	e := New(CodeWireMissingProvider, "undefined: NewThingProvider")
	if e.Code != string(CodeWireMissingProvider) {
		t.Errorf("Code = %q, want %q", e.Code, CodeWireMissingProvider)
	}
	if e.Hint == "" {
		t.Error("Hint is empty — registry lookup did not populate it")
	}
	if e.Docs == "" {
		t.Error("Docs is empty — registry lookup did not populate it")
	}
}

func TestNew_UnknownCodeStillUsable(t *testing.T) {
	// Unregistered codes must not panic; they simply produce an error
	// without a hint or docs URL.
	e := New(Code("UNREGISTERED_CODE"), "something happened")
	if e.Hint != "" || e.Docs != "" {
		t.Errorf("expected empty hint/docs for unregistered code, got %+v", e)
	}
	if e.Message != "something happened" {
		t.Errorf("Message lost: %q", e.Message)
	}
}

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

func TestFrom_PassesThroughExistingClierr(t *testing.T) {
	original := New(CodeDeployHostRequired, "deploy host is required")
	out := From(CodeInternal, original)
	if out != original {
		t.Error("From did not return the original *Error by identity")
	}
}

func TestFrom_WrapsArbitraryError(t *testing.T) {
	plain := errors.New("some lower-layer error")
	out := From(CodeGoBuildFailed, plain)
	if out.Code != string(CodeGoBuildFailed) {
		t.Errorf("Code = %q, want %q", out.Code, CodeGoBuildFailed)
	}
	if !errors.Is(out, plain) {
		t.Error("From did not preserve the original error in the cause chain")
	}
}

func TestFrom_NilReturnsNil(t *testing.T) {
	if From(CodeInternal, nil) != nil {
		t.Error("From(nil) must return nil so callers can chain safely")
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

// TestRegistry_EveryCodeHasAHint guards against adding a code constant
// and forgetting to register its hint. If a registered code has an empty
// hint, the test fails — that's a contract with agents/CI.
func TestRegistry_EveryCodeHasAHint(t *testing.T) {
	for code, entry := range registry {
		if entry.Hint == "" && code != CodeInternal {
			t.Errorf("code %q has no Hint — add one to registry in codes.go", code)
		}
	}
}

func TestNewf_FormatsMessage(t *testing.T) {
	e := Newf(CodeInvalidName, "name %q is not a valid module path", "My App")
	if e.Message != `name "My App" is not a valid module path` {
		t.Errorf("Message = %q", e.Message)
	}
}
