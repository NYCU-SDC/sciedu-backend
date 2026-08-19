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
	listFn         func(ctx context.Context, input ListInput) (Page, error)
	createFn       func(ctx context.Context, createdBy uuid.UUID, params EditableParams) (Record, error)
	findByIDFn     func(ctx context.Context, id uuid.UUID) (Record, error)
	updateFn       func(ctx context.Context, id uuid.UUID, params EditableParams) (Record, error)
	updateStatusFn func(ctx context.Context, id uuid.UUID, status Status) (Record, error)

	lastListInput ListInput
	lastCreatedBy uuid.UUID
}

func (f *fakeHandlerService) List(ctx context.Context, input ListInput) (Page, error) {
	f.lastListInput = input
	if f.listFn != nil {
		return f.listFn(ctx, input)
	}
	return Page{CurrentPage: input.Page, PageSize: input.PageSize}, nil
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
