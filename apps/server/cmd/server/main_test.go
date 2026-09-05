package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestConfiguredAddress(t *testing.T) {
	t.Setenv("AGENT_BOARD_SERVER_ADDR", "")
	if got := configuredAddress(); got != defaultAddress {
		t.Fatalf("expected default address %q, got %q", defaultAddress, got)
	}

	const configured = "127.0.0.1:4321"
	t.Setenv("AGENT_BOARD_SERVER_ADDR", configured)
	if got := configuredAddress(); got != configured {
		t.Fatalf("expected configured address %q, got %q", configured, got)
	}
}

func TestNewHTTPServerUsesBoundedTimeouts(t *testing.T) {
	t.Parallel()

	server := newHTTPServer("127.0.0.1:0")
	if server.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("unexpected ReadHeaderTimeout: %s", server.ReadHeaderTimeout)
	}
	if server.ReadTimeout != serverReadTimeout {
		t.Fatalf("unexpected ReadTimeout: %s", server.ReadTimeout)
	}
	if server.IdleTimeout != serverIdleTimeout {
		t.Fatalf("unexpected IdleTimeout: %s", server.IdleTimeout)
	}
	if server.Handler == nil {
		t.Fatal("expected server handler")
	}
}

func TestRunRejectsInvalidAddress(t *testing.T) {
	t.Parallel()

	if err := run(context.Background(), "127.0.0.1:not-a-port"); err == nil {
		t.Fatal("expected listen error")
	}
}

func TestRunStopsWhenContextIsCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := run(ctx, "127.0.0.1:0"); err != nil {
		t.Fatalf("expected graceful shutdown, got %v", err)
	}
}

func TestServeReturnsListenerError(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	server := newHTTPServer("127.0.0.1:0")
	if err := serve(context.Background(), server, listener); err == nil {
		t.Fatal("expected Serve to return the closed-listener error")
	}
}

func TestServeTreatsServerCloseAsCleanShutdown(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := newHTTPServer(listener.Addr().String())

	done := make(chan error, 1)
	go func() {
		done <- serve(context.Background(), server, listener)
	}()

	waitForServer(t, listener.Addr().String())
	if err := server.Close(); err != nil {
		t.Fatalf("close server: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected clean server close, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop")
	}
}

func waitForServer(t *testing.T, address string) {
	t.Helper()

	client := &http.Client{Timeout: 100 * time.Millisecond}
	deadline := time.Now().Add(2 * time.Second)
	url := "http://" + address + "/healthz"
	for time.Now().Before(deadline) {
		response, err := client.Get(url)
		if err == nil {
			_ = response.Body.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server did not become reachable at %s", address)
}
