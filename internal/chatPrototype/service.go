package chatPrototype

import (
	"context"

	"go.uber.org/zap"
)

type Service struct {
	logger   *zap.Logger
	upstream UpstreamClient
}

func NewService(logger *zap.Logger, upstream UpstreamClient) *Service {
	return &Service{
		logger:   logger,
		upstream: upstream,
	}
}

func (s *Service) StreamChat(ctx context.Context) (<-chan string, <-chan error) {
	return s.upstream.StreamChat(ctx)
}
