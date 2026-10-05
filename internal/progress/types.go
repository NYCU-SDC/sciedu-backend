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

// Page is a snapshot of a page used for progress derivation.
type PageInfo struct {
	ID           uuid.UUID
	CourseID     uuid.UUID
	Title        string
	DisplayOrder int32
}

// ProgressRow is a single student's progress on a single page.
type ProgressRow struct {
	StudentID   uuid.UUID
	PageID      uuid.UUID
	CourseID    uuid.UUID
	ReachedAt   time.Time
	CompletedAt *time.Time
}

// PageProgressView is the per-page view returned by Progress APIs.
type PageProgressView struct {
	PageID      uuid.UUID
	PageNumber  int32
	Title       string
	ReachedAt   *time.Time
	CompletedAt *time.Time
}

type CourseProgressSummary struct {
	TotalPages           int32
	ReachedPages         int32
	CompletedPages       int32
	CompletionPercentage int32
	Status               Status
	HighestReachedPage   *uuid.UUID
}

type CourseProgressDetail struct {
	CourseID uuid.UUID
	Summary  CourseProgressSummary
	Pages    []PageProgressView
}

// Participant is the minimal user shape used by Progress listing endpoints.
type Participant struct {
	ID         uuid.UUID
	Email      string
	Name       string
	AvatarURL  *string
	AssignedAt time.Time
}

// StudentCourseProgress combines participant identity with their summary.
type StudentCourseProgress struct {
	Participant Participant
	Summary     CourseProgressSummary
}
