package progress

import (
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusNotStarted Status = "NOT_STARTED"
	StatusInProgress Status = "IN_PROGRESS"
	StatusCompleted  Status = "COMPLETED"
)

type PageInfo struct {
	ID           uuid.UUID
	CourseID     uuid.UUID
	Title        string
	DisplayOrder int32
}

type ProgressRow struct {
	StudentID   uuid.UUID
	PageID      uuid.UUID
	CourseID    uuid.UUID
	ReachedAt   time.Time
	CompletedAt *time.Time
}

type PageProgressView struct {
	PageID      uuid.UUID
	PageNumber  int32
	Title       string
	ReachedAt   *time.Time
	CompletedAt *time.Time
}

type ProgressPageReference struct {
	PageID     uuid.UUID
	PageNumber int32
	Title      string
}

type CourseProgressSummary struct {
	TotalPages           int32
	ReachedPages         int32
	CompletedPages       int32
	CompletionPercentage int32
	Status               Status
	HighestReachedPage   *ProgressPageReference
}

type CourseProgressDetail struct {
	StudentID    uuid.UUID
	ExperimentID uuid.UUID
	CourseID     uuid.UUID
	Summary      CourseProgressSummary
	Pages        []PageProgressView
}

type Participant struct {
	ID         uuid.UUID
	Email      string
	Name       string
	AvatarURL  *string
	Roles      []string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	AssignedAt time.Time
}

type StudentCourseProgress struct {
	Participant Participant
	Summary     CourseProgressSummary
}

type StudentCourseProgressPage struct {
	Items       []StudentCourseProgress
	TotalPages  int32
	TotalItems  int32
	CurrentPage int32
	PageSize    int32
	HasNextPage bool
}

type ListCourseStudentProgressInput struct {
	ExperimentID uuid.UUID
	CourseID     uuid.UUID
	Page         int32
	PageSize     int32
}
