package redact

import (
	"bytes"
	"errors"
	"io"
	"sort"
)

var replacement = []byte("***")

type Stream struct {
	patterns [][]byte
	maxLen   int
	pending  []byte
}

func New(values []string) *Stream {
	seen := make(map[string]struct{}, len(values))
	patterns := make([][]byte, 0, len(values))
	maxLen := 0
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		pattern := []byte(value)
		patterns = append(patterns, pattern)
		if len(pattern) > maxLen {
			maxLen = len(pattern)
		}
	}
	sort.Slice(patterns, func(i, j int) bool { return len(patterns[i]) > len(patterns[j]) })
	return &Stream{patterns: patterns, maxLen: maxLen}
}

func (s *Stream) Push(data []byte) []byte {
	if s.maxLen == 0 {
		return append([]byte(nil), data...)
	}
	s.pending = append(s.pending, data...)
	return s.consume(false)
}

func (s *Stream) Flush() []byte {
	if s.maxLen == 0 || len(s.pending) == 0 {
		return nil
	}
	return s.consume(true)
}

func (s *Stream) consume(final bool) []byte {
	var output []byte
	for len(s.pending) > 0 {
		matched := false
		for _, pattern := range s.patterns {
			if len(s.pending) >= len(pattern) && bytes.HasPrefix(s.pending, pattern) {
				output = append(output, replacement...)
				s.pending = s.pending[len(pattern):]
				matched = true
				break
			}
		}
		if matched {
			continue
		}

		// Keep only a suffix that can still become a secret when the next
		// source chunk arrives. Everything before it is known-safe and can be
		// emitted immediately, which is important for interactive processes.
		if !final && s.pendingIsSecretPrefix() {
			break
		}
		output = append(output, s.pending[0])
		s.pending = s.pending[1:]
	}
	return output
}

func (s *Stream) pendingIsSecretPrefix() bool {
	for _, pattern := range s.patterns {
		if len(s.pending) < len(pattern) && bytes.HasPrefix(pattern, s.pending) {
			return true
		}
	}
	return false
}

type reader struct {
	source     io.Reader
	redactor   *Stream
	output     []byte
	pendingErr error
	buffer     []byte
}

func NewReader(source io.Reader, values []string) io.Reader {
	stream := New(values)
	if stream.maxLen == 0 {
		return source
	}
	return &reader{source: source, redactor: stream, buffer: make([]byte, 32*1024)}
}

func (r *reader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(r.output) == 0 {
		if r.pendingErr != nil {
			err := r.pendingErr
			r.pendingErr = nil
			return 0, err
		}

		n, err := r.source.Read(r.buffer)
		if n > 0 {
			r.output = append(r.output, r.redactor.Push(r.buffer[:n])...)
		}
		if err != nil {
			r.output = append(r.output, r.redactor.Flush()...)
			if errors.Is(err, io.EOF) {
				r.pendingErr = io.EOF
			} else {
				r.pendingErr = err
			}
		}
		if n == 0 && err == nil {
			return 0, nil
		}
	}

	n := copy(p, r.output)
	r.output = r.output[n:]
	return n, nil
}
