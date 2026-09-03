package page

import (
	"context"
	"errors"
	"testing"

	"sciedu-backend/internal/course"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"go.uber.org/zap"
)

func newPageService(querier *fakeQuerier, courses CourseAccess) *PageService {
	blocks := newBlockService(querier, querier, &fakeContentLookup{}, &fakeQuestionLookup{})
	return NewPageService(querier, blocks, courses, zap.NewNop())
}

func TestPageServiceReadAuthorization(t *testing.T) {
	actorID, courseID, pageID := uuid.New(), uuid.New(), uuid.New()

	t.Run("list authorizes the requested course before querying pages", func(t *testing.T) {
		queried := false
		querier := &fakeQuerier{
			listPagesByCourseFn: func(_ context.Context, gotCourseID uuid.UUID) ([]Page, error) {
				queried = true
				if gotCourseID != courseID {
					t.Fatalf("expected course %s, got %s", courseID, gotCourseID)
				}
				return []Page{{ID: pageID, CourseID: courseID}}, nil
			},
		}
		courses := &fakeCourseLookup{}

		pages, err := newPageService(querier, courses).ListByCourseForActor(t.Context(), actorID, courseID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !queried || len(pages) != 1 {
			t.Fatalf("expected one queried page, got %#v", pages)
		}
		assertCourseAccessCall(t, courses, actorID, courseID)
	})

	t.Run("list denial prevents the page query", func(t *testing.T) {
		queried := false
		querier := &fakeQuerier{
			listPagesByCourseFn: func(context.Context, uuid.UUID) ([]Page, error) {
				queried = true
				return nil, nil
			},
		}
		courses := &fakeCourseLookup{
			byIDForActorFn: func(context.Context, uuid.UUID, uuid.UUID) (course.Record, error) {
				return course.Record{}, handlerutil.ErrForbidden
			},
		}

		_, err := newPageService(querier, courses).ListByCourseForActor(t.Context(), actorID, courseID)
		if !errors.Is(err, handlerutil.ErrForbidden) {
			t.Fatalf("expected forbidden, got %v", err)
		}
		if queried {
			t.Fatal("page query ran after access was denied")
		}
	})

	t.Run("page detail authorizes the page course before loading blocks", func(t *testing.T) {
		blocksQueried := false
		querier := &fakeQuerier{
			getPageByIDFn: func(context.Context, uuid.UUID) (Page, error) {
				return Page{ID: pageID, CourseID: courseID}, nil
			},
			listBlocksByPageFn: func(context.Context, uuid.UUID) ([]PageBlock, error) {
				blocksQueried = true
				return nil, nil
			},
		}
		courses := &fakeCourseLookup{}

		_, err := newPageService(querier, courses).GetForActor(t.Context(), actorID, pageID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !blocksQueried {
			t.Fatal("expected blocks to be loaded after authorization")
		}
		assertCourseAccessCall(t, courses, actorID, courseID)
	})

	t.Run("page detail denial prevents loading blocks", func(t *testing.T) {
		blocksQueried := false
		querier := &fakeQuerier{
			getPageByIDFn: func(context.Context, uuid.UUID) (Page, error) {
				return Page{ID: pageID, CourseID: courseID}, nil
			},
			listBlocksByPageFn: func(context.Context, uuid.UUID) ([]PageBlock, error) {
				blocksQueried = true
				return nil, nil
			},
		}
		courses := &fakeCourseLookup{
			byIDForActorFn: func(context.Context, uuid.UUID, uuid.UUID) (course.Record, error) {
				return course.Record{}, handlerutil.ErrForbidden
			},
		}

		_, err := newPageService(querier, courses).GetForActor(t.Context(), actorID, pageID)
		if !errors.Is(err, handlerutil.ErrForbidden) {
			t.Fatalf("expected forbidden, got %v", err)
		}
		if blocksQueried {
			t.Fatal("blocks were loaded after access was denied")
		}
	})

	t.Run("block list authorizes using the owning page course", func(t *testing.T) {
		querier := &fakeQuerier{
			getPageByIDFn: func(context.Context, uuid.UUID) (Page, error) {
				return Page{ID: pageID, CourseID: courseID}, nil
			},
			listBlocksByPageFn: func(context.Context, uuid.UUID) ([]PageBlock, error) {
				return []PageBlock{}, nil
			},
		}
		courses := &fakeCourseLookup{}

		_, err := newPageService(querier, courses).ListBlocksForActor(t.Context(), actorID, pageID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertCourseAccessCall(t, courses, actorID, courseID)
	})
}

func assertCourseAccessCall(t *testing.T, courses *fakeCourseLookup, actorID, courseID uuid.UUID) {
	t.Helper()
	if courses.byIDForActorCalls != 1 || courses.lastActorID != actorID || courses.lastAccessCourseID != courseID {
		t.Fatalf(
			"expected one access check for actor %s and course %s; got calls=%d actor=%s course=%s",
			actorID, courseID, courses.byIDForActorCalls, courses.lastActorID, courses.lastAccessCourseID,
		)
	}
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
				byIDFn: func(_ context.Context, id uuid.UUID) (course.Record, error) {
					return course.Record{}, handlerutil.NewNotFoundError("courses", "id", id.String(), "")
				},
			},
			wantErr:      true,
			wantNotFound: true,
			wantCalls:    nil,
		},
		{
			name:         "accepts the maximum int32 display order",
			courses:      &fakeCourseLookup{},
			displayOrder: 2147483647,
			wantCalls:    []string{"CreatePage"},
		},
		{
			name:         "rejects a negative display order",
			courses:      &fakeCourseLookup{},
			displayOrder: -1,
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
				if detail.DisplayOrder != tt.displayOrder {
					t.Fatalf("expected displayOrder %d, got %d", tt.displayOrder, detail.DisplayOrder)
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
			wantCalls: []string{"LockCourseForPageReorder", "DeferOrderConstraints", "SetPageOrders"},
			wantOrder: []uuid.UUID{third, first, second},
		},
		{
			name:      "rejects a short list without writing",
			requested: []uuid.UUID{first, second},
			wantErr:   errInvalidPagePayload,
			wantCalls: []string{"LockCourseForPageReorder"},
		},
		{
			name:      "rejects an unknown id without writing",
			requested: []uuid.UUID{first, second, uuid.New()},
			wantErr:   errInvalidPagePayload,
			wantCalls: []string{"LockCourseForPageReorder"},
		},
		{
			name:      "rejects duplicates without writing",
			requested: []uuid.UUID{first, second, second},
			wantErr:   errInvalidPagePayload,
			wantCalls: []string{"LockCourseForPageReorder"},
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

	// The constraint has already been deferred, so the caller must see a failure
	// rather than a partial success. Rollback is covered by the integration test.
	if _, err := svc.Reorder(context.Background(), courseID, []uuid.UUID{pageID}); err == nil {
		t.Fatal("expected the write-back failure to propagate, got nil")
	}

	wantCalls := []string{"LockCourseForPageReorder", "DeferOrderConstraints", "SetPageOrders"}
	if len(querier.calls) != len(wantCalls) {
		t.Fatalf("expected calls %v, got %v", wantCalls, querier.calls)
	}
}

func TestWrapTransactionError(t *testing.T) {
	t.Run("maps a deferred unique violation", func(t *testing.T) {
		err := wrapTransactionError(&pgconn.PgError{Code: databaseutil.PGErrUniqueViolation}, zap.NewNop(), "commit reorder")
		if !errors.Is(err, databaseutil.ErrUniqueViolation) {
			t.Fatalf("expected unique violation mapping, got %v", err)
		}
	})

	t.Run("preserves validation errors", func(t *testing.T) {
		if err := wrapTransactionError(errInvalidPagePayload, zap.NewNop(), "commit reorder"); !errors.Is(err, errInvalidPagePayload) {
			t.Fatalf("expected validation error to pass through, got %v", err)
		}
	})
}

// Delete reads the row count back from the DELETE itself rather than pre-checking.
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
