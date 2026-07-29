package commands

import (
	"testing"

	"github.com/gofastadev/cli/internal/featurize"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Coverage for new.go's --layout=feature path: the routing decision made for
// every rendered template file. featurizeFile is a dispatch over five
// categories, and picking the wrong one either writes a file to the wrong
// directory or leaves it referencing a package that no longer exists.

func TestStarterResources(t *testing.T) {
	got := starterResources()
	require.Len(t, got, 1, "the scaffold ships exactly one starter resource")
	assert.Equal(t, featurize.Resource{Name: "User", Snake: "user", Plural: "Users"}, got[0])
}

// --- featurizeFile: category 1, per-resource reroute ---

func TestFeaturizeFile_ReroutesPerResourceFiles(t *testing.T) {
	resources := starterResources()

	cases := map[string]string{
		"app/services/user.service.go":                   "app/user/service.go",
		"app/repositories/user.repository.go":            "app/user/repository.go",
		"app/rest/controllers/user.controller.go":        "app/user/controller.go",
		"app/rest/routes/user.routes.go":                 "app/user/routes.go",
		"app/dtos/user.dtos.go":                          "app/user/dtos.go",
		"app/repositories/interfaces/user_repository.go": "app/user/repository_iface.go",
		"app/services/interfaces/user_service.go":        "app/user/service_iface.go",
		"app/di/providers/user.go":                       "app/user/wire.go",
	}

	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			out, body, err := featurizeFile(in, []byte("package services\n\ntype UserService struct{}\n"),
				fixtureModulePath, resources)
			require.NoError(t, err)
			assert.Equal(t, want, out)
			assert.Contains(t, string(body), "package user\n",
				"a file rerouted into app/user/ must declare the feature package")
		})
	}
}

func TestFeaturizeFile_PerResourceTransformFailure(t *testing.T) {
	_, _, err := featurizeFile("app/services/user.service.go",
		[]byte("package services\n\nfunc Broken( {\n"), fixtureModulePath, starterResources())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "featurize")
}

// --- featurizeFile: category 3, password generator ---

// TestFeaturizeFile_PasswordGeneratorFollowsUser covers the standalone file
// that is not a per-resource mapping entry but still belongs to a feature.
func TestFeaturizeFile_PasswordGeneratorFollowsUser(t *testing.T) {
	out, body, err := featurizeFile("app/services/password_generator.go",
		[]byte("package services\n\ntype PasswordGenerator struct{}\n"),
		fixtureModulePath, starterResources())
	require.NoError(t, err)

	assert.Equal(t, "app/user/password_generator.go", out)
	assert.Contains(t, string(body), "package user\n")
}

func TestFeaturizeFile_PasswordGeneratorTransformFailure(t *testing.T) {
	_, _, err := featurizeFile("app/services/password_generator.go",
		[]byte("package services\n\nfunc Broken( {\n"), fixtureModulePath, starterResources())
	require.Error(t, err)
}

// --- featurizeFile: category 2, cross-cutting files ---

// TestFeaturizeFile_CrossCuttingFilesStayPut covers the four shared files: the
// path is unchanged, only the content is rewritten to reach the feature
// packages.
func TestFeaturizeFile_CrossCuttingFilesStayPut(t *testing.T) {
	resources := starterResources()

	cases := map[string]string{
		"app/di/container.go": `package di

import (
	svcInterfaces "` + fixtureModulePath + `/app/services/interfaces"
)

type Container struct {
	UserService svcInterfaces.UserServiceInterface
}
`,
		"app/di/wire.go": `package di

import "github.com/google/wire"

var Set = wire.NewSet()
`,
		"app/rest/routes/index.routes.go": `package routes

type RouteConfig struct{}

func InitAPIRoutes(config *RouteConfig) {}
`,
		"app/di/providers/core.go": `package providers

import "` + fixtureModulePath + `/app/services"

var CoreSet = services.NewDefaultPasswordGenerator
`,
	}

	for path, src := range cases {
		t.Run(path, func(t *testing.T) {
			out, body, err := featurizeFile(path, []byte(src), fixtureModulePath, resources)
			require.NoError(t, err)
			assert.Equal(t, path, out, "cross-cutting files must not be rerouted")
			assert.NotEmpty(t, body)
		})
	}
}

func TestFeaturizeFile_CrossCuttingTransformFailures(t *testing.T) {
	for _, path := range []string{
		"app/di/container.go",
		"app/di/wire.go",
		"app/rest/routes/index.routes.go",
		"app/di/providers/core.go",
	} {
		t.Run(path, func(t *testing.T) {
			_, _, err := featurizeFile(path, []byte("package x\n\nfunc Broken( {\n"),
				fixtureModulePath, starterResources())
			require.Error(t, err)
		})
	}
}

// --- featurizeFile: mocks ---

// TestFeaturizeFile_MocksAreRewrittenInPlace covers the testutil/mocks branch:
// the mock stays where it is, but its qualifiers follow the resource.
func TestFeaturizeFile_MocksAreRewrittenInPlace(t *testing.T) {
	src := `package mocks

import (
	svcInterfaces "` + fixtureModulePath + `/app/services/interfaces"
)

type UserServiceMock struct{}

var _ svcInterfaces.UserServiceInterface = (*UserServiceMock)(nil)
`
	for _, path := range []string{
		"testutil/mocks/user_service_mock.go",
		"testutil/mocks/user_repository_mock.go",
	} {
		t.Run(path, func(t *testing.T) {
			out, body, err := featurizeFile(path, []byte(src), fixtureModulePath, starterResources())
			require.NoError(t, err)
			assert.Equal(t, path, out, "mocks stay in testutil/mocks")
			assert.Contains(t, string(body), "userpkg")
		})
	}
}

// TestFeaturizeFile_MockForUnknownResourceIsUntouched covers the loop falling
// through: a mock whose snake name matches no resource is left alone.
func TestFeaturizeFile_MockForUnknownResourceIsUntouched(t *testing.T) {
	src := []byte("package mocks\n\ntype GhostServiceMock struct{}\n")
	out, body, err := featurizeFile("testutil/mocks/ghost_service_mock.go", src,
		fixtureModulePath, starterResources())
	require.NoError(t, err)
	assert.Equal(t, "testutil/mocks/ghost_service_mock.go", out)
	assert.Equal(t, src, body)
}

func TestFeaturizeFile_MockTransformFailure(t *testing.T) {
	_, _, err := featurizeFile("testutil/mocks/user_service_mock.go",
		[]byte("package mocks\n\nfunc Broken( {\n"), fixtureModulePath, starterResources())
	require.Error(t, err)
}

// --- featurizeFile: pass-through ---

// TestFeaturizeFile_UnrelatedFilesPassThrough covers the default: a file that
// matches no category lives in the same place in both layouts and must come
// back byte-identical.
func TestFeaturizeFile_UnrelatedFilesPassThrough(t *testing.T) {
	src := []byte("package models\n\ntype User struct{}\n")

	for _, path := range []string{
		"app/models/user.model.go", // models deliberately do not move
		"cmd/serve.go",
		"config.yaml",
		"db/migrations/000001_create_users.up.sql",
	} {
		t.Run(path, func(t *testing.T) {
			out, body, err := featurizeFile(path, src, fixtureModulePath, starterResources())
			require.NoError(t, err)
			assert.Equal(t, path, out)
			assert.Equal(t, src, body)
		})
	}
}
