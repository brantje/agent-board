package client

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxSSELineSize = 8 << 20
const maxSSEEventSize = 8 << 20

type EventStream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
}

func (c *Client) Subscribe(ctx context.Context) (*EventStream, error) {
	if c == nil || c.http == nil {
		return nil, fmt.Errorf("opencode: client is unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/event", nil)
	if err != nil {
		return nil, fmt.Errorf("opencode: build event subscription: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opencode: subscribe events: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, &HTTPError{StatusCode: resp.StatusCode, Method: http.MethodGet, Path: "/api/event", Body: strings.TrimSpace(string(data))}
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64<<10), maxSSELineSize)
	return &EventStream{body: resp.Body, scanner: scanner}, nil
}

func (s *EventStream) Next() (Event, error) {
	if s == nil || s.scanner == nil {
		return Event{}, fmt.Errorf("opencode: event stream is unavailable")
	}
	var data strings.Builder
	for s.scanner.Scan() {
		line := s.scanner.Text()
		if line == "" {
			if data.Len() == 0 {
				continue
			}
			return decodeSSEEvent(data.String())
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			field = line
			value = ""
		} else if strings.HasPrefix(value, " ") {
			value = value[1:]
		}
		if field != "data" {
			continue
		}
		added := len(value)
		if data.Len() > 0 {
			added++
		}
		if added > maxSSEEventSize-data.Len() {
			return Event{}, fmt.Errorf("opencode: event payload exceeds %d bytes", maxSSEEventSize)
		}
		if data.Len() > 0 {
			data.WriteByte('\n')
		}
		data.WriteString(value)
	}
	if err := s.scanner.Err(); err != nil {
		return Event{}, fmt.Errorf("opencode: read event stream: %w", err)
	}
	if data.Len() > 0 {
		return decodeSSEEvent(data.String())
	}
	return Event{}, io.EOF
}

func (s *EventStream) Close() error {
	if s == nil || s.body == nil {
		return nil
	}
	return s.body.Close()
}

func decodeSSEEvent(data string) (Event, error) {
	var event Event
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return Event{}, fmt.Errorf("opencode: decode event payload: %w", err)
	}
	if strings.TrimSpace(event.Type) == "" {
		return Event{}, fmt.Errorf("opencode: event payload has no type")
	}
	return event, nil
}