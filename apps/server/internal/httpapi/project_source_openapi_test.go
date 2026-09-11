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
	if !strings.Contains(create, "oneOf:\n    - required: [repositoryPath]") {
		t.Fatalf("ProjectCreate must require repositoryPath for local source requests: %s", create)
	}
	if !strings.Contains(create, "sourceType: {type: string, enum: [local]}") {
		t.Fatalf("ProjectCreate local branch must restrict sourceType to local when supplied: %s", create)
	}
	if !strings.Contains(create, "- required: [sourceType, cloneUrl]") {
		t.Fatalf("ProjectCreate must require sourceType and cloneUrl for git source requests: %s", create)
	}
	if !strings.Contains(create, "sourceType: {type: string, enum: [git]}") || !strings.Contains(create, "cloneUrl: {type: string, minLength: 1}") {
		t.Fatalf("ProjectCreate git branch must require a non-empty cloneUrl: %s", create)
	}

	update := topLevelYAMLBlock(doc, "ProjectUpdate")
	for _, field := range []string{"sourceType:", "cloneUrl:", "sourceRef:", "repositoryPath:", "defaultBranch:"} {
		if !strings.Contains(update, field) {
			t.Fatalf("ProjectUpdate missing %s: %s", field, update)
		}
	}
}
