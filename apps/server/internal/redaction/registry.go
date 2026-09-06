package redaction

import (
	"encoding/json"
	"errors"
	"io"
	"sync"

	shared "github.com/brantje/agent-board/packages/redact"
)

type runRegistration struct {
	values     []string
	references int
}

type Registry struct {
	mu sync.RWMutex

	runs map[string]runRegistration

	cacheValid   bool
	cachedValues []string
	cachedAll    *shared.Matcher
}

func NewRegistry() *Registry {
	return &Registry{runs: make(map[string]runRegistration)}
}

func (r *Registry) Register(runID string, values []string) {
	if r == nil || runID == "" {
		return
	}
	normalized := uniqueValues(values)
	if len(normalized) == 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	registration := r.runs[runID]
	registration.references++
	registration.values = mergeValues(registration.values, normalized)
	r.runs[runID] = registration
	r.invalidateCacheLocked()
}

// Release drops one active execution reference for a Run. Values remain active
// until every registration for that Run has been released.
func (r *Registry) Release(runID string) {
	if r == nil || runID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	registration, ok := r.runs[runID]
	if !ok {
		return
	}
	if registration.references > 1 {
		registration.references--
		r.runs[runID] = registration
	} else {
		delete(r.runs, runID)
	}
	r.invalidateCacheLocked()
}

func (r *Registry) Values(runID string) []string {
	if r == nil || runID == "" {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.runs[runID].values...)
}

func (r *Registry) AllValues() []string {
	values, _ := r.allState()
	return append([]string(nil), values...)
}

func (r *Registry) RedactString(runID, value string) string {
	return shared.Compile(r.Values(runID)).String(value)
}

func (r *Registry) RedactAllString(value string) string {
	_, matcher := r.allState()
	return matcher.String(value)
}

func (r *Registry) RedactBytes(runID string, value []byte) []byte {
	return shared.Compile(r.Values(runID)).Bytes(value)
}

func (r *Registry) Reader(runID string, source io.Reader) io.Reader {
	return shared.Compile(r.Values(runID)).Reader(source)
}

func (r *Registry) RedactJSON(runID string, raw json.RawMessage) (json.RawMessage, error) {
	matcher := shared.Compile(r.Values(runID))
	if len(raw) == 0 || matcher.Empty() {
		return append(json.RawMessage(nil), raw...), nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	value = redactJSONValue(value, matcher)
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func (r *Registry) allState() ([]string, *shared.Matcher) {
	if r == nil {
		return nil, shared.Compile(nil)
	}
	r.mu.RLock()
	if r.cacheValid {
		values := r.cachedValues
		matcher := r.cachedAll
		r.mu.RUnlock()
		return values, matcher
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.cacheValid {
		seen := make(map[string]struct{})
		values := make([]string, 0)
		for _, registration := range r.runs {
			for _, value := range registration.values {
				if _, exists := seen[value]; exists {
					continue
				}
				seen[value] = struct{}{}
				values = append(values, value)
			}
		}
		r.cachedValues = values
		r.cachedAll = shared.Compile(values)
		r.cacheValid = true
	}
	return r.cachedValues, r.cachedAll
}

func (r *Registry) invalidateCacheLocked() {
	r.cacheValid = false
	r.cachedValues = nil
	r.cachedAll = nil
}

func uniqueValues(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

func mergeValues(existing, additional []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(additional))
	merged := make([]string, 0, len(existing)+len(additional))
	for _, value := range append(append([]string(nil), existing...), additional...) {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		merged = append(merged, value)
	}
	return merged
}

func redactJSONValue(value any, matcher *shared.Matcher) any {
	switch typed := value.(type) {
	case string:
		return matcher.String(typed)
	case []any:
		for index := range typed {
			typed[index] = redactJSONValue(typed[index], matcher)
		}
		return typed
	case map[string]any:
		redacted := make(map[string]any, len(typed))
		for key, item := range typed {
			redacted[matcher.String(key)] = redactJSONValue(item, matcher)
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
	return &safeError{message: shared.Compile(values).String(err.Error()), cause: err}
}

func IsSafeWrapped(err error) bool {
	var target *safeError
	return errors.As(err, &target)
}
