package generate

import (
	"bytes"
	"errors"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/require"
)

// chdirTest is a local test helper — switch cwd for the duration of one
// test, restore on cleanup. Mirrors the one in the commands package; kept
// here so the generate package's tests stay self-contained.
func chdirTest(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

// setupMockProject lays out a minimal gofasta project tree under tmp
// with go.mod + one interface file. Returns the project root.
func setupMockProject(t *testing.T, ifaceSrc string) string {
	t.Helper()
	tmp := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))

	ifaceDir := filepath.Join(tmp, "app", "services", "interfaces")
	require.NoError(t, os.MkdirAll(ifaceDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ifaceDir, "thing_service.go"),
		[]byte(ifaceSrc), 0o644))

	return tmp
}

func TestGenMock_BasicInterfaceEndToEnd(t *testing.T) {
	src := `package interfaces

import "context"

type ThingService interface {
	Hello(ctx context.Context, name string) (string, error)
	Goodbye(ctx context.Context)
}
`
	tmp := setupMockProject(t, src)
	chdirTest(t, tmp)

	require.NoError(t, GenMock("ThingService", GenMockOpts{}))

	body, err := os.ReadFile(filepath.Join(tmp, "testutil", "mocks", "thing_service_mock.go"))
	require.NoError(t, err)

	s := string(body)
	require.Contains(t, s, "type ThingServiceMock struct")
	require.Contains(t, s, "var _ interfaces.ThingService = (*ThingServiceMock)(nil)")
	require.Contains(t, s, "func (m *ThingServiceMock) Hello(")
	require.Contains(t, s, "func (m *ThingServiceMock) Goodbye(")
	require.Contains(t, s, "args.Error(1)")
	// gofmt'd output starts with the doc comment line.
	require.True(t, strings.HasPrefix(s, "// Code generated"))
}

func TestGenMock_MissingInterfaceFires(t *testing.T) {
	src := `package interfaces

type Other interface{}
`
	tmp := setupMockProject(t, src)
	chdirTest(t, tmp)

	err := GenMock("ThingService", GenMockOpts{})
	require.Error(t, err)
	var ce *clierr.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, string(clierr.CodeInterfaceNotFound), ce.Code)
}

func TestGenMock_CheckModeDetectsDrift(t *testing.T) {
	src := `package interfaces

type ThingService interface {
	Hello() error
}
`
	tmp := setupMockProject(t, src)
	chdirTest(t, tmp)

	// Generate once so the file exists.
	require.NoError(t, GenMock("ThingService", GenMockOpts{}))

	// Mutate the interface so the regenerated mock would differ.
	newSrc := `package interfaces

type ThingService interface {
	Hello() error
	NewMethod() error
}
`
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "app", "services", "interfaces", "thing_service.go"),
		[]byte(newSrc), 0o644))

	err := GenMock("ThingService", GenMockOpts{Check: true})
	require.Error(t, err)
	var ce *clierr.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, string(clierr.CodeMockDrift), ce.Code)
}

func TestGenMock_AllModeRegeneratesEveryInterface(t *testing.T) {
	tmp := setupMockProject(t, `package interfaces

type One interface{ A() error }
type Two interface{ B() error }
`)
	chdirTest(t, tmp)

	require.NoError(t, GenMock("", GenMockOpts{All: true}))

	for _, n := range []string{"one_mock.go", "two_mock.go"} {
		_, err := os.Stat(filepath.Join(tmp, "testutil", "mocks", n))
		require.NoError(t, err, "expected %s to be generated", n)
	}
}

// TestRenderMock_OutputIsGofmtClean asserts the generator emits already-
// formatted code — a regression here would force users to run gofmt after
// every regeneration.
func TestRenderMock_OutputIsGofmtClean(t *testing.T) {
	tmp := setupMockProject(t, `package interfaces

import "context"

type Svc interface {
	Do(ctx context.Context, n int) (string, error)
}
`)
	chdirTest(t, tmp)
	require.NoError(t, GenMock("Svc", GenMockOpts{}))

	body, err := os.ReadFile(filepath.Join(tmp, "testutil", "mocks", "svc_mock.go"))
	require.NoError(t, err)

	formatted, err := format.Source(body)
	require.NoError(t, err)
	require.True(t, bytes.Equal(body, formatted),
		"generated mock is not gofmt-clean — diff is non-empty after format.Source")
}

func TestToMockSnake(t *testing.T) {
	cases := map[string]string{
		"OrderService":         "order_service",
		"UserRepositoryReader": "user_repository_reader",
		"X":                    "x",
		"xyz":                  "xyz",
	}
	for in, want := range cases {
		if got := toMockSnake(in); got != want {
			t.Errorf("toMockSnake(%q) = %q, want %q", in, got, want)
		}
	}
}
