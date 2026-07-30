package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToPascalCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"product", "Product"},
		{"product_name", "ProductName"},
		{"product-name", "ProductName"},
		{"", ""},
		{"already", "Already"},
		{"UPPER", "UPPER"},
		{"a", "A"},
		{"multi_word_name", "MultiWordName"},
		{"kebab-case-name", "KebabCaseName"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, toPascalCase(tc.input))
		})
	}
}

func TestToCamelCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"product", "product"},
		{"product_name", "productName"},
		{"product-name", "productName"},
		{"", ""},
		{"Already", "already"},
		{"a", "a"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, toCamelCase(tc.input))
		})
	}
}

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Product", "product"},
		{"ProductName", "product_name"},
		{"product", "product"},
		{"", ""},
		{"A", "a"},
		{"ABTest", "a_b_test"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, toSnakeCase(tc.input))
		})
	}
}

func TestPluralize(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Product", "Products"},
		{"Box", "Boxes"},
		{"Church", "Churches"},
		{"Brush", "Brushes"},
		{"Category", "Categories"},
		{"Day", "Days"},
		{"Bus", "Buses"},
		{"Fizz", "Fizzes"},
		{"Fox", "Foxes"},
		{"Boy", "Boys"},
		{"Key", "Keys"},
		{"City", "Cities"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, pluralize(tc.input))
		})
	}
}

func TestFieldPascalCase_Initialisms(t *testing.T) {
	cases := map[string]string{
		"owner_id":   "OwnerID",
		"id":         "ID",
		"api_key":    "APIKey",
		"avatar_url": "AvatarURL",
		"name":       "Name",
		"unit_price": "UnitPrice",
		"uuid":       "UUID",
		"http_code":  "HTTPCode",
	}
	for in, want := range cases {
		assert.Equal(t, want, fieldPascalCase(in), "fieldPascalCase(%q)", in)
	}
}
