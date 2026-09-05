package session

import (
	"io"
	"sync"
)

type safeWriteCloser struct {
	mu     sync.Mutex
	writer io.WriteCloser
	closed bool
}

func newSafeWriteCloser(writer io.WriteCloser) io.WriteCloser {
	return &safeWriteCloser{writer: writer}
}

func (w *safeWriteCloser) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, io.ErrClosedPipe
	}
	return w.writer.Write(data)
}

func (w *safeWriteCloser) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	return w.writer.Close()
}
