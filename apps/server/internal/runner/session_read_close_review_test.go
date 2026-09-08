package runner

import (
	"errors"
	"io"
	"testing"
	"time"
)

func TestSessionConnReadDrainsBufferedDataBeforeCloseError(t *testing.T) {
	for attempt := 0; attempt < 32; attempt++ {
		connection := newSessionConn(nil, "session-1", "connection-1", "tcp", "127.0.0.1:4096")
		connection.setCloseError(io.EOF)

		type readResult struct {
			n    int
			data byte
			err  error
		}
		result := make(chan readResult, 1)
		go func() {
			buf := make([]byte, 1)
			n, err := connection.Read(buf)
			var data byte
			if n > 0 {
				data = buf[0]
			}
			result <- readResult{n: n, data: data, err: err}
		}()

		// Give Read a chance to reach its blocking select, then make data and the
		// close signal ready together. Read must consume the queued byte first.
		time.Sleep(time.Millisecond)
		connection.incoming <- []byte{'x'}
		close(connection.done)

		select {
		case got := <-result:
			if got.err != nil || got.n != 1 || got.data != 'x' {
				t.Fatalf("attempt %d first read n=%d data=%q err=%v", attempt, got.n, got.data, got.err)
			}
		case <-time.After(time.Second):
			t.Fatalf("attempt %d first read did not return", attempt)
		}

		buf := make([]byte, 1)
		if n, err := connection.Read(buf); n != 0 || !errors.Is(err, io.EOF) {
			t.Fatalf("attempt %d close read n=%d err=%v", attempt, n, err)
		}
	}
}
