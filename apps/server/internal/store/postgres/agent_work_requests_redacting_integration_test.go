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

	firstComment := createHumanMentionComment(t, f, author.ID, "wrapped-first", "first wrapped request")
	first := firstComment.Mentions[0]
	if first.Outcome != store.IssueCommentMentionOutcomeQueued || first.DelegatedRunID == nil || first.WorkRequestID == nil {
		t.Fatalf("first mention=%+v", first)
	}

	secondComment := createHumanMentionComment(t, f, author.ID, "wrapped-second", "second wrapped request")
	second := secondComment.Mentions[0]
	if second.Outcome != store.IssueCommentMentionOutcomeCoalesced || second.WorkRequestID == nil || *second.WorkRequestID != *first.WorkRequestID {
		t.Fatalf("second mention=%+v first=%+v", second, first)
	}
	if second.DelegatedRunID == nil || *second.DelegatedRunID != *first.DelegatedRunID {
		t.Fatalf("coalesced mention changed Run: first=%+v second=%+v", first, second)
	}

	wrapped := evidence.NewRedactingStore(f.store, redaction.NewRegistry())
	contextStore, ok := any(wrapped).(store.AgentWorkRequestExecutionContextStore)
	if !ok {
		t.Fatal("production RedactingStore lost Agent work request execution-context capability")
	}
	executionContext, err := contextStore.GetAgentWorkRequestExecutionContext(ctx, f.project.ID, *first.DelegatedRunID)
	if err != nil {
		t.Fatal(err)
	}
	if executionContext == nil || executionContext.WorkRequestID != *first.WorkRequestID || len(executionContext.Comments) != 2 {
		t.Fatalf("wrapped execution context=%+v", executionContext)
	}

	byID := make(map[string]store.AgentWorkRequestComment, len(executionContext.Comments))
	for _, comment := range executionContext.Comments {
		byID[comment.CommentID] = comment
	}
	for _, expected := range []struct {
		id   string
		body string
	}{
		{id: firstComment.ID, body: firstComment.Body},
		{id: secondComment.ID, body: secondComment.Body},
	} {
		comment, ok := byID[expected.id]
		if !ok {
			t.Fatalf("wrapped execution context missing comment %s: %+v", expected.id, executionContext.Comments)
		}
		if comment.Body != expected.body || comment.AuthorID != author.ID || comment.AuthorType != store.ActorTypeHuman ||
			comment.TriggerKind != store.AgentWorkRequestTriggerMention {
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


func TestGetAgentWorkRequestExecutionContextReturnsNilWithoutWorkRequest(t *testing.T) {
	f := newDelegationFixture(t, true)
	got, err := f.store.GetAgentWorkRequestExecutionContext(t.Context(), f.project.ID, f.parentRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("execution context=%+v want nil", got)
	}
}

func TestGetAgentWorkRequestExecutionContextRejectsInvalidIdentifiers(t *testing.T) {
	f := newDelegationFixture(t, true)
	if _, err := f.store.GetAgentWorkRequestExecutionContext(t.Context(), "", f.parentRun.ID); err != store.ErrInvalidArgument {
		t.Fatalf("empty project error=%v want %v", err, store.ErrInvalidArgument)
	}
	if _, err := f.store.GetAgentWorkRequestExecutionContext(t.Context(), f.project.ID, ""); err != store.ErrInvalidArgument {
		t.Fatalf("empty Run error=%v want %v", err, store.ErrInvalidArgument)
	}
}
