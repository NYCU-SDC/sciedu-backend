package healthz

import "go.uber.org/zap"

type Service struct {
	logger *zap.Logger
}

func NewService(logger *zap.Logger) Service {
	return Service{
		logger: logger,
	}
}

func (s Service) Healthz() (bool, error) {
	return true, nil
}
