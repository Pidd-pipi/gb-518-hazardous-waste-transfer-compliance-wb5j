package repository

import (
	"context"
	"time"

	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/dto"
	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ManifestQuotaStates are the states that occupy the generator's annual quota.
// Drafts wait outside the quota and rejected manifests release their hold;
// received history keeps counting toward the natural-year limit.
var ManifestQuotaStates = []string{"submitted", "in_transit", "received"}

// QuotaExceededError carries the annual quota decision back to the service so
// the HTTP layer can explain used, remaining and attempted weights. The
// manifest stays a draft: no status update and no audit row are written.
type QuotaExceededError struct {
	GeneratorCode string
	Year          int
	AnnualQuotaKg float64
	UsedKg        float64
	RemainingKg   float64
	AttemptedKg   float64
}

func (e *QuotaExceededError) Error() string {
	return "annual permit quota exceeded"
}

// TransferManifestRepository owns all persistence operations for 转运清单.
type TransferManifestRepository interface {
	List(context.Context, dto.PageQuery) (Page[model.TransferManifest], error)
	Get(context.Context, uint) (model.TransferManifest, error)
	FindByCode(context.Context, string) (model.TransferManifest, error)
	Create(context.Context, *model.TransferManifest) error
	CreateAudited(context.Context, *model.TransferManifest, *model.AuditLog) error
	Update(context.Context, uint, uint, *model.TransferManifest) error
	UpdateAudited(context.Context, uint, uint, *model.TransferManifest, *model.AuditLog) error
	Delete(context.Context, uint) error
	DeleteAudited(context.Context, uint, *model.AuditLog) error
	CountByStatus(context.Context) (map[string]int64, error)
	// ListQuotaRows returns quota-relevant manifests (submitted, in transit and
	// received) so the service can aggregate natural-year weight per generator.
	ListQuotaRows(context.Context) ([]model.TransferManifest, error)
	// SubmitWithQuotaGuard moves a draft manifest to submitted inside one
	// transaction. The generator row is locked, effective-year usage is summed
	// and the optimistic version is checked; when the new weight would cross
	// the annual quota the whole transaction rolls back with QuotaExceededError.
	SubmitWithQuotaGuard(ctx context.Context, generatorID uint, manifest *model.TransferManifest, expectedVersion uint, audit *model.AuditLog) error
}

type transferManifestRepository struct {
	store *Store[model.TransferManifest]
}

func NewTransferManifestRepository(db *gorm.DB) TransferManifestRepository {
	return &transferManifestRepository{store: NewStore[model.TransferManifest](db)}
}

func (r *transferManifestRepository) List(ctx context.Context, q dto.PageQuery) (Page[model.TransferManifest], error) {
	return r.store.List(ctx, q)
}
func (r *transferManifestRepository) Get(ctx context.Context, id uint) (model.TransferManifest, error) {
	return r.store.Get(ctx, id)
}
func (r *transferManifestRepository) FindByCode(ctx context.Context, code string) (model.TransferManifest, error) {
	return r.store.FindByCode(ctx, code)
}
func (r *transferManifestRepository) Create(ctx context.Context, item *model.TransferManifest) error {
	return r.store.Create(ctx, item)
}
func (r *transferManifestRepository) CreateAudited(ctx context.Context, item *model.TransferManifest, audit *model.AuditLog) error {
	return r.store.CreateAudited(ctx, item, audit)
}
func (r *transferManifestRepository) Update(ctx context.Context, id, version uint, item *model.TransferManifest) error {
	return r.store.Update(ctx, id, version, item)
}
func (r *transferManifestRepository) UpdateAudited(ctx context.Context, id, version uint, item *model.TransferManifest, audit *model.AuditLog) error {
	return r.store.UpdateAudited(ctx, id, version, item, audit)
}
func (r *transferManifestRepository) Delete(ctx context.Context, id uint) error {
	return r.store.Delete(ctx, id)
}
func (r *transferManifestRepository) DeleteAudited(ctx context.Context, id uint, audit *model.AuditLog) error {
	return r.store.DeleteAudited(ctx, id, audit)
}
func (r *transferManifestRepository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	return r.store.CountByStatus(ctx)
}

func (r *transferManifestRepository) ListQuotaRows(ctx context.Context) ([]model.TransferManifest, error) {
	var items []model.TransferManifest
	err := r.store.DB(ctx).
		Select("generator_code", "effective_at", "quantity_kg").
		Where("status IN ?", ManifestQuotaStates).
		Find(&items).Error
	return items, err
}

func (r *transferManifestRepository) SubmitWithQuotaGuard(ctx context.Context, generatorID uint, manifest *model.TransferManifest, expectedVersion uint, audit *model.AuditLog) error {
	year := manifest.EffectiveAt.UTC().Year()
	return r.store.DB(ctx).Transaction(func(tx *gorm.DB) error {
		// SQLite has no SELECT ... FOR UPDATE; the service serializes same-
		// generator submissions with a keyed mutex. PostgreSQL and MySQL take
		// row locks so multi-instance submissions cannot both cross the cap.
		withLock := func(query *gorm.DB) *gorm.DB {
			if tx.Dialector.Name() == "sqlite" {
				return query
			}
			return query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		var generator model.WasteGenerator
		generatorQuery := withLock(tx.Where("UPPER(code) = ?", manifest.GeneratorCode))
		if generatorID != 0 {
			generatorQuery = generatorQuery.Where("id = ?", generatorID)
		}
		if err := generatorQuery.First(&generator).Error; err != nil {
			return err
		}

		// Re-read the manifest under the same lock so concurrent submissions
		// serialize on the generator row instead of racing on usage totals.
		var current model.TransferManifest
		if err := withLock(tx).First(&current, manifest.ID).Error; err != nil {
			return err
		}
		if current.Version != expectedVersion || current.Status != "draft" {
			return ErrVersionConflict
		}

		yearStart := timeYearStart(year)
		yearEnd := yearStart.AddDate(1, 0, 0)
		var used float64
		if err := tx.Model(&model.TransferManifest{}).
			Where("generator_code = ? AND status IN ? AND effective_at >= ? AND effective_at < ?",
				manifest.GeneratorCode, ManifestQuotaStates, yearStart, yearEnd).
			Select("COALESCE(SUM(quantity_kg), 0)").Scan(&used).Error; err != nil {
			return err
		}
		remaining := generator.AnnualQuotaKg - used
		if remaining < 0 {
			remaining = 0
		}
		if used+manifest.QuantityKg > generator.AnnualQuotaKg+quotaEpsilon {
			return &QuotaExceededError{
				GeneratorCode: manifest.GeneratorCode,
				Year:          year,
				AnnualQuotaKg: generator.AnnualQuotaKg,
				UsedKg:        used,
				RemainingKg:   remaining,
				AttemptedKg:   manifest.QuantityKg,
			}
		}

		if err := updateRecord(tx, manifest.ID, expectedVersion, manifest); err != nil {
			return err
		}
		audit.EntityID = manifest.ID
		return tx.Create(audit).Error
	})
}

const quotaEpsilon = 1e-6

// timeYearStart returns the UTC instant at which year begins, avoiding dialect
// differences in date extraction.
func timeYearStart(year int) time.Time {
	return time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)
}
