package page

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"sciedu-backend/internal/auth"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	logutil "github.com/NYCU-SDC/summer/pkg/log"
	middlewareutil "github.com/NYCU-SDC/summer/pkg/middleware"
	problemutil "github.com/NYCU-SDC/summer/pkg/problem"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type PageHandlerService interface {
	ListByCourseForActor(ctx context.Context, actorID, courseID uuid.UUID) ([]Page, error)
	GetForActor(ctx context.Context, actorID, id uuid.UUID) (Detail, error)
	ListBlocksForActor(ctx context.Context, actorID, pageID uuid.UUID) ([]Block, error)
	Create(ctx context.Context, courseID uuid.UUID, req PageRequest) (Detail, error)
	Update(ctx context.Context, id uuid.UUID, req PageRequest) (Detail, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Reorder(ctx context.Context, courseID uuid.UUID, pageIDs []uuid.UUID) ([]Page, error)
}

type BlockHandlerService interface {
	Create(ctx context.Context, pageID uuid.UUID, req BlockRequest) (Block, error)
	Update(ctx context.Context, pageID, blockID uuid.UUID, req BlockRequest) (Block, error)
	Delete(ctx context.Context, pageID, blockID uuid.UUID) error
	Reorder(ctx context.Context, pageID uuid.UUID, blockIDs []uuid.UUID) ([]Block, error)
}

type Handler struct {
	pageService   PageHandlerService
	blockService  BlockHandlerService
	logger        *zap.Logger
	problemWriter *problemutil.HttpWriter
	validator     *validator.Validate
}

type createUpdatePageRequest struct {
	Title        string `json:"title" validate:"required,min=1,max=200"`
	DisplayOrder *int32 `json:"displayOrder" validate:"required,gte=0,lte=99999"`
}

type reorderPagesRequest struct {
	PageIDs []uuid.UUID `json:"pageIds" validate:"required,min=1,max=500,unique"`
}

type createUpdateBlockRequest struct {
	Type         string    `json:"type" validate:"required,oneof=TEXT MEDIA QUESTION"`
	ResourceID   uuid.UUID `json:"resourceId" validate:"required"`
	DisplayOrder *int32    `json:"displayOrder" validate:"required,gte=0,lte=99999"`
	Required     *bool     `json:"required" validate:"required"`
}

type reorderBlocksRequest struct {
	BlockIDs []uuid.UUID `json:"blockIds" validate:"required,min=1,max=1000,unique"`
}

type pageResponse struct {
	ID           uuid.UUID `json:"id"`
	CourseID     uuid.UUID `json:"courseId"`
	Title        string    `json:"title"`
	DisplayOrder int32     `json:"displayOrder"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type pageDetailResponse struct {
	pageResponse
	Blocks []blockResponse `json:"blocks"`
}

type blockResponse struct {
	ID           uuid.UUID `json:"id"`
	PageID       uuid.UUID `json:"pageId"`
	Type         string    `json:"type"`
	ResourceID   uuid.UUID `json:"resourceId"`
	DisplayOrder int32     `json:"displayOrder"`
	Required     bool      `json:"required"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

func NewHandler(pageService PageHandlerService, blockService BlockHandlerService, logger *zap.Logger) *Handler {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Handler{
		pageService:  pageService,
		blockService: blockService,
		logger:       logger,
		problemWriter: problemutil.NewWithMapping(func(err error) problemutil.Problem {
			var syntaxErr *json.SyntaxError
			var typeErr *json.UnmarshalTypeError
			switch {
			case errors.Is(err, errInvalidPagePayload), errors.Is(err, errInvalidBlockPayload):
				return problemutil.NewValidateProblem(err.Error())
			case errors.Is(err, io.EOF), errors.As(err, &syntaxErr), errors.As(err, &typeErr):
				return problemutil.NewValidateProblem("invalid JSON request body")
			case errors.Is(err, databaseutil.ErrUniqueViolation):
				// summer has no 409 problem and leaves unique violations unmapped.
				return problemutil.Problem{
					Title:  "Conflict",
					Status: http.StatusConflict,
					Type:   "https://developer.mozilla.org/en-US/docs/Web/HTTP/Status/409",
					Detail: "displayOrder is already taken",
				}
			case errors.Is(err, databaseutil.ErrForeignKeyViolation):
				// The resource validateResource confirmed can be deleted before the
				// INSERT lands; the FK rejects the write.
				return problemutil.Problem{
					Title:  "Conflict",
					Status: http.StatusConflict,
					Type:   "https://developer.mozilla.org/en-US/docs/Web/HTTP/Status/409",
					Detail: "referenced resource no longer exists",
				}
			default:
				return problemutil.Problem{}
			}
		}),
		validator: validator.New(),
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, middlewares *middlewareutil.Set, authorizer *auth.Authorizer) {
	handle := func(pattern string, set *middlewareutil.Set, fn http.HandlerFunc) {
		if set != nil {
			fn = set.HandlerFunc(fn)
		}
		mux.HandleFunc(pattern, fn)
	}

	readAccess := middlewares
	writeAccess := middlewares
	if middlewares != nil && authorizer != nil {
		writeAccess = middlewares.Append(authorizer.RequireAnyRole(auth.EXPERIMENTER, auth.ADMIN))
	}

	handle("GET /api/courses/{courseId}/pages", readAccess, h.ListPages)
	handle("POST /api/courses/{courseId}/pages", writeAccess, h.CreatePage)
	handle("PUT /api/courses/{courseId}/pages/order", writeAccess, h.ReorderPages)

	handle("GET /api/pages/{id}", readAccess, h.GetPage)
	handle("PUT /api/pages/{id}", writeAccess, h.UpdatePage)
	handle("DELETE /api/pages/{id}", writeAccess, h.DeletePage)

	handle("GET /api/pages/{id}/blocks", readAccess, h.ListBlocks)
	handle("POST /api/pages/{id}/blocks", writeAccess, h.CreateBlock)
	handle("PUT /api/pages/{id}/blocks/order", writeAccess, h.ReorderBlocks)
	handle("PUT /api/pages/{id}/blocks/{blockId}", writeAccess, h.UpdateBlock)
	handle("DELETE /api/pages/{id}/blocks/{blockId}", writeAccess, h.DeleteBlock)
}

func (h *Handler) ListPages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)
	actorID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		h.problemWriter.WriteError(ctx, w, handlerutil.ErrUnauthorized, logger)
		return
	}

	courseID, err := handlerutil.ParseUUID(r.PathValue("courseId"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	pages, err := h.pageService.ListByCourseForActor(ctx, actorID, courseID)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusOK, buildPageResponses(pages))
}

func (h *Handler) CreatePage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	courseID, err := handlerutil.ParseUUID(r.PathValue("courseId"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	var req createUpdatePageRequest
	if err := handlerutil.ParseAndValidateRequestBody(ctx, h.validator, r, &req); err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	detail, err := h.pageService.Create(ctx, courseID, PageRequest{
		Title:        req.Title,
		DisplayOrder: *req.DisplayOrder,
	})
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusCreated, buildPageDetailResponse(detail))
}

func (h *Handler) ReorderPages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	courseID, err := handlerutil.ParseUUID(r.PathValue("courseId"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	var req reorderPagesRequest
	if err := handlerutil.ParseAndValidateRequestBody(ctx, h.validator, r, &req); err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	pages, err := h.pageService.Reorder(ctx, courseID, req.PageIDs)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusOK, buildPageResponses(pages))
}

func (h *Handler) GetPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)
	actorID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		h.problemWriter.WriteError(ctx, w, handlerutil.ErrUnauthorized, logger)
		return
	}

	id, err := handlerutil.ParseUUID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	detail, err := h.pageService.GetForActor(ctx, actorID, id)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusOK, buildPageDetailResponse(detail))
}

func (h *Handler) UpdatePage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	id, err := handlerutil.ParseUUID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	var req createUpdatePageRequest
	if err := handlerutil.ParseAndValidateRequestBody(ctx, h.validator, r, &req); err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	detail, err := h.pageService.Update(ctx, id, PageRequest{
		Title:        req.Title,
		DisplayOrder: *req.DisplayOrder,
	})
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusOK, buildPageDetailResponse(detail))
}

func (h *Handler) DeletePage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	id, err := handlerutil.ParseUUID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	if err := h.pageService.Delete(ctx, id); err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListBlocks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)
	actorID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		h.problemWriter.WriteError(ctx, w, handlerutil.ErrUnauthorized, logger)
		return
	}

	pageID, err := handlerutil.ParseUUID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	blocks, err := h.pageService.ListBlocksForActor(ctx, actorID, pageID)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusOK, buildBlockResponses(blocks))
}

func (h *Handler) CreateBlock(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	pageID, err := handlerutil.ParseUUID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	req, err := h.parseBlockRequest(ctx, r)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	block, err := h.blockService.Create(ctx, pageID, req)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusCreated, buildBlockResponse(block))
}

func (h *Handler) UpdateBlock(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	pageID, blockID, err := parseBlockPath(r)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	req, err := h.parseBlockRequest(ctx, r)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	block, err := h.blockService.Update(ctx, pageID, blockID, req)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusOK, buildBlockResponse(block))
}

func (h *Handler) DeleteBlock(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	pageID, blockID, err := parseBlockPath(r)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	if err := h.blockService.Delete(ctx, pageID, blockID); err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ReorderBlocks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	pageID, err := handlerutil.ParseUUID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	var req reorderBlocksRequest
	if err := handlerutil.ParseAndValidateRequestBody(ctx, h.validator, r, &req); err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	blocks, err := h.blockService.Reorder(ctx, pageID, req.BlockIDs)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusOK, buildBlockResponses(blocks))
}

func parseBlockPath(r *http.Request) (uuid.UUID, uuid.UUID, error) {
	pageID, err := handlerutil.ParseUUID(r.PathValue("id"))
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	blockID, err := handlerutil.ParseUUID(r.PathValue("blockId"))
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	return pageID, blockID, nil
}

func (h *Handler) parseBlockRequest(ctx context.Context, r *http.Request) (BlockRequest, error) {
	var req createUpdateBlockRequest
	if err := handlerutil.ParseAndValidateRequestBody(ctx, h.validator, r, &req); err != nil {
		return BlockRequest{}, err
	}

	return BlockRequest{
		Type:         req.Type,
		ResourceID:   req.ResourceID,
		DisplayOrder: *req.DisplayOrder,
		Required:     *req.Required,
	}, nil
}

func buildPageResponse(p Page) pageResponse {
	resp := pageResponse{
		ID:           p.ID,
		CourseID:     p.CourseID,
		Title:        p.Title,
		DisplayOrder: p.DisplayOrder,
	}

	if p.CreatedAt.Valid {
		resp.CreatedAt = p.CreatedAt.Time
	}
	if p.UpdatedAt.Valid {
		resp.UpdatedAt = p.UpdatedAt.Time
	}

	return resp
}

func buildPageResponses(pages []Page) []pageResponse {
	resp := make([]pageResponse, 0, len(pages))
	for _, p := range pages {
		resp = append(resp, buildPageResponse(p))
	}
	return resp
}

func buildPageDetailResponse(detail Detail) pageDetailResponse {
	return pageDetailResponse{
		pageResponse: buildPageResponse(detail.Page),
		Blocks:       buildBlockResponses(detail.Blocks),
	}
}

func buildBlockResponse(b Block) blockResponse {
	resp := blockResponse{
		ID:           b.ID,
		PageID:       b.PageID,
		Type:         b.Type,
		ResourceID:   b.ResourceID,
		DisplayOrder: b.DisplayOrder,
		Required:     b.Required,
	}

	if b.CreatedAt.Valid {
		resp.CreatedAt = b.CreatedAt.Time
	}
	if b.UpdatedAt.Valid {
		resp.UpdatedAt = b.UpdatedAt.Time
	}

	return resp
}

func buildBlockResponses(blocks []Block) []blockResponse {
	resp := make([]blockResponse, 0, len(blocks))
	for _, b := range blocks {
		resp = append(resp, buildBlockResponse(b))
	}
	return resp
}
