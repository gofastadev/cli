package templates

var SvcInterface = `package interfaces

import (
	"context"

	"github.com/gofastadev/gofasta/app/dtos"
)

type {{.Name}}ServiceInterface interface {
	FindAll(ctx context.Context, filters dtos.{{.Name}}FiltersDto) (*dtos.T{{.PluralName}}ResponseDto, error)
	FindByID(ctx context.Context, input dtos.TFind{{.Name}}ByIDDto) (*dtos.T{{.Name}}ResponseDto, error)
	Create(ctx context.Context, input dtos.TCreate{{.Name}}Dto) (*dtos.T{{.Name}}ResponseDto, error)
	Update(ctx context.Context, input dtos.TUpdate{{.Name}}Dto) (*dtos.T{{.Name}}ResponseDto, error)
	Archive(ctx context.Context, input dtos.TArchive{{.Name}}Dto) (*dtos.TCommonResponseDto, error)
}
`
