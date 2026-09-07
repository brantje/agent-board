package server

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
)

func TestPendingSessionConnectKeepsControlMessagesResponsive(t *testing.T) {
	originalDial := connectDialContext
	t.Cleanup(func() { connectDialContext = originalDial })

	dialStarted := make(chan struct{})
	dialCanceled := make(chan struct{})
	connectDialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		close(dialStarted)
		<-ctx.Done()
		close(dialCanceled)
		return nil, ctx.Err()
	}

	_, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()
	startConnectTestSession(t, conn, "pending-control-session")

	send(t, conn, protocol.TypeConnect, "pending-control-session", protocol.ConnectRequest{
		ConnectionID: "pending-control",
		Network:      "tcp",
		Address:      "127.0.0.1:4096",
	})
	select {
	case <-dialStarted:
	case <-time.After(time.Second):
		t.Fatal("session connect dial did not start")
	}

	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	send(t, conn, protocol.TypeHealth, "", nil)
	if msg := read(t, conn); msg.Type != protocol.TypeHealth {
		t.Fatalf("health response=%#v", msg)
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}

	send(t, conn, protocol.TypeConnectClose, "pending-control-session", protocol.ConnectClose{ConnectionID: "pending-control"})
	assertConnectCloseCode(t, read(t, conn), "pending-control", "")
	select {
	case <-dialCanceled:
	case <-time.After(time.Second):
		t.Fatal("pending dial was not cancelled by connect_close")
	}

	send(t, conn, protocol.TypeKill, "pending-control-session", nil)
	waitForConnectTestExit(t, conn)
}

func TestPendingSessionConnectQueuesDataUntilDialCompletes(t *testing.T) {
	originalDial := connectDialContext
	t.Cleanup(func() { connectDialContext = originalDial })

	dialStarted := make(chan struct{})
	releaseDial := make(chan struct{})
	peerReady := make(chan net.Conn, 1)
	connectDialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		close(dialStarted)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-releaseDial:
		}
		local, peer := net.Pipe()
		peerReady <- peer
		return local, nil
	}

	_, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()
	startConnectTestSession(t, conn, "pending-data-session")

	send(t, conn, protocol.TypeConnect, "pending-data-session", protocol.ConnectRequest{
		ConnectionID: "pending-data",
		Network:      "tcp",
		Address:      "127.0.0.1:4096",
	})
	select {
	case <-dialStarted:
	case <-time.After(time.Second):
		t.Fatal("session connect dial did not start")
	}

	send(t, conn, protocol.TypeConnectData, "pending-data-session", protocol.ConnectData{
		ConnectionID: "pending-data",
		Data:         []byte("hello"),
	})
	close(releaseDial)
	if msg := read(t, conn); msg.Type != protocol.TypeConnected {
		t.Fatalf("connect response=%#v", msg)
	}

	var peer net.Conn
	select {
	case peer = <-peerReady:
	case <-time.After(time.Second):
		t.Fatal("dial peer was not created")
	}
	defer peer.Close()
	if err := peer.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, len("hello"))
	if _, err := io.ReadFull(peer, buffer); err != nil {
		t.Fatalf("read queued data: %v", err)
	}
	if string(buffer) != "hello" {
		t.Fatalf("queued data=%q", buffer)
	}

	send(t, conn, protocol.TypeConnectClose, "pending-data-session", protocol.ConnectClose{ConnectionID: "pending-data"})
	assertConnectCloseCode(t, read(t, conn), "pending-data", "")
	send(t, conn, protocol.TypeKill, "pending-data-session", nil)
	waitForConnectTestExit(t, conn)
}

func TestNetworkAllowsIPEnforcesRequestedAddressFamily(t *testing.T) {
	ipv4 := net.ParseIP("127.0.0.1")
	ipv6 := net.ParseIP("::1")
	cases := []struct {
		name    string
		network string
		ip      net.IP
		want    bool
	}{
		{name: "tcp accepts ipv4", network: "tcp", ip: ipv4, want: true},
		{name: "tcp accepts ipv6", network: "tcp", ip: ipv6, want: true},
		{name: "tcp4 accepts ipv4", network: "tcp4", ip: ipv4, want: true},
		{name: "tcp4 rejects ipv6", network: "tcp4", ip: ipv6, want: false},
		{name: "tcp6 rejects ipv4", network: "tcp6", ip: ipv4, want: false},
		{name: "tcp6 accepts ipv6", network: "tcp6", ip: ipv6, want: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := networkAllowsIP(test.network, test.ip); got != test.want {
				t.Fatalf("networkAllowsIP(%q, %v)=%v want %v", test.network, test.ip, got, test.want)
			}
		})
	}
}
