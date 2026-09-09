package question

import (
	"time"

	"github.com/google/uuid"
)

type GradingStatus string

const (
	GradingStatusPending GradingStatus = "PENDING"
	GradingStatusGraded  GradingStatus = "GRADED"
	GradingStatusFailed  GradingStatus = "FAILED"
)

type GradingMethod string

const (
	GradingMethodDeterministic GradingMethod = "DETERMINISTIC"
	GradingMethodManual        GradingMethod = "MANUAL"
	GradingMethodLLM           GradingMethod = "LLM"
)

type PersistedGrading struct {
	Status               GradingStatus
	Method               *GradingMethod
	IsCorrect            *bool
	GradedAt             *time.Time
	CorrectAnswerVersion *int64
}

type GradingProjection struct {
	Status    GradingStatus
	Method    *GradingMethod
	IsCorrect *bool
	GradedAt  *time.Time
}

// DeterministicChoiceGrading compares option identifiers without accessing a clock or database.
// The caller supplies gradedAt and the correct-answer version so the result is deterministic and
// can later be persisted in the same transaction as an Answer.
func DeterministicChoiceGrading(
	selectedOptionID uuid.UUID,
	correctOptionID uuid.UUID,
	correctAnswerVersion int64,
	gradedAt time.Time,
) PersistedGrading {
	isCorrect := selectedOptionID == correctOptionID
	method := GradingMethodDeterministic
	return PersistedGrading{
		Status:               GradingStatusGraded,
		Method:               &method,
		IsCorrect:            &isCorrect,
		GradedAt:             &gradedAt,
		CorrectAnswerVersion: &correctAnswerVersion,
	}
}

// ProjectGrading returns the API-facing grading state without recalculating correctness.
// A missing result or a graded result based on a missing/different correct-answer version is
// projected as PENDING, and non-graded states never expose isCorrect.
func ProjectGrading(result *PersistedGrading, currentCorrectAnswerVersion *int64) GradingProjection {
	if result == nil {
		return GradingProjection{Status: GradingStatusPending}
	}

	projection := GradingProjection{
		Status:   result.Status,
		Method:   result.Method,
		GradedAt: result.GradedAt,
	}
	if result.Status != GradingStatusGraded {
		return projection
	}
	if result.Method != nil && *result.Method == GradingMethodManual {
		projection.IsCorrect = result.IsCorrect
		return projection
	}
	if result.CorrectAnswerVersion == nil ||
		currentCorrectAnswerVersion == nil ||
		*result.CorrectAnswerVersion != *currentCorrectAnswerVersion {
		projection.Status = GradingStatusPending
		return projection
	}

	projection.IsCorrect = result.IsCorrect
	return projection
}
