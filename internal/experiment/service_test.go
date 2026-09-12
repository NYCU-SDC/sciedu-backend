package experiment

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRepository struct {
	listFn              func(ctx context.Context, params ListParams) ([]Record, error)
	countFn             func(ctx context.Context, filter ListFilter) (int64, error)
	createFn            func(ctx context.Context, params CreateParams) (Record, error)
	findByIDFn          func(ctx context.Context, id uuid.UUID) (Record, error)
	updateFn            func(ctx context.Context, params UpdateParams) (Record, error)
	updateStatusFn      func(ctx context.Context, id uuid.UUID, status Status) (Record, error)
	listParticipantsFn  func(ctx context.Context, params ParticipantListParams) ([]ParticipantAssignment, int64, error)
	addParticipantsFn   func(ctx context.Context, experimentID uuid.UUID, userIDs []uuid.UUID) ([]ParticipantAssignment, error)
	removeParticipantFn func(ctx context.Context, experimentID, userID uuid.UUID) error

	createCalls       int
	listCalls         int
	countCalls        int
	updateStatusCalls int
	lastListParams    ListParams
}

func (f *fakeRepository) List(ctx context.Context, params ListParams) ([]Record, error) {
	f.listCalls++
	f.lastListParams = params
	if f.listFn != nil {
		return f.listFn(ctx, params)
	}
	return nil, nil
}

func (f *fakeRepository) Count(ctx context.Context, filter ListFilter) (int64, error) {
	f.countCalls++
	if f.countFn != nil {
		return f.countFn(ctx, filter)
	}
	return 0, nil
}

func (f *fakeRepository) Create(ctx context.Context, params CreateParams) (Record, error) {
	f.createCalls++
	if f.createFn != nil {
		return f.createFn(ctx, params)
	}
	return Record{}, nil
}

func (f *fakeRepository) FindByID(ctx context.Context, id uuid.UUID) (Record, error) {
	if f.findByIDFn != nil {
		return f.findByIDFn(ctx, id)
	}
	return Record{}, nil
}

func (f *fakeRepository) Update(ctx context.Context, params UpdateParams) (Record, error) {
	if f.updateFn != nil {
		return f.updateFn(ctx, params)
	}
	return Record{}, nil
}

func (f *fakeRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status Status) (Record, error) {
	f.updateStatusCalls++
	if f.updateStatusFn != nil {
		return f.updateStatusFn(ctx, id, status)
	}
	return Record{}, nil
}

func (f *fakeRepository) ListParticipants(
	ctx context.Context,
	params ParticipantListParams,
) ([]ParticipantAssignment, int64, error) {
	if f.listParticipantsFn != nil {
		return f.listParticipantsFn(ctx, params)
	}
	return nil, 0, nil
}

func (f *fakeRepository) AddParticipants(
	ctx context.Context,
	experimentID uuid.UUID,
	userIDs []uuid.UUID,
) ([]ParticipantAssignment, error) {
	if f.addParticipantsFn != nil {
		return f.addParticipantsFn(ctx, experimentID, userIDs)
	}
	return nil, nil
}

func (f *fakeRepository) RemoveParticipant(ctx context.Context, experimentID, userID uuid.UUID) error {
	if f.removeParticipantFn != nil {
		return f.removeParticipantFn(ctx, experimentID, userID)
	}
	return nil
}

func validEditableParams() EditableParams {
	start := time.Date(2026, 9, 1, 1, 0, 0, 0, time.UTC)
	return EditableParams{
		Name:             "Experiment One",
		ScheduledStartAt: start,
		ScheduledEndAt:   start.Add(time.Hour),
		Configuration: Configuration{
			MaxAttempts:              1,
			GradingMode:              GradingModeAutomatic,
			CorrectAnswerReleaseMode: CorrectAnswerReleaseNever,
		},
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
		wantOffset     int64
	}{
		{name: "empty result", page: 1, pageSize: 20},
		{name: "first of two pages", total: 21, page: 1, pageSize: 20, wantTotalPages: 2, wantHasNext: true},
		{name: "last page", total: 21, page: 2, pageSize: 20, wantTotalPages: 2, wantOffset: 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{countFn: func(context.Context, ListFilter) (int64, error) { return tt.total, nil }}
			service := NewService(repo, nil)

			page, err := service.List(context.Background(), ListInput{Page: tt.page, PageSize: tt.pageSize})

			require.NoError(t, err)
			assert.Equal(t, tt.wantTotalPages, page.TotalPages)
			assert.Equal(t, tt.wantHasNext, page.HasNextPage)
			assert.Equal(t, tt.wantOffset, repo.lastListParams.Offset)
			assert.Equal(t, tt.pageSize, repo.lastListParams.Limit)
		})
	}
}

func TestServiceListValidation(t *testing.T) {
	invalidStatus := Status("PAUSED")
	emptySearch := ""
	longSearch := strings.Repeat("x", maxSearchLength+1)
	tests := []struct {
		name  string
		input ListInput
	}{
		{name: "zero page", input: ListInput{Page: 0, PageSize: 20}},
		{name: "zero page size", input: ListInput{Page: 1, PageSize: 0}},
		{name: "oversized page size", input: ListInput{Page: 1, PageSize: maxPageSize + 1}},
		{name: "invalid status", input: ListInput{Page: 1, PageSize: 20, Status: &invalidStatus}},
		{name: "empty search", input: ListInput{Page: 1, PageSize: 20, Search: &emptySearch}},
		{name: "long search", input: ListInput{Page: 1, PageSize: 20, Search: &longSearch}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{}
			service := NewService(repo, nil)

			_, err := service.List(context.Background(), tt.input)

			assert.ErrorIs(t, err, errInvalidExperimentPayload)
			assert.Zero(t, repo.listCalls)
			assert.Zero(t, repo.countCalls)
		})
	}
}

func TestServiceListRejectsCountOverflow(t *testing.T) {
	repo := &fakeRepository{countFn: func(context.Context, ListFilter) (int64, error) {
		return int64(math.MaxInt32) + 1, nil
	}}
	service := NewService(repo, nil)

	_, err := service.List(context.Background(), ListInput{Page: 1, PageSize: 20})

	assert.ErrorIs(t, err, errInvalidExperimentPayload)
	assert.Equal(t, 1, repo.listCalls)
	assert.Equal(t, 1, repo.countCalls)
}

func TestServiceCreateValidation(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*EditableParams)
		wantError bool
		wantName  string
	}{
		{name: "valid configuration", wantName: "Experiment One"},
		{name: "trims name", mutate: func(p *EditableParams) { p.Name = "  Valid name  " }, wantName: "Valid name"},
		{name: "blank name", mutate: func(p *EditableParams) { p.Name = "   " }, wantError: true},
		{name: "end equals start", mutate: func(p *EditableParams) { p.ScheduledEndAt = p.ScheduledStartAt }, wantError: true},
		{name: "retry requires two attempts", mutate: func(p *EditableParams) { p.Configuration.AllowRetry = true }, wantError: true},
		{name: "no retry requires one attempt", mutate: func(p *EditableParams) { p.Configuration.MaxAttempts = 2 }, wantError: true},
		{name: "unknown grading mode", mutate: func(p *EditableParams) { p.Configuration.GradingMode = "HYBRID" }, wantError: true},
		{name: "unknown release mode", mutate: func(p *EditableParams) { p.Configuration.CorrectAnswerReleaseMode = "NOW" }, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := validEditableParams()
			if tt.mutate != nil {
				tt.mutate(&params)
			}
			repo := &fakeRepository{createFn: func(_ context.Context, params CreateParams) (Record, error) {
				return Record{Name: params.Name}, nil
			}}
			service := NewService(repo, nil)

			record, err := service.Create(context.Background(), uuid.New(), params)

			if tt.wantError {
				assert.ErrorIs(t, err, errInvalidExperimentPayload)
				assert.Zero(t, repo.createCalls)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, record.Name)
			assert.Equal(t, 1, repo.createCalls)
		})
	}
}

func TestServiceUpdateStatusValidation(t *testing.T) {
	tests := []struct {
		name      string
		status    Status
		wantError bool
	}{
		{name: "draft", status: StatusDraft},
		{name: "active", status: StatusActive},
		{name: "unknown", status: "PAUSED", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{updateStatusFn: func(_ context.Context, id uuid.UUID, status Status) (Record, error) {
				return Record{ID: id, Status: status}, nil
			}}
			service := NewService(repo, nil)

			record, err := service.UpdateStatus(context.Background(), uuid.New(), tt.status)

			if tt.wantError {
				assert.ErrorIs(t, err, errInvalidExperimentPayload)
				assert.Zero(t, repo.updateStatusCalls)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.status, record.Status)
			assert.Equal(t, 1, repo.updateStatusCalls)
		})
	}
}

func TestServiceListParticipants(t *testing.T) {
	experimentID := uuid.New()
	tests := []struct {
		name           string
		input          ParticipantListInput
		total          int64
		repoErr        error
		wantError      bool
		wantNotFound   bool
		wantTotalPages int32
		wantOffset     int64
	}{
		{name: "empty", input: ParticipantListInput{Page: 1, PageSize: 20}},
		{name: "first of three pages", input: ParticipantListInput{Page: 1, PageSize: 10}, total: 21, wantTotalPages: 3},
		{name: "last page", input: ParticipantListInput{Page: 3, PageSize: 10}, total: 21, wantTotalPages: 3, wantOffset: 20},
		{name: "invalid page", input: ParticipantListInput{Page: 0, PageSize: 20}, wantError: true},
		{name: "invalid page size", input: ParticipantListInput{Page: 1, PageSize: 101}, wantError: true},
		{name: "count overflow", input: ParticipantListInput{Page: 1, PageSize: 20}, total: int64(math.MaxInt32) + 1, wantError: true},
		{name: "missing experiment", input: ParticipantListInput{Page: 1, PageSize: 20}, repoErr: pgx.ErrNoRows, wantError: true, wantNotFound: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var received ParticipantListParams
			repo := &fakeRepository{listParticipantsFn: func(
				_ context.Context,
				params ParticipantListParams,
			) ([]ParticipantAssignment, int64, error) {
				received = params
				return nil, tt.total, tt.repoErr
			}}
			page, err := NewService(repo, nil).ListParticipants(context.Background(), experimentID, tt.input)

			if tt.wantError {
				require.Error(t, err)
				if tt.wantNotFound {
					assert.ErrorIs(t, err, handlerutil.ErrNotFound)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, experimentID, received.ExperimentID)
			assert.Equal(t, tt.wantOffset, received.Offset)
			assert.Equal(t, tt.wantTotalPages, page.TotalPages)
			assert.Equal(t, tt.input.Page < tt.wantTotalPages, page.HasNextPage)
		})
	}
}

func TestServiceAddParticipants(t *testing.T) {
	experimentID := uuid.New()
	firstID := uuid.New()
	secondID := uuid.New()
	tests := []struct {
		name         string
		userIDs      []uuid.UUID
		repoErr      error
		wantError    error
		wantRepoCall bool
	}{
		{name: "adds batch", userIDs: []uuid.UUID{firstID, secondID}, wantRepoCall: true},
		{name: "empty batch", wantError: errInvalidExperimentPayload},
		{name: "oversized batch", userIDs: make([]uuid.UUID, maxParticipantBatchSize+1), wantError: errInvalidExperimentPayload},
		{name: "duplicate IDs", userIDs: []uuid.UUID{firstID, firstID}, wantError: errExperimentParticipantConflict},
		{name: "overlap conflict", userIDs: []uuid.UUID{firstID}, repoErr: errExperimentParticipantConflict, wantError: errExperimentParticipantConflict, wantRepoCall: true},
		{name: "concurrent duplicate conflict", userIDs: []uuid.UUID{firstID}, repoErr: &pgconn.PgError{Code: "23505"}, wantError: errExperimentParticipantConflict, wantRepoCall: true},
		{name: "missing experiment", userIDs: []uuid.UUID{firstID}, repoErr: pgx.ErrNoRows, wantError: handlerutil.ErrNotFound, wantRepoCall: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			repo := &fakeRepository{addParticipantsFn: func(
				_ context.Context,
				gotExperimentID uuid.UUID,
				gotUserIDs []uuid.UUID,
			) ([]ParticipantAssignment, error) {
				calls++
				assert.Equal(t, experimentID, gotExperimentID)
				assert.Equal(t, tt.userIDs, gotUserIDs)
				return []ParticipantAssignment{{Participant: Participant{ID: firstID}}}, tt.repoErr
			}}

			assignments, err := NewService(repo, nil).AddParticipants(context.Background(), experimentID, tt.userIDs)

			if tt.wantError != nil {
				assert.ErrorIs(t, err, tt.wantError)
			} else {
				require.NoError(t, err)
				require.Len(t, assignments, 1)
				assert.Equal(t, firstID, assignments[0].Participant.ID)
			}
			if tt.wantRepoCall {
				assert.Equal(t, 1, calls)
			} else {
				assert.Zero(t, calls)
			}
		})
	}
}

func TestServiceRemoveParticipant(t *testing.T) {
	experimentID := uuid.New()
	userID := uuid.New()
	repoErr := errors.New("database unavailable")
	tests := []struct {
		name      string
		repoErr   error
		wantError bool
	}{
		{name: "removes assignment"},
		{name: "repository failure", repoErr: repoErr, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{removeParticipantFn: func(
				_ context.Context,
				gotExperimentID uuid.UUID,
				gotUserID uuid.UUID,
			) error {
				assert.Equal(t, experimentID, gotExperimentID)
				assert.Equal(t, userID, gotUserID)
				return tt.repoErr
			}}
			err := NewService(repo, nil).RemoveParticipant(context.Background(), experimentID, userID)
			assert.Equal(t, tt.wantError, err != nil)
		})
	}
}
