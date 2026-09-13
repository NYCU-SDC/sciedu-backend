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
	Stream(ctx context.Context, req AgentsRequest) (<-chan StreamChunk, <-chan error)
	GetTitle(ctx context.Context, messages []ChatMessage) (string, error)
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
		endpoint: strings.TrimRight(endpoint, "/"),
		client:   client,
		headers:  h,
	}
}

func parseSSEEventData(payload string) (StreamChunk, bool, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return StreamChunk{}, false, nil
	}

	if payload == "[DONE]" {
		chunk := StreamChunk{Legacy: &StreamDelta{IsFinished: true}}
		return chunk, true, nil
	}

	if strings.HasPrefix(payload, "{") {
		var envelope struct {
			Type AgentEventType `json:"type"`
		}
		if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
			return StreamChunk{}, false, fmt.Errorf("invalid json chunk payload: %w", err)
		}
		if envelope.Type != "" {
			if !validAgentEventType(envelope.Type) {
				return StreamChunk{}, false, fmt.Errorf("unsupported agent event type %q", envelope.Type)
			}
			var event AgentEvent
			if err := json.Unmarshal([]byte(payload), &event); err != nil {
				return StreamChunk{}, false, fmt.Errorf("invalid agent event payload: %w", err)
			}
			chunk := StreamChunk{Agentic: &event}
			return chunk, chunk.Terminal(), nil
		}

		var c StreamDelta
		if err := json.Unmarshal([]byte(payload), &c); err != nil {
			return StreamChunk{}, false, fmt.Errorf("invalid legacy chunk payload: %w", err)
		}
		chunk := StreamChunk{Legacy: &c}
		return chunk, chunk.Terminal(), nil
	}

	chunk := StreamChunk{Legacy: &StreamDelta{Delta: payload}}
	return chunk, false, nil
}

func validAgentEventType(eventType AgentEventType) bool {
	switch eventType {
	case AgentEventCast,
		AgentEventPartStart,
		AgentEventDelta,
		AgentEventPartEnd,
		AgentEventAgentStart,
		AgentEventAgentEnd,
		AgentEventDone,
		AgentEventError:
		return true
	default:
		return false
	}
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

func (p *Provider) Stream(ctx context.Context, req AgentsRequest) (<-chan StreamChunk, <-chan error) {
	chunks := make(chan StreamChunk)
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

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint+"/agents", bytes.NewReader(body))
		if err != nil {
			errs <- err
			return
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
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
		defer func() {
			if closeErr := resp.Body.Close(); closeErr != nil {
				select {
				case errs <- fmt.Errorf("failed to close response body: %w", closeErr):
				default:
				}
			}
		}()

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
				if err == io.EOF {
					if len(eventLines) > 0 {
						done, perr := publishSSEPayload(ctx, eventLines, chunks)
						if perr != nil {
							errs <- perr
							return
						}
						if done {
							return
						}
					}
					errs <- fmt.Errorf("upstream closed before finish")
					return
				}
				errs <- err
				return
			}

			// SSE event terminator: blank line
			if line == "\n" || line == "\r\n" {
				done, perr := publishSSEPayload(ctx, eventLines, chunks)
				eventLines = eventLines[:0]
				if perr != nil {
					errs <- perr
					return
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

func publishSSEPayload(ctx context.Context, eventLines []string, chunks chan<- StreamChunk) (bool, error) {
	payload := readSSEEventFromLines(eventLines)
	chunk, done, err := parseSSEEventData(payload)
	if err != nil {
		return false, err
	}
	if payload == "" {
		return false, nil
	}
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case chunks <- chunk:
	}
	return done, nil
}

func (p *Provider) GetTitle(ctx context.Context, messages []ChatMessage) (string, error) {
	reqBody := struct {
		Messages []ChatMessage `json:"messages"`
	}{
		Messages: messages,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint+"/chat/title", bytes.NewReader(body))
	if err != nil {
		return "", err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	for k, vs := range p.headers {
		for _, v := range vs {
			httpReq.Header.Add(k, v)
		}
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return "", fmt.Errorf("upstream status=%d body=%q", resp.StatusCode, string(b))
	}

	var respData struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		return "", fmt.Errorf("failed to decode get-title response: %w", err)
	}
	return respData.Title, nil
}
