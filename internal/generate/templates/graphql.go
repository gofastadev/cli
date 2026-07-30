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
  findAll{{.PluralName}}(filters: T{{.Name}}FiltersQueryParamsDto!): T{{.PluralName}}ResponseDto!
  find{{.Name}}ById(input: TFind{{.Name}}ByIdDto!): {{.Name}}!
}

extend type Mutation {
  create{{.Name}}(input: TCreate{{.Name}}Dto!): {{.Name}}!
  update{{.Name}}(input: TUpdate{{.Name}}GraphQLInput!): {{.Name}}!
  # archive returns the soft-deleted record (matches REST's 200 + body
  # response). Idempotent: a second archive of the same id returns a
  # NOT_FOUND gqlerror.
  archive{{.Name}}(input: TArchive{{.Name}}Dto!): {{.Name}}!
}

# Input names deliberately match the hand-written DTO type names so
# gqlgen's autobind reuses those structs (validate tags + ToCreateInput
# / ToPatch / ToFilter helpers) instead of minting helperless models.
# gqlgen's name mangling binds TFind{{.Name}}ByIdDto to the Go type
# TFind{{.Name}}ByIDDto (Id → ID initialism), same as the skeleton's
# user schema.
input TFind{{.Name}}ByIdDto {
  id: ID!
}

input TArchive{{.Name}}Dto {
  id: ID!
}

input TCreate{{.Name}}Dto {
{{- range .Fields}}
  {{.JSONName}}: {{.GQLType}}!
{{- end}}
}

input TUpdate{{.Name}}GraphQLInput {
  id: ID!
  recordVersion: Int!
{{- range .Fields}}
  {{.JSONName}}: {{.GQLType}}
{{- end}}
  isActive: Boolean
  isDeletable: Boolean
}

input T{{.Name}}FiltersQueryParamsDto {
{{- range .Fields}}
  {{.JSONName}}: {{.GQLType}}
{{- end}}
  page: Int
  limit: Int
  sortByField: String
  sortOrientation: SortOrientation
}
`
