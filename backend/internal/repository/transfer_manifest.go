package repository

import (
	"context"
	"time"

	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/dto"
	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/model"
	"gorm.io/gorm"
)

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
	// SumOccupiedWeights totals the weight of quota-occupying manifests
	// (submitted, in_transit, received) grouped by generator code and the UTC
	// natural year of effective_at.
	SumOccupiedWeights(context.Context) ([]GeneratorYearWeight, error)
	// SubmitWithQuota runs a single serializable transaction that locks the
	// linked generator row, invokes decide with the freshly locked generator
	// and current-year occupied weight, then persists the transition chosen by
	// the service. decide returning QuotaDecision{Allowed:false} commits the
	// quota-blocked audit row but leaves the manifest in draft.
	SubmitWithQuota(ctx context.Context, manifestID uint, expectedVersion uint, decide QuotaDecisionFunc, audit, blockedAudit *model.AuditLog) (model.TransferManifest, error)
}

// GeneratorYearWeight is one aggregated occupancy row.
type GeneratorYearWeight struct {
	GeneratorCode string  `gorm:"column:generator_code"`
	Year          int     `gorm:"column:year"`
	TotalKg       float64 `gorm:"column:total_kg"`
}

// QuotaDecision tells SubmitWithQuota how the service wants the transaction to
// finish.
type QuotaDecision struct {
	TargetStatus string
	Reason       string
	Allowed      bool
}

// QuotaDecisionFunc receives the row-locked generator, the manifest being
// submitted and the weight already occupied in its effective-at year.
type QuotaDecisionFunc func(generator model.WasteGenerator, manifest model.TransferManifest, usedKg float64) (QuotaDecision, error)

type transferManifestRepository struct {
	store      *Store[model.TransferManifest]
	generators WasteGeneratorRepository
}

func NewTransferManifestRepository(db *gorm.DB, generators WasteGeneratorRepository) TransferManifestRepository {
	return &transferManifestRepository{store: NewStore[model.TransferManifest](db), generators: generators}
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

func (r *transferManifestRepository) SumOccupiedWeights(ctx context.Context) ([]GeneratorYearWeight, error) {
	// Aggregate in Go rather than with year-extraction SQL so the same query
	// works on SQLite (dev/tests), PostgreSQL and MySQL.
	rows := make([]struct {
		GeneratorCode string
		EffectiveAt   time.Time
		QuantityKg    float64
	}, 0)
	err := r.store.DB().WithContext(ctx).
		Model(&model.TransferManifest{}).
		Select("generator_code, effective_at, quantity_kg").
		Where("status IN ?", model.QuotaOccupyingManifestStates).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	type key struct {
		code string
		year int
	}
	totals := make(map[key]float64)
	order := make([]key, 0)
	for _, row := range rows {
		k := key{code: row.GeneratorCode, year: row.EffectiveAt.UTC().Year()}
		if _, seen := totals[k]; !seen {
			order = append(order, k)
		}
		totals[k] += row.QuantityKg
	}
	result := make([]GeneratorYearWeight, 0, len(order))
	for _, k := range order {
		result = append(result, GeneratorYearWeight{GeneratorCode: k.code, Year: k.year, TotalKg: totals[k]})
	}
	return result, nil
}

func (r *transferManifestRepository) SubmitWithQuota(ctx context.Context, manifestID uint, expectedVersion uint, decide QuotaDecisionFunc, audit, blockedAudit *model.AuditLog) (model.TransferManifest, error) {
	var result model.TransferManifest
	err := r.store.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var manifest model.TransferManifest
		if err := tx.First(&manifest, manifestID).Error; err != nil {
			return err
		}
		generator, err := r.generators.FindByCodeForUpdate(tx, manifest.GeneratorCode)
		if err != nil {
			return err
		}
		year := manifest.EffectiveAt.UTC().Year()
		var usedKg float64
		if err := tx.Model(&model.TransferManifest{}).
			Where("generator_code = ? AND status IN ? AND effective_at >= ? AND effective_at < ?",
				manifest.GeneratorCode, model.QuotaOccupyingManifestStates,
				time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC),
				time.Date(year+1, time.January, 1, 0, 0, 0, 0, time.UTC)).
			Select("COALESCE(SUM(quantity_kg), 0)").Scan(&usedKg).Error; err != nil {
			return err
		}
		decision, err := decide(generator, manifest, usedKg)
		if err != nil {
			return err
		}
		if !decision.Allowed {
			blockedAudit.EntityID = manifestID
			return tx.Create(blockedAudit).Error
		}
		manifest.Status = decision.TargetStatus
		manifest.Version = expectedVersion + 1
		manifest.UpdatedAt = time.Now().UTC()
		if err := updateRecord(tx, manifestID, expectedVersion, &manifest); err != nil {
			return err
		}
		audit.EntityID = manifestID
		if err := tx.Create(audit).Error; err != nil {
			return err
		}
		return tx.First(&result, manifestID).Error
	})
	return result, err
}
