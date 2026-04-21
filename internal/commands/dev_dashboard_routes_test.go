package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Route-metadata extraction tests.
//
// `readRouteEntries` must pull the OpenAPI operation's request body
// type and primary 2xx response type out of docs/swagger.json so the
// dashboard can render them alongside method + path. These tests craft
// small swagger documents and verify the extractor handles:
//
//   - OpenAPI 2.0 `parameters[in=body].schema`
//   - OpenAPI 3.0 `requestBody.content['application/json'].schema`
//   - Array-of-ref response shapes
//   - Fallback to lowest response when no 2xx exists
//   - Summary copied verbatim into the route
// ─────────────────────────────────────────────────────────────────────

// writeSwagger writes the given JSON content to docs/swagger.json
// inside a tempdir that becomes the working directory for the duration
// of the test.
func writeSwagger(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	require.NoError(t, os.Chdir(dir))
	require.NoError(t, os.MkdirAll("docs", 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join("docs", "swagger.json"), []byte(body), 0o644,
	))
}

// findRoute returns the first route matching method + path. Used to
// assert on a single entry without depending on the order the map
// iteration happens to produce.
func findRoute(t *testing.T, routes []dashboardRoute, method, path string) dashboardRoute {
	t.Helper()
	for _, r := range routes {
		if r.Method == method && r.Path == path {
			return r
		}
	}
	t.Fatalf("route %s %s not found among %+v", method, path, routes)
	return dashboardRoute{}
}

func TestReadRouteEntries_OpenAPI2_BodyParamAndResponseRef(t *testing.T) {
	writeSwagger(t, `{
	  "paths": {
	    "/users": {
	      "post": {
	        "summary": "Create a user",
	        "parameters": [
	          { "in": "body", "name": "user", "schema": { "$ref": "#/definitions/CreateUser" } }
	        ],
	        "responses": {
	          "201": { "schema": { "$ref": "#/definitions/User" } },
	          "400": { "schema": { "$ref": "#/definitions/Error" } }
	        }
	      }
	    }
	  }
	}`)
	routes := readRouteEntries()
	require.Len(t, routes, 1)
	r := findRoute(t, routes, "POST", "/users")
	assert.Equal(t, "Create a user", r.Summary)
	assert.Equal(t, "CreateUser", r.Request)
	assert.Equal(t, "User", r.Response)
}

func TestReadRouteEntries_OpenAPI2_ArrayResponse(t *testing.T) {
	writeSwagger(t, `{
	  "paths": {
	    "/users": {
	      "get": {
	        "summary": "List users",
	        "responses": {
	          "200": {
	            "schema": {
	              "type": "array",
	              "items": { "$ref": "#/definitions/User" }
	            }
	          }
	        }
	      }
	    }
	  }
	}`)
	r := findRoute(t, readRouteEntries(), "GET", "/users")
	assert.Equal(t, "List users", r.Summary)
	assert.Empty(t, r.Request)
	assert.Equal(t, "[]User", r.Response)
}

func TestReadRouteEntries_OpenAPI3_RequestBodyAndContent(t *testing.T) {
	writeSwagger(t, `{
	  "paths": {
	    "/sessions": {
	      "post": {
	        "requestBody": {
	          "content": {
	            "application/json": {
	              "schema": { "$ref": "#/components/schemas/Credentials" }
	            }
	          }
	        },
	        "responses": {
	          "200": {
	            "content": {
	              "application/json": {
	                "schema": { "$ref": "#/components/schemas/Session" }
	              }
	            }
	          }
	        }
	      }
	    }
	  }
	}`)
	r := findRoute(t, readRouteEntries(), "POST", "/sessions")
	assert.Equal(t, "Credentials", r.Request)
	assert.Equal(t, "Session", r.Response)
}

func TestReadRouteEntries_PrimitiveTypeResponse(t *testing.T) {
	writeSwagger(t, `{
	  "paths": {
	    "/health": {
	      "get": {
	        "responses": {
	          "200": { "schema": { "type": "string" } }
	        }
	      }
	    }
	  }
	}`)
	r := findRoute(t, readRouteEntries(), "GET", "/health")
	assert.Equal(t, "string", r.Response)
}

func TestReadRouteEntries_FallsBackToLowestCodeWhenNo2xx(t *testing.T) {
	// Operation declares only error responses — the extractor should
	// pick the lowest code rather than leaving Response empty.
	writeSwagger(t, `{
	  "paths": {
	    "/admin": {
	      "get": {
	        "responses": {
	          "401": { "schema": { "$ref": "#/definitions/Error" } },
	          "403": { "schema": { "$ref": "#/definitions/Error" } }
	        }
	      }
	    }
	  }
	}`)
	r := findRoute(t, readRouteEntries(), "GET", "/admin")
	assert.Equal(t, "Error", r.Response)
}

func TestReadRouteEntries_HandlesEmptyOperations(t *testing.T) {
	writeSwagger(t, `{
	  "paths": {
	    "/ping": {
	      "get": {}
	    }
	  }
	}`)
	r := findRoute(t, readRouteEntries(), "GET", "/ping")
	assert.Empty(t, r.Request)
	assert.Empty(t, r.Response)
	assert.Empty(t, r.Summary)
}

func TestReadRouteEntries_MalformedJSONReturnsNil(t *testing.T) {
	writeSwagger(t, `{not json`)
	assert.Nil(t, readRouteEntries())
}

// --- Direct helper tests ---------------------------------------------------

func TestTypeNameFromSchema(t *testing.T) {
	assert.Equal(t, "", typeNameFromSchema(nil))
	assert.Equal(t, "User",
		typeNameFromSchema(&schemaRef{Ref: "#/definitions/User"}))
	assert.Equal(t, "Session",
		typeNameFromSchema(&schemaRef{Ref: "#/components/schemas/Session"}))
	// No slash separator — fall back to the raw ref value.
	assert.Equal(t, "BareRef",
		typeNameFromSchema(&schemaRef{Ref: "BareRef"}))
	// Array-of-ref renders as []TypeName.
	assert.Equal(t, "[]User",
		typeNameFromSchema(&schemaRef{
			Type:  "array",
			Items: &schemaRef{Ref: "#/definitions/User"},
		}))
	// Array with no items type — falls through to "array".
	assert.Equal(t, "array",
		typeNameFromSchema(&schemaRef{Type: "array"}))
	// Primitive types.
	assert.Equal(t, "string", typeNameFromSchema(&schemaRef{Type: "string"}))
	assert.Equal(t, "integer", typeNameFromSchema(&schemaRef{Type: "integer"}))
	// Empty schema → empty name.
	assert.Empty(t, typeNameFromSchema(&schemaRef{}))
}

func TestPickPrimaryResponseCode(t *testing.T) {
	// 2xx wins over everything else.
	assert.Equal(t, "200",
		pickPrimaryResponseCode(map[string]responseSpec{
			"200": {}, "201": {}, "400": {}, "500": {},
		}))
	// Lowest 2xx wins.
	assert.Equal(t, "201",
		pickPrimaryResponseCode(map[string]responseSpec{
			"201": {}, "202": {}, "204": {},
		}))
	// No 2xx — fall back to lowest of any tier.
	assert.Equal(t, "401",
		pickPrimaryResponseCode(map[string]responseSpec{
			"401": {}, "403": {}, "500": {},
		}))
	// Empty map.
	assert.Empty(t, pickPrimaryResponseCode(map[string]responseSpec{}))
	// Skip empty-string keys (shouldn't happen but defensive).
	assert.Equal(t, "200",
		pickPrimaryResponseCode(map[string]responseSpec{"": {}, "200": {}}))
}

// TestJSONRoundTrip_DashboardRoute — dashboardRoute must marshal cleanly
// so the SSE stream + /api/state endpoint can serialize it without
// surprise (nil maps, unexported fields, etc.).
func TestJSONRoundTrip_DashboardRoute(t *testing.T) {
	r := dashboardRoute{
		Method:   "POST",
		Path:     "/api/v1/users",
		Summary:  "Create user",
		Request:  "CreateUser",
		Response: "User",
	}
	b, err := json.Marshal(r)
	require.NoError(t, err)
	var back dashboardRoute
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, r, back)
}
