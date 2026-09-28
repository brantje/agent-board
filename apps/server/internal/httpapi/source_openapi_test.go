package httpapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceConnectionOpenAPIIsSafeAndReadOnlyForRepositoryPersistence(t *testing.T) {
	schemaPath := filepath.Join("..", "..", "..", "..", "packages", "api", "schemas", "control-plane.yaml")
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	connection := topLevelYAMLBlock(doc, "SourceConnection")
	if strings.Contains(connection, "credentialRef") || strings.Contains(connection, "credential:") {
		t.Fatalf("SourceConnection read model exposes credential material: %s", connection)
	}
	input := topLevelYAMLBlock(doc, "SourceConnectionInput")
	if !strings.Contains(input, "writeOnly: true") {
		t.Fatalf("SourceConnectionInput credential must be write-only: %s", input)
	}
	repository := topLevelYAMLBlock(doc, "SourceRepository")
	if strings.Contains(repository, "credential") {
		t.Fatalf("SourceRepository read model exposes credential material: %s", repository)
	}

	openapiPath := filepath.Join("..", "..", "..", "..", "packages", "api", "openapi.yaml")
	openapi, err := os.ReadFile(openapiPath)
	if err != nil {
		t.Fatal(err)
	}
	api := string(openapi)
	for _, route := range []string{
		"/api/source-connections:",
		"/api/source-connections/{resourceID}/repositories:",
		"/api/projects/{projectID}/source-connections:",
		"/api/projects/{projectID}/source-connections/{resourceID}/repositories:",
	} {
		if !strings.Contains(api, route) {
			t.Fatalf("OpenAPI missing Source route %s", route)
		}
	}
	if strings.Contains(api, "source-repositories") {
		t.Fatal("OpenAPI must not expose a standalone repository mutation collection")
	}
}
