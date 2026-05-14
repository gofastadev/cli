package generate

import (
	"fmt"
	"os"
	"strings"

	"github.com/gofastadev/cli/internal/termcolor"
)

// containerFieldsMarker pins the line `gofasta g scaffold` inserts new
// resource fields above. The scaffolded container.go.tmpl ships the
// marker; if it's removed the patch silently no-ops, which is why
// PatchContainer treats a missing marker as a hard error rather than a
// warning. Keep this string in sync with container.go.tmpl.
const containerFieldsMarker = "// gofasta:scaffold:container-fields"

// PatchContainer adds repo/service/controller fields to app/di/container.go.
func PatchContainer(d ScaffoldData) error {
	path := "app/di/container.go"
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(content)

	if strings.Contains(s, d.Name+"Service ") {
		termcolor.PrintSkip(path, "already wired")
		return nil
	}

	repoImport := fmt.Sprintf("\trepoInterfaces \"%s/app/repositories/interfaces\"", d.ModulePath)
	controllersImport := fmt.Sprintf("\"%s/app/rest/controllers\"", d.ModulePath)
	if !strings.Contains(s, "repoInterfaces") {
		s = strings.Replace(s, "\t"+controllersImport, repoImport+"\n\t"+controllersImport, 1)
	}

	fields := fmt.Sprintf("\t%sRepo       repoInterfaces.%sRepositoryInterface\n\t%sService    svcInterfaces.%sServiceInterface\n",
		d.Name, d.Name, d.Name, d.Name)
	if d.IncludeController {
		fields += fmt.Sprintf("\t%sController *controllers.%sController\n", d.Name, d.Name)
	}

	if !strings.Contains(s, containerFieldsMarker) {
		return fmt.Errorf("%s is missing the %q marker — the scaffold template is out of sync with the patcher; restore the marker comment to enable code generation", path, containerFieldsMarker)
	}
	s = strings.Replace(s, "\t"+containerFieldsMarker, fields+"\t"+containerFieldsMarker, 1)

	return writeOrRecordPatch(path,
		describePatch(fmt.Sprintf("add %sRepo/%sService fields", d.Name, d.Name)),
		[]byte(s))
}

// wireProvidersMarker pins the line `gofasta g scaffold` inserts new
// providers.<Name>Set entries above. Keep in sync with wire.go.tmpl.
const wireProvidersMarker = "// gofasta:scaffold:wire-providers"

// PatchWireFile adds the provider set to wire.Build in app/di/wire.go.
func PatchWireFile(d ScaffoldData) error {
	path := "app/di/wire.go"
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(content)

	providerRef := fmt.Sprintf("providers.%sSet", d.Name)
	if strings.Contains(s, providerRef) {
		termcolor.PrintSkip(path, "already wired")
		return nil
	}

	if !strings.Contains(s, wireProvidersMarker) {
		return fmt.Errorf("%s is missing the %q marker — the scaffold template is out of sync with the patcher; restore the marker comment to enable code generation", path, wireProvidersMarker)
	}
	s = strings.Replace(s, "\t\t"+wireProvidersMarker, fmt.Sprintf("\t\t%s,\n\t\t%s", providerRef, wireProvidersMarker), 1)

	return writeOrRecordPatch(path,
		describePatch("add "+providerRef+" to wire.Build"),
		[]byte(s))
}

// PatchResolver adds a service field and constructor param to app/graphql/resolvers/resolver.go.
func PatchResolver(d ScaffoldData) error {
	path := "app/graphql/resolvers/resolver.go"
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(content)

	fieldName := d.Name + "Service"
	if strings.Contains(s, fieldName) {
		termcolor.PrintSkip(path, "already wired")
		return nil
	}

	// Add field to Resolver struct
	fieldLine := fmt.Sprintf("\t%s svcInterfaces.%sServiceInterface\n", fieldName, d.Name)
	s = strings.Replace(s, "}\n\n// NewResolver", fieldLine+"}\n\n// NewResolver", 1)

	// Update NewResolver signature
	paramName := d.LowerName + "Service"
	oldSig := "func NewResolver("
	sigStart := strings.Index(s, oldSig)
	if sigStart == -1 {
		return fmt.Errorf("could not find NewResolver signature")
	}
	sigEnd := strings.Index(s[sigStart:], ")")
	currentParams := s[sigStart+len(oldSig) : sigStart+sigEnd]
	newParam := fmt.Sprintf("%s svcInterfaces.%sServiceInterface", paramName, d.Name)
	s = s[:sigStart+len(oldSig)] + currentParams + ", " + newParam + s[sigStart+sigEnd:]

	// Add field assignment in constructor body
	oldAssign := "return &Resolver{"
	retIdx := strings.Index(s, oldAssign)
	if retIdx == -1 {
		return fmt.Errorf("could not find Resolver constructor body")
	}
	closingBrace := strings.Index(s[retIdx:], "}")
	beforeClose := s[:retIdx+closingBrace]
	afterClose := s[retIdx+closingBrace:]
	s = beforeClose + ", " + fieldName + ": " + paramName + afterClose

	return writeOrRecordPatch(path,
		describePatch("inject "+fieldName+" into Resolver"),
		[]byte(s))
}

// PatchRouteConfig adds controller to RouteConfig and registers routes in app/rest/routes/index.routes.go.
func PatchRouteConfig(d ScaffoldData) error {
	path := "app/rest/routes/index.routes.go"
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(content)

	controllerField := d.Name + "Controller"
	if strings.Contains(s, controllerField) {
		termcolor.PrintSkip(path, "already wired")
		return nil
	}

	if !strings.Contains(s, routeConfigFieldsMarker) {
		return fmt.Errorf("%s is missing the %q marker — restore the marker comment to enable code generation", path, routeConfigFieldsMarker)
	}
	if !strings.Contains(s, routeRegistrationsMarker) {
		return fmt.Errorf("%s is missing the %q marker — restore the marker comment to enable code generation", path, routeRegistrationsMarker)
	}

	newField := fmt.Sprintf("\t%s *controllers.%sController", controllerField, d.Name)
	s = strings.Replace(s,
		"\t"+routeConfigFieldsMarker,
		newField+"\n\t"+routeConfigFieldsMarker,
		1)

	routeCall := fmt.Sprintf("\t%sRoutes(api, config.%s)", d.Name, controllerField)
	s = strings.Replace(s,
		"\t"+routeRegistrationsMarker,
		routeCall+"\n\t"+routeRegistrationsMarker,
		1)

	return writeOrRecordPatch(path,
		describePatch("register "+d.Name+"Routes under /api/v1"),
		[]byte(s))
}

// routeConfigFieldsMarker pins where new <Name>Controller fields land
// in the RouteConfig struct. Keep in sync with index.routes.go.tmpl.
const routeConfigFieldsMarker = "// gofasta:scaffold:route-config-fields"

// routeRegistrationsMarker pins where new <Name>Routes(api, ...) calls
// land in InitAPIRoutes. Keep in sync with index.routes.go.tmpl.
const routeRegistrationsMarker = "// gofasta:scaffold:route-registrations"

// routeConfigInitMarker pins where new <Name>Controller: container.<Name>Controller,
// lines land in the RouteConfig literal in cmd/serve.go. Keep in sync
// with serve.go.tmpl.
const routeConfigInitMarker = "// gofasta:scaffold:routeconfig-init"

// PatchServeFile adds the controller to RouteConfig initialization in cmd/serve.go.
func PatchServeFile(d ScaffoldData) error {
	path := "cmd/serve.go"
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(content)

	controllerField := d.Name + "Controller"
	if strings.Contains(s, controllerField) {
		termcolor.PrintSkip(path, "already wired")
		return nil
	}

	if !strings.Contains(s, routeConfigInitMarker) {
		return fmt.Errorf("%s is missing the %q marker — restore the marker comment to enable code generation", path, routeConfigInitMarker)
	}
	newLine := fmt.Sprintf("%s: container.%s,", controllerField, controllerField)
	s = strings.Replace(s,
		"\t\t"+routeConfigInitMarker,
		newLine+"\n\t\t"+routeConfigInitMarker,
		1)

	return writeOrRecordPatch(path,
		describePatch("wire "+controllerField+" into RouteConfig"),
		[]byte(s))
}
