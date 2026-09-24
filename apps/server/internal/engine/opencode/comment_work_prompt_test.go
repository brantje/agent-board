package opencode

import (
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
)

func TestInitialTaskPromptIncludesAllCommentWorkProvenance(t *testing.T) {
	parent := "comment-root"
	root := "comment-root"
	reason := "DIRECT_AGENT_REPLY"
	prompt := initialTaskPromptForRequest(engine.Request{
		Context: executioncontext.SafeContext{
			Issue: executioncontext.IssueContext{Title: "Coalesced work", Status: "TODO"},
		},
		CommentWork: &engine.CommentWorkContext{
			WorkRequestID: "work-1",
			Comments: []engine.CommentWorkInput{
				{
					CommentID: "comment-1", AuthorType: "HUMAN", AuthorID: "user-1", AuthorName: "Ada",
					Body: "first request", RootCommentID: &root, TriggerKind: engine.CommentWorkTriggerMention,
				},
				{
					CommentID: "comment-2", AuthorType: "HUMAN", AuthorID: "user-2", AuthorName: "Grace",
					Body: "second request", ParentCommentID: &parent, RootCommentID: &root,
					TriggerKind: engine.CommentWorkTriggerImplicit, RoutingReason: &reason,
				},
			},
		},
	})
	for _, want := range []string{
		"Comment-triggered work admitted for this Run:",
		"Work request ID: work-1",
		"Comment ID: comment-1",
		"Author: HUMAN user-1 (Ada)",
		"structured Agent mention",
		"Body:\nfirst request",
		"Comment ID: comment-2",
		"Author: HUMAN user-2 (Grace)",
		"implicit Issue-discussion route (DIRECT_AGENT_REPLY)",
		"Parent comment ID: comment-root",
		"Thread root comment ID: comment-root",
		"Body:\nsecond request",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestCommentWorkPromptMarksDeletedInputWithoutDroppingIdentity(t *testing.T) {
	prompt := commentWorkPrompt(&engine.CommentWorkContext{
		WorkRequestID: "work-1",
		Comments: []engine.CommentWorkInput{{
			CommentID: "comment-1", AuthorType: "HUMAN", AuthorID: "user-1",
			Deleted: true, TriggerKind: engine.CommentWorkTriggerMention,
		}},
	})
	if !strings.Contains(prompt, "Comment ID: comment-1") || !strings.Contains(prompt, "[comment deleted before execution]") {
		t.Fatalf("deleted input lost provenance:\n%s", prompt)
	}
}
