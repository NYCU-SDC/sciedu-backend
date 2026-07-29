package user

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRepo struct {
	listUsersFn     func(ctx context.Context, params ListParams) ([]Profile, error)
	countUsersFn    func(ctx context.Context, filter ListFilter) (int64, error)
	getActiveUserFn func(ctx context.Context, id uuid.UUID) (Profile, error)
	replaceRolesFn  func(ctx context.Context, id uuid.UUID, roles []string) (Profile, error)
	softDeleteFn    func(ctx context.Context, id uuid.UUID) error

	lastListParams    ListParams
	lastCountFilter   ListFilter
	softDeleteCalls   int
	replaceRolesCalls int
}

func (f *fakeRepo) ListUsers(ctx context.Context, params ListParams) ([]Profile, error) {
	f.lastListParams = params
	if f.listUsersFn != nil {
		return f.listUsersFn(ctx, params)
	}
	return nil, nil
}

func (f *fakeRepo) CountUsers(ctx context.Context, filter ListFilter) (int64, error) {
	f.lastCountFilter = filter
	if f.countUsersFn != nil {
		return f.countUsersFn(ctx, filter)
	}
	return 0, nil
}

func (f *fakeRepo) GetActiveUser(ctx context.Context, id uuid.UUID) (Profile, error) {
	if f.getActiveUserFn != nil {
		return f.getActiveUserFn(ctx, id)
	}
	return Profile{}, nil
}

func (f *fakeRepo) ReplaceRoles(ctx context.Context, id uuid.UUID, roles []string) (Profile, error) {
	f.replaceRolesCalls++
	if f.replaceRolesFn != nil {
		return f.replaceRolesFn(ctx, id, roles)
	}
	return Profile{}, nil
}

func (f *fakeRepo) SoftDelete(ctx context.Context, id uuid.UUID) error {
	f.softDeleteCalls++
	if f.softDeleteFn != nil {
		return f.softDeleteFn(ctx, id)
	}
	return nil
}

func strPtr(s string) *string { return &s }

func TestServiceDeleteSelfOperation(t *testing.T) {
	actor := uuid.New()
	other := uuid.New()

	tests := []struct {
		name          string
		actorID       uuid.UUID
		targetID      uuid.UUID
		softDeleteFn  func(ctx context.Context, id uuid.UUID) error
		wantSelfErr   bool
		wantErr       bool
		wantDeleteHit int
	}{
		{
			name:          "self delete is rejected before touching the DB",
			actorID:       actor,
			targetID:      actor,
			wantSelfErr:   true,
			wantErr:       true,
			wantDeleteHit: 0,
		},
		{
			name:          "deleting another user calls the repository",
			actorID:       actor,
			targetID:      other,
			wantDeleteHit: 1,
		},
		{
			name:          "missing target surfaces the repository error",
			actorID:       actor,
			targetID:      other,
			softDeleteFn:  func(ctx context.Context, id uuid.UUID) error { return pgx.ErrNoRows },
			wantErr:       true,
			wantDeleteHit: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{softDeleteFn: tt.softDeleteFn}
			svc := NewService(repo, nil)

			err := svc.Delete(context.Background(), tt.actorID, tt.targetID)

			assert.Equal(t, tt.wantDeleteHit, repo.softDeleteCalls)
			if tt.wantSelfErr {
				assert.ErrorIs(t, err, errSelfOperation)
			}
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestServiceReplaceRolesSelfOperation(t *testing.T) {
	actor := uuid.New()
	other := uuid.New()

	tests := []struct {
		name           string
		actorID        uuid.UUID
		targetID       uuid.UUID
		roles          []string
		replaceFn      func(ctx context.Context, id uuid.UUID, roles []string) (Profile, error)
		wantSelfErr    bool
		wantInvalidErr bool
		wantErr        bool
		wantReplaceHit int
	}{
		{
			name:           "self role change is rejected before touching the DB",
			actorID:        actor,
			targetID:       actor,
			roles:          []string{"ADMIN"},
			wantSelfErr:    true,
			wantErr:        true,
			wantReplaceHit: 0,
		},
		{
			name:     "replacing another user's roles calls the repository",
			actorID:  actor,
			targetID: other,
			roles:    []string{"ADMIN"},
			replaceFn: func(ctx context.Context, id uuid.UUID, roles []string) (Profile, error) {
				return Profile{ID: id, Roles: roles}, nil
			},
			wantReplaceHit: 1,
		},
		{
			name:     "missing target surfaces the repository error",
			actorID:  actor,
			targetID: other,
			roles:    []string{"ADMIN"},
			replaceFn: func(ctx context.Context, id uuid.UUID, roles []string) (Profile, error) {
				return Profile{}, pgx.ErrNoRows
			},
			wantErr:        true,
			wantReplaceHit: 1,
		},
		{
			// Guards against callers other than the HTTP handler (whose
			// validator tag is the only other check today) reaching the DB
			// with a role string that isn't one of the known roles.
			name:           "unknown role is rejected before touching the DB",
			actorID:        actor,
			targetID:       other,
			roles:          []string{"SUPERADMIN"},
			wantInvalidErr: true,
			wantErr:        true,
			wantReplaceHit: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{replaceRolesFn: tt.replaceFn}
			svc := NewService(repo, nil)

			_, err := svc.ReplaceRoles(context.Background(), tt.actorID, tt.targetID, tt.roles)

			assert.Equal(t, tt.wantReplaceHit, repo.replaceRolesCalls)
			if tt.wantSelfErr {
				assert.ErrorIs(t, err, errSelfOperation)
			}
			if tt.wantInvalidErr {
				assert.ErrorIs(t, err, errInvalidUserPayload)
			}
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestServiceListPagination(t *testing.T) {
	tests := []struct {
		name           string
		total          int64
		page           int32
		pageSize       int32
		wantTotalPages int32
		wantHasNext    bool
		wantOffset     int32
	}{
		{name: "empty result", total: 0, page: 1, pageSize: 20, wantTotalPages: 0, wantHasNext: false, wantOffset: 0},
		{name: "single full page divides evenly", total: 20, page: 1, pageSize: 20, wantTotalPages: 1, wantHasNext: false, wantOffset: 0},
		{name: "first of two pages", total: 21, page: 1, pageSize: 20, wantTotalPages: 2, wantHasNext: true, wantOffset: 0},
		{name: "last page has no next", total: 21, page: 2, pageSize: 20, wantTotalPages: 2, wantHasNext: false, wantOffset: 20},
		{name: "middle page has next", total: 45, page: 2, pageSize: 20, wantTotalPages: 3, wantHasNext: true, wantOffset: 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{
				countUsersFn: func(ctx context.Context, filter ListFilter) (int64, error) { return tt.total, nil },
			}
			svc := NewService(repo, nil)

			page, err := svc.List(context.Background(), ListInput{Page: tt.page, PageSize: tt.pageSize})
			require.NoError(t, err)

			assert.Equal(t, tt.wantTotalPages, page.TotalPages)
			assert.Equal(t, tt.wantHasNext, page.HasNextPage)
			assert.Equal(t, tt.page, page.CurrentPage)
			assert.Equal(t, tt.pageSize, page.PageSize)
			assert.Equal(t, int32(tt.total), page.TotalItems)
			assert.Equal(t, tt.pageSize, repo.lastListParams.Limit)
			assert.Equal(t, tt.wantOffset, repo.lastListParams.Offset)
		})
	}
}

func TestServiceListPassesFilter(t *testing.T) {
	repo := &fakeRepo{
		countUsersFn: func(ctx context.Context, filter ListFilter) (int64, error) { return 0, nil },
	}
	svc := NewService(repo, nil)

	_, err := svc.List(context.Background(), ListInput{
		Page:     1,
		PageSize: 20,
		Search:   strPtr("alice"),
		Role:     strPtr("ADMIN"),
	})
	require.NoError(t, err)

	require.NotNil(t, repo.lastListParams.Search)
	assert.Equal(t, "alice", *repo.lastListParams.Search)
	require.NotNil(t, repo.lastListParams.Role)
	assert.Equal(t, "ADMIN", *repo.lastListParams.Role)

	require.NotNil(t, repo.lastCountFilter.Search)
	assert.Equal(t, "alice", *repo.lastCountFilter.Search)
	require.NotNil(t, repo.lastCountFilter.Role)
	assert.Equal(t, "ADMIN", *repo.lastCountFilter.Role)
}
