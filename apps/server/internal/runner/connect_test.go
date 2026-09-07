package runner

import (
	"context"
	"errors"
	"io"
	"testing"

	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
	"github.com/gorilla/websocket"
)

func TestDialSessionForwardsBytes(t *testing.T) {
	features := []string{"stdin", "stdout", "stderr", "terminate", "kill", "health", sessionConnectFeature}
	server := newProtocolTestServer(t, protocol.Capabilities{MaxActiveSessions: 1, Features: features}, func(conn *websocket.Conn, msg protocol.Message) {
		switch msg.Type {
		case protocol.TypeConnect:
			request, err := protocol.DecodePayload[protocol.ConnectRequest](msg)
			if err != nil {
				t.Errorf("decode connect: %v", err)
				return
			}
			if err := writeProtocol(conn, protocol.TypeConnected, msg.SessionID, protocol.Connected{ConnectionID: request.ConnectionID}); err != nil {
				t.Errorf("write connected: %v", err)
			}
		case protocol.TypeConnectData:
			data, err := protocol.DecodePayload[protocol.ConnectData](msg)
			if err != nil {
				t.Errorf("decode connect data: %v", err)
				return
			}
			if err := writeProtocol(conn, protocol.TypeConnectData, msg.SessionID, data); err != nil {
				t.Errorf("write connect data: %v", err)
			}
		case protocol.TypeConnectClose:
			closeMessage, err := protocol.DecodePayload[protocol.ConnectClose](msg)
			if err != nil {
				t.Errorf("decode connect close: %v", err)
				return
			}
			if err := writeProtocol(conn, protocol.TypeConnectClose, msg.SessionID, closeMessage); err != nil {
				t.Errorf("write connect close: %v", err)
			}
		}
	})
	defer server.Close()

	client, err := Dial(context.Background(), wsURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	connection, err := client.DialSession(context.Background(), "session-1", "tcp", "127.0.0.1:4096")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 5)
	if _, err := io.ReadFull(connection, buffer); err != nil {
		t.Fatal(err)
	}
	if string(buffer) != "hello" {
		t.Fatalf("read %q", buffer)
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDialSessionRequiresCapability(t *testing.T) {
	features := []string{"stdin", "stdout", "stderr", "terminate", "kill", "health"}
	server := newProtocolTestServer(t, protocol.Capabilities{MaxActiveSessions: 1, Features: features}, nil)
	defer server.Close()
	client, err := Dial(context.Background(), wsURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_, err = client.DialSession(context.Background(), "session-1", "tcp", "127.0.0.1:4096")
	if !errors.Is(err, ErrSessionConnectUnsupported) {
		t.Fatalf("DialSession() error=%v", err)
	}
}

func TestDialSessionReturnsScopedProtocolError(t *testing.T) {
	features := []string{"stdin", "stdout", "stderr", "terminate", "kill", "health", sessionConnectFeature}
	server := newProtocolTestServer(t, protocol.Capabilities{MaxActiveSessions: 1, Features: features}, func(conn *websocket.Conn, msg protocol.Message) {
		if msg.Type != protocol.TypeConnect {
			return
		}
		request, err := protocol.DecodePayload[protocol.ConnectRequest](msg)
		if err != nil {
			t.Errorf("decode connect: %v", err)
			return
		}
		if err := writeProtocol(conn, protocol.TypeConnectClose, msg.SessionID, protocol.ConnectClose{
			ConnectionID: request.ConnectionID,
			Code:         "connect_failed",
			Message:      "connection could not be established",
		}); err != nil {
			t.Errorf("write close: %v", err)
		}
	})
	defer server.Close()
	client, err := Dial(context.Background(), wsURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_, err = client.DialSession(context.Background(), "session-1", "tcp", "127.0.0.1:1")
	var protocolErr *ProtocolError
	if !errors.As(err, &protocolErr) || protocolErr.Code != "connect_failed" {
		t.Fatalf("DialSession() error=%v", err)
	}
}
