package runner

import (
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"
)

func TestSessionConnQueuedWriteHonorsDeadlineChanges(t *testing.T) {
	setters := map[string]func(*sessionConn, time.Time) error{
		"write deadline": (*sessionConn).SetWriteDeadline,
		"all deadlines": (*sessionConn).SetDeadline,
	}
	for name, setDeadline := range setters {
		t.Run(name, func(t *testing.T) {
			parent := &Connection{done: make(chan struct{})}
			connection := newSessionConn(parent, "session-1", "connection-1", "tcp", "127.0.0.1:4096")
			parent.writeMu.Lock()
			defer parent.writeMu.Unlock()

			result := make(chan error, 1)
			go func() {
				_, err := connection.Write([]byte("payload"))
				result <- err
			}()

			time.Sleep(10 * time.Millisecond)
			if err := setDeadline(connection, time.Now().Add(25*time.Millisecond)); err != nil {
				t.Fatalf("set deadline: %v", err)
			}
			select {
			case err := <-result:
				if !errors.Is(err, os.ErrDeadlineExceeded) {
					t.Fatalf("Write() error=%v want deadline exceeded", err)
				}
			case <-time.After(500 * time.Millisecond):
				t.Fatal("queued Write did not observe updated deadline")
			}
			select {
			case <-connection.done:
				t.Fatal("local write deadline unexpectedly closed the session connection")
			default:
			}
			if err := parent.Err(); err != nil {
				t.Fatalf("local write deadline unexpectedly failed parent connection: %v", err)
			}
		})
	}
}

func TestSessionConnQueuedWriteStopsWhenSessionCloses(t *testing.T) {
	parent := &Connection{done: make(chan struct{})}
	connection := newSessionConn(parent, "session-1", "connection-1", "tcp", "127.0.0.1:4096")
	parent.writeMu.Lock()
	defer parent.writeMu.Unlock()

	result := make(chan error, 1)
	go func() {
		_, err := connection.Write([]byte("payload"))
		result <- err
	}()

	time.Sleep(10 * time.Millisecond)
	connection.closeRemote(io.EOF)
	select {
	case err := <-result:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("Write() error=%v want net.ErrClosed", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("queued Write did not stop after session close")
	}
}
