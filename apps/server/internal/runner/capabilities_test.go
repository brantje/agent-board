package runner

import (
	"encoding/json"
	"testing"
)

func TestMaxActiveSessions(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int
	}{
		{name: "default", raw: `{}`, want: 10},
		{name: "configured", raw: `{"max_active_sessions":4}`, want: 4},
		{name: "invalid", raw: `{"max_active_sessions":0}`, want: 10},
		{name: "malformed", raw: `{`, want: 10},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := MaxActiveSessions(json.RawMessage(tc.raw)); got != tc.want {
				t.Fatalf("MaxActiveSessions()=%d want %d", got, tc.want)
			}
		})
	}
}
