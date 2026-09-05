package redact

import (
	"bytes"
	"errors"
	"io"
	"sort"
)

var replacementCandidates = []string{"***", "[REDACTED]", "<redacted>", "[masked]"}

type Stream struct {
	patterns      [][]byte
	replacement   []byte
	maxLen        int
	pending       []byte
	outputPending []byte
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
	return &Stream{
		patterns:    patterns,
		replacement: chooseReplacement(patterns),
		maxLen:      maxLen,
	}
}

func chooseReplacement(patterns [][]byte) []byte {
	for _, candidate := range replacementCandidates {
		encoded := []byte(candidate)
		safe := true
		for _, pattern := range patterns {
			if bytes.Contains(encoded, pattern) || bytes.Contains(pattern, encoded) {
				safe = false
				break
			}
		}
		if safe {
			return encoded
		}
	}
	// The output sanitizer below is the final secrecy boundary. Dropping a
	// matched value here avoids choosing a human-readable marker that directly
	// collides with the configured secret set.
	return nil
}

func (s *Stream) Push(data []byte) []byte {
	if s.maxLen == 0 {
		return append([]byte(nil), data...)
	}
	s.pending = append(s.pending, data...)
	return s.consume(false)
}

func (s *Stream) Flush() []byte {
	if s.maxLen == 0 {
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
				output = append(output, s.appendSanitized(s.replacement)...)
				s.pending = s.pending[len(pattern):]
				matched = true
				break
			}
		}
		if matched {
			continue
		}

		// Keep raw input that can still become a secret when the next source
		// chunk arrives. Everything before it can be transformed immediately.
		if !final && s.pendingIsSecretPrefix() {
			break
		}
		output = append(output, s.appendSanitized(s.pending[:1])...)
		s.pending = s.pending[1:]
	}
	if final {
		output = append(output, s.flushSanitized()...)
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

// appendSanitized keeps a small transformed-output suffix until it is certain
// future bytes cannot turn that suffix into a configured secret. Any secret
// reconstructed by a replacement marker or by bytes adjacent to a replacement
// is removed before the suffix is emitted.
func (s *Stream) appendSanitized(data []byte) []byte {
	var output []byte
	for _, value := range data {
		s.outputPending = append(s.outputPending, value)
		s.removeSecretSuffixes()
		output = append(output, s.emitSafePrefix()...)
	}
	return output
}

func (s *Stream) removeSecretSuffixes() {
	for {
		matched := false
		for _, pattern := range s.patterns {
			if len(s.outputPending) >= len(pattern) && bytes.HasSuffix(s.outputPending, pattern) {
				s.outputPending = s.outputPending[:len(s.outputPending)-len(pattern)]
				matched = true
				break
			}
		}
		if !matched {
			return
		}
	}
}

func (s *Stream) emitSafePrefix() []byte {
	keep := s.longestSecretPrefixSuffix()
	emitLen := len(s.outputPending) - keep
	if emitLen <= 0 {
		return nil
	}

	output := append([]byte(nil), s.outputPending[:emitLen]...)
	copy(s.outputPending, s.outputPending[emitLen:])
	s.outputPending = s.outputPending[:keep]
	return output
}

func (s *Stream) longestSecretPrefixSuffix() int {
	longest := 0
	for _, pattern := range s.patterns {
		limit := len(s.outputPending)
		if patternLimit := len(pattern) - 1; limit > patternLimit {
			limit = patternLimit
		}
		for length := limit; length > longest; length-- {
			if bytes.Equal(s.outputPending[len(s.outputPending)-length:], pattern[:length]) {
				longest = length
				break
			}
		}
	}
	return longest
}

func (s *Stream) flushSanitized() []byte {
	output := append([]byte(nil), s.outputPending...)
	s.outputPending = nil
	return output
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
