package question

import (
	"errors"
	"net/http"

	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	"github.com/jackc/pgx/v5"
	"sciedu-backend/internal/auth"
)

func (h *Handler) GetAnswerResult(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	actorID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		h.problemWriter.WriteError(ctx, w, handlerutil.ErrUnauthorized, h.logger)
		return
	}
	questionID, err := h.parseID(r.PathValue("questionId"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, h.logger)
		return
	}
	answerID, err := h.parseID(r.PathValue("answerId"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, h.logger)
		return
	}
	if h.resultService == nil || h.resultRoles == nil {
		h.problemWriter.WriteError(ctx, w, errors.New("answer result is not configured"), h.logger)
		return
	}
	roles, err := h.resultRoles.ActiveUserRoles(ctx, actorID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = handlerutil.ErrUnauthorized
	}
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, h.logger)
		return
	}
	var viewer ResultViewerKind
	for _, role := range roles {
		if role == auth.ADMIN || role == auth.EXPERIMENTER {
			viewer = ResultViewerManagement
			break
		}
		if role == auth.STUDENT {
			viewer = ResultViewerStudent
		}
	}
	if viewer == "" {
		h.problemWriter.WriteError(ctx, w, handlerutil.ErrForbidden, h.logger)
		return
	}
	result, err := h.resultService.Get(ctx, questionID, answerID, AnswerResultAccess{ViewerKind: viewer, ViewerID: actorID})
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, h.logger)
		return
	}
	handlerutil.WriteJSONResponse(w, http.StatusOK, buildAnswerResultResponse(result))
}
