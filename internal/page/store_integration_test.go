//go:build integration

package page

import (
	"context"
	"errors"
	"os"
	"testing"

	"sciedu-backend/internal/course"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

var errInjectedWriteFailure = errors.New("injected write-back failure")

// failingTransactor runs the real Store transaction but makes the write-back
// step fail, so the test can observe what the database is left holding.
type failingTransactor struct {
	*Store
}

func (f failingTransactor) WithinTx(ctx context.Context, fn func(PageQuerier, BlockQuerier) error) error {
	return f.Store.WithinTx(ctx, func(pageQuerier PageQuerier, blockQuerier BlockQuerier) error {
		return fn(failingPageQuerier{PageQuerier: pageQuerier}, blockQuerier)
	})
}

type failingPageQuerier struct {
	PageQuerier
}

func (failingPageQuerier) SetPageOrders(context.Context, SetPageOrdersParams) ([]Page, error) {
	return nil, errInjectedWriteFailure
}

// blockServiceForPages skips the content and question services: these tests only
// exercise page reordering, which never reaches a block lookup.
func blockServiceForPages(store *Store) *BlockService {
	return NewBlockService(store, store, nil, nil, zap.NewNop())
}

func newIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("PAGE_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PAGE_INTEGRATION_DATABASE_URL is not set")
	}

	pool, err := pgxpool.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(t.Context()); err != nil {
		t.Fatalf("failed to reach database: %v", err)
	}
	return pool
}

func seedCourseWithPages(t *testing.T, pool *pgxpool.Pool, titles ...string) (uuid.UUID, []uuid.UUID) {
	t.Helper()
	ctx := t.Context()

	var courseID uuid.UUID
	code := "page-integration-" + uuid.NewString()
	err := pool.QueryRow(ctx,
		"INSERT INTO courses (code, title) VALUES ($1, $2) RETURNING id",
		code, "Page integration course").Scan(&courseID)
	if err != nil {
		t.Fatalf("failed to seed course: %v", err)
	}

	t.Cleanup(func() {
		//nolint:errcheck // best-effort cleanup
		pool.Exec(context.Background(), "DELETE FROM pages WHERE course_id = $1", courseID)
		//nolint:errcheck // best-effort cleanup
		pool.Exec(context.Background(), "DELETE FROM courses WHERE id = $1", courseID)
	})

	pageIDs := make([]uuid.UUID, 0, len(titles))
	for i, title := range titles {
		var pageID uuid.UUID
		err := pool.QueryRow(ctx,
			"INSERT INTO pages (course_id, title, display_order) VALUES ($1, $2, $3) RETURNING id",
			courseID, title, int32(i)).Scan(&pageID)
		if err != nil {
			t.Fatalf("failed to seed page %q: %v", title, err)
		}
		pageIDs = append(pageIDs, pageID)
	}

	return courseID, pageIDs
}

func readDisplayOrders(t *testing.T, pool *pgxpool.Pool, courseID uuid.UUID) map[uuid.UUID]int32 {
	t.Helper()

	rows, err := pool.Query(t.Context(), "SELECT id, display_order FROM pages WHERE course_id = $1", courseID)
	if err != nil {
		t.Fatalf("failed to read display orders: %v", err)
	}
	defer rows.Close()

	orders := make(map[uuid.UUID]int32)
	for rows.Next() {
		var (
			id    uuid.UUID
			order int32
		)
		if err := rows.Scan(&id, &order); err != nil {
			t.Fatalf("failed to scan display order: %v", err)
		}
		orders[id] = order
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("failed to iterate display orders: %v", err)
	}
	return orders
}

// The deferred constraint and write-back must share one transaction: a failed
// write must restore both the data and the constraint mode at rollback.
func TestStoreReorderRollsBackDeferredWrite(t *testing.T) {
	pool := newIntegrationPool(t)
	courseID, pageIDs := seedCourseWithPages(t, pool, "First", "Second", "Third")

	before := readDisplayOrders(t, pool, courseID)

	store := NewStore(pool)
	courses := course.NewService(course.NewStore(pool), nil, nil, zap.NewNop())
	svc := NewPageService(failingTransactor{Store: store}, blockServiceForPages(store), courses, zap.NewNop())

	reversed := []uuid.UUID{pageIDs[2], pageIDs[1], pageIDs[0]}
	_, err := svc.Reorder(t.Context(), courseID, reversed)

	// WrapDBError wraps unrecognised errors in summer's InternalServerError, which
	// has no Unwrap, so errors.Is cannot see through it. Reach it through Source.
	var internal databaseutil.InternalServerError
	if !errors.As(err, &internal) || !errors.Is(internal.Source, errInjectedWriteFailure) {
		t.Fatalf("expected the injected write failure, got %v", err)
	}

	after := readDisplayOrders(t, pool, courseID)
	for id, want := range before {
		got, ok := after[id]
		if !ok {
			t.Fatalf("page %s disappeared after the failed reorder", id)
		}
		if got != want {
			t.Fatalf("page %s left at display_order %d, expected the original %d", id, got, want)
		}
	}
}

// TestStoreReorderCommitsNewOrder covers the deferred-constraint success path.
func TestStoreReorderCommitsNewOrder(t *testing.T) {
	pool := newIntegrationPool(t)
	courseID, pageIDs := seedCourseWithPages(t, pool, "First", "Second", "Third")

	store := NewStore(pool)
	svc := NewPageService(store, blockServiceForPages(store), course.NewService(course.NewStore(pool), nil, nil, zap.NewNop()), zap.NewNop())

	reversed := []uuid.UUID{pageIDs[2], pageIDs[1], pageIDs[0]}
	reordered, err := svc.Reorder(t.Context(), courseID, reversed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i, want := range reversed {
		if reordered[i].ID != want {
			t.Fatalf("expected returned order %v, got %s at index %d", reversed, reordered[i].ID, i)
		}
	}

	after := readDisplayOrders(t, pool, courseID)
	for i, id := range reversed {
		if after[id] != int32(i) {
			t.Fatalf("expected page %s at display_order %d, got %d", id, i, after[id])
		}
	}
}
