package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func newRegisteredProjectRunnerStore() *runnerAPIStore {
	ownerID := projectID
	now := time.Now().UTC()
	return &runnerAPIStore{value: store.Runner{
		ID:           otherID,
		ProjectID:    &ownerID,
		Name:         "owned-host",
		TokenHash:    make([]byte, 32),
		RegisteredAt: &now,
	}}
}

func TestProjectRunnerLifecycleRoutes(t *testing.T) {
	t.Run("rotate", func(t *testing.T) {
		memory := newRegisteredProjectRunnerStore()
		router := NewRouter(app.New(memory))
		response := runnerAPIRequest(router, http.MethodPost, "/api/projects/"+projectID+"/runners/"+otherID+"/rotate-token", "")
		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("rotate=%d %s", response.Code, response.Body.String())
		}
		var body struct {
			RunnerToken string `json:"runnerToken"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.RunnerToken == "" {
			t.Fatalf("rotate body=%#v err=%v", body, err)
		}
	})

	t.Run("revoke", func(t *testing.T) {
		memory := newRegisteredProjectRunnerStore()
		router := NewRouter(app.New(memory))
		response := runnerAPIRequest(router, http.MethodPost, "/api/projects/"+projectID+"/runners/"+otherID+"/revoke", "")
		if response.Code != http.StatusOK {
			t.Fatalf("revoke=%d %s", response.Code, response.Body.String())
		}
		if memory.value.RevokedAt == nil || memory.value.DeletedAt != nil {
			t.Fatalf("revoke state=%+v", memory.value)
		}
	})

	t.Run("delete", func(t *testing.T) {
		memory := newRegisteredProjectRunnerStore()
		router := NewRouter(app.New(memory))
		response := runnerAPIRequest(router, http.MethodDelete, "/api/projects/"+projectID+"/runners/"+otherID, "")
		if response.Code != http.StatusNoContent {
			t.Fatalf("delete=%d %s", response.Code, response.Body.String())
		}
		if memory.value.RevokedAt == nil || memory.value.DeletedAt == nil {
			t.Fatalf("delete state=%+v", memory.value)
		}
	})
}
