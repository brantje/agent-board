package httpapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectSourceOpenAPISchemasExposeLocalAndGitConfiguration(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "packages", "api", "schemas", "control-plane.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)

	project := topLevelYAMLBlock(doc, "Project")
	for _, required := range []string{"sourceType", "cloneUrl", "sourceRef", "repositoryPath", "defaultBranch"} {
		if !strings.Contains(project, "  - "+required) {
			t.Fatalf("Project schema must require %s: %s", required, project)
		}
	}
	if !strings.Contains(project, "enum: [local, git]") {
		t.Fatalf("Project sourceType must allow local and git: %s", project)
	}
	if !strings.Contains(project, "sourceRef:\n      type: [string, 'null']") {
		t.Fatalf("Project sourceRef must be nullable: %s", project)
	}

	create := topLevelYAMLBlock(doc, "ProjectCreate")
	if !strings.Contains(create, "required: [name, issuePrefix]") {
		t.Fatalf("ProjectCreate must not unconditionally require local-only fields: %s", create)
	}
	for _, field := range []string{"sourceType:", "cloneUrl:", "sourceRef:", "repositoryPath:", "defaultBranch:"} {
		if !strings.Contains(create, field) {
			t.Fatalf("ProjectCreate missing %s: %s", field, create)
		}
	}

	update := topLevelYAMLBlock(doc, "ProjectUpdate")
	for _, field := range []string{"sourceType:", "cloneUrl:", "sourceRef:", "repositoryPath:", "defaultBranch:"} {
		if !strings.Contains(update, field) {
			t.Fatalf("ProjectUpdate missing %s: %s", field, update)
		}
	}
}
