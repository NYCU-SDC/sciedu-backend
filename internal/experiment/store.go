package experiment

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Status string

const (
	StatusDraft     Status = "DRAFT"
	StatusScheduled Status = "SCHEDULED"
	StatusActive    Status = "ACTIVE"
	StatusCompleted Status = "COMPLETED"
	StatusArchived  Status = "ARCHIVED"
)

type GradingMode string

const (
	GradingModeAutomatic GradingMode = "AUTOMATIC"
	GradingModeManual    GradingMode = "MANUAL"
)

type CorrectAnswerReleaseMode string

const (
	CorrectAnswerReleaseAfterPageSubmission   CorrectAnswerReleaseMode = "AFTER_PAGE_SUBMISSION"
	CorrectAnswerReleaseAfterCourseCompletion CorrectAnswerReleaseMode = "AFTER_COURSE_COMPLETION"
	CorrectAnswerReleaseNever                 CorrectAnswerReleaseMode = "NEVER"
)

type Configuration struct {
	MaxAttempts              int32                    `json:"maxAttempts"`
	AllowRetry               bool                     `json:"allowRetry"`
	ShowScore                bool                     `json:"showScore"`
	ShowExplanations         bool                     `json:"showExplanations"`
	GradingMode              GradingMode              `json:"gradingMode"`
	CorrectAnswerReleaseMode CorrectAnswerReleaseMode `json:"correctAnswerReleaseMode"`
}

type Record struct {
	ID               uuid.UUID
	CreatedBy        uuid.UUID
	Name             string
	Description      *string
	Configuration    Configuration
	Status           Status
	ScheduledStartAt time.Time
	ScheduledEndAt   time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ParticipantCount int32
	CourseCount      int32
}

type ListFilter struct {
	Status        *Status
	ScheduledFrom *time.Time
	ScheduledTo   *time.Time
	Search        *string
}

type ListParams struct {
	ListFilter
	Limit  int32
	Offset int64
}

type EditableParams struct {
	Name             string
	Description      *string
	Configuration    Configuration
	ScheduledStartAt time.Time
	ScheduledEndAt   time.Time
}

type CreateParams struct {
	EditableParams
	CreatedBy uuid.UUID
}

type UpdateParams struct {
	EditableParams
	ID uuid.UUID
}

type Store struct {
	queries *Queries
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{queries: New(pool)}
}

func (s *Store) List(ctx context.Context, params ListParams) ([]Record, error) {
	rows, err := s.queries.ListExperiments(ctx, ListExperimentsParams{
		Status:        nullExperimentStatus(params.Status),
		ScheduledFrom: pgTimestamptzFromPtr(params.ScheduledFrom),
		ScheduledTo:   pgTimestamptzFromPtr(params.ScheduledTo),
		Search:        pgTextFromPtr(params.Search),
		Limit:         params.Limit,
		Offset:        params.Offset,
	})
	if err != nil {
		return nil, err
	}

	records := make([]Record, 0, len(rows))
	for _, row := range rows {
		record, err := recordFromRow(row)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (s *Store) Count(ctx context.Context, filter ListFilter) (int64, error) {
	return s.queries.CountExperiments(ctx, CountExperimentsParams{
		Status:        nullExperimentStatus(filter.Status),
		ScheduledFrom: pgTimestamptzFromPtr(filter.ScheduledFrom),
		ScheduledTo:   pgTimestamptzFromPtr(filter.ScheduledTo),
		Search:        pgTextFromPtr(filter.Search),
	})
}

// CanAccessCourse reports whether any current ACTIVE experiment grants the
// student access to the requested PUBLISHED course.
func (s *Store) CanAccessCourse(ctx context.Context, studentID, courseID uuid.UUID) (bool, error) {
	return s.queries.StudentCanAccessCourse(ctx, StudentCanAccessCourseParams{
		StudentID: studentID,
		CourseID:  courseID,
	})
}

func (s *Store) Create(ctx context.Context, params CreateParams) (Record, error) {
	configuration, err := json.Marshal(params.Configuration)
	if err != nil {
		return Record{}, fmt.Errorf("marshal experiment configuration: %w", err)
	}
	row, err := s.queries.CreateExperiment(ctx, CreateExperimentParams{
		CreatedBy:        params.CreatedBy,
		Name:             params.Name,
		Description:      pgTextFromPtr(params.Description),
		Configuration:    configuration,
		ScheduledStartAt: pgTimestamptz(params.ScheduledStartAt),
		ScheduledEndAt:   pgTimestamptz(params.ScheduledEndAt),
	})
	if err != nil {
		return Record{}, err
	}
	return recordFromRow(row)
}

func (s *Store) FindByID(ctx context.Context, id uuid.UUID) (Record, error) {
	row, err := s.queries.ExperimentByID(ctx, id)
	if err != nil {
		return Record{}, err
	}
	return s.withCounts(ctx, row)
}

func (s *Store) Update(ctx context.Context, params UpdateParams) (Record, error) {
	configuration, err := json.Marshal(params.Configuration)
	if err != nil {
		return Record{}, fmt.Errorf("marshal experiment configuration: %w", err)
	}
	row, err := s.queries.UpdateExperiment(ctx, UpdateExperimentParams{
		ID:               params.ID,
		Name:             params.Name,
		Description:      pgTextFromPtr(params.Description),
		Configuration:    configuration,
		ScheduledStartAt: pgTimestamptz(params.ScheduledStartAt),
		ScheduledEndAt:   pgTimestamptz(params.ScheduledEndAt),
	})
	if err != nil {
		return Record{}, err
	}
	return s.withCounts(ctx, row)
}

func (s *Store) UpdateStatus(ctx context.Context, id uuid.UUID, status Status) (Record, error) {
	row, err := s.queries.UpdateExperimentStatus(ctx, UpdateExperimentStatusParams{
		ID:     id,
		Status: ExperimentStatus(status),
	})
	if err != nil {
		return Record{}, err
	}
	return s.withCounts(ctx, row)
}

func (s *Store) withCounts(ctx context.Context, row Experiment) (Record, error) {
	record, err := recordFromRow(row)
	if err != nil {
		return Record{}, err
	}
	participantCount, err := s.queries.CountExperimentParticipants(ctx, row.ID)
	if err != nil {
		return Record{}, err
	}
	courseCount, err := s.queries.CountExperimentCourses(ctx, row.ID)
	if err != nil {
		return Record{}, err
	}
	record.ParticipantCount = int32(participantCount)
	record.CourseCount = int32(courseCount)
	return record, nil
}

func recordFromRow(row Experiment) (Record, error) {
	var configuration Configuration
	if err := json.Unmarshal(row.Configuration, &configuration); err != nil {
		return Record{}, fmt.Errorf("unmarshal experiment configuration: %w", err)
	}
	return Record{
		ID:               row.ID,
		CreatedBy:        row.CreatedBy,
		Name:             row.Name,
		Description:      textPtr(row.Description),
		Configuration:    configuration,
		Status:           Status(row.Status),
		ScheduledStartAt: row.ScheduledStartAt.Time,
		ScheduledEndAt:   row.ScheduledEndAt.Time,
		CreatedAt:        row.CreatedAt.Time,
		UpdatedAt:        row.UpdatedAt.Time,
	}, nil
}

func nullExperimentStatus(status *Status) NullExperimentStatus {
	if status == nil {
		return NullExperimentStatus{}
	}
	return NullExperimentStatus{ExperimentStatus: ExperimentStatus(*status), Valid: true}
}

func pgTextFromPtr(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

func pgTimestamptzFromPtr(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return pgTimestamptz(*value)
}

func pgTimestamptz(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

func textPtr(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}
