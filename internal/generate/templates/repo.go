package templates

// Repo is the Go template for generating a GORM repository.
var Repo = `package repositories

import (
	"context"
	"time"

	"github.com/google/uuid"
	"{{.ModulePath}}/app/models"
	repoInterfaces "{{.ModulePath}}/app/repositories/interfaces"
	"gorm.io/gorm"
)

var _ repoInterfaces.{{.Name}}RepositoryInterface = (*{{.Name}}Repository)(nil)

type {{.Name}}Repository struct {
	DB *gorm.DB
}

func New{{.Name}}Repository(db *gorm.DB) *{{.Name}}Repository {
	return &{{.Name}}Repository{DB: db}
}

func (r *{{.Name}}Repository) FindAll(ctx context.Context, page, limit int, sort string) ([]*models.{{.Name}}, int64, error) {
	var total int64
	query := r.DB.WithContext(ctx).Model(&models.{{.Name}}{}).Where("deleted_at IS NULL")
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var entities []*models.{{.Name}}
	offset := (page - 1) * limit
	if err := query.Limit(limit).Offset(offset).Order(sort).Find(&entities).Error; err != nil {
		return nil, 0, err
	}
	return entities, total, nil
}

func (r *{{.Name}}Repository) FindByID(ctx context.Context, id uuid.UUID) (*models.{{.Name}}, error) {
	var entity models.{{.Name}}
	if err := r.DB.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", id).First(&entity).Error; err != nil {
		return nil, err
	}
	return &entity, nil
}

func (r *{{.Name}}Repository) FindByIDAndRecordVersion(ctx context.Context, id uuid.UUID, version int) (*models.{{.Name}}, error) {
	var entity models.{{.Name}}
	if err := r.DB.WithContext(ctx).Where("id = ? AND deleted_at IS NULL AND record_version = ?", id, version).First(&entity).Error; err != nil {
		return nil, err
	}
	return &entity, nil
}

func (r *{{.Name}}Repository) Create(ctx context.Context, entity *models.{{.Name}}) error {
	return r.DB.WithContext(ctx).Create(entity).Error
}

func (r *{{.Name}}Repository) Update(ctx context.Context, id uuid.UUID, fields map[string]interface{}) error {
	return r.DB.WithContext(ctx).Model(&models.{{.Name}}{}).Where("id = ?", id).Updates(fields).Error
}

func (r *{{.Name}}Repository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	return r.DB.WithContext(ctx).Model(&models.{{.Name}}{}).
		Where("id = ? AND is_deletable = ?", id, true).
		Updates(map[string]interface{}{"deleted_at": time.Now(), "is_active": false}).Error
}
`
