package redact

import (
	"io"

	shared "github.com/brantje/agent-board/packages/redact"
)

type Stream struct {
	inner *shared.Stream
}

func New(values []string) *Stream {
	return &Stream{inner: shared.New(values)}
}

func (s *Stream) Push(data []byte) []byte {
	return s.inner.Push(data)
}

func (s *Stream) Flush() []byte {
	return s.inner.Flush()
}

func NewReader(source io.Reader, values []string) io.Reader {
	return shared.NewReader(source, values)
}
