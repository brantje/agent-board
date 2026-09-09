package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type redirectDialer struct {
	target string
	mu     sync.Mutex
	calls  []string
}

func (d *redirectDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d.mu.Lock()
	d.calls = append(d.calls, network+" "+address)
	d.mu.Unlock()
	return (&net.Dialer{}).DialContext(ctx, network, d.target)
}

func (d *redirectDialer) lastCall() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.calls) == 0 {
		return ""
	}
	return d.calls[len(d.calls)-1]
}

func TestNewSessionRoutesHTTPThroughExecutionSessionDialer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "127.0.0.1:4321" {
			t.Errorf("host=%q want session-local address", r.Host)
		}
		if r.URL.Path != "/api/health" {
			t.Errorf("path=%q", r.URL.Path)
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		writeJSON(t, w, http.StatusOK, Health{Healthy: true, Version: "test"})
	}))
	defer server.Close()

	dialer := &redirectDialer{target: server.Listener.Addr().String()}
	native, err := NewSession(dialer, "127.0.0.1:4321")
	if err != nil {
		t.Fatal(err)
	}
	health, err := native.Health(context.Background())
	if err != nil || !health.Healthy {
		t.Fatalf("Health()=%+v err=%v", health, err)
	}
	if got := dialer.lastCall(); got != "tcp 127.0.0.1:4321" {
		t.Fatalf("dial=%q want session-local destination", got)
	}
	native.CloseIdleConnections()
}

func TestNewSessionUsesDefaultAddressAndValidatesDialer(t *testing.T) {
	if _, err := NewSession(nil, "127.0.0.1:4096"); err == nil {
		t.Fatal("nil session dialer unexpectedly accepted")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, Health{Healthy: true})
	}))
	defer server.Close()
	dialer := &redirectDialer{target: server.Listener.Addr().String()}
	native, err := NewSession(dialer, " ")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := native.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := dialer.lastCall(); got != "tcp "+defaultSessionAddress {
		t.Fatalf("dial=%q want default address %q", got, defaultSessionAddress)
	}
}

func TestEventStreamParsesMultilineDataAndEOF(t *testing.T) {
	stream := eventStreamForTest(": heartbeat\nevent: ignored\ndata: {\"id\":\"evt_1\",\ndata: \"type\":\"server.connected\",\"properties\":{}}\n\n")
	defer stream.Close()

	event, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() error=%v", err)
	}
	if event.ID != "evt_1" || event.Type != "server.connected" {
		t.Fatalf("event=%+v", event)
	}
	if _, err := stream.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("second Next() error=%v want EOF", err)
	}
}

func TestEventStreamRejectsOversizedAggregatePayload(t *testing.T) {
	const chunkSize = 64 << 10
	chunk := strings.Repeat("x", chunkSize)
	var input strings.Builder
	for input.Len() <= maxSSEEventSize+chunkSize {
		input.WriteString("data: ")
		input.WriteString(chunk)
		input.WriteByte('\n')
	}
	stream := eventStreamForTest(input.String())
	defer stream.Close()

	if _, err := stream.Next(); err == nil || !strings.Contains(err.Error(), "event payload exceeds") {
		t.Fatalf("Next() error=%v want aggregate payload limit", err)
	}
}

func TestEventStreamNormalizesOpenCodeV2DataPayload(t *testing.T) {
	stream := eventStreamForTest("data: {\"id\":\"evt_question\",\"type\":\"question.v2.asked\",\"data\":{\"id\":\"req_1\",\"sessionID\":\"ses_1\"}}\n\n")
	defer stream.Close()

	event, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() error=%v", err)
	}
	if event.ID != "evt_question" || event.Type != "question.v2.asked" {
		t.Fatalf("event=%+v", event)
	}
	var payload struct {
		ID        string `json:"id"`
		SessionID string `json:"sessionID"`
	}
	if err := json.Unmarshal(event.Properties, &payload); err != nil {
		t.Fatalf("decode normalized payload: %v", err)
	}
	if payload.ID != "req_1" || payload.SessionID != "ses_1" {
		t.Fatalf("payload=%+v", payload)
	}
}

func TestEventStreamPrefersV2DataOverCompatibilityProperties(t *testing.T) {
	event, err := decodeSSEEvent(`{"id":"evt_1","type":"session.error","data":{"sessionID":"ses_data"},"properties":{"sessionID":"ses_properties"}}`)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		SessionID string `json:"sessionID"`
	}
	if err := json.Unmarshal(event.Properties, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.SessionID != "ses_data" {
		t.Fatalf("sessionID=%q want V2 data payload", payload.SessionID)
	}
}

func TestEventStreamValidationAndUnavailableCases(t *testing.T) {
	var unavailable *EventStream
	if _, err := unavailable.Next(); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("nil Next() error=%v", err)
	}
	if err := unavailable.Close(); err != nil {
		t.Fatalf("nil Close() error=%v", err)
	}
	if _, err := decodeSSEEvent("{"); err == nil {
		t.Fatal("malformed SSE JSON unexpectedly accepted")
	}
	if _, err := decodeSSEEvent(`{"id":"evt_1"}`); err == nil || !strings.Contains(err.Error(), "no type") {
		t.Fatalf("missing type error=%v", err)
	}
}

func TestSubscribeReturnsTypedHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/event" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "stream unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	native, err := New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = native.Subscribe(context.Background())
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusServiceUnavailable || httpErr.Path != "/api/event" {
		t.Fatalf("Subscribe() error=%v", err)
	}
}

func TestHTTPErrorFormatting(t *testing.T) {
	withoutBody := (&HTTPError{StatusCode: http.StatusBadGateway, Method: http.MethodGet, Path: "/api/event"}).Error()
	if !strings.Contains(withoutBody, "HTTP 502") || strings.Contains(withoutBody, ": temporary") {
		t.Fatalf("without body=%q", withoutBody)
	}
	withBody := (&HTTPError{StatusCode: http.StatusBadGateway, Method: http.MethodGet, Path: "/api/event", Body: "temporary"}).Error()
	if !strings.Contains(withBody, "HTTP 502: temporary") {
		t.Fatalf("with body=%q", withBody)
	}
}

func eventStreamForTest(input string) *EventStream {
	body := io.NopCloser(strings.NewReader(input))
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64<<10), maxSSELineSize)
	return &EventStream{body: body, scanner: scanner}
}
