package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/config"
	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/database"
	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/router"
	"github.com/gin-gonic/gin"
)

type quotaHarness struct {
	t        *testing.T
	engine   http.Handler
	operator string
}

func newQuotaHarness(t *testing.T, dsn string) quotaHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := config.Config{
		AppName: "quota-test", Environment: "test", Port: "0",
		DatabaseDriver: "sqlite", DatabaseDSN: dsn,
		JWTSecret: "quota-test-secret-at-least-32-characters", TokenTTL: time.Hour,
		RequestLimit: 10000, StartupTimeout: 5 * time.Second, ShutdownTimeout: 5 * time.Second,
		ReadHeaderTimeout: time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 10 * time.Second,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, _, err := database.Open(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	engine := router.New(cfg, db, nil, logger)
	return quotaHarness{t: t, engine: engine, operator: login(t, engine, "operator")}
}

func quotaGeneratorPayload(code string, quota float64) map[string]any {
	now := time.Now().UTC().Format(time.RFC3339)
	return map[string]any{
		"code": code, "name": "额度测试产废单位", "permitNumber": "PERMIT-QUOTA-" + code,
		"permitExpiresAt": time.Now().UTC().AddDate(1, 0, 0).Format(time.RFC3339),
		"annualQuotaKg":   quota, "wasteCategories": "HW08 废矿物油",
		"facility": "额度测试区域", "owner": "operator", "category": "危废转运",
		"riskLevel": "medium", "metricValue": 1, "metricUnit": "unit",
		"effectiveAt": now, "evidence": "minio://evidence/tests/generator.pdf",
	}
}

func quotaManifestPayload(code, generator string, weight float64, effectiveAt time.Time) map[string]any {
	return map[string]any{
		"code": code, "name": "额度测试联单", "description": "annual quota workflow",
		"generatorCode": generator, "carrierCode": "CP-002", "wasteCode": "HW08-900-249-08",
		"quantityKg": weight, "destination": "合规处置中心 A", "facility": "东区危废暂存区",
		"owner": "operator", "category": "危废转运", "riskLevel": "medium",
		"metricValue": weight, "metricUnit": "kg", "effectiveAt": effectiveAt.Format(time.RFC3339),
		"evidence": "minio://evidence/tests/manifest.pdf",
	}
}

func (h quotaHarness) createGenerator(code string, quota float64) {
	h.t.Helper()
	response, _ := request(h.t, h.engine, http.MethodPost, "/api/generators", h.operator, "quota-generator-create", quotaGeneratorPayload(code, quota))
	assertStatus(h.t, response, http.StatusCreated)
}

func (h quotaHarness) createManifest(code, generator string, weight float64, effectiveAt time.Time) record {
	h.t.Helper()
	response, body := request(h.t, h.engine, http.MethodPost, "/api/manifests", h.operator, "quota-manifest-create", quotaManifestPayload(code, generator, weight, effectiveAt))
	assertStatus(h.t, response, http.StatusCreated)
	return decodeRecord(h.t, body)
}

func (h quotaHarness) transition(id uint, version uint, status, requestID string) (*http.Response, []byte) {
	h.t.Helper()
	return request(h.t, h.engine, http.MethodPost, fmt.Sprintf("/api/manifests/%d/transition", id), h.operator, requestID, map[string]any{
		"status": status, "expectedVersion": version, "reason": "quota test transition",
	})
}

func quotaEntry(t *testing.T, engine http.Handler, token, generator string, year int) map[string]any {
	t.Helper()
	response, body := request(t, engine, http.MethodGet, fmt.Sprintf("/api/quota-usage?year=%d", year), token, "", nil)
	assertStatus(t, response, http.StatusOK)
	var envelope struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode quota usage: %v body=%s", err, string(body))
	}
	for _, entry := range envelope.Data {
		if entry["generatorCode"] == generator && int(entry["year"].(float64)) == year {
			return entry
		}
	}
	t.Fatalf("quota entry for %s %d not found in %s", generator, year, string(body))
	return nil
}

func TestAnnualQuotaBlocksOverLimitAndReleasesOnRejection(t *testing.T) {
	h := newQuotaHarness(t, "file:quota-block?mode=memory&cache=shared")
	const generator = "WG-QUOTA-1"
	h.createGenerator(generator, 1000)
	now := time.Now().UTC()

	first := h.createManifest("TM-QUOTA-1", generator, 600, now)
	response, body := h.transition(first.ID, first.Version, "submitted", "quota-submit-ok")
	assertStatus(t, response, http.StatusOK)
	first = decodeRecord(t, body)

	entry := quotaEntry(t, h.engine, h.operator, generator, now.Year())
	if entry["usedKg"].(float64) != 600 || entry["remainingKg"].(float64) != 400 || entry["exceeded"].(bool) {
		t.Fatalf("unexpected quota usage after submit: %+v", entry)
	}

	// 600 + 500 would cross the 1000 kg cap: the manifest stays a draft and
	// the error reports used, remaining and attempted weights.
	over := h.createManifest("TM-QUOTA-2", generator, 500, now)
	response, body = h.transition(over.ID, over.Version, "submitted", "quota-submit-blocked")
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for over-limit submit, got %d", response.StatusCode)
	}
	var failure struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &failure); err != nil || failure.Error != "quota_exceeded" {
		t.Fatalf("expected quota_exceeded error, got %s", string(body))
	}
	if !bytes.Contains(body, []byte("已用")) || !bytes.Contains(body, []byte("剩余")) || !bytes.Contains(body, []byte("本次")) {
		t.Fatalf("quota error must explain used/remaining/attempted weight: %s", string(body))
	}
	blocked, _ := request(t, h.engine, http.MethodGet, fmt.Sprintf("/api/manifests/%d", over.ID), h.operator, "", nil)
	assertStatus(t, blocked, http.StatusOK)

	// Exactly the remaining 400 kg must be accepted.
	exact := h.createManifest("TM-QUOTA-3", generator, 400, now)
	response, _ = h.transition(exact.ID, exact.Version, "submitted", "quota-submit-exact")
	assertStatus(t, response, http.StatusOK)

	// Receiving a manifest keeps its weight in the historical usage total.
	exact = decodeRecord(t, mustGetBody(t, h, exact.ID))
	response, body = h.transition(exact.ID, exact.Version, "in_transit", "quota-in-transit")
	assertStatus(t, response, http.StatusOK)
	exact = decodeRecord(t, body)
	response, _ = h.transition(exact.ID, exact.Version, "received", "quota-received")
	assertStatus(t, response, http.StatusOK)
	entry = quotaEntry(t, h.engine, h.operator, generator, now.Year())
	if entry["usedKg"].(float64) != 1000 {
		t.Fatalf("received history must keep counting, got %+v", entry)
	}

	// Rejecting the in-flight first manifest releases its 600 kg hold; the
	// received history alone keeps occupying the cap. The formerly blocked
	// 500 kg draft then fits again and can be resubmitted.
	response, body = h.transition(first.ID, first.Version, "in_transit", "quota-first-transit")
	assertStatus(t, response, http.StatusOK)
	first = decodeRecord(t, body)
	response, _ = h.transition(first.ID, first.Version, "rejected", "quota-reject-release")
	assertStatus(t, response, http.StatusOK)
	entry = quotaEntry(t, h.engine, h.operator, generator, now.Year())
	if entry["usedKg"].(float64) != 400 || entry["remainingKg"].(float64) != 600 {
		t.Fatalf("rejected manifest must release quota, got %+v", entry)
	}

	response, _ = h.transition(over.ID, over.Version, "submitted", "quota-resubmit")
	assertStatus(t, response, http.StatusOK)
	entry = quotaEntry(t, h.engine, h.operator, generator, now.Year())
	if entry["usedKg"].(float64) != 900 {
		t.Fatalf("resubmitted draft must occupy released quota, got %+v", entry)
	}
}

func mustGetBody(t *testing.T, h quotaHarness, id uint) []byte {
	t.Helper()
	response, body := request(t, h.engine, http.MethodGet, fmt.Sprintf("/api/manifests/%d", id), h.operator, "", nil)
	assertStatus(t, response, http.StatusOK)
	return body
}

func TestAnnualQuotaIsBucketedByEffectiveYear(t *testing.T) {
	h := newQuotaHarness(t, "file:quota-year?mode=memory&cache=shared")
	const generator = "WG-QUOTA-2"
	h.createGenerator(generator, 1000)
	now := time.Now().UTC()

	current := h.createManifest("TM-YEAR-1", generator, 900, now)
	response, _ := h.transition(current.ID, current.Version, "submitted", "quota-year-current")
	assertStatus(t, response, http.StatusOK)

	// Same weight in the next natural year uses a fresh quota bucket.
	next := h.createManifest("TM-YEAR-2", generator, 900, now.AddDate(1, 0, 1))
	response, _ = h.transition(next.ID, next.Version, "submitted", "quota-year-next")
	assertStatus(t, response, http.StatusOK)

	currentEntry := quotaEntry(t, h.engine, h.operator, generator, now.Year())
	if currentEntry["usedKg"].(float64) != 900 || currentEntry["remainingKg"].(float64) != 100 {
		t.Fatalf("current year bucket mismatch: %+v", currentEntry)
	}
	nextEntry := quotaEntry(t, h.engine, h.operator, generator, now.AddDate(1, 0, 1).Year())
	if nextEntry["usedKg"].(float64) != 900 {
		t.Fatalf("next year bucket mismatch: %+v", nextEntry)
	}
}

func TestConcurrentSubmissionsCannotBothCrossQuota(t *testing.T) {
	h := newQuotaHarness(t, "file:quota-concurrent?mode=memory&cache=shared")
	const generator = "WG-QUOTA-3"
	h.createGenerator(generator, 1000)
	now := time.Now().UTC()

	// Seed 600 kg already submitted; two 300 kg drafts race for the 400 kg
	// remaining: one succeeds, the other must be retained as a draft.
	seed := h.createManifest("TM-RACE-0", generator, 600, now)
	response, _ := h.transition(seed.ID, seed.Version, "submitted", "quota-race-seed")
	assertStatus(t, response, http.StatusOK)

	drafts := make([]record, 0, 4)
	for i := 1; i <= 4; i++ {
		drafts = append(drafts, h.createManifest(fmt.Sprintf("TM-RACE-%d", i), generator, 300, now))
	}

	statuses := make(chan int, len(drafts))
	var wg sync.WaitGroup
	for _, draft := range drafts {
		wg.Add(1)
		go func(item record) {
			defer wg.Done()
			response, _ := h.transition(item.ID, item.Version, "submitted", "quota-race-submit")
			statuses <- response.StatusCode
		}(draft)
	}
	wg.Wait()
	close(statuses)

	ok, blocked := 0, 0
	for status := range statuses {
		switch status {
		case http.StatusOK:
			ok++
		case http.StatusUnprocessableEntity:
			blocked++
		default:
			t.Fatalf("unexpected concurrent status %d", status)
		}
	}
	if ok != 1 || blocked != len(drafts)-1 {
		t.Fatalf("expected exactly one concurrent submit to pass, got ok=%d blocked=%d", ok, blocked)
	}

	entry := quotaEntry(t, h.engine, h.operator, generator, now.Year())
	if entry["usedKg"].(float64) != 900 {
		t.Fatalf("concurrent submissions overshot the cap: %+v", entry)
	}
}
