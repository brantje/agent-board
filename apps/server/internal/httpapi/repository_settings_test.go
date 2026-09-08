package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
)

func TestGetRepositorySettings(t *testing.T) {
	t.Setenv("AGENT_BOARD_REPOSITORY_ROOTS", "/repositories")
	t.Setenv("AGENT_BOARD_REPOSITORY_MOUNT_PATH", "/repositories")
	router := NewRouter(app.New(&fakeControlPlaneStore{}))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/repository-settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"defaultRepositoryPath":"/repositories"`) {
		t.Fatalf("body = %s", body)
	}
	if !strings.Contains(body, `"repositoryRoots":["/repositories"]`) {
		t.Fatalf("body = %s", body)
	}
}
