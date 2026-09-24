package service_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/config"
	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/dto"
	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/model"
	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/repository"
	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/service"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type quotaFixture struct {
	db         *gorm.DB
	manifests  service.TransferManifestService
	generators service.WasteGeneratorService
}

func newQuotaFixture(t *testing.T, quotaKg float64) quotaFixture {
	t.Helper()
	dsn := fmt.Sprintf("file:quota-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sqlite pool: %v", err)
	}
	// Mirror database.Open: a single pooled connection serializes SQLite
	// writers so concurrent submissions queue inside their transactions rather
	// than hitting SQLITE_BUSY/LOCKED.
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&model.User{}, &model.AuditLog{}, &model.WasteGenerator{},
		&model.CarrierProfile{}, &model.TransferManifest{}, &model.ComplianceCheck{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Exec("PRAGMA busy_timeout = 8000").Error; err != nil {
		t.Fatalf("busy timeout: %v", err)
	}

	now := time.Now().UTC()
	generator := model.WasteGenerator{
		BaseModel:    model.BaseModel{Code: "WG-Q", Name: "额度产废单位", Status: "active", Version: 1},
		PermitNumber: "PERMIT-WG-Q", PermitExpiresAt: now.AddDate(2, 0, 0), AnnualQuotaKg: quotaKg,
		WasteCategories: "HW08", Facility: "厂区", Owner: "operator", Category: "常规",
		RiskLevel: "low", EffectiveAt: now, Evidence: "evidence",
	}
	if err := db.Create(&generator).Error; err != nil {
		t.Fatalf("seed generator: %v", err)
	}
	carrier := model.CarrierProfile{
		BaseModel:     model.BaseModel{Code: "CP-Q", Name: "额度承运单位", Status: "verified", Version: 1},
		LicenseNumber: "LIC-CP-Q", LicenseExpiresAt: now.AddDate(2, 0, 0), VehicleCount: 4,
		Facility: "厂区", Owner: "operator", Category: "常规",
		RiskLevel: "low", EffectiveAt: now, Evidence: "evidence",
	}
	if err := db.Create(&carrier).Error; err != nil {
		t.Fatalf("seed carrier: %v", err)
	}

	generatorRepo := repository.NewWasteGeneratorRepository(db)
	carrierRepo := repository.NewCarrierProfileRepository(db)
	manifestRepo := repository.NewTransferManifestRepository(db, generatorRepo)
	securityService := service.NewSecurityService(repository.NewSecurityRepository(db), config.Config{
		JWTSecret: "quota-test-secret-at-least-32-chars-long", TokenTTL: time.Hour,
	})
	generatorService := service.NewWasteGeneratorService(generatorRepo, securityService, manifestRepo)
	manifestService := service.NewTransferManifestService(manifestRepo, generatorRepo, carrierRepo)
	return quotaFixture{db: db, manifests: manifestService, generators: generatorService}
}

func (f quotaFixture) createManifest(t *testing.T, code string, kg float64, effectiveAt time.Time) model.TransferManifest {
	t.Helper()
	input := dto.CreateTransferManifest{
		Code: code, Name: "额度联单 " + code, GeneratorCode: "WG-Q", CarrierCode: "CP-Q",
		WasteCode: "HW08-900-249-08", QuantityKg: kg, Destination: "处置中心",
		Facility: "厂区", Owner: "operator", Category: "常规", RiskLevel: "low",
		EffectiveAt: effectiveAt, Evidence: "minio://evidence/m.pdf",
	}
	item, err := f.manifests.Create(context.Background(), input, "operator", "quota-test")
	if err != nil {
		t.Fatalf("create manifest %s: %v", code, err)
	}
	return item
}

func submit(t *testing.T, f quotaFixture, item model.TransferManifest) model.TransferManifest {
	t.Helper()
	out, err := f.manifests.Transition(context.Background(), item.ID,
		dto.TransitionRequest{Status: "submitted", ExpectedVersion: item.Version, Reason: "submit"}, "operator", "quota-test")
	if err != nil {
		t.Fatalf("submit %s: %v", item.Code, err)
	}
	return out
}

func TestQuotaExceededKeepsDraftWithNumbers(t *testing.T) {
	f := newQuotaFixture(t, 1000)
	now := time.Now().UTC()
	first := submit(t, f, f.createManifest(t, "TM-Q1", 600, now))
	if first.Status != "submitted" {
		t.Fatalf("first manifest should submit, got %s", first.Status)
	}

	second := f.createManifest(t, "TM-Q2", 500, now)
	_, err := f.manifests.Transition(context.Background(), second.ID,
		dto.TransitionRequest{Status: "submitted", ExpectedVersion: second.Version, Reason: "submit"},
		"operator", "quota-test")
	var quotaErr *service.ErrQuotaExceeded
	if !errors.As(err, &quotaErr) {
		t.Fatalf("expected ErrQuotaExceeded, got %v", err)
	}
	if quotaErr.UsedKg != 600 || quotaErr.RemainingKg != 400 || quotaErr.RequestedKg != 500 || quotaErr.Year != now.Year() {
		t.Fatalf("unexpected quota numbers: %+v", quotaErr)
	}
	reloaded, err := f.manifests.Get(context.Background(), second.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Status != "draft" || reloaded.Version != 1 {
		t.Fatalf("oversized manifest must remain an untouched draft, got status=%s version=%d", reloaded.Status, reloaded.Version)
	}
	if reloaded.QuotaUsage == nil || reloaded.QuotaUsage.UsedKg != 600 || reloaded.QuotaUsage.RemainingKg != 400 {
		t.Fatalf("draft response must carry quota usage: %+v", reloaded.QuotaUsage)
	}
}

func TestQuotaExactlyAtLimitIsAllowed(t *testing.T) {
	f := newQuotaFixture(t, 1000)
	now := time.Now().UTC()
	submit(t, f, f.createManifest(t, "TM-E1", 600, now))
	boundary := submit(t, f, f.createManifest(t, "TM-E2", 400, now))
	if boundary.Status != "submitted" {
		t.Fatalf("manifest exactly filling the quota must submit, got %s", boundary.Status)
	}
}

func TestQuotaRejectionReleasesWeight(t *testing.T) {
	f := newQuotaFixture(t, 1000)
	now := time.Now().UTC()
	first := submit(t, f, f.createManifest(t, "TM-R1", 800, now))

	rejected, err := f.manifests.Transition(context.Background(), first.ID,
		dto.TransitionRequest{Status: "rejected", ExpectedVersion: first.Version, Reason: "destination mismatch"},
		"reviewer", "quota-test")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if rejected.Status != "rejected" {
		t.Fatalf("expected rejected, got %s", rejected.Status)
	}
	next := submit(t, f, f.createManifest(t, "TM-R2", 900, now))
	if next.Status != "submitted" {
		t.Fatalf("rejected weight must be released, got status=%s", next.Status)
	}
}

func TestQuotaReceivedHistoryStillCounts(t *testing.T) {
	f := newQuotaFixture(t, 1000)
	now := time.Now().UTC()
	first := submit(t, f, f.createManifest(t, "TM-H1", 700, now))
	inTransit, err := f.manifests.Transition(context.Background(), first.ID,
		dto.TransitionRequest{Status: "in_transit", ExpectedVersion: first.Version, Reason: "ship"},
		"operator", "quota-test")
	if err != nil {
		t.Fatalf("in transit: %v", err)
	}
	received, err := f.manifests.Transition(context.Background(), inTransit.ID,
		dto.TransitionRequest{Status: "received", ExpectedVersion: inTransit.Version, Reason: "signed"},
		"operator", "quota-test")
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if received.Status != "received" {
		t.Fatalf("expected received, got %s", received.Status)
	}
	second := f.createManifest(t, "TM-H2", 400, now)
	_, err = f.manifests.Transition(context.Background(), second.ID,
		dto.TransitionRequest{Status: "submitted", ExpectedVersion: second.Version, Reason: "submit"},
		"operator", "quota-test")
	var quotaErr *service.ErrQuotaExceeded
	if !errors.As(err, &quotaErr) || quotaErr.UsedKg != 700 {
		t.Fatalf("received weight must keep counting, got err=%v", err)
	}
}

func TestQuotaGroupsByEffectiveAtYear(t *testing.T) {
	f := newQuotaFixture(t, 1000)
	thisYear := time.Now().UTC()
	submit(t, f, f.createManifest(t, "TM-Y1", 900, thisYear))
	nextYear := time.Date(thisYear.Year()+1, time.March, 1, 0, 0, 0, 0, time.UTC)
	otherYear := submit(t, f, f.createManifest(t, "TM-Y2", 900, nextYear))
	if otherYear.Status != "submitted" {
		t.Fatalf("next-year manifests must use a fresh annual quota, got %s", otherYear.Status)
	}
	over := f.createManifest(t, "TM-Y3", 200, thisYear)
	_, err := f.manifests.Transition(context.Background(), over.ID,
		dto.TransitionRequest{Status: "submitted", ExpectedVersion: over.Version, Reason: "submit"},
		"operator", "quota-test")
	var quotaErr *service.ErrQuotaExceeded
	if !errors.As(err, &quotaErr) || quotaErr.Year != thisYear.Year() || quotaErr.UsedKg != 900 {
		t.Fatalf("this-year quota must still be full, got err=%v", err)
	}
}

func TestQuotaConcurrentSubmissionsCannotOvershoot(t *testing.T) {
	f := newQuotaFixture(t, 1000)
	now := time.Now().UTC()
	const n = 5
	items := make([]model.TransferManifest, n)
	for i := 0; i < n; i++ {
		items[i] = f.createManifest(t, fmt.Sprintf("TM-C%d", i), 300, now)
	}
	var wg sync.WaitGroup
	results := make([]string, n)
	errs := make([]error, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			out, err := f.manifests.Transition(context.Background(), items[i].ID,
				dto.TransitionRequest{Status: "submitted", ExpectedVersion: items[i].Version, Reason: "concurrent"},
				"operator", "quota-test")
			if err == nil {
				results[i] = out.Status
			} else {
				results[i] = "error"
				errs[i] = err
			}
		}(i)
	}
	close(start)
	wg.Wait()

	allowed, blocked := 0, 0
	for i, status := range results {
		var quotaErr *service.ErrQuotaExceeded
		switch {
		case status == "submitted":
			allowed++
		case errs[i] != nil && errors.As(errs[i], &quotaErr):
			blocked++
		default:
			t.Fatalf("submission %d returned unexpected status=%s err=%v", i, status, errs[i])
		}
	}
	if allowed != 3 || blocked != 2 {
		t.Fatalf("expected exactly 3 allowed and 2 blocked, got allowed=%d blocked=%d (300kg x5 vs 1000kg quota)", allowed, blocked)
	}
	page, err := f.manifests.List(context.Background(), dto.PageQuery{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	used := 0.0
	for _, m := range page.Items {
		if m.Status == "submitted" {
			used += m.QuantityKg
		}
	}
	if used != 900 {
		t.Fatalf("occupied weight must be 900kg, got %.1f", used)
	}
}

func TestGeneratorResponseExposesQuotaUsage(t *testing.T) {
	f := newQuotaFixture(t, 1000)
	now := time.Now().UTC()
	submit(t, f, f.createManifest(t, "TM-D1", 650, now))

	page, err := f.generators.List(context.Background(), dto.PageQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list generators: %v", err)
	}
	var found bool
	for _, g := range page.Items {
		if g.Code == "WG-Q" {
			found = true
			if g.QuotaUsage == nil || g.QuotaUsage.UsedKg != 650 || g.QuotaUsage.RemainingKg != 350 || g.QuotaUsage.AnnualQuotaKg != 1000 {
				t.Fatalf("generator list must expose quota usage: %+v", g.QuotaUsage)
			}
		}
	}
	if !found {
		t.Fatalf("seeded generator missing from list")
	}
}
