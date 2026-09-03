package page

import (
	"context"
	"fmt"
	"sort"

	"sciedu-backend/internal/course"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type PageQuerier interface {
	ListPagesByCourse(ctx context.Context, courseID uuid.UUID) ([]Page, error)
	GetPageByID(ctx context.Context, id uuid.UUID) (Page, error)
	CreatePage(ctx context.Context, arg CreatePageParams) (Page, error)
	UpdatePage(ctx context.Context, arg UpdatePageParams) (Page, error)
	DeletePage(ctx context.Context, id uuid.UUID) (int64, error)
	LockCourseForPageReorder(ctx context.Context, id uuid.UUID) (uuid.UUID, error)
	DeferOrderConstraints(ctx context.Context) error
	SetPageOrders(ctx context.Context, arg SetPageOrdersParams) ([]Page, error)
}

type Transactor interface {
	WithinTx(ctx context.Context, fn func(PageQuerier, BlockQuerier) error) error
}

// PageStore is what *Store provides. Embedding Transactor rather than asserting for
// it at runtime makes a store without transaction support a compile error.
type PageStore interface {
	PageQuerier
	Transactor
}

// CourseAccess centralizes role- and Experiment-backed Course read authorization.
type CourseAccess interface {
	ByID(ctx context.Context, id uuid.UUID) (course.Record, error)
	ByIDForActor(ctx context.Context, actorID, courseID uuid.UUID) (course.Record, error)
}

type PageRequest struct {
	Title        string
	DisplayOrder int32
}

// Detail mirrors the PageDetail model in pages.tsp: a page plus its blocks in display order.
type Detail struct {
	Page
	Blocks []Block
}

type PageService struct {
	logger       *zap.Logger
	querier      PageStore
	blockService *BlockService
	courses      CourseAccess
}

func NewPageService(querier PageStore, blockService *BlockService, courses CourseAccess, logger *zap.Logger) *PageService {
	if logger == nil {
		logger = zap.NewNop()
	}
	if blockService == nil {
		panic("page: NewPageService requires a block service")
	}

	return &PageService{
		logger:       logger,
		querier:      querier,
		blockService: blockService,
		courses:      courses,
	}
}

func (s *PageService) ListByCourseForActor(ctx context.Context, actorID, courseID uuid.UUID) ([]Page, error) {
	if _, err := s.courses.ByIDForActor(ctx, actorID, courseID); err != nil {
		return nil, err
	}

	pages, err := s.querier.ListPagesByCourse(ctx, courseID)
	if err != nil {
		return nil, databaseutil.WrapDBError(err, s.logger, "list pages by course")
	}
	return pages, nil
}

func (s *PageService) GetForActor(ctx context.Context, actorID, id uuid.UUID) (Detail, error) {
	page, err := s.querier.GetPageByID(ctx, id)
	if err != nil {
		return Detail{}, databaseutil.WrapDBErrorWithKeyValue(err, "pages", "id", id.String(), s.logger, "get page")
	}
	if _, err := s.courses.ByIDForActor(ctx, actorID, page.CourseID); err != nil {
		return Detail{}, err
	}

	blocks, err := s.blockService.listByPage(ctx, id)
	if err != nil {
		return Detail{}, err
	}

	return Detail{Page: page, Blocks: blocks}, nil
}

func (s *PageService) ListBlocksForActor(ctx context.Context, actorID, pageID uuid.UUID) ([]Block, error) {
	page, err := s.querier.GetPageByID(ctx, pageID)
	if err != nil {
		return nil, databaseutil.WrapDBErrorWithKeyValue(err, "pages", "id", pageID.String(), s.logger, "get page")
	}
	if _, err := s.courses.ByIDForActor(ctx, actorID, page.CourseID); err != nil {
		return nil, err
	}
	return s.blockService.listByPage(ctx, pageID)
}

func (s *PageService) Create(ctx context.Context, courseID uuid.UUID, req PageRequest) (Detail, error) {
	if err := validateDisplayOrder(req.DisplayOrder, errInvalidPagePayload); err != nil {
		return Detail{}, err
	}
	if err := s.ensureCourseExists(ctx, courseID); err != nil {
		return Detail{}, err
	}

	page, err := s.querier.CreatePage(ctx, CreatePageParams{
		CourseID:     courseID,
		Title:        req.Title,
		DisplayOrder: req.DisplayOrder,
	})
	if err != nil {
		return Detail{}, databaseutil.WrapDBError(err, s.logger, "create page")
	}

	return Detail{Page: page, Blocks: []Block{}}, nil
}

func (s *PageService) Update(ctx context.Context, id uuid.UUID, req PageRequest) (Detail, error) {
	if err := validateDisplayOrder(req.DisplayOrder, errInvalidPagePayload); err != nil {
		return Detail{}, err
	}

	page, err := s.querier.UpdatePage(ctx, UpdatePageParams{
		ID:           id,
		Title:        req.Title,
		DisplayOrder: req.DisplayOrder,
	})
	if err != nil {
		return Detail{}, databaseutil.WrapDBErrorWithKeyValue(err, "pages", "id", id.String(), s.logger, "update page")
	}

	blocks, err := s.blockService.listByPage(ctx, id)
	if err != nil {
		return Detail{}, err
	}

	return Detail{Page: page, Blocks: blocks}, nil
}

func (s *PageService) Delete(ctx context.Context, id uuid.UUID) error {
	rows, err := s.querier.DeletePage(ctx, id)
	if err != nil {
		return databaseutil.WrapDBErrorWithKeyValue(err, "pages", "id", id.String(), s.logger, "delete page")
	}
	if rows == 0 {
		return handlerutil.NewNotFoundError("pages", "id", id.String(), "")
	}
	return nil
}

// Reorder replaces the display order of every page in a course. Validation,
// constraint deferral, and write-back share one transaction so uniqueness is
// checked against the complete final order at commit.
func (s *PageService) Reorder(ctx context.Context, courseID uuid.UUID, pageIDs []uuid.UUID) ([]Page, error) {
	var reordered []Page
	// The parent lock serializes membership changes before the transaction reads
	// and validates the complete Page set.
	err := s.querier.WithinTx(ctx, func(pageQuerier PageQuerier, _ BlockQuerier) error {
		if _, err := pageQuerier.LockCourseForPageReorder(ctx, courseID); err != nil {
			return databaseutil.WrapDBErrorWithKeyValue(err, "courses", "id", courseID.String(), s.logger, "lock course for page reorder")
		}

		existing, err := pageQuerier.ListPagesByCourse(ctx, courseID)
		if err != nil {
			return databaseutil.WrapDBError(err, s.logger, "list pages for reorder")
		}

		existingIDs := make([]uuid.UUID, 0, len(existing))
		for _, page := range existing {
			existingIDs = append(existingIDs, page.ID)
		}
		if err := validateReorderIDs(pageIDs, existingIDs, errInvalidPagePayload); err != nil {
			return err
		}

		if err := pageQuerier.DeferOrderConstraints(ctx); err != nil {
			return databaseutil.WrapDBError(err, s.logger, "defer page order constraint")
		}

		updated, err := pageQuerier.SetPageOrders(ctx, SetPageOrdersParams{
			CourseID: courseID,
			PageIds:  pageIDs,
		})
		if err != nil {
			return databaseutil.WrapDBError(err, s.logger, "set page orders")
		}
		if len(updated) != len(pageIDs) {
			return fmt.Errorf("%w: reordered %d of %d pages", errInvalidPagePayload, len(updated), len(pageIDs))
		}

		sort.Slice(updated, func(i, j int) bool { return updated[i].DisplayOrder < updated[j].DisplayOrder })
		reordered = updated
		return nil
	})
	if err != nil {
		return nil, wrapTransactionError(err, s.logger, "commit page reorder")
	}

	return reordered, nil
}

func (s *PageService) ensureCourseExists(ctx context.Context, courseID uuid.UUID) error {
	_, err := s.courses.ByID(ctx, courseID)
	return err
}

// validateReorderIDs enforces that the requested order names every existing item exactly once.
func validateReorderIDs(requested, existing []uuid.UUID, base error) error {
	if len(requested) != len(existing) {
		return fmt.Errorf("%w: expected %d ids, got %d", base, len(existing), len(requested))
	}

	seen := make(map[uuid.UUID]struct{}, len(requested))
	for _, id := range requested {
		if _, ok := seen[id]; ok {
			return fmt.Errorf("%w: duplicate id %s", base, id)
		}
		seen[id] = struct{}{}
	}

	for _, id := range existing {
		if _, ok := seen[id]; !ok {
			return fmt.Errorf("%w: missing id %s", base, id)
		}
	}

	return nil
}

func validateDisplayOrder(order int32, base error) error {
	if order < 0 {
		return fmt.Errorf("%w: displayOrder must be non-negative, got %d", base, order)
	}
	return nil
}
