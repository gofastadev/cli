package templates

// Routes is the Go template for generating route registration for a resource.
var Routes = `package routes

import (
	"github.com/go-chi/chi/v5"
	"{{.ModulePath}}/app/rest/controllers"
	"github.com/gofastadev/gofasta/pkg/httputil"
)

func {{.Name}}Routes(r chi.Router, c *controllers.{{.Name}}Controller) {
	r.Get("/{{.PluralSnake}}", httputil.Handle(c.List))
	r.Post("/{{.PluralSnake}}", httputil.Handle(c.Create))
	r.Get("/{{.PluralSnake}}/{id}", httputil.Handle(c.GetByID))
	r.Put("/{{.PluralSnake}}/{id}", httputil.Handle(c.Update))
	r.Delete("/{{.PluralSnake}}/{id}", httputil.Handle(c.Archive))
}
`
