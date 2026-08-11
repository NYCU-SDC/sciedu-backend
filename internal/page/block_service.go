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
	OffsetBlockOrders(ctx context.Context, pageID uuid.UUID) error
	SetBlockOrders(ctx context.Context, arg SetBlockOrdersParams) ([]PageBlock, error)
}

// BlockStore is what *Store provides. Requiring the transaction capability here rather
// than type-asserting it at runtime makes a store without it a compile error.
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

// Block is the API-facing shape of a page_blocks row: the mutually exclusive
// content_id/question_id columns are collapsed into one resource reference.
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

// TODO(experiments): 開放 STUDENT 存取前需完成 experiments 的 current-experiment 邏輯
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

// Delete scopes the statement by page_id, so a block that belongs to another page is
// indistinguishable from a missing one without a second lookup — and without the race
// a separate existence check would leave open.
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

// Reorder replaces the display order of every block in a page in one transaction.
// The offset step and the write-back step must share a transaction: on its own, the
// offset leaves display_order values parked in the temporary range with no way back.
func (s *BlockService) Reorder(ctx context.Context, pageID uuid.UUID, blockIDs []uuid.UUID) ([]Block, error) {
	if err := s.ensurePageExists(ctx, pageID); err != nil {
		return nil, err
	}

	var rows []PageBlock
	// The requested set is validated against a read inside the transaction, so a
	// concurrent create/delete cannot slip between the check and the write.
	err := s.querier.WithinTx(ctx, func(_ PageQuerier, blockQuerier BlockQuerier) error {
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

		if err := blockQuerier.OffsetBlockOrders(ctx, pageID); err != nil {
			return databaseutil.WrapDBError(err, s.logger, "offset page block orders")
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
		return nil, err
	}

	return s.toBlocks(ctx, rows)
}

// validateResource checks that resourceID exists in the table implied by blockType.
// TEXT/MEDIA additionally have to match contents.type, which no FK can enforce:
// both map to content_id, and only the contents row itself tells them apart.
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

// toBlocks resolves TEXT vs MEDIA for content-backed rows, which requires the
// contents rows themselves; they are fetched in one batch rather than per block.
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

// isNotFound matches both shapes summer produces: the NotFoundError struct from
// WrapDBErrorWithKeyValue (its Is method reports ErrNotFound) and the bare
// ErrNotFound sentinel from WrapDBError.
func isNotFound(err error) bool {
	return errors.Is(err, handlerutil.ErrNotFound)
}
