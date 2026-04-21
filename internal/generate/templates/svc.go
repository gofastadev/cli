package templates

// Svc is the Go template for generating a service implementation.
//
// Each method opens an OpenTelemetry span covering its body. This
// feeds the dev-dashboard trace waterfall: when a developer watches a
// request roll in, the controller → service → repository spans nest
// correctly and every span carries its entry stack (captured by the
// devtools span processor at OnStart). In production the spans go to
// whichever exporter pkg/observability is configured with; if tracing
// is disabled the tracer is a no-op and the cost is a single function
// call per boundary.
var Svc = `package services

import (
	"context"
	"math"

	"{{.ModulePath}}/app/dtos"
	"{{.ModulePath}}/app/models"
	repoInterfaces "{{.ModulePath}}/app/repositories/interfaces"
	svcInterfaces "{{.ModulePath}}/app/services/interfaces"
	"github.com/gofastadev/gofasta/pkg/utils"
	"github.com/gofastadev/gofasta/pkg/validators"
	"go.opentelemetry.io/otel"
)

// {{.LowerName}}ServiceTracerName is the tracer scope reported on each
// span this service opens. Matches the instrumentation library pattern
// used elsewhere in the scaffold so traces group cleanly in the
// dashboard.
const {{.LowerName}}ServiceTracerName = "{{.ModulePath}}/app/services/{{.LowerName}}"

var _ svcInterfaces.{{.Name}}ServiceInterface = (*{{.Name}}Service)(nil)

type {{.Name}}Service struct {
	{{.Name}}Repo repoInterfaces.{{.Name}}RepositoryInterface
	Validator     *validators.AppValidator
}

func New{{.Name}}Service(repo repoInterfaces.{{.Name}}RepositoryInterface, validator *validators.AppValidator) *{{.Name}}Service {
	return &{{.Name}}Service{
		{{.Name}}Repo: repo,
		Validator:     validator,
	}
}

func (s *{{.Name}}Service) FindAll(ctx context.Context, filters dtos.{{.Name}}FiltersDto) (*dtos.T{{.PluralName}}ResponseDto, error) {
	ctx, span := otel.Tracer({{.LowerName}}ServiceTracerName).Start(ctx, "{{.Name}}Service.FindAll")
	defer span.End()

	paginator := utils.PreparePaginating{PageFilters: filters.Pagination, Sorting: filters.Sorting}
	page := paginator.GetPage()
	limit := paginator.GetLimit()

	entities, totalCount, err := s.{{.Name}}Repo.FindAll(ctx, page, limit, paginator.GetSort())
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	var items []*dtos.{{.Name}}
	for _, e := range entities {
		items = append(items, cast{{.Name}}ToDto(e))
	}

	totalRecords := int(totalCount)
	totalPages := int(math.Ceil(float64(totalCount) / float64(limit)))
	return &dtos.T{{.PluralName}}ResponseDto{
		Data: items,
		Pagination: &dtos.TPaginationObjectDto{
			TotalRecords: &totalRecords, CurrentPage: &page,
			RecordsPerPage: &limit, TotalPages: &totalPages,
		},
	}, nil
}

func (s *{{.Name}}Service) FindByID(ctx context.Context, input dtos.TFind{{.Name}}ByIDDto) (*dtos.T{{.Name}}ResponseDto, error) {
	ctx, span := otel.Tracer({{.LowerName}}ServiceTracerName).Start(ctx, "{{.Name}}Service.FindByID")
	defer span.End()

	if errs := s.Validator.ValidateStruct(input); len(errs) > 0 {
		return &dtos.T{{.Name}}ResponseDto{Errors: errs}, nil
	}
	entity, err := s.{{.Name}}Repo.FindByID(ctx, input.ID)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	return &dtos.T{{.Name}}ResponseDto{Data: cast{{.Name}}ToDto(entity)}, nil
}

func (s *{{.Name}}Service) Create(ctx context.Context, input dtos.TCreate{{.Name}}Dto) (*dtos.T{{.Name}}ResponseDto, error) {
	ctx, span := otel.Tracer({{.LowerName}}ServiceTracerName).Start(ctx, "{{.Name}}Service.Create")
	defer span.End()

	if errs := s.Validator.ValidateStruct(input); len(errs) > 0 {
		return &dtos.T{{.Name}}ResponseDto{Errors: errs}, nil
	}
	entity := &models.{{.Name}}{
		// TODO: Map input fields to model fields
	}
	if err := s.{{.Name}}Repo.Create(ctx, entity); err != nil {
		span.RecordError(err)
		return nil, err
	}
	return &dtos.T{{.Name}}ResponseDto{Data: cast{{.Name}}ToDto(entity)}, nil
}

func (s *{{.Name}}Service) Update(ctx context.Context, input dtos.TUpdate{{.Name}}Dto) (*dtos.T{{.Name}}ResponseDto, error) {
	ctx, span := otel.Tracer({{.LowerName}}ServiceTracerName).Start(ctx, "{{.Name}}Service.Update")
	defer span.End()

	if errs := s.Validator.ValidateStruct(input); len(errs) > 0 {
		return &dtos.T{{.Name}}ResponseDto{Errors: errs}, nil
	}
	if found, _ := s.{{.Name}}Repo.FindByIDAndRecordVersion(ctx, input.ID, input.RecordVersion); found == nil {
		fieldName := "recordVersion"
		return &dtos.T{{.Name}}ResponseDto{Errors: []*dtos.TCommonAPIErrorDto{{lbrace}}{{lbrace}}FieldName: &fieldName, Message: "The record version you passed is not matching"{{rbrace}}{{rbrace}}}, nil
	}
	fields := utils.ConvertStructToMap(input)
	if err := s.{{.Name}}Repo.Update(ctx, input.ID, fields); err != nil {
		span.RecordError(err)
		return nil, err
	}
	updated, err := s.{{.Name}}Repo.FindByID(ctx, input.ID)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	return &dtos.T{{.Name}}ResponseDto{Data: cast{{.Name}}ToDto(updated)}, nil
}

func (s *{{.Name}}Service) Archive(ctx context.Context, input dtos.TArchive{{.Name}}Dto) (*dtos.TCommonResponseDto, error) {
	ctx, span := otel.Tracer({{.LowerName}}ServiceTracerName).Start(ctx, "{{.Name}}Service.Archive")
	defer span.End()

	if errs := s.Validator.ValidateStruct(input); len(errs) > 0 {
		return &dtos.TCommonResponseDto{Errors: errs}, nil
	}
	if err := s.{{.Name}}Repo.SoftDelete(ctx, input.ID); err != nil {
		span.RecordError(err)
		return nil, err
	}
	status := 200
	message := "Success"
	return &dtos.TCommonResponseDto{Status: status, Message: &message}, nil
}

func cast{{.Name}}ToDto(e *models.{{.Name}}) *dtos.{{.Name}} {
	return &dtos.{{.Name}}{
		ID: e.ID, RecordVersion: e.RecordVersion,
		CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
		IsActive: e.IsActive, IsDeletable: e.IsDeletable,
		DeletedAt: &e.DeletedAt,
		// TODO: Map remaining model fields to DTO fields
	}
}
`
