package runner

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
	"github.com/gorilla/websocket"
)

type replacementIdentity struct{}

func (replacementIdentity) Authenticate(_ context.Context, id, token string) (store.Runner, error) {
	if id != "runner-replacement" || token != "secret" {
		return store.Runner{}, errors.New("denied")
	}
	return store.Runner{ID: id}, nil
}
func (replacementIdentity) ObserveRunner(context.Context, string, json.RawMessage) error { return nil }

type replacementReconcilerFunc func(context.Context, string, protocol.Health, protocol.Health) error

func (f replacementReconcilerFunc) ReconcileRunnerConnection(ctx context.Context, id string, current, replacement protocol.Health) error {
	return f(ctx, id, current, replacement)
}

func dialReplacementRunner(t *testing.T, serverURL string, health protocol.Health) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(serverURL, "http"), http.Header{
		"Authorization":          []string{"Bearer secret"},
		protocol.RunnerIDHeader: []string{"runner-replacement"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var serverHello protocol.Message
	if err := conn.ReadJSON(&serverHello); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	hello, err := protocol.NewMessage(protocol.Version2, protocol.TypeRunnerHello, "", protocol.RunnerHello{
		Version: protocol.Version2,
		Capabilities: protocol.Capabilities{
			RunnerVersion: "test", OS: "linux", Architecture: "amd64", MaxActiveSessions: 1,
			Engines: []string{"opencode"}, Features: []string{"stdin", "stdout", "stderr", "terminate", "kill", "health"},
		},
	})
	if err != nil {
		conn.Close()
		t.Fatal(err)
	}
	if err := conn.WriteJSON(hello); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	healthMessage, err := protocol.NewMessage(protocol.Version2, protocol.TypeHealth, "", health)
	if err != nil {
		conn.Close()
		t.Fatal(err)
	}
	if err := conn.WriteJSON(healthMessage); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	return conn
}

func waitReplacementConnected(t *testing.T, registry *Registry) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !registry.Connected("runner-replacement") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !registry.Connected("runner-replacement") {
		t.Fatal("runner did not become connected")
	}
}

func TestRegistryReplacementKeepsOldTransportOnSessionMismatch(t *testing.T) {
	registry := NewRegistry(replacementIdentity{}, replacementIdentity{})
	registry.SetConnectionReconciler(replacementReconcilerFunc(func(_ context.Context, _ string, current, replacement protocol.Health) error {
		if len(current.ActiveSessionIDs) == 1 && current.ActiveSessionIDs[0] == "session-x" && len(replacement.ActiveSessionIDs) == 0 {
			return errors.New("active session would be lost")
		}
		return nil
	}))
	defer registry.Close()
	server := httptest.NewServer(registry)
	defer server.Close()

	active := protocol.Health{Status: "ok", ActiveSessions: 1, ActiveSessionIDs: []string{"session-x"}}
	first := dialReplacementRunner(t, server.URL, active)
	defer first.Close()
	waitReplacementConnected(t, registry)
	old, err := registry.Connect(context.Background(), "project", "runner-replacement")
	if err != nil {
		t.Fatal(err)
	}

	second := dialReplacementRunner(t, server.URL, protocol.Health{Status: "ok"})
	defer second.Close()
	_ = second.SetReadDeadline(time.Now().Add(time.Second))
	var msg protocol.Message
	if err := second.ReadJSON(&msg); err != nil {
		t.Fatalf("read replacement rejection: %v", err)
	}
	if msg.Type != protocol.TypeError {
		t.Fatalf("replacement response=%#v", msg)
	}
	payload, err := protocol.DecodePayload[protocol.ErrorPayload](msg)
	if err != nil || payload.Code != "runner_session_conflict" {
		t.Fatalf("replacement error=%#v decode=%v", payload, err)
	}
	select {
	case <-old.Done():
		t.Fatal("old active transport was displaced")
	default:
	}
	current, err := registry.Connect(context.Background(), "project", "runner-replacement")
	if err != nil || current != old {
		t.Fatalf("registry lost old transport current=%v err=%v", current, err)
	}
}

func TestRegistryReplacementReattachesSameSessionWithoutDuplicateEligibility(t *testing.T) {
	registry := NewRegistry(replacementIdentity{}, replacementIdentity{})
	registry.SetConnectionReconciler(replacementReconcilerFunc(func(_ context.Context, _ string, current, replacement protocol.Health) error {
		if len(current.ActiveSessionIDs) == 1 && len(replacement.ActiveSessionIDs) == 1 && current.ActiveSessionIDs[0] == replacement.ActiveSessionIDs[0] {
			return nil
		}
		return errors.New("session claim mismatch")
	}))
	defer registry.Close()
	server := httptest.NewServer(registry)
	defer server.Close()

	active := protocol.Health{Status: "ok", ActiveSessions: 1, ActiveSessionIDs: []string{"session-x"}}
	first := dialReplacementRunner(t, server.URL, active)
	defer first.Close()
	waitReplacementConnected(t, registry)
	old, err := registry.Connect(context.Background(), "project", "runner-replacement")
	if err != nil {
		t.Fatal(err)
	}

	second := dialReplacementRunner(t, server.URL, active)
	defer second.Close()
	select {
	case <-old.Done():
	case <-time.After(time.Second):
		t.Fatal("same-session replacement did not retire stale transport")
	}
	if !registry.Connected("runner-replacement") {
		t.Fatal("same-session replacement was not installed")
	}
	if got := registry.Candidates("opencode"); len(got) != 0 {
		t.Fatalf("active replacement became scheduler-eligible and could duplicate Engine execution: %v", got)
	}
}

func TestRegistryReplacementRejectsUnexpectedActiveClaim(t *testing.T) {
	registry := NewRegistry(replacementIdentity{}, replacementIdentity{})
	registry.SetConnectionReconciler(replacementReconcilerFunc(func(_ context.Context, _ string, _ protocol.Health, replacement protocol.Health) error {
		if len(replacement.ActiveSessionIDs) > 0 {
			return errors.New("unexpected active session")
		}
		return nil
	}))
	defer registry.Close()
	server := httptest.NewServer(registry)
	defer server.Close()

	conn := dialReplacementRunner(t, server.URL, protocol.Health{Status: "ok", ActiveSessions: 1, ActiveSessionIDs: []string{"unauthorized"}})
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	var msg protocol.Message
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("read rejection: %v", err)
	}
	if msg.Type != protocol.TypeError || registry.Connected("runner-replacement") {
		t.Fatalf("unexpected active claim became authoritative: msg=%#v connected=%v", msg, registry.Connected("runner-replacement"))
	}
}
