//go:build integration

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
	"sciedu-backend/internal/content"
	"sciedu-backend/internal/course"
	"sciedu-backend/internal/experiment"
	"sciedu-backend/internal/question"

	middlewareutil "github.com/NYCU-SDC/summer/pkg/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// These tests drive the real handler over HTTP against a real database, which is
// where the fakes in the unit tests stop being able to help: they are the only
// thing that proves Postgres error codes reach the problem mapping, that the
// cross-package resource lookups are wired to real services, and that the
// constraints actually fire.

type integrationRoleQuerier struct {
	roles []auth.Role
}

func (q integrationRoleQuerier) ActiveUserRoles(context.Context, uuid.UUID) ([]auth.Role, error) {
	return q.roles, nil
}

// newAPI wires the package the same way cmd/backend/main.go does, then fronts it
// with a middleware that injects an actor carrying the given roles.
func newAPI(t *testing.T, pool *pgxpool.Pool, roles ...auth.Role) *http.ServeMux {
	t.Helper()
	return newAPIForActor(t, pool, uuid.New(), roles...)
}

func newAPIForActor(t *testing.T, pool *pgxpool.Pool, actorID uuid.UUID, roles ...auth.Role) *http.ServeMux {
	t.Helper()

	if len(roles) == 0 {
		roles = []auth.Role{auth.EXPERIMENTER}
	}
	logger := zap.NewNop()
	roleQuerier := integrationRoleQuerier{roles: roles}

	store := NewStore(pool)
	contentService := content.NewService(content.New(pool), logger)
	questionStore := question.NewStore(pool)
	questionService := question.NewQuestionService(questionStore, question.NewOptionService(questionStore, logger), logger)
	experimentStore := experiment.NewStore(pool)
	courseService := course.NewService(course.NewStore(pool), roleQuerier, experimentStore, logger)

	blockService := NewBlockService(store, store, contentService, questionService, logger)
	pageService := NewPageService(store, blockService, courseService, logger)
	handler := NewHandler(pageService, blockService, logger)

	set := middlewareutil.NewSet(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			next(w, r.WithContext(auth.ContextWithUserID(r.Context(), actorID)))
		}
	})

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, set, auth.NewAuthorizer(roleQuerier, nil))
	return mux
}

func call(t *testing.T, mux *http.ServeMux, method, url, body string) (int, string) {
	t.Helper()

	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, url, nil)
	} else {
		req = httptest.NewRequest(method, url, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func idFromJSON(t *testing.T, body string) uuid.UUID {
	t.Helper()

	var parsed struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("failed to decode id from %s: %v", body, err)
	}
	return parsed.ID
}

// seedCourse inserts a course directly, since the Courses CRUD API is deliberately
// not part of this feature.
func seedCourse(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()

	var courseID uuid.UUID
	code := "page-api-" + uuid.NewString()
	err := pool.QueryRow(t.Context(),
		"INSERT INTO courses (code, title, status) VALUES ($1, $2, 'PUBLISHED') RETURNING id",
		code, "Page API course").Scan(&courseID)
	if err != nil {
		t.Fatalf("failed to seed course: %v", err)
	}

	t.Cleanup(func() {
		//nolint:errcheck // best-effort cleanup
		pool.Exec(context.Background(), "DELETE FROM page_blocks WHERE page_id IN (SELECT id FROM pages WHERE course_id = $1)", courseID)
		//nolint:errcheck // best-effort cleanup
		pool.Exec(context.Background(), "DELETE FROM pages WHERE course_id = $1", courseID)
		//nolint:errcheck // best-effort cleanup
		pool.Exec(context.Background(), "DELETE FROM courses WHERE id = $1", courseID)
	})

	return courseID
}

func seedStudentExperimentAccess(t *testing.T, pool *pgxpool.Pool, courseID uuid.UUID) uuid.UUID {
	t.Helper()

	studentID := uuid.New()
	_, err := pool.Exec(t.Context(), `
		INSERT INTO users (id, email, name, roles)
		VALUES ($1, $2, $3, ARRAY['STUDENT']::user_role[])
	`, studentID, "page-api-"+studentID.String()+"@example.test", "Page API student")
	if err != nil {
		t.Fatalf("failed to seed Student: %v", err)
	}

	configuration := `{"maxAttempts":3,"allowRetry":true,"showScore":true,"showExplanations":true,"gradingMode":"AUTOMATIC","correctAnswerReleaseMode":"NEVER"}`
	var experimentID uuid.UUID
	err = pool.QueryRow(t.Context(), `
		INSERT INTO experiments (
			created_by, name, configuration, status, scheduled_start_at, scheduled_end_at
		) VALUES ($1, $2, $3::jsonb, 'ACTIVE', $4, $5)
		RETURNING id
	`, studentID, "Page API access", configuration, time.Now().Add(-time.Hour), time.Now().Add(time.Hour)).Scan(&experimentID)
	if err != nil {
		t.Fatalf("failed to seed Experiment: %v", err)
	}

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO experiment_participants (experiment_id, user_id) VALUES ($1, $2)
	`, experimentID, studentID); err != nil {
		t.Fatalf("failed to seed Experiment participant: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO experiment_courses (experiment_id, course_id) VALUES ($1, $2)
	`, experimentID, courseID); err != nil {
		t.Fatalf("failed to seed Experiment course: %v", err)
	}

	t.Cleanup(func() {
		//nolint:errcheck // cascades remove participant and course links
		pool.Exec(context.Background(), "DELETE FROM experiments WHERE id = $1", experimentID)
		//nolint:errcheck // best-effort cleanup for an isolated integration fixture
		pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", studentID)
	})
	return studentID
}

func seedContent(t *testing.T, pool *pgxpool.Pool, contentType string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := pool.QueryRow(t.Context(),
		"INSERT INTO contents (type, content) VALUES ($1::content_type, $2) RETURNING id",
		contentType, "seeded "+contentType).Scan(&id)
	if err != nil {
		t.Fatalf("failed to seed content: %v", err)
	}

	t.Cleanup(func() {
		//nolint:errcheck // best-effort cleanup
		pool.Exec(context.Background(), "DELETE FROM contents WHERE id = $1", id)
	})
	return id
}

func seedQuestion(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := pool.QueryRow(t.Context(),
		"INSERT INTO questions (content, type) VALUES ($1, 'TEXT') RETURNING id",
		"seeded question").Scan(&id)
	if err != nil {
		t.Fatalf("failed to seed question: %v", err)
	}

	t.Cleanup(func() {
		//nolint:errcheck // best-effort cleanup
		pool.Exec(context.Background(), "DELETE FROM questions WHERE id = $1", id)
	})
	return id
}

func newPageViaAPI(t *testing.T, mux *http.ServeMux, courseID uuid.UUID, title string, order int) uuid.UUID {
	t.Helper()

	code, body := call(t, mux, http.MethodPost, "/api/courses/"+courseID.String()+"/pages",
		fmt.Sprintf(`{"title":%q,"displayOrder":%d}`, title, order))
	if code != http.StatusCreated {
		t.Fatalf("failed to create page %q: %d %s", title, code, body)
	}
	return idFromJSON(t, body)
}

func newBlockViaAPI(t *testing.T, mux *http.ServeMux, pageID, resourceID uuid.UUID, order int32) uuid.UUID {
	t.Helper()

	code, body := call(t, mux, http.MethodPost, "/api/pages/"+pageID.String()+"/blocks",
		fmt.Sprintf(`{"type":"TEXT","resourceId":"%s","displayOrder":%d,"required":false}`, resourceID, order))
	if code != http.StatusCreated {
		t.Fatalf("failed to create block at order %d: %d %s", order, code, body)
	}
	return idFromJSON(t, body)
}

// A duplicate displayOrder has to come back as 409. The unit test feeds the
// mapping a hand-made ErrUniqueViolation; only a real database proves that
// Postgres 23505 travels through WrapDBError into that mapping.
func TestAPIDuplicateDisplayOrderReturnsConflict(t *testing.T) {
	pool := newIntegrationPool(t)
	mux := newAPI(t, pool)
	courseID := seedCourse(t, pool)

	newPageViaAPI(t, mux, courseID, "First", 0)

	code, body := call(t, mux, http.MethodPost, "/api/courses/"+courseID.String()+"/pages",
		`{"title":"Duplicate","displayOrder":0}`)

	if code != http.StatusConflict {
		t.Fatalf("expected 409 for a duplicate displayOrder, got %d %s", code, body)
	}
	if !strings.Contains(body, "application/problem+json") && !strings.Contains(body, "Conflict") {
		t.Fatalf("expected a conflict problem document, got %s", body)
	}
}

func TestAPIReorderPages(t *testing.T) {
	pool := newIntegrationPool(t)
	mux := newAPI(t, pool)
	courseID := seedCourse(t, pool)

	first := newPageViaAPI(t, mux, courseID, "First", 0)
	second := newPageViaAPI(t, mux, courseID, "Second", 1)

	code, body := call(t, mux, http.MethodPut, "/api/courses/"+courseID.String()+"/pages/order",
		fmt.Sprintf(`{"pageIds":["%s","%s"]}`, second, first))
	if code != http.StatusOK {
		t.Fatalf("expected 200 from reorder, got %d %s", code, body)
	}

	var got []struct {
		ID           uuid.UUID `json:"id"`
		DisplayOrder int32     `json:"displayOrder"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("failed to decode reorder response: %v", err)
	}
	if len(got) != 2 || got[0].ID != second || got[1].ID != first {
		t.Fatalf("expected [second, first], got %s", body)
	}
	if got[0].DisplayOrder != 0 || got[1].DisplayOrder != 1 {
		t.Fatalf("expected display orders 0 and 1, got %s", body)
	}
}

func TestAPIReorderSupportsMaxInt32DisplayOrders(t *testing.T) {
	pool := newIntegrationPool(t)
	contentID := seedContent(t, pool, "TEXT")
	mux := newAPI(t, pool)
	courseID := seedCourse(t, pool)

	maxPage := newPageViaAPI(t, mux, courseID, "Maximum", 2147483647)
	zeroPage := newPageViaAPI(t, mux, courseID, "Zero", 0)

	code, body := call(t, mux, http.MethodPut, "/api/courses/"+courseID.String()+"/pages/order",
		fmt.Sprintf(`{"pageIds":["%s","%s"]}`, maxPage, zeroPage))
	if code != http.StatusOK {
		t.Fatalf("expected 200 reordering pages from max int32, got %d %s", code, body)
	}

	maxBlock := newBlockViaAPI(t, mux, maxPage, contentID, 2147483647)
	zeroBlock := newBlockViaAPI(t, mux, maxPage, contentID, 0)
	code, body = call(t, mux, http.MethodPut, "/api/pages/"+maxPage.String()+"/blocks/order",
		fmt.Sprintf(`{"blockIds":["%s","%s"]}`, maxBlock, zeroBlock))
	if code != http.StatusOK {
		t.Fatalf("expected 200 reordering blocks from max int32, got %d %s", code, body)
	}

	var pageOrder, maxBlockOrder, zeroBlockOrder int32
	if err := pool.QueryRow(t.Context(), "SELECT display_order FROM pages WHERE id = $1", maxPage).Scan(&pageOrder); err != nil {
		t.Fatalf("failed to read reordered page: %v", err)
	}
	if err := pool.QueryRow(t.Context(), "SELECT display_order FROM page_blocks WHERE id = $1", maxBlock).Scan(&maxBlockOrder); err != nil {
		t.Fatalf("failed to read max block: %v", err)
	}
	if err := pool.QueryRow(t.Context(), "SELECT display_order FROM page_blocks WHERE id = $1", zeroBlock).Scan(&zeroBlockOrder); err != nil {
		t.Fatalf("failed to read zero block: %v", err)
	}
	if pageOrder != 0 || maxBlockOrder != 0 || zeroBlockOrder != 1 {
		t.Fatalf("unexpected final orders: page=%d maxBlock=%d zeroBlock=%d", pageOrder, maxBlockOrder, zeroBlockOrder)
	}
}

// Exercises the cross-package resource lookup against the real content and
// question services rather than fakes.
func TestAPIBlockResourceValidation(t *testing.T) {
	pool := newIntegrationPool(t)
	mux := newAPI(t, pool)
	courseID := seedCourse(t, pool)
	pageID := newPageViaAPI(t, mux, courseID, "Blocks", 0)

	textContentID := seedContent(t, pool, "TEXT")
	mediaContentID := seedContent(t, pool, "MEDIA")
	questionID := seedQuestion(t, pool)
	blocksURL := "/api/pages/" + pageID.String() + "/blocks"

	tests := []struct {
		name     string
		body     string
		wantCode int
	}{
		{
			name:     "TEXT block against a TEXT content",
			body:     fmt.Sprintf(`{"type":"TEXT","resourceId":"%s","displayOrder":0,"required":true}`, textContentID),
			wantCode: http.StatusCreated,
		},
		{
			name:     "MEDIA block against a MEDIA content",
			body:     fmt.Sprintf(`{"type":"MEDIA","resourceId":"%s","displayOrder":1,"required":false}`, mediaContentID),
			wantCode: http.StatusCreated,
		},
		{
			name:     "QUESTION block against a question",
			body:     fmt.Sprintf(`{"type":"QUESTION","resourceId":"%s","displayOrder":2,"required":true}`, questionID),
			wantCode: http.StatusCreated,
		},
		{
			// The FK cannot catch this: TEXT and MEDIA share content_id, so only
			// contents.type distinguishes them.
			name:     "TEXT block pointing at a MEDIA content is rejected",
			body:     fmt.Sprintf(`{"type":"TEXT","resourceId":"%s","displayOrder":3,"required":true}`, mediaContentID),
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "QUESTION block pointing at a content is rejected",
			body:     fmt.Sprintf(`{"type":"QUESTION","resourceId":"%s","displayOrder":4,"required":true}`, textContentID),
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "unknown resourceId is rejected",
			body:     fmt.Sprintf(`{"type":"TEXT","resourceId":"%s","displayOrder":5,"required":true}`, uuid.New()),
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, body := call(t, mux, http.MethodPost, blocksURL, tt.body)
			if code != tt.wantCode {
				t.Fatalf("want %d got %d, body=%s", tt.wantCode, code, body)
			}
		})
	}

	// The stored blocks must report the type they were created with, which the
	// service can only know by reading contents.type back.
	code, body := call(t, mux, http.MethodGet, blocksURL, "")
	if code != http.StatusOK {
		t.Fatalf("expected 200 listing blocks, got %d %s", code, body)
	}

	var blocks []struct {
		Type       string    `json:"type"`
		ResourceID uuid.UUID `json:"resourceId"`
	}
	if err := json.Unmarshal([]byte(body), &blocks); err != nil {
		t.Fatalf("failed to decode blocks: %v", err)
	}
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %s", body)
	}
	wantTypes := map[uuid.UUID]string{
		textContentID:  BlockTypeText,
		mediaContentID: BlockTypeMedia,
		questionID:     BlockTypeQuestion,
	}
	for _, block := range blocks {
		if want := wantTypes[block.ResourceID]; want != block.Type {
			t.Fatalf("resource %s came back as %q, expected %q", block.ResourceID, block.Type, want)
		}
	}
}

// Deleting a content a block still references must be a 409, not a 500. The FK is
// ON DELETE RESTRICT, and summer leaves 23503 unmapped without the content
// handler's mapping.
func TestAPIDeleteReferencedContentReturnsConflict(t *testing.T) {
	pool := newIntegrationPool(t)
	mux := newAPI(t, pool)
	courseID := seedCourse(t, pool)
	pageID := newPageViaAPI(t, mux, courseID, "Blocks", 0)
	contentID := seedContent(t, pool, "TEXT")

	code, body := call(t, mux, http.MethodPost, "/api/pages/"+pageID.String()+"/blocks",
		fmt.Sprintf(`{"type":"TEXT","resourceId":"%s","displayOrder":0,"required":true}`, contentID))
	if code != http.StatusCreated {
		t.Fatalf("failed to create block: %d %s", code, body)
	}

	logger := zap.NewNop()
	contentHandler := content.NewHandler(content.NewService(content.New(pool), logger), logger)
	contentMux := http.NewServeMux()
	contentHandler.RegisterRoutes(contentMux, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/content/"+contentID.String(), nil)
	rec := httptest.NewRecorder()
	contentMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 deleting a referenced content, got %d %s", rec.Code, rec.Body.String())
	}
}

// Same RESTRICT rule, other side of the block: a question a block points at
// cannot be deleted either.
func TestAPIDeleteReferencedQuestionReturnsConflict(t *testing.T) {
	pool := newIntegrationPool(t)
	mux := newAPI(t, pool)
	courseID := seedCourse(t, pool)
	pageID := newPageViaAPI(t, mux, courseID, "Blocks", 0)
	questionID := seedQuestion(t, pool)

	code, body := call(t, mux, http.MethodPost, "/api/pages/"+pageID.String()+"/blocks",
		fmt.Sprintf(`{"type":"QUESTION","resourceId":"%s","displayOrder":0,"required":true}`, questionID))
	if code != http.StatusCreated {
		t.Fatalf("failed to create block: %d %s", code, body)
	}

	logger := zap.NewNop()
	questionStore := question.NewStore(pool)
	questionService := question.NewQuestionService(questionStore, question.NewOptionService(questionStore, logger), logger)
	answerService := question.NewAnswerService(questionStore, questionService, logger)
	questionMux := http.NewServeMux()
	question.NewHandler(questionService, answerService, logger).RegisterRoutes(questionMux, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/questions/"+questionID.String(), nil)
	rec := httptest.NewRecorder()
	questionMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 deleting a referenced question, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestAPIDeletePageCascadesBlocks(t *testing.T) {
	pool := newIntegrationPool(t)
	mux := newAPI(t, pool)
	courseID := seedCourse(t, pool)
	pageID := newPageViaAPI(t, mux, courseID, "Doomed", 0)
	contentID := seedContent(t, pool, "TEXT")

	code, body := call(t, mux, http.MethodPost, "/api/pages/"+pageID.String()+"/blocks",
		fmt.Sprintf(`{"type":"TEXT","resourceId":"%s","displayOrder":0,"required":true}`, contentID))
	if code != http.StatusCreated {
		t.Fatalf("failed to create block: %d %s", code, body)
	}

	if code, body := call(t, mux, http.MethodDelete, "/api/pages/"+pageID.String(), ""); code != http.StatusNoContent {
		t.Fatalf("expected 204 deleting the page, got %d %s", code, body)
	}

	var remaining int
	if err := pool.QueryRow(t.Context(),
		"SELECT count(*) FROM page_blocks WHERE page_id = $1", pageID).Scan(&remaining); err != nil {
		t.Fatalf("failed to count blocks: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expected the page's blocks to be cascaded away, %d remain", remaining)
	}

	// The content itself is shared material and must survive its block.
	var contentCount int
	if err := pool.QueryRow(t.Context(),
		"SELECT count(*) FROM contents WHERE id = $1", contentID).Scan(&contentCount); err != nil {
		t.Fatalf("failed to count contents: %v", err)
	}
	if contentCount != 1 {
		t.Fatalf("expected the content to survive the page deletion, found %d", contentCount)
	}
}

func TestAPIPageAndBlockUpdateDeleteLifecycle(t *testing.T) {
	pool := newIntegrationPool(t)
	mux := newAPI(t, pool)
	courseID := seedCourse(t, pool)
	pageID := newPageViaAPI(t, mux, courseID, "Initial page", 0)
	initialContentID := seedContent(t, pool, "TEXT")
	updatedContentID := seedContent(t, pool, "TEXT")
	blockID := newBlockViaAPI(t, mux, pageID, initialContentID, 0)

	code, body := call(t, mux, http.MethodPut, "/api/pages/"+pageID.String(), `{"title":"Updated page","displayOrder":0}`)
	if code != http.StatusOK {
		t.Fatalf("expected 200 updating page, got %d %s", code, body)
	}
	var updatedPage pageDetailResponse
	if err := json.Unmarshal([]byte(body), &updatedPage); err != nil {
		t.Fatalf("failed to decode updated page: %v", err)
	}
	if updatedPage.Title != "Updated page" || updatedPage.DisplayOrder != 0 {
		t.Fatalf("unexpected updated page response: %s", body)
	}
	if len(updatedPage.Blocks) != 1 || updatedPage.Blocks[0].ID != blockID {
		t.Fatalf("page update did not retain its block: %s", body)
	}

	blocksURL := "/api/pages/" + pageID.String() + "/blocks"
	code, body = call(t, mux, http.MethodPut, blocksURL+"/"+blockID.String(), fmt.Sprintf(
		`{"type":"TEXT","resourceId":"%s","displayOrder":0,"required":true}`, updatedContentID,
	))
	if code != http.StatusOK {
		t.Fatalf("expected 200 updating block, got %d %s", code, body)
	}
	var updatedBlock blockResponse
	if err := json.Unmarshal([]byte(body), &updatedBlock); err != nil {
		t.Fatalf("failed to decode updated block: %v", err)
	}
	if updatedBlock.ID != blockID || updatedBlock.ResourceID != updatedContentID || !updatedBlock.Required {
		t.Fatalf("unexpected updated block response: %s", body)
	}

	code, body = call(t, mux, http.MethodGet, "/api/pages/"+pageID.String(), "")
	if code != http.StatusOK {
		t.Fatalf("expected 200 reading updated page, got %d %s", code, body)
	}
	var fetched pageDetailResponse
	if err := json.Unmarshal([]byte(body), &fetched); err != nil {
		t.Fatalf("failed to decode fetched page: %v", err)
	}
	if fetched.Title != "Updated page" || len(fetched.Blocks) != 1 || fetched.Blocks[0].ResourceID != updatedContentID {
		t.Fatalf("updates were not persisted: %s", body)
	}

	code, body = call(t, mux, http.MethodDelete, blocksURL+"/"+blockID.String(), "")
	if code != http.StatusNoContent || len(body) != 0 {
		t.Fatalf("expected empty 204 deleting block, got %d %s", code, body)
	}

	code, body = call(t, mux, http.MethodGet, blocksURL, "")
	if code != http.StatusOK {
		t.Fatalf("expected 200 listing blocks after delete, got %d %s", code, body)
	}
	var blocks []blockResponse
	if err := json.Unmarshal([]byte(body), &blocks); err != nil || len(blocks) != 0 {
		t.Fatalf("expected an empty block list after delete, got %s: %v", body, err)
	}

	var remaining int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM page_blocks WHERE id = $1", blockID).Scan(&remaining); err != nil {
		t.Fatalf("failed to verify block deletion: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expected block %s to be deleted, found %d rows", blockID, remaining)
	}
}

func TestAPIStudentReadAccessAndWriteDenial(t *testing.T) {
	pool := newIntegrationPool(t)
	courseID := seedCourse(t, pool)

	experimenter := newAPI(t, pool, auth.EXPERIMENTER)
	pageID := newPageViaAPI(t, experimenter, courseID, "Locked", 0)

	studentID := seedStudentExperimentAccess(t, pool, courseID)
	student := newAPIForActor(t, pool, studentID, auth.STUDENT)
	blocksURL := "/api/pages/" + pageID.String() + "/blocks"

	readRoutes := []struct {
		url string
	}{
		{"/api/courses/" + courseID.String() + "/pages"},
		{"/api/pages/" + pageID.String()},
		{blocksURL},
	}
	for _, route := range readRoutes {
		code, body := call(t, student, http.MethodGet, route.url, "")
		if code != http.StatusOK {
			t.Fatalf("GET %s: expected 200 for assigned Student, got %d %s", route.url, code, body)
		}
	}

	writeRoutes := []struct {
		method string
		url    string
		body   string
	}{
		{http.MethodPost, "/api/courses/" + courseID.String() + "/pages", `{"title":"x","displayOrder":9}`},
		{http.MethodPut, "/api/courses/" + courseID.String() + "/pages/order", fmt.Sprintf(`{"pageIds":["%s"]}`, pageID)},
		{http.MethodPut, "/api/pages/" + pageID.String(), `{"title":"x","displayOrder":0}`},
		{http.MethodDelete, "/api/pages/" + pageID.String(), ""},
		{http.MethodPost, blocksURL, `{"type":"TEXT","resourceId":"` + uuid.New().String() + `","displayOrder":0,"required":true}`},
		{http.MethodPut, blocksURL + "/order", fmt.Sprintf(`{"blockIds":["%s"]}`, uuid.New())},
		{http.MethodPut, blocksURL + "/" + uuid.New().String(), `{"type":"TEXT","resourceId":"` + uuid.New().String() + `","displayOrder":0,"required":true}`},
		{http.MethodDelete, blocksURL + "/" + uuid.New().String(), ""},
	}

	for _, route := range writeRoutes {
		code, body := call(t, student, route.method, route.url, route.body)
		if code != http.StatusForbidden {
			t.Fatalf("%s %s: expected 403 for a Student write, got %d %s", route.method, route.url, code, body)
		}
	}

	unassigned := newAPIForActor(t, pool, uuid.New(), auth.STUDENT)
	for _, route := range readRoutes {
		code, body := call(t, unassigned, http.MethodGet, route.url, "")
		if code != http.StatusForbidden {
			t.Fatalf("GET %s: expected 403 for unassigned Student, got %d %s", route.url, code, body)
		}
	}

	// The page must still be intact: Student writes never reach the DB.
	var pageCount int
	if err := pool.QueryRow(t.Context(),
		"SELECT count(*) FROM pages WHERE course_id = $1", courseID).Scan(&pageCount); err != nil {
		t.Fatalf("failed to count pages: %v", err)
	}
	if pageCount != 1 {
		t.Fatalf("expected the student's calls to change nothing, found %d pages", pageCount)
	}
}
