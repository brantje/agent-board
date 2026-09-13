package httpapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectAccessOpenAPIPathsAndSchemas(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "packages", "api")
	mainData, err := os.ReadFile(filepath.Join(root, "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	mainDoc := string(mainData)
	for _, route := range []string{
		"/api/projects/{projectID}/access/effective-role:",
		"/api/projects/{projectID}/access/users:",
		"/api/projects/{projectID}/access/users/{userID}:",
		"/api/projects/{projectID}/access/groups:",
		"/api/projects/{projectID}/access/groups/{groupID}:",
		"/api/projects/{projectID}/access/directory/users:",
		"/api/projects/{projectID}/access/directory/groups:",
	} {
		if !strings.Contains(mainDoc, route) {
			t.Fatalf("OpenAPI missing Project access route %s", route)
		}
	}
	pathsData, err := os.ReadFile(filepath.Join(root, "paths", "project-access.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	pathsDoc := string(pathsData)
	for _, operation := range []string{
		"getEffectiveProjectRole", "listProjectUserAccess", "upsertProjectUserAccess", "deleteProjectUserAccess",
		"listProjectGroupAccess", "upsertProjectGroupAccess", "deleteProjectGroupAccess", "searchProjectAccessUsers", "searchProjectAccessGroups",
	} {
		if !strings.Contains(pathsDoc, operation) {
			t.Fatalf("Project access path document missing %s", operation)
		}
	}
	schemaData, err := os.ReadFile(filepath.Join(root, "schemas", "project-access.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	schemaDoc := string(schemaData)
	for _, schema := range []string{"ProjectRole:", "ProjectUserAccess:", "ProjectGroupAccess:", "ProjectDirectoryUser:", "ProjectDirectoryGroup:"} {
		if !strings.Contains(schemaDoc, schema) {
			t.Fatalf("Project access schema document missing %s", schema)
		}
	}
	for _, role := range []string{"viewer", "member", "admin"} {
		if !strings.Contains(schemaDoc, role) {
			t.Fatalf("Project access schema missing role %s", role)
		}
	}
}
