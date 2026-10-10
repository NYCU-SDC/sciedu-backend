package question

import "github.com/google/uuid"

type ResultViewerKind string

const (
	ResultViewerStudent    ResultViewerKind = "STUDENT"
	ResultViewerManagement ResultViewerKind = "MANAGEMENT"
)

type ResultVisibilityInput struct {
	ViewerKind   ResultViewerKind
	ViewerID     uuid.UUID
	AnswerUserID uuid.UUID
	ShowScore    bool
	Grading      GradingProjection
}

type ResultVisibilityProjection struct {
	Readable      bool
	ResultVisible bool
	IsCorrect     *bool
}

// ProjectResultVisibility applies ownership and score-visibility rules without
// loading Experiment data or recalculating grading. A student cannot distinguish
// another user's answer from a missing resource because Readable is false. Treating
// showScore=true as ResultVisible=true for pending, failed, or stale projections is
// a reasonable pre-integration assumption that still requires maintainer confirmation.
func ProjectResultVisibility(input ResultVisibilityInput) ResultVisibilityProjection {
	switch input.ViewerKind {
	case ResultViewerManagement:
		return ResultVisibilityProjection{
			Readable:      true,
			ResultVisible: true,
			IsCorrect:     input.Grading.IsCorrect,
		}
	case ResultViewerStudent:
		if input.ViewerID != input.AnswerUserID {
			return ResultVisibilityProjection{}
		}
		projection := ResultVisibilityProjection{
			Readable:      true,
			ResultVisible: input.ShowScore,
		}
		if input.ShowScore {
			projection.IsCorrect = input.Grading.IsCorrect
		}
		return projection
	default:
		return ResultVisibilityProjection{}
	}
}
