package templates

// GraphQL is the Go template for generating a GraphQL schema fragment.
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

type T{{.Name}}ResponseDto {
  data: {{.Name}}
  errors: [TCommonApiErrorDto]
}

extend type Query {
  findAll{{.PluralName}}(filters: {{.Name}}FiltersInput!): T{{.PluralName}}ResponseDto!
  find{{.Name}}ById(input: TFind{{.Name}}ByIdInput!): T{{.Name}}ResponseDto!
}

extend type Mutation {
  create{{.Name}}(input: TCreate{{.Name}}Input!): T{{.Name}}ResponseDto!
  update{{.Name}}(input: TUpdate{{.Name}}Input!): T{{.Name}}ResponseDto!
  archive{{.Name}}(input: TArchive{{.Name}}Input!): TCommonResponseDto!
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
}

input {{.Name}}FiltersInput {
  pagination: TPaginationInputDto
  sorting: TSortingInputDto
}
`
