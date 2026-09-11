package runner

import (
	"encoding/json"
	"testing"
)

func TestWithConfiguredMaxActiveSessions(t *testing.T) {
	got := WithConfiguredMaxActiveSessions(json.RawMessage(`{"engines":["opencode"]}`), 4)
	var payload map[string]any
	if err := json.Unmarshal(got, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["max_active_sessions"] != float64(4) || payload["max_active_sessions_configured"] != true {
		t.Fatalf("payload=%v", payload)
	}
	engines, ok := payload["engines"].([]any)
	if !ok || len(engines) != 1 || engines[0] != "opencode" {
		t.Fatalf("engines=%v", payload["engines"])
	}
}

func TestMergeObservedCapabilitiesPreservesConfiguredCapacity(t *testing.T) {
	existing := json.RawMessage(`{"engines":["opencode"],"max_active_sessions":4,"max_active_sessions_configured":true}`)
	observed := json.RawMessage(`{"engines":["opencode","scripted"],"max_active_sessions":10,"os":"linux"}`)
	got := MergeObservedCapabilities(existing, observed)
	var payload map[string]any
	if err := json.Unmarshal(got, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["max_active_sessions"] != float64(4) {
		t.Fatalf("max_active_sessions=%v want 4", payload["max_active_sessions"])
	}
	if payload["max_active_sessions_configured"] != true {
		t.Fatalf("configured flag=%v want true", payload["max_active_sessions_configured"])
	}
	engines, ok := payload["engines"].([]any)
	if !ok || len(engines) != 2 {
		t.Fatalf("engines=%v want merged observed engines", payload["engines"])
	}
}

func TestMergeObservedCapabilitiesUsesAdvertisedCapacityWhenNotConfigured(t *testing.T) {
	got := MergeObservedCapabilities(json.RawMessage(`{"max_active_sessions":4}`), json.RawMessage(`{"max_active_sessions":10,"engines":["scripted"]}`))
	var payload map[string]any
	if err := json.Unmarshal(got, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["max_active_sessions"] != float64(10) {
		t.Fatalf("max_active_sessions=%v want 10", payload["max_active_sessions"])
	}
}

func TestMaxActiveSessions(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int
	}{
		{name: "default", raw: `{}`, want: 5},
		{name: "configured", raw: `{"max_active_sessions":4}`, want: 4},
		{name: "invalid", raw: `{"max_active_sessions":0}`, want: 5},
		{name: "malformed", raw: `{`, want: 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := MaxActiveSessions(json.RawMessage(tc.raw)); got != tc.want {
				t.Fatalf("MaxActiveSessions()=%d want %d", got, tc.want)
			}
		})
	}
}
