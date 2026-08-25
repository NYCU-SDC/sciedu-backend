package page

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"sciedu-backend/internal/content"
	"sciedu-backend/internal/question"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/zap"
)

const (
	BlockTypeText     = "TEXT"
	BlockTypeMedia    = "MEDIA"
	BlockTypeQuestion = "QUESTION"
)

type BlockQuerier interface {
	ListBlocksByPage(ctx context.Context, pageID uuid.UUID) ([]PageBlock, error)
	GetBlockByID(ctx context.Context, id uuid.UUID) (PageBlock, error)
	CreateContentBlock(ctx context.Context, arg CreateContentBlockParams) (PageBlock, error)
	CreateQuestionBlock(ctx context.Context, arg CreateQuestionBlockParams) (PageBlock, error)
	UpdateContentBlock(ctx context.Context, arg UpdateContentBlockParams) (PageBlock, error)
	UpdateQuestionBlock(ctx context.Context, arg UpdateQuestionBlockParams) (PageBlock, error)
	DeleteBlock(ctx context.Context, arg DeleteBlockParams) (int64, error)
	LockPageForBlockReorder(ctx context.Context, id uuid.UUID) (uuid.UUID, error)
	DeferOrderConstraints(ctx context.Context) error
	SetBlockOrders(ctx context.Context, arg SetBlockOrdersParams) ([]PageBlock, error)
}

// BlockStore is the block-side counterpart of PageStore.
type BlockStore interface {
	BlockQuerier
	Transactor
}

// PageLookup is satisfied by *Store; blocks only need to confirm their page exists.
type PageLookup interface {
	GetPageByID(ctx context.Context, id uuid.UUID) (Page, error)
}

// ContentLookup is satisfied by *content.Service.
type ContentLookup interface {
	GetContent(ctx context.Context, id uuid.UUID) (content.Content, error)
	BatchGetContents(ctx context.Context, ids []uuid.UUID) ([]content.Content, error)
}

// QuestionLookup is satisfied by *question.QuestionService.
type QuestionLookup interface {
	Get(ctx context.Context, id uuid.UUID) (question.Question, error)
}

type BlockRequest struct {
	Type         string
	ResourceID   uuid.UUID
	DisplayOrder int32
	Required     bool
}

// Block collapses the mutually exclusive content_id/question_id columns into the
// single resource reference the API exposes.
type Block struct {
	ID           uuid.UUID
	PageID       uuid.UUID
	Type         string
	ResourceID   uuid.UUID
	DisplayOrder int32
	Required     bool
	CreatedAt    pgtype.Timestamptz
	UpdatedAt    pgtype.Timestamptz
}

type BlockService struct {
	logger    *zap.Logger
	querier   BlockStore
	pages     PageLookup
	contents  ContentLookup
	questions QuestionLookup
}

func NewBlockService(querier BlockStore, pages PageLookup, contents ContentLookup, questions QuestionLookup, logger *zap.Logger) *BlockService {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &BlockService{
		logger:    logger,
		querier:   querier,
		pages:     pages,
		contents:  contents,
		questions: questions,
	}
}

func (s *BlockService) ListByPage(ctx context.Context, pageID uuid.UUID) ([]Block, error) {
	if err := s.ensurePageExists(ctx, pageID); err != nil {
		return nil, err
	}
	return s.listByPage(ctx, pageID)
}

// listByPage skips the page existence check for callers that already hold the page.
func (s *BlockService) listByPage(ctx context.Context, pageID uuid.UUID) ([]Block, error) {
	rows, err := s.querier.ListBlocksByPage(ctx, pageID)
	if err != nil {
		return nil, databaseutil.WrapDBError(err, s.logger, "list page blocks")
	}
	return s.toBlocks(ctx, rows)
}

func (s *BlockService) Create(ctx context.Context, pageID uuid.UUID, req BlockRequest) (Block, error) {
	if err := validateDisplayOrder(req.DisplayOrder, errInvalidBlockPayload); err != nil {
		return Block{}, err
	}
	if err := s.ensurePageExists(ctx, pageID); err != nil {
		return Block{}, err
	}
	if err := s.validateResource(ctx, req.Type, req.ResourceID); err != nil {
		return Block{}, err
	}

	var (
		row PageBlock
		err error
	)
	if req.Type == BlockTypeQuestion {
		row, err = s.querier.CreateQuestionBlock(ctx, CreateQuestionBlockParams{
			PageID:       pageID,
			QuestionID:   toPgUUID(req.ResourceID),
			DisplayOrder: req.DisplayOrder,
			Required:     req.Required,
		})
	} else {
		row, err = s.querier.CreateContentBlock(ctx, CreateContentBlockParams{
			PageID:       pageID,
			ContentID:    toPgUUID(req.ResourceID),
			DisplayOrder: req.DisplayOrder,
			Required:     req.Required,
		})
	}
	if err != nil {
		return Block{}, databaseutil.WrapDBError(err, s.logger, "create page block")
	}

	return blockFromRow(row, req.Type, req.ResourceID), nil
}

func (s *BlockService) Update(ctx context.Context, pageID, blockID uuid.UUID, req BlockRequest) (Block, error) {
	if err := validateDisplayOrder(req.DisplayOrder, errInvalidBlockPayload); err != nil {
		return Block{}, err
	}
	if err := s.ensureBlockBelongsToPage(ctx, pageID, blockID); err != nil {
		return Block{}, err
	}
	if err := s.validateResource(ctx, req.Type, req.ResourceID); err != nil {
		return Block{}, err
	}

	var (
		row PageBlock
		err error
	)
	if req.Type == BlockTypeQuestion {
		row, err = s.querier.UpdateQuestionBlock(ctx, UpdateQuestionBlockParams{
			ID:           blockID,
			QuestionID:   toPgUUID(req.ResourceID),
			DisplayOrder: req.DisplayOrder,
			Required:     req.Required,
		})
	} else {
		row, err = s.querier.UpdateContentBlock(ctx, UpdateContentBlockParams{
			ID:           blockID,
			ContentID:    toPgUUID(req.ResourceID),
			DisplayOrder: req.DisplayOrder,
			Required:     req.Required,
		})
	}
	if err != nil {
		return Block{}, databaseutil.WrapDBErrorWithKeyValue(err, "page_blocks", "id", blockID.String(), s.logger, "update page block")
	}

	return blockFromRow(row, req.Type, req.ResourceID), nil
}

// Delete scopes the statement by page_id, so a block on another page is
// indistinguishable from a missing one without a second, racy lookup.
func (s *BlockService) Delete(ctx context.Context, pageID, blockID uuid.UUID) error {
	rows, err := s.querier.DeleteBlock(ctx, DeleteBlockParams{ID: blockID, PageID: pageID})
	if err != nil {
		return databaseutil.WrapDBErrorWithKeyValue(err, "page_blocks", "id", blockID.String(), s.logger, "delete page block")
	}
	if rows == 0 {
		return handlerutil.NewNotFoundError("page_blocks", "id", blockID.String(), "")
	}
	return nil
}

// Reorder replaces the display order of every block in a page. Validation,
// constraint deferral, and write-back share one transaction so uniqueness is
// checked against the complete final order at commit.
func (s *BlockService) Reorder(ctx context.Context, pageID uuid.UUID, blockIDs []uuid.UUID) ([]Block, error) {
	var rows []PageBlock
	// The parent lock serializes membership changes before the transaction reads
	// and validates the complete PageBlock set.
	err := s.querier.WithinTx(ctx, func(_ PageQuerier, blockQuerier BlockQuerier) error {
		if _, err := blockQuerier.LockPageForBlockReorder(ctx, pageID); err != nil {
			return databaseutil.WrapDBErrorWithKeyValue(err, "pages", "id", pageID.String(), s.logger, "lock page for block reorder")
		}

		existing, err := blockQuerier.ListBlocksByPage(ctx, pageID)
		if err != nil {
			return databaseutil.WrapDBError(err, s.logger, "list page blocks for reorder")
		}

		existingIDs := make([]uuid.UUID, 0, len(existing))
		for _, block := range existing {
			existingIDs = append(existingIDs, block.ID)
		}
		if err := validateReorderIDs(blockIDs, existingIDs, errInvalidBlockPayload); err != nil {
			return err
		}

		if err := blockQuerier.DeferOrderConstraints(ctx); err != nil {
			return databaseutil.WrapDBError(err, s.logger, "defer page block order constraint")
		}

		updated, err := blockQuerier.SetBlockOrders(ctx, SetBlockOrdersParams{
			PageID:   pageID,
			BlockIds: blockIDs,
		})
		if err != nil {
			return databaseutil.WrapDBError(err, s.logger, "set page block orders")
		}
		if len(updated) != len(blockIDs) {
			return fmt.Errorf("%w: reordered %d of %d blocks", errInvalidBlockPayload, len(updated), len(blockIDs))
		}

		sort.Slice(updated, func(i, j int) bool { return updated[i].DisplayOrder < updated[j].DisplayOrder })
		rows = updated
		return nil
	})
	if err != nil {
		return nil, wrapTransactionError(err, s.logger, "commit page block reorder")
	}

	return s.toBlocks(ctx, rows)
}

// validateResource checks resourceID against the table implied by blockType. The
// TEXT/MEDIA match on contents.type cannot be an FK: both map to content_id, and
// only the contents row tells them apart.
func (s *BlockService) validateResource(ctx context.Context, blockType string, resourceID uuid.UUID) error {
	switch blockType {
	case BlockTypeText, BlockTypeMedia:
		item, err := s.contents.GetContent(ctx, resourceID)
		if err != nil {
			if isNotFound(err) {
				return fmt.Errorf("%w: resourceId %s does not exist", errInvalidBlockPayload, resourceID)
			}
			return err
		}
		if item.Type != blockType {
			return fmt.Errorf("%w: resourceId %s is %s, not %s", errInvalidBlockPayload, resourceID, item.Type, blockType)
		}
	case BlockTypeQuestion:
		if _, err := s.questions.Get(ctx, resourceID); err != nil {
			if isNotFound(err) {
				return fmt.Errorf("%w: resourceId %s does not exist", errInvalidBlockPayload, resourceID)
			}
			return err
		}
	default:
		return fmt.Errorf("%w: unsupported block type %q", errInvalidBlockPayload, blockType)
	}

	return nil
}

// toBlocks needs the contents rows to tell TEXT from MEDIA, so it fetches them in
// one batch rather than per block.
func (s *BlockService) toBlocks(ctx context.Context, rows []PageBlock) ([]Block, error) {
	contentIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		if row.ContentID.Valid {
			contentIDs = append(contentIDs, uuid.UUID(row.ContentID.Bytes))
		}
	}

	contentTypes := make(map[uuid.UUID]string, len(contentIDs))
	if len(contentIDs) > 0 {
		items, err := s.contents.BatchGetContents(ctx, contentIDs)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			contentTypes[item.ID] = item.Type
		}
	}

	blocks := make([]Block, 0, len(rows))
	for _, row := range rows {
		switch {
		case row.QuestionID.Valid:
			blocks = append(blocks, blockFromRow(row, BlockTypeQuestion, uuid.UUID(row.QuestionID.Bytes)))
		case row.ContentID.Valid:
			resourceID := uuid.UUID(row.ContentID.Bytes)
			blockType, ok := contentTypes[resourceID]
			if !ok {
				return nil, fmt.Errorf("block %s references missing content %s", row.ID, resourceID)
			}
			blocks = append(blocks, blockFromRow(row, blockType, resourceID))
		default:
			return nil, fmt.Errorf("block %s references neither a content nor a question", row.ID)
		}
	}

	return blocks, nil
}

func (s *BlockService) ensurePageExists(ctx context.Context, pageID uuid.UUID) error {
	_, err := s.pages.GetPageByID(ctx, pageID)
	return databaseutil.WrapDBErrorWithKeyValue(err, "pages", "id", pageID.String(), s.logger, "get page")
}

func (s *BlockService) ensureBlockBelongsToPage(ctx context.Context, pageID, blockID uuid.UUID) error {
	row, err := s.querier.GetBlockByID(ctx, blockID)
	if err != nil {
		return databaseutil.WrapDBErrorWithKeyValue(err, "page_blocks", "id", blockID.String(), s.logger, "get page block")
	}
	if row.PageID != pageID {
		return handlerutil.NewNotFoundError("page_blocks", "id", blockID.String(), "")
	}
	return nil
}

func blockFromRow(row PageBlock, blockType string, resourceID uuid.UUID) Block {
	return Block{
		ID:           row.ID,
		PageID:       row.PageID,
		Type:         blockType,
		ResourceID:   resourceID,
		DisplayOrder: row.DisplayOrder,
		Required:     row.Required,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}

func toPgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

// isNotFound matches both shapes summer produces: the NotFoundError struct and the
// bare ErrNotFound sentinel.
func isNotFound(err error) bool {
	return errors.Is(err, handlerutil.ErrNotFound)
}
