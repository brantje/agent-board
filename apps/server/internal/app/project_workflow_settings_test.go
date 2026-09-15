package app

import (
	"encoding/json"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestValidateProjectStrictOrderRequiresBoolean(t *testing.T) {
	base := store.Project{
		Name:           "Project",
		IssuePrefix:    "AB",
		RepositoryPath: "/repo",
	}

	for _, settings := range []string{
		`{}`,
		`{"strictOrder":true}`,
		`{"strictOrder":false,"other":"kept"}`,
	} {
		project := base
		project.WorkflowSettings = json.RawMessage(settings)
		if err := validateProject(project); err != nil {
			t.Errorf("validateProject(%s): %v", settings, err)
		}
	}

	for _, settings := range []string{
		`{"strictOrder":"banana"}`,
		`{"strictOrder":"false"}`,
		`{"strictOrder":null}`,
		`{"strictOrder":{}}`,
		`{"strictOrder":[]}`,
	} {
		project := base
		project.WorkflowSettings = json.RawMessage(settings)
		if err := validateProject(project); err == nil {
			t.Errorf("validateProject(%s) succeeded, want invalid_argument", settings)
		}
	}
}
