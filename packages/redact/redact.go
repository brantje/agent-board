package redact

import (
	"bytes"
	"errors"
	"io"
	"sort"
)

var replacementCandidates = []string{"***", "[REDACTED]", "<redacted>", "[masked]"}

// Matcher is an immutable compiled redaction configuration. It can be reused
// safely across calls and creates independent Stream instances for stateful
// chunk processing.
type Matcher struct {
	patterns    [][]byte
	replacement []byte
	maxLen      int
}

// Compile normalizes and compiles secret values once for repeated use.
func Compile(values []string) *Matcher {
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
	return &Matcher{patterns: patterns, replacement: chooseReplacement(patterns), maxLen: maxLen}
}

func (m *Matcher) Empty() bool {
	return m == nil || m.maxLen == 0
}

func (m *Matcher) NewStream() *Stream {
	if m == nil {
		m = &Matcher{}
	}
	return &Stream{matcher: m}
}

func (m *Matcher) Bytes(data []byte) []byte {
	stream := m.NewStream()
	output := stream.Push(data)
	return append(output, stream.Flush()...)
}

func (m *Matcher) String(value string) string {
	return string(m.Bytes([]byte(value)))
}

func (m *Matcher) Reader(source io.Reader) io.Reader {
	if m.Empty() {
		return source
	}
	return &reader{source: source, redactor: m.NewStream(), buffer: make([]byte, 32*1024)}
}

type Stream struct {
	matcher       *Matcher
	pending       []byte
	outputPending []byte
}

func New(values []string) *Stream {
	return Compile(values).NewStream()
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
	return nil
}

func (s *Stream) Push(data []byte) []byte {
	if s == nil || s.matcher == nil || s.matcher.maxLen == 0 {
		return append([]byte(nil), data...)
	}
	s.pending = append(s.pending, data...)
	return s.consume(false)
}

func (s *Stream) Flush() []byte {
	if s == nil || s.matcher == nil || s.matcher.maxLen == 0 {
		return nil
	}
	return s.consume(true)
}

func (s *Stream) consume(final bool) []byte {
	var output []byte
	literalStart := 0
	position := 0
	for position < len(s.pending) {
		remaining := s.pending[position:]
		if matchLen := s.matchPrefix(remaining); matchLen > 0 {
			if position > literalStart {
				output = s.appendSanitized(output, s.pending[literalStart:position])
			}
			output = s.appendSanitized(output, s.matcher.replacement)
			position += matchLen
			literalStart = position
			continue
		}
		if !final && s.pendingIsSecretPrefix(remaining) {
			break
		}
		position++
	}
	if position > literalStart {
		output = s.appendSanitized(output, s.pending[literalStart:position])
	}
	if position > 0 {
		s.pending = s.pending[position:]
	}
	if final {
		output = s.flushSanitizedInto(output)
	}
	return output
}

func (s *Stream) matchPrefix(data []byte) int {
	for _, pattern := range s.matcher.patterns {
		if len(data) >= len(pattern) && bytes.HasPrefix(data, pattern) {
			return len(pattern)
		}
	}
	return 0
}

func (s *Stream) pendingIsSecretPrefix(data []byte) bool {
	for _, pattern := range s.matcher.patterns {
		if len(data) < len(pattern) && bytes.HasPrefix(pattern, data) {
			return true
		}
	}
	return false
}

// appendSanitized processes contiguous safe data as one unit. The input parser
// has already removed configured secrets from literal runs and replacements are
// chosen not to contain configured secrets, so any remaining match can only be
// introduced by joining output boundaries. Searching once per run/replacement
// avoids the previous per-byte O(n*P*maxLen^2) suffix work.
func (s *Stream) appendSanitized(output, data []byte) []byte {
	if len(data) == 0 {
		return output
	}
	s.outputPending = append(s.outputPending, data...)
	s.removeSecretOccurrences()
	return s.emitSafePrefix(output)
}

func (s *Stream) removeSecretOccurrences() {
	for {
		matchIndex := -1
		matchLen := 0
		for _, pattern := range s.matcher.patterns {
			index := bytes.Index(s.outputPending, pattern)
			if index < 0 {
				continue
			}
			if matchIndex < 0 || index < matchIndex || (index == matchIndex && len(pattern) > matchLen) {
				matchIndex = index
				matchLen = len(pattern)
			}
		}
		if matchIndex < 0 {
			return
		}
		copy(s.outputPending[matchIndex:], s.outputPending[matchIndex+matchLen:])
		s.outputPending = s.outputPending[:len(s.outputPending)-matchLen]
	}
}

func (s *Stream) emitSafePrefix(output []byte) []byte {
	keep := s.longestSecretPrefixSuffix()
	emitLen := len(s.outputPending) - keep
	if emitLen <= 0 {
		return output
	}
	output = append(output, s.outputPending[:emitLen]...)
	copy(s.outputPending, s.outputPending[emitLen:])
	s.outputPending = s.outputPending[:keep]
	return output
}

func (s *Stream) longestSecretPrefixSuffix() int {
	longest := 0
	for _, pattern := range s.matcher.patterns {
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

func (s *Stream) flushSanitizedInto(output []byte) []byte {
	output = append(output, s.outputPending...)
	s.outputPending = nil
	return output
}

func Bytes(data []byte, values []string) []byte {
	return Compile(values).Bytes(data)
}

func String(value string, values []string) string {
	return Compile(values).String(value)
}

type reader struct {
	source     io.Reader
	redactor   *Stream
	output     []byte
	pendingErr error
	buffer     []byte
}

func NewReader(source io.Reader, values []string) io.Reader {
	return Compile(values).Reader(source)
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
