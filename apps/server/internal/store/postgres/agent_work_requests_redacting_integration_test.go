package postgres

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestAgentWorkRequestExecutionContextSurvivesProductionRedactingStore(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	author, err := f.store.CreateUser(ctx, authUser("wrapped-work-author", "wrapped-work-author@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	firstComment := createPlainWorkRequestComment(t, f, author.ID, "first wrapped request")
	first := requestAgentWorkForTest(t, f, firstComment, nil)
	if first.Outcome != agentWorkRequestOutcomeQueued || first.WorkRequest.RunID == nil {
		t.Fatalf("first request=%+v", first)
	}
	secondComment := createPlainWorkRequestComment(t, f, author.ID, "second wrapped request")
	second := requestAgentWorkForTest(t, f, secondComment, nil)
	if second.Outcome != agentWorkRequestOutcomeCoalesced || second.WorkRequest.ID != first.WorkRequest.ID {
		t.Fatalf("second request=%+v first=%+v", second, first)
	}

	wrapped := evidence.NewRedactingStore(f.store, redaction.NewRegistry())
	contextStore, ok := any(wrapped).(store.AgentWorkRequestExecutionContextStore)
	if !ok {
		t.Fatal("production RedactingStore lost Agent work request execution-context capability")
	}
	executionContext, err := contextStore.GetAgentWorkRequestExecutionContext(ctx, f.project.ID, *first.WorkRequest.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if executionContext == nil || executionContext.WorkRequestID != first.WorkRequest.ID || len(executionContext.Comments) != 2 {
		t.Fatalf("wrapped execution context=%+v", executionContext)
	}
	byID := make(map[string]store.AgentWorkRequestComment, len(executionContext.Comments))
	for _, comment := range executionContext.Comments {
		byID[comment.CommentID] = comment
	}
	for _, expected := range []struct {
		id string
		body string
	}{
		{id: firstComment.ID, body: firstComment.Body},
		{id: secondComment.ID, body: secondComment.Body},
	} {
		comment, ok := byID[expected.id]
		if !ok {
			t.Fatalf("wrapped execution context missing comment %s: %+v", expected.id, executionContext.Comments)
		}
		if comment.Body != expected.body || comment.AuthorID != author.ID || comment.AuthorType != store.ActorTypeHuman || comment.TriggerKind == "" {
			t.Fatalf("wrapped comment provenance=%+v", comment)
		}
	}
	runs, err := f.store.ListRuns(ctx, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("Runs=%+v want parent plus one target", runs)
	}
}
