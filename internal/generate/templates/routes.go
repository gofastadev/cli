package templates

var Routes = `package routes

import (
	"github.com/gorilla/mux"
	"{{.ModulePath}}/app/rest/controllers"
	"github.com/gofastadev/gofasta/pkg/httputil"
)

func {{.Name}}Routes(r *mux.Router, c *controllers.{{.Name}}Controller) {
	r.HandleFunc("/{{.PluralSnake}}", httputil.Handle(c.List)).Methods("GET")
	r.HandleFunc("/{{.PluralSnake}}", httputil.Handle(c.Create)).Methods("POST")
	r.HandleFunc("/{{.PluralSnake}}/{id}", httputil.Handle(c.GetByID)).Methods("GET")
	r.HandleFunc("/{{.PluralSnake}}/{id}", httputil.Handle(c.Update)).Methods("PUT")
	r.HandleFunc("/{{.PluralSnake}}/{id}", httputil.Handle(c.Archive)).Methods("DELETE")
}
`
