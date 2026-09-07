package app

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestNewServicesPreservesMissingQuestionCapabilityThroughRedactingStore(t *testing.T) {
	base := &fakeStore{}
	secured := evidence.NewRedactingStore(base, redaction.NewRegistry())
	if store.SupportsQuestionStore(base) {
		t.Fatal("base unexpectedly reports QuestionStore support")
	}
	if store.SupportsQuestionStore(secured) {
		t.Fatal("redacting decorator must preserve missing QuestionStore support")
	}

	services, err := NewServices(secured, workspaceMaterializerFunc(func(_ context.Context, _ store.Project, _ store.Issue, workspace store.Workspace) (store.Workspace, error) {
		return workspace, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if services.Questions != nil {
		t.Fatal("Question service must remain unavailable when the wrapped base store lacks QuestionStore support")
	}
}
