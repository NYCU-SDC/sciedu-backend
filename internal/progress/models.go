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

type PageProgress struct {
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
	Pages    []PageProgress
}
