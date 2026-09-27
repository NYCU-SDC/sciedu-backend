package question

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// AnswerSynchronizationStore is owned by the consuming Question domain.
type AnswerSynchronizationStore interface {
	PendingAnswerSynchronizations(context.Context) ([]PendingAnswerSynchronizationsRow, error)
	SynchronizeAnswerResults(context.Context, uuid.UUID, int64) (int, error)
	UpdateAnswerResultSyncStatusForVersion(context.Context, UpdateAnswerResultSyncStatusForVersionParams) (CorrectAnswer, error)
}

// RunAnswerSynchronization resumes persisted PENDING work after restart. Each
// batch is bounded; three failed attempts exhaust this run's retry budget.
// FAILED is retried only by an explicit identical or changed CorrectAnswer PUT.
func RunAnswerSynchronization(ctx context.Context, store AnswerSynchronizationStore, logger *zap.Logger) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := SynchronizePendingAnswers(ctx, store); err != nil && ctx.Err() == nil {
			logger.Error("synchronize answer results", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func SynchronizePendingAnswers(ctx context.Context, store AnswerSynchronizationStore) error {
	jobs, err := store.PendingAnswerSynchronizations(ctx)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		var batchErr error
		for attempt := 0; attempt < 3; attempt++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			_, batchErr = store.SynchronizeAnswerResults(ctx, job.QuestionID, job.Version)
			if batchErr == nil {
				break
			}
		}
		if batchErr == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := store.UpdateAnswerResultSyncStatusForVersion(ctx, UpdateAnswerResultSyncStatusForVersionParams{
			QuestionID: job.QuestionID, TargetVersion: job.Version, Status: string(AnswerResultSyncStatusFailed),
		}); err != nil {
			return err
		}
	}
	return nil
}
