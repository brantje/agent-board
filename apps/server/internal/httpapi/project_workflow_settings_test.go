package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
)

func TestUpdateProjectStrictOrderAPIValidation(t *testing.T) {
	router := NewRouter(app.New(&fakeControlPlaneStore{}))

	for name, value := range map[string]string{
		"string": `"false"`,
		"null":   `null`,
		"object": `{}`,
		"array":  `[]`,
	} {
		t.Run("rejects "+name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/projects/"+projectID,
				strings.NewReader(`{"workflowSettings":{"strictOrder":`+value+`}}`))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), "invalid_argument") {
				t.Fatalf("body=%s", recorder.Body.String())
			}
		})
	}

	for _, value := range []string{"true", "false"} {
		t.Run("accepts "+value, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/projects/"+projectID,
				strings.NewReader(`{"workflowSettings":{"strictOrder":`+value+`}}`))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
			}
		})
	}
}
