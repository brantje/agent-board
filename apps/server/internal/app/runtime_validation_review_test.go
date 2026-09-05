package app

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestValidateRuntimeRejectsNonPositiveOptionalLimits(t *testing.T) {
	zero := 0
	negative := -1
	zero64 := int64(0)
	negative64 := int64(-1)

	base := store.Runtime{
		Name:            "runtime",
		Kind:            "docker",
		Image:           "image",
		NetworkPolicy:   "none",
		WorkspacePolicy: "issue",
		Capabilities:    store.EmptyObject,
	}

	cases := []struct {
		name   string
		mutate func(*store.Runtime)
	}{
		{name: "zero cpu", mutate: func(v *store.Runtime) { v.CPULimitMillis = &zero }},
		{name: "negative cpu", mutate: func(v *store.Runtime) { v.CPULimitMillis = &negative }},
		{name: "zero memory", mutate: func(v *store.Runtime) { v.MemoryLimitBytes = &zero64 }},
		{name: "negative memory", mutate: func(v *store.Runtime) { v.MemoryLimitBytes = &negative64 }},
		{name: "zero pid", mutate: func(v *store.Runtime) { v.PIDLimit = &zero }},
		{name: "negative pid", mutate: func(v *store.Runtime) { v.PIDLimit = &negative }},
		{name: "zero timeout", mutate: func(v *store.Runtime) { v.TimeoutSeconds = &zero }},
		{name: "negative timeout", mutate: func(v *store.Runtime) { v.TimeoutSeconds = &negative }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value := base
			tc.mutate(&value)
			if err := validateRuntime(value); err == nil {
				t.Fatal("expected invalid runtime limit")
			}
		})
	}

	one := 1
	one64 := int64(1)
	valid := base
	valid.CPULimitMillis = &one
	valid.MemoryLimitBytes = &one64
	valid.PIDLimit = &one
	valid.TimeoutSeconds = &one
	if err := validateRuntime(valid); err != nil {
		t.Fatalf("minimum valid limits rejected: %v", err)
	}
}
