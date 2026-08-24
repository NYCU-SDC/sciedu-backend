package page

import (
	"context"
	"errors"

	"sciedu-backend/internal/content"
	"sciedu-backend/internal/course"
	"sciedu-backend/internal/question"

	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// fakeQuerier implements PageQuerier, BlockQuerier and Transactor so a single
// value can stand in for *Store in service tests.
type fakeQuerier struct {
	listPagesByCourseFn  func(ctx context.Context, courseID uuid.UUID) ([]Page, error)
	getPageByIDFn        func(ctx context.Context, id uuid.UUID) (Page, error)
	createPageFn         func(ctx context.Context, arg CreatePageParams) (Page, error)
	updatePageFn         func(ctx context.Context, arg UpdatePageParams) (Page, error)
	deletePageFn         func(ctx context.Context, id uuid.UUID) (int64, error)
	offsetPageOrdersFn   func(ctx context.Context, courseID uuid.UUID) error
	setPageOrdersFn      func(ctx context.Context, arg SetPageOrdersParams) ([]Page, error)
	listBlocksByPageFn   func(ctx context.Context, pageID uuid.UUID) ([]PageBlock, error)
	getBlockByIDFn       func(ctx context.Context, id uuid.UUID) (PageBlock, error)
	createContentBlockFn func(ctx context.Context, arg CreateContentBlockParams) (PageBlock, error)
	createQuestionBlkFn  func(ctx context.Context, arg CreateQuestionBlockParams) (PageBlock, error)
	updateContentBlockFn func(ctx context.Context, arg UpdateContentBlockParams) (PageBlock, error)
	updateQuestionBlkFn  func(ctx context.Context, arg UpdateQuestionBlockParams) (PageBlock, error)
	deleteBlockFn        func(ctx context.Context, arg DeleteBlockParams) (int64, error)
	offsetBlockOrdersFn  func(ctx context.Context, pageID uuid.UUID) error
	setBlockOrdersFn     func(ctx context.Context, arg SetBlockOrdersParams) ([]PageBlock, error)

	// calls records write operations in order, so tests can assert that offset ran
	// before write-back and that neither ran when validation rejected the request.
	calls []string
}

func (f *fakeQuerier) ListPagesByCourse(ctx context.Context, courseID uuid.UUID) ([]Page, error) {
	if f.listPagesByCourseFn != nil {
		return f.listPagesByCourseFn(ctx, courseID)
	}
	return nil, nil
}

func (f *fakeQuerier) GetPageByID(ctx context.Context, id uuid.UUID) (Page, error) {
	if f.getPageByIDFn != nil {
		return f.getPageByIDFn(ctx, id)
	}
	return Page{ID: id}, nil
}

func (f *fakeQuerier) CreatePage(ctx context.Context, arg CreatePageParams) (Page, error) {
	f.calls = append(f.calls, "CreatePage")
	if f.createPageFn != nil {
		return f.createPageFn(ctx, arg)
	}
	return Page{ID: uuid.New(), CourseID: arg.CourseID, Title: arg.Title, DisplayOrder: arg.DisplayOrder}, nil
}

func (f *fakeQuerier) UpdatePage(ctx context.Context, arg UpdatePageParams) (Page, error) {
	f.calls = append(f.calls, "UpdatePage")
	if f.updatePageFn != nil {
		return f.updatePageFn(ctx, arg)
	}
	return Page{ID: arg.ID, Title: arg.Title, DisplayOrder: arg.DisplayOrder}, nil
}

func (f *fakeQuerier) DeletePage(ctx context.Context, id uuid.UUID) (int64, error) {
	f.calls = append(f.calls, "DeletePage")
	if f.deletePageFn != nil {
		return f.deletePageFn(ctx, id)
	}
	return 1, nil
}

func (f *fakeQuerier) OffsetPageOrders(ctx context.Context, courseID uuid.UUID) error {
	f.calls = append(f.calls, "OffsetPageOrders")
	if f.offsetPageOrdersFn != nil {
		return f.offsetPageOrdersFn(ctx, courseID)
	}
	return nil
}

func (f *fakeQuerier) SetPageOrders(ctx context.Context, arg SetPageOrdersParams) ([]Page, error) {
	f.calls = append(f.calls, "SetPageOrders")
	if f.setPageOrdersFn != nil {
		return f.setPageOrdersFn(ctx, arg)
	}
	return nil, nil
}

func (f *fakeQuerier) ListBlocksByPage(ctx context.Context, pageID uuid.UUID) ([]PageBlock, error) {
	if f.listBlocksByPageFn != nil {
		return f.listBlocksByPageFn(ctx, pageID)
	}
	return nil, nil
}

func (f *fakeQuerier) GetBlockByID(ctx context.Context, id uuid.UUID) (PageBlock, error) {
	if f.getBlockByIDFn != nil {
		return f.getBlockByIDFn(ctx, id)
	}
	return PageBlock{ID: id}, nil
}

func (f *fakeQuerier) CreateContentBlock(ctx context.Context, arg CreateContentBlockParams) (PageBlock, error) {
	f.calls = append(f.calls, "CreateContentBlock")
	if f.createContentBlockFn != nil {
		return f.createContentBlockFn(ctx, arg)
	}
	return PageBlock{ID: uuid.New(), PageID: arg.PageID, ContentID: arg.ContentID, DisplayOrder: arg.DisplayOrder, Required: arg.Required}, nil
}

func (f *fakeQuerier) CreateQuestionBlock(ctx context.Context, arg CreateQuestionBlockParams) (PageBlock, error) {
	f.calls = append(f.calls, "CreateQuestionBlock")
	if f.createQuestionBlkFn != nil {
		return f.createQuestionBlkFn(ctx, arg)
	}
	return PageBlock{ID: uuid.New(), PageID: arg.PageID, QuestionID: arg.QuestionID, DisplayOrder: arg.DisplayOrder, Required: arg.Required}, nil
}

func (f *fakeQuerier) UpdateContentBlock(ctx context.Context, arg UpdateContentBlockParams) (PageBlock, error) {
	f.calls = append(f.calls, "UpdateContentBlock")
	if f.updateContentBlockFn != nil {
		return f.updateContentBlockFn(ctx, arg)
	}
	return PageBlock{ID: arg.ID, ContentID: arg.ContentID, DisplayOrder: arg.DisplayOrder, Required: arg.Required}, nil
}

func (f *fakeQuerier) UpdateQuestionBlock(ctx context.Context, arg UpdateQuestionBlockParams) (PageBlock, error) {
	f.calls = append(f.calls, "UpdateQuestionBlock")
	if f.updateQuestionBlkFn != nil {
		return f.updateQuestionBlkFn(ctx, arg)
	}
	return PageBlock{ID: arg.ID, QuestionID: arg.QuestionID, DisplayOrder: arg.DisplayOrder, Required: arg.Required}, nil
}

func (f *fakeQuerier) DeleteBlock(ctx context.Context, arg DeleteBlockParams) (int64, error) {
	f.calls = append(f.calls, "DeleteBlock")
	if f.deleteBlockFn != nil {
		return f.deleteBlockFn(ctx, arg)
	}
	return 1, nil
}

func (f *fakeQuerier) OffsetBlockOrders(ctx context.Context, pageID uuid.UUID) error {
	f.calls = append(f.calls, "OffsetBlockOrders")
	if f.offsetBlockOrdersFn != nil {
		return f.offsetBlockOrdersFn(ctx, pageID)
	}
	return nil
}

func (f *fakeQuerier) SetBlockOrders(ctx context.Context, arg SetBlockOrdersParams) ([]PageBlock, error) {
	f.calls = append(f.calls, "SetBlockOrders")
	if f.setBlockOrdersFn != nil {
		return f.setBlockOrdersFn(ctx, arg)
	}
	return nil, nil
}

func (f *fakeQuerier) WithinTx(ctx context.Context, fn func(PageQuerier, BlockQuerier) error) error {
	return fn(f, f)
}

type fakeCourseLookup struct {
	getCourseByIDFn func(ctx context.Context, id uuid.UUID) (course.Course, error)
}

func (f *fakeCourseLookup) GetCourseByID(ctx context.Context, id uuid.UUID) (course.Course, error) {
	if f.getCourseByIDFn != nil {
		return f.getCourseByIDFn(ctx, id)
	}
	return course.Course{ID: id}, nil
}

type fakeContentLookup struct {
	getContentFn        func(ctx context.Context, id uuid.UUID) (content.Content, error)
	batchGetContentsFn  func(ctx context.Context, ids []uuid.UUID) ([]content.Content, error)
	batchGetContentsIDs [][]uuid.UUID
	batchGetCallCount   int
}

func (f *fakeContentLookup) GetContent(ctx context.Context, id uuid.UUID) (content.Content, error) {
	if f.getContentFn != nil {
		return f.getContentFn(ctx, id)
	}
	return content.Content{ID: id, Type: BlockTypeText}, nil
}

func (f *fakeContentLookup) BatchGetContents(ctx context.Context, ids []uuid.UUID) ([]content.Content, error) {
	f.batchGetCallCount++
	f.batchGetContentsIDs = append(f.batchGetContentsIDs, ids)
	if f.batchGetContentsFn != nil {
		return f.batchGetContentsFn(ctx, ids)
	}
	items := make([]content.Content, 0, len(ids))
	for _, id := range ids {
		items = append(items, content.Content{ID: id, Type: BlockTypeText})
	}
	return items, nil
}

type fakeQuestionLookup struct {
	getFn func(ctx context.Context, id uuid.UUID) (question.Question, error)
}

func (f *fakeQuestionLookup) Get(ctx context.Context, id uuid.UUID) (question.Question, error) {
	if f.getFn != nil {
		return f.getFn(ctx, id)
	}
	return question.Question{ID: id}, nil
}

func notFoundErr(table string, id uuid.UUID) error {
	return handlerutil.NewNotFoundError(table, "id", id.String(), "")
}

func noRowsErr() error {
	return pgx.ErrNoRows
}

func isNotFoundError(err error) bool {
	return errors.Is(err, handlerutil.ErrNotFound)
}
