package postgres

import (
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestDelegationTerminalOutcomeMapsAllTerminalStates(t *testing.T) {
	tests := []struct {
		status    string
		outcome   string
		eventType string
		fallback  string
	}{
		{status: "COMPLETED", outcome: store.DelegationOutcomeSucceeded, eventType: "delegation.completed", fallback: "Delegated task completed successfully."},
		{status: "FAILED", outcome: store.DelegationOutcomeFailed, eventType: "delegation.failed", fallback: "Delegated task failed."},
		{status: "CANCELLED", outcome: store.DelegationOutcomeCancelled, eventType: "delegation.cancelled", fallback: "Delegated task was cancelled."},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			outcome, eventType, fallback, err := delegationTerminalOutcome(tt.status)
			if err != nil {
				t.Fatal(err)
			}
			if outcome != tt.outcome || eventType != tt.eventType || fallback != tt.fallback {
				t.Fatalf("outcome=%q event=%q fallback=%q", outcome, eventType, fallback)
			}
		})
	}
	if _, _, _, err := delegationTerminalOutcome("RUNNING"); err != store.ErrInvalidArgument {
		t.Fatalf("invalid status error=%v want ErrInvalidArgument", err)
	}
}

func TestBoundDelegationResultTrimsBoundsAndSuppliesFallback(t *testing.T) {
	if got := boundDelegationResult("  bounded result  "); got != "bounded result" {
		t.Fatalf("trimmed result=%q", got)
	}
	if got := boundDelegationResult("   "); got != "Delegated task produced no summary." {
		t.Fatalf("empty result=%q", got)
	}
	long := strings.Repeat("界", store.MaxDelegationResultCharacters+5)
	got := boundDelegationResult(long)
	if len([]rune(got)) != store.MaxDelegationResultCharacters {
		t.Fatalf("bounded rune length=%d want %d", len([]rune(got)), store.MaxDelegationResultCharacters)
	}
	if !strings.HasSuffix(got, "界") {
		t.Fatalf("bounded unicode result was split incorrectly")
	}
}
