// Coverage for crosscutting.go — the shared per-layout files whose
// import shape changes (container, wire, index routes, mocks, core providers).

package featurize

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTransformMock flips a generated testify mock from the layered interface
// packages to the feature package. The mock lives in `package mocks`, a
// different package from the feature it mocks, so references stay
// SelectorExprs — only the qualifier changes.
func TestTransformMock(t *testing.T) {
	src := `package mocks

import (
	"github.com/stretchr/testify/mock"
	"example.com/myapp/app/models"
	repoInterfaces "example.com/myapp/app/repositories/interfaces"
	svcInterfaces "example.com/myapp/app/services/interfaces"
	"example.com/myapp/app/services"
)

type UserServiceMock struct{ mock.Mock }

var _ svcInterfaces.UserServiceInterface = (*UserServiceMock)(nil)

func (m *UserServiceMock) Create(in services.CreateUserInput) (*models.User, error) {
	_ = repoInterfaces.ErrUserNotDeletable
	return nil, nil
}

func (m *UserServiceMock) Update(p services.UpdateUserPatch) error { return nil }

func (m *UserServiceMock) List(f services.ListUsersFilter) ([]*models.User, error) { return nil, nil }

func (m *UserServiceMock) Repo() repoInterfaces.UserRepositoryInterface { return nil }
`
	got, err := TransformMock([]byte(src), testMod, userResource())
	require.NoError(t, err)
	out := string(got)

	assert.Contains(t, out, `userpkg "example.com/myapp/app/user"`)
	assert.Contains(t, out, "userpkg.UserServiceInterface")
	assert.Contains(t, out, "userpkg.UserRepositoryInterface")
	assert.Contains(t, out, "userpkg.CreateUserInput")
	assert.Contains(t, out, "userpkg.UpdateUserPatch")
	assert.Contains(t, out, "userpkg.ListUsersFilter")
	assert.Contains(t, out, "userpkg.ErrUserNotDeletable")

	// The model deliberately stays put: under Option B it remains in
	// package models, so the mock keeps the cross-package reference.
	assert.Contains(t, out, "models.User")
	assert.Contains(t, out, `"example.com/myapp/app/models"`,
		"the models import must survive — the model does not move")

	assert.NotContains(t, out, "repoInterfaces.")
	assert.NotContains(t, out, "svcInterfaces.")
	assert.Contains(t, out, "package mocks", "the mock stays in package mocks")
}

// TestTransformMock_RoundTripsWithReverse pins the pair: featurizing a mock and
// unwinding it must restore the layered qualifiers.
func TestTransformMock_RoundTripsWithReverse(t *testing.T) {
	src := `package mocks

import (
	repoInterfaces "example.com/myapp/app/repositories/interfaces"
	svcInterfaces "example.com/myapp/app/services/interfaces"
	"example.com/myapp/app/services"
)

type UserServiceMock struct{}

var _ svcInterfaces.UserServiceInterface = (*UserServiceMock)(nil)

func (m *UserServiceMock) Create(in services.CreateUserInput) error { return nil }

func (m *UserServiceMock) Repo() repoInterfaces.UserRepositoryInterface { return nil }
`
	forward, err := TransformMock([]byte(src), testMod, userResource())
	require.NoError(t, err)

	back, err := TransformMockReverse(forward, testMod, userResource())
	require.NoError(t, err)

	out := string(back)
	assert.Contains(t, out, "svcInterfaces.UserServiceInterface")
	assert.Contains(t, out, "repoInterfaces.UserRepositoryInterface")
	assert.Contains(t, out, "services.CreateUserInput")
	assert.NotContains(t, out, "userpkg.")
}

// TestTransformCoreProviders rewrites only the password-generator binding.
// PasswordGenerator follows the user feature because it is the sole consumer
// in the bootstrap scaffold; everything else in core.go still resolves.
func TestTransformCoreProviders(t *testing.T) {
	src := `package providers

import (
	"github.com/google/wire"
	"example.com/myapp/app/services"
	"example.com/myapp/app/validators"
)

var CoreSet = wire.NewSet(
	services.NewDefaultPasswordGenerator,
	validators.NewAppValidator,
)
`
	got, err := TransformCoreProviders([]byte(src), testMod, []Resource{userResource()})
	require.NoError(t, err)
	out := string(got)

	assert.Contains(t, out, "userpkg.NewDefaultPasswordGenerator")
	assert.Contains(t, out, `userpkg "example.com/myapp/app/user"`)
	assert.NotContains(t, out, "services.NewDefaultPasswordGenerator")

	// Untouched bindings must survive verbatim.
	assert.Contains(t, out, "validators.NewAppValidator")
	assert.Contains(t, out, `"example.com/myapp/app/validators"`)
}

// TestTransformCoreProviders_NoUserResource covers the early return. A project
// with no user feature has nothing to rebind, and the file must come back
// byte-identical rather than losing its services import.
func TestTransformCoreProviders_NoUserResource(t *testing.T) {
	src := `package providers

import "example.com/myapp/app/services"

var CoreSet = services.NewDefaultPasswordGenerator
`
	got, err := TransformCoreProviders([]byte(src), testMod, []Resource{
		{Name: "Order", Snake: "order", Plural: "Orders"},
	})
	require.NoError(t, err)
	assert.Equal(t, src, string(got), "with no user feature the source must be returned unchanged")
}

func TestTransformCrossCutting_ParseError(t *testing.T) {
	_, err := TransformContainer([]byte("package di\n\nfunc Broken( {\n"), testMod, []Resource{userResource()})
	require.Error(t, err)
}
