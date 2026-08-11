package page

import (
	"context"
	"errors"
	"testing"

	"sciedu-backend/internal/course"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func newPageService(querier *fakeQuerier, courses CourseLookup) *PageService {
	blocks := newBlockService(querier, querier, &fakeContentLookup{}, &fakeQuestionLookup{})
	return NewPageService(querier, blocks, courses, zap.NewNop())
}

func TestPageServiceCreate_TableDriven(t *testing.T) {
	courseID := uuid.New()

	tests := []struct {
		name         string
		courses      *fakeCourseLookup
		displayOrder int32
		wantErr      bool
		wantNotFound bool
		wantCalls    []string
	}{
		{
			name:      "creates page when course exists",
			courses:   &fakeCourseLookup{},
			wantCalls: []string{"CreatePage"},
		},
		{
			name: "returns not found when course is missing",
			courses: &fakeCourseLookup{
				getCourseByIDFn: func(context.Context, uuid.UUID) (course.Course, error) {
					return course.Course{}, noRowsErr()
				},
			},
			wantErr:      true,
			wantNotFound: true,
			wantCalls:    nil,
		},
		{
			// Out of range for the CHECK constraint, and past the point where the
			// reorder offset would still fit in an INT column.
			name:         "rejects a display order above the allowed range",
			courses:      &fakeCourseLookup{},
			displayOrder: maxDisplayOrder + 1,
			wantErr:      true,
			wantCalls:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			querier := &fakeQuerier{}
			svc := newPageService(querier, tt.courses)

			detail, err := svc.Create(context.Background(), courseID, PageRequest{Title: "Intro", DisplayOrder: tt.displayOrder})

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				if tt.wantNotFound && !isNotFoundError(err) {
					t.Fatalf("expected a not found error, got %v", err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if detail.Title != "Intro" {
					t.Fatalf("expected title to be preserved, got %q", detail.Title)
				}
				if detail.Blocks == nil || len(detail.Blocks) != 0 {
					t.Fatalf("expected a new page to carry an empty block slice, got %#v", detail.Blocks)
				}
			}

			if len(querier.calls) != len(tt.wantCalls) {
				t.Fatalf("expected calls %v, got %v", tt.wantCalls, querier.calls)
			}
			for i, want := range tt.wantCalls {
				if querier.calls[i] != want {
					t.Fatalf("expected calls %v, got %v", tt.wantCalls, querier.calls)
				}
			}
		})
	}
}

func TestPageServiceReorder_TableDriven(t *testing.T) {
	courseID := uuid.New()
	first, second, third := uuid.New(), uuid.New(), uuid.New()

	existing := []Page{
		{ID: first, CourseID: courseID, DisplayOrder: 0},
		{ID: second, CourseID: courseID, DisplayOrder: 1},
		{ID: third, CourseID: courseID, DisplayOrder: 2},
	}

	tests := []struct {
		name      string
		requested []uuid.UUID
		setOrders func(ctx context.Context, arg SetPageOrdersParams) ([]Page, error)
		wantErr   error
		wantCalls []string
		wantOrder []uuid.UUID
	}{
		{
			name:      "reorders every page",
			requested: []uuid.UUID{third, first, second},
			setOrders: func(_ context.Context, arg SetPageOrdersParams) ([]Page, error) {
				// Returned in arbitrary order on purpose: the service must sort.
				return []Page{
					{ID: arg.PageIds[1], DisplayOrder: 1},
					{ID: arg.PageIds[2], DisplayOrder: 2},
					{ID: arg.PageIds[0], DisplayOrder: 0},
				}, nil
			},
			wantCalls: []string{"OffsetPageOrders", "SetPageOrders"},
			wantOrder: []uuid.UUID{third, first, second},
		},
		{
			name:      "rejects a short list without writing",
			requested: []uuid.UUID{first, second},
			wantErr:   errInvalidPagePayload,
			wantCalls: nil,
		},
		{
			name:      "rejects an unknown id without writing",
			requested: []uuid.UUID{first, second, uuid.New()},
			wantErr:   errInvalidPagePayload,
			wantCalls: nil,
		},
		{
			name:      "rejects duplicates without writing",
			requested: []uuid.UUID{first, second, second},
			wantErr:   errInvalidPagePayload,
			wantCalls: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			querier := &fakeQuerier{
				listPagesByCourseFn: func(context.Context, uuid.UUID) ([]Page, error) {
					return existing, nil
				},
				setPageOrdersFn: tt.setOrders,
			}
			svc := newPageService(querier, &fakeCourseLookup{})

			pages, err := svc.Reorder(context.Background(), courseID, tt.requested)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(pages) != len(tt.wantOrder) {
					t.Fatalf("expected %d pages, got %d", len(tt.wantOrder), len(pages))
				}
				for i, want := range tt.wantOrder {
					if pages[i].ID != want {
						t.Fatalf("expected pages sorted by display order %v, got %v at index %d", tt.wantOrder, pages[i].ID, i)
					}
				}
			}

			if len(querier.calls) != len(tt.wantCalls) {
				t.Fatalf("expected calls %v, got %v", tt.wantCalls, querier.calls)
			}
			for i, want := range tt.wantCalls {
				if querier.calls[i] != want {
					t.Fatalf("expected calls %v, got %v", tt.wantCalls, querier.calls)
				}
			}
		})
	}
}

// A store without transaction support is now a compile error (PageStore embeds
// Transactor), so there is no runtime case left to test for it.

func TestPageServiceReorderPropagatesWriteFailure(t *testing.T) {
	courseID := uuid.New()
	pageID := uuid.New()
	writeErr := errors.New("write-back failed")

	querier := &fakeQuerier{
		listPagesByCourseFn: func(context.Context, uuid.UUID) ([]Page, error) {
			return []Page{{ID: pageID, CourseID: courseID}}, nil
		},
		setPageOrdersFn: func(context.Context, SetPageOrdersParams) ([]Page, error) {
			return nil, writeErr
		},
	}
	svc := newPageService(querier, &fakeCourseLookup{})

	// The offset step has already run here, so the caller must see a failure
	// rather than a partial success; the rollback itself is Store.WithinTx's job
	// and is covered by store_integration_test.go.
	if _, err := svc.Reorder(context.Background(), courseID, []uuid.UUID{pageID}); err == nil {
		t.Fatal("expected the write-back failure to propagate, got nil")
	}

	wantCalls := []string{"OffsetPageOrders", "SetPageOrders"}
	if len(querier.calls) != len(wantCalls) {
		t.Fatalf("expected calls %v, got %v", wantCalls, querier.calls)
	}
}

// Delete no longer pre-checks existence; it reads the row count back from the
// DELETE itself, so a missing page is one statement rather than two.
func TestPageServiceDeleteReturnsNotFound(t *testing.T) {
	querier := &fakeQuerier{
		deletePageFn: func(context.Context, uuid.UUID) (int64, error) {
			return 0, nil
		},
	}
	svc := newPageService(querier, &fakeCourseLookup{})

	err := svc.Delete(context.Background(), uuid.New())

	if !isNotFoundError(err) {
		t.Fatalf("expected a not found error, got %v", err)
	}
	wantCalls := []string{"DeletePage"}
	if len(querier.calls) != len(wantCalls) || querier.calls[0] != wantCalls[0] {
		t.Fatalf("expected calls %v, got %v", wantCalls, querier.calls)
	}
}
