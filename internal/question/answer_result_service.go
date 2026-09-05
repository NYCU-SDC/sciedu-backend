package question

import (
	"context"
	"time"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
)

type AnswerResultQuerier interface {
	FindAnswerResultByQuestionAndID(
		ctx context.Context,
		arg FindAnswerResultByQuestionAndIDParams,
	) (FindAnswerResultByQuestionAndIDRow, error)
}

type AnswerResultAccess struct {
	ViewerKind ResultViewerKind
	ViewerID   uuid.UUID
}

type AnswerResultView struct {
	AnswerID      uuid.UUID
	QuestionID    uuid.UUID
	Status        GradingStatus
	Method        *GradingMethod
	ResultVisible bool
	IsCorrect     *bool
	GradedAt      *time.Time
}

type AnswerResultService struct {
	visibility ResultVisibilityResolver
	querier    AnswerResultQuerier
	logger     *zap.Logger
}

func NewAnswerResultService(querier AnswerResultQuerier, visibility ResultVisibilityResolver, logger *zap.Logger) *AnswerResultService {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &AnswerResultService{querier: querier, visibility: visibility, logger: logger}
}

// Get performs only persisted-result lookup, ownership enforcement, and
// projection. Visibility is resolved from the persisted Answer's Experiment.
func (s *AnswerResultService) Get(
	ctx context.Context,
	questionID, answerID uuid.UUID,
	access AnswerResultAccess,
) (AnswerResultView, error) {
	row, err := s.querier.FindAnswerResultByQuestionAndID(ctx, FindAnswerResultByQuestionAndIDParams{
		QuestionID: questionID,
		AnswerID:   answerID,
	})
	if err != nil {
		return AnswerResultView{}, databaseutil.WrapDBErrorWithKeyValue(
			err,
			"answers",
			"id",
			answerID.String(),
			s.logger,
			"get answer result",
		)
	}

	if access.ViewerKind != ResultViewerManagement &&
		(access.ViewerKind != ResultViewerStudent || access.ViewerID != row.UserID) {
		return AnswerResultView{}, databaseutil.WrapDBErrorWithKeyValue(pgx.ErrNoRows, "answers", "id", answerID.String(), s.logger, "get answer result")
	}
	showScore := false
	if access.ViewerKind == ResultViewerStudent {
		showScore, err = s.visibility.ResolveResultVisibility(ctx, row.ExperimentID)
		if err != nil {
			return AnswerResultView{}, databaseutil.WrapDBErrorWithKeyValue(err, "experiments", "id", row.ExperimentID.String(), s.logger, "resolve result visibility")
		}
	}
	grading := projectAnswerResultRow(row)
	visibility := ProjectResultVisibility(ResultVisibilityInput{
		ViewerKind:   access.ViewerKind,
		ViewerID:     access.ViewerID,
		AnswerUserID: row.UserID,
		ShowScore:    showScore,
		Grading:      grading,
	})
	if !visibility.Readable {
		return AnswerResultView{}, databaseutil.WrapDBErrorWithKeyValue(
			pgx.ErrNoRows,
			"answers",
			"id",
			answerID.String(),
			s.logger,
			"get answer result",
		)
	}

	return AnswerResultView{
		AnswerID:      row.AnswerID,
		QuestionID:    row.QuestionID,
		Status:        grading.Status,
		Method:        grading.Method,
		ResultVisible: visibility.ResultVisible,
		IsCorrect:     visibility.IsCorrect,
		GradedAt:      grading.GradedAt,
	}, nil
}

func projectAnswerResultRow(row FindAnswerResultByQuestionAndIDRow) GradingProjection {
	var persisted *PersistedGrading
	if row.GradingStatus.Valid {
		status := GradingStatus(row.GradingStatus.String)
		persisted = &PersistedGrading{Status: status}
		if row.GradingMethod.Valid {
			method := GradingMethod(row.GradingMethod.String)
			persisted.Method = &method
		}
		if row.IsCorrect.Valid {
			isCorrect := row.IsCorrect.Bool
			persisted.IsCorrect = &isCorrect
		}
		if row.GradedAt.Valid {
			gradedAt := row.GradedAt.Time
			persisted.GradedAt = &gradedAt
		}
		if row.CorrectAnswerVersion.Valid {
			version := row.CorrectAnswerVersion.Int64
			persisted.CorrectAnswerVersion = &version
		}
	}

	var currentVersion *int64
	if row.CurrentCorrectAnswerVersion.Valid {
		version := row.CurrentCorrectAnswerVersion.Int64
		currentVersion = &version
	}
	return ProjectGrading(persisted, currentVersion)
}
