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

type TransferManifestService interface {
	List(context.Context, dto.PageQuery) (repository.Page[model.TransferManifest], error)
	Get(context.Context, uint) (model.TransferManifest, error)
	Create(context.Context, dto.CreateTransferManifest, string, string) (model.TransferManifest, error)
	Update(context.Context, uint, dto.UpdateTransferManifest, string, string) (model.TransferManifest, error)
	Transition(context.Context, uint, dto.TransitionRequest, string, string) (model.TransferManifest, error)
	Delete(context.Context, uint, string, string) error
	StatusCounts(context.Context) (map[string]int64, error)
}

type transferManifestService struct {
	repository repository.TransferManifestRepository
	generators repository.WasteGeneratorRepository
	carriers   repository.CarrierProfileRepository
	quota      *quotaService
}

func NewTransferManifestService(repo repository.TransferManifestRepository, generators repository.WasteGeneratorRepository, carriers repository.CarrierProfileRepository) TransferManifestService {
	return &transferManifestService{repository: repo, generators: generators, carriers: carriers, quota: newQuotaService(repo, generators)}
}

func (s *transferManifestService) List(ctx context.Context, query dto.PageQuery) (repository.Page[model.TransferManifest], error) {
	page, err := s.repository.List(ctx, query)
	if err != nil {
		return page, err
	}
	if err := s.quota.decorateManifests(ctx, page.Items); err != nil {
		return repository.Page[model.TransferManifest]{}, fmt.Errorf("load manifest quota usage: %w", err)
	}
	return page, nil
}

func (s *transferManifestService) Get(ctx context.Context, id uint) (model.TransferManifest, error) {
	item, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.TransferManifest{}, err
	}
	if err := s.quota.decorateManifest(ctx, &item); err != nil {
		return model.TransferManifest{}, fmt.Errorf("load manifest quota usage: %w", err)
	}
	return item, nil
}

func (s *transferManifestService) Create(ctx context.Context, input dto.CreateTransferManifest, actor, requestID string) (model.TransferManifest, error) {
	if err := validateTransferManifestBusinessFields(input.Code, input.Name, input.Facility, input.Owner, input.GeneratorCode, input.CarrierCode, input.WasteCode, input.Destination, input.Evidence, input.QuantityKg); err != nil {
		return model.TransferManifest{}, err
	}
	if _, err := s.generators.FindByCode(ctx, input.GeneratorCode); err != nil {
		return model.TransferManifest{}, fmt.Errorf("%w: generator %s does not exist", ErrInvalidInput, input.GeneratorCode)
	}
	if _, err := s.carriers.FindByCode(ctx, input.CarrierCode); err != nil {
		return model.TransferManifest{}, fmt.Errorf("%w: carrier %s does not exist", ErrInvalidInput, input.CarrierCode)
	}
	item := model.TransferManifest{
		BaseModel: model.BaseModel{
			Code: strings.ToUpper(strings.TrimSpace(input.Code)), Name: strings.TrimSpace(input.Name),
			Status: model.TransferManifestInitialStatus, Version: 1, Description: strings.TrimSpace(input.Description),
		},
		GeneratorCode: strings.ToUpper(strings.TrimSpace(input.GeneratorCode)), CarrierCode: strings.ToUpper(strings.TrimSpace(input.CarrierCode)),
		WasteCode: strings.ToUpper(strings.TrimSpace(input.WasteCode)), QuantityKg: input.QuantityKg, Destination: strings.TrimSpace(input.Destination),
		Facility: strings.TrimSpace(input.Facility), Owner: strings.TrimSpace(input.Owner),
		Category: strings.TrimSpace(input.Category), RiskLevel: input.RiskLevel,
		MetricValue: input.MetricValue, MetricUnit: strings.TrimSpace(input.MetricUnit),
		EffectiveAt: input.EffectiveAt.UTC(), Evidence: strings.TrimSpace(input.Evidence),
		RelatedCode: strings.ToUpper(strings.TrimSpace(input.RelatedCode)),
	}
	if err := s.repository.CreateAudited(ctx, &item, newAuditLog(actor, requestID, "create", "TransferManifest", "", item.Status, "created linked transfer manifest")); err != nil {
		return model.TransferManifest{}, fmt.Errorf("create 转运清单: %w", err)
	}
	return item, nil
}

func (s *transferManifestService) Update(ctx context.Context, id uint, input dto.UpdateTransferManifest, actor, requestID string) (model.TransferManifest, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.TransferManifest{}, err
	}
	if current.Status != "draft" {
		return model.TransferManifest{}, fmt.Errorf("%w: only draft manifests can be edited", ErrInvalidInput)
	}
	if err := validateTransferManifestBusinessFields(current.Code, input.Name, input.Facility, input.Owner, input.GeneratorCode, input.CarrierCode, input.WasteCode, input.Destination, input.Evidence, input.QuantityKg); err != nil {
		return model.TransferManifest{}, err
	}
	if _, err := s.generators.FindByCode(ctx, input.GeneratorCode); err != nil {
		return model.TransferManifest{}, fmt.Errorf("%w: generator %s does not exist", ErrInvalidInput, input.GeneratorCode)
	}
	if _, err := s.carriers.FindByCode(ctx, input.CarrierCode); err != nil {
		return model.TransferManifest{}, fmt.Errorf("%w: carrier %s does not exist", ErrInvalidInput, input.CarrierCode)
	}
	current.Name = strings.TrimSpace(input.Name)
	current.GeneratorCode = strings.ToUpper(strings.TrimSpace(input.GeneratorCode))
	current.CarrierCode = strings.ToUpper(strings.TrimSpace(input.CarrierCode))
	current.WasteCode = strings.ToUpper(strings.TrimSpace(input.WasteCode))
	current.QuantityKg = input.QuantityKg
	current.Destination = strings.TrimSpace(input.Destination)
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
	if err := s.repository.UpdateAudited(ctx, id, input.ExpectedVersion, &current, newAuditLog(actor, requestID, "update", "TransferManifest", current.Status, current.Status, "updated draft manifest and evidence")); err != nil {
		return model.TransferManifest{}, fmt.Errorf("update 转运清单: %w", err)
	}
	return s.repository.Get(ctx, id)
}

func (s *transferManifestService) Transition(ctx context.Context, id uint, input dto.TransitionRequest, actor, requestID string) (model.TransferManifest, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.TransferManifest{}, err
	}
	target := strings.TrimSpace(input.Status)
	if !constants.CanTransition(constants.TransferManifestTransitions, current.Status, target) {
		return model.TransferManifest{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.Status, target)
	}
	if target == "submitted" || target == "in_transit" {
		if err := s.validateLinkedParties(ctx, current); err != nil {
			return model.TransferManifest{}, err
		}
	}
	if target == "submitted" {
		return s.submitWithQuota(ctx, current, input, actor, requestID)
	}
	before := current.Status
	current.Status = target
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	if err := s.repository.UpdateAudited(ctx, id, input.ExpectedVersion, &current, newAuditLog(actor, requestID, "transition", "TransferManifest", before, target, input.Reason)); err != nil {
		return model.TransferManifest{}, fmt.Errorf("transition 转运清单: %w", err)
	}
	return s.repository.Get(ctx, id)
}

// submitWithQuota serializes on the linked generator row inside one database
// transaction. If the annual quota would be exceeded the manifest stays a
// draft, a "quota_blocked" audit row is committed, and ErrQuotaExceeded
// carries the used/remaining/requested weight back to the operator.
func (s *transferManifestService) submitWithQuota(ctx context.Context, current model.TransferManifest, input dto.TransitionRequest, actor, requestID string) (model.TransferManifest, error) {
	var quotaErr *ErrQuotaExceeded
	blockedAudit := newAuditLog(actor, requestID, "quota_blocked", "TransferManifest", "draft", "draft", "annual quota exceeded; manifest kept as draft")
	decide := func(generator model.WasteGenerator, manifest model.TransferManifest, usedKg float64) (repository.QuotaDecision, error) {
		if manifest.Status != "draft" {
			return repository.QuotaDecision{}, ErrInvalidTransition
		}
		if generator.AnnualQuotaKg <= 0 {
			return repository.QuotaDecision{Allowed: true, TargetStatus: "submitted", Reason: input.Reason}, nil
		}
		remaining := generator.AnnualQuotaKg - usedKg
		if usedKg+manifest.QuantityKg > generator.AnnualQuotaKg {
			quotaErr = &ErrQuotaExceeded{
				GeneratorCode: generator.Code,
				Year:          manifest.EffectiveAt.UTC().Year(),
				AnnualQuotaKg: generator.AnnualQuotaKg,
				UsedKg:        usedKg,
				RemainingKg:   remaining,
				RequestedKg:   manifest.QuantityKg,
			}
			blockedAudit.Detail = quotaErr.Error()
			return repository.QuotaDecision{
				Allowed:      false,
				TargetStatus: "draft",
				Reason:       quotaErr.Error(),
			}, nil
		}
		return repository.QuotaDecision{Allowed: true, TargetStatus: "submitted", Reason: input.Reason}, nil
	}
	audit := newAuditLog(actor, requestID, "transition", "TransferManifest", "draft", "submitted", input.Reason)
	updated, err := s.repository.SubmitWithQuota(ctx, current.ID, input.ExpectedVersion, decide, audit, blockedAudit)
	if quotaErr != nil {
		// Quota blocked: the blocked audit row was committed, the manifest is
		// still a draft. Re-read so version and timestamps reflect reality.
		draft, getErr := s.repository.Get(ctx, current.ID)
		if getErr == nil {
			_ = s.quota.decorateManifest(ctx, &draft)
		}
		return draft, quotaErr
	}
	if err != nil {
		return model.TransferManifest{}, fmt.Errorf("transition 转运清单: %w", err)
	}
	if err := s.quota.decorateManifest(ctx, &updated); err != nil {
		return model.TransferManifest{}, fmt.Errorf("load manifest quota usage: %w", err)
	}
	return updated, nil
}

func (s *transferManifestService) Delete(ctx context.Context, id uint, actor, requestID string) error {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return err
	}
	if current.Status != "draft" {
		return fmt.Errorf("%w: submitted manifests must be retained for compliance", ErrInvalidInput)
	}
	return s.repository.DeleteAudited(ctx, id, newAuditLog(actor, requestID, "delete", "TransferManifest", current.Status, "deleted", "soft deleted draft manifest"))
}

func (s *transferManifestService) StatusCounts(ctx context.Context) (map[string]int64, error) {
	return s.repository.CountByStatus(ctx)
}

func (s *transferManifestService) validateLinkedParties(ctx context.Context, manifest model.TransferManifest) error {
	generator, err := s.generators.FindByCode(ctx, manifest.GeneratorCode)
	if err != nil {
		return fmt.Errorf("%w: linked generator is unavailable", ErrInvalidInput)
	}
	if generator.Status != "active" || !generator.PermitExpiresAt.After(time.Now().UTC()) {
		return fmt.Errorf("%w: generator permit must be active and unexpired", ErrInvalidInput)
	}
	carrier, err := s.carriers.FindByCode(ctx, manifest.CarrierCode)
	if err != nil {
		return fmt.Errorf("%w: linked carrier is unavailable", ErrInvalidInput)
	}
	if carrier.Status != "verified" || !carrier.LicenseExpiresAt.After(time.Now().UTC()) {
		return fmt.Errorf("%w: carrier license must be verified and unexpired", ErrInvalidInput)
	}
	return nil
}

func validateTransferManifestBusinessFields(code, name, facility, owner, generatorCode, carrierCode, wasteCode, destination, evidence string, quantityKg float64) error {
	if strings.TrimSpace(code) == "" || strings.TrimSpace(name) == "" || strings.TrimSpace(facility) == "" || strings.TrimSpace(owner) == "" || strings.TrimSpace(generatorCode) == "" || strings.TrimSpace(carrierCode) == "" || strings.TrimSpace(wasteCode) == "" || strings.TrimSpace(destination) == "" {
		return fmt.Errorf("%w: manifest identity, parties and route are required", ErrInvalidInput)
	}
	if quantityKg <= 0 || strings.TrimSpace(evidence) == "" {
		return fmt.Errorf("%w: positive waste quantity and manifest evidence are required", ErrInvalidInput)
	}
	return nil
}
