package question

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type fakeSynchronizationStore struct {
	job                    PendingAnswerSynchronizationsRow
	failures, calls, marks int
	status                 UpdateAnswerResultSyncStatusForVersionParams
}

func (f *fakeSynchronizationStore) PendingAnswerSynchronizations(context.Context) ([]PendingAnswerSynchronizationsRow, error) {
	return []PendingAnswerSynchronizationsRow{f.job}, nil
}
func (f *fakeSynchronizationStore) SynchronizeAnswerResults(_ context.Context, id uuid.UUID, version int64) (int, error) {
	f.calls++
	if id != f.job.QuestionID || version != f.job.Version {
		return 0, errors.New("wrong target")
	}
	if f.calls <= f.failures {
		return 0, errors.New("batch failed")
	}
	return 1, nil
}
func (f *fakeSynchronizationStore) UpdateAnswerResultSyncStatusForVersion(_ context.Context, arg UpdateAnswerResultSyncStatusForVersionParams) (CorrectAnswer, error) {
	f.marks++
	f.status = arg
	return CorrectAnswer{}, nil
}

func TestSynchronizePendingAnswers(t *testing.T) {
	for _, tt := range []struct {
		name                   string
		failures, calls, marks int
	}{
		{"success", 0, 1, 0}, {"retry missing batch", 1, 2, 0}, {"exhausted", 3, 3, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeSynchronizationStore{job: PendingAnswerSynchronizationsRow{QuestionID: uuid.New(), Version: 4}, failures: tt.failures}
			require.NoError(t, SynchronizePendingAnswers(t.Context(), store))
			require.Equal(t, tt.calls, store.calls)
			require.Equal(t, tt.marks, store.marks)
			if tt.marks > 0 {
				require.Equal(t, int64(4), store.status.TargetVersion)
				require.Equal(t, "FAILED", store.status.Status)
			}
		})
	}
	t.Run("cancellation does not mark failed", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		store := &fakeSynchronizationStore{}
		require.ErrorIs(t, SynchronizePendingAnswers(ctx, store), context.Canceled)
		require.Zero(t, store.marks)
	})
}
