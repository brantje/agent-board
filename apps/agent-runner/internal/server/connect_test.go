package server

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
)

func TestSessionConnectForwardsLoopbackTCP(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(conn, conn)
	}()

	_, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()

	send(t, conn, protocol.TypeStart, "connect-session", protocol.StartRequest{Command: []string{"sh", "-c", "sleep 30"}})
	if msg := read(t, conn); msg.Type != protocol.TypeSessionStarted {
		t.Fatalf("unexpected start response %#v", msg)
	}

	send(t, conn, protocol.TypeConnect, "connect-session", protocol.ConnectRequest{
		ConnectionID: "tcp-1",
		Network:      "tcp",
		Address:      listener.Addr().String(),
	})
	connected := read(t, conn)
	if connected.Type != protocol.TypeConnected {
		t.Fatalf("expected connected, got %#v", connected)
	}
	payload, err := protocol.DecodePayload[protocol.Connected](connected)
	if err != nil {
		t.Fatal(err)
	}
	if payload.ConnectionID != "tcp-1" {
		t.Fatalf("connection id=%q", payload.ConnectionID)
	}

	send(t, conn, protocol.TypeConnectData, "connect-session", protocol.ConnectData{ConnectionID: "tcp-1", Data: []byte("hello")})
	dataMessage := read(t, conn)
	if dataMessage.Type != protocol.TypeConnectData {
		t.Fatalf("expected connect data, got %#v", dataMessage)
	}
	data, err := protocol.DecodePayload[protocol.ConnectData](dataMessage)
	if err != nil {
		t.Fatal(err)
	}
	if data.ConnectionID != "tcp-1" || string(data.Data) != "hello" {
		t.Fatalf("unexpected connect data %#v", data)
	}

	send(t, conn, protocol.TypeConnectClose, "connect-session", protocol.ConnectClose{ConnectionID: "tcp-1"})
	closed := read(t, conn)
	if closed.Type != protocol.TypeConnectClose {
		t.Fatalf("expected connect close, got %#v", closed)
	}

	send(t, conn, protocol.TypeKill, "connect-session", nil)
	for {
		msg := read(t, conn)
		if msg.Type == protocol.TypeExit {
			return
		}
	}
}

func TestSessionConnectRejectsNonLoopbackDestination(t *testing.T) {
	runner, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()

	send(t, conn, protocol.TypeStart, "connect-security", protocol.StartRequest{Command: []string{"sh", "-c", "sleep 30"}})
	if msg := read(t, conn); msg.Type != protocol.TypeSessionStarted {
		t.Fatalf("unexpected start response %#v", msg)
	}

	send(t, conn, protocol.TypeConnect, "connect-security", protocol.ConnectRequest{
		ConnectionID: "forbidden",
		Network:      "tcp",
		Address:      "example.com:80",
	})
	msg := read(t, conn)
	if msg.Type != protocol.TypeConnectClose {
		t.Fatalf("expected connect close, got %#v", msg)
	}
	payload, err := protocol.DecodePayload[protocol.ConnectClose](msg)
	if err != nil {
		t.Fatal(err)
	}
	if payload.ConnectionID != "forbidden" || payload.Code != "address_not_allowed" {
		t.Fatalf("unexpected close %#v", payload)
	}

	send(t, conn, protocol.TypeKill, "connect-security", nil)
	waitFor(t, 2*time.Second, func() bool {
		return runner.manager.ActiveCount() == 0
	})
}

func TestValidateConnectDestination(t *testing.T) {
	for _, address := range []string{"127.0.0.1:4096", "[::1]:4096", "localhost:4096"} {
		if err := validateConnectDestination("tcp", address); err != nil {
			t.Fatalf("validateConnectDestination(%q)=%v", address, err)
		}
	}
	for _, address := range []string{"0.0.0.0:4096", "10.0.0.1:4096", "example.com:4096", "127.0.0.1:0"} {
		if err := validateConnectDestination("tcp", address); err == nil {
			t.Fatalf("validateConnectDestination(%q) succeeded", address)
		}
	}
	if err := validateConnectDestination("udp", "127.0.0.1:4096"); err == nil {
		t.Fatal("udp destination unexpectedly allowed")
	}
}
