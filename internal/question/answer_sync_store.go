package question

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// UpsertCorrectAnswer shares the question lock with submission and batch sync.
// The lock also covers initial creation, when there is no correct-answer row yet.
func (s *Store) UpsertCorrectAnswer(ctx context.Context, arg UpsertCorrectAnswerParams) (CorrectAnswer, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CorrectAnswer{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.WithTx(tx)
	if _, err := q.LockAnswerQuestion(ctx, arg.QuestionID); err != nil {
		return CorrectAnswer{}, err
	}
	request := CorrectAnswerRequest{Type: arg.Type}
	if arg.SelectedOptionID.Valid {
		id := uuid.UUID(arg.SelectedOptionID.Bytes)
		request.SelectedOptionID = &id
	}
	if arg.ReferenceAnswer.Valid {
		request.ReferenceAnswer = &arg.ReferenceAnswer.String
	}
	currentQuestion, err := q.GetQuestion(ctx, arg.QuestionID)
	if err != nil {
		return CorrectAnswer{}, err
	}
	if err := validateCorrectAnswerRequest(currentQuestion.Type, request); err != nil {
		return CorrectAnswer{}, err
	}
	if request.SelectedOptionID != nil {
		option, err := q.GetOption(ctx, *request.SelectedOptionID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && option.QuestionID != currentQuestion.ID) {
			return CorrectAnswer{}, fmt.Errorf("%w: selected option does not belong to the question", errInvalidCorrectAnswerPayload)
		}
		if err != nil {
			return CorrectAnswer{}, err
		}
	}
	answer, err := q.UpsertCorrectAnswer(ctx, arg)
	if err != nil {
		return CorrectAnswer{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CorrectAnswer{}, err
	}
	return answer, nil
}

// SynchronizeAnswerResults processes a bounded batch, not a GET-side projection.
// Missing results and stale results use the same guarded persistence operation.
// A scheduler must retry failed batches and mark FAILED only after exhaustion.
func (s *Store) SynchronizeAnswerResults(ctx context.Context, questionID uuid.UUID, version int64) (int, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.WithTx(tx)
	if _, err := q.LockAnswerQuestion(ctx, questionID); err != nil {
		return 0, err
	}
	correct, err := q.GetCorrectAnswer(ctx, questionID)
	if err != nil {
		return 0, err
	}
	if correct.Version != version {
		return 0, nil
	}
	params := ListAnswerSynchronizationCandidatesParams{QuestionID: questionID, TargetVersion: version, BatchSize: 100}
	rows, err := q.ListAnswerSynchronizationCandidates(ctx, params)
	if err != nil {
		return 0, err
	}
	for _, row := range rows {
		grading := DeterministicChoiceGrading(uuid.UUID(row.SelectedOptionID.Bytes), uuid.UUID(row.CorrectOptionID.Bytes), version, time.Now().UTC())
		result := createAnswerResultParams(row.ID, grading)
		_, err := q.RegradeAnswerResultForVersion(ctx, RegradeAnswerResultForVersionParams{
			AnswerID: row.ID, Status: result.Status, Method: result.Method, IsCorrect: result.IsCorrect,
			GradedAt: result.GradedAt, TargetVersion: version,
		})
		if err != nil {
			return 0, err
		}
	}
	params.BatchSize = 1
	remaining, err := q.ListAnswerSynchronizationCandidates(ctx, params)
	if err != nil {
		return 0, err
	}
	status := AnswerResultSyncStatusPending
	if len(remaining) == 0 {
		status = AnswerResultSyncStatusSynced
	}
	if _, err := q.UpdateAnswerResultSyncStatusForVersion(ctx, UpdateAnswerResultSyncStatusForVersionParams{
		QuestionID: questionID, TargetVersion: version, Status: string(status),
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(rows), nil
}
