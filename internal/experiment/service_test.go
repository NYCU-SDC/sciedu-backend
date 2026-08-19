package experiment

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRepository struct {
	listFn         func(ctx context.Context, params ListParams) ([]Record, error)
	countFn        func(ctx context.Context, filter ListFilter) (int64, error)
	createFn       func(ctx context.Context, params CreateParams) (Record, error)
	findByIDFn     func(ctx context.Context, id uuid.UUID) (Record, error)
	updateFn       func(ctx context.Context, params UpdateParams) (Record, error)
	updateStatusFn func(ctx context.Context, id uuid.UUID, status Status) (Record, error)

	createCalls       int
	updateStatusCalls int
	lastListParams    ListParams
}

func (f *fakeRepository) List(ctx context.Context, params ListParams) ([]Record, error) {
	f.lastListParams = params
	if f.listFn != nil {
		return f.listFn(ctx, params)
	}
	return nil, nil
}

func (f *fakeRepository) Count(ctx context.Context, filter ListFilter) (int64, error) {
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
