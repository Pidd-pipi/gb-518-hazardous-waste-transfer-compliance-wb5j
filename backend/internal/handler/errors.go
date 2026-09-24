package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/repository"
	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/service"
	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/util"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func handleError(c *gin.Context, err error) {
	var quotaErr *service.QuotaExceededError
	if errors.As(err, &quotaErr) {
		// The manifest is intentionally retained as a draft; the response
		// explains used weight, remaining quota and this manifest's weight so
		// the operator can adjust it before resubmitting.
		util.Fail(c, http.StatusUnprocessableEntity, "quota_exceeded",
			formatQuotaMessage(quotaErr))
		return
	}
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		util.Fail(c, http.StatusNotFound, "not_found", "record was not found")
	case errors.Is(err, repository.ErrVersionConflict):
		util.Fail(c, http.StatusConflict, "version_conflict", "record changed; refresh and retry")
	case errors.Is(err, service.ErrInvalidTransition), errors.Is(err, service.ErrInvalidInput):
		util.Fail(c, http.StatusUnprocessableEntity, "business_rule", err.Error())
	default:
		_ = c.Error(err)
		util.Fail(c, http.StatusInternalServerError, "internal_error", "request could not be completed")
	}
}

func formatQuotaMessage(e *service.QuotaExceededError) string {
	return "年度许可额度不足，联单已保留为草稿：" +
		formatQuotaNumber(e.AnnualQuotaKg) + " kg/年；" +
		"已用 " + formatQuotaNumber(e.UsedKg) + " kg，" +
		"剩余 " + formatQuotaNumber(e.RemainingKg) + " kg，" +
		"本次 " + formatQuotaNumber(e.AttemptedKg) + " kg（" +
		e.GeneratorCode + " · " + strconv.Itoa(e.Year) + " 年）"
}

func formatQuotaNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
