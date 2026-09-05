package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
)

func TestUpdateProjectRejectsNullBody(t *testing.T) {
	router := NewRouter(app.New(&fakeControlPlaneStore{}))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/projects/"+projectID, strings.NewReader("null"))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_request") {
		t.Fatalf("expected invalid_request in %s", rec.Body.String())
	}
}

func TestResponseSchemasRequireAlwaysSerializedNullableFields(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "packages", "api", "schemas", "control-plane.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	cases := map[string][]string{
		"Provider":     {"baseUrl"},
		"ModelProfile": {"temperature", "maxTokens", "maxConcurrent"},
		"Runtime":      {"cpuLimitMillis", "memoryLimitBytes", "pidLimit", "timeoutSeconds"},
		"Issue":        {"assignedAgentId"},
		"Run":          {"agentId", "queueReason", "failureReason", "startedAt", "completedAt"},
	}

	for schema, fields := range cases {
		t.Run(schema, func(t *testing.T) {
			block := topLevelYAMLBlock(doc, schema)
			if block == "" {
				t.Fatalf("schema %s not found", schema)
			}
			required := requiredLine(block)
			for _, field := range fields {
				if !strings.Contains(required, field) {
					t.Fatalf("schema %s must require always-serialized nullable field %s; required=%s", schema, field, required)
				}
			}
		})
	}
}
