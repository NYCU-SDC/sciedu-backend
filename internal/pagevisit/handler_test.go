package pagevisit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sciedu-backend/internal/auth"

	middlewareutil "github.com/NYCU-SDC/summer/pkg/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/zap"
)

type fakeHandlerService struct {
	enterFn func(context.Context, EnterInput) (PageVisit, error)
	leaveFn func(context.Context, LeaveInput) (PageVisit, error)
	listFn  func(context.Context, ListInput) (VisitPage, error)
}

func (f fakeHandlerService) Enter(ctx context.Context, input EnterInput) (PageVisit, error) {
	return f.enterFn(ctx, input)
}

func (f fakeHandlerService) Leave(ctx context.Context, input LeaveInput) (PageVisit, error) {
	return f.leaveFn(ctx, input)
}

func (f fakeHandlerService) List(ctx context.Context, input ListInput) (VisitPage, error) {
	return f.listFn(ctx, input)
}

func handlerVisit(studentID uuid.UUID, closed bool) PageVisit {
	now := time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC)
	visit := PageVisit{
		ID:              uuid.New(),
		StudentID:       studentID,
		CourseID:        uuid.New(),
		PageID:          uuid.New(),
		ClientSessionID: uuid.New(),
		IdempotencyKey:  uuid.New(),
		EnteredAt:       pgtype.Timestamptz{Time: now, Valid: true},
	}
	if closed {
		visit.LeftAt = pgtype.Timestamptz{Time: now.Add(time.Minute), Valid: true}
	}
	return visit
}

func TestHandlerEnter(t *testing.T) {
	studentID := uuid.New()
	visit := handlerVisit(studentID, false)
	body := `{"courseId":"` + visit.CourseID.String() + `","pageId":"` + visit.PageID.String() + `","clientSessionId":"` + visit.ClientSessionID.String() + `"}`

	tests := []struct {
		name       string
		body       string
		header     string
		serviceErr error
		wantCode   int
		wantCall   bool
	}{
		{name: "valid request", body: body, header: uuid.NewString(), wantCode: http.StatusCreated, wantCall: true},
		{name: "identical replay remains created", body: body, header: uuid.NewString(), wantCode: http.StatusCreated, wantCall: true},
		{name: "missing idempotency key", body: body, wantCode: http.StatusBadRequest},
		{name: "malformed idempotency key", body: body, header: "bad", wantCode: http.StatusBadRequest},
		{name: "malformed body", body: `{`, header: uuid.NewString(), wantCode: http.StatusBadRequest},
		{name: "unknown client-owned field", body: strings.TrimSuffix(body, "}") + `,"enteredAt":"2026-09-12T00:00:00Z"}`, header: uuid.NewString(), wantCode: http.StatusBadRequest},
		{name: "missing resource", body: body, header: uuid.NewString(), serviceErr: ErrNotFound, wantCode: http.StatusNotFound, wantCall: true},
		{name: "idempotency conflict", body: body, header: uuid.NewString(), serviceErr: ErrIdempotencyConflict, wantCode: http.StatusConflict, wantCall: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			service := fakeHandlerService{enterFn: func(_ context.Context, input EnterInput) (PageVisit, error) {
				called = true
				if input.StudentID != studentID {
					t.Fatalf("authenticated student mismatch: %s", input.StudentID)
				}
				return visit, tt.serviceErr
			}}
			handler := NewHandler(service, zap.NewNop())
			req := httptest.NewRequest(http.MethodPost, "/api/page-visits", strings.NewReader(tt.body))
			req = req.WithContext(auth.ContextWithUserID(req.Context(), studentID))
			if tt.header != "" {
				req.Header.Set("Idempotency-Key", tt.header)
			}
			recorder := httptest.NewRecorder()

			handler.Enter(recorder, req)

			if recorder.Code != tt.wantCode {
				t.Fatalf("expected status %d, got %d: %s", tt.wantCode, recorder.Code, recorder.Body.String())
			}
			if called != tt.wantCall {
				t.Fatalf("expected service called=%v, got %v", tt.wantCall, called)
			}
			if tt.wantCode == http.StatusCreated && strings.Contains(recorder.Body.String(), "idempotencyKey") {
				t.Fatal("response exposed idempotencyKey")
			}
		})
	}
}

func TestHandlerLeave(t *testing.T) {
	studentID := uuid.New()
	visit := handlerVisit(studentID, true)
	tests := []struct {
		name       string
		pathID     string
		serviceErr error
		wantCode   int
		wantCall   bool
	}{
		{name: "valid leave", pathID: visit.ID.String(), wantCode: http.StatusOK, wantCall: true},
		{name: "repeated leave", pathID: visit.ID.String(), wantCode: http.StatusOK, wantCall: true},
		{name: "invalid id", pathID: "bad", wantCode: http.StatusBadRequest},
		{name: "missing or other student", pathID: visit.ID.String(), serviceErr: ErrNotFound, wantCode: http.StatusNotFound, wantCall: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			service := fakeHandlerService{leaveFn: func(_ context.Context, input LeaveInput) (PageVisit, error) {
				called = true
				if input.StudentID != studentID {
					t.Fatalf("authenticated student mismatch: %s", input.StudentID)
				}
				return visit, tt.serviceErr
			}}
			handler := NewHandler(service, zap.NewNop())
			req := httptest.NewRequest(http.MethodPost, "/api/page-visits/"+tt.pathID+"/leave", nil)
			req.SetPathValue("id", tt.pathID)
			req = req.WithContext(auth.ContextWithUserID(req.Context(), studentID))
			recorder := httptest.NewRecorder()

			handler.Leave(recorder, req)

			if recorder.Code != tt.wantCode {
				t.Fatalf("expected status %d, got %d: %s", tt.wantCode, recorder.Code, recorder.Body.String())
			}
			if called != tt.wantCall {
				t.Fatalf("expected service called=%v, got %v", tt.wantCall, called)
			}
		})
	}
}

func TestParseListInput(t *testing.T) {
	id := uuid.NewString()
	tests := []struct {
		name     string
		query    string
		wantErr  bool
		validate func(*testing.T, ListInput)
	}{
		{name: "defaults", validate: func(t *testing.T, input ListInput) {
			if input.Page != 1 || input.PageSize != 20 {
				t.Fatalf("unexpected defaults: %+v", input)
			}
		}},
		{name: "all filters", query: "studentId=" + id + "&courseId=" + id + "&pageId=" + id + "&clientSessionId=" + id + "&status=OPEN&enteredFrom=2026-09-12T00:00:00Z&enteredBefore=2026-09-13T00:00:00Z&page=2&pageSize=10", validate: func(t *testing.T, input ListInput) {
			if input.StudentID == nil || input.CourseID == nil || input.PageID == nil || input.ClientSessionID == nil || input.Status == nil || input.EnteredFrom == nil || input.EnteredBefore == nil || input.Page != 2 || input.PageSize != 10 {
				t.Fatalf("filters not parsed: %+v", input)
			}
		}},
		{name: "empty page", query: "page=", wantErr: true},
		{name: "empty pageSize", query: "pageSize=", wantErr: true},
		{name: "non integer page", query: "page=x", wantErr: true},
		{name: "non integer pageSize", query: "pageSize=x", wantErr: true},
		{name: "page below minimum", query: "page=0", wantErr: true},
		{name: "pageSize below minimum", query: "pageSize=0", wantErr: true},
		{name: "pageSize above maximum", query: "pageSize=101", wantErr: true},
		{name: "invalid status", query: "status=PENDING", wantErr: true},
		{name: "empty status", query: "status=", wantErr: true},
		{name: "invalid UUID", query: "studentId=bad", wantErr: true},
		{name: "empty UUID", query: "courseId=", wantErr: true},
		{name: "invalid timestamp", query: "enteredFrom=bad", wantErr: true},
		{name: "empty timestamp", query: "enteredBefore=", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/page-visits?"+tt.query, nil)
			input, err := parseListInput(req)
			if (err != nil) != tt.wantErr {
				t.Fatalf("expected error=%v, got %v", tt.wantErr, err)
			}
			if tt.validate != nil {
				tt.validate(t, input)
			}
		})
	}
}

func TestHandlerListResponse(t *testing.T) {
	visit := handlerVisit(uuid.New(), false)
	var gotInput ListInput
	service := fakeHandlerService{listFn: func(_ context.Context, input ListInput) (VisitPage, error) {
		gotInput = input
		return VisitPage{Items: []PageVisit{visit}, TotalPages: 2, TotalItems: 21, CurrentPage: 1, PageSize: 20, HasNextPage: true}, nil
	}}
	handler := NewHandler(service, zap.NewNop())
	req := httptest.NewRequest(http.MethodGet, "/api/page-visits?status=OPEN", nil)
	recorder := httptest.NewRecorder()

	handler.List(recorder, req)

	if recorder.Code != http.StatusOK || gotInput.Status == nil || *gotInput.Status != StatusOpen {
		t.Fatalf("unexpected list result status=%d input=%+v body=%s", recorder.Code, gotInput, recorder.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	items := body["items"].([]any)
	item := items[0].(map[string]any)
	if _, found := item["idempotencyKey"]; found {
		t.Fatal("response exposed idempotencyKey")
	}
	if _, found := item["status"]; found {
		t.Fatal("response exposed derived status")
	}
	if _, found := item["leftAt"]; found {
		t.Fatal("open visit response included leftAt")
	}
}

type handlerRoleQuerier struct {
	roles []auth.Role
}

func (q handlerRoleQuerier) ActiveUserRoles(context.Context, uuid.UUID) ([]auth.Role, error) {
	return q.roles, nil
}

func TestHandlerRouteAuthorization(t *testing.T) {
	actorID := uuid.New()
	service := fakeHandlerService{
		enterFn: func(_ context.Context, input EnterInput) (PageVisit, error) {
			return handlerVisit(input.StudentID, false), nil
		},
		leaveFn: func(_ context.Context, input LeaveInput) (PageVisit, error) {
			return handlerVisit(input.StudentID, true), nil
		},
		listFn: func(context.Context, ListInput) (VisitPage, error) { return VisitPage{}, nil },
	}
	tests := []struct {
		name     string
		role     auth.Role
		method   string
		path     string
		body     string
		header   bool
		auth     bool
		wantCode int
	}{
		{name: "student enter allowed", role: auth.STUDENT, method: http.MethodPost, path: "/api/page-visits", body: `{"courseId":"` + uuid.NewString() + `","pageId":"` + uuid.NewString() + `","clientSessionId":"` + uuid.NewString() + `"}`, header: true, auth: true, wantCode: http.StatusCreated},
		{name: "student list forbidden", role: auth.STUDENT, method: http.MethodGet, path: "/api/page-visits", auth: true, wantCode: http.StatusForbidden},
		{name: "experimenter list allowed", role: auth.EXPERIMENTER, method: http.MethodGet, path: "/api/page-visits", auth: true, wantCode: http.StatusOK},
		{name: "admin list allowed", role: auth.ADMIN, method: http.MethodGet, path: "/api/page-visits", auth: true, wantCode: http.StatusOK},
		{name: "experimenter enter forbidden", role: auth.EXPERIMENTER, method: http.MethodPost, path: "/api/page-visits", body: `{}`, auth: true, wantCode: http.StatusForbidden},
		{name: "missing authentication rejected", role: auth.STUDENT, method: http.MethodGet, path: "/api/page-visits", wantCode: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := middlewareutil.NewSet(func(next http.HandlerFunc) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					if tt.auth {
						r = r.WithContext(auth.ContextWithUserID(r.Context(), actorID))
					}
					next(w, r)
				}
			})
			mux := http.NewServeMux()
			NewHandler(service, zap.NewNop()).RegisterRoutes(mux, set, auth.NewAuthorizer(handlerRoleQuerier{roles: []auth.Role{tt.role}}, nil))
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.header {
				req.Header.Set("Idempotency-Key", uuid.NewString())
			}
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, req)
			if recorder.Code != tt.wantCode {
				t.Fatalf("expected status %d, got %d: %s", tt.wantCode, recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestHandlerUnexpectedErrorReturnsInternalServerError(t *testing.T) {
	service := fakeHandlerService{listFn: func(context.Context, ListInput) (VisitPage, error) {
		return VisitPage{}, errors.New("database unavailable")
	}}
	recorder := httptest.NewRecorder()
	NewHandler(service, zap.NewNop()).List(recorder, httptest.NewRequest(http.MethodGet, "/api/page-visits", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", recorder.Code)
	}
}
