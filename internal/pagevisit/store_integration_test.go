//go:build integration

package pagevisit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pageVisitFixture struct {
	studentID uuid.UUID
	otherID   uuid.UUID
	courseID  uuid.UUID
	pageID    uuid.UUID
}

func newPageVisitIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("PAGE_VISIT_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PAGE_VISIT_INTEGRATION_DATABASE_URL is not set")
	}

	pool, err := pgxpool.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("create integration pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(t.Context()); err != nil {
		t.Fatalf("reach integration database: %v", err)
	}
	return pool
}

func seedPageVisitFixture(t *testing.T, pool *pgxpool.Pool) pageVisitFixture {
	t.Helper()

	fixture := pageVisitFixture{
		studentID: uuid.New(),
		otherID:   uuid.New(),
	}
	suffix := uuid.NewString()
	ctx := t.Context()

	for i, studentID := range []uuid.UUID{fixture.studentID, fixture.otherID} {
		_, err := pool.Exec(ctx,
			"INSERT INTO users (id, email, name, roles) VALUES ($1, $2, $3, ARRAY['STUDENT']::user_role[])",
			studentID, fmt.Sprintf("page-visit-%d-%s@example.invalid", i, suffix), "PageVisit Student")
		if err != nil {
			t.Fatalf("seed student: %v", err)
		}
	}

	err := pool.QueryRow(ctx,
		"INSERT INTO courses (code, title) VALUES ($1, $2) RETURNING id",
		"page-visit-"+suffix, "PageVisit Course").Scan(&fixture.courseID)
	if err != nil {
		t.Fatalf("seed course: %v", err)
	}
	err = pool.QueryRow(ctx,
		"INSERT INTO pages (course_id, title, display_order) VALUES ($1, $2, 0) RETURNING id",
		fixture.courseID, "PageVisit Page").Scan(&fixture.pageID)
	if err != nil {
		t.Fatalf("seed page: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		//nolint:errcheck // best-effort test cleanup
		pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = ANY($1::uuid[])", []uuid.UUID{fixture.studentID, fixture.otherID})
		//nolint:errcheck // best-effort test cleanup
		pool.Exec(cleanupCtx, "DELETE FROM pages WHERE id = $1", fixture.pageID)
		//nolint:errcheck // best-effort test cleanup
		pool.Exec(cleanupCtx, "DELETE FROM courses WHERE id = $1", fixture.courseID)
	})

	return fixture
}

func createVisit(t *testing.T, queries *Queries, fixture pageVisitFixture, studentID, sessionID, key uuid.UUID, enteredAt time.Time) PageVisit {
	t.Helper()

	visit, err := queries.CreatePageVisit(t.Context(), CreatePageVisitParams{
		StudentID:       studentID,
		CourseID:        fixture.courseID,
		PageID:          fixture.pageID,
		ClientSessionID: sessionID,
		IdempotencyKey:  key,
		EnteredAt:       pgtype.Timestamptz{Time: enteredAt, Valid: true},
	})
	if err != nil {
		t.Fatalf("create PageVisit: %v", err)
	}
	return visit
}

func requirePostgresConstraint(t *testing.T, err error, code, constraint string) {
	t.Helper()

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("expected PostgreSQL error, got %v", err)
	}
	if pgErr.Code != code || pgErr.ConstraintName != constraint {
		t.Fatalf("expected PostgreSQL %s on %s, got %s on %s", code, constraint, pgErr.Code, pgErr.ConstraintName)
	}
}

func TestPageVisitDatabaseInvariants(t *testing.T) {
	pool := newPageVisitIntegrationPool(t)
	queries := New(pool)
	enteredAt := time.Now().UTC().Truncate(time.Microsecond)

	t.Run("idempotency key is unique per student", func(t *testing.T) {
		fixture := seedPageVisitFixture(t, pool)
		key := uuid.New()
		createVisit(t, queries, fixture, fixture.studentID, uuid.New(), key, enteredAt)

		_, err := queries.CreatePageVisit(t.Context(), CreatePageVisitParams{
			StudentID:       fixture.studentID,
			CourseID:        fixture.courseID,
			PageID:          fixture.pageID,
			ClientSessionID: uuid.New(),
			IdempotencyKey:  key,
			EnteredAt:       pgtype.Timestamptz{Time: enteredAt, Valid: true},
		})
		requirePostgresConstraint(t, err, "23505", "page_visits_student_idempotency_key_unique")

		createVisit(t, queries, fixture, fixture.otherID, uuid.New(), key, enteredAt)
	})

	t.Run("only one open visit exists per student session", func(t *testing.T) {
		fixture := seedPageVisitFixture(t, pool)
		sessionID := uuid.New()
		first := createVisit(t, queries, fixture, fixture.studentID, sessionID, uuid.New(), enteredAt)

		_, err := queries.CreatePageVisit(t.Context(), CreatePageVisitParams{
			StudentID:       fixture.studentID,
			CourseID:        fixture.courseID,
			PageID:          fixture.pageID,
			ClientSessionID: sessionID,
			IdempotencyKey:  uuid.New(),
			EnteredAt:       pgtype.Timestamptz{Time: enteredAt.Add(time.Second), Valid: true},
		})
		requirePostgresConstraint(t, err, "23505", "page_visits_one_open_per_student_session_idx")

		_, err = queries.CloseOpenPageVisit(t.Context(), CloseOpenPageVisitParams{
			ID:        first.ID,
			StudentID: fixture.studentID,
			LeftAt:    pgtype.Timestamptz{Time: enteredAt.Add(time.Second), Valid: true},
		})
		if err != nil {
			t.Fatalf("close first PageVisit: %v", err)
		}
		createVisit(t, queries, fixture, fixture.studentID, sessionID, uuid.New(), enteredAt.Add(time.Second))
	})

	t.Run("left time cannot precede entered time", func(t *testing.T) {
		fixture := seedPageVisitFixture(t, pool)
		_, err := pool.Exec(t.Context(), `
			INSERT INTO page_visits (
				student_id, course_id, page_id, client_session_id, idempotency_key, entered_at, left_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			fixture.studentID, fixture.courseID, fixture.pageID, uuid.New(), uuid.New(), enteredAt, enteredAt.Add(-time.Second))
		requirePostgresConstraint(t, err, "23514", "page_visits_left_at_not_before_entered_at")
	})
}

func TestPageVisitForeignKeysCascade(t *testing.T) {
	pool := newPageVisitIntegrationPool(t)
	fixture := seedPageVisitFixture(t, pool)
	visit := createVisit(t, New(pool), fixture, fixture.studentID, uuid.New(), uuid.New(), time.Now().UTC())

	if _, err := pool.Exec(t.Context(), "DELETE FROM pages WHERE id = $1", fixture.pageID); err != nil {
		t.Fatalf("delete Page: %v", err)
	}

	var count int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM page_visits WHERE id = $1", visit.ID).Scan(&count); err != nil {
		t.Fatalf("count PageVisits after Page deletion: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected Page deletion to cascade to PageVisit, found %d rows", count)
	}
}

func TestStoreWithinTxRollsBackPageVisit(t *testing.T) {
	pool := newPageVisitIntegrationPool(t)
	fixture := seedPageVisitFixture(t, pool)
	store := NewStore(pool)
	key := uuid.New()
	errRollback := errors.New("force rollback")

	err := store.WithinTx(t.Context(), func(queries TransactionRepository) error {
		_, err := queries.CreatePageVisit(t.Context(), CreatePageVisitParams{
			StudentID:       fixture.studentID,
			CourseID:        fixture.courseID,
			PageID:          fixture.pageID,
			ClientSessionID: uuid.New(),
			IdempotencyKey:  key,
			EnteredAt:       pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		})
		if err != nil {
			return err
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("expected rollback error, got %v", err)
	}

	var count int
	if err := pool.QueryRow(t.Context(),
		"SELECT count(*) FROM page_visits WHERE student_id = $1 AND idempotency_key = $2",
		fixture.studentID, key).Scan(&count); err != nil {
		t.Fatalf("count rolled-back PageVisit: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected transaction rollback to remove PageVisit, found %d rows", count)
	}
}

func TestListAndCountPageVisitsApplyFiltersAndOrdering(t *testing.T) {
	pool := newPageVisitIntegrationPool(t)
	fixture := seedPageVisitFixture(t, pool)
	queries := New(pool)
	base := time.Now().UTC().Truncate(time.Microsecond)

	oldVisit := createVisit(t, queries, fixture, fixture.studentID, uuid.New(), uuid.New(), base)
	_, err := queries.LeavePageVisitIfOpen(t.Context(), LeavePageVisitIfOpenParams{
		ID:        oldVisit.ID,
		StudentID: fixture.studentID,
		LeftAt:    pgtype.Timestamptz{Time: base.Add(time.Minute), Valid: true},
	})
	if err != nil {
		t.Fatalf("close older PageVisit: %v", err)
	}
	newVisit := createVisit(t, queries, fixture, fixture.studentID, uuid.New(), uuid.New(), base.Add(2*time.Minute))

	params := ListPageVisitsParams{
		StudentID: pgtype.UUID{Bytes: fixture.studentID, Valid: true},
		Status:    pgtype.Text{String: "OPEN", Valid: true},
		Offset:    0,
		Limit:     20,
	}
	visits, err := queries.ListPageVisits(t.Context(), params)
	if err != nil {
		t.Fatalf("list filtered PageVisits: %v", err)
	}
	if len(visits) != 1 || visits[0].ID != newVisit.ID {
		t.Fatalf("expected only newest open visit %s, got %+v", newVisit.ID, visits)
	}

	count, err := queries.CountPageVisits(t.Context(), CountPageVisitsParams{
		StudentID: params.StudentID,
		Status:    params.Status,
	})
	if err != nil {
		t.Fatalf("count filtered PageVisits: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one open PageVisit, got %d", count)
	}

	all, err := queries.ListPageVisits(t.Context(), ListPageVisitsParams{Offset: 0, Limit: 20})
	if err != nil {
		t.Fatalf("list all PageVisits: %v", err)
	}
	var oldIndex, newIndex = -1, -1
	for i, visit := range all {
		switch visit.ID {
		case oldVisit.ID:
			oldIndex = i
		case newVisit.ID:
			newIndex = i
		}
	}
	if oldIndex < 0 || newIndex < 0 || newIndex >= oldIndex {
		t.Fatalf("expected entered_at descending order, old index %d and new index %d", oldIndex, newIndex)
	}
}

func TestServiceEnterRejectsMissingOrMismatchedTarget(t *testing.T) {
	pool := newPageVisitIntegrationPool(t)

	tests := []struct {
		name  string
		input func(pageVisitFixture) EnterInput
	}{
		{
			name: "missing course",
			input: func(fixture pageVisitFixture) EnterInput {
				return EnterInput{StudentID: fixture.studentID, CourseID: uuid.New(), PageID: fixture.pageID, ClientSessionID: uuid.New(), IdempotencyKey: uuid.New()}
			},
		},
		{
			name: "missing page",
			input: func(fixture pageVisitFixture) EnterInput {
				return EnterInput{StudentID: fixture.studentID, CourseID: fixture.courseID, PageID: uuid.New(), ClientSessionID: uuid.New(), IdempotencyKey: uuid.New()}
			},
		},
		{
			name: "page belongs to a different course",
			input: func(fixture pageVisitFixture) EnterInput {
				var otherCourseID uuid.UUID
				err := pool.QueryRow(t.Context(),
					"INSERT INTO courses (code, title) VALUES ($1, $2) RETURNING id",
					"page-visit-mismatch-"+uuid.NewString(), "Other PageVisit Course").Scan(&otherCourseID)
				if err != nil {
					t.Fatalf("seed mismatched course: %v", err)
				}
				t.Cleanup(func() {
					//nolint:errcheck // best-effort test cleanup
					pool.Exec(context.Background(), "DELETE FROM courses WHERE id = $1", otherCourseID)
				})
				return EnterInput{StudentID: fixture.studentID, CourseID: otherCourseID, PageID: fixture.pageID, ClientSessionID: uuid.New(), IdempotencyKey: uuid.New()}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := seedPageVisitFixture(t, pool)
			_, err := NewService(NewStore(pool), time.Now).Enter(t.Context(), tt.input(fixture))
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("expected ErrNotFound, got %v", err)
			}
			requireVisitCount(t, pool, fixture.studentID, 0)
		})
	}
}

func TestServiceConcurrentIdempotentEnter(t *testing.T) {
	pool := newPageVisitIntegrationPool(t)
	fixture := seedPageVisitFixture(t, pool)
	service := NewService(NewStore(pool), time.Now)
	input := EnterInput{
		StudentID:       fixture.studentID,
		CourseID:        fixture.courseID,
		PageID:          fixture.pageID,
		ClientSessionID: uuid.New(),
		IdempotencyKey:  uuid.New(),
	}

	results := runConcurrentEnters(t, service, input, input)
	if results[0].err != nil || results[1].err != nil {
		t.Fatalf("expected both idempotent enters to succeed, got %v and %v", results[0].err, results[1].err)
	}
	if results[0].visit.ID != results[1].visit.ID {
		t.Fatalf("expected the same PageVisit, got %s and %s", results[0].visit.ID, results[1].visit.ID)
	}
	requireVisitCount(t, pool, fixture.studentID, 1)
}

func TestServiceConcurrentIdempotencyConflict(t *testing.T) {
	pool := newPageVisitIntegrationPool(t)
	fixture := seedPageVisitFixture(t, pool)
	service := NewService(NewStore(pool), time.Now)
	key := uuid.New()
	first := EnterInput{
		StudentID:       fixture.studentID,
		CourseID:        fixture.courseID,
		PageID:          fixture.pageID,
		ClientSessionID: uuid.New(),
		IdempotencyKey:  key,
	}
	second := first
	second.ClientSessionID = uuid.New()

	results := runConcurrentEnters(t, service, first, second)
	successes, conflicts := 0, 0
	for _, result := range results {
		switch {
		case result.err == nil:
			successes++
		case errors.Is(result.err, ErrIdempotencyConflict):
			conflicts++
		default:
			t.Fatalf("unexpected enter error: %v", result.err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("expected one success and one conflict, got %d and %d", successes, conflicts)
	}
	requireVisitCount(t, pool, fixture.studentID, 1)
}

func TestServiceConcurrentEntersInSameSessionSerialize(t *testing.T) {
	pool := newPageVisitIntegrationPool(t)
	fixture := seedPageVisitFixture(t, pool)
	service := NewService(NewStore(pool), time.Now)
	sessionID := uuid.New()
	first := EnterInput{
		StudentID:       fixture.studentID,
		CourseID:        fixture.courseID,
		PageID:          fixture.pageID,
		ClientSessionID: sessionID,
		IdempotencyKey:  uuid.New(),
	}
	second := first
	second.IdempotencyKey = uuid.New()

	results := runConcurrentEnters(t, service, first, second)
	for _, result := range results {
		if result.err != nil {
			t.Fatalf("concurrent enter failed: %v", result.err)
		}
	}

	var openCount, closedCount int
	if err := pool.QueryRow(t.Context(), `
		SELECT count(*) FILTER (WHERE left_at IS NULL),
		       count(*) FILTER (WHERE left_at IS NOT NULL)
		FROM page_visits
		WHERE student_id = $1 AND client_session_id = $2`, fixture.studentID, sessionID).Scan(&openCount, &closedCount); err != nil {
		t.Fatalf("count open and closed PageVisits: %v", err)
	}
	if openCount != 1 || closedCount != 1 {
		t.Fatalf("expected one open and one closed PageVisit, got %d open and %d closed", openCount, closedCount)
	}
}

func TestServiceConcurrentEnterAndLeavePreserveFirstClose(t *testing.T) {
	pool := newPageVisitIntegrationPool(t)
	fixture := seedPageVisitFixture(t, pool)
	clock := &lockedClock{next: time.Now().UTC()}
	service := NewService(NewStore(pool), clock.Now)
	sessionID := uuid.New()
	old := createVisit(t, New(pool), fixture, fixture.studentID, sessionID, uuid.New(), clock.next.Add(-time.Hour))
	enter := EnterInput{
		StudentID:       fixture.studentID,
		CourseID:        fixture.courseID,
		PageID:          fixture.pageID,
		ClientSessionID: sessionID,
		IdempotencyKey:  uuid.New(),
	}

	start := make(chan struct{})
	errCh := make(chan error, 2)
	go func() {
		<-start
		_, err := service.Enter(t.Context(), enter)
		errCh <- err
	}()
	go func() {
		<-start
		_, err := service.Leave(t.Context(), LeaveInput{StudentID: fixture.studentID, VisitID: old.ID})
		errCh <- err
	}()
	close(start)
	for range 2 {
		if err := <-errCh; err != nil {
			t.Fatalf("concurrent enter/leave failed: %v", err)
		}
	}

	stored, err := New(pool).PageVisitByIDForStudent(t.Context(), PageVisitByIDForStudentParams{ID: old.ID, StudentID: fixture.studentID})
	if err != nil {
		t.Fatalf("read old PageVisit: %v", err)
	}
	if !stored.LeftAt.Valid {
		t.Fatal("expected old PageVisit to be closed")
	}
	if calls := clock.Calls(); calls < 1 || calls > 2 {
		t.Fatalf("expected one timestamp per operation that actually closed or entered, got %d", calls)
	}
}

func TestServiceConcurrentDoubleLeavePreservesTimestamp(t *testing.T) {
	pool := newPageVisitIntegrationPool(t)
	fixture := seedPageVisitFixture(t, pool)
	clock := &lockedClock{next: time.Now().UTC()}
	service := NewService(NewStore(pool), clock.Now)
	visit := createVisit(t, New(pool), fixture, fixture.studentID, uuid.New(), uuid.New(), clock.next.Add(-time.Hour))

	start := make(chan struct{})
	results := make(chan PageVisit, 2)
	errorsCh := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			closed, err := service.Leave(t.Context(), LeaveInput{StudentID: fixture.studentID, VisitID: visit.ID})
			results <- closed
			errorsCh <- err
		}()
	}
	close(start)
	first, second := <-results, <-results
	for range 2 {
		if err := <-errorsCh; err != nil {
			t.Fatalf("concurrent leave failed: %v", err)
		}
	}
	if !first.LeftAt.Valid || !second.LeftAt.Valid || !first.LeftAt.Time.Equal(second.LeftAt.Time) {
		t.Fatalf("expected both leaves to preserve one timestamp, got %v and %v", first.LeftAt, second.LeftAt)
	}
	if clock.Calls() != 1 {
		t.Fatalf("expected exactly one generated leave timestamp, got %d", clock.Calls())
	}
}

type enterResult struct {
	visit PageVisit
	err   error
}

func runConcurrentEnters(t *testing.T, service *Service, inputs ...EnterInput) []enterResult {
	t.Helper()

	start := make(chan struct{})
	results := make(chan enterResult, len(inputs))
	for _, input := range inputs {
		go func(input EnterInput) {
			<-start
			visit, err := service.Enter(t.Context(), input)
			results <- enterResult{visit: visit, err: err}
		}(input)
	}
	close(start)

	collected := make([]enterResult, 0, len(inputs))
	for range inputs {
		collected = append(collected, <-results)
	}
	return collected
}

func requireVisitCount(t *testing.T, pool *pgxpool.Pool, studentID uuid.UUID, want int) {
	t.Helper()

	var got int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM page_visits WHERE student_id = $1", studentID).Scan(&got); err != nil {
		t.Fatalf("count PageVisits: %v", err)
	}
	if got != want {
		t.Fatalf("expected %d PageVisits, got %d", want, got)
	}
}

type lockedClock struct {
	mu    sync.Mutex
	next  time.Time
	calls int
}

func (c *lockedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	result := c.next.Add(time.Duration(c.calls) * time.Second)
	c.calls++
	return result
}

func (c *lockedClock) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}
