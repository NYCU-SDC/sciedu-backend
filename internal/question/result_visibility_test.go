package question

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectResultVisibility_TableDriven(t *testing.T) {
	ownerID := uuid.New()
	otherID := uuid.New()
	isCorrect := true
	graded := GradingProjection{Status: GradingStatusGraded, IsCorrect: &isCorrect}
	pending := GradingProjection{Status: GradingStatusPending}

	tests := []struct {
		name         string
		input        ResultVisibilityInput
		wantReadable bool
		wantVisible  bool
		wantCorrect  *bool
	}{
		{
			name: "management can read any current graded result",
			input: ResultVisibilityInput{ViewerKind: ResultViewerManagement, ViewerID: otherID,
				AnswerUserID: ownerID, Grading: graded},
			wantReadable: true, wantVisible: true, wantCorrect: &isCorrect,
		},
		{
			name: "management sees pending without correctness",
			input: ResultVisibilityInput{ViewerKind: ResultViewerManagement, ViewerID: otherID,
				AnswerUserID: ownerID, Grading: pending},
			wantReadable: true, wantVisible: true,
		},
		{
			name: "student own result follows show score true",
			input: ResultVisibilityInput{ViewerKind: ResultViewerStudent, ViewerID: ownerID,
				AnswerUserID: ownerID, ShowScore: true, Grading: graded},
			wantReadable: true, wantVisible: true, wantCorrect: &isCorrect,
		},
		{
			name: "student own result hides correctness when show score false",
			input: ResultVisibilityInput{ViewerKind: ResultViewerStudent, ViewerID: ownerID,
				AnswerUserID: ownerID, ShowScore: false, Grading: graded},
			wantReadable: true,
		},
		{
			name: "student cannot enumerate another user answer",
			input: ResultVisibilityInput{ViewerKind: ResultViewerStudent, ViewerID: otherID,
				AnswerUserID: ownerID, ShowScore: true, Grading: graded},
		},
		{
			name: "unknown viewer is not readable",
			input: ResultVisibilityInput{ViewerKind: "UNKNOWN", ViewerID: ownerID,
				AnswerUserID: ownerID, ShowScore: true, Grading: graded},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projection := ProjectResultVisibility(tt.input)
			assert.Equal(t, tt.wantReadable, projection.Readable)
			assert.Equal(t, tt.wantVisible, projection.ResultVisible)
			if tt.wantCorrect == nil {
				assert.Nil(t, projection.IsCorrect)
				return
			}
			require.NotNil(t, projection.IsCorrect)
			assert.Equal(t, *tt.wantCorrect, *projection.IsCorrect)
		})
	}
}
