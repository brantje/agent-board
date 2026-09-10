package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/agent-runner/internal/session"
	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
	"github.com/gorilla/websocket"
)

func TestOutboundConnectionAuthenticatesAndExecutes(t *testing.T) {
	done := make(chan error, 1)
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" || r.Header.Get(protocol.RunnerIDHeader) != "runner" || r.URL.RawQuery != "" {
			http.Error(w, "auth", 401)
			return
		}
		u := websocket.Upgrader{}
		c, err := u.Upgrade(w, r, nil)
		if err != nil {
			done <- err
			return
		}
		defer c.Close()
		send := func(typ protocol.MessageType, id string, payload any) {
			m, _ := protocol.NewMessage(protocol.Version2, typ, id, payload)
			if err = c.WriteJSON(m); err != nil {
				t.Error(err)
			}
		}
		send(protocol.TypeServerHello, "", protocol.ServerHello{SupportedVersions: []int{protocol.Version2}})
		var hello, health protocol.Message
		if err = c.ReadJSON(&hello); err != nil {
			done <- err
			return
		}
		caps, e := protocol.DecodePayload[protocol.RunnerHello](hello)
		if e != nil || caps.Capabilities.Validate() != nil {
			t.Error("invalid capabilities")
		}
		if err = c.ReadJSON(&health); err != nil {
			done <- err
			return
		}
		send(protocol.TypeStart, "session", protocol.StartRequest{Command: []string{"sh", "-c", "printf hello"}})
		output := ""
		for {
			var m protocol.Message
			if err = c.ReadJSON(&m); err != nil {
				done <- err
				return
			}
			if m.Type == protocol.TypeStdout {
				v, _ := protocol.DecodePayload[protocol.StreamData](m)
				output += string(v.Data)
			}
			if m.Type == protocol.TypeError {
				t.Errorf("runner error: %s", m.Payload)
				done <- nil
				return
			}
			if m.Type == protocol.TypeExit {
				if output != "hello" {
					t.Errorf("output %q", output)
				}
				done <- nil
				return
			}
		}
	}))
	defer host.Close()
	runner := New(Config{WorkspaceRoot: t.TempDir(), MaxActiveSessions: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = runner.Connect(ctx, host.URL, "runner", "token") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("outbound execution timed out")
	}
	cancel()
	if err := runner.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestOutboundConnectionReturnsAuthenticationFailureWithoutRetry(t *testing.T) {
	requests := make(chan struct{}, 2)
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests <- struct{}{}
		http.Error(w, "revoked", http.StatusUnauthorized)
	}))
	defer host.Close()

	runner := New(Config{WorkspaceRoot: t.TempDir(), MaxActiveSessions: 1})
	execution, err := runner.manager.Start("active", session.Request{Command: []string{"sh", "-c", "sleep 30"}})
	if err != nil {
		t.Fatal(err)
	}
	err = runner.Connect(t.Context(), host.URL, "runner", "revoked-token")
	if !errors.Is(err, ErrAuthentication) {
		t.Fatalf("Connect() error=%v, want ErrAuthentication", err)
	}
	if len(requests) != 1 {
		t.Fatalf("authentication rejection retried %d times", len(requests))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := execution.Wait(ctx)
	if err != nil {
		t.Fatalf("active session was not terminated after credential rejection: %v", err)
	}
	if !result.Signaled {
		t.Fatalf("credential rejection did not kill active execution: %#v", result)
	}
}

func TestOutboundConnectionRetriesTransientFailure(t *testing.T) {
	var attempts atomic.Int32
	connected := make(chan struct{}, 1)
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			http.Error(w, "temporary failure", http.StatusServiceUnavailable)
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		hello, err := protocol.NewMessage(protocol.Version2, protocol.TypeServerHello, "", protocol.ServerHello{SupportedVersions: []int{protocol.Version2}})
		if err != nil {
			t.Error(err)
			return
		}
		if err := conn.WriteJSON(hello); err != nil {
			t.Error(err)
			return
		}
		var runnerHello, health protocol.Message
		if err := conn.ReadJSON(&runnerHello); err != nil {
			t.Error(err)
			return
		}
		if err := conn.ReadJSON(&health); err != nil {
			t.Error(err)
			return
		}
		connected <- struct{}{}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer host.Close()

	runner := New(Config{WorkspaceRoot: t.TempDir(), MaxActiveSessions: 1})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runner.Connect(ctx, host.URL, "runner", "token") }()

	select {
	case <-connected:
	case <-time.After(4 * time.Second):
		cancel()
		t.Fatal("runner did not reconnect after transient failure")
	}
	if attempts.Load() < 2 {
		cancel()
		t.Fatalf("connection attempts=%d, want retry", attempts.Load())
	}
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Connect() after cancellation=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Connect() did not stop after cancellation")
	}
}

func TestOutboundConnectionStopsRetryingOnRunnerShutdown(t *testing.T) {
	requests := make(chan struct{}, 2)
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests <- struct{}{}
		http.Error(w, "temporary failure", http.StatusServiceUnavailable)
	}))
	defer host.Close()

	runner := New(Config{WorkspaceRoot: t.TempDir(), MaxActiveSessions: 1})
	result := make(chan error, 1)
	go func() { result <- runner.Connect(context.Background(), host.URL, "runner", "token") }()
	select {
	case <-requests:
	case <-time.After(time.Second):
		t.Fatal("runner did not attempt connection")
	}
	if err := runner.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Connect() after runner shutdown=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Connect() kept retrying after runner shutdown")
	}
}

func TestOutboundConnectionRejectsInvalidConfiguration(t *testing.T) {
	runner := New(Config{WorkspaceRoot: t.TempDir(), MaxActiveSessions: 1})
	for _, test := range []struct {
		name, serverURL, id, token string
	}{
		{name: "query", serverURL: "https://agent-board.example.com?token=leak", id: "runner", token: "token"},
		{name: "userinfo", serverURL: "https://user@agent-board.example.com", id: "runner", token: "token"},
		{name: "scheme", serverURL: "ws://agent-board.example.com", id: "runner", token: "token"},
		{name: "missing id", serverURL: "https://agent-board.example.com", token: "token"},
		{name: "missing token", serverURL: "https://agent-board.example.com", id: "runner"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := runner.Connect(t.Context(), test.serverURL, test.id, test.token); err == nil {
				t.Fatal("invalid outbound configuration was accepted")
			}
		})
	}
}
