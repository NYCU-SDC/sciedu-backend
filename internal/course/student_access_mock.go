package course

import (
	"context"

	"sciedu-backend/internal/auth"

	"github.com/google/uuid"
)

const (
	DevelopmentMockStudentID          = auth.DevelopmentMockStudentID
	DevelopmentMockAssignedCourseID   = "00000000-0000-0000-0000-000000000012"
	DevelopmentMockUnassignedCourseID = "00000000-0000-0000-0000-000000000013"
)

// DevelopmentMockStudentCourseAccessChecker is temporary development-only
// authorization data. Replace it with the Experiment-backed checker.
type DevelopmentMockStudentCourseAccessChecker struct {
	assignments map[uuid.UUID]map[uuid.UUID]struct{}
}

func NewDevelopmentMockStudentCourseAccessChecker() *DevelopmentMockStudentCourseAccessChecker {
	studentID := uuid.MustParse(DevelopmentMockStudentID)
	courseID := uuid.MustParse(DevelopmentMockAssignedCourseID)
	return &DevelopmentMockStudentCourseAccessChecker{
		assignments: map[uuid.UUID]map[uuid.UUID]struct{}{
			studentID: {courseID: {}},
		},
	}
}

func (c *DevelopmentMockStudentCourseAccessChecker) CanAccessCourse(
	_ context.Context,
	studentID uuid.UUID,
	courseID uuid.UUID,
) (bool, error) {
	courses, ok := c.assignments[studentID]
	if !ok {
		return false, nil
	}
	_, ok = courses[courseID]
	return ok, nil
}
