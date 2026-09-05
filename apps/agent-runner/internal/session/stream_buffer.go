package session

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"
)

const defaultStreamBufferLimit = 1 << 20

var ErrOutputTruncated = errors.New("session output truncated")

// streamBuffer decouples process output draining from external consumers while
// bounding the amount of unread output retained in memory.
type streamBuffer struct {
	mu      sync.Mutex
	cond    *sync.Cond
	buffer  bytes.Buffer
	limit   int
	dropped int64
	closed  bool
	err     error
}

func newStreamBuffer() *streamBuffer {
	return newStreamBufferWithLimit(defaultStreamBufferLimit)
}

func newStreamBufferWithLimit(limit int) *streamBuffer {
	if limit < 1 {
		limit = 1
	}
	buffer := &streamBuffer{limit: limit}
	buffer.cond = sync.NewCond(&buffer.mu)
	return buffer
}

func (b *streamBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0, io.ErrClosedPipe
	}

	written := len(p)
	if written >= b.limit {
		b.dropped += int64(b.buffer.Len() + written - b.limit)
		b.buffer.Reset()
		_, _ = b.buffer.Write(p[written-b.limit:])
		b.cond.Broadcast()
		return written, nil
	}

	if overflow := b.buffer.Len() + written - b.limit; overflow > 0 {
		b.buffer.Next(overflow)
		b.dropped += int64(overflow)
	}
	_, err := b.buffer.Write(p)
	b.cond.Broadcast()
	return written, err
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
	if b.dropped > 0 {
		truncated := fmt.Errorf("%w: %d bytes dropped", ErrOutputTruncated, b.dropped)
		if b.err != nil {
			return 0, errors.Join(b.err, truncated)
		}
		return 0, truncated
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
