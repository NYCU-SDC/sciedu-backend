package chat

import (
	"context"

	"go.uber.org/zap"
)

type ChatService struct {
	provider LLMProvider
	logger   *zap.Logger
}

func NewService(provider LLMProvider, logger *zap.Logger) *ChatService {
	return &ChatService{
		provider: provider,
		logger:   logger,
	}
}

func (s *ChatService) StreamChat(ctx context.Context, req CreateChatCompletionRequest) (<-chan ChatCompletionChunk, <-chan error) {
	outChunks := make(chan ChatCompletionChunk)
	outErrs := make(chan error, 1)

	req.Stream = true

	inChunks, inErrs := s.provider.StreamChat(ctx, req)

	go func() {
		defer close(outChunks)
		defer close(outErrs)

		for {
			select {
			case <-ctx.Done():
				return

			case err, ok := <-inErrs:
				if !ok {
					inErrs = nil
					continue
				}
				if err != nil {
					outErrs <- err
					return
				}

			case chunk, ok := <-inChunks:
				if !ok {
					return
				}

				select {
				case <-ctx.Done():
					return
				case outChunks <- chunk:
				}
			}
		}
	}()

	return outChunks, outErrs
}
