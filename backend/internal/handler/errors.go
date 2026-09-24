package handler

import (
	"errors"
	"net/http"

	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/repository"
	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/service"
	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/util"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func handleError(c *gin.Context, err error) {
	var quotaExceeded *service.ErrQuotaExceeded
	switch {
	case errors.As(err, &quotaExceeded):
		c.AbortWithStatusJSON(http.StatusUnprocessableEntity, util.Envelope{
			Error:   "annual_quota_exceeded",
			Message: err.Error(),
			Meta: gin.H{
				"generatorCode": quotaExceeded.GeneratorCode,
				"year":          quotaExceeded.Year,
				"annualQuotaKg": quotaExceeded.AnnualQuotaKg,
				"usedKg":        quotaExceeded.UsedKg,
				"remainingKg":   quotaExceeded.RemainingKg,
				"requestedKg":   quotaExceeded.RequestedKg,
				"keptAsDraft":   true,
			},
		})
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
