package experiment

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRepository struct {
	listFn           func(ctx context.Context, params ListParams) ([]Record, error)
	countFn          func(ctx context.Context, filter ListFilter) (int64, error)
	createFn         func(ctx context.Context, params CreateParams) (Record, error)
	findByIDFn       func(ctx context.Context, id uuid.UUID) (Record, error)
	updateFn         func(ctx context.Context, params UpdateParams) (Record, error)
	updateStatusFn   func(ctx context.Context, id uuid.UUID, status Status) (Record, error)
	participantIDs   []uuid.UUID
	scheduleConflict bool

	createCalls           int
	listCalls             int
	countCalls            int
	withinTxCalls         int
	lockByIDCalls         int
	lockParticipantCalls  int
	scheduleConflictCalls int
	updateCalls           int
	updateStatusCalls     int
	lastListParams        ListParams
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
	f.updateCalls++
	if f.updateFn != nil {
		return f.updateFn(ctx, params)
	}
	return Record{}, nil
}

func (f *fakeRepository) LockByID(ctx context.Context, id uuid.UUID) (Record, error) {
	f.lockByIDCalls++
	return f.FindByID(ctx, id)
}

func (f *fakeRepository) LockParticipantUsers(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	f.lockParticipantCalls++
	return f.participantIDs, nil
}

func (f *fakeRepository) HasParticipantScheduleConflict(
	context.Context,
	uuid.UUID,
	[]uuid.UUID,
	time.Time,
	time.Time,
) (bool, error) {
	f.scheduleConflictCalls++
	return f.scheduleConflict, nil
}

func (f *fakeRepository) WithinTx(ctx context.Context, fn func(MutationRepository) error) error {
	f.withinTxCalls++
	return fn(f)
}

func (f *fakeRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status Status) (Record, error) {
	f.updateStatusCalls++
	if f.updateStatusFn != nil {
		return f.updateStatusFn(ctx, id, status)
	}
	return Record{}, nil
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

func recordWithEditableParams(id uuid.UUID, status Status, params EditableParams) Record {
	return Record{
		ID:               id,
		Name:             params.Name,
		Description:      params.Description,
		Configuration:    params.Configuration,
		Status:           status,
		ScheduledStartAt: params.ScheduledStartAt,
		ScheduledEndAt:   params.ScheduledEndAt,
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

func TestServiceUpdateLifecyclePolicy(t *testing.T) {
	description := "updated description"
	tests := []struct {
		name              string
		status            Status
		mutate            func(*EditableParams)
		overlap           bool
		wantConflict      bool
		wantScheduleCheck bool
	}{
		{name: "draft metadata update", status: StatusDraft, mutate: func(p *EditableParams) { p.Name = "Updated" }},
		{name: "draft schedule update", status: StatusDraft, mutate: func(p *EditableParams) {
			p.ScheduledStartAt = p.ScheduledStartAt.Add(time.Hour)
			p.ScheduledEndAt = p.ScheduledEndAt.Add(time.Hour)
		}, wantScheduleCheck: true},
		{name: "scheduled schedule update", status: StatusScheduled, mutate: func(p *EditableParams) {
			p.ScheduledStartAt = p.ScheduledStartAt.Add(time.Hour)
			p.ScheduledEndAt = p.ScheduledEndAt.Add(time.Hour)
		}, wantScheduleCheck: true},
		{name: "active identical retry", status: StatusActive},
		{name: "active equal instant in another offset", status: StatusActive, mutate: func(p *EditableParams) {
			p.ScheduledEndAt = p.ScheduledEndAt.In(time.FixedZone("UTC+8", 8*60*60))
		}},
		{name: "active end extension", status: StatusActive, mutate: func(p *EditableParams) {
			p.ScheduledEndAt = p.ScheduledEndAt.Add(time.Hour)
		}, wantScheduleCheck: true},
		{name: "active shorter end", status: StatusActive, mutate: func(p *EditableParams) {
			p.ScheduledEndAt = p.ScheduledEndAt.Add(-time.Minute)
		}, wantConflict: true},
		{name: "active name change", status: StatusActive, mutate: func(p *EditableParams) { p.Name = "Updated" }, wantConflict: true},
		{name: "active description change", status: StatusActive, mutate: func(p *EditableParams) { p.Description = &description }, wantConflict: true},
		{name: "active start change", status: StatusActive, mutate: func(p *EditableParams) { p.ScheduledStartAt = p.ScheduledStartAt.Add(time.Minute) }, wantConflict: true},
		{name: "active configuration change", status: StatusActive, mutate: func(p *EditableParams) { p.Configuration.ShowScore = true }, wantConflict: true},
		{name: "completed is read only", status: StatusCompleted, wantConflict: true},
		{name: "archived is read only", status: StatusArchived, wantConflict: true},
		{name: "scheduled overlap rejects update", status: StatusScheduled, mutate: func(p *EditableParams) {
			p.ScheduledEndAt = p.ScheduledEndAt.Add(time.Hour)
		}, overlap: true, wantConflict: true, wantScheduleCheck: true},
		{name: "active extension overlap rejects update", status: StatusActive, mutate: func(p *EditableParams) {
			p.ScheduledEndAt = p.ScheduledEndAt.Add(time.Hour)
		}, overlap: true, wantConflict: true, wantScheduleCheck: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := uuid.New()
			currentParams := validEditableParams()
			requested := currentParams
			if tt.mutate != nil {
				tt.mutate(&requested)
			}
			repo := &fakeRepository{
				findByIDFn: func(context.Context, uuid.UUID) (Record, error) {
					return recordWithEditableParams(id, tt.status, currentParams), nil
				},
				updateFn: func(_ context.Context, params UpdateParams) (Record, error) {
					return recordWithEditableParams(id, tt.status, params.EditableParams), nil
				},
				scheduleConflict: tt.overlap,
			}
			if tt.wantScheduleCheck {
				repo.participantIDs = []uuid.UUID{uuid.New()}
			}

			record, err := NewService(repo, nil).Update(context.Background(), id, requested)

			assert.Equal(t, 1, repo.withinTxCalls)
			assert.Equal(t, 1, repo.lockByIDCalls)
			if tt.wantConflict {
				assert.ErrorIs(t, err, errExperimentConflict)
				assert.Zero(t, repo.updateCalls)
			} else {
				require.NoError(t, err)
				assert.Equal(t, requested.ScheduledEndAt, record.ScheduledEndAt)
				assert.Equal(t, 1, repo.updateCalls)
			}
			if tt.wantScheduleCheck {
				assert.Equal(t, 1, repo.lockParticipantCalls)
				assert.Equal(t, 1, repo.scheduleConflictCalls)
			} else {
				assert.Zero(t, repo.lockParticipantCalls)
				assert.Zero(t, repo.scheduleConflictCalls)
			}
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
