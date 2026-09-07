package experiment

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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

type Participant struct {
	ID         uuid.UUID
	Email      string
	Name       string
	AvatarURL  *string
	Roles      []string
	DisabledAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type ParticipantAssignment struct {
	Participant Participant
	AssignedAt  time.Time
}

type AssignedCourse struct {
	ID          uuid.UUID
	Code        string
	Title       string
	Description *string
	Status      CourseStatus
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CourseAssignment struct {
	Course   AssignedCourse
	LinkedAt time.Time
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

func (s *Store) WithinTx(ctx context.Context, fn func(MutationRepository) error) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	txStore := &Store{queries: s.queries.WithTx(tx)}
	if err := fn(txStore); err != nil {
		return err
	}

	return tx.Commit(ctx)
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

func (s *Store) ListParticipants(ctx context.Context, experimentID uuid.UUID, limit int32, offset int64) ([]ParticipantAssignment, error) {
	rows, err := s.queries.ListExperimentParticipants(ctx, ListExperimentParticipantsParams{
		ExperimentID: experimentID,
		Limit:        limit,
		Offset:       offset,
	})
	if err != nil {
		return nil, err
	}

	assignments := make([]ParticipantAssignment, 0, len(rows))
	for _, row := range rows {
		assignments = append(assignments, participantAssignment(
			row.ID,
			row.Email,
			row.Name,
			row.AvatarUrl,
			row.Roles,
			row.CreatedAt,
			row.UpdatedAt,
			row.AssignedAt,
		))
	}
	return assignments, nil
}

func (s *Store) CountParticipants(ctx context.Context, experimentID uuid.UUID) (int64, error) {
	return s.queries.CountExperimentParticipants(ctx, experimentID)
}

func (s *Store) ListCourses(ctx context.Context, experimentID uuid.UUID, limit int32, offset int64) ([]CourseAssignment, error) {
	rows, err := s.queries.ListExperimentCourses(ctx, ListExperimentCoursesParams{
		ExperimentID: experimentID,
		Limit:        limit,
		Offset:       offset,
	})
	if err != nil {
		return nil, err
	}

	assignments := make([]CourseAssignment, 0, len(rows))
	for _, row := range rows {
		assignments = append(assignments, courseAssignment(
			row.ID, row.Code, row.Title, row.Description, row.Status,
			row.CreatedAt, row.UpdatedAt, row.LinkedAt,
		))
	}
	return assignments, nil
}

func (s *Store) CountCourses(ctx context.Context, experimentID uuid.UUID) (int64, error) {
	return s.queries.CountExperimentCourses(ctx, experimentID)
}

func (s *Store) StudentExperimentAccessible(ctx context.Context, experimentID, studentID uuid.UUID) (bool, error) {
	return s.queries.StudentExperimentAccessible(ctx, StudentExperimentAccessibleParams{
		ExperimentID: experimentID,
		StudentID:    studentID,
	})
}

func (s *Store) ListStudentCourses(ctx context.Context, experimentID uuid.UUID, limit int32, offset int64) ([]CourseAssignment, error) {
	rows, err := s.queries.ListStudentExperimentCourses(ctx, ListStudentExperimentCoursesParams{
		ExperimentID: experimentID,
		Limit:        limit,
		Offset:       offset,
	})
	if err != nil {
		return nil, err
	}

	assignments := make([]CourseAssignment, 0, len(rows))
	for _, row := range rows {
		assignments = append(assignments, courseAssignment(
			row.ID, row.Code, row.Title, row.Description, row.Status,
			row.CreatedAt, row.UpdatedAt, row.LinkedAt,
		))
	}
	return assignments, nil
}

func (s *Store) CountStudentCourses(ctx context.Context, experimentID uuid.UUID) (int64, error) {
	return s.queries.CountStudentExperimentCourses(ctx, experimentID)
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

func (s *Store) LockByID(ctx context.Context, id uuid.UUID) (Record, error) {
	row, err := s.queries.LockExperimentByID(ctx, id)
	if err != nil {
		return Record{}, err
	}
	return recordFromRow(row)
}

func (s *Store) LockParticipantUsers(ctx context.Context, experimentID uuid.UUID) ([]uuid.UUID, error) {
	return s.queries.LockExperimentParticipantUsers(ctx, experimentID)
}

func (s *Store) LockParticipantCandidates(ctx context.Context, userIDs []uuid.UUID) ([]Participant, error) {
	rows, err := s.queries.LockParticipantCandidates(ctx, userIDs)
	if err != nil {
		return nil, err
	}

	participants := make([]Participant, 0, len(rows))
	for _, row := range rows {
		participants = append(participants, participantFromFields(
			row.ID,
			row.Email,
			row.Name,
			row.AvatarUrl,
			row.Roles,
			timePtr(row.DisabledAt),
			row.CreatedAt,
			row.UpdatedAt,
		))
	}
	return participants, nil
}

func (s *Store) LockCourseCandidates(ctx context.Context, courseIDs []uuid.UUID) ([]AssignedCourse, error) {
	rows, err := s.queries.LockCourseCandidates(ctx, courseIDs)
	if err != nil {
		return nil, err
	}

	courses := make([]AssignedCourse, 0, len(rows))
	for _, row := range rows {
		courses = append(courses, assignedCourse(
			row.ID, row.Code, row.Title, row.Description, row.Status, row.CreatedAt, row.UpdatedAt,
		))
	}
	return courses, nil
}

func (s *Store) HasParticipantScheduleConflict(
	ctx context.Context,
	experimentID uuid.UUID,
	userIDs []uuid.UUID,
	start time.Time,
	end time.Time,
) (bool, error) {
	return s.queries.HasParticipantScheduleConflict(ctx, HasParticipantScheduleConflictParams{
		UserIds:          userIDs,
		ExperimentID:     experimentID,
		ScheduledEndAt:   pgTimestamptz(end),
		ScheduledStartAt: pgTimestamptz(start),
	})
}

func (s *Store) AddParticipants(ctx context.Context, experimentID uuid.UUID, userIDs []uuid.UUID) ([]ParticipantAssignment, error) {
	rows, err := s.queries.AddExperimentParticipants(ctx, AddExperimentParticipantsParams{
		ExperimentID: experimentID,
		UserIds:      userIDs,
	})
	if err != nil {
		return nil, err
	}

	assignments := make([]ParticipantAssignment, 0, len(rows))
	for _, row := range rows {
		assignments = append(assignments, participantAssignment(
			row.ID,
			row.Email,
			row.Name,
			row.AvatarUrl,
			row.Roles,
			row.CreatedAt,
			row.UpdatedAt,
			row.AssignedAt,
		))
	}
	return assignments, nil
}

func (s *Store) RemoveParticipant(ctx context.Context, experimentID, userID uuid.UUID) error {
	_, err := s.queries.RemoveExperimentParticipant(ctx, RemoveExperimentParticipantParams{
		ExperimentID: experimentID,
		UserID:       userID,
	})
	return err
}

func (s *Store) AddCourses(ctx context.Context, experimentID uuid.UUID, courseIDs []uuid.UUID) ([]CourseAssignment, error) {
	rows, err := s.queries.AddExperimentCourses(ctx, AddExperimentCoursesParams{
		ExperimentID: experimentID,
		CourseIds:    courseIDs,
	})
	if err != nil {
		return nil, err
	}

	assignments := make([]CourseAssignment, 0, len(rows))
	for _, row := range rows {
		assignments = append(assignments, courseAssignment(
			row.ID, row.Code, row.Title, row.Description, row.Status,
			row.CreatedAt, row.UpdatedAt, row.LinkedAt,
		))
	}
	return assignments, nil
}

func (s *Store) RemoveCourse(ctx context.Context, experimentID, courseID uuid.UUID) error {
	_, err := s.queries.RemoveExperimentCourse(ctx, RemoveExperimentCourseParams{
		ExperimentID: experimentID,
		CourseID:     courseID,
	})
	return err
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

func timePtr(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

func participantFromFields(
	id uuid.UUID,
	email string,
	name string,
	avatarURL pgtype.Text,
	roles []string,
	disabledAt *time.Time,
	createdAt pgtype.Timestamptz,
	updatedAt pgtype.Timestamptz,
) Participant {
	return Participant{
		ID:         id,
		Email:      email,
		Name:       name,
		AvatarURL:  textPtr(avatarURL),
		Roles:      roles,
		DisabledAt: disabledAt,
		CreatedAt:  createdAt.Time,
		UpdatedAt:  updatedAt.Time,
	}
}

func participantAssignment(
	id uuid.UUID,
	email string,
	name string,
	avatarURL pgtype.Text,
	roles []string,
	createdAt pgtype.Timestamptz,
	updatedAt pgtype.Timestamptz,
	assignedAt pgtype.Timestamptz,
) ParticipantAssignment {
	return ParticipantAssignment{
		Participant: participantFromFields(id, email, name, avatarURL, roles, nil, createdAt, updatedAt),
		AssignedAt:  assignedAt.Time,
	}
}

func assignedCourse(
	id uuid.UUID,
	code string,
	title string,
	description pgtype.Text,
	status CourseStatus,
	createdAt pgtype.Timestamptz,
	updatedAt pgtype.Timestamptz,
) AssignedCourse {
	return AssignedCourse{
		ID:          id,
		Code:        code,
		Title:       title,
		Description: textPtr(description),
		Status:      status,
		CreatedAt:   createdAt.Time,
		UpdatedAt:   updatedAt.Time,
	}
}

func courseAssignment(
	id uuid.UUID,
	code string,
	title string,
	description pgtype.Text,
	status CourseStatus,
	createdAt pgtype.Timestamptz,
	updatedAt pgtype.Timestamptz,
	linkedAt pgtype.Timestamptz,
) CourseAssignment {
	return CourseAssignment{
		Course:   assignedCourse(id, code, title, description, status, createdAt, updatedAt),
		LinkedAt: linkedAt.Time,
	}
}
