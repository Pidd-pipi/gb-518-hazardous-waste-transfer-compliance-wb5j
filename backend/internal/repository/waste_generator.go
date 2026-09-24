package repository

import (
	"context"
	"strings"

	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/dto"
	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WasteGeneratorRepository owns all persistence operations for 产废单位.
type WasteGeneratorRepository interface {
	List(context.Context, dto.PageQuery) (Page[model.WasteGenerator], error)
	Get(context.Context, uint) (model.WasteGenerator, error)
	FindByCode(context.Context, string) (model.WasteGenerator, error)
	Create(context.Context, *model.WasteGenerator) error
	CreateAudited(context.Context, *model.WasteGenerator, *model.AuditLog) error
	Update(context.Context, uint, uint, *model.WasteGenerator) error
	UpdateAudited(context.Context, uint, uint, *model.WasteGenerator, *model.AuditLog) error
	Delete(context.Context, uint) error
	DeleteAudited(context.Context, uint, *model.AuditLog) error
	CountByStatus(context.Context) (map[string]int64, error)
	// FindByCodeForUpdate re-reads a generator on the given transaction while
	// taking a write lock on its row. It serializes concurrent manifest
	// submissions for the same generator so they cannot overshoot the annual
	// quota together. The call MUST run inside the caller's transaction.
	FindByCodeForUpdate(tx *gorm.DB, code string) (model.WasteGenerator, error)
}

type wasteGeneratorRepository struct {
	store *Store[model.WasteGenerator]
}

func NewWasteGeneratorRepository(db *gorm.DB) WasteGeneratorRepository {
	return &wasteGeneratorRepository{store: NewStore[model.WasteGenerator](db)}
}

func (r *wasteGeneratorRepository) List(ctx context.Context, q dto.PageQuery) (Page[model.WasteGenerator], error) {
	return r.store.List(ctx, q)
}
func (r *wasteGeneratorRepository) Get(ctx context.Context, id uint) (model.WasteGenerator, error) {
	return r.store.Get(ctx, id)
}
func (r *wasteGeneratorRepository) FindByCode(ctx context.Context, code string) (model.WasteGenerator, error) {
	return r.store.FindByCode(ctx, code)
}
func (r *wasteGeneratorRepository) Create(ctx context.Context, item *model.WasteGenerator) error {
	return r.store.Create(ctx, item)
}
func (r *wasteGeneratorRepository) CreateAudited(ctx context.Context, item *model.WasteGenerator, audit *model.AuditLog) error {
	return r.store.CreateAudited(ctx, item, audit)
}
func (r *wasteGeneratorRepository) Update(ctx context.Context, id, version uint, item *model.WasteGenerator) error {
	return r.store.Update(ctx, id, version, item)
}
func (r *wasteGeneratorRepository) UpdateAudited(ctx context.Context, id, version uint, item *model.WasteGenerator, audit *model.AuditLog) error {
	return r.store.UpdateAudited(ctx, id, version, item, audit)
}
func (r *wasteGeneratorRepository) Delete(ctx context.Context, id uint) error {
	return r.store.Delete(ctx, id)
}
func (r *wasteGeneratorRepository) DeleteAudited(ctx context.Context, id uint, audit *model.AuditLog) error {
	return r.store.DeleteAudited(ctx, id, audit)
}
func (r *wasteGeneratorRepository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	return r.store.CountByStatus(ctx)
}

func (r *wasteGeneratorRepository) FindByCodeForUpdate(tx *gorm.DB, code string) (model.WasteGenerator, error) {
	normalized := strings.ToUpper(strings.TrimSpace(code))
	var item model.WasteGenerator
	var err error
	if tx.Dialector.Name() == "sqlite" {
		// SQLite only has a database-level write lock. The raw no-op UPDATE
		// forces the deferred transaction to acquire that lock immediately
		// (waiting up to busy_timeout) so a second submission reads the
		// committed quota usage instead of racing past the cap together.
		result := tx.Exec("UPDATE waste_generators SET code = code WHERE UPPER(code) = ?", normalized)
		if result.Error != nil {
			return model.WasteGenerator{}, result.Error
		}
		if result.RowsAffected == 0 {
			return model.WasteGenerator{}, gorm.ErrRecordNotFound
		}
		err = tx.Where("UPPER(code) = ?", normalized).First(&item).Error
	} else {
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("UPPER(code) = ?", normalized).First(&item).Error
	}
	return item, err
}
