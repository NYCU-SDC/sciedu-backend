package progress

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type transactionDB interface {
	DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

type Store struct {
	queries *Queries
	db      transactionDB
}

func NewStore(db transactionDB) *Store {
	return &Store{queries: New(db), db: db}
}

func (s *Store) UpsertReach(ctx context.Context, studentID, pageID uuid.UUID, reachedAt time.Time) (ProgressRow, error) {
	row, err := s.queries.UpsertReach(ctx, UpsertReachParams{
		StudentID: studentID,
		PageID:    pageID,
		ReachedAt: pgTimestamptz(reachedAt),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProgressRow{}, ErrPageNotFound
		}
		return ProgressRow{}, err
	}
	return progressRowFromDB(row), nil
}

func (s *Store) UpsertComplete(ctx context.Context, studentID, pageID uuid.UUID, completedAt time.Time) (ProgressRow, error) {
	row, err := s.queries.UpsertComplete(ctx, UpsertCompleteParams{
		StudentID:   studentID,
		PageID:      pageID,
		CompletedAt: pgTimestamptz(completedAt),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProgressRow{}, ErrPageNotFound
		}
		return ProgressRow{}, err
	}
	return progressRowFromDB(row), nil
}

func (s *Store) PageByID(ctx context.Context, pageID uuid.UUID) (PageInfo, error) {
	row, err := s.queries.PageByID(ctx, pageID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PageInfo{}, ErrPageNotFound
		}
		return PageInfo{}, err
	}
	return PageInfo{
		ID:           row.ID,
		CourseID:     row.CourseID,
		Title:        row.Title,
		DisplayOrder: row.DisplayOrder,
	}, nil
}

func (s *Store) PagesByCourse(ctx context.Context, courseID uuid.UUID) ([]PageInfo, error) {
	rows, err := s.queries.PagesByCourse(ctx, courseID)
	if err != nil {
		return nil, err
	}
	out := make([]PageInfo, 0, len(rows))
	for _, r := range rows {
		out = append(out, PageInfo{
			ID:           r.ID,
			CourseID:     r.CourseID,
			Title:        r.Title,
			DisplayOrder: r.DisplayOrder,
		})
	}
	return out, nil
}

func (s *Store) ProgressByStudentCourse(ctx context.Context, studentID, courseID uuid.UUID) ([]ProgressRow, error) {
	rows, err := s.queries.ProgressByStudentCourse(ctx, ProgressByStudentCourseParams{
		StudentID: studentID,
		CourseID:  courseID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]ProgressRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, progressRowFromDB(r))
	}
	return out, nil
}

func (s *Store) ListCourseParticipants(ctx context.Context, experimentID uuid.UUID, limit int32, offset int64) ([]Participant, error) {
	rows, err := s.queries.ListCourseParticipants(ctx, ListCourseParticipantsParams{
		ExperimentID: experimentID,
		Limit:        limit,
		Offset:       offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Participant, 0, len(rows))
	for _, r := range rows {
		out = append(out, Participant{
			ID:         r.ID,
			Email:      r.Email,
			Name:       r.Name,
			AvatarURL:  textPtr(r.AvatarUrl),
			AssignedAt: r.AssignedAt.Time.UTC(),
		})
	}
	return out, nil
}

func (s *Store) CountCourseParticipants(ctx context.Context, experimentID uuid.UUID) (int64, error) {
	return s.queries.CountCourseParticipants(ctx, experimentID)
}

func (s *Store) ProgressForParticipants(ctx context.Context, courseID uuid.UUID, studentIDs []uuid.UUID) (map[uuid.UUID][]ProgressRow, error) {
	if len(studentIDs) == 0 {
		return map[uuid.UUID][]ProgressRow{}, nil
	}
	rows, err := s.queries.ProgressForParticipants(ctx, ProgressForParticipantsParams{
		CourseID:   courseID,
		StudentIds: studentIDs,
	})
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID][]ProgressRow, len(studentIDs))
	for _, r := range rows {
		out[r.StudentID] = append(out[r.StudentID], ProgressRow{
			StudentID:   r.StudentID,
			PageID:      r.PageID,
			CourseID:    courseID,
			ReachedAt:   r.ReachedAt.Time.UTC(),
			CompletedAt: timePtr(r.CompletedAt),
		})
	}
	return out, nil
}

func (s *Store) ExperimentCourseExists(ctx context.Context, experimentID, courseID uuid.UUID) (bool, error) {
	return s.queries.ExperimentCourseExists(ctx, ExperimentCourseExistsParams{
		ExperimentID: experimentID,
		CourseID:     courseID,
	})
}

func (s *Store) ParticipantInExperiment(ctx context.Context, experimentID, studentID uuid.UUID) (bool, error) {
	return s.queries.ParticipantInExperiment(ctx, ParticipantInExperimentParams{
		ExperimentID: experimentID,
		StudentID:    studentID,
	})
}

func progressRowFromDB(row PageProgress) ProgressRow {
	return ProgressRow{
		StudentID:   row.StudentID,
		PageID:      row.PageID,
		CourseID:    row.CourseID,
		ReachedAt:   row.ReachedAt.Time.UTC(),
		CompletedAt: timePtr(row.CompletedAt),
	}
}

func pgTimestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func timePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time.UTC()
	return &v
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	v := t.String
	return &v
}
