package app

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestAuthorizedExecutionReleaseWaitsForTerminalAndBothStreams(t *testing.T) {
	releases := 0
	process := &AuthorizedExecutionProcess{release: func() { releases++ }}

	process.markTerminal()
	if releases != 0 {
		t.Fatalf("release after terminal only=%d", releases)
	}
	process.markStdoutDone()
	if releases != 0 {
		t.Fatalf("release before stderr completion=%d", releases)
	}
	process.markStderrDone()
	if releases != 1 {
		t.Fatalf("release after full lifecycle=%d, want 1", releases)
	}

	process.markTerminal()
	process.markStdoutDone()
	process.markStderrDone()
	if releases != 1 {
		t.Fatalf("release repeated after lifecycle replay=%d", releases)
	}
}

func TestCompletionReaderCoversBufferedSettledAndSourceErrors(t *testing.T) {
	t.Run("buffer drains into final error", func(t *testing.T) {
		finalErr := errors.New("terminal read error")
		reader := &completionReader{buffer: bytes.NewReader([]byte("buffered")), settled: true, finalErr: finalErr}
		data, err := io.ReadAll(reader)
		if string(data) != "buffered" || !errors.Is(err, finalErr) {
			t.Fatalf("data=%q err=%v", data, err)
		}
	})

	t.Run("settled reader returns eof", func(t *testing.T) {
		reader := &completionReader{settled: true}
		buffer := make([]byte, 1)
		if n, err := reader.Read(buffer); n != 0 || !errors.Is(err, io.EOF) {
			t.Fatalf("Read()=(%d,%v), want (0,EOF)", n, err)
		}
	})

	t.Run("source error settles and completes once", func(t *testing.T) {
		sourceErr := errors.New("source failed")
		completed := 0
		reader := &completionReader{source: failingReader{err: sourceErr}, onDone: func() { completed++ }}
		buffer := make([]byte, 4)
		if n, err := reader.Read(buffer); n != 0 || !errors.Is(err, sourceErr) {
			t.Fatalf("first Read()=(%d,%v)", n, err)
		}
		if n, err := reader.Read(buffer); n != 0 || !errors.Is(err, sourceErr) {
			t.Fatalf("second Read()=(%d,%v)", n, err)
		}
		if completed != 1 {
			t.Fatalf("completion callbacks=%d, want 1", completed)
		}
	})
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }
