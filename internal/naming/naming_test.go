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

// FuzzNamingRoundTrip — for any valid identifier, the conversion set
// must be internally stable: Snake output survives Pascal→Snake
// round-tripping (the invariant `g rename` and the generators depend
// on), and no conversion may panic on arbitrary input.
func FuzzNamingRoundTrip(f *testing.F) {
	f.Add("APIKey")
	f.Add("owner_id")
	f.Add("HTTPServer")
	f.Add("simple")
	f.Add("Product2Go")
	f.Add("UTF8Name")
	f.Fuzz(func(t *testing.T, in string) {
		// Conversions must never panic, valid input or not.
		p := Pascal(in)
		_ = Camel(in)
		s := Snake(in)
		_ = Pluralize(p)
		if !IsIdentifier(in) {
			return // stability invariants only hold for valid identifiers
		}
		// Snake must be idempotent unconditionally.
		if Snake(s) != s {
			t.Fatalf("Snake not idempotent for %q: Snake=%q, Snake(Snake)=%q", in, s, Snake(s))
		}
		// Some boundaries are inherently unencodable in a case-based
		// join: "a_a" → "AA", "api_id" → "APIID", "a_a0a" → "AA0a" — no
		// PascalCase spelling can mark where one segment ends and the
		// next begins. The invariant asserted is therefore operational:
		// WHENEVER the Pascal join preserves the word boundaries, the
		// round trip must be exact. (Purely Go-identifier cosmetics
		// either way — GORM's NamingStrategy makes the same split we do,
		// APIID → api_id, so database columns never drift; verified
		// empirically against gorm.io/gorm/schema.)
		joined := Pascal(s)
		if len(words(joined)) != len(words(s)) {
			return
		}
		if again := Snake(joined); again != s {
			t.Fatalf("snake round-trip unstable for %q: Snake=%q, Snake(Pascal(Snake))=%q", in, s, again)
		}
	})
}

func TestPascalWordEmptySegment(t *testing.T) {
	// splitWords never emits an empty segment today, so Pascal cannot
	// reach this branch — but pascalWord must stay safe on one (the
	// w[:1] slice below the guard would panic otherwise).
	assert.Equal(t, "", pascalWord(""))
	assert.Equal(t, "ID", pascalWord("id"))
	assert.Equal(t, "Name", pascalWord("name"))
}
