package respond

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/NYCU-SDC/summer/pkg/problem"
	"go.uber.org/zap"
)

var ErrValidation = errors.New("validation error")

func JSON(w http.ResponseWriter, status int, payload any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload == nil {
		return nil
	}
	return json.NewEncoder(w).Encode(payload)
}

func Error(ctx context.Context, w http.ResponseWriter, err error, logger *zap.Logger) {
	writer := problem.NewWithMapping(func(err error) problem.Problem {
		if errors.Is(err, ErrValidation) {
			return problem.NewValidateProblem(err.Error())
		}
		return problem.Problem{}
	})
	writer.WriteError(ctx, w, err, logger)
}

func DecodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	return nil
}
