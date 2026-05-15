package templates

// GraphQL is the Go template for generating a GraphQL schema fragment.
//
// After the senior-architecture refactor:
//   - The single-resource response wraps `data` only — validation /
//     domain errors flow via gqlerror with structured `extensions.code`.
//   - Mutation outputs return the typed entity directly (or Boolean!
//     for archive) instead of a Response envelope. Less conditional
//     branching at the client.
//   - Input types include filter/page/sort fields directly — list
//     queries no longer require a nested FiltersInput object.
var GraphQL = `type {{.Name}} {
  id: ID!
  recordVersion: Int!
  createdAt: DateTime!
  updatedAt: DateTime!
  isActive: Boolean!
  isDeletable: Boolean!
  deletedAt: DateTime
{{- range .Fields}}
  {{.JSONName}}: {{.GQLType}}!
{{- end}}
}

type T{{.PluralName}}ResponseDto {
  data: [{{.Name}}!]!
  pagination: TPaginationObjectDto!
}

extend type Query {
  findAll{{.PluralName}}(filters: T{{.Name}}FiltersInput!): T{{.PluralName}}ResponseDto!
  find{{.Name}}ById(input: TFind{{.Name}}ByIdInput!): {{.Name}}!
}

extend type Mutation {
  create{{.Name}}(input: TCreate{{.Name}}Input!): {{.Name}}!
  update{{.Name}}(input: TUpdate{{.Name}}Input!): {{.Name}}!
  archive{{.Name}}(input: TArchive{{.Name}}Input!): Boolean!
}

input TFind{{.Name}}ByIdInput {
  id: ID!
}

input TArchive{{.Name}}Input {
  id: ID!
}

input TCreate{{.Name}}Input {
{{- range .Fields}}
  {{.JSONName}}: {{.GQLType}}!
{{- end}}
}

input TUpdate{{.Name}}Input {
  id: ID!
  recordVersion: Int!
{{- range .Fields}}
  {{.JSONName}}: {{.GQLType}}
{{- end}}
  isActive: Boolean
  isDeletable: Boolean
}

input T{{.Name}}FiltersInput {
{{- range .Fields}}
  {{.JSONName}}: {{.GQLType}}
{{- end}}
  page: Int
  limit: Int
  sortByField: String
  sortOrientation: SortOrientation
}
`
