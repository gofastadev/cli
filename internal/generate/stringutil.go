package generate

import "strings"

func toPascalCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '-' })
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

// commonInitialisms maps lowercase name segments to their Go-idiomatic
// all-caps form. Mirrors revive's var-naming list for the segments that
// plausibly appear in column names — the generated project's own lint
// (revive) rejects `OwnerId`, so the generator must emit `OwnerID`.
var commonInitialisms = map[string]string{
	"api": "API", "cpu": "CPU", "db": "DB", "dns": "DNS", "eof": "EOF",
	"guid": "GUID", "html": "HTML", "http": "HTTP", "https": "HTTPS",
	"id": "ID", "ip": "IP", "json": "JSON", "ram": "RAM", "sku": "SKU",
	"sql": "SQL", "ssh": "SSH", "tcp": "TCP", "tls": "TLS", "ttl": "TTL",
	"udp": "UDP", "ui": "UI", "uid": "UID", "uri": "URI", "url": "URL",
	"utf8": "UTF8", "uuid": "UUID", "vm": "VM", "xml": "XML",
}

// fieldPascalCase is toPascalCase with initialism awareness, used for
// struct-field names parsed from `name:type` definitions:
// "owner_id" → "OwnerID", "api_key" → "APIKey", "name" → "Name".
func fieldPascalCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '-' })
	for i, p := range parts {
		if p == "" {
			continue
		}
		if up, ok := commonInitialisms[strings.ToLower(p)]; ok {
			parts[i] = up
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

func toCamelCase(s string) string {
	p := toPascalCase(s)
	if p == "" {
		return p
	}
	return strings.ToLower(p[:1]) + p[1:]
}

func toSnakeCase(s string) string {
	var result []byte
	for i, c := range s {
		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				result = append(result, '_')
			}
			result = append(result, byte(c+32))
		} else {
			result = append(result, byte(c))
		}
	}
	return string(result)
}

func pluralize(s string) string {
	if strings.HasSuffix(s, "s") || strings.HasSuffix(s, "x") || strings.HasSuffix(s, "z") ||
		strings.HasSuffix(s, "ch") || strings.HasSuffix(s, "sh") {
		return s + "es"
	}
	if strings.HasSuffix(s, "y") && len(s) > 1 {
		c := s[len(s)-2]
		if c != 'a' && c != 'e' && c != 'i' && c != 'o' && c != 'u' {
			return s[:len(s)-1] + "ies"
		}
	}
	return s + "s"
}
