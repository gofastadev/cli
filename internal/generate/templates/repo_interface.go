package templates

var RepoInterface = `package interfaces

import (
	"context"

	"github.com/google/uuid"
	"github.com/gofastadev/gofasta/app/models"
)

type {{.Name}}RepositoryInterface interface {
	FindAll(ctx context.Context, page, limit int, sort string) ([]*models.{{.Name}}, int64, error)
	FindByID(ctx context.Context, id uuid.UUID) (*models.{{.Name}}, error)
	FindByIDAndRecordVersion(ctx context.Context, id uuid.UUID, version int) (*models.{{.Name}}, error)
	Create(ctx context.Context, entity *models.{{.Name}}) error
	Update(ctx context.Context, id uuid.UUID, fields map[string]interface{}) error
	SoftDelete(ctx context.Context, id uuid.UUID) error
}
`
