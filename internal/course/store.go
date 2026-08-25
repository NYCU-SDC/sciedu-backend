package course

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Record struct {
	ID          uuid.UUID
	Code        string
	Title       string
	Description *string
	Status      CourseStatus
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CreateParams struct {
	Code        string
	Title       string
	Description *string
}

type ListFilter struct {
	Status *CourseStatus
	Search *string
}

type ListParams struct {
	ListFilter
	Limit  int32
	Offset int32
}

type UpdateParams struct {
	ID          uuid.UUID
	Code        string
	Title       string
	Description *string
}

type Store struct {
	queries *Queries
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{queries: New(pool)}
}

func (s *Store) Create(ctx context.Context, params CreateParams) (Record, error) {
	row, err := s.queries.CreateCourse(ctx, CreateCourseParams{
		Code:        params.Code,
		Title:       params.Title,
		Description: pgTextFromPtr(params.Description),
	})
	if err != nil {
		return Record{}, err
	}
	return toRecord(row), nil
}

func (s *Store) List(ctx context.Context, params ListParams) ([]Record, error) {
	rows, err := s.queries.ListCourses(ctx, ListCoursesParams{
		Status: pgTextFromStatus(params.Status),
		Search: pgTextFromPtr(params.Search),
		Limit:  params.Limit,
		Offset: params.Offset,
	})
	if err != nil {
		return nil, err
	}

	records := make([]Record, 0, len(rows))
	for _, row := range rows {
		records = append(records, toRecord(row))
	}
	return records, nil
}

func (s *Store) Count(ctx context.Context, filter ListFilter) (int64, error) {
	return s.queries.CountCourses(ctx, CountCoursesParams{
		Status: pgTextFromStatus(filter.Status),
		Search: pgTextFromPtr(filter.Search),
	})
}

func (s *Store) ByID(ctx context.Context, id uuid.UUID) (Record, error) {
	row, err := s.queries.GetCourseByID(ctx, id)
	if err != nil {
		return Record{}, err
	}
	return toRecord(row), nil
}

func (s *Store) CourseForStudent(ctx context.Context, studentID, courseID uuid.UUID) (StudentCourseDecision, error) {
	row, err := s.queries.CourseForStudent(ctx, CourseForStudentParams{
		StudentID: studentID,
		CourseID:  courseID,
	})
	if err != nil {
		return StudentCourseDecision{}, err
	}
	return StudentCourseDecision{
		Course: Record{
			ID:          row.ID,
			Code:        row.Code,
			Title:       row.Title,
			Description: textPtr(row.Description),
			Status:      row.Status,
			CreatedAt:   row.CreatedAt.Time,
			UpdatedAt:   row.UpdatedAt.Time,
		},
		Allowed: row.Allowed,
	}, nil
}

func (s *Store) Update(ctx context.Context, params UpdateParams) (Record, error) {
	row, err := s.queries.UpdateCourse(ctx, UpdateCourseParams{
		Code:        params.Code,
		Title:       params.Title,
		Description: pgTextFromPtr(params.Description),
		ID:          params.ID,
	})
	if err != nil {
		return Record{}, err
	}
	return toRecord(row), nil
}

func (s *Store) UpdateStatus(ctx context.Context, id uuid.UUID, status CourseStatus) (Record, error) {
	row, err := s.queries.UpdateCourseStatus(ctx, UpdateCourseStatusParams{Status: status, ID: id})
	if err != nil {
		return Record{}, err
	}
	return toRecord(row), nil
}

func toRecord(row Course) Record {
	return Record{
		ID:          row.ID,
		Code:        row.Code,
		Title:       row.Title,
		Description: textPtr(row.Description),
		Status:      row.Status,
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}
}

func textPtr(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func pgTextFromPtr(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

func pgTextFromStatus(status *CourseStatus) pgtype.Text {
	if status == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: string(*status), Valid: true}
}
