// layout_test.go — snapshot tests pinning the path strings each layout
// returns. The layered snapshot in particular is the regression test
// promised by Phase A ("byte-for-byte equivalent to the pre-refactor
// hardcoded paths"); a failure here means a generator that used to
// write `app/models/<snake>.model.go` would now write somewhere else.

package layout

import "testing"

func TestLayered_PerResourceFiles(t *testing.T) {
	lo := For(Layered)
	if got, want := lo.Kind(), Layered; got != want {
		t.Fatalf("Kind() = %v, want %v", got, want)
	}
	if lo.IsFeature() {
		t.Fatal("IsFeature() = true, want false for layered")
	}
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"ModelFile", lo.ModelFile("user"), "app/models/user.model.go"},
		{"RepoIfaceFile", lo.RepoIfaceFile("user"), "app/repositories/interfaces/user_repository.go"},
		{"RepoImplFile", lo.RepoImplFile("user"), "app/repositories/user.repository.go"},
		{"RepoTestFile", lo.RepoTestFile("user"), "app/repositories/user.repository_test.go"},
		{"SvcIfaceFile", lo.SvcIfaceFile("user"), "app/services/interfaces/user_service.go"},
		{"SvcImplFile", lo.SvcImplFile("user"), "app/services/user.service.go"},
		{"SvcTestFile", lo.SvcTestFile("user"), "app/services/user.service_test.go"},
		{"ErrorsFile", lo.ErrorsFile("user"), "app/services/user_errors.go"},
		{"InputsFile", lo.InputsFile("user"), "app/services/user_inputs.go"},
		{"InputsTestFile", lo.InputsTestFile("user"), "app/services/user_inputs_test.go"},
		{"DTOsFile", lo.DTOsFile("user"), "app/dtos/user.dtos.go"},
		{"DTOsTestFile", lo.DTOsTestFile("user"), "app/dtos/user.dtos_test.go"},
		{"ControllerFile", lo.ControllerFile("user"), "app/rest/controllers/user.controller.go"},
		{"ControllerTestFile", lo.ControllerTestFile("user"), "app/rest/controllers/user.controller_test.go"},
		{"RoutesFile", lo.RoutesFile("user"), "app/rest/routes/user.routes.go"},
		{"ValidatorsFile", lo.ValidatorsFile("user"), "app/validators/user.validators.go"},
		{"WireProviderFile", lo.WireProviderFile("user"), "app/di/providers/user.go"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestLayered_SingleFiles(t *testing.T) {
	lo := For(Layered)
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"ContainerFile", lo.ContainerFile(), "app/di/container.go"},
		{"WireFile", lo.WireFile(), "app/di/wire.go"},
		{"RouteIndexFile", lo.RouteIndexFile(), "app/rest/routes/index.routes.go"},
		{"ServeFile", lo.ServeFile(), "cmd/serve.go"},
		{"ResolverFile", lo.ResolverFile(), "app/graphql/resolvers/resolver.go"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestLayered_InterfaceDirs(t *testing.T) {
	got := For(Layered).InterfaceDirs()
	want := []string{"app/services/interfaces", "app/repositories/interfaces"}
	if len(got) != len(want) {
		t.Fatalf("InterfaceDirs(): got %v (len=%d), want %v (len=%d)", got, len(got), want, len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("InterfaceDirs[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLayered_OtherDirs(t *testing.T) {
	lo := For(Layered)
	if got, want := lo.RoutesDir(), "app/rest/routes"; got != want {
		t.Errorf("RoutesDir(): got %q, want %q", got, want)
	}
	if got, want := lo.MigrationsDir(), "db/migrations"; got != want {
		t.Errorf("MigrationsDir(): got %q, want %q", got, want)
	}
}

func TestFeature_PerResourceFiles(t *testing.T) {
	lo := For(Feature)
	if got, want := lo.Kind(), Feature; got != want {
		t.Fatalf("Kind() = %v, want %v", got, want)
	}
	if !lo.IsFeature() {
		t.Fatal("IsFeature() = false, want true for feature")
	}
	cases := []struct {
		name string
		got  string
		want string
	}{
		// Under Option B, models stay in app/models/ in feature mode.
		{"ModelFile", lo.ModelFile("user"), "app/models/user.model.go"},
		{"RepoIfaceFile", lo.RepoIfaceFile("user"), "app/user/repository_iface.go"},
		{"RepoImplFile", lo.RepoImplFile("user"), "app/user/repository.go"},
		{"RepoTestFile", lo.RepoTestFile("user"), "app/user/repository_test.go"},
		{"SvcIfaceFile", lo.SvcIfaceFile("user"), "app/user/service_iface.go"},
		{"SvcImplFile", lo.SvcImplFile("user"), "app/user/service.go"},
		{"SvcTestFile", lo.SvcTestFile("user"), "app/user/service_test.go"},
		{"ErrorsFile", lo.ErrorsFile("user"), "app/user/errors.go"},
		{"InputsFile", lo.InputsFile("user"), "app/user/inputs.go"},
		{"InputsTestFile", lo.InputsTestFile("user"), "app/user/inputs_test.go"},
		{"DTOsFile", lo.DTOsFile("user"), "app/user/dtos.go"},
		{"DTOsTestFile", lo.DTOsTestFile("user"), "app/user/dtos_test.go"},
		{"ControllerFile", lo.ControllerFile("user"), "app/user/controller.go"},
		{"ControllerTestFile", lo.ControllerTestFile("user"), "app/user/controller_test.go"},
		{"RoutesFile", lo.RoutesFile("user"), "app/user/routes.go"},
		// Under Option B, per-resource validators stay in app/validators/.
		{"ValidatorsFile", lo.ValidatorsFile("user"), "app/validators/user.validators.go"},
		{"WireProviderFile", lo.WireProviderFile("user"), "app/user/wire.go"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestParseKind(t *testing.T) {
	cases := []struct {
		in   string
		want Kind
	}{
		{"layered", Layered},
		{"feature", Feature},
		{"", Layered},
		{"unknown", Layered},
	}
	for _, c := range cases {
		if got := ParseKind(c.in); got != c.want {
			t.Errorf("ParseKind(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestKindString(t *testing.T) {
	if got, want := Layered.String(), "layered"; got != want {
		t.Errorf("Layered.String() = %q, want %q", got, want)
	}
	if got, want := Feature.String(), "feature"; got != want {
		t.Errorf("Feature.String() = %q, want %q", got, want)
	}
}
