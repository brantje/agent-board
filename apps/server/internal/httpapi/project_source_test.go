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

type projectSourceStore struct {
	fakeControlPlaneStore
	project store.Project
}

func (s *projectSourceStore) ListProjects(context.Context) ([]store.Project, error) {
	return []store.Project{s.project}, nil
}

func (s *projectSourceStore) GetProject(_ context.Context, id string) (store.Project, error) {
	if id != s.project.ID {
		return store.Project{}, store.ErrNotFound
	}
	return s.project, nil
}

func TestProjectSourceConfigurationIsReturnedByAPIReads(t *testing.T) {
	cloneURL := "https://example.com/acme/widget.git"
	ref := "release/v1"
	project := store.Project{
		ID:               projectID,
		Name:             "Remote Project",
		IssuePrefix:      "AB",
		SourceType:       store.ProjectSourceGit,
		CloneURL:         &cloneURL,
		SourceRef:        &ref,
		WorkflowSettings: store.EmptyObject,
	}
	router := NewRouter(app.New(&projectSourceStore{project: project}))

	t.Run("get", func(t *testing.T) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/projects/"+projectID, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}

		var got ProjectDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode Project response: %v", err)
		}
		assertProjectSourceDTO(t, got, cloneURL, ref)
	})

	t.Run("list", func(t *testing.T) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/projects", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}

		var got []ProjectDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode Projects response: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("projects=%d want=1", len(got))
		}
		assertProjectSourceDTO(t, got[0], cloneURL, ref)
	})
}

func TestProjectSourcePatchSourceRefNullClearsReference(t *testing.T) {
	cloneURL := "https://example.com/acme/widget.git"
	ref := "release/v1"
	project := store.Project{
		ID:               projectID,
		Name:             "Remote Project",
		IssuePrefix:      "AB",
		SourceType:       store.ProjectSourceGit,
		CloneURL:         &cloneURL,
		SourceRef:        &ref,
		WorkflowSettings: store.EmptyObject,
	}
	router := NewRouter(app.New(&projectSourceStore{project: project}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/projects/"+projectID, strings.NewReader(`{"sourceRef":null}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var got ProjectDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode Project response: %v", err)
	}
	if got.SourceRef != nil {
		t.Fatalf("sourceRef=%v want=nil", got.SourceRef)
	}
}

func TestProjectSourcePatchOmittedSourceRefKeepsReference(t *testing.T) {
	cloneURL := "https://example.com/acme/widget.git"
	ref := "release/v1"
	project := store.Project{
		ID:               projectID,
		Name:             "Remote Project",
		IssuePrefix:      "AB",
		SourceType:       store.ProjectSourceGit,
		CloneURL:         &cloneURL,
		SourceRef:        &ref,
		WorkflowSettings: store.EmptyObject,
	}
	router := NewRouter(app.New(&projectSourceStore{project: project}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/projects/"+projectID, strings.NewReader(`{"name":"Renamed"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var got ProjectDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode Project response: %v", err)
	}
	if got.SourceRef == nil || *got.SourceRef != ref {
		t.Fatalf("sourceRef=%v want=%q", got.SourceRef, ref)
	}
}

func assertProjectSourceDTO(t *testing.T, project ProjectDTO, cloneURL, ref string) {
	t.Helper()
	if project.SourceType != store.ProjectSourceGit {
		t.Fatalf("sourceType=%q want=%q", project.SourceType, store.ProjectSourceGit)
	}
	if project.CloneURL == nil || *project.CloneURL != cloneURL {
		t.Fatalf("cloneUrl=%v want=%q", project.CloneURL, cloneURL)
	}
	if project.SourceRef == nil || *project.SourceRef != ref {
		t.Fatalf("sourceRef=%v want=%q", project.SourceRef, ref)
	}
	if project.RepositoryPath != "" || project.DefaultBranch != "" {
		t.Fatalf("Git source leaked local fields: repositoryPath=%q defaultBranch=%q", project.RepositoryPath, project.DefaultBranch)
	}
}
