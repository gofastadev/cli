package templates

// Controller is the Go template for generating a REST controller.
var Controller = `package controllers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"{{.ModulePath}}/app/dtos"
	svcInterfaces "{{.ModulePath}}/app/services/interfaces"
	"github.com/gofastadev/gofasta/pkg/utils"
	apperrors "github.com/gofastadev/gofasta/pkg/errors"
	"github.com/gofastadev/gofasta/pkg/httputil"
)

type {{.Name}}Controller struct {
	{{.Name}}Service svcInterfaces.{{.Name}}ServiceInterface
}

func New{{.Name}}ControllerInstance(svc svcInterfaces.{{.Name}}ServiceInterface) *{{.Name}}Controller {
	return &{{.Name}}Controller{ {{.Name}}Service: svc}
}

{{- if .IncludeSwagger}}
// List godoc
//
//	@Summary		List {{.PluralLower}}
//	@Description	Get all {{.PluralLower}} with optional filtering, pagination, and sorting
//	@Tags			{{.PluralLower}}
//	@Produce		json
//	@Param			sortByField	query		string	false	"Field to sort by"
//	@Success		200			{object}	dtos.T{{.PluralName}}ResponseDto
//	@Failure		500			{object}	dtos.TCommonAPIErrorDto
//	@Router			/{{.PluralLower}} [get]
{{- end}}
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

{{- if .IncludeSwagger}}
// GetByID godoc
//
//	@Summary		Get a {{.LowerName}}
//	@Description	Get a single {{.LowerName}} by ID
//	@Tags			{{.PluralLower}}
//	@Produce		json
//	@Param			id	path		string	true	"{{.Name}} ID"
//	@Success		200	{object}	dtos.T{{.Name}}ResponseDto
//	@Failure		400	{object}	dtos.TCommonAPIErrorDto
//	@Failure		404	{object}	dtos.TCommonAPIErrorDto
//	@Failure		500	{object}	dtos.TCommonAPIErrorDto
//	@Router			/{{.PluralLower}}/{id} [get]
{{- end}}
func (c *{{.Name}}Controller) GetByID(w http.ResponseWriter, r *http.Request) error {
	id, err := utils.ParseIDStringIsValidUUID(chi.URLParam(r, "id"))
	if err != nil {
		return apperrors.NewBadRequest("id should be a valid UUID", nil)
	}
	res, err := c.{{.Name}}Service.FindByID(r.Context(), dtos.TFind{{.Name}}ByIDDto{ID: id})
	if err != nil {
		return apperrors.NewInternal("failed to find {{.LowerName}}", err)
	}
	return httputil.OK(w, res)
}

{{- if .IncludeSwagger}}
// Create godoc
//
//	@Summary		Create a {{.LowerName}}
//	@Description	Create a new {{.LowerName}}
//	@Tags			{{.PluralLower}}
//	@Accept			json
//	@Produce		json
//	@Param			{{.LowerName}}	body		dtos.TCreate{{.Name}}Dto	true	"{{.Name}} data"
//	@Success		201				{object}	dtos.T{{.Name}}ResponseDto
//	@Failure		400				{object}	dtos.TCommonAPIErrorDto
//	@Failure		500				{object}	dtos.TCommonAPIErrorDto
//	@Router			/{{.PluralLower}} [post]
{{- end}}
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

{{- if .IncludeSwagger}}
// Update godoc
//
//	@Summary		Update a {{.LowerName}}
//	@Description	Update {{.LowerName}} fields by ID
//	@Tags			{{.PluralLower}}
//	@Accept			json
//	@Produce		json
//	@Param			id				path		string						true	"{{.Name}} ID"
//	@Param			{{.LowerName}}	body		dtos.TUpdate{{.Name}}Dto	true	"Fields to update"
//	@Success		200				{object}	dtos.T{{.Name}}ResponseDto
//	@Failure		400				{object}	dtos.TCommonAPIErrorDto
//	@Failure		500				{object}	dtos.TCommonAPIErrorDto
//	@Router			/{{.PluralLower}}/{id} [put]
{{- end}}
func (c *{{.Name}}Controller) Update(w http.ResponseWriter, r *http.Request) error {
	id, err := utils.ParseIDStringIsValidUUID(chi.URLParam(r, "id"))
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

{{- if .IncludeSwagger}}
// Archive godoc
//
//	@Summary		Archive a {{.LowerName}}
//	@Description	Soft-delete a {{.LowerName}} by ID
//	@Tags			{{.PluralLower}}
//	@Produce		json
//	@Param			id	path		string	true	"{{.Name}} ID"
//	@Success		200	{object}	dtos.T{{.Name}}ResponseDto
//	@Failure		400	{object}	dtos.TCommonAPIErrorDto
//	@Failure		404	{object}	dtos.TCommonAPIErrorDto
//	@Failure		500	{object}	dtos.TCommonAPIErrorDto
//	@Router			/{{.PluralLower}}/{id} [delete]
{{- end}}
func (c *{{.Name}}Controller) Archive(w http.ResponseWriter, r *http.Request) error {
	id, err := utils.ParseIDStringIsValidUUID(chi.URLParam(r, "id"))
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
