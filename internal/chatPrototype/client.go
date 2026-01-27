package chatPrototype

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

type UpstreamClient interface {
	StreamChat(ctx context.Context) (<-chan string, <-chan error)
}

type HTTPSSEClient struct {
	logger  *zap.Logger
	client  *http.Client
	url     string
	headers http.Header
}

func NewHTTPSSEClient(logger *zap.Logger, client *http.Client, url string) *HTTPSSEClient {
	return &HTTPSSEClient{
		logger: logger,
		client: client,
		url:    url,
		headers: http.Header{
			"Accept": []string{"text/event-stream"},
		},
	}
}

func (c *HTTPSSEClient) StreamChat(ctx context.Context) (<-chan string, <-chan error) {
	contents := make(chan string)
	errs := make(chan error, 1)

	go func() {
		defer close(contents)
		defer close(errs)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
		if err != nil {
			errs <- err
			return
		}
		// optional headers
		for k, vs := range c.headers {
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}

		resp, err := c.client.Do(req)
		if err != nil {
			// includes ctx cancellation errors
			if ctx.Err() != nil {
				return
			}
			errs <- err
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			errs <- fmt.Errorf("upstream status %d", resp.StatusCode)
			return
		}

		reader := bufio.NewReader(resp.Body)

		for {
			select {
			case <-ctx.Done():
				return
			default:
				// proceed to read
			}

			line, err := reader.ReadString('\n')
			if err != nil {
				// If ctx cancelled, this is expected; otherwise report.
				if ctx.Err() != nil {
					return
				}
				// Could also be server closed normally; treat as end-of-stream.
				if errors.Is(err, context.Canceled) {
					return
				}
				errs <- err
				return
			}

			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				// blank line between SSE events
				continue
			}

			// Only handle "data:" lines in this minimal version.
			if !strings.HasPrefix(line, "data:") {
				continue
			}

			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" {
				continue
			}
			if data == "[DONE]" {
				return
			}

			// Immediately forward the content (key: no buffering).
			select {
			case <-ctx.Done():
				return
			case contents <- data:
			}
		}
	}()

	return contents, errs
}
