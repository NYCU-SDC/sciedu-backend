package experiment

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
)

type stubCurrentExperimentQuerier struct {
	id  uuid.UUID
	err error
}

func (s stubCurrentExperimentQuerier) CurrentExperimentForStudent(_ context.Context, _ uuid.UUID) (uuid.UUID, error) {
	return s.id, s.err
}

func TestCurrentExperimentForStudent(t *testing.T) {
	studentID := uuid.New()
	expID := uuid.New()
	boom := errors.New("boom")

	tests := []struct {
		name      string
		stub      stubCurrentExperimentQuerier
		wantID    uuid.UUID
		wantFound bool
		wantErr   error
	}{
		{
			name:      "found",
			stub:      stubCurrentExperimentQuerier{id: expID},
			wantID:    expID,
			wantFound: true,
		},
		{
			name:      "no rows maps to found=false, nil error",
			stub:      stubCurrentExperimentQuerier{err: pgx.ErrNoRows},
			wantID:    uuid.Nil,
			wantFound: false,
		},
		{
			name:    "other error propagates",
			stub:    stubCurrentExperimentQuerier{err: boom},
			wantErr: boom,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, found, err := resolveCurrentExperimentForStudent(context.Background(), tc.stub, studentID)
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.wantID, id)
			assert.Equal(t, tc.wantFound, found)
		})
	}
}
