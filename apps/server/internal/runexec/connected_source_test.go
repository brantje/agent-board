package runexec

import (
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestValidateExecutionProjectSourceFailsClosedForConnectedProjects(t *testing.T) {
	safe := executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{
			ID: "project-1", SourceType: store.ProjectSourceConnected,
		},
	}
	err := validateExecutionProjectSource(safe)
	if err == nil || !strings.Contains(err.Error(), "connected Project source execution is unavailable") {
		t.Fatalf("connected source error = %v", err)
	}
}

func TestValidateExecutionProjectSourcePreservesExistingModes(t *testing.T) {
	for _, sourceType := range []string{"", store.ProjectSourceLocal, store.ProjectSourceGit} {
		t.Run(sourceType, func(t *testing.T) {
			if err := validateExecutionProjectSource(executioncontext.SafeContext{
				Project: executioncontext.ProjectContext{SourceType: sourceType},
			}); err != nil {
				t.Fatalf("source %q rejected: %v", sourceType, err)
			}
		})
	}
}
