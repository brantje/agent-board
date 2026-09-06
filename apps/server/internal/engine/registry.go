package engine

import (
	"fmt"
	"reflect"
	"strings"
)

type Registry struct {
	engines map[string]Engine
}

func NewRegistry(engines ...Engine) (*Registry, error) {
	r := &Registry{engines: make(map[string]Engine, len(engines))}
	for _, adapter := range engines {
		if isNilEngine(adapter) {
			return nil, fmt.Errorf("engine: adapter is required")
		}
		name := strings.TrimSpace(adapter.Name())
		if name == "" {
			return nil, fmt.Errorf("engine: adapter name is required")
		}
		if _, exists := r.engines[name]; exists {
			return nil, fmt.Errorf("engine: duplicate adapter %q", name)
		}
		r.engines[name] = adapter
	}
	return r, nil
}

func isNilEngine(adapter Engine) bool {
	if adapter == nil {
		return true
	}
	value := reflect.ValueOf(adapter)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (r *Registry) Get(name string) (Engine, error) {
	if r == nil {
		return nil, fmt.Errorf("engine: registry is unavailable")
	}
	adapter, ok := r.engines[strings.TrimSpace(name)]
	if !ok {
		return nil, fmt.Errorf("engine: adapter %q is not registered", name)
	}
	return adapter, nil
}
