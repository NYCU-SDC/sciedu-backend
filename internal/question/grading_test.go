package question

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeterministicChoiceGrading_TableDriven(t *testing.T) {
	correctOptionID := uuid.New()
	gradedAt := time.Date(2026, time.August, 25, 1, 2, 3, 0, time.UTC)
	tests := []struct {
		name             string
		selectedOptionID uuid.UUID
		wantCorrect      bool
	}{
		{name: "matching option is correct", selectedOptionID: correctOptionID, wantCorrect: true},
		{name: "different option is incorrect", selectedOptionID: uuid.New(), wantCorrect: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DeterministicChoiceGrading(tt.selectedOptionID, correctOptionID, 3, gradedAt)
			require.NotNil(t, result.IsCorrect)
			assert.Equal(t, tt.wantCorrect, *result.IsCorrect)
			assert.Equal(t, GradingStatusGraded, result.Status)
			require.NotNil(t, result.Method)
			assert.Equal(t, GradingMethodDeterministic, *result.Method)
			require.NotNil(t, result.CorrectAnswerVersion)
			assert.Equal(t, int64(3), *result.CorrectAnswerVersion)
			require.NotNil(t, result.GradedAt)
			assert.Equal(t, gradedAt, *result.GradedAt)
		})
	}
}

func TestProjectGrading_TableDriven(t *testing.T) {
	method := GradingMethodDeterministic
	correct := true
	gradedAt := time.Now().UTC()
	v1 := int64(1)
	v2 := int64(2)
	manual := GradingMethodManual

	tests := []struct {
		name           string
		result         *PersistedGrading
		currentVersion *int64
		wantStatus     GradingStatus
		wantCorrect    *bool
	}{
		{name: "missing result is pending", currentVersion: &v1, wantStatus: GradingStatusPending},
		{name: "manual ignores changed correct answer", result: &PersistedGrading{Status: GradingStatusGraded, Method: &manual, IsCorrect: &correct, CorrectAnswerVersion: &v1}, currentVersion: &v2, wantStatus: GradingStatusGraded, wantCorrect: &correct},
		{name: "manual needs no correct answer", result: &PersistedGrading{Status: GradingStatusGraded, Method: &manual, IsCorrect: &correct}, wantStatus: GradingStatusGraded, wantCorrect: &correct},
		{
			name:           "current graded result exposes correctness",
			result:         &PersistedGrading{Status: GradingStatusGraded, Method: &method, IsCorrect: &correct, GradedAt: &gradedAt, CorrectAnswerVersion: &v1},
			currentVersion: &v1,
			wantStatus:     GradingStatusGraded,
			wantCorrect:    &correct,
		},
		{
			name:           "older version is pending",
			result:         &PersistedGrading{Status: GradingStatusGraded, Method: &method, IsCorrect: &correct, GradedAt: &gradedAt, CorrectAnswerVersion: &v1},
			currentVersion: &v2,
			wantStatus:     GradingStatusPending,
		},
		{
			name:       "missing current correct answer is pending",
			result:     &PersistedGrading{Status: GradingStatusGraded, Method: &method, IsCorrect: &correct, GradedAt: &gradedAt, CorrectAnswerVersion: &v1},
			wantStatus: GradingStatusPending,
		},
		{
			name:           "pending persisted result never exposes correctness",
			result:         &PersistedGrading{Status: GradingStatusPending, IsCorrect: &correct},
			currentVersion: &v1,
			wantStatus:     GradingStatusPending,
		},
		{
			name:           "failed persisted result never exposes correctness",
			result:         &PersistedGrading{Status: GradingStatusFailed, IsCorrect: &correct},
			currentVersion: &v1,
			wantStatus:     GradingStatusFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projection := ProjectGrading(tt.result, tt.currentVersion)
			assert.Equal(t, tt.wantStatus, projection.Status)
			if tt.wantCorrect == nil {
				assert.Nil(t, projection.IsCorrect)
			} else {
				require.NotNil(t, projection.IsCorrect)
				assert.Equal(t, *tt.wantCorrect, *projection.IsCorrect)
			}
		})
	}
}

// Deletion clears the deterministic result's version before a new CorrectAnswer
// can reuse version 1. Projection must not resurrect the old correctness.
func TestProjectInvalidatedResultAfterCorrectAnswerRecreation(t *testing.T) {
	method := GradingMethodDeterministic
	version := int64(1)
	for _, current := range []*int64{nil, &version} {
		projection := ProjectGrading(&PersistedGrading{Status: GradingStatusPending, Method: &method}, current)
		assert.Equal(t, GradingStatusPending, projection.Status)
		assert.Equal(t, &method, projection.Method)
		assert.Nil(t, projection.IsCorrect)
		assert.Nil(t, projection.GradedAt)
	}
}
