package experiment

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRepository struct {
	listFn                func(ctx context.Context, params ListParams) ([]Record, error)
	countFn               func(ctx context.Context, filter ListFilter) (int64, error)
	createFn              func(ctx context.Context, params CreateParams) (Record, error)
	findByIDFn            func(ctx context.Context, id uuid.UUID) (Record, error)
	listParticipantsFn    func(ctx context.Context, experimentID uuid.UUID, limit int32, offset int64) ([]ParticipantAssignment, error)
	countParticipantsFn   func(ctx context.Context, experimentID uuid.UUID) (int64, error)
	updateFn              func(ctx context.Context, params UpdateParams) (Record, error)
	updateStatusFn        func(ctx context.Context, id uuid.UUID, status Status) (Record, error)
	participantIDs        []uuid.UUID
	participantCandidates []Participant
	scheduleConflict      bool
	addParticipantsFn     func(ctx context.Context, experimentID uuid.UUID, userIDs []uuid.UUID) ([]ParticipantAssignment, error)
	removeParticipantFn   func(ctx context.Context, experimentID, userID uuid.UUID) error

	createCalls            int
	listCalls              int
	countCalls             int
	listParticipantsCalls  int
	countParticipantsCalls int
	withinTxCalls          int
	lockByIDCalls          int
	lockParticipantCalls   int
	scheduleConflictCalls  int
	updateCalls            int
	addParticipantsCalls   int
	removeParticipantCalls int
	updateStatusCalls      int
	lastListParams         ListParams
	lastParticipantLimit   int32
	lastParticipantOffset  int64
	lastAddedUserIDs       []uuid.UUID
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

func (f *fakeRepository) ListParticipants(ctx context.Context, experimentID uuid.UUID, limit int32, offset int64) ([]ParticipantAssignment, error) {
	f.listParticipantsCalls++
	f.lastParticipantLimit = limit
	f.lastParticipantOffset = offset
	if f.listParticipantsFn != nil {
		return f.listParticipantsFn(ctx, experimentID, limit, offset)
	}
	return nil, nil
}

func (f *fakeRepository) CountParticipants(ctx context.Context, experimentID uuid.UUID) (int64, error) {
	f.countParticipantsCalls++
	if f.countParticipantsFn != nil {
		return f.countParticipantsFn(ctx, experimentID)
	}
	return 0, nil
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

func (f *fakeRepository) LockParticipantCandidates(context.Context, []uuid.UUID) ([]Participant, error) {
	return f.participantCandidates, nil
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

func (f *fakeRepository) AddParticipants(ctx context.Context, experimentID uuid.UUID, userIDs []uuid.UUID) ([]ParticipantAssignment, error) {
	f.addParticipantsCalls++
	f.lastAddedUserIDs = append([]uuid.UUID(nil), userIDs...)
	if f.addParticipantsFn != nil {
		return f.addParticipantsFn(ctx, experimentID, userIDs)
	}
	return nil, nil
}

func (f *fakeRepository) RemoveParticipant(ctx context.Context, experimentID, userID uuid.UUID) error {
	f.removeParticipantCalls++
	if f.removeParticipantFn != nil {
		return f.removeParticipantFn(ctx, experimentID, userID)
	}
	return nil
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

func TestServiceListParticipants(t *testing.T) {
	experimentID := uuid.New()
	assignment := ParticipantAssignment{Participant: Participant{ID: uuid.New(), Roles: []string{"STUDENT"}}}
	repo := &fakeRepository{
		findByIDFn: func(context.Context, uuid.UUID) (Record, error) { return Record{ID: experimentID}, nil },
		listParticipantsFn: func(context.Context, uuid.UUID, int32, int64) ([]ParticipantAssignment, error) {
			return []ParticipantAssignment{assignment}, nil
		},
		countParticipantsFn: func(context.Context, uuid.UUID) (int64, error) { return 21, nil },
	}

	page, err := NewService(repo, nil).ListParticipants(context.Background(), experimentID, 2, 20)

	require.NoError(t, err)
	assert.Equal(t, []ParticipantAssignment{assignment}, page.Items)
	assert.Equal(t, int32(2), page.TotalPages)
	assert.Equal(t, int32(21), page.TotalItems)
	assert.Equal(t, int32(2), page.CurrentPage)
	assert.Equal(t, int32(20), page.PageSize)
	assert.False(t, page.HasNextPage)
	assert.Equal(t, int32(20), repo.lastParticipantLimit)
	assert.Equal(t, int64(20), repo.lastParticipantOffset)
}

func TestServiceListParticipantsValidationAndMissingParent(t *testing.T) {
	tests := []struct {
		name      string
		page      int32
		pageSize  int32
		findError error
		wantError error
	}{
		{name: "invalid page", page: 0, pageSize: 20, wantError: errInvalidExperimentPayload},
		{name: "invalid page size", page: 1, pageSize: 101, wantError: errInvalidExperimentPayload},
		{name: "missing parent", page: 1, pageSize: 20, findError: pgx.ErrNoRows, wantError: handlerutil.ErrNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{findByIDFn: func(context.Context, uuid.UUID) (Record, error) {
				return Record{}, tt.findError
			}}
			_, err := NewService(repo, nil).ListParticipants(context.Background(), uuid.New(), tt.page, tt.pageSize)
			assert.ErrorIs(t, err, tt.wantError)
			assert.Zero(t, repo.listParticipantsCalls)
			assert.Zero(t, repo.countParticipantsCalls)
		})
	}
}

func TestServiceAddParticipants(t *testing.T) {
	lowID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	highID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	disabledAt := time.Now().UTC()
	activeStudent := func(id uuid.UUID, roles ...string) Participant {
		return Participant{ID: id, Roles: roles}
	}
	tests := []struct {
		name       string
		status     Status
		requestIDs []uuid.UUID
		candidates []Participant
		overlap    bool
		addError   error
		wantError  error
		wantAdd    bool
	}{
		{name: "draft add", status: StatusDraft, requestIDs: []uuid.UUID{highID, lowID}, candidates: []Participant{activeStudent(lowID, "STUDENT"), activeStudent(highID, "STUDENT")}, wantAdd: true},
		{name: "scheduled add", status: StatusScheduled, requestIDs: []uuid.UUID{lowID}, candidates: []Participant{activeStudent(lowID, "STUDENT")}, wantAdd: true},
		{name: "active add", status: StatusActive, requestIDs: []uuid.UUID{lowID}, candidates: []Participant{activeStudent(lowID, "STUDENT", "EXPERIMENTER")}, wantAdd: true},
		{name: "completed rejects add", status: StatusCompleted, requestIDs: []uuid.UUID{lowID}, wantError: errExperimentConflict},
		{name: "archived rejects add", status: StatusArchived, requestIDs: []uuid.UUID{lowID}, wantError: errExperimentConflict},
		{name: "duplicate request id", status: StatusDraft, requestIDs: []uuid.UUID{lowID, lowID}, wantError: errExperimentConflict},
		{name: "missing participant", status: StatusDraft, requestIDs: []uuid.UUID{lowID}, wantError: errExperimentConflict},
		{name: "disabled participant", status: StatusDraft, requestIDs: []uuid.UUID{lowID}, candidates: []Participant{{ID: lowID, Roles: []string{"STUDENT"}, DisabledAt: &disabledAt}}, wantError: errExperimentConflict},
		{name: "non student", status: StatusDraft, requestIDs: []uuid.UUID{lowID}, candidates: []Participant{activeStudent(lowID, "EXPERIMENTER")}, wantError: errExperimentConflict},
		{name: "overlap", status: StatusDraft, requestIDs: []uuid.UUID{lowID}, candidates: []Participant{activeStudent(lowID, "STUDENT")}, overlap: true, wantError: errExperimentConflict},
		{name: "existing assignment", status: StatusDraft, requestIDs: []uuid.UUID{lowID}, candidates: []Participant{activeStudent(lowID, "STUDENT")}, addError: &pgconn.PgError{Code: databaseutil.PGErrUniqueViolation}, wantError: errExperimentConflict},
		{name: "empty batch", status: StatusDraft, wantError: errInvalidExperimentPayload},
		{name: "oversized batch", status: StatusDraft, requestIDs: make([]uuid.UUID, 101), wantError: errInvalidExperimentPayload},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			experimentID := uuid.New()
			params := validEditableParams()
			repo := &fakeRepository{
				findByIDFn: func(context.Context, uuid.UUID) (Record, error) {
					return recordWithEditableParams(experimentID, tt.status, params), nil
				},
				participantCandidates: tt.candidates,
				scheduleConflict:      tt.overlap,
				addParticipantsFn: func(context.Context, uuid.UUID, []uuid.UUID) ([]ParticipantAssignment, error) {
					if tt.addError != nil {
						return nil, tt.addError
					}
					return []ParticipantAssignment{{Participant: activeStudent(lowID, "STUDENT")}}, nil
				},
			}

			assignments, err := NewService(repo, nil).AddParticipants(context.Background(), experimentID, tt.requestIDs)

			if tt.wantError != nil {
				assert.ErrorIs(t, err, tt.wantError)
				assert.Empty(t, assignments)
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, assignments)
			}
			if tt.wantAdd {
				assert.Equal(t, 1, repo.addParticipantsCalls)
				if len(tt.requestIDs) == 2 {
					assert.Equal(t, []uuid.UUID{lowID, highID}, repo.lastAddedUserIDs)
				}
			} else if tt.addError == nil {
				assert.Zero(t, repo.addParticipantsCalls)
			}
		})
	}
}

func TestServiceRemoveParticipantLifecycle(t *testing.T) {
	tests := []struct {
		name       string
		status     Status
		findError  error
		wantError  error
		wantRemove bool
	}{
		{name: "draft removes", status: StatusDraft, wantRemove: true},
		{name: "scheduled removes", status: StatusScheduled, wantRemove: true},
		{name: "active rejects", status: StatusActive, wantError: errExperimentConflict},
		{name: "completed rejects", status: StatusCompleted, wantError: errExperimentConflict},
		{name: "archived rejects", status: StatusArchived, wantError: errExperimentConflict},
		{name: "missing parent", findError: pgx.ErrNoRows, wantError: handlerutil.ErrNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{findByIDFn: func(_ context.Context, id uuid.UUID) (Record, error) {
				return Record{ID: id, Status: tt.status}, tt.findError
			}}
			err := NewService(repo, nil).RemoveParticipant(context.Background(), uuid.New(), uuid.New())
			if tt.wantError != nil {
				assert.ErrorIs(t, err, tt.wantError)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantRemove, repo.removeParticipantCalls == 1)
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
