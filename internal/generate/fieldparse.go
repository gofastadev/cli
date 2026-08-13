package generate

import "strings"

// ParseFields converts CLI args like "name:string price:float" into typed Fields.
func ParseFields(args []string) []Field {
	var fields []Field
	for _, arg := range args {
		parts := strings.SplitN(arg, ":", 2)
		if len(parts) != 2 {
			continue
		}
		f := Field{
			// fieldPascalCase (not toPascalCase) so "owner_id" becomes
			// OwnerID — the generated project's revive lint rejects
			// OwnerId. JSON/snake names keep the plain conversions
			// ("ownerId" / "owner_id").
			Name:      fieldPascalCase(parts[0]),
			JSONName:  toCamelCase(parts[0]),
			SnakeName: toSnakeCase(parts[0]),
		}

		t, ok := lookupFieldType(strings.ToLower(parts[1]))
		if !ok {
			// Unknown type names fall back to the string entry — the
			// registry's first row by construction.
			t = fieldTypes[0]
		}
		t.applyTo(&f)

		fields = append(fields, f)
	}
	return fields
}
