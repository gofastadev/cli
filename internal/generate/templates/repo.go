package templates

// Repo is the Go template for generating a GORM repository.
//
// Each method opens a span so the dashboard waterfall clearly
// distinguishes repository time from service time and SQL latency
// from business-logic latency. SQL bodies themselves are surfaced by
// the devtools GORM plugin on the Recent SQL panel — the spans here
// just anchor the operation in the trace tree.
var Repo = `package repositories

import (
	"context"
	"time"

	"github.com/google/uuid"
	"{{.ModulePath}}/app/models"
	repoInterfaces "{{.ModulePath}}/app/repositories/interfaces"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

const {{.LowerName}}RepositoryTracerName = "{{.ModulePath}}/app/repositories/{{.LowerName}}"

var _ repoInterfaces.{{.Name}}RepositoryInterface = (*{{.Name}}Repository)(nil)

type {{.Name}}Repository struct {
	DB *gorm.DB
}

func New{{.Name}}Repository(db *gorm.DB) *{{.Name}}Repository {
	return &{{.Name}}Repository{DB: db}
}

func (r *{{.Name}}Repository) FindAll(ctx context.Context, page, limit int, sort string) ([]*models.{{.Name}}, int64, error) {
	ctx, span := otel.Tracer({{.LowerName}}RepositoryTracerName).Start(ctx, "{{.Name}}Repository.FindAll")
	defer span.End()

	var total int64
	query := r.DB.WithContext(ctx).Model(&models.{{.Name}}{}).Where("deleted_at IS NULL")
	if err := query.Count(&total).Error; err != nil {
		span.RecordError(err)
		return nil, 0, err
	}
	var entities []*models.{{.Name}}
	offset := (page - 1) * limit
	if err := query.Limit(limit).Offset(offset).Order(sort).Find(&entities).Error; err != nil {
		span.RecordError(err)
		return nil, 0, err
	}
	return entities, total, nil
}

func (r *{{.Name}}Repository) FindByID(ctx context.Context, id uuid.UUID) (*models.{{.Name}}, error) {
	ctx, span := otel.Tracer({{.LowerName}}RepositoryTracerName).Start(ctx, "{{.Name}}Repository.FindByID")
	defer span.End()

	var entity models.{{.Name}}
	if err := r.DB.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", id).First(&entity).Error; err != nil {
		span.RecordError(err)
		return nil, err
	}
	return &entity, nil
}

func (r *{{.Name}}Repository) FindByIDAndRecordVersion(ctx context.Context, id uuid.UUID, version int) (*models.{{.Name}}, error) {
	ctx, span := otel.Tracer({{.LowerName}}RepositoryTracerName).Start(ctx, "{{.Name}}Repository.FindByIDAndRecordVersion")
	defer span.End()

	var entity models.{{.Name}}
	if err := r.DB.WithContext(ctx).Where("id = ? AND deleted_at IS NULL AND record_version = ?", id, version).First(&entity).Error; err != nil {
		span.RecordError(err)
		return nil, err
	}
	return &entity, nil
}

func (r *{{.Name}}Repository) Create(ctx context.Context, entity *models.{{.Name}}) error {
	ctx, span := otel.Tracer({{.LowerName}}RepositoryTracerName).Start(ctx, "{{.Name}}Repository.Create")
	defer span.End()

	if err := r.DB.WithContext(ctx).Create(entity).Error; err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}

func (r *{{.Name}}Repository) Update(ctx context.Context, id uuid.UUID, fields map[string]interface{}) error {
	ctx, span := otel.Tracer({{.LowerName}}RepositoryTracerName).Start(ctx, "{{.Name}}Repository.Update")
	defer span.End()

	if err := r.DB.WithContext(ctx).Model(&models.{{.Name}}{}).Where("id = ?", id).Updates(fields).Error; err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}

func (r *{{.Name}}Repository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	ctx, span := otel.Tracer({{.LowerName}}RepositoryTracerName).Start(ctx, "{{.Name}}Repository.SoftDelete")
	defer span.End()

	if err := r.DB.WithContext(ctx).Model(&models.{{.Name}}{}).
		Where("id = ? AND is_deletable = ?", id, true).
		Updates(map[string]interface{}{"deleted_at": time.Now(), "is_active": false}).Error; err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}
`
