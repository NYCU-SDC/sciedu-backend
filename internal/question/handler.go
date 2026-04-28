package question

import (
	"errors"
	"net/http"

	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	"github.com/NYCU-SDC/summer/pkg/problem"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"sciedu-backend/internal/respond"
)

type Handler struct {
	service *Service
	logger  *zap.Logger
}

func NewHandler(service *Service, logger *zap.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/questions", h.list)
	mux.HandleFunc("POST /api/questions", h.create)
	mux.HandleFunc("GET /api/questions/{id}", h.get)
	mux.HandleFunc("PUT /api/questions/{id}", h.update)
	mux.HandleFunc("DELETE /api/questions/{id}", h.delete)
	mux.HandleFunc("GET /api/questions/{id}/answers", h.listAnswers)
	mux.HandleFunc("POST /api/questions/{id}/answers", h.submitAnswer)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	questions, err := h.service.List(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.writeJSON(w, http.StatusOK, questions)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := h.parseID(w, r)
	if !ok {
		return
	}
	q, err := h.service.Get(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.writeJSON(w, http.StatusOK, q)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req UpsertRequest
	if err := respond.DecodeJSON(r, &req); err != nil {
		h.writeError(w, r, err)
		return
	}
	q, err := h.service.Create(r.Context(), req)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.writeJSON(w, http.StatusCreated, q)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := h.parseID(w, r)
	if !ok {
		return
	}
	var req UpsertRequest
	if err := respond.DecodeJSON(r, &req); err != nil {
		h.writeError(w, r, err)
		return
	}
	q, err := h.service.Update(r.Context(), id, req)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.writeJSON(w, http.StatusOK, q)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := h.parseID(w, r)
	if !ok {
		return
	}
	if err := h.service.Delete(r.Context(), id); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listAnswers(w http.ResponseWriter, r *http.Request) {
	id, ok := h.parseID(w, r)
	if !ok {
		return
	}
	answers, err := h.service.ListAnswers(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.writeJSON(w, http.StatusOK, answers)
}

func (h *Handler) submitAnswer(w http.ResponseWriter, r *http.Request) {
	id, ok := h.parseID(w, r)
	if !ok {
		return
	}
	var req SubmitAnswerRequest
	if err := respond.DecodeJSON(r, &req); err != nil {
		h.writeError(w, r, err)
		return
	}
	answer, err := h.service.SubmitAnswer(r.Context(), id, req)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.writeJSON(w, http.StatusCreated, answer)
}

func (h *Handler) parseID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, handlerutil.ErrInvalidUUID)
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	writer := problem.NewWithMapping(func(err error) problem.Problem {
		if errors.Is(err, ErrInvalidQuestion) {
			return problem.NewValidateProblem(err.Error())
		}
		return problem.Problem{}
	})
	writer.WriteError(r.Context(), w, err, h.logger)
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, payload any) {
	if err := respond.JSON(w, status, payload); err != nil {
		h.logger.Error("failed to write response", zap.Error(err))
	}
}
