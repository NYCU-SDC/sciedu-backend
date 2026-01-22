package healthz

import (
	"net/http"

	"go.uber.org/zap"
)

//go:generate mockery --name=Store
type Store interface {
	Healthz() (bool, error)
}

type Handler struct {
	logger *zap.Logger
	store  Store
}

func NewHandler(logger *zap.Logger, store Store) Handler {
	return Handler{
		logger: logger,
		store:  store,
	}
}

func (h Handler) Healthz(w http.ResponseWriter, r *http.Request) {
	healthz, err := h.store.Healthz()
	if err != nil || !healthz {
		h.logger.Error("healthz check error", zap.Error(err))
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
}
