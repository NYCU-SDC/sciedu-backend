package progress

import "go.uber.org/zap"

type Service struct {
	logger *zap.Logger
}

func NewService(logger *zap.Logger) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Service{logger: logger}
}
