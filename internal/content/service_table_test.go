package content

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/zap"
)

type fakeMediaQuerier struct {
	createMediaContentFn   func(ctx context.Context, content pgtype.Text) (Content, error)
	createMediaContentArgs []string
	createTextContentFn    func(ctx context.Context, content pgtype.Text) (Content, error)
	createTextContentArgs  []string
	getMediaContentFn      func(ctx context.Context, id uuid.UUID) (Content, error)
	getTextContentFn       func(ctx context.Context, id uuid.UUID) (Content, error)
	getContentFn           func(ctx context.Context, id uuid.UUID) (Content, error)
	listTextContentsFn     func(ctx context.Context, arg ListTextContentsParams) ([]Content, error)
	countTextContentsFn    func(ctx context.Context) (int64, error)
	batchGetTextContentsFn func(ctx context.Context, ids []uuid.UUID) ([]Content, error)
	deleteContentFn        func(ctx context.Context, id uuid.UUID) error
}

func (f *fakeMediaQuerier) CreateMediaContent(ctx context.Context, content pgtype.Text) (Content, error) {
	f.createMediaContentArgs = append(f.createMediaContentArgs, content.String)
	if f.createMediaContentFn != nil {
		return f.createMediaContentFn(ctx, content)
	}
	return Content{}, nil
}

func (f *fakeMediaQuerier) CreateTextContent(ctx context.Context, content pgtype.Text) (Content, error) {
	f.createTextContentArgs = append(f.createTextContentArgs, content.String)
	if f.createTextContentFn != nil {
		return f.createTextContentFn(ctx, content)
	}
	return Content{}, nil
}

func (f *fakeMediaQuerier) GetMediaContent(ctx context.Context, id uuid.UUID) (Content, error) {
	if f.getMediaContentFn != nil {
		return f.getMediaContentFn(ctx, id)
	}
	return Content{}, nil
}

func (f *fakeMediaQuerier) GetTextContent(ctx context.Context, id uuid.UUID) (Content, error) {
	if f.getTextContentFn != nil {
		return f.getTextContentFn(ctx, id)
	}
	return Content{}, nil
}

func (f *fakeMediaQuerier) GetContent(ctx context.Context, id uuid.UUID) (Content, error) {
	if f.getContentFn != nil {
		return f.getContentFn(ctx, id)
	}
	return Content{}, nil
}

func (f *fakeMediaQuerier) ListTextContents(ctx context.Context, arg ListTextContentsParams) ([]Content, error) {
	if f.listTextContentsFn != nil {
		return f.listTextContentsFn(ctx, arg)
	}
	return nil, nil
}

func (f *fakeMediaQuerier) CountTextContents(ctx context.Context) (int64, error) {
	if f.countTextContentsFn != nil {
		return f.countTextContentsFn(ctx)
	}
	return 0, nil
}

func (f *fakeMediaQuerier) BatchGetTextContents(ctx context.Context, ids []uuid.UUID) ([]Content, error) {
	if f.batchGetTextContentsFn != nil {
		return f.batchGetTextContentsFn(ctx, ids)
	}
	return nil, nil
}

func (f *fakeMediaQuerier) DeleteContent(ctx context.Context, id uuid.UUID) error {
	if f.deleteContentFn != nil {
		return f.deleteContentFn(ctx, id)
	}
	return nil
}

func TestCreateMediaContent(t *testing.T) {
	tests := []struct {
		name       string
		raw        []byte
		file       string
		setup      func(q *fakeMediaQuerier)
		wantErr    error
		wantAnyErr bool
		assert     func(t *testing.T, root string, q *fakeMediaQuerier, got Content)
	}{
		{
			name: "success",
			raw:  []byte("hello world"),
			file: "photo.png",
			setup: func(q *fakeMediaQuerier) {
				q.createMediaContentFn = func(_ context.Context, storedPath pgtype.Text) (Content, error) {
					return Content{ID: uuid.New(), Type: "MEDIA", Content: storedPath}, nil
				}
			},
			assert: func(t *testing.T, _ string, q *fakeMediaQuerier, got Content) {
				t.Helper()
				if len(q.createMediaContentArgs) != 1 {
					t.Fatalf("expected exactly one db call, got %d", len(q.createMediaContentArgs))
				}
				storedPath := q.createMediaContentArgs[0]
				if filepath.Ext(storedPath) != ".png" {
					t.Fatalf("expected .png extension, got %s", filepath.Ext(storedPath))
				}
				fileContent, err := os.ReadFile(storedPath)
				if err != nil {
					t.Fatalf("read stored file: %v", err)
				}
				if string(fileContent) != "hello world" {
					t.Fatalf("stored file content mismatch: got %q", string(fileContent))
				}
				if got.Content.String != storedPath {
					t.Fatalf("expected content path %q, got %q", storedPath, got.Content.String)
				}
			},
		},
		{
			name:       "db error cleans up file",
			raw:        []byte("payload"),
			file:       "x.txt",
			wantAnyErr: true,
			setup: func(q *fakeMediaQuerier) {
				q.createMediaContentFn = func(_ context.Context, _ pgtype.Text) (Content, error) {
					return Content{}, errors.New("db boom")
				}
			},
			assert: func(t *testing.T, root string, _ *fakeMediaQuerier, _ Content) {
				t.Helper()
				entries, err := os.ReadDir(root)
				if err != nil {
					t.Fatalf("read media root: %v", err)
				}
				if len(entries) != 0 {
					t.Fatalf("expected cleanup on DB error, found %d files", len(entries))
				}
			},
		},
		{
			name:    "empty payload",
			raw:     nil,
			file:    "x.txt",
			wantErr: errEmptyMediaContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			q := &fakeMediaQuerier{}
			if tt.setup != nil {
				tt.setup(q)
			}
			svc := NewService(q, zap.NewNop())

			got, err := svc.CreateMediaContent(context.Background(), tt.raw, tt.file, root)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
			} else if tt.wantAnyErr {
				if err == nil {
					t.Fatalf("expected an error, got nil")
				}
			} else if err != nil {
				t.Fatalf("CreateMediaContent error: %v", err)
			}

			if tt.assert != nil {
				tt.assert(t, root, q, got)
			}
		})
	}
}

func TestCreateTextContent(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		setup     func(q *fakeMediaQuerier)
		wantErr   error
		wantSaved string
	}{
		{
			name:    "empty text",
			input:   "   ",
			wantErr: errEmptyTextContent,
		},
		{
			name:  "success trims before save",
			input: "  hello  ",
			setup: func(q *fakeMediaQuerier) {
				q.createTextContentFn = func(_ context.Context, content pgtype.Text) (Content, error) {
					return Content{ID: uuid.New(), Type: "TEXT", Content: content}, nil
				}
			},
			wantSaved: "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &fakeMediaQuerier{}
			if tt.setup != nil {
				tt.setup(q)
			}
			svc := NewService(q, zap.NewNop())

			_, err := svc.CreateTextContent(context.Background(), tt.input)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateTextContent error: %v", err)
			}
			if len(q.createTextContentArgs) != 1 || q.createTextContentArgs[0] != tt.wantSaved {
				t.Fatalf("expected saved text %q, got %v", tt.wantSaved, q.createTextContentArgs)
			}
		})
	}
}

func TestListTextContents(t *testing.T) {
	tests := []struct {
		name           string
		page           int32
		pageSize       int32
		totalItems     int64
		wantLimit      int32
		wantOffset     int32
		wantTotalPages int32
		wantHasNext    bool
	}{
		{
			name:           "defaults pagination",
			page:           0,
			pageSize:       0,
			totalItems:     35,
			wantLimit:      defaultPageSize,
			wantOffset:     0,
			wantTotalPages: 2,
			wantHasNext:    true,
		},
		{
			name:           "caps max page size",
			page:           2,
			pageSize:       999,
			totalItems:     350,
			wantLimit:      maxPageSize,
			wantOffset:     maxPageSize,
			wantTotalPages: 4,
			wantHasNext:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &fakeMediaQuerier{
				countTextContentsFn: func(context.Context) (int64, error) {
					return tt.totalItems, nil
				},
				listTextContentsFn: func(_ context.Context, arg ListTextContentsParams) ([]Content, error) {
					if arg.Limit != tt.wantLimit {
						t.Fatalf("expected limit %d, got %d", tt.wantLimit, arg.Limit)
					}
					if arg.Offset != tt.wantOffset {
						t.Fatalf("expected offset %d, got %d", tt.wantOffset, arg.Offset)
					}
					return []Content{}, nil
				},
			}
			svc := NewService(q, zap.NewNop())

			page, err := svc.ListTextContents(context.Background(), tt.page, tt.pageSize)
			if err != nil {
				t.Fatalf("ListTextContents error: %v", err)
			}
			if page.TotalPages != tt.wantTotalPages {
				t.Fatalf("expected totalPages %d, got %d", tt.wantTotalPages, page.TotalPages)
			}
			if page.HasNextPage != tt.wantHasNext {
				t.Fatalf("expected hasNextPage %t, got %t", tt.wantHasNext, page.HasNextPage)
			}
		})
	}
}
