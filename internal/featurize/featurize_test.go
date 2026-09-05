// Fixtures for featurize.go's public types (Resource, Options), shared
// by the test files of the transforms that consume them.

package featurize

const testMod = "example.com/myapp"

func userResource() Resource {
	return Resource{Name: "User", Snake: "user", Plural: "Users"}
}
