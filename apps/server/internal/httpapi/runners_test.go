package httpapi

import (
	"context"
	"encoding/json"
	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"net/http/httptest"
	"strings"
	"testing"
)

type runnerAPIStore struct {
	fakeControlPlaneStore
	store.RunnerStore
	value store.Runner
}

func (s *runnerAPIStore) CreateRunner(_ context.Context, r store.Runner) (store.Runner, error) {
	r.ID = otherID
	s.value = r
	return r, nil
}
func (s *runnerAPIStore) GetRunner(_ context.Context, id string) (store.Runner, error) {
	if id != s.value.ID {
		return store.Runner{}, store.ErrNotFound
	}
	return s.value, nil
}
func (s *runnerAPIStore) ListRunners(context.Context) ([]store.Runner, error) {
	return []store.Runner{s.value}, nil
}

func TestRunnerAPIOneTimeTokenAndPublicDTO(t *testing.T) {
	memory := &runnerAPIStore{}
	router := NewRouter(app.New(memory))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("POST", "/api/runners", strings.NewReader(`{"name":"Build host"}`)))
	if response.Code != 201 {
		t.Fatalf("create %d %s", response.Code, response.Body.String())
	}
	var created struct {
		Runner RunnerDTO `json:"runner"`
		Token  string    `json:"token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Token == "" || created.Runner.ID != otherID {
		t.Fatal("missing identity/token")
	}
	for _, path := range []string{"/api/runners", "/api/runners/" + otherID} {
		response = httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != 200 {
			t.Fatal(response.Code)
		}
		for _, secret := range []string{created.Token, "tokenHash", "TokenHash", "token_hash"} {
			if strings.Contains(response.Body.String(), secret) {
				t.Fatal("credential leaked")
			}
		}
	}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("POST", "/api/runners", strings.NewReader(`{"name":"","internal":true}`)))
	if response.Code != 400 {
		t.Fatal("caller can supply internal configuration")
	}
}
