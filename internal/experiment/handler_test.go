package experiment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sciedu-backend/internal/auth"

	middlewareutil "github.com/NYCU-SDC/summer/pkg/middleware"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeHandlerService struct {
	listFn              func(ctx context.Context, input ListInput) (ExperimentPage, error)
	createFn            func(ctx context.Context, createdBy uuid.UUID, params EditableParams) (Record, error)
	findByIDFn          func(ctx context.Context, id uuid.UUID) (Record, error)
	updateFn            func(ctx context.Context, id uuid.UUID, params EditableParams) (Record, error)
	updateStatusFn      func(ctx context.Context, id uuid.UUID, status Status) (Record, error)
	listParticipantsFn  func(ctx context.Context, experimentID uuid.UUID, input ParticipantListInput) (ParticipantPage, error)
	addParticipantsFn   func(ctx context.Context, experimentID uuid.UUID, userIDs []uuid.UUID) ([]ParticipantAssignment, error)
	removeParticipantFn func(ctx context.Context, experimentID, userID uuid.UUID) error

	lastListInput ListInput
	lastCreatedBy uuid.UUID
}

func (f *fakeHandlerService) List(ctx context.Context, input ListInput) (ExperimentPage, error) {
	f.lastListInput = input
	if f.listFn != nil {
		return f.listFn(ctx, input)
	}
	return ExperimentPage{CurrentPage: input.Page, PageSize: input.PageSize}, nil
}

func (f *fakeHandlerService) Create(ctx context.Context, createdBy uuid.UUID, params EditableParams) (Record, error) {
	f.lastCreatedBy = createdBy
	if f.createFn != nil {
		return f.createFn(ctx, createdBy, params)
	}
	return sampleRecord(uuid.New()), nil
}

func (f *fakeHandlerService) FindByID(ctx context.Context, id uuid.UUID) (Record, error) {
	if f.findByIDFn != nil {
		return f.findByIDFn(ctx, id)
	}
	return sampleRecord(id), nil
}

func (f *fakeHandlerService) Update(ctx context.Context, id uuid.UUID, params EditableParams) (Record, error) {
	if f.updateFn != nil {
		return f.updateFn(ctx, id, params)
	}
	return sampleRecord(id), nil
}

func (f *fakeHandlerService) UpdateStatus(ctx context.Context, id uuid.UUID, status Status) (Record, error) {
	if f.updateStatusFn != nil {
		return f.updateStatusFn(ctx, id, status)
	}
	record := sampleRecord(id)
	record.Status = status
	return record, nil
}

func (f *fakeHandlerService) ListParticipants(
	ctx context.Context,
	experimentID uuid.UUID,
	input ParticipantListInput,
) (ParticipantPage, error) {
	if f.listParticipantsFn != nil {
		return f.listParticipantsFn(ctx, experimentID, input)
	}
	return ParticipantPage{CurrentPage: input.Page, PageSize: input.PageSize}, nil
}

func (f *fakeHandlerService) AddParticipants(
	ctx context.Context,
	experimentID uuid.UUID,
	userIDs []uuid.UUID,
) ([]ParticipantAssignment, error) {
	if f.addParticipantsFn != nil {
		return f.addParticipantsFn(ctx, experimentID, userIDs)
	}
	return nil, nil
}

func (f *fakeHandlerService) RemoveParticipant(ctx context.Context, experimentID, userID uuid.UUID) error {
	if f.removeParticipantFn != nil {
		return f.removeParticipantFn(ctx, experimentID, userID)
	}
	return nil
}

type fakeRoleQuerier struct {
	roles []auth.Role
}

func (f fakeRoleQuerier) ActiveUserRoles(context.Context, uuid.UUID) ([]auth.Role, error) {
	return f.roles, nil
}

func sampleRecord(id uuid.UUID) Record {
	params := validEditableParams()
	return Record{
		ID:               id,
		CreatedBy:        uuid.New(),
		Name:             params.Name,
		Configuration:    params.Configuration,
		Status:           StatusDraft,
		ScheduledStartAt: params.ScheduledStartAt,
		ScheduledEndAt:   params.ScheduledEndAt,
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
		ParticipantCount: 2,
		CourseCount:      3,
	}
}

func validRequestBody() string {
	return `{
		"name":"Experiment One",
		"scheduledStartAt":"2026-09-01T01:00:00Z",
		"scheduledEndAt":"2026-09-01T02:00:00Z",
		"configuration":{
			"maxAttempts":1,
			"allowRetry":false,
			"showScore":true,
			"showExplanations":false,
			"gradingMode":"AUTOMATIC",
			"correctAnswerReleaseMode":"NEVER"
		}
	}`
}

func directMux(handler *Handler, actorID uuid.UUID) *http.ServeMux {
	mux := http.NewServeMux()
	inject := func(fn http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			fn(w, r.WithContext(auth.ContextWithUserID(r.Context(), actorID)))
		}
	}
	mux.HandleFunc("GET /api/experiments", inject(handler.List))
	mux.HandleFunc("POST /api/experiments", inject(handler.Create))
	mux.HandleFunc("GET /api/experiments/{id}", inject(handler.Get))
	mux.HandleFunc("PUT /api/experiments/{id}", inject(handler.Update))
	mux.HandleFunc("PUT /api/experiments/{id}/status", inject(handler.UpdateStatus))
	mux.HandleFunc("GET /api/experiments/{id}/participants", inject(handler.ListParticipants))
	mux.HandleFunc("POST /api/experiments/{id}/participants", inject(handler.AddParticipants))
	mux.HandleFunc("DELETE /api/experiments/{id}/participants/{userId}", inject(handler.RemoveParticipant))
	return mux
}

func TestHandlerList(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		wantCode int
		verify   func(*testing.T, ListInput)
	}{
		{name: "defaults", wantCode: http.StatusOK, verify: func(t *testing.T, input ListInput) {
			assert.Equal(t, int32(1), input.Page)
			assert.Equal(t, int32(20), input.PageSize)
		}},
		{name: "all filters", query: "?page=2&pageSize=10&status=ACTIVE&scheduledFrom=2026-09-01T01%3A00%3A00Z&scheduledTo=2026-09-02T01%3A00%3A00Z&search=biology", wantCode: http.StatusOK, verify: func(t *testing.T, input ListInput) {
			require.NotNil(t, input.Status)
			assert.Equal(t, StatusActive, *input.Status)
			require.NotNil(t, input.ScheduledFrom)
			require.NotNil(t, input.ScheduledTo)
			require.NotNil(t, input.Search)
			assert.Equal(t, "biology", *input.Search)
		}},
		{name: "invalid page", query: "?page=0", wantCode: http.StatusBadRequest},
		{name: "overflow page", query: "?page=2147483648", wantCode: http.StatusBadRequest},
		{name: "invalid page size", query: "?pageSize=101", wantCode: http.StatusBadRequest},
		{name: "invalid status", query: "?status=PAUSED", wantCode: http.StatusBadRequest},
		{name: "invalid timestamp", query: "?scheduledFrom=tomorrow", wantCode: http.StatusBadRequest},
		{name: "empty search", query: "?search=", wantCode: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeHandlerService{}
			mux := directMux(NewHandler(service, nil), uuid.New())
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/experiments"+tt.query, nil))

			assert.Equal(t, tt.wantCode, recorder.Code)
			if tt.verify != nil {
				tt.verify(t, service.lastListInput)
			}
		})
	}
}

func TestHandlerCreate(t *testing.T) {
	actorID := uuid.New()
	tests := []struct {
		name       string
		body       string
		withUserID bool
		wantCode   int
	}{
		{name: "creates experiment", body: validRequestBody(), withUserID: true, wantCode: http.StatusCreated},
		{name: "missing user context", body: validRequestBody(), wantCode: http.StatusUnauthorized},
		{name: "missing configuration field", body: strings.Replace(validRequestBody(), `"showScore":true,`, "", 1), withUserID: true, wantCode: http.StatusBadRequest},
		{name: "invalid grading mode", body: strings.Replace(validRequestBody(), "AUTOMATIC", "HYBRID", 1), withUserID: true, wantCode: http.StatusBadRequest},
		{name: "malformed JSON", body: `{`, withUserID: true, wantCode: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeHandlerService{}
			handler := NewHandler(service, nil)
			request := httptest.NewRequest(http.MethodPost, "/api/experiments", strings.NewReader(tt.body))
			if tt.withUserID {
				request = request.WithContext(auth.ContextWithUserID(request.Context(), actorID))
			}
			recorder := httptest.NewRecorder()
			handler.Create(recorder, request)

			assert.Equal(t, tt.wantCode, recorder.Code)
			if tt.wantCode == http.StatusCreated {
				assert.Equal(t, actorID, service.lastCreatedBy)
				var response experimentDetailResponse
				require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
				assert.Equal(t, int32(2), response.ParticipantCount)
				assert.Equal(t, int32(3), response.CourseCount)
			}
		})
	}
}

func TestHandlerResourceOperations(t *testing.T) {
	id := uuid.New()
	tests := []struct {
		name     string
		method   string
		path     string
		body     string
		wantCode int
	}{
		{name: "get", method: http.MethodGet, path: "/api/experiments/" + id.String(), wantCode: http.StatusOK},
		{name: "get malformed ID", method: http.MethodGet, path: "/api/experiments/not-a-uuid", wantCode: http.StatusBadRequest},
		{name: "update", method: http.MethodPut, path: "/api/experiments/" + id.String(), body: validRequestBody(), wantCode: http.StatusOK},
		{name: "update status", method: http.MethodPut, path: "/api/experiments/" + id.String() + "/status", body: `{"status":"ACTIVE"}`, wantCode: http.StatusOK},
		{name: "invalid status", method: http.MethodPut, path: "/api/experiments/" + id.String() + "/status", body: `{"status":"PAUSED"}`, wantCode: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := directMux(NewHandler(&fakeHandlerService{}, nil), uuid.New())
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))
			assert.Equal(t, tt.wantCode, recorder.Code)
		})
	}
}

func TestRegisterRoutesAuthorization(t *testing.T) {
	tests := []struct {
		name     string
		roles    []auth.Role
		wantCode int
	}{
		{name: "student forbidden", roles: []auth.Role{auth.STUDENT}, wantCode: http.StatusForbidden},
		{name: "experimenter allowed", roles: []auth.Role{auth.EXPERIMENTER}, wantCode: http.StatusOK},
		{name: "admin allowed", roles: []auth.Role{auth.ADMIN}, wantCode: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			handler := NewHandler(&fakeHandlerService{}, nil)
			handler.RegisterRoutes(mux, middlewareutil.NewSet(), auth.NewAuthorizer(fakeRoleQuerier{roles: tt.roles}, nil))
			request := httptest.NewRequest(http.MethodGet, "/api/experiments", nil)
			request = request.WithContext(auth.ContextWithUserID(request.Context(), uuid.New()))
			recorder := httptest.NewRecorder()

			mux.ServeHTTP(recorder, request)

			assert.Equal(t, tt.wantCode, recorder.Code)
		})
	}
}

func sampleParticipantAssignment(userID uuid.UUID) ParticipantAssignment {
	now := time.Date(2026, time.September, 8, 1, 2, 3, 0, time.UTC)
	return ParticipantAssignment{
		Participant: Participant{
			ID:        userID,
			Email:     "student@example.test",
			Name:      "Student",
			Roles:     []string{"STUDENT"},
			CreatedAt: now,
			UpdatedAt: now,
		},
		AssignedAt: now,
	}
}

func TestHandlerListParticipants(t *testing.T) {
	experimentID := uuid.New()
	participantID := uuid.New()
	tests := []struct {
		name     string
		query    string
		wantCode int
	}{
		{name: "defaults", wantCode: http.StatusOK},
		{name: "custom pagination", query: "?page=2&pageSize=1", wantCode: http.StatusOK},
		{name: "invalid page", query: "?page=0", wantCode: http.StatusBadRequest},
		{name: "invalid page size", query: "?pageSize=101", wantCode: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeHandlerService{listParticipantsFn: func(
				_ context.Context,
				gotExperimentID uuid.UUID,
				input ParticipantListInput,
			) (ParticipantPage, error) {
				assert.Equal(t, experimentID, gotExperimentID)
				return ParticipantPage{
					Items:       []ParticipantAssignment{sampleParticipantAssignment(participantID)},
					TotalPages:  2,
					TotalItems:  2,
					CurrentPage: input.Page,
					PageSize:    input.PageSize,
					HasNextPage: input.Page == 1,
				}, nil
			}}
			mux := directMux(NewHandler(service, nil), uuid.New())
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, httptest.NewRequest(
				http.MethodGet,
				"/api/experiments/"+experimentID.String()+"/participants"+tt.query,
				nil,
			))

			assert.Equal(t, tt.wantCode, recorder.Code)
			if tt.wantCode == http.StatusOK {
				var response paginatedExperimentParticipantsResponse
				require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
				require.Len(t, response.Items, 1)
				assert.Equal(t, participantID, response.Items[0].Participant.ID)
				assert.Equal(t, []string{"STUDENT"}, response.Items[0].Participant.Roles)
			}
		})
	}
}

func TestHandlerAddParticipants(t *testing.T) {
	experimentID := uuid.New()
	participantID := uuid.New()
	tests := []struct {
		name     string
		body     string
		service  *fakeHandlerService
		wantCode int
	}{
		{
			name: "adds participants",
			body: `{"userIds":["` + participantID.String() + `"]}`,
			service: &fakeHandlerService{addParticipantsFn: func(
				_ context.Context,
				gotExperimentID uuid.UUID,
				userIDs []uuid.UUID,
			) ([]ParticipantAssignment, error) {
				assert.Equal(t, experimentID, gotExperimentID)
				assert.Equal(t, []uuid.UUID{participantID}, userIDs)
				return []ParticipantAssignment{sampleParticipantAssignment(participantID)}, nil
			}},
			wantCode: http.StatusOK,
		},
		{name: "empty batch", body: `{"userIds":[]}`, service: &fakeHandlerService{}, wantCode: http.StatusBadRequest},
		{name: "invalid UUID", body: `{"userIds":["not-a-uuid"]}`, service: &fakeHandlerService{}, wantCode: http.StatusBadRequest},
		{name: "malformed JSON", body: `{`, service: &fakeHandlerService{}, wantCode: http.StatusBadRequest},
		{
			name: "assignment conflict",
			body: `{"userIds":["` + participantID.String() + `"]}`,
			service: &fakeHandlerService{addParticipantsFn: func(
				context.Context,
				uuid.UUID,
				[]uuid.UUID,
			) ([]ParticipantAssignment, error) {
				return nil, errExperimentParticipantConflict
			}},
			wantCode: http.StatusConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := directMux(NewHandler(tt.service, nil), uuid.New())
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, httptest.NewRequest(
				http.MethodPost,
				"/api/experiments/"+experimentID.String()+"/participants",
				strings.NewReader(tt.body),
			))
			assert.Equal(t, tt.wantCode, recorder.Code)
		})
	}
}

func TestHandlerRemoveParticipant(t *testing.T) {
	experimentID := uuid.New()
	participantID := uuid.New()
	service := &fakeHandlerService{removeParticipantFn: func(
		_ context.Context,
		gotExperimentID uuid.UUID,
		gotUserID uuid.UUID,
	) error {
		assert.Equal(t, experimentID, gotExperimentID)
		assert.Equal(t, participantID, gotUserID)
		return nil
	}}
	mux := directMux(NewHandler(service, nil), uuid.New())
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(
		http.MethodDelete,
		"/api/experiments/"+experimentID.String()+"/participants/"+participantID.String(),
		nil,
	))

	assert.Equal(t, http.StatusNoContent, recorder.Code)
	assert.Empty(t, recorder.Body.String())
}

func TestHandlerParticipantMalformedIDs(t *testing.T) {
	experimentID := uuid.New()
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "list experiment ID", method: http.MethodGet, path: "/api/experiments/not-a-uuid/participants"},
		{name: "add experiment ID", method: http.MethodPost, path: "/api/experiments/not-a-uuid/participants", body: `{"userIds":["` + uuid.NewString() + `"]}`},
		{name: "remove experiment ID", method: http.MethodDelete, path: "/api/experiments/not-a-uuid/participants/" + uuid.NewString()},
		{name: "remove user ID", method: http.MethodDelete, path: "/api/experiments/" + experimentID.String() + "/participants/not-a-uuid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := directMux(NewHandler(&fakeHandlerService{}, nil), uuid.New())
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
		})
	}
}

func TestParticipantRoutesAuthorization(t *testing.T) {
	experimentID := uuid.New()
	participantID := uuid.New()
	tests := []struct {
		name     string
		roles    []auth.Role
		method   string
		path     string
		body     string
		wantCode int
	}{
		{name: "student cannot list", roles: []auth.Role{auth.STUDENT}, method: http.MethodGet, path: "/api/experiments/" + experimentID.String() + "/participants", wantCode: http.StatusForbidden},
		{name: "student cannot add", roles: []auth.Role{auth.STUDENT}, method: http.MethodPost, path: "/api/experiments/" + experimentID.String() + "/participants", body: `{"userIds":["` + participantID.String() + `"]}`, wantCode: http.StatusForbidden},
		{name: "student cannot remove", roles: []auth.Role{auth.STUDENT}, method: http.MethodDelete, path: "/api/experiments/" + experimentID.String() + "/participants/" + participantID.String(), wantCode: http.StatusForbidden},
		{name: "experimenter can list", roles: []auth.Role{auth.EXPERIMENTER}, method: http.MethodGet, path: "/api/experiments/" + experimentID.String() + "/participants", wantCode: http.StatusOK},
		{name: "admin can list", roles: []auth.Role{auth.ADMIN}, method: http.MethodGet, path: "/api/experiments/" + experimentID.String() + "/participants", wantCode: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			handler := NewHandler(&fakeHandlerService{}, nil)
			handler.RegisterRoutes(mux, middlewareutil.NewSet(), auth.NewAuthorizer(fakeRoleQuerier{roles: tt.roles}, nil))
			request := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			request = request.WithContext(auth.ContextWithUserID(request.Context(), uuid.New()))
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, request)
			assert.Equal(t, tt.wantCode, recorder.Code)
		})
	}
}
