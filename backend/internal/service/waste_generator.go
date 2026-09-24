package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/constants"
	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/dto"
	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/model"
	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/repository"
)

type WasteGeneratorService interface {
	List(context.Context, dto.PageQuery) (repository.Page[model.WasteGenerator], error)
	Get(context.Context, uint) (model.WasteGenerator, error)
	Create(context.Context, dto.CreateWasteGenerator, string, string) (model.WasteGenerator, error)
	Update(context.Context, uint, dto.UpdateWasteGenerator, string, string) (model.WasteGenerator, error)
	Transition(context.Context, uint, dto.TransitionRequest, string, string) (model.WasteGenerator, error)
	Delete(context.Context, uint, string, string) error
	StatusCounts(context.Context) (map[string]int64, error)
	QuotaUsage(ctx context.Context, year int) ([]dto.QuotaUsage, error)
}

type wasteGeneratorService struct {
	repository repository.WasteGeneratorRepository
	manifests  repository.TransferManifestRepository
	security   SecurityService
}

func NewWasteGeneratorService(repo repository.WasteGeneratorRepository, manifests repository.TransferManifestRepository, security SecurityService) WasteGeneratorService {
	return &wasteGeneratorService{repository: repo, manifests: manifests, security: security}
}

func (s *wasteGeneratorService) List(ctx context.Context, query dto.PageQuery) (repository.Page[model.WasteGenerator], error) {
	return s.repository.List(ctx, query)
}

func (s *wasteGeneratorService) Get(ctx context.Context, id uint) (model.WasteGenerator, error) {
	return s.repository.Get(ctx, id)
}

func (s *wasteGeneratorService) Create(ctx context.Context, input dto.CreateWasteGenerator, actor, requestID string) (model.WasteGenerator, error) {
	if err := validateWasteGeneratorBusinessFields(input.Code, input.Name, input.Facility, input.Owner, input.PermitNumber, input.WasteCategories, input.Evidence, input.PermitExpiresAt, input.AnnualQuotaKg); err != nil {
		return model.WasteGenerator{}, err
	}
	item := model.WasteGenerator{
		BaseModel: model.BaseModel{
			Code: strings.ToUpper(strings.TrimSpace(input.Code)), Name: strings.TrimSpace(input.Name),
			Status: model.WasteGeneratorInitialStatus, Version: 1, Description: strings.TrimSpace(input.Description),
		},
		PermitNumber: strings.ToUpper(strings.TrimSpace(input.PermitNumber)), PermitExpiresAt: input.PermitExpiresAt.UTC(),
		AnnualQuotaKg:   input.AnnualQuotaKg,
		WasteCategories: strings.TrimSpace(input.WasteCategories),
		Facility:        strings.TrimSpace(input.Facility), Owner: strings.TrimSpace(input.Owner),
		Category: strings.TrimSpace(input.Category), RiskLevel: input.RiskLevel,
		MetricValue: input.MetricValue, MetricUnit: strings.TrimSpace(input.MetricUnit),
		EffectiveAt: input.EffectiveAt.UTC(), Evidence: strings.TrimSpace(input.Evidence),
		RelatedCode: strings.ToUpper(strings.TrimSpace(input.RelatedCode)),
	}
	if err := s.repository.CreateAudited(ctx, &item, newAuditLog(actor, requestID, "create", "WasteGenerator", "", item.Status, "created generator permit")); err != nil {
		return model.WasteGenerator{}, fmt.Errorf("create 产废单位: %w", err)
	}
	return item, nil
}

func (s *wasteGeneratorService) Update(ctx context.Context, id uint, input dto.UpdateWasteGenerator, actor, requestID string) (model.WasteGenerator, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.WasteGenerator{}, err
	}
	if err := validateWasteGeneratorBusinessFields(current.Code, input.Name, input.Facility, input.Owner, input.PermitNumber, input.WasteCategories, input.Evidence, input.PermitExpiresAt, input.AnnualQuotaKg); err != nil {
		return model.WasteGenerator{}, err
	}
	current.Name = strings.TrimSpace(input.Name)
	current.PermitNumber = strings.ToUpper(strings.TrimSpace(input.PermitNumber))
	current.PermitExpiresAt = input.PermitExpiresAt.UTC()
	current.AnnualQuotaKg = input.AnnualQuotaKg
	current.WasteCategories = strings.TrimSpace(input.WasteCategories)
	current.Description = strings.TrimSpace(input.Description)
	current.Facility = strings.TrimSpace(input.Facility)
	current.Owner = strings.TrimSpace(input.Owner)
	current.Category = strings.TrimSpace(input.Category)
	current.RiskLevel = input.RiskLevel
	current.MetricValue = input.MetricValue
	current.MetricUnit = strings.TrimSpace(input.MetricUnit)
	current.EffectiveAt = input.EffectiveAt.UTC()
	current.Evidence = strings.TrimSpace(input.Evidence)
	current.RelatedCode = strings.ToUpper(strings.TrimSpace(input.RelatedCode))
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	if err := s.repository.UpdateAudited(ctx, id, input.ExpectedVersion, &current, newAuditLog(actor, requestID, "update", "WasteGenerator", current.Status, current.Status, "updated generator permit and evidence")); err != nil {
		return model.WasteGenerator{}, fmt.Errorf("update 产废单位: %w", err)
	}
	return s.repository.Get(ctx, id)
}

func (s *wasteGeneratorService) Transition(ctx context.Context, id uint, input dto.TransitionRequest, actor, requestID string) (model.WasteGenerator, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.WasteGenerator{}, err
	}
	target := strings.TrimSpace(input.Status)
	if !constants.CanTransition(constants.WasteGeneratorTransitions, current.Status, target) {
		return model.WasteGenerator{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.Status, target)
	}
	if target == "active" && !current.PermitExpiresAt.After(time.Now().UTC()) {
		return model.WasteGenerator{}, fmt.Errorf("%w: expired permit cannot be activated", ErrInvalidInput)
	}
	before := current.Status
	current.Status = target
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	if err := s.repository.UpdateAudited(ctx, id, input.ExpectedVersion, &current, newAuditLog(actor, requestID, "transition", "WasteGenerator", before, target, input.Reason)); err != nil {
		return model.WasteGenerator{}, fmt.Errorf("transition 产废单位: %w", err)
	}
	return s.repository.Get(ctx, id)
}

func (s *wasteGeneratorService) Delete(ctx context.Context, id uint, actor, requestID string) error {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return err
	}
	return s.repository.DeleteAudited(ctx, id, newAuditLog(actor, requestID, "delete", "WasteGenerator", current.Status, "deleted", "soft deleted generator permit"))
}

func (s *wasteGeneratorService) StatusCounts(ctx context.Context) (map[string]int64, error) {
	return s.repository.CountByStatus(ctx)
}

// QuotaUsage aggregates occupied annual weight for every generator. Only
// submitted, in-transit and received manifests count, bucketed by the
// manifest effective year. The response always includes the requested year
// (current UTC year when year <= 0) plus any other years with usage so the
// manifest page can show per-row historical context.
func (s *wasteGeneratorService) QuotaUsage(ctx context.Context, year int) ([]dto.QuotaUsage, error) {
	if year <= 0 {
		year = time.Now().UTC().Year()
	}
	generators, err := s.repository.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("load generators for quota usage: %w", err)
	}
	rows, err := s.manifests.ListQuotaRows(ctx)
	if err != nil {
		return nil, fmt.Errorf("load manifests for quota usage: %w", err)
	}
	// generator code (upper) -> year -> used kg
	used := make(map[string]map[int]float64)
	for _, row := range rows {
		code := strings.ToUpper(strings.TrimSpace(row.GeneratorCode))
		rowYear := row.EffectiveAt.UTC().Year()
		if used[code] == nil {
			used[code] = make(map[int]float64)
		}
		used[code][rowYear] += row.QuantityKg
	}

	result := make([]dto.QuotaUsage, 0, len(generators))
	for _, generator := range generators {
		code := strings.ToUpper(strings.TrimSpace(generator.Code))
		years := []int{year}
		for otherYear := range used[code] {
			if otherYear != year {
				years = append(years, otherYear)
			}
		}
		for _, usageYear := range years {
			usedKg := used[code][usageYear]
			remaining := generator.AnnualQuotaKg - usedKg
			entry := dto.QuotaUsage{
				GeneratorCode: code, GeneratorName: generator.Name, Year: usageYear,
				AnnualQuotaKg: generator.AnnualQuotaKg, UsedKg: usedKg,
			}
			if remaining < 0 {
				entry.RemainingKg = 0
				entry.ExceededKg = -remaining
				entry.Exceeded = true
			} else {
				entry.RemainingKg = remaining
			}
			result = append(result, entry)
		}
	}
	return result, nil
}

func validateWasteGeneratorBusinessFields(code, name, facility, owner, permitNumber, categories, evidence string, expiresAt time.Time, annualQuotaKg float64) error {
	if strings.TrimSpace(code) == "" || strings.TrimSpace(name) == "" || strings.TrimSpace(facility) == "" || strings.TrimSpace(owner) == "" || strings.TrimSpace(permitNumber) == "" || strings.TrimSpace(categories) == "" {
		return fmt.Errorf("%w: generator identity and permit fields are required", ErrInvalidInput)
	}
	if strings.TrimSpace(evidence) == "" {
		return fmt.Errorf("%w: permit evidence reference is required", ErrInvalidInput)
	}
	if !expiresAt.After(time.Now().UTC()) {
		return fmt.Errorf("%w: generator permit must not be expired", ErrInvalidInput)
	}
	if annualQuotaKg <= 0 {
		return fmt.Errorf("%w: annual permit quota must be a positive weight", ErrInvalidInput)
	}
	return nil
}
