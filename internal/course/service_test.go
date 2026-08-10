package course

import (
	"context"
	"errors"
	"strings"
	"testing"

	"sciedu-backend/internal/auth"

	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRepository struct {
	createFn       func(ctx context.Context, params CreateParams) (Record, error)
	listFn         func(ctx context.Context, params ListParams) ([]Record, error)
	countFn        func(ctx context.Context, filter ListFilter) (int64, error)
	byIDFn         func(ctx context.Context, id uuid.UUID) (Record, error)
	updateFn       func(ctx context.Context, params UpdateParams) (Record, error)
	updateStatusFn func(ctx context.Context, id uuid.UUID, status CourseStatus) (Record, error)

	createCalls       int
	byIDCalls         int
	updateCalls       int
	updateStatusCalls int
	lastListParams    ListParams
	lastCountFilter   ListFilter
}

func (f *fakeRepository) Create(ctx context.Context, params CreateParams) (Record, error) {
	f.createCalls++
	if f.createFn != nil {
		return f.createFn(ctx, params)
	}
	return Record{}, nil
}

func (f *fakeRepository) List(ctx context.Context, params ListParams) ([]Record, error) {
	f.lastListParams = params
	if f.listFn != nil {
		return f.listFn(ctx, params)
	}
	return nil, nil
}

func (f *fakeRepository) Count(ctx context.Context, filter ListFilter) (int64, error) {
	f.lastCountFilter = filter
	if f.countFn != nil {
		return f.countFn(ctx, filter)
	}
	return 0, nil
}

func (f *fakeRepository) ByID(ctx context.Context, id uuid.UUID) (Record, error) {
	f.byIDCalls++
	if f.byIDFn != nil {
		return f.byIDFn(ctx, id)
	}
	return Record{}, nil
}

type fakeCourseRoleQuerier struct {
	roles      []auth.Role
	err        error
	calls      int
	lastUserID uuid.UUID
}

func (f *fakeCourseRoleQuerier) ActiveUserRoles(_ context.Context, userID uuid.UUID) ([]auth.Role, error) {
	f.calls++
	f.lastUserID = userID
	return f.roles, f.err
}

type fakeStudentCourseAccessChecker struct {
	allowed       bool
	err           error
	calls         int
	lastStudentID uuid.UUID
	lastCourseID  uuid.UUID
}

func (f *fakeStudentCourseAccessChecker) CanAccessCourse(_ context.Context, studentID, courseID uuid.UUID) (bool, error) {
	f.calls++
	f.lastStudentID = studentID
	f.lastCourseID = courseID
	return f.allowed, f.err
}

func (f *fakeRepository) Update(ctx context.Context, params UpdateParams) (Record, error) {
	f.updateCalls++
	if f.updateFn != nil {
		return f.updateFn(ctx, params)
	}
	return Record{}, nil
}

func (f *fakeRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status CourseStatus) (Record, error) {
	f.updateStatusCalls++
	if f.updateStatusFn != nil {
		return f.updateStatusFn(ctx, id, status)
	}
	return Record{}, nil
}

func TestServiceListCourses(t *testing.T) {
	status := CourseStatusPUBLISHED
	search := "biology"
	tests := []struct {
		name           string
		total          int64
		input          ListInput
		wantTotalPages int32
		wantHasNext    bool
		wantOffset     int32
		wantErr        bool
	}{
		{name: "empty result", input: ListInput{Page: 1, PageSize: 20}, wantTotalPages: 0},
		{name: "first of two pages", total: 21, input: ListInput{Page: 1, PageSize: 20}, wantTotalPages: 2, wantHasNext: true},
		{name: "last page", total: 21, input: ListInput{Page: 2, PageSize: 20}, wantTotalPages: 2, wantOffset: 20},
		{name: "passes filters", total: 1, input: ListInput{Page: 1, PageSize: 10, Status: &status, Search: &search}, wantTotalPages: 1},
		{name: "rejects invalid page", input: ListInput{Page: 0, PageSize: 20}, wantErr: true},
		{name: "rejects oversized page size", input: ListInput{Page: 1, PageSize: 101}, wantErr: true},
		{name: "rejects overflowing offset", input: ListInput{Page: 2147483647, PageSize: 100}, wantErr: true},
		{name: "rejects invalid status", input: ListInput{Page: 1, PageSize: 20, Status: statusPtr(CourseStatus("UNKNOWN"))}, wantErr: true},
		{name: "rejects long search", input: ListInput{Page: 1, PageSize: 20, Search: stringPointer(strings.Repeat("x", 201))}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{countFn: func(context.Context, ListFilter) (int64, error) { return tt.total, nil }}
			page, err := NewService(repo, nil, nil, nil).List(t.Context(), tt.input)
			if tt.wantErr {
				assert.ErrorIs(t, err, errInvalidCoursePayload)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantTotalPages, page.TotalPages)
			assert.Equal(t, tt.wantHasNext, page.HasNextPage)
			assert.Equal(t, tt.wantOffset, repo.lastListParams.Offset)
			assert.Equal(t, tt.input.Status, repo.lastListParams.Status)
			assert.Equal(t, tt.input.Search, repo.lastCountFilter.Search)
		})
	}
}

func TestServiceCourseMetadataValidation(t *testing.T) {
	tests := []struct {
		name        string
		code        string
		title       string
		description *string
		wantErr     bool
	}{
		{name: "valid metadata", code: "BIO101", title: "Biology"},
		{name: "blank code", code: "  ", title: "Biology", wantErr: true},
		{name: "long code", code: strings.Repeat("x", 101), title: "Biology", wantErr: true},
		{name: "blank title", code: "BIO101", title: "  ", wantErr: true},
		{name: "long title", code: "BIO101", title: strings.Repeat("x", 201), wantErr: true},
		{name: "long description", code: "BIO101", title: "Biology", description: stringPointer(strings.Repeat("x", 4001)), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{
				createFn: func(ctx context.Context, params CreateParams) (Record, error) {
					return Record{Code: params.Code, Title: params.Title}, nil
				},
			}
			_, err := NewService(repo, nil, nil, nil).Create(t.Context(), CreateParams{
				Code: tt.code, Title: tt.title, Description: tt.description,
			})
			if tt.wantErr {
				assert.ErrorIs(t, err, errInvalidCoursePayload)
				assert.Zero(t, repo.createCalls)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, 1, repo.createCalls)
		})
	}
}

func TestServiceUpdateAndStatusValidation(t *testing.T) {
	id := uuid.New()
	tests := []struct {
		name            string
		params          UpdateParams
		status          CourseStatus
		updateStatus    bool
		wantErr         bool
		wantUpdateCalls int
		wantStatusCalls int
	}{
		{name: "updates metadata", params: UpdateParams{ID: id, Code: "BIO101", Title: "Biology"}, wantUpdateCalls: 1},
		{name: "rejects invalid metadata", params: UpdateParams{ID: id, Code: "", Title: "Biology"}, wantErr: true},
		{name: "updates status", status: CourseStatusARCHIVED, updateStatus: true, wantStatusCalls: 1},
		{name: "rejects invalid status", status: CourseStatus("INVALID"), updateStatus: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{}
			svc := NewService(repo, nil, nil, nil)
			var err error
			if tt.updateStatus {
				_, err = svc.UpdateStatus(t.Context(), id, tt.status)
			} else {
				_, err = svc.Update(t.Context(), tt.params)
			}
			if tt.wantErr {
				assert.ErrorIs(t, err, errInvalidCoursePayload)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.wantUpdateCalls, repo.updateCalls)
			assert.Equal(t, tt.wantStatusCalls, repo.updateStatusCalls)
		})
	}
}

func TestServiceWrapsRepositoryErrors(t *testing.T) {
	repositoryErr := errors.New("database unavailable")
	repo := &fakeRepository{
		byIDFn: func(context.Context, uuid.UUID) (Record, error) { return Record{}, repositoryErr },
	}
	_, err := NewService(repo, nil, nil, nil).ByID(t.Context(), uuid.New())
	assert.Error(t, err)
}

func TestServiceByIDForActor(t *testing.T) {
	actorID := uuid.New()
	courseID := uuid.New()
	dependencyErr := errors.New("experiment dependency unavailable")
	repositoryErr := pgx.ErrNoRows

	tests := []struct {
		name            string
		roles           []auth.Role
		roleErr         error
		allowed         bool
		accessErr       error
		byIDErr         error
		wantErr         error
		wantAnyErr      bool
		wantRoleCalls   int
		wantAccessCalls int
		wantByIDCalls   int
	}{
		{name: "student allowed", roles: []auth.Role{auth.STUDENT}, allowed: true, wantRoleCalls: 1, wantAccessCalls: 1, wantByIDCalls: 1},
		{name: "student denied", roles: []auth.Role{auth.STUDENT}, wantErr: handlerutil.ErrForbidden, wantRoleCalls: 1, wantAccessCalls: 1},
		{name: "student access dependency error", roles: []auth.Role{auth.STUDENT}, accessErr: dependencyErr, wantErr: dependencyErr, wantRoleCalls: 1, wantAccessCalls: 1},
		{name: "experimenter bypasses student checker", roles: []auth.Role{auth.EXPERIMENTER}, wantRoleCalls: 1, wantByIDCalls: 1},
		{name: "admin bypasses student checker", roles: []auth.Role{auth.ADMIN}, wantRoleCalls: 1, wantByIDCalls: 1},
		{name: "management role takes precedence", roles: []auth.Role{auth.STUDENT, auth.EXPERIMENTER}, wantRoleCalls: 1, wantByIDCalls: 1},
		{name: "inactive user is unauthorized", roleErr: pgx.ErrNoRows, wantErr: handlerutil.ErrUnauthorized, wantRoleCalls: 1},
		{name: "role dependency error", roleErr: dependencyErr, wantAnyErr: true, wantRoleCalls: 1},
		{name: "unsupported roles are forbidden", roles: []auth.Role{"UNKNOWN"}, wantErr: handlerutil.ErrForbidden, wantRoleCalls: 1},
		{name: "allowed student course is missing", roles: []auth.Role{auth.STUDENT}, allowed: true, byIDErr: repositoryErr, wantRoleCalls: 1, wantAccessCalls: 1, wantByIDCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roles := &fakeCourseRoleQuerier{roles: tt.roles, err: tt.roleErr}
			access := &fakeStudentCourseAccessChecker{allowed: tt.allowed, err: tt.accessErr}
			repo := &fakeRepository{byIDFn: func(context.Context, uuid.UUID) (Record, error) {
				if tt.byIDErr != nil {
					return Record{}, tt.byIDErr
				}
				return sampleCourse(courseID), nil
			}}

			record, err := NewService(repo, roles, access, nil).ByIDForActor(t.Context(), actorID, courseID)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else if tt.wantAnyErr {
				assert.Error(t, err)
			} else if tt.byIDErr != nil {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, courseID, record.ID)
			}

			assert.Equal(t, tt.wantRoleCalls, roles.calls)
			assert.Equal(t, tt.wantAccessCalls, access.calls)
			assert.Equal(t, tt.wantByIDCalls, repo.byIDCalls)
			if roles.calls > 0 {
				assert.Equal(t, actorID, roles.lastUserID)
			}
			if access.calls > 0 {
				assert.Equal(t, actorID, access.lastStudentID)
				assert.Equal(t, courseID, access.lastCourseID)
			}
		})
	}
}

func stringPointer(value string) *string         { return &value }
func statusPtr(value CourseStatus) *CourseStatus { return &value }
