package runner

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
	"github.com/gorilla/websocket"
)

func TestDialBoundsApplicationHandshakeByContextDeadline(t *testing.T) {
	release := make(chan struct{})
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		<-release
	}))
	defer server.Close()
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := Dial(ctx, wsURL(server.URL)); err == nil {
		t.Fatal("Dial() unexpectedly completed without runner handshake")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("handshake deadline was not honored: %s", elapsed)
	}
}

func TestPendingSessionBufferCapsDistinctSessionIDs(t *testing.T) {
	conn := &Connection{pending: make(map[string]*pendingSessionMessages)}
	for i := 0; i < maxPendingSessions; i++ {
		msg, err := protocol.NewMessage(protocol.Version1, protocol.TypeStdout, fmt.Sprintf("session-%d", i), protocol.StreamData{Data: []byte("x")})
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.bufferPendingLocked(msg); err != nil {
			t.Fatalf("buffer session %d: %v", i, err)
		}
	}
	msg, err := protocol.NewMessage(protocol.Version1, protocol.TypeStdout, "session-over-limit", protocol.StreamData{Data: []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.bufferPendingLocked(msg); err == nil {
		t.Fatal("buffer accepted an unbounded distinct session id")
	}
}
