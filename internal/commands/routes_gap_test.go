package commands

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunRoutes_FeatureLayoutUsesLayoutRouteFiles covers the feature arm of
// runRoutes' layout switch.
//
// Layered keeps every registration under app/rest/routes/, so runRoutes scans
// that directory. Feature scatters them to app/<resource>/routes.go, and the
// only thing that knows where they went is layout.RouteFiles(). Taking the
// wrong arm makes `gofasta routes` silently report an empty route table for a
// feature project.
func TestRunRoutes_FeatureLayoutUsesLayoutRouteFiles(t *testing.T) {
	inRenderedProject(t)

	// Move the user routes into the feature package and declare the layout,
	// which is what layout.Detect() reads.
	_, _, err := migrateResource(userResourceFixture(), fixtureModulePath)
	require.NoError(t, err)
	require.NoError(t, flipLayoutInConfig())

	out := captureStdout(t, func() {
		require.NoError(t, runRoutes())
	})

	assert.Contains(t, out, "/users",
		"a feature project's per-resource routes must still be discovered")
}

// TestRunRoutes_FeatureLayoutWithNoRouteFiles covers the same arm when the
// layout resolves to nothing — an empty listing rather than an error.
func TestRunRoutes_FeatureLayoutWithNoRouteFiles(t *testing.T) {
	inRenderedProject(t)
	require.NoError(t, flipLayoutInConfig())
	require.NoError(t, os.RemoveAll("app/rest/routes"))

	assert.NoError(t, runRoutes(),
		"a project with no route files is an empty table, not a failure")
}
