package naming

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPascal(t *testing.T) {
	cases := map[string]string{
		"user":            "User",
		"api_key":         "APIKey",
		"api-key":         "APIKey",
		"owner_id":        "OwnerID",
		"blog_post":       "BlogPost",
		"http_code":       "HTTPCode",
		"avatar_url":      "AvatarURL",
		"utf8_name":       "UTF8Name",
		"api-key_token":   "APIKeyToken",
		"purchase__order": "PurchaseOrder",
		"APIKey":          "APIKey", // idempotent on its own output
		"OwnerID":         "OwnerID",
		"BlogPost":        "BlogPost",
		"":                "",
	}
	for in, want := range cases {
		assert.Equal(t, want, Pascal(in), "Pascal(%q)", in)
	}
}

func TestSnake(t *testing.T) {
	cases := map[string]string{
		"User":       "user",
		"APIKey":     "api_key",
		"OwnerID":    "owner_id",
		"BlogPost":   "blog_post",
		"HTTPCode":   "http_code",
		"AvatarURL":  "avatar_url",
		"UTF8Name":   "utf8_name",
		"DNSServers": "dns_servers",
		"api_key":    "api_key", // idempotent on snake input
		"blog-post":  "blog_post",
		// Initialism plural tails stay attached to their word.
		"APIs":    "apis",
		"APIKeys": "api_keys",
		"DNSs":    "dnss",
		"URLs":    "urls",
		"":        "",
	}
	for in, want := range cases {
		assert.Equal(t, want, Snake(in), "Snake(%q)", in)
	}
}

func TestCamel(t *testing.T) {
	cases := map[string]string{
		"user":     "user",
		"api_key":  "apiKey",
		"owner_id": "ownerID",
		"APIKey":   "apiKey",
		"BlogPost": "blogPost",
		"":         "",
	}
	for in, want := range cases {
		assert.Equal(t, want, Camel(in), "Camel(%q)", in)
	}
}

func TestPluralize(t *testing.T) {
	cases := map[string]string{
		"User":     "Users",
		"Box":      "Boxes",
		"Class":    "Classes",
		"Church":   "Churches",
		"Dish":     "Dishes",
		"Category": "Categories",
		"Day":      "Days",
		"Buzz":     "Buzzes",
		// Initialism tails take a bare s, never es.
		"API":    "APIs",
		"DNS":    "DNSs",
		"APIKey": "APIKeys",
		"SKU":    "SKUs",
		// OwnerID-style tails too.
		"OwnerID": "OwnerIDs",
	}
	for in, want := range cases {
		assert.Equal(t, want, Pluralize(in), "Pluralize(%q)", in)
	}
}

// TestRoundTrips pins the two invariants the generators depend on:
// Snake(Pascal(snake_input)) is identity, and Pascal(Snake(pascal))
// is identity for names built from the initialism list.
func TestRoundTrips(t *testing.T) {
	snakeInputs := []string{"user", "api_key", "owner_id", "blog_post", "http_code", "utf8_name", "sku_alias"}
	for _, in := range snakeInputs {
		assert.Equal(t, in, Snake(Pascal(in)), "Snake(Pascal(%q))", in)
	}
	pascalInputs := []string{"User", "APIKey", "OwnerID", "BlogPost", "HTTPCode", "UTF8Name", "DNSServers"}
	for _, in := range pascalInputs {
		assert.Equal(t, in, Pascal(Snake(in)), "Pascal(Snake(%q))", in)
	}
}

func TestIsResourceName(t *testing.T) {
	assert.True(t, IsResourceName("api_key"))
	assert.True(t, IsResourceName("User2"))
	assert.False(t, IsResourceName("blog-post"), "hyphens produce invalid feature-package aliases")
	assert.False(t, IsResourceName("1user"))
	assert.False(t, IsResourceName(""))
	assert.False(t, IsResourceName("../etc"))
}

func TestIsIdentifier(t *testing.T) {
	assert.True(t, IsIdentifier("cleanup-tokens"), "job/task names keep kebab-case")
	assert.True(t, IsIdentifier("send_welcome"))
	assert.False(t, IsIdentifier("-leading"))
	assert.False(t, IsIdentifier("has space"))
	assert.False(t, IsIdentifier(""))
}
