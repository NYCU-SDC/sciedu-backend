package page

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sciedu-backend/internal/auth"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	middlewareutil "github.com/NYCU-SDC/summer/pkg/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/zap"
)

type fakePageService struct {
	listByCourseFn func(ctx context.Context, actorID, courseID uuid.UUID) ([]Page, error)
	getFn          func(ctx context.Context, actorID, id uuid.UUID) (Detail, error)
	listBlocksFn   func(ctx context.Context, actorID, pageID uuid.UUID) ([]Block, error)
	createFn       func(ctx context.Context, courseID uuid.UUID, req PageRequest) (Detail, error)
	updateFn       func(ctx context.Context, id uuid.UUID, req PageRequest) (Detail, error)
	deleteFn       func(ctx context.Context, id uuid.UUID) error
	reorderFn      func(ctx context.Context, courseID uuid.UUID, pageIDs []uuid.UUID) ([]Page, error)

	createReq  PageRequest
	reorderIDs []uuid.UUID
	callCount  int
}

func (f *fakePageService) ListByCourseForActor(ctx context.Context, actorID, courseID uuid.UUID) ([]Page, error) {
	f.callCount++
	if f.listByCourseFn != nil {
		return f.listByCourseFn(ctx, actorID, courseID)
	}
	return nil, nil
}

func (f *fakePageService) GetForActor(ctx context.Context, actorID, id uuid.UUID) (Detail, error) {
	f.callCount++
	if f.getFn != nil {
		return f.getFn(ctx, actorID, id)
	}
	return Detail{Page: Page{ID: id}, Blocks: []Block{}}, nil
}

func (f *fakePageService) ListBlocksForActor(ctx context.Context, actorID, pageID uuid.UUID) ([]Block, error) {
	f.callCount++
	if f.listBlocksFn != nil {
		return f.listBlocksFn(ctx, actorID, pageID)
	}
	return nil, nil
}

func (f *fakePageService) Create(ctx context.Context, courseID uuid.UUID, req PageRequest) (Detail, error) {
	f.callCount++
	f.createReq = req
	if f.createFn != nil {
		return f.createFn(ctx, courseID, req)
	}
	return Detail{Page: Page{ID: uuid.New(), CourseID: courseID, Title: req.Title, DisplayOrder: req.DisplayOrder}, Blocks: []Block{}}, nil
}

func (f *fakePageService) Update(ctx context.Context, id uuid.UUID, req PageRequest) (Detail, error) {
	f.callCount++
	if f.updateFn != nil {
		return f.updateFn(ctx, id, req)
	}
	return Detail{Page: Page{ID: id, Title: req.Title, DisplayOrder: req.DisplayOrder}, Blocks: []Block{}}, nil
}

func (f *fakePageService) Delete(ctx context.Context, id uuid.UUID) error {
	f.callCount++
	if f.deleteFn != nil {
		return f.deleteFn(ctx, id)
	}
	return nil
}

func (f *fakePageService) Reorder(ctx context.Context, courseID uuid.UUID, pageIDs []uuid.UUID) ([]Page, error) {
	f.callCount++
	f.reorderIDs = pageIDs
	if f.reorderFn != nil {
		return f.reorderFn(ctx, courseID, pageIDs)
	}
	return nil, nil
}

type fakeBlockService struct {
	listByPageFn func(ctx context.Context, pageID uuid.UUID) ([]Block, error)
	createFn     func(ctx context.Context, pageID uuid.UUID, req BlockRequest) (Block, error)
	updateFn     func(ctx context.Context, pageID, blockID uuid.UUID, req BlockRequest) (Block, error)
	deleteFn     func(ctx context.Context, pageID, blockID uuid.UUID) error
	reorderFn    func(ctx context.Context, pageID uuid.UUID, blockIDs []uuid.UUID) ([]Block, error)

	createReq BlockRequest
	callCount int
}

func (f *fakeBlockService) ListByPage(ctx context.Context, pageID uuid.UUID) ([]Block, error) {
	f.callCount++
	if f.listByPageFn != nil {
		return f.listByPageFn(ctx, pageID)
	}
	return nil, nil
}

func (f *fakeBlockService) Create(ctx context.Context, pageID uuid.UUID, req BlockRequest) (Block, error) {
	f.callCount++
	f.createReq = req
	if f.createFn != nil {
		return f.createFn(ctx, pageID, req)
	}
	return Block{ID: uuid.New(), PageID: pageID, Type: req.Type, ResourceID: req.ResourceID, DisplayOrder: req.DisplayOrder, Required: req.Required}, nil
}

func (f *fakeBlockService) Update(ctx context.Context, pageID, blockID uuid.UUID, req BlockRequest) (Block, error) {
	f.callCount++
	if f.updateFn != nil {
		return f.updateFn(ctx, pageID, blockID, req)
	}
	return Block{ID: blockID, PageID: pageID, Type: req.Type, ResourceID: req.ResourceID}, nil
}

func (f *fakeBlockService) Delete(ctx context.Context, pageID, blockID uuid.UUID) error {
	f.callCount++
	if f.deleteFn != nil {
		return f.deleteFn(ctx, pageID, blockID)
	}
	return nil
}

func (f *fakeBlockService) Reorder(ctx context.Context, pageID uuid.UUID, blockIDs []uuid.UUID) ([]Block, error) {
	f.callCount++
	if f.reorderFn != nil {
		return f.reorderFn(ctx, pageID, blockIDs)
	}
	return nil, nil
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("failed to parse %q: %v", value, err)
	}
	return parsed
}

func newTestMux(pages *fakePageService, blocks *fakeBlockService) *http.ServeMux {
	mux := http.NewServeMux()
	NewHandler(pages, blocks, zap.NewNop()).RegisterRoutes(mux, nil, nil)
	return mux
}

func doRequest(t *testing.T, mux *http.ServeMux, method, url, body string) *httptest.ResponseRecorder {
	t.Helper()

	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, url, nil)
	} else {
		req = httptest.NewRequest(method, url, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req = req.WithContext(auth.ContextWithUserID(req.Context(), uuid.New()))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestHandlerRoutes_TableDriven(t *testing.T) {
	courseID, pageID := uuid.New(), uuid.New()

	tests := []struct {
		name      string
		method    string
		url       string
		body      string
		wantCode  int
		wantCalls int
	}{
		{
			name: "list pages", method: http.MethodGet,
			url:      "/api/courses/" + courseID.String() + "/pages",
			wantCode: http.StatusOK, wantCalls: 1,
		},
		{
			name: "create page returns 201", method: http.MethodPost,
			url:  "/api/courses/" + courseID.String() + "/pages",
			body: `{"title":"Intro","displayOrder":0}`,
			// displayOrder 0 must survive validation: it is the first position,
			// not a missing field.
			wantCode: http.StatusCreated, wantCalls: 1,
		},
		{
			name: "create page rejects a blank title", method: http.MethodPost,
			url:  "/api/courses/" + courseID.String() + "/pages",
			body: `{"title":"","displayOrder":0}`,
			// Structural validation runs before the service is reached.
			wantCode: http.StatusBadRequest, wantCalls: 0,
		},
		{
			name: "create page rejects an out of range displayOrder", method: http.MethodPost,
			url:  "/api/courses/" + courseID.String() + "/pages",
			body: `{"title":"Intro","displayOrder":100000}`,
			// Must stay below the reorder's +100000 shift.
			wantCode: http.StatusBadRequest, wantCalls: 0,
		},
		{
			name: "create page rejects a malformed course id", method: http.MethodPost,
			url:  "/api/courses/not-a-uuid/pages",
			body: `{"title":"Intro","displayOrder":0}`,
			// Handler rejects on the path parse, so the body never matters.
			wantCode: http.StatusBadRequest, wantCalls: 0,
		},
		{
			name: "reorder pages", method: http.MethodPut,
			url:      "/api/courses/" + courseID.String() + "/pages/order",
			body:     fmt.Sprintf(`{"pageIds":["%s"]}`, pageID),
			wantCode: http.StatusOK, wantCalls: 1,
		},
		{
			name: "reorder pages rejects duplicate ids", method: http.MethodPut,
			url:      "/api/courses/" + courseID.String() + "/pages/order",
			body:     fmt.Sprintf(`{"pageIds":["%s","%s"]}`, pageID, pageID),
			wantCode: http.StatusBadRequest, wantCalls: 0,
		},
		{
			name: "reorder pages rejects an empty list", method: http.MethodPut,
			url:      "/api/courses/" + courseID.String() + "/pages/order",
			body:     `{"pageIds":[]}`,
			wantCode: http.StatusBadRequest, wantCalls: 0,
		},
		{
			name: "get page", method: http.MethodGet,
			url:      "/api/pages/" + pageID.String(),
			wantCode: http.StatusOK, wantCalls: 1,
		},
		{
			name: "update page", method: http.MethodPut,
			url:      "/api/pages/" + pageID.String(),
			body:     `{"title":"Renamed","displayOrder":2}`,
			wantCode: http.StatusOK, wantCalls: 1,
		},
		{
			name: "delete page returns 204", method: http.MethodDelete,
			url:      "/api/pages/" + pageID.String(),
			wantCode: http.StatusNoContent, wantCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pages := &fakePageService{}
			mux := newTestMux(pages, &fakeBlockService{})

			rec := doRequest(t, mux, tt.method, tt.url, tt.body)

			if rec.Code != tt.wantCode {
				t.Fatalf("status mismatch: want %d got %d, body=%s", tt.wantCode, rec.Code, rec.Body.String())
			}
			if pages.callCount != tt.wantCalls {
				t.Fatalf("expected %d service calls, got %d", tt.wantCalls, pages.callCount)
			}
		})
	}
}

func TestHandlerBlockRoutes_TableDriven(t *testing.T) {
	pageID, blockID, resourceID := uuid.New(), uuid.New(), uuid.New()
	blocksURL := "/api/pages/" + pageID.String() + "/blocks"

	tests := []struct {
		name           string
		method         string
		url            string
		body           string
		wantCode       int
		wantPageCalls  int
		wantBlockCalls int
	}{
		{
			name: "list blocks", method: http.MethodGet,
			url: blocksURL, wantCode: http.StatusOK, wantPageCalls: 1,
		},
		{
			name: "create block returns 201", method: http.MethodPost,
			url:      blocksURL,
			body:     fmt.Sprintf(`{"type":"TEXT","resourceId":"%s","displayOrder":0,"required":true}`, resourceID),
			wantCode: http.StatusCreated, wantBlockCalls: 1,
		},
		{
			name: "create block rejects an unknown type", method: http.MethodPost,
			url:      blocksURL,
			body:     fmt.Sprintf(`{"type":"AUDIO","resourceId":"%s","displayOrder":0,"required":true}`, resourceID),
			wantCode: http.StatusBadRequest,
		},
		{
			name: "create block rejects a missing resourceId", method: http.MethodPost,
			url:      blocksURL,
			body:     `{"type":"TEXT","displayOrder":0,"required":true}`,
			wantCode: http.StatusBadRequest,
		},
		{
			name: "update block", method: http.MethodPut,
			url:      blocksURL + "/" + blockID.String(),
			body:     fmt.Sprintf(`{"type":"QUESTION","resourceId":"%s","displayOrder":1,"required":false}`, resourceID),
			wantCode: http.StatusOK, wantBlockCalls: 1,
		},
		{
			name: "delete block returns 204", method: http.MethodDelete,
			url:      blocksURL + "/" + blockID.String(),
			wantCode: http.StatusNoContent, wantBlockCalls: 1,
		},
		{
			name: "reorder blocks", method: http.MethodPut,
			url:      blocksURL + "/order",
			body:     fmt.Sprintf(`{"blockIds":["%s"]}`, blockID),
			wantCode: http.StatusOK, wantBlockCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pages := &fakePageService{}
			blocks := &fakeBlockService{}
			mux := newTestMux(pages, blocks)

			rec := doRequest(t, mux, tt.method, tt.url, tt.body)

			if rec.Code != tt.wantCode {
				t.Fatalf("status mismatch: want %d got %d, body=%s", tt.wantCode, rec.Code, rec.Body.String())
			}
			if pages.callCount != tt.wantPageCalls {
				t.Fatalf("expected %d page service calls, got %d", tt.wantPageCalls, pages.callCount)
			}
			if blocks.callCount != tt.wantBlockCalls {
				t.Fatalf("expected %d block service calls, got %d", tt.wantBlockCalls, blocks.callCount)
			}
		})
	}
}

// PUT /blocks/order and PUT /blocks/{blockId} share a shape; the literal segment
// has to win, or reordering would be parsed as updating a block named "order".
func TestHandlerReorderRouteBeatsBlockIDRoute(t *testing.T) {
	pageID, blockID := uuid.New(), uuid.New()
	blocks := &fakeBlockService{
		reorderFn: func(context.Context, uuid.UUID, []uuid.UUID) ([]Block, error) {
			return []Block{}, nil
		},
		updateFn: func(context.Context, uuid.UUID, uuid.UUID, BlockRequest) (Block, error) {
			t.Fatal("reorder request was routed to UpdateBlock")
			return Block{}, nil
		},
	}
	mux := newTestMux(&fakePageService{}, blocks)

	rec := doRequest(t, mux, http.MethodPut,
		"/api/pages/"+pageID.String()+"/blocks/order",
		fmt.Sprintf(`{"blockIds":["%s"]}`, blockID))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerErrorMapping_TableDriven(t *testing.T) {
	pageID := uuid.New()

	tests := []struct {
		name     string
		err      error
		wantCode int
	}{
		{
			name:     "unique violation maps to 409",
			err:      fmt.Errorf("%w: duplicate", databaseutil.ErrUniqueViolation),
			wantCode: http.StatusConflict,
		},
		{
			name:     "payload sentinel maps to 400",
			err:      fmt.Errorf("%w: bad displayOrder", errInvalidPagePayload),
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "not found maps to 404",
			err:      handlerutil.NewNotFoundError("pages", "id", pageID.String(), ""),
			wantCode: http.StatusNotFound,
		},
		{
			// A referenced content/question can be deleted between validateResource
			// and the INSERT; the FK rejects the write and must not read as a 500.
			name:     "foreign key violation maps to 409",
			err:      fmt.Errorf("%w: page_blocks_content_id_fkey", databaseutil.ErrForeignKeyViolation),
			wantCode: http.StatusConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pages := &fakePageService{
				updateFn: func(context.Context, uuid.UUID, PageRequest) (Detail, error) {
					return Detail{}, tt.err
				},
			}
			mux := newTestMux(pages, &fakeBlockService{})

			rec := doRequest(t, mux, http.MethodPut, "/api/pages/"+pageID.String(), `{"title":"x","displayOrder":0}`)

			if rec.Code != tt.wantCode {
				t.Fatalf("status mismatch: want %d got %d, body=%s", tt.wantCode, rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Fatalf("expected a problem+json content type, got %q", ct)
			}
		})
	}
}

// The response shape has to match pages.tsp: camelCase, and PageDetail carries its
// blocks alongside the page's own fields rather than nested under a wrapper.
func TestHandlerGetPageResponseShape(t *testing.T) {
	pageID, courseID, resourceID := uuid.New(), uuid.New(), uuid.New()
	now := pgtype.Timestamptz{Time: mustParseTime(t, "2026-08-11T00:00:00Z"), Valid: true}

	pages := &fakePageService{
		getFn: func(context.Context, uuid.UUID, uuid.UUID) (Detail, error) {
			return Detail{
				Page: Page{ID: pageID, CourseID: courseID, Title: "Intro", DisplayOrder: 3, CreatedAt: now, UpdatedAt: now},
				Blocks: []Block{
					{ID: uuid.New(), PageID: pageID, Type: BlockTypeMedia, ResourceID: resourceID, DisplayOrder: 0, Required: true, CreatedAt: now, UpdatedAt: now},
				},
			}, nil
		},
	}
	mux := newTestMux(pages, &fakeBlockService{})

	rec := doRequest(t, mux, http.MethodGet, "/api/pages/"+pageID.String(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	for _, key := range []string{"id", "courseId", "title", "displayOrder", "createdAt", "updatedAt", "blocks"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("expected key %q in the page detail response, got %v", key, body)
		}
	}

	blocks, ok := body["blocks"].([]any)
	if !ok || len(blocks) != 1 {
		t.Fatalf("expected exactly one block, got %v", body["blocks"])
	}

	block, ok := blocks[0].(map[string]any)
	if !ok {
		t.Fatalf("expected a block object, got %v", blocks[0])
	}
	for _, key := range []string{"id", "pageId", "type", "resourceId", "displayOrder", "required", "createdAt", "updatedAt"} {
		if _, ok := block[key]; !ok {
			t.Fatalf("expected key %q in the block response, got %v", key, block)
		}
	}
	if block["type"] != BlockTypeMedia {
		t.Fatalf("expected the block type to survive, got %v", block["type"])
	}
	// content_id/question_id are internal; only the collapsed pair is exposed.
	for _, key := range []string{"contentId", "questionId"} {
		if _, ok := block[key]; ok {
			t.Fatalf("expected %q not to be exposed, got %v", key, block)
		}
	}
}

// An empty page must serialise blocks as [] rather than null, so clients can index
// into it without a nil check.
func TestHandlerListBlocksEmptyIsArray(t *testing.T) {
	pages := &fakePageService{
		listBlocksFn: func(context.Context, uuid.UUID, uuid.UUID) ([]Block, error) {
			return nil, nil
		},
	}
	mux := newTestMux(pages, &fakeBlockService{})

	rec := doRequest(t, mux, http.MethodGet, "/api/pages/"+uuid.New().String()+"/blocks", "")

	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Fatalf("expected an empty JSON array, got %q", got)
	}
}

type fakeRoleQuerier struct {
	roles []auth.Role
}

func (f fakeRoleQuerier) ActiveUserRoles(ctx context.Context, userID uuid.UUID) ([]auth.Role, error) {
	return f.roles, nil
}

// TestHandlerAuthorization exercises the real route split: authenticated
// Students reach reads so the service can apply Experiment-backed access, while
// writes remain EXPERIMENTER/ADMIN only.
func TestHandlerAuthorization(t *testing.T) {
	courseID, pageID := uuid.New(), uuid.New()
	blockID, resourceID := uuid.New(), uuid.New()
	actorID := uuid.New()

	newMux := func(roles []auth.Role) *http.ServeMux {
		handler := NewHandler(&fakePageService{}, &fakeBlockService{}, zap.NewNop())
		set := middlewareutil.NewSet(func(next http.HandlerFunc) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				next(w, r.WithContext(auth.ContextWithUserID(r.Context(), actorID)))
			}
		})
		mux := http.NewServeMux()
		handler.RegisterRoutes(mux, set, auth.NewAuthorizer(fakeRoleQuerier{roles: roles}, nil))
		return mux
	}

	routes := []struct {
		method string
		url    string
		body   string
		read   bool
	}{
		{http.MethodGet, "/api/courses/" + courseID.String() + "/pages", "", true},
		{http.MethodPost, "/api/courses/" + courseID.String() + "/pages", `{"title":"t","displayOrder":0}`, false},
		{http.MethodPut, "/api/courses/" + courseID.String() + "/pages/order", fmt.Sprintf(`{"pageIds":["%s"]}`, pageID), false},
		{http.MethodGet, "/api/pages/" + pageID.String(), "", true},
		{http.MethodPut, "/api/pages/" + pageID.String(), `{"title":"t","displayOrder":0}`, false},
		{http.MethodDelete, "/api/pages/" + pageID.String(), "", false},
		{http.MethodGet, "/api/pages/" + pageID.String() + "/blocks", "", true},
		{http.MethodPost, "/api/pages/" + pageID.String() + "/blocks",
			fmt.Sprintf(`{"type":"TEXT","resourceId":"%s","displayOrder":0,"required":true}`, resourceID), false},
		{http.MethodPut, "/api/pages/" + pageID.String() + "/blocks/order",
			fmt.Sprintf(`{"blockIds":["%s"]}`, blockID), false},
		{http.MethodPut, "/api/pages/" + pageID.String() + "/blocks/" + blockID.String(),
			fmt.Sprintf(`{"type":"TEXT","resourceId":"%s","displayOrder":0,"required":true}`, resourceID), false},
		{http.MethodDelete, "/api/pages/" + pageID.String() + "/blocks/" + blockID.String(), "", false},
	}

	tests := []struct {
		name  string
		roles []auth.Role
	}{
		{name: "student reaches reads only", roles: []auth.Role{auth.STUDENT}},
		{name: "experimenter is allowed", roles: []auth.Role{auth.EXPERIMENTER}},
		{name: "admin is allowed", roles: []auth.Role{auth.ADMIN}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newMux(tt.roles)
			for _, route := range routes {
				rec := doRequest(t, mux, route.method, route.url, route.body)

				if tt.roles[0] == auth.STUDENT && !route.read {
					if rec.Code != http.StatusForbidden {
						t.Fatalf("%s %s: want 403 for Student write, got %d", route.method, route.url, rec.Code)
					}
					continue
				}
				if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
					t.Fatalf("%s %s: unexpectedly refused with %d", route.method, route.url, rec.Code)
				}
			}
		})
	}
}

func TestHandlerReadRequiresActor(t *testing.T) {
	pages := &fakePageService{}
	mux := newTestMux(pages, &fakeBlockService{})
	req := httptest.NewRequest(http.MethodGet, "/api/pages/"+uuid.NewString(), nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without an actor, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if pages.callCount != 0 {
		t.Fatalf("expected no service call without an actor, got %d", pages.callCount)
	}
}
