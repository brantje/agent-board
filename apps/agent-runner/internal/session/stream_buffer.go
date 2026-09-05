package session

import (
	"bytes"
	"io"
	"sync"
)

// streamBuffer decouples process output draining from external consumers.
// Writers append without waiting for a reader; readers still observe output in order.
type streamBuffer struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buffer bytes.Buffer
	closed bool
	err    error
}

func newStreamBuffer() *streamBuffer {
	buffer := &streamBuffer{}
	buffer.cond = sync.NewCond(&buffer.mu)
	return buffer
}

func (b *streamBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0, io.ErrClosedPipe
	}
	n, err := b.buffer.Write(p)
	b.cond.Broadcast()
	return n, err
}

func (b *streamBuffer) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	for b.buffer.Len() == 0 && !b.closed {
		b.cond.Wait()
	}
	if b.buffer.Len() > 0 {
		return b.buffer.Read(p)
	}
	if b.err != nil {
		return 0, b.err
	}
	return 0, io.EOF
}

func (b *streamBuffer) CloseWithError(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	b.err = err
	b.cond.Broadcast()
}
