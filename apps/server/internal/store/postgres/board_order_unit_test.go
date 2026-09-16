package postgres

import (
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func stringPointer(value string) *string { return &value }

func TestPlacementIndex(t *testing.T) {
	ids := []string{"a", "b", "c"}
	tests := []struct {
		name   string
		ids    []string
		before *string
		after  *string
		want   int
		fail   bool
	}{
		{name: "empty", ids: nil, want: 0},
		{name: "top", ids: ids, after: stringPointer("a"), want: 0},
		{name: "middle", ids: ids, before: stringPointer("a"), after: stringPointer("b"), want: 1},
		{name: "bottom", ids: ids, before: stringPointer("c"), want: 3},
		{name: "both null non-empty", ids: ids, fail: true},
		{name: "stale top", ids: ids, after: stringPointer("b"), fail: true},
		{name: "stale bottom", ids: ids, before: stringPointer("b"), fail: true},
		{name: "non-adjacent", ids: ids, before: stringPointer("a"), after: stringPointer("c"), fail: true},
		{name: "missing anchor", ids: ids, before: stringPointer("missing"), after: stringPointer("b"), fail: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := placementIndex(tt.ids, tt.before, tt.after)
			if tt.fail {
				if !errors.Is(err, store.ErrConflict) {
					t.Fatalf("error = %v, want ErrConflict", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("placementIndex: %v", err)
			}
			if got != tt.want {
				t.Fatalf("index = %d, want %d", got, tt.want)
			}
		})
	}
}
