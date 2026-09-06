package httpapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunEvidenceOpenAPIPathsAndSchemas(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "packages", "api")
	mainData, err := os.ReadFile(filepath.Join(root, "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	mainDoc := string(mainData)
	for _, route := range []string{
		"/api/projects/{projectID}/runs/{runID}/evidence:",
		"/api/projects/{projectID}/runs/{runID}/raw-output/{chunkID}:",
		"/api/projects/{projectID}/runs/{runID}/artifacts/{artifactID}:",
	} {
		if !strings.Contains(mainDoc, route) {
			t.Fatalf("OpenAPI missing run evidence route %s", route)
		}
	}
	pathsData, err := os.ReadFile(filepath.Join(root, "paths", "run-evidence.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(pathsData), "operationId:") != 3 {
		t.Fatalf("run evidence path document must define three operations: %s", pathsData)
	}
	schemaData, err := os.ReadFile(filepath.Join(root, "schemas", "execution-evidence.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, schema := range []string{"RunEvidence:", "RuntimeInstanceEvidence:", "ExecutionSessionEvidence:", "EventEvidence:", "RawOutputChunkEvidence:", "ArtifactEvidence:"} {
		if !strings.Contains(string(schemaData), schema) {
			t.Fatalf("execution evidence schema document missing %s", schema)
		}
	}
}
