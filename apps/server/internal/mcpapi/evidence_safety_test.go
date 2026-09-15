package mcpapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestExecutionSessionDTOOmitsSensitiveRuntimeContext(t *testing.T) {
	now := time.Date(2026, 9, 15, 16, 40, 0, 0, time.UTC)
	value := executionSessionDTO(store.ExecutionSession{
		ID:                "session-1",
		RuntimeInstanceID: "runtime-instance-1",
		RunnerID:          "runner-1",
		Status:            "RUNNING",
		CWD:               "/var/lib/agent-board/workspaces/private-repository",
		CommandArgv:       json.RawMessage(`["tool","--token","super-secret-token"]`),
		CreatedAt:         now,
		UpdatedAt:         now,
	})

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	payload := string(raw)
	for _, forbidden := range []string{"\"cwd\"", "\"command\"", "/var/lib/agent-board/workspaces/private-repository", "--token", "super-secret-token"} {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("serialized session DTO contains %q: %s", forbidden, payload)
		}
	}
}
