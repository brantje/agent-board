package httpapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDelegationOpenAPIPathsAndSchemas(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "packages", "api")
	mainData, err := os.ReadFile(filepath.Join(root, "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	mainDoc := string(mainData)
	for _, route := range []string{
		"/api/projects/{projectID}/runs/{runID}/delegations:",
		"/api/projects/{projectID}/runs/{runID}/delegation:",
	} {
		if !strings.Contains(mainDoc, route) {
			t.Fatalf("OpenAPI missing delegation route %s", route)
		}
	}

	pathsData, err := os.ReadFile(filepath.Join(root, "paths", "delegations.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	pathsDoc := string(pathsData)
	for _, operation := range []string{"listRunDelegations", "getRunDelegation"} {
		if !strings.Contains(pathsDoc, "operationId: "+operation) {
			t.Fatalf("delegation path document missing %s: %s", operation, pathsData)
		}
	}
	if strings.Contains(pathsDoc, "operationId: createRunDelegation") || strings.Contains(pathsDoc, "\n  post:") {
		t.Fatalf("delegation write operation must not be public: %s", pathsData)
	}
	if strings.Count(pathsDoc, "operationId:") != 2 {
		t.Fatalf("delegation path document must define two read operations: %s", pathsData)
	}

	schemaData, err := os.ReadFile(filepath.Join(root, "schemas", "control-plane.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	schemaDoc := string(schemaData)
	for _, fragment := range []string{
		"Delegation:",
		"allowDelegation:",
		"default: false",
	} {
		if !strings.Contains(schemaDoc, fragment) {
			t.Fatalf("delegation schema contract missing %q", fragment)
		}
	}
}
