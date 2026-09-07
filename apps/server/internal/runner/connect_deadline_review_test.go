package runner

import (
	"errors"
	"os"
	"testing"
	"time"
)

func TestSessionConnInFlightReadObservesReadDeadlineChange(t *testing.T) {
	connection := newSessionConn(nil, "session-1", "connection-1", "tcp", "127.0.0.1:4096")
	result := make(chan error, 1)
	go func() {
		_, err := connection.Read(make([]byte, 1))
		result <- err
	}()

	time.Sleep(10 * time.Millisecond)
	if err := connection.SetReadDeadline(time.Now().Add(20 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("Read() error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked Read did not observe SetReadDeadline")
	}
}

func TestSessionConnInFlightReadObservesSetDeadline(t *testing.T) {
	connection := newSessionConn(nil, "session-1", "connection-1", "tcp", "127.0.0.1:4096")
	result := make(chan error, 1)
	go func() {
		_, err := connection.Read(make([]byte, 1))
		result <- err
	}()

	time.Sleep(10 * time.Millisecond)
	if err := connection.SetDeadline(time.Now().Add(20 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("Read() error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked Read did not observe SetDeadline")
	}
}
