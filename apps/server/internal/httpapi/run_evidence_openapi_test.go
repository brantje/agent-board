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
		"/api/projects/{projectID}/runs/{runID}/usage:",
		"/api/projects/{projectID}/runs/{runID}/raw-output/{chunkID}:",
		"/api/projects/{projectID}/runs/{runID}/artifacts/{artifactID}:",
		"/api/projects/{projectID}/runs/{runID}/events:",
	} {
		if !strings.Contains(mainDoc, route) {
			t.Fatalf("OpenAPI missing run evidence route %s", route)
		}
	}
	pathsData, err := os.ReadFile(filepath.Join(root, "paths", "run-evidence.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	pathsDoc := string(pathsData)
	if strings.Count(pathsDoc, "operationId:") != 5 {
		t.Fatalf("run evidence path document must define five operations: %s", pathsData)
	}
	if !strings.Contains(pathsDoc, "text/event-stream:") || !strings.Contains(pathsDoc, "afterSequence") || !strings.Contains(pathsDoc, "streamRunEvents") {
		t.Fatalf("run event stream contract is missing: %s", pathsData)
	}
	if !strings.Contains(pathsDoc, "getRunUsage") || !strings.Contains(pathsDoc, "RunUsageEvidence") {
		t.Fatalf("run usage contract is missing: %s", pathsData)
	}
	if !strings.Contains(pathsDoc, "'*/*':") {
		t.Fatalf("artifact download must document its validated dynamic response media type: %s", pathsData)
	}
	schemaData, err := os.ReadFile(filepath.Join(root, "schemas", "execution-evidence.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, schema := range []string{"RunEvidence:", "RunUsageEvidence:", "RuntimeInstanceEvidence:", "ExecutionSessionEvidence:", "EventEvidence:", "RawOutputChunkEvidence:", "ArtifactEvidence:"} {
		if !strings.Contains(string(schemaData), schema) {
			t.Fatalf("execution evidence schema document missing %s", schema)
		}
	}
}
