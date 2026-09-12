package pagevisit

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type TransactionRepository interface {
	LockPageVisitStudent(ctx context.Context, studentID uuid.UUID) (uuid.UUID, error)
	PageVisitByIdempotencyKey(ctx context.Context, arg PageVisitByIdempotencyKeyParams) (PageVisit, error)
	PageVisitByIDForStudent(ctx context.Context, arg PageVisitByIDForStudentParams) (PageVisit, error)
	LockPageVisitTarget(ctx context.Context, arg LockPageVisitTargetParams) (uuid.UUID, error)
	OpenPageVisitForStudentSession(ctx context.Context, arg OpenPageVisitForStudentSessionParams) (PageVisit, error)
	CloseOpenPageVisit(ctx context.Context, arg CloseOpenPageVisitParams) (PageVisit, error)
	CreatePageVisit(ctx context.Context, arg CreatePageVisitParams) (PageVisit, error)
	LeavePageVisitIfOpen(ctx context.Context, arg LeavePageVisitIfOpenParams) (PageVisit, error)
}

type Repository interface {
	Transactor
	ListPageVisits(ctx context.Context, arg ListPageVisitsParams) ([]PageVisit, error)
	CountPageVisits(ctx context.Context, arg CountPageVisitsParams) (int64, error)
}

type Transactor interface {
	WithinTx(ctx context.Context, fn func(TransactionRepository) error) error
}

type Clock func() time.Time

type EnterInput struct {
	StudentID       uuid.UUID
	CourseID        uuid.UUID
	PageID          uuid.UUID
	ClientSessionID uuid.UUID
	IdempotencyKey  uuid.UUID
}

type LeaveInput struct {
	StudentID uuid.UUID
	VisitID   uuid.UUID
}

type Service struct {
	repo Repository
	now  Clock
}

func NewService(repo Repository, now Clock) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{repo: repo, now: now}
}

func (s *Service) Enter(ctx context.Context, input EnterInput) (PageVisit, error) {
	var result PageVisit
	err := s.repo.WithinTx(ctx, func(repo TransactionRepository) error {
		if _, err := repo.LockPageVisitStudent(ctx, input.StudentID); err != nil {
			return mapStudentLockError(err)
		}

		existing, err := repo.PageVisitByIdempotencyKey(ctx, PageVisitByIdempotencyKeyParams{
			StudentID:      input.StudentID,
			IdempotencyKey: input.IdempotencyKey,
		})
		switch {
		case err == nil:
			if !sameEnterRequest(existing, input) {
				return ErrIdempotencyConflict
			}
			result = existing
			return nil
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("look up PageVisit idempotency key: %w", err)
		}

		_, err = repo.LockPageVisitTarget(ctx, LockPageVisitTargetParams{
			PageID:   input.PageID,
			CourseID: input.CourseID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("check PageVisit target: %w", err)
		}

		enteredAt := pgtype.Timestamptz{Time: s.now().UTC(), Valid: true}
		openVisit, err := repo.OpenPageVisitForStudentSession(ctx, OpenPageVisitForStudentSessionParams{
			StudentID:       input.StudentID,
			ClientSessionID: input.ClientSessionID,
		})
		switch {
		case err == nil:
			if _, err := repo.CloseOpenPageVisit(ctx, CloseOpenPageVisitParams{
				LeftAt:    enteredAt,
				ID:        openVisit.ID,
				StudentID: input.StudentID,
			}); err != nil {
				return fmt.Errorf("automatically close PageVisit: %w", err)
			}
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("find open PageVisit: %w", err)
		}

		created, err := repo.CreatePageVisit(ctx, CreatePageVisitParams{
			StudentID:       input.StudentID,
			CourseID:        input.CourseID,
			PageID:          input.PageID,
			ClientSessionID: input.ClientSessionID,
			IdempotencyKey:  input.IdempotencyKey,
			EnteredAt:       enteredAt,
		})
		if err != nil {
			return fmt.Errorf("create PageVisit: %w", err)
		}
		result = created
		return nil
	})
	if err != nil {
		return PageVisit{}, err
	}
	return result, nil
}

func (s *Service) Leave(ctx context.Context, input LeaveInput) (PageVisit, error) {
	var result PageVisit
	err := s.repo.WithinTx(ctx, func(repo TransactionRepository) error {
		if _, err := repo.LockPageVisitStudent(ctx, input.StudentID); err != nil {
			return mapStudentLockError(err)
		}

		existing, err := repo.PageVisitByIDForStudent(ctx, PageVisitByIDForStudentParams{
			ID:        input.VisitID,
			StudentID: input.StudentID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("get PageVisit for leave: %w", err)
		}
		if existing.LeftAt.Valid {
			result = existing
			return nil
		}

		updated, err := repo.LeavePageVisitIfOpen(ctx, LeavePageVisitIfOpenParams{
			LeftAt:    pgtype.Timestamptz{Time: s.now().UTC(), Valid: true},
			ID:        input.VisitID,
			StudentID: input.StudentID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("leave PageVisit: %w", err)
		}
		result = updated
		return nil
	})
	if err != nil {
		return PageVisit{}, err
	}
	return result, nil
}

type Status string

const (
	StatusOpen   Status = "OPEN"
	StatusClosed Status = "CLOSED"
)

type ListInput struct {
	StudentID       *uuid.UUID
	CourseID        *uuid.UUID
	PageID          *uuid.UUID
	ClientSessionID *uuid.UUID
	Status          *Status
	EnteredFrom     *time.Time
	EnteredBefore   *time.Time
	Page            int32
	PageSize        int32
}

type VisitPage struct {
	Items       []PageVisit
	TotalPages  int32
	TotalItems  int32
	CurrentPage int32
	PageSize    int32
	HasNextPage bool
}

func (s *Service) List(ctx context.Context, input ListInput) (VisitPage, error) {
	if input.Page < 1 || input.PageSize < 1 || input.PageSize > 100 {
		return VisitPage{}, fmt.Errorf("%w: invalid pagination", ErrInvalidInput)
	}
	if input.Status != nil && *input.Status != StatusOpen && *input.Status != StatusClosed {
		return VisitPage{}, fmt.Errorf("%w: unknown status", ErrInvalidInput)
	}
	offset := (int64(input.Page) - 1) * int64(input.PageSize)

	params := listParams(input)
	params.Offset = offset
	params.Limit = input.PageSize
	items, err := s.repo.ListPageVisits(ctx, params)
	if err != nil {
		return VisitPage{}, fmt.Errorf("list page visits: %w", err)
	}
	count, err := s.repo.CountPageVisits(ctx, countParams(input))
	if err != nil {
		return VisitPage{}, fmt.Errorf("count page visits: %w", err)
	}
	if count > math.MaxInt32 {
		return VisitPage{}, fmt.Errorf("page visit count exceeds supported range: %d", count)
	}

	var totalPages int32
	if count > 0 {
		totalPages = int32((count + int64(input.PageSize) - 1) / int64(input.PageSize))
	}
	return VisitPage{
		Items:       items,
		TotalPages:  totalPages,
		TotalItems:  int32(count),
		CurrentPage: input.Page,
		PageSize:    input.PageSize,
		HasNextPage: input.Page < totalPages,
	}, nil
}

func listParams(input ListInput) ListPageVisitsParams {
	filter := countParams(input)
	return ListPageVisitsParams{
		StudentID:       filter.StudentID,
		CourseID:        filter.CourseID,
		PageID:          filter.PageID,
		ClientSessionID: filter.ClientSessionID,
		Status:          filter.Status,
		EnteredFrom:     filter.EnteredFrom,
		EnteredBefore:   filter.EnteredBefore,
	}
}

func countParams(input ListInput) CountPageVisitsParams {
	return CountPageVisitsParams{
		StudentID:       nullableUUID(input.StudentID),
		CourseID:        nullableUUID(input.CourseID),
		PageID:          nullableUUID(input.PageID),
		ClientSessionID: nullableUUID(input.ClientSessionID),
		Status:          nullableStatus(input.Status),
		EnteredFrom:     nullableTime(input.EnteredFrom),
		EnteredBefore:   nullableTime(input.EnteredBefore),
	}
}

func nullableUUID(value *uuid.UUID) pgtype.UUID {
	if value == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *value, Valid: true}
}

func nullableStatus(value *Status) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: string(*value), Valid: true}
}

func nullableTime(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *value, Valid: true}
}

func sameEnterRequest(visit PageVisit, input EnterInput) bool {
	return visit.StudentID == input.StudentID &&
		visit.CourseID == input.CourseID &&
		visit.PageID == input.PageID &&
		visit.ClientSessionID == input.ClientSessionID
}

func mapStudentLockError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return fmt.Errorf("lock page visit student: %w", err)
}
