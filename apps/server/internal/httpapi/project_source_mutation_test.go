package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type projectMutationStore struct {
	fakeControlPlaneStore
	project store.Project
}

func (s *projectMutationStore) CreateProject(_ context.Context, project store.Project) (store.Project, error) {
	project.ID = projectID
	s.project = project
	return project, nil
}

func (s *projectMutationStore) GetProject(_ context.Context, id string) (store.Project, error) {
	if id != projectID || s.project.ID == "" {
		return store.Project{}, store.ErrNotFound
	}
	return s.project, nil
}

func (s *projectMutationStore) UpdateProject(_ context.Context, project store.Project) (store.Project, error) {
	s.project = project
	return project, nil
}

func TestCreateGitProjectUsesSourceContract(t *testing.T) {
	storeImpl := &projectMutationStore{}
	router := NewRouter(app.New(storeImpl))
	body := `{"name":"Remote","issuePrefix":"RM","sourceType":"git","cloneUrl":"https://example.com/acme/widget.git","sourceRef":"release/v1","workflowSettings":{}}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/projects", strings.NewReader(body))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if storeImpl.project.SourceType != store.ProjectSourceGit || storeImpl.project.CloneURL == nil || *storeImpl.project.CloneURL != "https://example.com/acme/widget.git" {
		t.Fatalf("stored project source = %#v", storeImpl.project)
	}
	if storeImpl.project.RepositoryPath != "" || storeImpl.project.DefaultBranch != "" {
		t.Fatalf("git source retained local fields: repositoryPath=%q defaultBranch=%q", storeImpl.project.RepositoryPath, storeImpl.project.DefaultBranch)
	}
}

func TestPatchProjectCanClearSourceRefWithNull(t *testing.T) {
	cloneURL := "https://example.com/acme/widget.git"
	ref := "release/v1"
	storeImpl := &projectMutationStore{project: store.Project{
		ID:               projectID,
		Name:             "Remote",
		IssuePrefix:      "RM",
		SourceType:       store.ProjectSourceGit,
		CloneURL:         &cloneURL,
		SourceRef:        &ref,
		WorkflowSettings: store.EmptyObject,
	}}
	router := NewRouter(app.New(storeImpl))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/projects/"+projectID, strings.NewReader(`{"sourceRef":null}`))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if storeImpl.project.SourceRef != nil {
		t.Fatalf("stored sourceRef=%v, want nil", storeImpl.project.SourceRef)
	}
	var got ProjectDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode Project response: %v", err)
	}
	if got.SourceRef != nil {
		t.Fatalf("response sourceRef=%v, want nil", got.SourceRef)
	}
}

func TestPatchProjectOmittingSourceRefPreservesIt(t *testing.T) {
	cloneURL := "https://example.com/acme/widget.git"
	ref := "release/v1"
	storeImpl := &projectMutationStore{project: store.Project{
		ID:               projectID,
		Name:             "Remote",
		IssuePrefix:      "RM",
		SourceType:       store.ProjectSourceGit,
		CloneURL:         &cloneURL,
		SourceRef:        &ref,
		WorkflowSettings: store.EmptyObject,
	}}
	router := NewRouter(app.New(storeImpl))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/projects/"+projectID, strings.NewReader(`{"name":"Renamed"}`))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if storeImpl.project.SourceRef == nil || *storeImpl.project.SourceRef != ref {
		t.Fatalf("stored sourceRef=%v, want %q", storeImpl.project.SourceRef, ref)
	}
}
