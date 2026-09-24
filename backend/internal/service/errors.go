package service

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidTransition = errors.New("requested status transition is not allowed")
	ErrInvalidInput      = errors.New("business input validation failed")
	ErrUnauthorized      = errors.New("invalid username or password")
	ErrInactiveUser      = errors.New("user account is inactive")
)

// ErrQuotaExceeded keeps a draft when submitting it would push the generator
// past its annual permit quota. The manifest is NOT transitioned and remains
// editable; the numbers are surfaced to the operator and the UI.
type ErrQuotaExceeded struct {
	GeneratorCode string
	Year          int
	AnnualQuotaKg float64
	UsedKg        float64
	RemainingKg   float64
	RequestedKg   float64
}

func (e *ErrQuotaExceeded) Error() string {
	return fmt.Errorf(
		"%w: 年度许可额度不足，联单已保留为草稿：%s %d 年度额度 %.3f kg，已用 %.3f kg，剩余 %.3f kg，本次 %.3f kg",
		ErrInvalidInput, e.GeneratorCode, e.Year, e.AnnualQuotaKg, e.UsedKg, e.RemainingKg, e.RequestedKg,
	).Error()
}

func (e *ErrQuotaExceeded) Unwrap() error { return ErrInvalidInput }
