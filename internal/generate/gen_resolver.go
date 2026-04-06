package generate

// GenResolver patches the GraphQL resolver to add a new service dependency.
func GenResolver(d ScaffoldData) error {
	return PatchResolver(d)
}
