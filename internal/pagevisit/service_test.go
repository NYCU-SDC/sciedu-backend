package pagevisit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var errCreateVisit = errors.New("create PageVisit failed")
var errCheckTarget = errors.New("check PageVisit target failed")

type fakeState struct {
	students map[uuid.UUID]bool
	visits   map[uuid.UUID]PageVisit
}

func (s fakeState) clone() fakeState {
	cloned := fakeState{students: make(map[uuid.UUID]bool, len(s.students)), visits: make(map[uuid.UUID]PageVisit, len(s.visits))}
	for id, exists := range s.students {
		cloned.students[id] = exists
	}
	for id, visit := range s.visits {
		cloned.visits[id] = visit
	}
	return cloned
}

type fakeTransactor struct {
	state        fakeState
	createErr    error
	targetExists bool
	targetErr    error
	listItems    []PageVisit
	listErr      error
	count        int64
	countErr     error
	listArg      ListPageVisitsParams
	countArg     CountPageVisitsParams
}

func newFakeTransactor(studentIDs ...uuid.UUID) *fakeTransactor {
	state := fakeState{students: make(map[uuid.UUID]bool), visits: make(map[uuid.UUID]PageVisit)}
	for _, studentID := range studentIDs {
		state.students[studentID] = true
	}
	return &fakeTransactor{state: state, targetExists: true}
}

func (f *fakeTransactor) WithinTx(ctx context.Context, fn func(TransactionRepository) error) error {
	working := f.state.clone()
	repo := &fakeTransactionRepository{
		state:        &working,
		createErr:    f.createErr,
		targetExists: f.targetExists,
		targetErr:    f.targetErr,
	}
	if err := fn(repo); err != nil {
		return err
	}
	f.state = working
	return nil
}

func (f *fakeTransactor) ListPageVisits(_ context.Context, arg ListPageVisitsParams) ([]PageVisit, error) {
	f.listArg = arg
	return f.listItems, f.listErr
}

func (f *fakeTransactor) CountPageVisits(_ context.Context, arg CountPageVisitsParams) (int64, error) {
	f.countArg = arg
	return f.count, f.countErr
}

type fakeTransactionRepository struct {
	state        *fakeState
	createErr    error
	targetExists bool
	targetErr    error
}

func (f *fakeTransactionRepository) LockPageVisitStudent(_ context.Context, studentID uuid.UUID) (uuid.UUID, error) {
	if !f.state.students[studentID] {
		return uuid.Nil, pgx.ErrNoRows
	}
	return studentID, nil
}

func (f *fakeTransactionRepository) PageVisitByIdempotencyKey(_ context.Context, arg PageVisitByIdempotencyKeyParams) (PageVisit, error) {
	for _, visit := range f.state.visits {
		if visit.StudentID == arg.StudentID && visit.IdempotencyKey == arg.IdempotencyKey {
			return visit, nil
		}
	}
	return PageVisit{}, pgx.ErrNoRows
}

func (f *fakeTransactionRepository) PageVisitByIDForStudent(_ context.Context, arg PageVisitByIDForStudentParams) (PageVisit, error) {
	visit, ok := f.state.visits[arg.ID]
	if !ok || visit.StudentID != arg.StudentID {
		return PageVisit{}, pgx.ErrNoRows
	}
	return visit, nil
}

func (f *fakeTransactionRepository) LockPageVisitTarget(_ context.Context, arg LockPageVisitTargetParams) (uuid.UUID, error) {
	if f.targetErr != nil {
		return uuid.Nil, f.targetErr
	}
	if !f.targetExists {
		return uuid.Nil, pgx.ErrNoRows
	}
	return arg.PageID, nil
}

func (f *fakeTransactionRepository) OpenPageVisitForStudentSession(_ context.Context, arg OpenPageVisitForStudentSessionParams) (PageVisit, error) {
	for _, visit := range f.state.visits {
		if visit.StudentID == arg.StudentID && visit.ClientSessionID == arg.ClientSessionID && !visit.LeftAt.Valid {
			return visit, nil
		}
	}
	return PageVisit{}, pgx.ErrNoRows
}

func (f *fakeTransactionRepository) CloseOpenPageVisit(_ context.Context, arg CloseOpenPageVisitParams) (PageVisit, error) {
	visit, ok := f.state.visits[arg.ID]
	if !ok || visit.StudentID != arg.StudentID || visit.LeftAt.Valid {
		return PageVisit{}, pgx.ErrNoRows
	}
	visit.LeftAt = arg.LeftAt
	f.state.visits[visit.ID] = visit
	return visit, nil
}

func (f *fakeTransactionRepository) CreatePageVisit(_ context.Context, arg CreatePageVisitParams) (PageVisit, error) {
	if f.createErr != nil {
		return PageVisit{}, f.createErr
	}
	visit := PageVisit{
		ID:              uuid.New(),
		StudentID:       arg.StudentID,
		CourseID:        arg.CourseID,
		PageID:          arg.PageID,
		ClientSessionID: arg.ClientSessionID,
		IdempotencyKey:  arg.IdempotencyKey,
		EnteredAt:       arg.EnteredAt,
	}
	f.state.visits[visit.ID] = visit
	return visit, nil
}

func (f *fakeTransactionRepository) LeavePageVisitIfOpen(_ context.Context, arg LeavePageVisitIfOpenParams) (PageVisit, error) {
	visit, ok := f.state.visits[arg.ID]
	if !ok || visit.StudentID != arg.StudentID || visit.LeftAt.Valid {
		return PageVisit{}, pgx.ErrNoRows
	}
	visit.LeftAt = arg.LeftAt
	f.state.visits[visit.ID] = visit
	return visit, nil
}

type fakeClock struct {
	times []time.Time
	calls int
}

func (c *fakeClock) now() time.Time {
	if c.calls >= len(c.times) {
		panic("fake clock called more times than configured")
	}
	result := c.times[c.calls]
	c.calls++
	return result
}

func enterInput(studentID, sessionID, key uuid.UUID) EnterInput {
	return EnterInput{
		StudentID:       studentID,
		CourseID:        uuid.New(),
		PageID:          uuid.New(),
		ClientSessionID: sessionID,
		IdempotencyKey:  key,
	}
}

func seedFakeVisit(tx *fakeTransactor, input EnterInput, enteredAt time.Time, leftAt *time.Time) PageVisit {
	visit := PageVisit{
		ID:              uuid.New(),
		StudentID:       input.StudentID,
		CourseID:        input.CourseID,
		PageID:          input.PageID,
		ClientSessionID: input.ClientSessionID,
		IdempotencyKey:  input.IdempotencyKey,
		EnteredAt:       pgtype.Timestamptz{Time: enteredAt, Valid: true},
	}
	if leftAt != nil {
		visit.LeftAt = pgtype.Timestamptz{Time: *leftAt, Valid: true}
	}
	tx.state.visits[visit.ID] = visit
	return visit
}

func TestServiceEnter(t *testing.T) {
	studentID := uuid.New()
	now := time.Date(2026, 9, 12, 6, 0, 0, 123456000, time.FixedZone("test", 8*60*60))

	tests := []struct {
		name         string
		arrange      func(*fakeTransactor, EnterInput) PageVisit
		mutate       func(EnterInput) EnterInput
		createErr    error
		targetExists *bool
		targetErr    error
		wantErr      error
		wantVisits   int
		wantClock    int
		check        func(*testing.T, *fakeTransactor, EnterInput, PageVisit, PageVisit)
	}{
		{
			name:       "first enter creates an open visit with server time",
			wantVisits: 1,
			wantClock:  1,
			check: func(t *testing.T, _ *fakeTransactor, _ EnterInput, got, _ PageVisit) {
				if !got.EnteredAt.Valid || !got.EnteredAt.Time.Equal(now.UTC()) || got.LeftAt.Valid {
					t.Fatalf("unexpected created PageVisit: %+v", got)
				}
			},
		},
		{
			name: "second enter closes the previous visit at exactly the new enteredAt",
			arrange: func(tx *fakeTransactor, input EnterInput) PageVisit {
				oldInput := input
				oldInput.IdempotencyKey = uuid.New()
				return seedFakeVisit(tx, oldInput, now.Add(-time.Hour), nil)
			},
			wantVisits: 2,
			wantClock:  1,
			check: func(t *testing.T, tx *fakeTransactor, _ EnterInput, got, old PageVisit) {
				closed := tx.state.visits[old.ID]
				if !closed.LeftAt.Valid || !closed.LeftAt.Time.Equal(got.EnteredAt.Time) {
					t.Fatalf("previous leftAt %v must equal new enteredAt %v", closed.LeftAt, got.EnteredAt)
				}
			},
		},
		{
			name: "different client session does not close unrelated visit",
			arrange: func(tx *fakeTransactor, input EnterInput) PageVisit {
				oldInput := input
				oldInput.ClientSessionID = uuid.New()
				oldInput.IdempotencyKey = uuid.New()
				return seedFakeVisit(tx, oldInput, now.Add(-time.Hour), nil)
			},
			wantVisits: 2,
			wantClock:  1,
			check: func(t *testing.T, tx *fakeTransactor, _ EnterInput, _ PageVisit, old PageVisit) {
				if tx.state.visits[old.ID].LeftAt.Valid {
					t.Fatal("unrelated PageVisit was closed")
				}
			},
		},
		{
			name: "same key and request returns original without calling clock",
			arrange: func(tx *fakeTransactor, input EnterInput) PageVisit {
				return seedFakeVisit(tx, input, now.Add(-time.Hour), nil)
			},
			wantVisits: 1,
			wantClock:  0,
			check: func(t *testing.T, _ *fakeTransactor, _ EnterInput, got, old PageVisit) {
				if got.ID != old.ID || !got.EnteredAt.Time.Equal(old.EnteredAt.Time) {
					t.Fatalf("expected original PageVisit, got %+v", got)
				}
			},
		},
		{
			name: "same key and different request conflicts without calling clock",
			arrange: func(tx *fakeTransactor, input EnterInput) PageVisit {
				return seedFakeVisit(tx, input, now.Add(-time.Hour), nil)
			},
			mutate: func(input EnterInput) EnterInput {
				input.PageID = uuid.New()
				return input
			},
			wantErr:    ErrIdempotencyConflict,
			wantVisits: 1,
			wantClock:  0,
		},
		{
			name:         "missing course or page returns not found before mutation",
			targetExists: ptrBool(false),
			wantErr:      ErrNotFound,
			wantVisits:   0,
			wantClock:    0,
		},
		{
			name:       "unexpected target lookup error remains unexpected",
			targetErr:  errCheckTarget,
			wantErr:    errCheckTarget,
			wantVisits: 0,
			wantClock:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newFakeTransactor(studentID)
			input := enterInput(studentID, uuid.New(), uuid.New())
			var old PageVisit
			if tt.arrange != nil {
				old = tt.arrange(tx, input)
			}
			if tt.mutate != nil {
				input = tt.mutate(input)
			}
			tx.createErr = tt.createErr
			if tt.targetExists != nil {
				tx.targetExists = *tt.targetExists
			}
			tx.targetErr = tt.targetErr
			clock := &fakeClock{times: []time.Time{now}}
			got, err := NewService(tx, clock.now).Enter(t.Context(), input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
			if len(tx.state.visits) != tt.wantVisits {
				t.Fatalf("expected %d visits, got %d", tt.wantVisits, len(tx.state.visits))
			}
			if clock.calls != tt.wantClock {
				t.Fatalf("expected %d clock calls, got %d", tt.wantClock, clock.calls)
			}
			if tt.check != nil {
				tt.check(t, tx, input, got, old)
			}
		})
	}
}

func ptrBool(value bool) *bool {
	return &value
}

func TestServiceEnterReplayDoesNotCloseNewerVisit(t *testing.T) {
	studentID := uuid.New()
	tx := newFakeTransactor(studentID)
	sessionID := uuid.New()
	originalInput := enterInput(studentID, sessionID, uuid.New())
	originalLeftAt := time.Date(2026, 9, 12, 5, 0, 0, 0, time.UTC)
	original := seedFakeVisit(tx, originalInput, originalLeftAt.Add(-time.Minute), &originalLeftAt)
	newerInput := enterInput(studentID, sessionID, uuid.New())
	newer := seedFakeVisit(tx, newerInput, originalLeftAt, nil)
	clock := &fakeClock{}

	got, err := NewService(tx, clock.now).Enter(t.Context(), originalInput)
	if err != nil {
		t.Fatalf("replay enter: %v", err)
	}
	if got.ID != original.ID || tx.state.visits[newer.ID].LeftAt.Valid || clock.calls != 0 {
		t.Fatalf("replay mutated current PageVisit: got=%+v newer=%+v calls=%d", got, tx.state.visits[newer.ID], clock.calls)
	}
}

func TestServiceEnterCreateFailureRollsBackAutomaticClose(t *testing.T) {
	studentID := uuid.New()
	tx := newFakeTransactor(studentID)
	input := enterInput(studentID, uuid.New(), uuid.New())
	oldInput := input
	oldInput.IdempotencyKey = uuid.New()
	old := seedFakeVisit(tx, oldInput, time.Now().Add(-time.Hour), nil)
	tx.createErr = errCreateVisit
	clock := &fakeClock{times: []time.Time{time.Now()}}

	_, err := NewService(tx, clock.now).Enter(t.Context(), input)
	if !errors.Is(err, errCreateVisit) {
		t.Fatalf("expected create failure, got %v", err)
	}
	if tx.state.visits[old.ID].LeftAt.Valid {
		t.Fatal("failed create committed automatic close")
	}
}

func TestServiceLeave(t *testing.T) {
	studentID := uuid.New()
	otherID := uuid.New()
	leftAt := time.Date(2026, 9, 12, 7, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		studentID uuid.UUID
		closedAt  *time.Time
		wantErr   error
		wantClock int
	}{
		{name: "first leave closes visit", studentID: studentID, wantClock: 1},
		{name: "repeated leave preserves original close time", studentID: studentID, closedAt: ptrTime(leftAt.Add(-time.Hour)), wantClock: 0},
		{name: "another student sees not found", studentID: otherID, wantErr: ErrNotFound, wantClock: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newFakeTransactor(studentID, otherID)
			input := enterInput(studentID, uuid.New(), uuid.New())
			visit := seedFakeVisit(tx, input, leftAt.Add(-2*time.Hour), tt.closedAt)
			clock := &fakeClock{times: []time.Time{leftAt}}

			got, err := NewService(tx, clock.now).Leave(t.Context(), LeaveInput{StudentID: tt.studentID, VisitID: visit.ID})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
			if clock.calls != tt.wantClock {
				t.Fatalf("expected %d clock calls, got %d", tt.wantClock, clock.calls)
			}
			if tt.wantErr != nil {
				return
			}
			want := leftAt
			if tt.closedAt != nil {
				want = *tt.closedAt
			}
			if !got.LeftAt.Valid || !got.LeftAt.Time.Equal(want) {
				t.Fatalf("expected leftAt %v, got %v", want, got.LeftAt)
			}
		})
	}
}

func TestServiceLeaveAfterAutomaticClosePreservesCloseTime(t *testing.T) {
	studentID := uuid.New()
	tx := newFakeTransactor(studentID)
	sessionID := uuid.New()
	oldInput := enterInput(studentID, sessionID, uuid.New())
	old := seedFakeVisit(tx, oldInput, time.Date(2026, 9, 12, 4, 0, 0, 0, time.UTC), nil)
	newInput := enterInput(studentID, sessionID, uuid.New())
	automaticCloseAt := time.Date(2026, 9, 12, 5, 0, 0, 0, time.UTC)
	clock := &fakeClock{times: []time.Time{automaticCloseAt}}
	service := NewService(tx, clock.now)

	created, err := service.Enter(t.Context(), newInput)
	if err != nil {
		t.Fatalf("enter next PageVisit: %v", err)
	}
	closed, err := service.Leave(t.Context(), LeaveInput{StudentID: studentID, VisitID: old.ID})
	if err != nil {
		t.Fatalf("leave automatically closed PageVisit: %v", err)
	}
	if !closed.LeftAt.Valid || !closed.LeftAt.Time.Equal(created.EnteredAt.Time) {
		t.Fatalf("expected automatic close time %v, got %v", created.EnteredAt, closed.LeftAt)
	}
	if clock.calls != 1 {
		t.Fatalf("expected leave not to generate another timestamp, got %d clock calls", clock.calls)
	}
}

func TestServiceLeaveMissingVisitReturnsNotFound(t *testing.T) {
	studentID := uuid.New()
	tx := newFakeTransactor(studentID)
	clock := &fakeClock{}

	_, err := NewService(tx, clock.now).Leave(t.Context(), LeaveInput{StudentID: studentID, VisitID: uuid.New()})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
	if clock.calls != 0 {
		t.Fatalf("expected no timestamp for missing PageVisit, got %d calls", clock.calls)
	}
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

func TestServiceList(t *testing.T) {
	studentID := uuid.New()
	status := StatusOpen
	from := time.Now().UTC().Add(-time.Hour)
	tx := newFakeTransactor()
	tx.listItems = []PageVisit{{ID: uuid.New(), StudentID: studentID}}
	tx.count = 21

	page, err := NewService(tx, nil).List(t.Context(), ListInput{
		StudentID:   &studentID,
		Status:      &status,
		EnteredFrom: &from,
		Page:        2,
		PageSize:    20,
	})
	if err != nil {
		t.Fatalf("list PageVisits: %v", err)
	}
	if len(page.Items) != 1 || page.TotalItems != 21 || page.TotalPages != 2 || page.CurrentPage != 2 || page.PageSize != 20 || page.HasNextPage {
		t.Fatalf("unexpected page: %+v", page)
	}
	if !tx.listArg.StudentID.Valid || tx.listArg.StudentID.Bytes != studentID || tx.listArg.Status.String != "OPEN" || !tx.listArg.EnteredFrom.Valid {
		t.Fatalf("filters not forwarded: %+v", tx.listArg)
	}
	if tx.listArg.Offset != 20 || tx.listArg.Limit != 20 {
		t.Fatalf("pagination not forwarded: %+v", tx.listArg)
	}
}

func TestServiceListAcceptsLargestValidPage(t *testing.T) {
	tx := newFakeTransactor()
	const page = int32(2147483647)
	const pageSize = int32(100)

	_, err := NewService(tx, nil).List(t.Context(), ListInput{Page: page, PageSize: pageSize})
	if err != nil {
		t.Fatalf("list largest valid page: %v", err)
	}
	wantOffset := (int64(page) - 1) * int64(pageSize)
	if tx.listArg.Offset != wantOffset {
		t.Fatalf("expected offset %d, got %d", wantOffset, tx.listArg.Offset)
	}
}
