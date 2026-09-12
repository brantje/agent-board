package httpapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPhase3GroupOpenAPIIsConcreteAndAdminAuthenticated(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "packages", "api")
	openAPIData, err := os.ReadFile(filepath.Join(root, "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	openAPI := string(openAPIData)
	for _, route := range []string{
		"/api/groups:",
		"/api/groups/{groupID}:",
		"/api/groups/{groupID}/members:",
		"/api/groups/{groupID}/members/{userID}:",
	} {
		if !strings.Contains(openAPI, route) {
			t.Fatalf("OpenAPI missing group route %s", route)
		}
	}
	pathsData, err := os.ReadFile(filepath.Join(root, "paths", "groups.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	paths := string(pathsData)
	if !strings.Contains(paths, "security: [{BearerAuth: []}]") {
		t.Fatal("group operations must require bearer authentication")
	}
	for _, forbidden := range []string{"principal", "subject", "projectRole", "deploymentRole", "memberType"} {
		if strings.Contains(strings.ToLower(paths), strings.ToLower(forbidden)) {
			t.Fatalf("group contract introduced deferred abstraction %q", forbidden)
		}
	}
}
