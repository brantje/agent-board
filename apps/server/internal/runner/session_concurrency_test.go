package runner

import (
	"io"
	"testing"
	"time"
)

func TestOutputStreamDrainsAfterReaderFailure(t *testing.T) {
	stream := newOutputStream()
	if err := stream.reader.Close(); err != nil {
		t.Fatalf("close reader: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 128; i++ {
			stream.push([]byte("chunk"))
		}
		stream.close(nil)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("output stream blocked after the reader failed")
	}
}

func TestOutputStreamCloseUnblocksConcurrentPush(t *testing.T) {
	stream := newOutputStream()
	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		for i := 0; i < 256; i++ {
			stream.push([]byte("chunk"))
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for len(stream.queue) < cap(stream.queue) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(stream.queue) < cap(stream.queue) {
		_ = stream.reader.Close()
		t.Fatal("test did not fill the output queue")
	}

	stream.close(io.ErrUnexpectedEOF)
	select {
	case <-producerDone:
	case <-time.After(2 * time.Second):
		_ = stream.reader.Close()
		t.Fatal("concurrent push remained blocked during stream close")
	}
	if err := stream.reader.Close(); err != nil {
		t.Fatalf("close reader: %v", err)
	}
}

func TestSessionOutputAbandonmentUnblocksBlockedWriter(t *testing.T) {
	session := newSession("session-abandon", &Connection{})
	defer session.stderr.close(nil)

	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		for i := 0; i < 256; i++ {
			session.stdout.push([]byte("chunk"))
		}
		session.stdout.close(nil)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for len(session.stdout.queue) < cap(session.stdout.queue) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(session.stdout.queue) < cap(session.stdout.queue) {
		_ = session.stdout.reader.Close()
		t.Fatal("test did not block stdout delivery")
	}

	if err := AbandonStdout(session); err != nil {
		t.Fatalf("AbandonStdout() error=%v", err)
	}
	select {
	case <-producerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("stdout producer remained blocked after abandonment")
	}
}
