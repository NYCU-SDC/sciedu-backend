package user

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"sciedu-backend/internal/auth"

	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	logutil "github.com/NYCU-SDC/summer/pkg/log"
	middlewareutil "github.com/NYCU-SDC/summer/pkg/middleware"
	problemutil "github.com/NYCU-SDC/summer/pkg/problem"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	defaultPage     = 1
	defaultPageSize = 20
	maxPageSize     = 100
	maxSearchLength = 200
)

type Handler struct {
	service       *Service
	logger        *zap.Logger
	problemWriter *problemutil.HttpWriter
	validator     *validator.Validate
}

type userResponse struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	AvatarURL *string   `json:"avatarUrl,omitempty"`
	Roles     []string  `json:"roles"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type paginatedUsersResponse struct {
	Items       []userResponse `json:"items"`
	TotalPages  int32          `json:"totalPages"`
	TotalItems  int32          `json:"totalItems"`
	CurrentPage int32          `json:"currentPage"`
	PageSize    int32          `json:"pageSize"`
	HasNextPage bool           `json:"hasNextPage"`
}

type updateRolesRequest struct {
	Roles []string `json:"roles" validate:"required,min=1,max=3,unique,dive,oneof=STUDENT EXPERIMENTER ADMIN"`
}

func NewHandler(service *Service, logger *zap.Logger) *Handler {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Handler{
		service: service,
		logger:  logger,
		problemWriter: problemutil.NewWithMapping(func(err error) problemutil.Problem {
			switch {
			case errors.Is(err, errSelfOperation):
				return problemutil.NewForbiddenProblem(err.Error())
			case errors.Is(err, errInvalidUserPayload):
				return problemutil.NewValidateProblem(err.Error())
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

	readAccess := middlewares.Append(authorizer.RequireAnyRole(auth.EXPERIMENTER, auth.ADMIN))
	adminOnly := middlewares.Append(authorizer.RequireAnyRole(auth.ADMIN))

	handle("GET /api/users/me", middlewares, h.Me)
	handle("GET /api/users", readAccess, h.List)
	handle("GET /api/users/{id}", readAccess, h.Get)
	handle("DELETE /api/users/{id}", adminOnly, h.Delete)
	handle("PUT /api/users/{id}/roles", adminOnly, h.UpdateRoles)
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		h.problemWriter.WriteError(ctx, w, handlerutil.ErrUnauthorized, logger)
		return
	}

	u, err := h.service.Get(ctx, userID)
	if err != nil {
		var notFoundErr handlerutil.NotFoundError
		if errors.As(err, &notFoundErr) {
			// A missing/disabled current user isn't a resource that's absent, it's a
			// caller who isn't a valid authenticated user anymore (e.g. their access
			// token was issued before an admin disabled them). GET /users/{id} keeps
			// 404 for this same underlying error; only the "self" lookup remaps it.
			err = handlerutil.ErrUnauthorized
		}
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusOK, buildUserResponse(u))
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	in, err := parseListInput(r)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	page, err := h.service.List(ctx, in)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	items := make([]userResponse, 0, len(page.Items))
	for _, u := range page.Items {
		items = append(items, buildUserResponse(u))
	}

	handlerutil.WriteJSONResponse(w, http.StatusOK, paginatedUsersResponse{
		Items:       items,
		TotalPages:  page.TotalPages,
		TotalItems:  page.TotalItems,
		CurrentPage: page.CurrentPage,
		PageSize:    page.PageSize,
		HasNextPage: page.HasNextPage,
	})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	id, err := handlerutil.ParseUUID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	u, err := h.service.Get(ctx, id)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusOK, buildUserResponse(u))
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	actorID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		h.problemWriter.WriteError(ctx, w, handlerutil.ErrUnauthorized, logger)
		return
	}

	targetID, err := handlerutil.ParseUUID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	if err := h.service.Delete(ctx, actorID, targetID); err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UpdateRoles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	actorID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		h.problemWriter.WriteError(ctx, w, handlerutil.ErrUnauthorized, logger)
		return
	}

	targetID, err := handlerutil.ParseUUID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	var req updateRolesRequest
	if err := handlerutil.ParseAndValidateRequestBody(ctx, h.validator, r, &req); err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	u, err := h.service.ReplaceRoles(ctx, actorID, targetID, req.Roles)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusOK, buildUserResponse(u))
}

func parseListInput(r *http.Request) (ListInput, error) {
	query := r.URL.Query()

	page, err := parseIntQuery(query.Get("page"), defaultPage)
	if err != nil || page < 1 {
		return ListInput{}, fmt.Errorf("%w: page must be a positive integer", errInvalidUserPayload)
	}

	pageSize, err := parseIntQuery(query.Get("pageSize"), defaultPageSize)
	if err != nil || pageSize < 1 || pageSize > maxPageSize {
		return ListInput{}, fmt.Errorf("%w: pageSize must be between 1 and %d", errInvalidUserPayload, maxPageSize)
	}

	var search *string
	if raw := strings.TrimSpace(query.Get("search")); raw != "" {
		if len(raw) > maxSearchLength {
			return ListInput{}, fmt.Errorf("%w: search must be at most %d characters", errInvalidUserPayload, maxSearchLength)
		}
		search = &raw
	}

	var role *string
	if raw := query.Get("role"); raw != "" {
		if !isKnownRole(raw) {
			return ListInput{}, fmt.Errorf("%w: unknown role filter", errInvalidUserPayload)
		}
		role = &raw
	}

	return ListInput{Page: page, PageSize: pageSize, Search: search, Role: role}, nil
}

func parseIntQuery(raw string, fallback int32) (int32, error) {
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	return int32(value), nil
}

func isKnownRole(role string) bool {
	switch auth.Role(role) {
	case auth.STUDENT, auth.EXPERIMENTER, auth.ADMIN:
		return true
	default:
		return false
	}
}

func buildUserResponse(u Profile) userResponse {
	return userResponse(u)
}
