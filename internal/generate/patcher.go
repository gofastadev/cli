package generate

import (
	"fmt"
	"os"
	"strings"
)

// PatchContainer adds repo/service/controller fields to app/di/container.go.
func PatchContainer(d ScaffoldData) error {
	path := "app/di/container.go"
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(content)

	if strings.Contains(s, d.Name+"Service ") {
		fmt.Printf("  skip (already wired): %s\n", path)
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
	s = strings.Replace(s, "\tResolver       *resolvers.Resolver", fields+"\tResolver       *resolvers.Resolver", 1)

	fmt.Printf("  patch: %s\n", path)
	return os.WriteFile(path, []byte(s), 0644)
}

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
		fmt.Printf("  skip (already wired): %s\n", path)
		return nil
	}

	s = strings.Replace(s, "\t\tproviders.GraphQLSet,", fmt.Sprintf("\t\t%s,\n\t\tproviders.GraphQLSet,", providerRef), 1)

	fmt.Printf("  patch: %s\n", path)
	return os.WriteFile(path, []byte(s), 0644)
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
		fmt.Printf("  skip (already wired): %s\n", path)
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

	fmt.Printf("  patch: %s\n", path)
	return os.WriteFile(path, []byte(s), 0644)
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
		fmt.Printf("  skip (already wired): %s\n", path)
		return nil
	}

	s = strings.Replace(s,
		"\tHealthController *controllers.HealthController",
		fmt.Sprintf("\t%s *controllers.%sController\n\tHealthController *controllers.HealthController", controllerField, d.Name),
		1)

	routeCall := fmt.Sprintf("\t%sRoutes(api, config.%s)\n", d.Name, controllerField)
	s = strings.Replace(s, "\n\treturn r", routeCall+"\n\treturn r", 1)

	fmt.Printf("  patch: %s\n", path)
	return os.WriteFile(path, []byte(s), 0644)
}

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
		fmt.Printf("  skip (already wired): %s\n", path)
		return nil
	}

	s = strings.Replace(s,
		"HealthController: healthController,",
		fmt.Sprintf("%s:   container.%s,\n\t\tHealthController: healthController,", controllerField, controllerField),
		1)

	fmt.Printf("  patch: %s\n", path)
	return os.WriteFile(path, []byte(s), 0644)
}
