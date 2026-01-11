package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type LLMProvider interface {
	StreamChat(ctx context.Context) (<-chan string, <-chan error)
}

type Provider struct {
	endpoint string
	client   *http.Client
	headers  http.Header
}

func NewProvider(endpoint string, client *http.Client, headers http.Header) *Provider {
	if client == nil {
		client = &http.Client{}
	}
	h := make(http.Header)
	for k, vs := range headers {
		for _, v := range vs {
			h.Add(k, v)
		}
	}
	return &Provider{
		endpoint: endpoint,
		client:   client,
		headers:  h,
	}
}

func parseSSEEventData(payload string) (ChatCompletionChunk, bool, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return ChatCompletionChunk{}, false, nil
	}

	if payload == "[DONE]" {
		return ChatCompletionChunk{Delta: "", IsFinished: true}, true, nil
	}

	// JSON payload support
	if strings.HasPrefix(payload, "{") {
		var c ChatCompletionChunk
		if err := json.Unmarshal([]byte(payload), &c); err != nil {
			return ChatCompletionChunk{}, false, fmt.Errorf("invalid json chunk payload: %w", err)
		}
		return c, c.IsFinished, nil
	}

	// Plain text Delta
	return ChatCompletionChunk{Delta: payload, IsFinished: false}, false, nil
}

func readSSEEventFromLines(lines []string) string {
	var dataLines []string
	for _, ln := range lines {
		ln = strings.TrimRight(ln, "\r\n")
		if ln == "" {
			continue
		}
		if strings.HasPrefix(ln, ":") {
			continue
		}
		if strings.HasPrefix(ln, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(ln, "data:")))
		}
	}
	return strings.TrimSpace(strings.Join(dataLines, "\n"))
}

func (p *Provider) StreamChat(ctx context.Context, req CreateChatCompletionRequest) (<-chan ChatCompletionChunk, <-chan error) {
	chunks := make(chan ChatCompletionChunk)
	errs := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errs)

		// enforce Stream true at adapter boundary
		req.Stream = true

		body, err := json.Marshal(req)
		if err != nil {
			errs <- err
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
		if err != nil {
			errs <- err
			return
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-Stream")
		for k, vs := range p.headers {
			for _, v := range vs {
				httpReq.Header.Add(k, v)
			}
		}

		resp, err := p.client.Do(httpReq)
		if err != nil {
			// If ctx was cancelled, treat as expected termination.
			if ctx.Err() != nil {
				return
			}
			errs <- err
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
			errs <- fmt.Errorf("upstream status=%d body=%q", resp.StatusCode, string(b))
			return
		}

		// Critical requirement: read line-by-line (do NOT buffer entire body).
		br := bufio.NewReader(resp.Body)

		var eventLines []string
		for {
			// If client disconnects, ctx cancellation should abort request and unblocks ReadString.
			if ctx.Err() != nil {
				return
			}

			line, err := br.ReadString('\n')
			if err != nil {
				// context cancellation is expected
				if ctx.Err() != nil {
					return
				}
				// upstream closed normally (EOF) => end
				if err == io.EOF {
					return
				}
				errs <- err
				return
			}

			// SSE event terminator: blank line
			if line == "\n" || line == "\r\n" {
				payload := readSSEEventFromLines(eventLines)
				eventLines = eventLines[:0]

				chunk, done, perr := parseSSEEventData(payload)
				if perr != nil {
					errs <- perr
					return
				}
				// ignore empty payload events
				if payload != "" {
					select {
					case <-ctx.Done():
						return
					case chunks <- chunk:
					}
				}
				if done {
					return
				}
				continue
			}

			eventLines = append(eventLines, line)
		}
	}()

	return chunks, errs
}
