package content

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	logutil "github.com/NYCU-SDC/summer/pkg/log"
	middlewareutil "github.com/NYCU-SDC/summer/pkg/middleware"
	problemutil "github.com/NYCU-SDC/summer/pkg/problem"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const defaultUploadDir = "contents"
const defaultMaxMediaUploadBytes = 200 << 20
const maxMultipartFormOverheadBytes = 1 << 20

var maxMediaUploadBytes int64 = defaultMaxMediaUploadBytes

type Handler struct {
	service       HandlerService
	logger        *zap.Logger
	problemWriter *problemutil.HttpWriter
	validator     *validator.Validate
}

type HandlerService interface {
	CreateMediaContent(ctx context.Context, upload MediaUploadRequest) (Content, error)
	GetMediaContent(ctx context.Context, id uuid.UUID) (Content, error)
	ListTextContents(ctx context.Context, page, pageSize int32) (TextPage, error)
	CreateTextContent(ctx context.Context, content string) (Content, error)
	BatchGetTextContents(ctx context.Context, ids []uuid.UUID) ([]Content, error)
	GetTextContent(ctx context.Context, id uuid.UUID) (Content, error)
	GetContent(ctx context.Context, id uuid.UUID) (Content, error)
	DeleteContent(ctx context.Context, id uuid.UUID) error
}

type createTextRequest struct {
	Content string `json:"content" validate:"required,min=1"`
}

type batchTextRequest struct {
	IDs []uuid.UUID `json:"ids" validate:"required,min=1,max=100,dive,required"`
}

type contentResponse struct {
	ID      uuid.UUID `json:"id"`
	Type    string    `json:"type"`
	Content string    `json:"content"`
}

type paginatedTextResponse struct {
	Items       []contentResponse `json:"items"`
	TotalPages  int32             `json:"totalPages"`
	TotalItems  int32             `json:"totalItems"`
	CurrentPage int32             `json:"currentPage"`
	PageSize    int32             `json:"pageSize"`
	HasNextPage bool              `json:"hasNextPage"`
}

func NewHandler(service HandlerService, logger *zap.Logger) *Handler {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Handler{
		service: service,
		logger:  logger,
		problemWriter: problemutil.NewWithMapping(func(err error) problemutil.Problem {
			if errors.Is(err, errMediaContentTooLarge) {
				return problemutil.Problem{
					Title:  "Payload Too Large",
					Status: http.StatusRequestEntityTooLarge,
					Type:   "https://developer.mozilla.org/en-US/docs/Web/HTTP/Status/413",
					Detail: err.Error(),
				}
			}
			if errors.Is(err, errInvalidContentPayload) {
				return problemutil.NewValidateProblem(err.Error())
			}
			return problemutil.Problem{}
		}),
		validator: validator.New(),
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, middlewares *middlewareutil.Set) {
	handle := func(pattern string, fn http.HandlerFunc) {
		if middlewares != nil {
			fn = middlewares.HandlerFunc(fn)
		}
		mux.HandleFunc(pattern, fn)
	}

	handle("POST /api/content/media", h.CreateMedia)
	handle("GET /api/content/media/{id}", h.StreamMedia)
	handle("GET /api/content/text", h.ListText)
	handle("POST /api/content/text", h.CreateText)
	handle("POST /api/content/text/batch", h.BatchGetText)
	handle("GET /api/content/text/{id}", h.GetText)
	handle("DELETE /api/content/{id}", h.Delete)
}

func (h *Handler) CreateMedia(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	r.Body = http.MaxBytesReader(w, r.Body, maxMediaUploadBytes+maxMultipartFormOverheadBytes)
	file, header, err := r.FormFile("content")
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			h.problemWriter.WriteError(ctx, w, fmt.Errorf("%w: maximum upload size is %d bytes",
				errMediaContentTooLarge, maxMediaUploadBytes), logger)
			return
		}
		h.problemWriter.WriteError(ctx, w, fmt.Errorf("%w: missing multipart field 'content'",
			errInvalidContentPayload), logger)
		return
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			logger.Warn("failed to close uploaded multipart file", zap.Error(cerr))
		}
	}()

	ext := sanitizeExt(filepath.Ext(header.Filename))
	generatedFilename := uuid.NewString() + ext
	item, err := h.service.CreateMediaContent(ctx, MediaUploadRequest{
		Content:  file,
		Filename: generatedFilename,
		Dir:      defaultUploadDir,
		MaxBytes: maxMediaUploadBytes,
	})
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusCreated, toContentResponse(item))
}

func (h *Handler) StreamMedia(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	id, err := h.parseID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	item, err := h.service.GetMediaContent(ctx, id)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	path := item.Content
	f, err := os.Open(path)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			logger.Warn("failed to close media file", zap.String("path", path), zap.Error(cerr))
		}
	}()

	info, err := f.Stat()
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	filename := filepath.Base(path)
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, f); err != nil {
		logger.Warn("failed to stream media", zap.Error(err))
	}
}

func (h *Handler) ListText(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	page, pageSize, err := parsePaginationParams(r)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	result, err := h.service.ListTextContents(ctx, page, pageSize)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	resp := paginatedTextResponse{
		Items:       make([]contentResponse, 0, len(result.Items)),
		TotalPages:  result.TotalPages,
		TotalItems:  result.TotalItems,
		CurrentPage: result.CurrentPage,
		PageSize:    result.PageSize,
		HasNextPage: result.HasNextPage,
	}

	for _, it := range result.Items {
		resp.Items = append(resp.Items, toContentResponse(it))
	}

	handlerutil.WriteJSONResponse(w, http.StatusOK, resp)
}

func (h *Handler) CreateText(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	var req createTextRequest
	if err := handlerutil.ParseAndValidateRequestBody(ctx, h.validator, r, &req); err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	item, err := h.service.CreateTextContent(ctx, req.Content)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusCreated, toContentResponse(item))
}

func (h *Handler) BatchGetText(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	var req batchTextRequest
	if err := handlerutil.ParseAndValidateRequestBody(ctx, h.validator, r, &req); err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	items, err := h.service.BatchGetTextContents(ctx, req.IDs)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	resp := make([]contentResponse, 0, len(items))
	for _, it := range items {
		resp = append(resp, toContentResponse(it))
	}

	handlerutil.WriteJSONResponse(w, http.StatusOK, resp)
}

func (h *Handler) GetText(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	id, err := h.parseID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	item, err := h.service.GetTextContent(ctx, id)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusOK, toContentResponse(item))
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	id, err := h.parseID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	item, err := h.service.GetContent(ctx, id)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	if err := h.service.DeleteContent(ctx, id); err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	if enumToString(item.Type) == "MEDIA" {
		if rmErr := os.Remove(item.Content); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			logger.Warn("failed to remove media file after deleting content", zap.String("path", item.Content), zap.Error(rmErr))
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

func parsePaginationParams(r *http.Request) (int32, int32, error) {
	var page, pageSize int32

	pageRaw := r.URL.Query().Get("page")
	if pageRaw != "" {
		parsed, parseErr := strconv.ParseInt(pageRaw, 10, 32)
		if parseErr != nil {
			return 0, 0, fmt.Errorf("%w: invalid page query", errInvalidContentPayload)
		}
		page = int32(parsed)
	}

	pageSizeRaw := r.URL.Query().Get("pageSize")
	if pageSizeRaw != "" {
		parsed, parseErr := strconv.ParseInt(pageSizeRaw, 10, 32)
		if parseErr != nil {
			return 0, 0, fmt.Errorf("%w: invalid pageSize query", errInvalidContentPayload)
		}
		pageSize = int32(parsed)
	}

	if pageRaw == "" {
		page = defaultPage
	}
	if pageSizeRaw == "" {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	if page < 1 || pageSize < 1 {
		return 0, 0, fmt.Errorf("%w: page and pageSize must be positive integers", errInvalidContentPayload)
	}

	return page, pageSize, nil
}

func toContentResponse(c Content) contentResponse {
	return contentResponse{
		ID:      c.ID,
		Type:    enumToString(c.Type),
		Content: c.Content,
	}
}

func enumToString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return fmt.Sprint(v)
	}
}

func (h *Handler) parseID(raw string) (uuid.UUID, error) {
	return handlerutil.ParseUUID(raw)
}
