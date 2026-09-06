package redaction

import (
	"encoding/json"
	"errors"
	"io"
	"sync"

	shared "github.com/brantje/agent-board/packages/redact"
)

type Registry struct {
	mu    sync.RWMutex
	runs  map[string][]string
}

func NewRegistry() *Registry {
	return &Registry{runs: make(map[string][]string)}
}

func (r *Registry) Register(runID string, values []string) {
	if r == nil || runID == "" || len(values) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := make(map[string]struct{}, len(r.runs[runID])+len(values))
	merged := make([]string, 0, len(r.runs[runID])+len(values))
	for _, value := range append(append([]string(nil), r.runs[runID]...), values...) {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		merged = append(merged, value)
	}
	if len(merged) > 0 {
		r.runs[runID] = merged
	}
}

func (r *Registry) Values(runID string) []string {
	if r == nil || runID == "" {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.runs[runID]...)
}

func (r *Registry) AllValues() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := map[string]struct{}{}
	var values []string
	for _, runValues := range r.runs {
		for _, value := range runValues {
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			values = append(values, value)
		}
	}
	return values
}

func (r *Registry) RedactString(runID, value string) string {
	return shared.String(value, r.Values(runID))
}

func (r *Registry) RedactAllString(value string) string {
	return shared.String(value, r.AllValues())
}

func (r *Registry) RedactBytes(runID string, value []byte) []byte {
	return shared.Bytes(value, r.Values(runID))
}

func (r *Registry) Reader(runID string, source io.Reader) io.Reader {
	return shared.NewReader(source, r.Values(runID))
}

func (r *Registry) RedactJSON(runID string, raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || len(r.Values(runID)) == 0 {
		return append(json.RawMessage(nil), raw...), nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	value = redactJSONValue(value, r.Values(runID))
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func redactJSONValue(value any, secrets []string) any {
	switch typed := value.(type) {
	case string:
		return shared.String(typed, secrets)
	case []any:
		for index := range typed {
			typed[index] = redactJSONValue(typed[index], secrets)
		}
		return typed
	case map[string]any:
		redacted := make(map[string]any, len(typed))
		for key, item := range typed {
			redacted[shared.String(key, secrets)] = redactJSONValue(item, secrets)
		}
		return redacted
	default:
		return value
	}
}

type safeError struct {
	message string
	cause   error
}

func (e *safeError) Error() string { return e.message }
func (e *safeError) Unwrap() error { return e.cause }

func WrapError(err error, values []string) error {
	if err == nil || len(values) == 0 {
		return err
	}
	return &safeError{message: shared.String(err.Error(), values), cause: err}
}

func IsSafeWrapped(err error) bool {
	var target *safeError
	return errors.As(err, &target)
}
