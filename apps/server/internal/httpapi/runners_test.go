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
	value      store.Runner
	attached   []string
	listErr    error
	projectErr error
	setErr     error
	createErr  error
}

func (s *runnerAPIStore) CreateRunner(_ context.Context, r store.Runner) (store.Runner, error) {
	if s.createErr != nil {
		return store.Runner{}, s.createErr
	}
	r.ID = otherID
	s.value = r
	return r, nil
}
func (s *runnerAPIStore) GetRunner(_ context.Context, id string) (store.Runner, error) {
	if id != s.value.ID || s.value.DeletedAt != nil {
		return store.Runner{}, store.ErrNotFound
	}
	return s.value, nil
}
func (s *runnerAPIStore) ListRunners(context.Context) ([]store.Runner, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	if s.value.ID == "" || s.value.DeletedAt != nil {
		return nil, nil
	}
	return []store.Runner{s.value}, nil
}
func (s *runnerAPIStore) RenameRunner(_ context.Context, id, name string) (store.Runner, error) {
	if id != s.value.ID {
		return store.Runner{}, store.ErrNotFound
	}
	s.value.Name = name
	return s.value, nil
}
func (s *runnerAPIStore) RotateRunnerCredential(_ context.Context, id string, hash []byte) (store.Runner, error) {
	if id != s.value.ID {
		return store.Runner{}, store.ErrNotFound
	}
	s.value.TokenHash = hash
	return s.value, nil
}
func (s *runnerAPIStore) RevokeRunner(_ context.Context, id string, deleted bool) (store.Runner, error) {
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
	if s.projectErr != nil {
		return nil, s.projectErr
	}
	return s.attached, nil
}
func (s *runnerAPIStore) SetProjectRunnerIDs(_ context.Context, _ string, ids []string) error {
	if s.setErr != nil {
		return s.setErr
	}
	s.attached = append([]string(nil), ids...)
	return nil
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

	patch := httptest.NewRecorder()
	req := httptest.NewRequest("PATCH", "/api/runners/"+otherID, strings.NewReader(`{"name":"Edge host"}`))
	router.ServeHTTP(patch, req)
	if patch.Code != 200 || !strings.Contains(patch.Body.String(), "Edge host") {
		t.Fatalf("rename %d %s", patch.Code, patch.Body.String())
	}

	rotate := httptest.NewRecorder()
	router.ServeHTTP(rotate, httptest.NewRequest("POST", "/api/runners/"+otherID+"/rotate-token", nil))
	if rotate.Code != 200 || rotate.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("rotate %d", rotate.Code)
	}
	var rotated struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rotate.Body.Bytes(), &rotated); err != nil || rotated.Token == "" || rotated.Token == created.Token {
		t.Fatalf("rotate token=%q err=%v", rotated.Token, err)
	}

	allow := httptest.NewRecorder()
	router.ServeHTTP(allow, httptest.NewRequest("PUT", "/api/projects/"+projectID+"/runners", strings.NewReader(`{"runnerIds":["`+otherID+`"]}`)))
	if allow.Code != 200 {
		t.Fatalf("set project runners %d %s", allow.Code, allow.Body.String())
	}
	listed := httptest.NewRecorder()
	router.ServeHTTP(listed, httptest.NewRequest("GET", "/api/projects/"+projectID+"/runners", nil))
	if listed.Code != 200 || !strings.Contains(listed.Body.String(), otherID) {
		t.Fatalf("get project runners %d %s", listed.Code, listed.Body.String())
	}
	badAllow := httptest.NewRecorder()
	router.ServeHTTP(badAllow, httptest.NewRequest("PUT", "/api/projects/"+projectID+"/runners", strings.NewReader(`{"runnerIds":["not-a-uuid"]}`)))
	if badAllow.Code != 400 {
		t.Fatalf("invalid allowlist %d", badAllow.Code)
	}

	revoke := httptest.NewRecorder()
	router.ServeHTTP(revoke, httptest.NewRequest("POST", "/api/runners/"+otherID+"/revoke", nil))
	if revoke.Code != 200 || !strings.Contains(revoke.Body.String(), `"revokedAt"`) {
		t.Fatalf("revoke %d %s", revoke.Code, revoke.Body.String())
	}
	del := httptest.NewRecorder()
	router.ServeHTTP(del, httptest.NewRequest(http.MethodDelete, "/api/runners/"+otherID, nil))
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete %d %s", del.Code, del.Body.String())
	}
	missing := httptest.NewRecorder()
	router.ServeHTTP(missing, httptest.NewRequest("GET", "/api/runners/"+otherID, nil))
	if missing.Code != 404 {
		t.Fatalf("deleted runner still visible %d", missing.Code)
	}
}

func TestRunnerAPIRejectsInvalidIDsAndStoreFailures(t *testing.T) {
	memory := &runnerAPIStore{
		listErr:    store.ErrNotFound,
		projectErr: store.ErrNotFound,
		setErr:     store.ErrNotFound,
		createErr:  store.ErrNotFound,
	}
	router := NewRouter(app.New(memory))

	for _, req := range []*http.Request{
		httptest.NewRequest("GET", "/api/projects/not-a-uuid/runners", nil),
		httptest.NewRequest("PUT", "/api/projects/not-a-uuid/runners", strings.NewReader(`{"runnerIds":[]}`)),
		httptest.NewRequest("GET", "/api/runners/not-a-uuid", nil),
		httptest.NewRequest("PATCH", "/api/runners/not-a-uuid", strings.NewReader(`{"name":"Host"}`)),
		httptest.NewRequest("POST", "/api/runners/not-a-uuid/rotate-token", nil),
		httptest.NewRequest("POST", "/api/runners/not-a-uuid/revoke", nil),
		httptest.NewRequest(http.MethodDelete, "/api/runners/not-a-uuid", nil),
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != 400 {
			t.Fatalf("%s %s -> %d", req.Method, req.URL.Path, response.Code)
		}
	}

	for _, req := range []*http.Request{
		httptest.NewRequest("PUT", "/api/projects/"+projectID+"/runners", strings.NewReader(`{`)),
		httptest.NewRequest("PATCH", "/api/runners/"+otherID, strings.NewReader(`{`)),
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != 400 {
			t.Fatalf("invalid json %s -> %d", req.URL.Path, response.Code)
		}
	}

	for _, req := range []*http.Request{
		httptest.NewRequest("GET", "/api/projects/"+projectID+"/runners", nil),
		httptest.NewRequest("PUT", "/api/projects/"+projectID+"/runners", strings.NewReader(`{"runnerIds":[]}`)),
		httptest.NewRequest("GET", "/api/runners", nil),
		httptest.NewRequest("POST", "/api/runners", strings.NewReader(`{"name":"Host"}`)),
		httptest.NewRequest("PATCH", "/api/runners/"+otherID, strings.NewReader(`{"name":"Host"}`)),
		httptest.NewRequest("POST", "/api/runners/"+otherID+"/rotate-token", nil),
		httptest.NewRequest("POST", "/api/runners/"+otherID+"/revoke", nil),
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code < 400 {
			t.Fatalf("store failure %s %s -> %d", req.Method, req.URL.Path, response.Code)
		}
	}

	okStore := &runnerAPIStore{}
	okRouter := NewRouter(app.New(okStore))
	cleared := httptest.NewRecorder()
	okRouter.ServeHTTP(cleared, httptest.NewRequest("PUT", "/api/projects/"+projectID+"/runners", strings.NewReader(`{}`)))
	if cleared.Code != 200 || !strings.Contains(cleared.Body.String(), `"runnerIds":[]`) {
		t.Fatalf("null allowlist %d %s", cleared.Code, cleared.Body.String())
	}
}
