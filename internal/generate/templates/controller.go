package templates

var Controller = `package controllers

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/gofastadev/gofasta/app/dtos"
	svcInterfaces "github.com/gofastadev/gofasta/app/services/interfaces"
	"github.com/gofastadev/gofasta/app/utils"
	apperrors "github.com/gofastadev/gofasta/pkg/errors"
	"github.com/gofastadev/gofasta/pkg/httputil"
)

type {{.Name}}Controller struct {
	{{.Name}}Service svcInterfaces.{{.Name}}ServiceInterface
}

func New{{.Name}}ControllerInstance(svc svcInterfaces.{{.Name}}ServiceInterface) *{{.Name}}Controller {
	return &{{.Name}}Controller{ {{.Name}}Service: svc}
}

func (c *{{.Name}}Controller) List(w http.ResponseWriter, r *http.Request) error {
	filters := dtos.{{.Name}}FiltersDto{
		Pagination: &dtos.TPaginationInputDto{},
		Sorting:    &dtos.TSortingInputDto{SortByField: "created_at"},
	}
	res, err := c.{{.Name}}Service.FindAll(r.Context(), filters)
	if err != nil {
		return apperrors.NewInternal("failed to fetch {{.PluralLower}}", err)
	}
	return httputil.OK(w, res)
}

func (c *{{.Name}}Controller) GetByID(w http.ResponseWriter, r *http.Request) error {
	id, err := utils.ParseIdStringIsValidUUID(mux.Vars(r)["id"])
	if err != nil {
		return apperrors.NewBadRequest("id should be a valid UUID", nil)
	}
	res, err := c.{{.Name}}Service.FindByID(r.Context(), dtos.TFind{{.Name}}ByIDDto{ID: id})
	if err != nil {
		return apperrors.NewInternal("failed to find {{.LowerName}}", err)
	}
	return httputil.OK(w, res)
}

func (c *{{.Name}}Controller) Create(w http.ResponseWriter, r *http.Request) error {
	var input dtos.TCreate{{.Name}}Dto
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		return apperrors.NewBadRequest("invalid request payload", nil)
	}
	res, err := c.{{.Name}}Service.Create(r.Context(), input)
	if err != nil {
		return apperrors.NewInternal("failed to create {{.LowerName}}", err)
	}
	return httputil.Created(w, res)
}

func (c *{{.Name}}Controller) Update(w http.ResponseWriter, r *http.Request) error {
	id, err := utils.ParseIdStringIsValidUUID(mux.Vars(r)["id"])
	if err != nil {
		return apperrors.NewBadRequest("id should be a valid UUID", nil)
	}
	var input dtos.TUpdate{{.Name}}Dto
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		return apperrors.NewBadRequest("invalid request payload", nil)
	}
	input.ID = id
	res, err := c.{{.Name}}Service.Update(r.Context(), input)
	if err != nil {
		return apperrors.NewInternal("failed to update {{.LowerName}}", err)
	}
	return httputil.OK(w, res)
}

func (c *{{.Name}}Controller) Archive(w http.ResponseWriter, r *http.Request) error {
	id, err := utils.ParseIdStringIsValidUUID(mux.Vars(r)["id"])
	if err != nil {
		return apperrors.NewBadRequest("id should be a valid UUID", nil)
	}
	res, err := c.{{.Name}}Service.Archive(r.Context(), dtos.TArchive{{.Name}}Dto{ID: id})
	if err != nil {
		return apperrors.NewInternal("failed to archive {{.LowerName}}", err)
	}
	return httputil.OK(w, res)
}
`
