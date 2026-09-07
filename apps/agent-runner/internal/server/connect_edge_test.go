package server

import (
	"io"
	"net"
	"testing"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
	"github.com/gorilla/websocket"
)

func TestSessionConnectRequiresActiveSession(t *testing.T) {
	_, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()

	send(t, conn, protocol.TypeConnect, "missing-session", protocol.ConnectRequest{
		ConnectionID: "missing",
		Network:      "tcp",
		Address:      "127.0.0.1:4096",
	})
	assertConnectCloseCode(t, read(t, conn), "missing", "session_not_found")
}

func TestSessionConnectValidatesConnectionMessages(t *testing.T) {
	_, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()
	startConnectTestSession(t, conn, "validation-session")

	send(t, conn, protocol.TypeConnect, "validation-session", protocol.ConnectRequest{
		Network: "tcp", Address: "127.0.0.1:4096",
	})
	assertConnectCloseCode(t, read(t, conn), "", "invalid_connect")

	send(t, conn, protocol.TypeConnectData, "validation-session", protocol.ConnectData{})
	assertConnectCloseCode(t, read(t, conn), "", "invalid_connect_data")

	send(t, conn, protocol.TypeConnectData, "validation-session", protocol.ConnectData{ConnectionID: "unknown", Data: []byte("data")})
	assertConnectCloseCode(t, read(t, conn), "unknown", "connection_not_found")

	send(t, conn, protocol.TypeConnectClose, "validation-session", protocol.ConnectClose{})
	assertConnectCloseCode(t, read(t, conn), "", "invalid_connect_close")

	send(t, conn, protocol.TypeKill, "validation-session", nil)
	waitForExit(t, conn)
}

func TestSessionConnectReportsLoopbackDialFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	_, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()
	startConnectTestSession(t, conn, "dial-failure-session")

	send(t, conn, protocol.TypeConnect, "dial-failure-session", protocol.ConnectRequest{
		ConnectionID: "dial-failure", Network: "tcp", Address: address,
	})
	assertConnectCloseCode(t, read(t, conn), "dial-failure", "connect_failed")

	send(t, conn, protocol.TypeKill, "dial-failure-session", nil)
	waitForExit(t, conn)
}

func TestSessionConnectRejectsDuplicateConnectionID(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			peer, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				defer peer.Close()
				_, _ = io.Copy(io.Discard, peer)
			}()
		}
	}()

	_, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()
	startConnectTestSession(t, conn, "duplicate-session")

	request := protocol.ConnectRequest{ConnectionID: "same-id", Network: "tcp", Address: listener.Addr().String()}
	send(t, conn, protocol.TypeConnect, "duplicate-session", request)
	if msg := read(t, conn); msg.Type != protocol.TypeConnected {
		t.Fatalf("first connection response=%#v", msg)
	}
	send(t, conn, protocol.TypeConnect, "duplicate-session", request)
	assertConnectCloseCode(t, read(t, conn), "same-id", "duplicate_connection")

	send(t, conn, protocol.TypeConnectClose, "duplicate-session", protocol.ConnectClose{ConnectionID: "same-id"})
	if msg := read(t, conn); msg.Type != protocol.TypeConnectClose {
		t.Fatalf("close response=%#v", msg)
	}
	send(t, conn, protocol.TypeKill, "duplicate-session", nil)
	waitForExit(t, conn)
}

func TestSessionEndClosesActiveConnections(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		peer, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer peer.Close()
		_, _ = io.Copy(io.Discard, peer)
	}()

	_, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()
	startConnectTestSession(t, conn, "end-session")

	send(t, conn, protocol.TypeConnect, "end-session", protocol.ConnectRequest{
		ConnectionID: "active", Network: "tcp", Address: listener.Addr().String(),
	})
	if msg := read(t, conn); msg.Type != protocol.TypeConnected {
		t.Fatalf("connect response=%#v", msg)
	}

	send(t, conn, protocol.TypeKill, "end-session", nil)
	seenExit, seenClosed := false, false
	for !seenExit || !seenClosed {
		msg := read(t, conn)
		switch msg.Type {
		case protocol.TypeExit:
			seenExit = true
		case protocol.TypeConnectClose:
			payload, decodeErr := protocol.DecodePayload[protocol.ConnectClose](msg)
			if decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if payload.ConnectionID == "active" && payload.Code == "session_ended" {
				seenClosed = true
			}
		}
	}
}

func TestValidateConnectDestinationRejectsMalformedAddressAndPort(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "127.0.0.1:not-a-port", "127.0.0.1:65536"} {
		if err := validateConnectDestination("tcp", address); err == nil {
			t.Fatalf("address %q unexpectedly accepted", address)
		}
	}
}

func startConnectTestSession(t *testing.T, conn *websocket.Conn, sessionID string) {
	t.Helper()
	send(t, conn, protocol.TypeStart, sessionID, protocol.StartRequest{Command: []string{"sh", "-c", "sleep 30"}})
	if msg := read(t, conn); msg.Type != protocol.TypeSessionStarted {
		t.Fatalf("start response=%#v", msg)
	}
}

func waitForExit(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	for {
		if msg := read(t, conn); msg.Type == protocol.TypeExit {
			return
		}
	}
}

func assertConnectCloseCode(t *testing.T, msg protocol.Message, connectionID, code string) {
	t.Helper()
	if msg.Type != protocol.TypeConnectClose {
		t.Fatalf("expected connect close, got %#v", msg)
	}
	payload, err := protocol.DecodePayload[protocol.ConnectClose](msg)
	if err != nil {
		t.Fatal(err)
	}
	if payload.ConnectionID != connectionID || payload.Code != code {
		t.Fatalf("close=%#v want connection=%q code=%q", payload, connectionID, code)
	}
}
