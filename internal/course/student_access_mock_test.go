package course

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDevelopmentMockStudentCourseAccessChecker(t *testing.T) {
	checker := NewDevelopmentMockStudentCourseAccessChecker()
	studentID := uuid.MustParse(DevelopmentMockStudentID)
	assignedCourseID := uuid.MustParse(DevelopmentMockAssignedCourseID)
	unassignedCourseID := uuid.MustParse(DevelopmentMockUnassignedCourseID)

	tests := []struct {
		name      string
		studentID uuid.UUID
		courseID  uuid.UUID
		want      bool
	}{
		{name: "assigned student and course", studentID: studentID, courseID: assignedCourseID, want: true},
		{name: "unassigned course", studentID: studentID, courseID: unassignedCourseID},
		{name: "unassigned student", studentID: uuid.New(), courseID: assignedCourseID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed, err := checker.CanAccessCourse(t.Context(), tt.studentID, tt.courseID)
			require.NoError(t, err)
			assert.Equal(t, tt.want, allowed)
		})
	}
}
