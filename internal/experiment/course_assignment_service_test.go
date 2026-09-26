package experiment

import (
	"context"
	"fmt"
	"testing"

	"sciedu-backend/internal/auth"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type serviceRoleQuerier struct {
	roles []auth.Role
	err   error
}

func (q serviceRoleQuerier) ActiveUserRoles(context.Context, uuid.UUID) ([]auth.Role, error) {
	return q.roles, q.err
}

func TestServiceListCoursesForActor(t *testing.T) {
	experimentID := uuid.New()
	actorID := uuid.New()
	managementCourse := CourseAssignment{Course: AssignedCourse{ID: uuid.New(), Status: CourseStatusARCHIVED}}
	studentCourse := CourseAssignment{Course: AssignedCourse{ID: uuid.New(), Status: CourseStatusPUBLISHED}}

	tests := []struct {
		name              string
		roles             []auth.Role
		roleError         error
		findError         error
		accessible        bool
		wantError         error
		wantItems         []CourseAssignment
		wantManagement    bool
		wantStudentLookup bool
	}{
		{name: "experimenter sees every stored assignment", roles: []auth.Role{auth.EXPERIMENTER}, wantItems: []CourseAssignment{managementCourse}, wantManagement: true},
		{name: "admin sees every stored assignment", roles: []auth.Role{auth.ADMIN}, wantItems: []CourseAssignment{managementCourse}, wantManagement: true},
		{name: "management role wins for multi role actor", roles: []auth.Role{auth.STUDENT, auth.EXPERIMENTER}, wantItems: []CourseAssignment{managementCourse}, wantManagement: true},
		{name: "eligible student sees published assignments", roles: []auth.Role{auth.STUDENT}, accessible: true, wantItems: []CourseAssignment{studentCourse}, wantStudentLookup: true},
		{name: "ineligible student forbidden", roles: []auth.Role{auth.STUDENT}, wantError: handlerutil.ErrForbidden, wantStudentLookup: true},
		{name: "unsupported role forbidden", roles: nil, wantError: handlerutil.ErrForbidden},
		{name: "inactive actor unauthorized", roleError: pgx.ErrNoRows, wantError: handlerutil.ErrUnauthorized},
		{name: "missing experiment", roles: []auth.Role{auth.EXPERIMENTER}, findError: pgx.ErrNoRows, wantError: handlerutil.ErrNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			managementCalls := 0
			studentCalls := 0
			accessibleCalls := 0
			repo := &fakeRepository{
				findByIDFn: func(context.Context, uuid.UUID) (Record, error) {
					return Record{ID: experimentID}, tt.findError
				},
				listCoursesFn: func(context.Context, uuid.UUID, int32, int64) ([]CourseAssignment, error) {
					managementCalls++
					return []CourseAssignment{managementCourse}, nil
				},
				countCoursesFn: func(context.Context, uuid.UUID) (int64, error) { return 1, nil },
				studentAccessibleFn: func(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
					accessibleCalls++
					return tt.accessible, nil
				},
				listStudentCoursesFn: func(context.Context, uuid.UUID, int32, int64) ([]CourseAssignment, error) {
					studentCalls++
					return []CourseAssignment{studentCourse}, nil
				},
				countStudentCoursesFn: func(context.Context, uuid.UUID) (int64, error) { return 1, nil },
			}
			service := NewServiceWithRoles(repo, serviceRoleQuerier{roles: tt.roles, err: tt.roleError}, nil)

			page, err := service.ListCoursesForActor(context.Background(), actorID, experimentID, 1, 20)

			if tt.wantError != nil {
				assert.ErrorIs(t, err, tt.wantError)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantItems, page.Items)
				assert.Equal(t, int32(1), page.TotalItems)
			}
			assert.Equal(t, tt.wantManagement, managementCalls == 1)
			assert.Equal(t, tt.wantStudentLookup, accessibleCalls == 1)
			assert.Equal(t, tt.wantStudentLookup && tt.accessible, studentCalls == 1)
		})
	}
}

func TestServiceListCoursesValidationAndFailures(t *testing.T) {
	experimentID := uuid.New()
	actorID := uuid.New()

	t.Run("validates pagination before querying roles", func(t *testing.T) {
		_, err := NewServiceWithRoles(&fakeRepository{}, serviceRoleQuerier{roles: []auth.Role{auth.ADMIN}}, nil).
			ListCoursesForActor(context.Background(), actorID, experimentID, 0, 20)
		assert.ErrorIs(t, err, errInvalidExperimentPayload)
	})

	t.Run("requires role repository", func(t *testing.T) {
		_, err := NewService(&fakeRepository{}, nil).ListCoursesForActor(context.Background(), actorID, experimentID, 1, 20)
		assert.EqualError(t, err, "experiment role querier is unavailable")
	})

	t.Run("wraps role lookup failure", func(t *testing.T) {
		_, err := NewServiceWithRoles(&fakeRepository{}, serviceRoleQuerier{err: fmt.Errorf("database unavailable")}, nil).
			ListCoursesForActor(context.Background(), actorID, experimentID, 1, 20)
		assert.Error(t, err)
	})
}

func TestServiceAddCourses(t *testing.T) {
	lowID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	highID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	tests := []struct {
		name       string
		status     Status
		requestIDs []uuid.UUID
		candidates []AssignedCourse
		addError   error
		wantError  error
		wantAdd    bool
	}{
		{name: "draft accepts draft and published courses", status: StatusDraft, requestIDs: []uuid.UUID{highID, lowID}, candidates: []AssignedCourse{{ID: lowID, Status: CourseStatusDRAFT}, {ID: highID, Status: CourseStatusPUBLISHED}}, wantAdd: true},
		{name: "scheduled accepts courses", status: StatusScheduled, requestIDs: []uuid.UUID{lowID}, candidates: []AssignedCourse{{ID: lowID, Status: CourseStatusDRAFT}}, wantAdd: true},
		{name: "active accepts courses", status: StatusActive, requestIDs: []uuid.UUID{lowID}, candidates: []AssignedCourse{{ID: lowID, Status: CourseStatusPUBLISHED}}, wantAdd: true},
		{name: "completed rejects", status: StatusCompleted, requestIDs: []uuid.UUID{lowID}, wantError: errExperimentConflict},
		{name: "archived rejects", status: StatusArchived, requestIDs: []uuid.UUID{lowID}, wantError: errExperimentConflict},
		{name: "archived course rejects", status: StatusDraft, requestIDs: []uuid.UUID{lowID}, candidates: []AssignedCourse{{ID: lowID, Status: CourseStatusARCHIVED}}, wantError: errExperimentConflict},
		{name: "missing course rejects whole batch", status: StatusDraft, requestIDs: []uuid.UUID{lowID, highID}, candidates: []AssignedCourse{{ID: lowID, Status: CourseStatusDRAFT}}, wantError: errExperimentConflict},
		{name: "duplicate request rejects", status: StatusDraft, requestIDs: []uuid.UUID{lowID, lowID}, wantError: errExperimentConflict},
		{name: "existing assignment conflicts", status: StatusDraft, requestIDs: []uuid.UUID{lowID}, candidates: []AssignedCourse{{ID: lowID, Status: CourseStatusDRAFT}}, addError: &pgconn.PgError{Code: databaseutil.PGErrUniqueViolation}, wantError: errExperimentConflict},
		{name: "empty batch rejects", status: StatusDraft, wantError: errInvalidExperimentPayload},
		{name: "oversized batch rejects", status: StatusDraft, requestIDs: make([]uuid.UUID, 101), wantError: errInvalidExperimentPayload},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addCalls := 0
			var gotIDs []uuid.UUID
			repo := &fakeRepository{
				findByIDFn: func(context.Context, uuid.UUID) (Record, error) {
					return Record{ID: uuid.New(), Status: tt.status}, nil
				},
				courseCandidates: tt.candidates,
				addCoursesFn: func(_ context.Context, _ uuid.UUID, courseIDs []uuid.UUID) ([]CourseAssignment, error) {
					addCalls++
					gotIDs = append([]uuid.UUID(nil), courseIDs...)
					return []CourseAssignment{{Course: AssignedCourse{ID: lowID}}}, tt.addError
				},
			}

			assignments, err := NewService(repo, nil).AddCourses(context.Background(), uuid.New(), tt.requestIDs)

			if tt.wantError != nil {
				assert.ErrorIs(t, err, tt.wantError)
				assert.Empty(t, assignments)
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, assignments)
			}
			if tt.wantAdd || tt.addError != nil {
				assert.Equal(t, 1, addCalls)
			} else {
				assert.Zero(t, addCalls)
			}
			if len(tt.requestIDs) == 2 && tt.wantAdd {
				assert.Equal(t, []uuid.UUID{lowID, highID}, gotIDs)
			}
		})
	}
}

func TestServiceRemoveCourseLifecycle(t *testing.T) {
	tests := []struct {
		status     Status
		findError  error
		wantError  error
		wantRemove bool
	}{
		{status: StatusDraft, wantRemove: true},
		{status: StatusScheduled, wantRemove: true},
		{status: StatusActive, wantError: errExperimentConflict},
		{status: StatusCompleted, wantError: errExperimentConflict},
		{status: StatusArchived, wantError: errExperimentConflict},
		{findError: pgx.ErrNoRows, wantError: handlerutil.ErrNotFound},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			removeCalls := 0
			repo := &fakeRepository{
				findByIDFn: func(_ context.Context, id uuid.UUID) (Record, error) {
					return Record{ID: id, Status: tt.status}, tt.findError
				},
				removeCourseFn: func(context.Context, uuid.UUID, uuid.UUID) error { removeCalls++; return nil },
			}
			err := NewService(repo, nil).RemoveCourse(context.Background(), uuid.New(), uuid.New())
			if tt.wantError != nil {
				assert.ErrorIs(t, err, tt.wantError)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantRemove, removeCalls == 1)
		})
	}
}
