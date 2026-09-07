package client

import (
	"bufio"
	"context"
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
		if r.URL.Path != "/api/health" {
			t.Fatalf("path=%q", r.URL.Path)
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
