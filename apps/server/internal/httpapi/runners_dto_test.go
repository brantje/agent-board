package httpapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestRunnerDTOIncludesSessionLoad(t *testing.T) {
	memory := &runnerAPIStore{}
	memory.value = store.Runner{
		ID: "runner-1", Name: "build-host", RegisteredAt: ptrTime(time.Now()),
		Capabilities: json.RawMessage(`{"engines":["opencode","scripted"],"max_active_sessions":4}`),
	}
	api := &api{service: app.New(memory)}
	dto := api.runnerDTO(memory.value, 3)
	if dto.MaxActiveSessions != 4 || dto.ReservedSessions != 3 || dto.ActiveSessions != nil {
		t.Fatalf("dto=%+v", dto)
	}
}

func ptrTime(value time.Time) *time.Time { return &value }
