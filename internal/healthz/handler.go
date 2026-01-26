package healthz

import (
	"net/http"

	problemutil "github.com/NYCU-SDC/summer/pkg/problem"
	"go.uber.org/zap"
)

//go:generate mockery --name=Store
type Store interface {
	Healthz() (bool, error)
}

type Handler struct {
	logger        *zap.Logger
	problemWriter *problemutil.HttpWriter
	store         Store
}

func NewHandler(logger *zap.Logger, problemWriter *problemutil.HttpWriter, store Store) Handler {
	return Handler{
		logger:        logger,
		problemWriter: problemWriter,
		store:         store,
	}
}

func (h Handler) Healthz(w http.ResponseWriter, r *http.Request) {
	healthz, err := h.store.Healthz()
	if err != nil || !healthz {
		h.logger.Error("healthz check error", zap.Error(err))
		w.WriteHeader(http.StatusServiceUnavailable)
		h.problemWriter.WriteError(nil, w, err, h.logger)
		return
	}

	w.WriteHeader(http.StatusOK)
}
