package dto

import "time"

// CreateWasteGenerator is the public write contract for 产废单位. Status is deliberately
// omitted so callers cannot bypass the service state machine.
type CreateWasteGenerator struct {
	Code            string    `json:"code" binding:"required,min=2,max=64"`
	Name            string    `json:"name" binding:"required,min=2,max=160"`
	PermitNumber    string    `json:"permitNumber" binding:"required,min=3,max=80"`
	PermitExpiresAt time.Time `json:"permitExpiresAt" binding:"required"`
	AnnualQuotaKg   float64   `json:"annualQuotaKg" binding:"required,gt=0"`
	WasteCategories string    `json:"wasteCategories" binding:"required,max=500"`
	Description     string    `json:"description" binding:"max=1000"`
	Facility        string    `json:"facility" binding:"required,max=120"`
	Owner           string    `json:"owner" binding:"required,max=120"`
	Category        string    `json:"category" binding:"required,max=80"`
	RiskLevel       string    `json:"riskLevel" binding:"required,oneof=low medium high critical"`
	MetricValue     float64   `json:"metricValue"`
	MetricUnit      string    `json:"metricUnit" binding:"max=24"`
	EffectiveAt     time.Time `json:"effectiveAt" binding:"required"`
	Evidence        string    `json:"evidence" binding:"max=2000"`
	RelatedCode     string    `json:"relatedCode" binding:"max=64"`
}

type UpdateWasteGenerator struct {
	ExpectedVersion uint      `json:"expectedVersion" binding:"required"`
	Name            string    `json:"name" binding:"required,min=2,max=160"`
	PermitNumber    string    `json:"permitNumber" binding:"required,min=3,max=80"`
	PermitExpiresAt time.Time `json:"permitExpiresAt" binding:"required"`
	AnnualQuotaKg   float64   `json:"annualQuotaKg" binding:"required,gt=0"`
	WasteCategories string    `json:"wasteCategories" binding:"required,max=500"`
	Description     string    `json:"description" binding:"max=1000"`
	Facility        string    `json:"facility" binding:"required,max=120"`
	Owner           string    `json:"owner" binding:"required,max=120"`
	Category        string    `json:"category" binding:"required,max=80"`
	RiskLevel       string    `json:"riskLevel" binding:"required,oneof=low medium high critical"`
	MetricValue     float64   `json:"metricValue"`
	MetricUnit      string    `json:"metricUnit" binding:"max=24"`
	EffectiveAt     time.Time `json:"effectiveAt" binding:"required"`
	Evidence        string    `json:"evidence" binding:"max=2000"`
	RelatedCode     string    `json:"relatedCode" binding:"max=64"`
}

// QuotaUsage summarizes one generator's annual permit quota. UsedKg sums
// submitted, in-transit and received manifests whose effective date falls in
// Year; rejected and draft manifests never occupy quota. RemainingKg is clamped
// at zero, and ExceededKg carries the deficit when Exceeded is true.
type QuotaUsage struct {
	GeneratorCode string  `json:"generatorCode"`
	GeneratorName string  `json:"generatorName"`
	Year          int     `json:"year"`
	AnnualQuotaKg float64 `json:"annualQuotaKg"`
	UsedKg        float64 `json:"usedKg"`
	RemainingKg   float64 `json:"remainingKg"`
	ExceededKg    float64 `json:"exceededKg"`
	Exceeded      bool    `json:"exceeded"`
}
