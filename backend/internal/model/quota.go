package model

// QuotaUsage is the read-only quota projection shared by 产废单位 and 转运清单
// responses. Weight is aggregated for the natural year (UTC) derived from the
// manifest EffectiveAt and only manifests occupying the permit are counted:
// submitted, in_transit and received. Drafts and rejected manifests never
// occupy quota, so rejecting a manifest releases its weight automatically.
type QuotaUsage struct {
	GeneratorCode string  `json:"generatorCode"`
	Year          int     `json:"year"`
	AnnualQuotaKg float64 `json:"annualQuotaKg"`
	UsedKg        float64 `json:"usedKg"`
	RemainingKg   float64 `json:"remainingKg"`
	// Limited reports whether the generator has an annual cap configured.
	Limited bool `json:"limited"`
}

// Manifest states that occupy the generator annual quota. Received history
// keeps counting, rejected manifests drop out and drafts never enter the sum.
var QuotaOccupyingManifestStates = []string{"submitted", "in_transit", "received"}
