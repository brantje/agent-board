package postgres

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestIssueCommentImplicitTopLevelAssigneeQueuesOnceAndRetryIsIdempotent(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	author, err := f.store.CreateUser(ctx, authUser("implicit-author", "implicit-author@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.pool.Exec(ctx, `
		UPDATE issues
		SET assignee_type='AGENT', assignee_id=$3
		WHERE project_id=$1 AND id=$2
	`, f.project.ID, f.issue.ID, f.target.ID); err != nil {
		t.Fatal(err)
	}
	before, err := f.store.GetIssue(ctx, f.project.ID, f.issue.ID)
	if err != nil {
		t.Fatal(err)
	}

	requestKey := "implicit-top-level-request"
	input := store.IssueComment{
		IssueID: f.issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID,
		SourceActionKey: &requestKey, Body: "Please continue this Issue.",
	}
	first, err := f.store.CreateIssueCommentWithTriggers(ctx, f.project.ID, input, store.IssueCommentTriggerRequest{})
	if err != nil {
		t.Fatal(err)
	}
	trigger := first.Comment.ImplicitTrigger
	if trigger == nil ||
		trigger.TargetAgentID != f.target.ID ||
		trigger.RoutingReason != store.IssueCommentImplicitRoutingReasonIssueAssignee ||
		trigger.Outcome != store.IssueCommentImplicitOutcomeQueued ||
		trigger.ReasonCode != nil ||
		trigger.DelegationID == nil ||
		trigger.DelegatedRunID == nil {
		t.Fatalf("implicit trigger=%+v", trigger)
	}
	if len(first.Comment.Mentions) != 0 {
		t.Fatalf("implicit route created structured mentions: %+v", first.Comment.Mentions)
	}
	if len(first.Events) != 2 || first.Events[0].Type != "issue.comment_created" || first.Events[1].Type != "run.created" {
		t.Fatalf("events=%+v", first.Events)
	}

	retry, err := f.store.CreateIssueCommentWithTriggers(ctx, f.project.ID, input, store.IssueCommentTriggerRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if retry.Comment.ID != first.Comment.ID || len(retry.Events) != 0 || retry.Comment.ImplicitTrigger == nil ||
		retry.Comment.ImplicitTrigger.DelegationID == nil || *retry.Comment.ImplicitTrigger.DelegationID != *trigger.DelegationID {
		t.Fatalf("retry=%+v first=%+v", retry, first)
	}
	runs, err := f.store.ListRuns(ctx, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("runs=%+v want original parent plus one implicit target", runs)
	}
	after, err := f.store.GetIssue(ctx, f.project.ID, f.issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != before.Status ||
		after.AssigneeType == nil || before.AssigneeType == nil || *after.AssigneeType != *before.AssigneeType ||
		after.AssigneeID == nil || before.AssigneeID == nil || *after.AssigneeID != *before.AssigneeID {
		t.Fatalf("implicit trigger mutated Issue: before=%+v after=%+v", before, after)
	}
}

func TestIssueCommentImplicitReplyPrecedencePreviewAndAmbiguity(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	author, err := f.store.CreateUser(ctx, authUser("thread-author", "thread-author@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}

	rootResult, err := f.store.CreateIssueComment(ctx, f.project.ID, store.IssueComment{
		IssueID: f.issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID, Body: "Thread root",
	})
	if err != nil {
		t.Fatal(err)
	}
	parentRunID := f.parentRun.ID
	parentAction := "parent-thread-comment"
	parentResult, err := f.store.CreateIssueComment(ctx, f.project.ID, store.IssueComment{
		IssueID: f.issue.ID, ParentCommentID: &rootResult.Comment.ID,
		AuthorType: store.ActorTypeAgent, AuthorID: f.parent.ID,
		SourceRunID: &parentRunID, SourceActionKey: &parentAction, Body: "Parent Agent reply",
	})
	if err != nil {
		t.Fatal(err)
	}

	preview, err := f.store.PreviewIssueCommentTriggers(
		ctx, f.project.ID, f.issue.ID, &parentResult.Comment.ID, "Human direct reply", store.IssueCommentTriggerRequest{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Implicit == nil ||
		preview.Implicit.TargetAgentID != f.parent.ID ||
		preview.Implicit.RoutingReason != store.IssueCommentImplicitRoutingReasonDirectAgentReply ||
		preview.Implicit.ReasonCode == nil || *preview.Implicit.ReasonCode != store.IssueCommentMentionReasonTargetBusy {
		t.Fatalf("direct preview=%+v", preview)
	}
	directKey := "direct-reply"
	direct, err := f.store.CreateIssueCommentWithTriggers(ctx, f.project.ID, store.IssueComment{
		IssueID: f.issue.ID, ParentCommentID: &parentResult.Comment.ID,
		AuthorType: store.ActorTypeHuman, AuthorID: author.ID, SourceActionKey: &directKey, Body: "Human direct reply",
	}, store.IssueCommentTriggerRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if direct.Comment.ImplicitTrigger == nil ||
		direct.Comment.ImplicitTrigger.TargetAgentID != preview.Implicit.TargetAgentID ||
		direct.Comment.ImplicitTrigger.RoutingReason != preview.Implicit.RoutingReason ||
		direct.Comment.ImplicitTrigger.Outcome != store.IssueCommentImplicitOutcomeBlocked ||
		direct.Comment.ImplicitTrigger.ReasonCode == nil ||
		*direct.Comment.ImplicitTrigger.ReasonCode != store.IssueCommentMentionReasonTargetBusy {
		t.Fatalf("direct post=%+v preview=%+v", direct.Comment.ImplicitTrigger, preview.Implicit)
	}

	uniqueKey := "unique-thread-reply"
	unique, err := f.store.CreateIssueCommentWithTriggers(ctx, f.project.ID, store.IssueComment{
		IssueID: f.issue.ID, ParentCommentID: &rootResult.Comment.ID,
		AuthorType: store.ActorTypeHuman, AuthorID: author.ID, SourceActionKey: &uniqueKey, Body: "Reply to the human root",
	}, store.IssueCommentTriggerRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if unique.Comment.ImplicitTrigger == nil ||
		unique.Comment.ImplicitTrigger.TargetAgentID != f.parent.ID ||
		unique.Comment.ImplicitTrigger.RoutingReason != store.IssueCommentImplicitRoutingReasonUniqueThreadAgent {
		t.Fatalf("unique thread trigger=%+v", unique.Comment.ImplicitTrigger)
	}

	var targetRunID string
	if err := f.store.pool.QueryRow(ctx, `
		INSERT INTO runs (project_id, issue_id, workspace_id, agent_id, attempt, status)
		VALUES ($1,$2,$3,$4,2,'QUEUED')
		RETURNING id::text
	`, f.project.ID, f.issue.ID, f.parentRun.WorkspaceID, f.target.ID).Scan(&targetRunID); err != nil {
		t.Fatal(err)
	}
	targetAction := "target-thread-comment"
	if _, err := f.store.CreateIssueComment(ctx, f.project.ID, store.IssueComment{
		IssueID: f.issue.ID, ParentCommentID: &rootResult.Comment.ID,
		AuthorType: store.ActorTypeAgent, AuthorID: f.target.ID,
		SourceRunID: &targetRunID, SourceActionKey: &targetAction, Body: "Second Agent participates",
	}); err != nil {
		t.Fatal(err)
	}

	ambiguousPreview, err := f.store.PreviewIssueCommentTriggers(
		ctx, f.project.ID, f.issue.ID, &rootResult.Comment.ID, "Ambiguous reply", store.IssueCommentTriggerRequest{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if ambiguousPreview.Implicit != nil {
		t.Fatalf("ambiguous preview routed unexpectedly: %+v", ambiguousPreview)
	}
	ambiguousKey := "ambiguous-thread-reply"
	ambiguous, err := f.store.CreateIssueCommentWithTriggers(ctx, f.project.ID, store.IssueComment{
		IssueID: f.issue.ID, ParentCommentID: &rootResult.Comment.ID,
		AuthorType: store.ActorTypeHuman, AuthorID: author.ID, SourceActionKey: &ambiguousKey, Body: "Ambiguous reply",
	}, store.IssueCommentTriggerRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if ambiguous.Comment.ImplicitTrigger != nil {
		t.Fatalf("ambiguous thread silently routed: %+v", ambiguous.Comment.ImplicitTrigger)
	}
}

func TestIssueCommentImplicitWorkflowSuppressionAndExplicitPrecedence(t *testing.T) {
	t.Run("workflow guards", func(t *testing.T) {
		for _, status := range []string{"BACKLOG", "DONE"} {
			t.Run(status, func(t *testing.T) {
				f := newDelegationFixture(t, true)
				ctx := t.Context()
				author, err := f.store.CreateUser(ctx, authUser("guard-"+status, "guard-"+status+"@example.com", store.UserStatusActive))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := f.store.pool.Exec(ctx, `UPDATE issues SET status=$3 WHERE project_id=$1 AND id=$2`, f.project.ID, f.issue.ID, status); err != nil {
					t.Fatal(err)
				}
				key := "guard-" + status
				result, err := f.store.CreateIssueCommentWithTriggers(ctx, f.project.ID, store.IssueComment{
					IssueID: f.issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID,
					SourceActionKey: &key, Body: "Do not wake parked or finished work.",
				}, store.IssueCommentTriggerRequest{})
				if err != nil {
					t.Fatal(err)
				}
				trigger := result.Comment.ImplicitTrigger
				if trigger == nil || trigger.TargetAgentID != f.parent.ID ||
					trigger.RoutingReason != store.IssueCommentImplicitRoutingReasonIssueAssignee ||
					trigger.Outcome != store.IssueCommentImplicitOutcomeBlocked ||
					trigger.ReasonCode == nil || *trigger.ReasonCode != store.IssueCommentImplicitReasonWorkflowBlocked ||
					trigger.DelegationID != nil || trigger.DelegatedRunID != nil {
					t.Fatalf("%s trigger=%+v", status, trigger)
				}
				runs, err := f.store.ListRuns(ctx, f.project.ID)
				if err != nil {
					t.Fatal(err)
				}
				if len(runs) != 1 {
					t.Fatalf("%s implicit wake created execution: %+v", status, runs)
				}
			})
		}
	})

	t.Run("per comment suppression", func(t *testing.T) {
		f := newDelegationFixture(t, true)
		ctx := t.Context()
		author, err := f.store.CreateUser(ctx, authUser("suppress-author", "suppress-author@example.com", store.UserStatusActive))
		if err != nil {
			t.Fatal(err)
		}
		key := "suppressed-comment"
		result, err := f.store.CreateIssueCommentWithTriggers(ctx, f.project.ID, store.IssueComment{
			IssueID: f.issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID,
			SourceActionKey: &key, Body: "Comment without an Agent wakeup.",
		}, store.IssueCommentTriggerRequest{SuppressImplicit: true})
		if err != nil {
			t.Fatal(err)
		}
		trigger := result.Comment.ImplicitTrigger
		if trigger == nil || trigger.TargetAgentID != f.parent.ID ||
			trigger.Outcome != store.IssueCommentImplicitOutcomeSuppressed ||
			trigger.ReasonCode != nil || trigger.DelegationID != nil || trigger.DelegatedRunID != nil {
			t.Fatalf("suppressed trigger=%+v", trigger)
		}
		reloaded, err := f.store.GetIssueComment(ctx, f.project.ID, f.issue.ID, result.Comment.ID)
		if err != nil {
			t.Fatal(err)
		}
		if reloaded.ImplicitTrigger == nil || reloaded.ImplicitTrigger.Outcome != store.IssueCommentImplicitOutcomeSuppressed {
			t.Fatalf("suppressed comment did not persist outcome: %+v", reloaded)
		}
	})

	t.Run("explicit mention disables implicit fallback", func(t *testing.T) {
		f := newDelegationFixture(t, true)
		ctx := t.Context()
		author, err := f.store.CreateUser(ctx, authUser("explicit-author", "explicit-author@example.com", store.UserStatusActive))
		if err != nil {
			t.Fatal(err)
		}
		key := "explicit-over-implicit"
		result, err := f.store.CreateIssueCommentWithTriggers(ctx, f.project.ID, store.IssueComment{
			IssueID: f.issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID,
			SourceActionKey: &key, Body: "Explicitly ask the target.",
		}, store.IssueCommentTriggerRequest{MentionAgentIDs: []string{f.target.ID}})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Comment.Mentions) != 1 || result.Comment.Mentions[0].TargetAgentID != f.target.ID {
			t.Fatalf("explicit mentions=%+v", result.Comment.Mentions)
		}
		if result.Comment.ImplicitTrigger != nil {
			t.Fatalf("explicit mention also created implicit target: %+v", result.Comment.ImplicitTrigger)
		}
	})
}

func TestIssueCommentImplicitOwnerFallbackExcludesUserAndUnassignedIssues(t *testing.T) {
	for _, test := range []struct {
		name         string
		assigneeType *string
	}{
		{name: "unassigned"},
		{name: "user", assigneeType: func() *string { value := "USER"; return &value }()},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newDelegationFixture(t, true)
			ctx := t.Context()
			author, err := f.store.CreateUser(ctx, authUser("owner-"+test.name, "owner-"+test.name+"@example.com", store.UserStatusActive))
			if err != nil {
				t.Fatal(err)
			}
			if test.assigneeType == nil {
				if _, err := f.store.pool.Exec(ctx, `UPDATE issues SET assignee_type=NULL, assignee_id=NULL WHERE project_id=$1 AND id=$2`, f.project.ID, f.issue.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := f.store.pool.Exec(ctx, `UPDATE issues SET assignee_type='USER', assignee_id=gen_random_uuid() WHERE project_id=$1 AND id=$2`, f.project.ID, f.issue.ID); err != nil {
					t.Fatal(err)
				}
			}
			key := "owner-" + test.name
			result, err := f.store.CreateIssueCommentWithTriggers(ctx, f.project.ID, store.IssueComment{
				IssueID: f.issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID,
				SourceActionKey: &key, Body: "No owner fallback Agent should be selected.",
			}, store.IssueCommentTriggerRequest{})
			if err != nil {
				t.Fatal(err)
			}
			if result.Comment.ImplicitTrigger != nil {
				t.Fatalf("%s unexpectedly routed: %+v", test.name, result.Comment.ImplicitTrigger)
			}
		})
	}
}

func TestIssueCommentImplicitPreviewRejectsCrossIssueParent(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	author, err := f.store.CreateUser(ctx, authUser("isolation-author", "isolation-author@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	other, err := f.store.CreateIssue(ctx, store.Issue{ProjectID: f.project.ID, Title: "Other routing Issue", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}
	otherComment, err := f.store.CreateIssueComment(ctx, f.project.ID, store.IssueComment{
		IssueID: other.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID, Body: "Other Issue comment",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.PreviewIssueCommentTriggers(
		ctx, f.project.ID, f.issue.ID, &otherComment.Comment.ID, "Cross-Issue reply", store.IssueCommentTriggerRequest{},
	); err == nil {
		t.Fatal("cross-Issue parent preview unexpectedly succeeded")
	}
}
