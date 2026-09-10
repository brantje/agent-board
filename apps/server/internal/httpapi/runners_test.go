package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type runnerAPIStore struct {
	fakeControlPlaneStore
	value    store.Runner
	attached []string
	err      error
}

func (s *runnerAPIStore) CreateRunner(_ context.Context, r store.Runner) (store.Runner, error) {
	if s.err != nil {
		return store.Runner{}, s.err
	}
	r.ID = otherID
	s.value = r
	return r, nil
}
func (s *runnerAPIStore) GetRunner(_ context.Context, id string) (store.Runner, error) {
	if s.err != nil {
		return store.Runner{}, s.err
	}
	if id != s.value.ID || s.value.DeletedAt != nil {
		return store.Runner{}, store.ErrNotFound
	}
	return s.value, nil
}
func (s *runnerAPIStore) ListRunners(context.Context) ([]store.Runner, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.value.ID == "" || s.value.DeletedAt != nil {
		return nil, nil
	}
	return []store.Runner{s.value}, nil
}
func (s *runnerAPIStore) RenameRunner(_ context.Context, id, name string) (store.Runner, error) {
	if s.err != nil {
		return store.Runner{}, s.err
	}
	if id != s.value.ID {
		return store.Runner{}, store.ErrNotFound
	}
	s.value.Name = name
	return s.value, nil
}
func (s *runnerAPIStore) RotateRunnerCredential(_ context.Context, id string, hash []byte) (store.Runner, error) {
	if s.err != nil {
		return store.Runner{}, s.err
	}
	if id != s.value.ID {
		return store.Runner{}, store.ErrNotFound
	}
	s.value.TokenHash = hash
	return s.value, nil
}
func (s *runnerAPIStore) RevokeRunner(_ context.Context, id string, deleted bool) (store.Runner, error) {
	if s.err != nil {
		return store.Runner{}, s.err
	}
	if id != s.value.ID {
		return store.Runner{}, store.ErrNotFound
	}
	now := time.Now().UTC()
	s.value.RevokedAt = &now
	if deleted {
		s.value.DeletedAt = &now
	}
	return s.value, nil
}
func (s *runnerAPIStore) ObserveRunner(context.Context, string, json.RawMessage) error { return nil }
func (s *runnerAPIStore) ListProjectRunnerIDs(context.Context, string) ([]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.attached, nil
}
func (s *runnerAPIStore) SetProjectRunnerIDs(_ context.Context, _ string, ids []string) error {
	if s.err != nil {
		return s.err
	}
	s.attached = append([]string(nil), ids...)
	return nil
}

func TestRunnerAPIPublicLifecycle(t *testing.T) {
	memory := &runnerAPIStore{}
	router := NewRouter(app.New(memory))

	createdResponse := runnerAPIRequest(router, http.MethodPost, "/api/runners", `{"name":"Build host"}`)
	if createdResponse.Code != http.StatusCreated || createdResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("create %d %s", createdResponse.Code, createdResponse.Body.String())
	}
	var created struct {
		Runner RunnerDTO `json:"runner"`
		Token  string    `json:"token"`
	}
	if err := json.Unmarshal(createdResponse.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Token == "" || created.Runner.ID != otherID {
		t.Fatal("missing runner identity or one-time token")
	}

	for _, path := range []string{"/api/runners", "/api/runners/" + otherID} {
		response := runnerAPIRequest(router, http.MethodGet, path, "")
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s -> %d", path, response.Code)
		}
		for _, secret := range []string{created.Token, "tokenHash", "TokenHash", "token_hash"} {
			if strings.Contains(response.Body.String(), secret) {
				t.Fatalf("credential leaked from %s", path)
			}
	}

	rename := runnerAPIRequest(router, http.MethodPatch, "/api/runners/"+otherID, `{"name":"Edge host"}`)
	if rename.Code != http.StatusOK || !strings.Contains(rename.Body.String(), "Edge host") {
		t.Fatalf("rename %d %s", rename.Code, rename.Body.String())
	}

	rotate := runnerAPIRequest(router, http.MethodPost, "/api/runners/"+otherID+"/rotate-token", "")
	var rotated struct {
		Token string `json:"token"`
	}
	if rotate.Code != http.StatusOK || rotate.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("rotate %d %s", rotate.Code, rotate.Body.String())
	}
	if err := json.Unmarshal(rotate.Body.Bytes(), &rotated); err != nil || rotated.Token == "" || rotated.Token == created.Token {
		t.Fatalf("rotated token=%q err=%v", rotated.Token, err)
	}

	allow := runnerAPIRequest(router, http.MethodPut, "/api/projects/"+projectID+"/runners", `{"runnerIds":["`+otherID+`"]}`)
	listed := runnerAPIRequest(router, http.MethodGet, "/api/projects/"+projectID+"/runners", "")
	if allow.Code != http.StatusOK || listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), otherID) {
		t.Fatalf("project runners set=%d get=%d body=%s", allow.Code, listed.Code, listed.Body.String())
	}
	cleared := runnerAPIRequest(router, http.MethodPut, "/api/projects/"+projectID+"/runners", `{}`)
	if cleared.Code != http.StatusOK || !strings.Contains(cleared.Body.String(), `"runnerIds":[]`) {
		t.Fatalf("clear project runners %d %s", cleared.Code, cleared.Body.String())
	}

	revoke := runnerAPIRequest(router, http.MethodPost, "/api/runners/"+otherID+"/revoke", "")
	if revoke.Code != http.StatusOK || !strings.Contains(revoke.Body.String(), `"revokedAt"`) {
		t.Fatalf("revoke %d %s", revoke.Code, revoke.Body.String())
	}
	deleted := runnerAPIRequest(router, http.MethodDelete, "/api/runners/"+otherID, "")
	missing := runnerAPIRequest(router, http.MethodGet, "/api/runners/"+otherID, "")
	if deleted.Code != http.StatusNoContent || missing.Code != http.StatusNotFound {
		t.Fatalf("delete=%d subsequent get=%d", deleted.Code, missing.Code)
	}
}

func TestRunnerAPIValidationAndStoreFailure(t *testing.T) {
	router := NewRouter(app.New(&runnerAPIStore{}))
	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/runners/not-a-uuid", ""},
		{http.MethodPut, "/api/projects/not-a-uuid/runners", `{"runnerIds":[]}`},
		{http.MethodPut, "/api/projects/" + projectID + "/runners", `{"runnerIds":["not-a-uuid"]}`},
		{http.MethodPatch, "/api/runners/" + otherID, `{`},
	} {
		if response := runnerAPIRequest(router, tc.method, tc.path, tc.body); response.Code != http.StatusBadRequest {
			t.Fatalf("%s %s -> %d", tc.method, tc.path, response.Code)
		}
	}

	failed := runnerAPIRequest(NewRouter(app.New(&runnerAPIStore{err: store.ErrNotFound})), http.MethodGet, "/api/runners", "")
	if failed.Code < http.StatusBadRequest {
		t.Fatalf("store failure returned %d", failed.Code)
	}
}

func runnerAPIRequest(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(body)))
	return response
}
