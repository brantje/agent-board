package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestPatchProjectCloneURLSemantics(t *testing.T) {
	const existingURL = "https://example.com/acme/widget.git"
	const changedURL = "https://example.com/acme/widget-v2.git"

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantURL    string
	}{
		{name: "omitted preserves", body: `{"name":"Renamed"}`, wantStatus: http.StatusOK, wantURL: existingURL},
		{name: "changed updates", body: `{"cloneUrl":"` + changedURL + `"}`, wantStatus: http.StatusOK, wantURL: changedURL},
		{name: "null clears then validation rejects git source", body: `{"cloneUrl":null}`, wantStatus: http.StatusBadRequest, wantURL: existingURL},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cloneURL := existingURL
			storeImpl := &projectMutationStore{project: store.Project{
				ID:               projectID,
				Name:             "Remote",
				IssuePrefix:      "RM",
				SourceType:       store.ProjectSourceGit,
				CloneURL:         &cloneURL,
				WorkflowSettings: store.EmptyObject,
			}}
			router := NewRouter(app.New(storeImpl))
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/projects/"+projectID, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d body=%s, want %d", rec.Code, rec.Body.String(), tc.wantStatus)
			}
			if storeImpl.project.CloneURL == nil || *storeImpl.project.CloneURL != tc.wantURL {
				t.Fatalf("stored cloneUrl=%v, want %q", storeImpl.project.CloneURL, tc.wantURL)
			}
			if tc.wantStatus == http.StatusBadRequest && !strings.Contains(rec.Body.String(), `"code":"invalid_argument"`) {
				t.Fatalf("body=%s, want invalid_argument from Project validation", rec.Body.String())
			}
		})
	}
}

func TestProjectUpdateCloneURLIsNullableInOpenAPI(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "packages", "api", "schemas", "control-plane.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	update := topLevelYAMLBlock(string(data), "ProjectUpdate")
	if !strings.Contains(update, "cloneUrl: {type: [string, 'null']}") {
		t.Fatalf("ProjectUpdate cloneUrl must remain nullable: %s", update)
	}
}
