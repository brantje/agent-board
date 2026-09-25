package runexec

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type commentWorkContextStoreFake struct {
	value *store.AgentWorkRequestExecutionContext
	err   error
}

func (f commentWorkContextStoreFake) GetAgentWorkRequestExecutionContext(context.Context, string, string) (*store.AgentWorkRequestExecutionContext, error) {
	return f.value, f.err
}

func TestLoadAgentWorkRequestContextMapsStoreProjection(t *testing.T) {
	parent := "parent"
	root := "root"
	reason := store.IssueCommentImplicitRoutingReasonUniqueThreadAgent
	value := &store.AgentWorkRequestExecutionContext{
		WorkRequestID: "work-1",
		Comments: []store.AgentWorkRequestComment{{
			CommentID: "comment-1", AuthorType: store.ActorTypeHuman, AuthorID: "user-1", AuthorName: "User",
			Body: "follow up", ParentCommentID: &parent, RootCommentID: &root,
			TriggerKind: store.AgentWorkRequestTriggerImplicit, RoutingReason: &reason,
		}},
	}
	got, err := loadAgentWorkRequestContext(t.Context(), commentWorkContextStoreFake{value: value}, executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{ID: "project-1"},
		Run: executioncontext.RunContext{ID: "run-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.WorkRequestID != value.WorkRequestID || len(got.Comments) != 1 {
		t.Fatalf("context=%+v", got)
	}
	comment := got.Comments[0]
	if comment.CommentID != "comment-1" || comment.Body != "follow up" || comment.ParentCommentID == nil ||
		*comment.ParentCommentID != parent || comment.RootCommentID == nil || *comment.RootCommentID != root ||
		comment.RoutingReason == nil || *comment.RoutingReason != reason {
		t.Fatalf("comment=%+v", comment)
	}
}

func TestLoadAgentWorkRequestContextIsOptional(t *testing.T) {
	got, err := loadAgentWorkRequestContext(t.Context(), struct{}{}, executioncontext.SafeContext{})
	if err != nil || got != nil {
		t.Fatalf("context=%+v err=%v", got, err)
	}
}

func TestLoadAgentWorkRequestContextPropagatesStoreFailure(t *testing.T) {
	want := errors.New("load comment work")
	got, err := loadAgentWorkRequestContext(t.Context(), commentWorkContextStoreFake{err: want}, executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{ID: "project-1"},
		Run: executioncontext.RunContext{ID: "run-1"},
	})
	if got != nil || !errors.Is(err, want) {
		t.Fatalf("context=%+v err=%v", got, err)
	}
}

type commentWorkExecutionStoreFake struct {
	*processTestStore
	err error
}

func (f *commentWorkExecutionStoreFake) GetAgentWorkRequestExecutionContext(context.Context, string, string) (*store.AgentWorkRequestExecutionContext, error) {
	return nil, f.err
}

func TestEngineRequestStopsWhenCommentWorkContextCannotLoad(t *testing.T) {
	want := errors.New("comment work unavailable")
	processor := &Processor{store: &commentWorkExecutionStoreFake{processTestStore: &processTestStore{}, err: want}}
	_, err := processor.engineRequestWithDelegationContinuation(t.Context(), executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{ID: "project-1"},
		Run: executioncontext.RunContext{ID: "run-1"},
	}, nil, "", nil)
	if !errors.Is(err, want) {
		t.Fatalf("engine request error=%v", err)
	}
}
