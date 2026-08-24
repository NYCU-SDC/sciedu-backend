package page

import (
	"context"
	"errors"
	"testing"

	"sciedu-backend/internal/content"
	"sciedu-backend/internal/question"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func newBlockService(querier BlockStore, pages PageLookup, contents ContentLookup, questions QuestionLookup) *BlockService {
	return NewBlockService(querier, pages, contents, questions, zap.NewNop())
}

func TestBlockServiceCreateValidatesResource_TableDriven(t *testing.T) {
	pageID := uuid.New()
	resourceID := uuid.New()

	tests := []struct {
		name      string
		request   BlockRequest
		contents  *fakeContentLookup
		questions *fakeQuestionLookup
		wantErr   error
		wantCall  string
	}{
		{
			name:    "creates a TEXT block against a TEXT content",
			request: BlockRequest{Type: BlockTypeText, ResourceID: resourceID},
			contents: &fakeContentLookup{
				getContentFn: func(_ context.Context, id uuid.UUID) (content.Content, error) {
					return content.Content{ID: id, Type: BlockTypeText}, nil
				},
			},
			wantCall: "CreateContentBlock",
		},
		{
			name:    "creates a MEDIA block against a MEDIA content",
			request: BlockRequest{Type: BlockTypeMedia, ResourceID: resourceID},
			contents: &fakeContentLookup{
				getContentFn: func(_ context.Context, id uuid.UUID) (content.Content, error) {
					return content.Content{ID: id, Type: BlockTypeMedia}, nil
				},
			},
			wantCall: "CreateContentBlock",
		},
		{
			name:     "creates a QUESTION block against an existing question",
			request:  BlockRequest{Type: BlockTypeQuestion, ResourceID: resourceID},
			wantCall: "CreateQuestionBlock",
		},
		{
			name:    "rejects a TEXT block pointing at a MEDIA content",
			request: BlockRequest{Type: BlockTypeText, ResourceID: resourceID},
			contents: &fakeContentLookup{
				getContentFn: func(_ context.Context, id uuid.UUID) (content.Content, error) {
					return content.Content{ID: id, Type: BlockTypeMedia}, nil
				},
			},
			wantErr: errInvalidBlockPayload,
		},
		{
			name:    "rejects a content resourceId that does not exist",
			request: BlockRequest{Type: BlockTypeText, ResourceID: resourceID},
			contents: &fakeContentLookup{
				getContentFn: func(_ context.Context, id uuid.UUID) (content.Content, error) {
					return content.Content{}, notFoundErr("contents", id)
				},
			},
			wantErr: errInvalidBlockPayload,
		},
		{
			name:    "rejects a QUESTION resourceId that does not exist",
			request: BlockRequest{Type: BlockTypeQuestion, ResourceID: resourceID},
			questions: &fakeQuestionLookup{
				getFn: func(_ context.Context, id uuid.UUID) (question.Question, error) {
					return question.Question{}, notFoundErr("questions", id)
				},
			},
			wantErr: errInvalidBlockPayload,
		},
		{
			name:    "rejects an unknown block type",
			request: BlockRequest{Type: "AUDIO", ResourceID: resourceID},
			wantErr: errInvalidBlockPayload,
		},
		{
			name:     "accepts the maximum int32 display order",
			request:  BlockRequest{Type: BlockTypeText, ResourceID: resourceID, DisplayOrder: 2147483647},
			wantCall: "CreateContentBlock",
		},
		{
			name:    "rejects a negative display order",
			request: BlockRequest{Type: BlockTypeText, ResourceID: resourceID, DisplayOrder: -1},
			wantErr: errInvalidBlockPayload,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			querier := &fakeQuerier{}
			contents := tt.contents
			if contents == nil {
				contents = &fakeContentLookup{}
			}
			questions := tt.questions
			if questions == nil {
				questions = &fakeQuestionLookup{}
			}
			svc := newBlockService(querier, querier, contents, questions)

			block, err := svc.Create(context.Background(), pageID, tt.request)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
				if len(querier.calls) != 0 {
					t.Fatalf("expected no write calls on a rejected request, got %v", querier.calls)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if block.Type != tt.request.Type {
				t.Fatalf("expected block type %q, got %q", tt.request.Type, block.Type)
			}
			if block.ResourceID != tt.request.ResourceID {
				t.Fatalf("expected resource id %s, got %s", tt.request.ResourceID, block.ResourceID)
			}
			if block.DisplayOrder != tt.request.DisplayOrder {
				t.Fatalf("expected display order %d, got %d", tt.request.DisplayOrder, block.DisplayOrder)
			}
			if len(querier.calls) != 1 || querier.calls[0] != tt.wantCall {
				t.Fatalf("expected a single %s call, got %v", tt.wantCall, querier.calls)
			}
		})
	}
}

func TestBlockServiceListResolvesTypes(t *testing.T) {
	pageID := uuid.New()
	textID, mediaID, questionID := uuid.New(), uuid.New(), uuid.New()

	querier := &fakeQuerier{
		listBlocksByPageFn: func(context.Context, uuid.UUID) ([]PageBlock, error) {
			return []PageBlock{
				{ID: uuid.New(), PageID: pageID, ContentID: toPgUUID(textID), DisplayOrder: 0},
				{ID: uuid.New(), PageID: pageID, QuestionID: toPgUUID(questionID), DisplayOrder: 1},
				{ID: uuid.New(), PageID: pageID, ContentID: toPgUUID(mediaID), DisplayOrder: 2},
			}, nil
		},
	}
	contents := &fakeContentLookup{
		batchGetContentsFn: func(_ context.Context, ids []uuid.UUID) ([]content.Content, error) {
			items := make([]content.Content, 0, len(ids))
			for _, id := range ids {
				blockType := BlockTypeText
				if id == mediaID {
					blockType = BlockTypeMedia
				}
				items = append(items, content.Content{ID: id, Type: blockType})
			}
			return items, nil
		},
	}
	svc := newBlockService(querier, querier, contents, &fakeQuestionLookup{})

	blocks, err := svc.ListByPage(context.Background(), pageID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantTypes := []string{BlockTypeText, BlockTypeQuestion, BlockTypeMedia}
	wantResources := []uuid.UUID{textID, questionID, mediaID}
	for i, want := range wantTypes {
		if blocks[i].Type != want {
			t.Fatalf("expected block %d to be %s, got %s", i, want, blocks[i].Type)
		}
		if blocks[i].ResourceID != wantResources[i] {
			t.Fatalf("expected block %d resource %s, got %s", i, wantResources[i], blocks[i].ResourceID)
		}
	}

	// Content types must be resolved in one batch, not one query per block.
	if contents.batchGetCallCount != 1 {
		t.Fatalf("expected exactly 1 batch lookup, got %d", contents.batchGetCallCount)
	}
	if len(contents.batchGetContentsIDs[0]) != 2 {
		t.Fatalf("expected only content-backed ids to be looked up, got %v", contents.batchGetContentsIDs[0])
	}
}

func TestBlockServiceUpdateRejectsForeignPage(t *testing.T) {
	pageID, otherPageID, blockID := uuid.New(), uuid.New(), uuid.New()

	querier := &fakeQuerier{
		getBlockByIDFn: func(_ context.Context, id uuid.UUID) (PageBlock, error) {
			return PageBlock{ID: id, PageID: otherPageID}, nil
		},
	}
	svc := newBlockService(querier, querier, &fakeContentLookup{}, &fakeQuestionLookup{})

	_, err := svc.Update(context.Background(), pageID, blockID, BlockRequest{Type: BlockTypeText, ResourceID: uuid.New()})

	if !isNotFoundError(err) {
		t.Fatalf("expected a not found error for a block on another page, got %v", err)
	}
	if len(querier.calls) != 0 {
		t.Fatalf("expected no write calls, got %v", querier.calls)
	}
}

// Delete is scoped by page_id in SQL, so a block on another page simply matches
// nothing: no separate ownership lookup, and no window between the two.
func TestBlockServiceDeleteScopesByPage(t *testing.T) {
	pageID, blockID := uuid.New(), uuid.New()

	var got DeleteBlockParams
	querier := &fakeQuerier{
		deleteBlockFn: func(_ context.Context, arg DeleteBlockParams) (int64, error) {
			got = arg
			return 0, nil
		},
	}
	svc := newBlockService(querier, querier, &fakeContentLookup{}, &fakeQuestionLookup{})

	err := svc.Delete(context.Background(), pageID, blockID)

	if !isNotFoundError(err) {
		t.Fatalf("expected a not found error, got %v", err)
	}
	if got.PageID != pageID || got.ID != blockID {
		t.Fatalf("expected the delete to be scoped to page %s block %s, got %+v", pageID, blockID, got)
	}
	wantCalls := []string{"DeleteBlock"}
	if len(querier.calls) != len(wantCalls) || querier.calls[0] != wantCalls[0] {
		t.Fatalf("expected calls %v, got %v", wantCalls, querier.calls)
	}
}

func TestBlockServiceReorder_TableDriven(t *testing.T) {
	pageID := uuid.New()
	first, second := uuid.New(), uuid.New()
	contentID := uuid.New()

	existing := []PageBlock{
		{ID: first, PageID: pageID, ContentID: toPgUUID(contentID), DisplayOrder: 0},
		{ID: second, PageID: pageID, ContentID: toPgUUID(contentID), DisplayOrder: 1},
	}

	tests := []struct {
		name      string
		requested []uuid.UUID
		wantErr   error
		wantCalls []string
	}{
		{
			name:      "reorders every block",
			requested: []uuid.UUID{second, first},
			wantCalls: []string{"DeferOrderConstraints", "SetBlockOrders"},
		},
		{
			name:      "rejects a mismatched set without writing",
			requested: []uuid.UUID{first, uuid.New()},
			wantErr:   errInvalidBlockPayload,
			wantCalls: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			querier := &fakeQuerier{
				listBlocksByPageFn: func(context.Context, uuid.UUID) ([]PageBlock, error) {
					return existing, nil
				},
				setBlockOrdersFn: func(_ context.Context, arg SetBlockOrdersParams) ([]PageBlock, error) {
					rows := make([]PageBlock, 0, len(arg.BlockIds))
					for i, id := range arg.BlockIds {
						rows = append(rows, PageBlock{ID: id, PageID: pageID, ContentID: toPgUUID(contentID), DisplayOrder: int32(i)})
					}
					return rows, nil
				},
			}
			svc := newBlockService(querier, querier, &fakeContentLookup{}, &fakeQuestionLookup{})

			blocks, err := svc.Reorder(context.Background(), pageID, tt.requested)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				for i, want := range tt.requested {
					if blocks[i].ID != want {
						t.Fatalf("expected block order %v, got %s at index %d", tt.requested, blocks[i].ID, i)
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
