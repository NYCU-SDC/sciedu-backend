package content

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/zap"
)

type fakeHandlerService struct {
	createMediaContentFn func(ctx context.Context, raw []byte, filename string, dir string) (Content, error)
	getMediaContentFn    func(ctx context.Context, id uuid.UUID) (Content, error)
	listTextContentsFn   func(ctx context.Context, page, pageSize int32) (TextPage, error)
	createTextContentFn  func(ctx context.Context, content string) (Content, error)
	batchGetTextFn       func(ctx context.Context, ids []uuid.UUID) ([]Content, error)
	getTextContentFn     func(ctx context.Context, id uuid.UUID) (Content, error)
	getContentFn         func(ctx context.Context, id uuid.UUID) (Content, error)
	deleteContentFn      func(ctx context.Context, id uuid.UUID) error
}

func (f *fakeHandlerService) CreateMediaContent(ctx context.Context, raw []byte, filename string, dir string) (Content, error) {
	if f.createMediaContentFn != nil {
		return f.createMediaContentFn(ctx, raw, filename, dir)
	}
	return Content{}, nil
}

func (f *fakeHandlerService) GetMediaContent(ctx context.Context, id uuid.UUID) (Content, error) {
	if f.getMediaContentFn != nil {
		return f.getMediaContentFn(ctx, id)
	}
	return Content{}, nil
}

func (f *fakeHandlerService) ListTextContents(ctx context.Context, page, pageSize int32) (TextPage, error) {
	if f.listTextContentsFn != nil {
		return f.listTextContentsFn(ctx, page, pageSize)
	}
	return TextPage{}, nil
}

func (f *fakeHandlerService) CreateTextContent(ctx context.Context, content string) (Content, error) {
	if f.createTextContentFn != nil {
		return f.createTextContentFn(ctx, content)
	}
	return Content{}, nil
}

func (f *fakeHandlerService) BatchGetTextContents(ctx context.Context, ids []uuid.UUID) ([]Content, error) {
	if f.batchGetTextFn != nil {
		return f.batchGetTextFn(ctx, ids)
	}
	return nil, nil
}

func (f *fakeHandlerService) GetTextContent(ctx context.Context, id uuid.UUID) (Content, error) {
	if f.getTextContentFn != nil {
		return f.getTextContentFn(ctx, id)
	}
	return Content{}, nil
}

func (f *fakeHandlerService) GetContent(ctx context.Context, id uuid.UUID) (Content, error) {
	if f.getContentFn != nil {
		return f.getContentFn(ctx, id)
	}
	return Content{}, nil
}

func (f *fakeHandlerService) DeleteContent(ctx context.Context, id uuid.UUID) error {
	if f.deleteContentFn != nil {
		return f.deleteContentFn(ctx, id)
	}
	return nil
}

func newContentTestMux(svc *fakeHandlerService) *http.ServeMux {
	h := NewHandler(svc, zap.NewNop())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, nil)
	return mux
}

func TestCreateText(t *testing.T) {
	id := uuid.New()
	tests := []struct {
		name       string
		body       string
		service    *fakeHandlerService
		wantStatus int
		assertBody func(t *testing.T, body string)
	}{
		{
			name:       "invalid payload",
			body:       `{}`,
			service:    &fakeHandlerService{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "success",
			body: `{"content":"hello"}`,
			service: &fakeHandlerService{
				createTextContentFn: func(context.Context, string) (Content, error) {
					return Content{ID: id, Type: "TEXT", Content: pgtype.Text{String: "hello", Valid: true}}, nil
				},
			},
			wantStatus: http.StatusCreated,
			assertBody: func(t *testing.T, body string) {
				t.Helper()
				var got map[string]any
				if err := json.Unmarshal([]byte(body), &got); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				if got["type"] != "TEXT" || got["content"] != "hello" {
					t.Fatalf("unexpected response body: %v", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/content/text", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			newContentTestMux(tt.service).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status mismatch: want %d got %d body=%s", tt.wantStatus, rec.Code, rec.Body.String())
			}
			if tt.assertBody != nil {
				tt.assertBody(t, rec.Body.String())
			}
		})
	}
}

func TestListText(t *testing.T) {
	id := uuid.New()
	tests := []struct {
		name       string
		path       string
		service    *fakeHandlerService
		wantStatus int
		assertBody func(t *testing.T, body string)
	}{
		{
			name:       "invalid query",
			path:       "/api/content/text?page=abc",
			service:    &fakeHandlerService{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "success",
			path: "/api/content/text?page=1&pageSize=20",
			service: &fakeHandlerService{
				listTextContentsFn: func(context.Context, int32, int32) (TextPage, error) {
					return TextPage{
						Items:       []Content{{ID: id, Type: "TEXT", Content: pgtype.Text{String: "data", Valid: true}}},
						TotalPages:  1,
						TotalItems:  1,
						CurrentPage: 1,
						PageSize:    20,
						HasNextPage: false,
					}, nil
				},
			},
			wantStatus: http.StatusOK,
			assertBody: func(t *testing.T, body string) {
				t.Helper()
				var got map[string]any
				if err := json.Unmarshal([]byte(body), &got); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				items, ok := got["items"].([]any)
				if !ok || len(items) != 1 {
					t.Fatalf("expected one item, got %v", got["items"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)

			newContentTestMux(tt.service).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status mismatch: want %d got %d body=%s", tt.wantStatus, rec.Code, rec.Body.String())
			}
			if tt.assertBody != nil {
				tt.assertBody(t, rec.Body.String())
			}
		})
	}
}

func TestDelete(t *testing.T) {
	id := uuid.New()
	mediaFile := filepath.Join(t.TempDir(), "to-delete.bin")
	if err := os.WriteFile(mediaFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("write media file: %v", err)
	}
	mediaFileKeep := filepath.Join(t.TempDir(), "to-keep.bin")
	if err := os.WriteFile(mediaFileKeep, []byte("x"), 0o644); err != nil {
		t.Fatalf("write media file: %v", err)
	}
	tests := []struct {
		name       string
		path       string
		service    *fakeHandlerService
		wantStatus int
		assert     func(t *testing.T)
	}{
		{
			name:       "invalid uuid",
			path:       "/api/content/not-a-uuid",
			service:    &fakeHandlerService{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "success",
			path: "/api/content/" + id.String(),
			service: &fakeHandlerService{
				getContentFn: func(context.Context, uuid.UUID) (Content, error) {
					return Content{ID: id, Type: "TEXT", Content: pgtype.Text{String: "x", Valid: true}}, nil
				},
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "delete error",
			path: "/api/content/" + id.String(),
			service: &fakeHandlerService{
				getContentFn: func(context.Context, uuid.UUID) (Content, error) {
					return Content{ID: id, Type: "TEXT", Content: pgtype.Text{String: "x", Valid: true}}, nil
				},
				deleteContentFn: func(context.Context, uuid.UUID) error {
					return errors.New("boom")
				},
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "delete error keeps media file",
			path: "/api/content/" + id.String(),
			service: &fakeHandlerService{
				getContentFn: func(context.Context, uuid.UUID) (Content, error) {
					return Content{ID: id, Type: "MEDIA", Content: pgtype.Text{String: mediaFileKeep, Valid: true}}, nil
				},
				deleteContentFn: func(context.Context, uuid.UUID) error {
					return errors.New("boom")
				},
			},
			wantStatus: http.StatusInternalServerError,
			assert: func(t *testing.T) {
				t.Helper()
				if _, err := os.Stat(mediaFileKeep); err != nil {
					t.Fatalf("expected media file kept, stat err=%v", err)
				}
			},
		},
		{
			name: "success media deletes file",
			path: "/api/content/" + id.String(),
			service: &fakeHandlerService{
				getContentFn: func(context.Context, uuid.UUID) (Content, error) {
					return Content{ID: id, Type: "MEDIA", Content: pgtype.Text{String: mediaFile, Valid: true}}, nil
				},
			},
			wantStatus: http.StatusNoContent,
			assert: func(t *testing.T) {
				t.Helper()
				if _, err := os.Stat(mediaFile); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("expected media file deleted, stat err=%v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodDelete, tt.path, nil)
			newContentTestMux(tt.service).ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status mismatch: want %d got %d body=%s", tt.wantStatus, rec.Code, rec.Body.String())
			}
			if tt.assert != nil {
				tt.assert(t)
			}
		})
	}
}

func TestCreateMedia(t *testing.T) {
	tests := []struct {
		name       string
		buildReq   func(t *testing.T) *http.Request
		service    *fakeHandlerService
		wantStatus int
	}{
		{
			name: "missing multipart field",
			buildReq: func(t *testing.T) *http.Request {
				t.Helper()
				req := httptest.NewRequest(http.MethodPost, "/api/content/media", bytes.NewReader(nil))
				req.Header.Set("Content-Type", "multipart/form-data; boundary=abc")
				return req
			},
			service:    &fakeHandlerService{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "success",
			buildReq: func(t *testing.T) *http.Request {
				t.Helper()
				var body bytes.Buffer
				writer := multipart.NewWriter(&body)
				part, err := writer.CreateFormFile("content", "a.txt")
				if err != nil {
					t.Fatalf("create form file: %v", err)
				}
				if _, err := part.Write([]byte("hello")); err != nil {
					t.Fatalf("write part: %v", err)
				}
				if err := writer.Close(); err != nil {
					t.Fatalf("close writer: %v", err)
				}
				req := httptest.NewRequest(http.MethodPost, "/api/content/media", &body)
				req.Header.Set("Content-Type", writer.FormDataContentType())
				return req
			},
			service: &fakeHandlerService{
				createMediaContentFn: func(_ context.Context, raw []byte, filename string, _ string) (Content, error) {
					if string(raw) != "hello" {
						t.Fatalf("unexpected media input raw=%q filename=%s", string(raw), filename)
					}
					ext := filepath.Ext(filename)
					if ext != ".txt" {
						t.Fatalf("expected extension .txt, got %q", ext)
					}
					if _, err := uuid.Parse(strings.TrimSuffix(filename, ext)); err != nil {
						t.Fatalf("expected generated uuid filename, got %q", filename)
					}
					return Content{ID: uuid.New(), Type: "MEDIA", Content: pgtype.Text{String: "./contents/a.txt", Valid: true}}, nil
				},
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "file exceeds upload limit",
			buildReq: func(t *testing.T) *http.Request {
				t.Helper()
				originalMaxMediaUploadBytes := maxMediaUploadBytes
				maxMediaUploadBytes = 8
				t.Cleanup(func() {
					maxMediaUploadBytes = originalMaxMediaUploadBytes
				})
				var body bytes.Buffer
				writer := multipart.NewWriter(&body)
				part, err := writer.CreateFormFile("content", "large.bin")
				if err != nil {
					t.Fatalf("create form file: %v", err)
				}
				if _, err := part.Write(bytes.Repeat([]byte("x"), int(maxMediaUploadBytes)+1)); err != nil {
					t.Fatalf("write part: %v", err)
				}
				if err := writer.Close(); err != nil {
					t.Fatalf("close writer: %v", err)
				}
				req := httptest.NewRequest(http.MethodPost, "/api/content/media", &body)
				req.Header.Set("Content-Type", writer.FormDataContentType())
				return req
			},
			service: &fakeHandlerService{
				createMediaContentFn: func(context.Context, []byte, string, string) (Content, error) {
					t.Fatal("service should not be called for oversized uploads")
					return Content{}, nil
				},
			},
			wantStatus: http.StatusRequestEntityTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			newContentTestMux(tt.service).ServeHTTP(rec, tt.buildReq(t))
			if rec.Code != tt.wantStatus {
				t.Fatalf("status mismatch: want %d got %d body=%s", tt.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestStreamMedia(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.bin")
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	id := uuid.New()

	tests := []struct {
		name       string
		path       string
		service    *fakeHandlerService
		wantStatus int
		assertBody func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name:       "invalid uuid",
			path:       "/api/content/media/not-a-uuid",
			service:    &fakeHandlerService{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "success",
			path: "/api/content/media/" + id.String(),
			service: &fakeHandlerService{
				getMediaContentFn: func(context.Context, uuid.UUID) (Content, error) {
					return Content{ID: id, Type: "MEDIA", Content: pgtype.Text{String: path, Valid: true}}, nil
				},
			},
			wantStatus: http.StatusOK,
			assertBody: func(t *testing.T, rec *httptest.ResponseRecorder) {
				t.Helper()
				if rec.Body.String() != "abc" {
					t.Fatalf("unexpected streamed body: %q", rec.Body.String())
				}
				if rec.Header().Get("Content-Length") != "3" {
					t.Fatalf("unexpected content-length: %s", rec.Header().Get("Content-Length"))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			newContentTestMux(tt.service).ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status mismatch: want %d got %d body=%s", tt.wantStatus, rec.Code, rec.Body.String())
			}
			if tt.assertBody != nil {
				tt.assertBody(t, rec)
			}
		})
	}
}
