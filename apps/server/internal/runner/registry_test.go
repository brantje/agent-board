package runner

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/brantje/agent-board/apps/server/internal/store"
	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type registryIdentity struct{}

func (registryIdentity) Authenticate(_ context.Context, id, token string) (store.Runner, error) {
	if id != "runner" || token != "secret" {
		return store.Runner{}, errors.New("denied")
	}
	return store.Runner{ID: id}, nil
}
func (registryIdentity) ObserveRunner(context.Context, string, json.RawMessage) error { return nil }

type registryConnectionReconciler struct {
	calls int
	old   protocol.Health
	new   protocol.Health
}

func (r *registryConnectionReconciler) ReconcileRunnerConnection(_ context.Context, _ string, old, replacement protocol.Health) error {
	r.calls++
	r.old = old
	r.new = replacement
	return nil
}

func TestInboundRegistryAuthenticatesBeforeUpgrade(t *testing.T) {
	registry := NewRegistry(registryIdentity{}, registryIdentity{})
	defer registry.Close()
	server := httptest.NewServer(registry)
	defer server.Close()
	for _, headers := range []http.Header{nil, {"Authorization": []string{"Bearer wrong"}, protocol.RunnerIDHeader: []string{"runner"}}, {"Authorization": []string{"Bearer secret"}}} {
		conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), headers)
		if conn != nil {
			conn.Close()
		}
		if err == nil || response == nil || response.StatusCode != 401 {
			t.Fatalf("unauthorized upgrade: %v", err)
		}
		response.Body.Close()
	}
}

func TestInboundRegistryNegotiatesLiveConnectionAndReplacement(t *testing.T) {
	registry := NewRegistry(registryIdentity{}, registryIdentity{})
	reconciler := &registryConnectionReconciler{}
	registry.SetConnectionReconciler(reconciler)
	defer registry.Close()
	server := httptest.NewServer(registry)
	defer server.Close()
	connect := func() *websocket.Conn {
		t.Helper()
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), http.Header{"Authorization": []string{"Bearer secret"}, protocol.RunnerIDHeader: []string{"runner"}})
		if err != nil {
			t.Fatal(err)
		}
		var msg protocol.Message
		if err = conn.ReadJSON(&msg); err != nil {
			t.Fatal(err)
		}
		hello, _ := protocol.NewMessage(protocol.Version2, protocol.TypeRunnerHello, "", protocol.RunnerHello{Version: protocol.Version2, Capabilities: protocol.Capabilities{RunnerVersion: "test", OS: "linux", Architecture: "amd64", MaxActiveSessions: 1, Engines: []string{"opencode"}, Features: []string{"stdin", "stdout", "stderr", "terminate", "kill", "health"}}})
		if err = conn.WriteJSON(hello); err != nil {
			t.Fatal(err)
		}
		health, _ := protocol.NewMessage(protocol.Version2, protocol.TypeHealth, "", protocol.Health{Status: "ok"})
		if err = conn.WriteJSON(health); err != nil {
			t.Fatal(err)
		}
		return conn
	}
	first := connect()
	defer first.Close()
	deadline := time.Now().Add(time.Second)
	for !registry.Connected("runner") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !registry.Connected("runner") {
		t.Fatal("authenticated runner not live")
	}
	old, err := registry.Connect(context.Background(), "project", "runner")
	if err != nil {
		t.Fatal(err)
	}
	second := connect()
	defer second.Close()
	select {
	case <-old.Done():
	case <-time.After(time.Second):
		t.Fatal("zombie transport not replaced")
	}
	if !registry.Connected("runner") {
		t.Fatal("replacement lost live state")
	}
	if reconciler.calls < 2 || reconciler.old.Status != "ok" || reconciler.new.Status != "ok" {
		t.Fatalf("replacement reconciliation calls=%d old=%+v new=%+v", reconciler.calls, reconciler.old, reconciler.new)
	}
	if err = registry.Close(); err != nil {
		t.Fatal(err)
	}
	if registry.Connected("runner") {
		t.Fatal("closed registry reports live")
	}
}

func TestRegistryCandidatesRequireLiveEngineMatch(t *testing.T) {
	registry := NewRegistry(registryIdentity{}, registryIdentity{})
	defer registry.Close()
	if ids := registry.Candidates("opencode"); len(ids) != 0 {
		t.Fatalf("empty registry candidates=%v", ids)
	}
	server := httptest.NewServer(registry)
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), http.Header{"Authorization": []string{"Bearer secret"}, protocol.RunnerIDHeader: []string{"runner"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var msg protocol.Message
	if err = conn.ReadJSON(&msg); err != nil {
		t.Fatal(err)
	}
	hello, _ := protocol.NewMessage(protocol.Version2, protocol.TypeRunnerHello, "", protocol.RunnerHello{Version: protocol.Version2, Capabilities: protocol.Capabilities{RunnerVersion: "test", OS: "linux", Architecture: "amd64", MaxActiveSessions: 1, Engines: []string{"opencode"}, Features: []string{"stdin", "stdout", "stderr", "terminate", "kill", "health"}}})
	if err = conn.WriteJSON(hello); err != nil {
		t.Fatal(err)
	}
	health, _ := protocol.NewMessage(protocol.Version2, protocol.TypeHealth, "", protocol.Health{Status: "ok"})
	if err = conn.WriteJSON(health); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for !registry.Connected("runner") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := registry.Candidates("opencode"); len(got) != 1 || got[0] != "runner" {
		t.Fatalf("live engine match=%v", got)
	}
	if got := registry.Candidates("scripted"); len(got) != 0 {
		t.Fatalf("mismatched engine candidates=%v", got)
	}
	_ = conn.Close()
	deadline = time.Now().Add(time.Second)
	for registry.Connected("runner") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := registry.Candidates("opencode"); len(got) != 0 {
		t.Fatalf("disconnected runner remained eligible=%v", got)
	}
}

func TestRegistryDisconnectAndReconcile(t *testing.T) {
	registry := NewRegistry(registryIdentity{}, registryIdentity{})
	defer registry.Close()
	if _, _, err := registry.Reconcile(context.Background(), "project", "runner", "session-1"); !errors.Is(err, ErrDisconnected) {
		t.Fatalf("reconcile disconnected: %v", err)
	}
	registry.Disconnect("missing")

	server := httptest.NewServer(registry)
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), http.Header{"Authorization": []string{"Bearer secret"}, protocol.RunnerIDHeader: []string{"runner"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var msg protocol.Message
	if err = conn.ReadJSON(&msg); err != nil {
		t.Fatal(err)
	}
	hello, _ := protocol.NewMessage(protocol.Version2, protocol.TypeRunnerHello, "", protocol.RunnerHello{Version: protocol.Version2, Capabilities: protocol.Capabilities{RunnerVersion: "test", OS: "linux", Architecture: "amd64", MaxActiveSessions: 1, Engines: []string{"opencode"}, Features: []string{"stdin", "stdout", "stderr", "terminate", "kill", "health"}}})
	if err = conn.WriteJSON(hello); err != nil {
		t.Fatal(err)
	}
	health, _ := protocol.NewMessage(protocol.Version2, protocol.TypeHealth, "", protocol.Health{Status: "ok"})
	if err = conn.WriteJSON(health); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for !registry.Connected("runner") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !registry.Connected("runner") {
		t.Fatal("authenticated runner not live")
	}
	healthSnapshot, ok := registry.Health("runner")
	if !ok || healthSnapshot.Status != "ok" {
		t.Fatalf("health=%+v ok=%v", healthSnapshot, ok)
	}
	if _, healthOK := registry.Health("missing"); healthOK {
		t.Fatal("missing runner reported health")
	}
	if _, _, err := registry.Reconcile(context.Background(), "project", "runner", ""); err == nil {
		t.Fatal("reconcile accepted a blank session")
	}
	if _, known := registry.DisconnectedSince("runner"); known {
		t.Fatal("live runner reported disconnected")
	}
	registry.Disconnect("runner")
	if registry.Connected("runner") {
		t.Fatal("disconnect left runner live")
	}
	disconnectedAt, known := registry.DisconnectedSince("runner")
	if !known || disconnectedAt.IsZero() {
		t.Fatalf("disconnect timestamp known=%v at=%v", known, disconnectedAt)
	}
	if again, _ := registry.DisconnectedSince("runner"); !again.Equal(disconnectedAt) {
		t.Fatalf("disconnect timestamp changed: first=%v second=%v", disconnectedAt, again)
	}
}
