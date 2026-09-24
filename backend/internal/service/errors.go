package service

import "errors"

var (
	ErrInvalidTransition = errors.New("requested status transition is not allowed")
	ErrInvalidInput      = errors.New("business input validation failed")
	ErrUnauthorized      = errors.New("invalid username or password")
	ErrInactiveUser      = errors.New("user account is inactive")
)

// QuotaExceededError mirrors repository.QuotaExceededError so the HTTP layer
// can report annual quota usage without depending on the repository package.
type QuotaExceededError struct {
	GeneratorCode string
	Year          int
	AnnualQuotaKg float64
	UsedKg        float64
	RemainingKg   float64
	AttemptedKg   float64
}

func (e *QuotaExceededError) Error() string { return "annual permit quota exceeded" }
