package runner

import (
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"
)

func TestSessionConnAddressesAndDeadlines(t *testing.T) {
	connection := newSessionConn(nil, "session-1", "connection-1", "tcp", "127.0.0.1:4096")
	if connection.LocalAddr().Network() != "tcp" || connection.LocalAddr().String() != "agent-board" {
		t.Fatalf("local address=%v", connection.LocalAddr())
	}
	if connection.RemoteAddr().Network() != "tcp" || connection.RemoteAddr().String() != "127.0.0.1:4096" {
		t.Fatalf("remote address=%v", connection.RemoteAddr())
	}

	deadline := time.Now().Add(time.Minute)
	if err := connection.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	if !connection.readDeadline.Equal(deadline) || !connection.writeDeadline.Equal(deadline) {
		t.Fatalf("deadlines read=%v write=%v", connection.readDeadline, connection.writeDeadline)
	}
	readDeadline := deadline.Add(time.Minute)
	writeDeadline := deadline.Add(2 * time.Minute)
	if err := connection.SetReadDeadline(readDeadline); err != nil {
		t.Fatal(err)
	}
	if err := connection.SetWriteDeadline(writeDeadline); err != nil {
		t.Fatal(err)
	}
	if !connection.readDeadline.Equal(readDeadline) || !connection.writeDeadline.Equal(writeDeadline) {
		t.Fatalf("deadlines read=%v write=%v", connection.readDeadline, connection.writeDeadline)
	}
}

func TestSessionConnReadBuffersChunksAndHonorsDeadline(t *testing.T) {
	connection := newSessionConn(nil, "session-1", "connection-1", "tcp", "127.0.0.1:4096")
	if !connection.push([]byte("hello")) {
		t.Fatal("push unexpectedly failed")
	}
	first := make([]byte, 2)
	if n, err := connection.Read(first); err != nil || n != 2 || string(first) != "he" {
		t.Fatalf("first read n=%d data=%q err=%v", n, first, err)
	}
	second := make([]byte, 3)
	if n, err := connection.Read(second); err != nil || n != 3 || string(second) != "llo" {
		t.Fatalf("second read n=%d data=%q err=%v", n, second, err)
	}

	if err := connection.SetReadDeadline(time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Read(make([]byte, 1)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("expired read error=%v", err)
	}
}

func TestSessionConnWriteHonorsExpiredDeadlineBeforeTransport(t *testing.T) {
	connection := newSessionConn(nil, "session-1", "connection-1", "tcp", "127.0.0.1:4096")
	if err := connection.SetWriteDeadline(time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if n, err := connection.Write([]byte("data")); n != 0 || !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("Write() n=%d err=%v", n, err)
	}
}

func TestSessionConnPushBackpressureAndCloseErrors(t *testing.T) {
	connection := newSessionConn(nil, "session-1", "connection-1", "tcp", "127.0.0.1:4096")
	for index := 0; index < connectReadQueueDepth; index++ {
		if !connection.push([]byte{byte(index)}) {
			t.Fatalf("push %d unexpectedly failed", index)
		}
	}
	if connection.push([]byte("overflow")) {
		t.Fatal("overflow push unexpectedly succeeded")
	}
	if !connection.push(nil) {
		t.Fatal("empty push should be a no-op")
	}

	connection.setCloseError(io.EOF)
	if err := connection.readCloseError(); !errors.Is(err, io.EOF) {
		t.Fatalf("read close error=%v", err)
	}
	if err := connection.writeCloseError(); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("write close error=%v", err)
	}
	transportErr := errors.New("transport failed")
	connection.setCloseError(transportErr)
	if !errors.Is(connection.readCloseError(), transportErr) || !errors.Is(connection.writeCloseError(), transportErr) {
		t.Fatalf("transport close errors read=%v write=%v", connection.readCloseError(), connection.writeCloseError())
	}
}

func TestDialSessionValidatesReceiverAndSessionID(t *testing.T) {
	var nilConnection *Connection
	if _, err := nilConnection.DialSession(t.Context(), "session-1", "tcp", "127.0.0.1:4096"); !errors.Is(err, ErrClosed) {
		t.Fatalf("nil DialSession error=%v", err)
	}
	connection := &Connection{}
	if _, err := connection.DialSession(t.Context(), "", "tcp", "127.0.0.1:4096"); err == nil {
		t.Fatal("empty session id unexpectedly accepted")
	}
}
