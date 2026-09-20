package postgres

import (
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestHumanIssueCommentMentionsPersistStableTargetsDispatchAndRetryIdempotently(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	beforeIssue, err := f.store.GetIssue(ctx, f.project.ID, f.issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	author, err := f.store.CreateUser(ctx, authUser("mention-author", "mention-author@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	requestKey := "human-mention-request-1"
	body := "Please inspect this focused request for @Target agent."

	result, err := f.store.CreateIssueCommentWithMentions(ctx, f.project.ID, store.IssueComment{
		IssueID: f.issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID,
		SourceActionKey: &requestKey, Body: body,
	}, []string{f.target.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Comment.Mentions) != 1 {
		t.Fatalf("mentions=%+v", result.Comment.Mentions)
	}
	mention := result.Comment.Mentions[0]
	if mention.TargetAgentID != f.target.ID || mention.TargetAgentName != f.target.Name ||
		mention.Outcome != store.IssueCommentMentionOutcomeQueued || mention.ReasonCode != nil ||
		mention.DelegationID == nil || mention.DelegatedRunID == nil {
		t.Fatalf("queued mention=%+v", mention)
	}
	if len(result.Events) != 2 || result.Events[0].Type != "issue.comment_created" || result.Events[1].Type != "run.created" {
		t.Fatalf("events=%+v", result.Events)
	}
	if result.Events[0].CreatedAt.After(result.Events[1].CreatedAt) {
		t.Fatalf("persisted event order is effect-before-cause: %+v", result.Events)
	}

	delegation, err := f.store.GetDelegationByRun(ctx, f.project.ID, *mention.DelegatedRunID)
	if err != nil {
		t.Fatal(err)
	}
	if delegation.SourceCommentID == nil || *delegation.SourceCommentID != result.Comment.ID ||
		delegation.ParentRunID != "" || delegation.TargetAgentID != f.target.ID {
		t.Fatalf("comment-origin delegation=%+v", delegation)
	}

	retry, err := f.store.CreateIssueCommentWithMentions(ctx, f.project.ID, store.IssueComment{
		IssueID: f.issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID,
		SourceActionKey: &requestKey, Body: body,
	}, []string{f.target.ID})
	if err != nil {
		t.Fatal(err)
	}
	if retry.Comment.ID != result.Comment.ID || len(retry.Events) != 0 || len(retry.Comment.Mentions) != 1 ||
		retry.Comment.Mentions[0].DelegationID == nil || *retry.Comment.Mentions[0].DelegationID != *mention.DelegationID {
		t.Fatalf("idempotent retry=%+v", retry)
	}
	runs, err := f.store.ListRuns(ctx, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("Runs=%+v want parent plus one mentioned target", runs)
	}
	afterIssue, err := f.store.GetIssue(ctx, f.project.ID, f.issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterIssue.Status != beforeIssue.Status || afterIssue.AssigneeType == nil || beforeIssue.AssigneeType == nil ||
		*afterIssue.AssigneeType != *beforeIssue.AssigneeType || afterIssue.AssigneeID == nil || beforeIssue.AssigneeID == nil ||
		*afterIssue.AssigneeID != *beforeIssue.AssigneeID {
		t.Fatalf("mention changed Issue ownership/status: before=%+v after=%+v", beforeIssue, afterIssue)
	}
}

func TestIssueCommentMentionBlockedTargetPreservesCommentAndSafeOutcome(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	author, err := f.store.CreateUser(ctx, authUser("blocked-mention-author", "blocked-mention@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	requestKey := "blocked-mention-request"
	unavailableTarget := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"

	result, err := f.store.CreateIssueCommentWithMentions(ctx, f.project.ID, store.IssueComment{
		IssueID: f.issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID,
		SourceActionKey: &requestKey, Body: "Please ask the unavailable Agent.",
	}, []string{unavailableTarget})
	if err != nil {
		t.Fatal(err)
	}
	if result.Comment.ID == "" || len(result.Comment.Mentions) != 1 {
		t.Fatalf("comment=%+v", result.Comment)
	}
	mention := result.Comment.Mentions[0]
	if mention.TargetAgentID != unavailableTarget || mention.TargetAgentName != "" ||
		mention.Outcome != store.IssueCommentMentionOutcomeBlocked || mention.ReasonCode == nil ||
		*mention.ReasonCode != store.IssueCommentMentionReasonTargetUnavailable ||
		mention.DelegationID != nil || mention.DelegatedRunID != nil {
		t.Fatalf("blocked mention=%+v", mention)
	}
	runs, err := f.store.ListRuns(ctx, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("blocked mention created execution: %+v", runs)
	}
}

func TestAgentIssueCommentMentionUsesParentDelegationPolicyAndPreservesCommentWhenBlocked(t *testing.T) {
	t.Run("allowed", func(t *testing.T) {
		f := newDelegationFixture(t, true)
		ctx := t.Context()
		runID := f.parentRun.ID
		actionKey := "agent-mention-allowed"

		result, err := f.store.CreateIssueCommentWithMentions(ctx, f.project.ID, store.IssueComment{
			IssueID: f.issue.ID, AuthorType: store.ActorTypeAgent, AuthorID: f.parent.ID,
			SourceRunID: &runID, SourceActionKey: &actionKey, Body: "Please inspect this bounded request.",
		}, []string{f.target.ID})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Comment.Mentions) != 1 {
			t.Fatalf("mentions=%+v", result.Comment.Mentions)
		}
		mention := result.Comment.Mentions[0]
		if mention.Outcome != store.IssueCommentMentionOutcomeQueued || mention.DelegationID == nil || mention.DelegatedRunID == nil {
			t.Fatalf("allowed mention=%+v", mention)
		}
		delegation, err := f.store.GetDelegationByRun(ctx, f.project.ID, *mention.DelegatedRunID)
		if err != nil {
			t.Fatal(err)
		}
		if delegation.ParentRunID != f.parentRun.ID || delegation.ParentAgentID != f.parent.ID || delegation.SourceCommentID != nil {
			t.Fatalf("Agent-origin delegation=%+v", delegation)
		}
		if delegation.DelegatedRunID != *mention.DelegatedRunID {
			t.Fatalf("delegation/mention mismatch: delegation=%+v mention=%+v", delegation, mention)
		}
	})

	t.Run("delegation disabled", func(t *testing.T) {
		f := newDelegationFixture(t, false)
		ctx := t.Context()
		runID := f.parentRun.ID
		actionKey := "agent-mention-blocked"

		result, err := f.store.CreateIssueCommentWithMentions(ctx, f.project.ID, store.IssueComment{
			IssueID: f.issue.ID, AuthorType: store.ActorTypeAgent, AuthorID: f.parent.ID,
			SourceRunID: &runID, SourceActionKey: &actionKey, Body: "Please inspect this bounded request.",
		}, []string{f.target.ID})
		if err != nil {
			t.Fatal(err)
		}
		if result.Comment.ID == "" || len(result.Comment.Mentions) != 1 {
			t.Fatalf("comment=%+v", result.Comment)
		}
		mention := result.Comment.Mentions[0]
		if mention.Outcome != store.IssueCommentMentionOutcomeBlocked || mention.ReasonCode == nil ||
			*mention.ReasonCode != store.IssueCommentMentionReasonDelegationBlocked || mention.DelegationID != nil {
			t.Fatalf("blocked Agent mention=%+v", mention)
		}
		runs, err := f.store.ListRuns(ctx, f.project.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) != 1 {
			t.Fatalf("disabled Agent mention created child execution: %+v", runs)
		}
	})
}

func TestIssueCommentMentionReloadUsesCurrentAgentNameAndDeletionPreservesProvenance(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	author, err := f.store.CreateUser(ctx, authUser("mention-reload-author", "mention-reload@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	requestKey := "mention-reload"
	result, err := f.store.CreateIssueCommentWithMentions(ctx, f.project.ID, store.IssueComment{
		IssueID: f.issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID,
		SourceActionKey: &requestKey, Body: "Please inspect this.",
	}, []string{f.target.ID})
	if err != nil {
		t.Fatal(err)
	}
	scope := f.project.ID
	f.target.Name = "Renamed target"
	if _, err := f.store.UpdateAgent(ctx, &scope, f.target); err != nil {
		t.Fatal(err)
	}

	reloaded, err := New(f.store.pool).GetIssueComment(ctx, f.project.ID, f.issue.ID, result.Comment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Mentions) != 1 || reloaded.Mentions[0].TargetAgentID != f.target.ID || reloaded.Mentions[0].TargetAgentName != "Renamed target" {
		t.Fatalf("reloaded mention=%+v", reloaded.Mentions)
	}

	if _, err := f.store.DeleteIssueComment(ctx, f.project.ID, f.issue.ID, result.Comment.ID, author.ID); err != nil {
		t.Fatal(err)
	}
	deleted, err := f.store.GetIssueComment(ctx, f.project.ID, f.issue.ID, result.Comment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.DeletedAt == nil || deleted.Body != "" || len(deleted.Mentions) != 1 {
		t.Fatalf("mention-bearing comment deletion lost provenance: %+v", deleted)
	}
}

func TestIssueCommentMentionValidationRejectsDuplicateOrChangedRetryTargets(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	author, err := f.store.CreateUser(ctx, authUser("mention-validation-author", "mention-validation@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	requestKey := "mention-validation"

	if _, err := f.store.CreateIssueCommentWithMentions(ctx, f.project.ID, store.IssueComment{
		IssueID: f.issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID,
		SourceActionKey: &requestKey, Body: "duplicate",
	}, []string{f.target.ID, f.target.ID}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("duplicate target error=%v", err)
	}

	created, err := f.store.CreateIssueCommentWithMentions(ctx, f.project.ID, store.IssueComment{
		IssueID: f.issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID,
		SourceActionKey: &requestKey, Body: "stable targets",
	}, []string{f.target.ID})
	if err != nil {
		t.Fatal(err)
	}
	if created.Comment.ID == "" {
		t.Fatal("comment was not created")
	}
	if _, err := f.store.CreateIssueCommentWithMentions(ctx, f.project.ID, store.IssueComment{
		IssueID: f.issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID,
		SourceActionKey: &requestKey, Body: "stable targets",
	}, nil); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("changed retry targets error=%v want conflict", err)
	}
}

func TestIssueCommentMentionPreviewUsesPostingEligibility(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()

	preview, err := f.store.PreviewIssueCommentMentions(ctx, f.project.ID, f.issue.ID, []string{f.target.ID, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview) != 2 || !preview[0].Eligible || preview[0].TargetAgentName != f.target.Name ||
		preview[0].ReasonCode != nil || preview[1].Eligible || preview[1].ReasonCode == nil ||
		*preview[1].ReasonCode != store.IssueCommentMentionReasonTargetUnavailable {
		t.Fatalf("preview=%+v", preview)
	}

	author, err := f.store.CreateUser(ctx, authUser("preview-author", "preview-author@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	requestKey := "preview-dispatch"
	if _, err := f.store.CreateIssueCommentWithMentions(ctx, f.project.ID, store.IssueComment{
		IssueID: f.issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID,
		SourceActionKey: &requestKey, Body: "Create target work.",
	}, []string{f.target.ID}); err != nil {
		t.Fatal(err)
	}

	busy, err := f.store.PreviewIssueCommentMentions(ctx, f.project.ID, f.issue.ID, []string{f.target.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(busy) != 1 || busy[0].Eligible || busy[0].ReasonCode == nil || *busy[0].ReasonCode != store.IssueCommentMentionReasonTargetBusy {
		t.Fatalf("busy preview=%+v", busy)
	}
}

func TestIssueCommentMentionTargetIsProjectScoped(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	author, err := f.store.CreateUser(ctx, authUser("mention-project-author", "mention-project@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	otherProject, err := f.store.CreateProject(ctx, testProjectInput("Other mention project", "/repos/other-mention", "OM"))
	if err != nil {
		t.Fatal(err)
	}
	foreign := createDelegationAgent(t, ctx, f.store, otherProject, "Foreign agent", false)

	preview, err := f.store.PreviewIssueCommentMentions(ctx, f.project.ID, f.issue.ID, []string{foreign.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview) != 1 || preview[0].Eligible || preview[0].ReasonCode == nil ||
		*preview[0].ReasonCode != store.IssueCommentMentionReasonTargetUnavailable || preview[0].TargetAgentName != "" {
		t.Fatalf("cross-Project preview leaked target state: %+v", preview)
	}

	requestKey := "cross-project-target"
	result, err := f.store.CreateIssueCommentWithMentions(ctx, f.project.ID, store.IssueComment{
		IssueID: f.issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID,
		SourceActionKey: &requestKey, Body: "Try a foreign target.",
	}, []string{foreign.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Comment.Mentions) != 1 || result.Comment.Mentions[0].Outcome != store.IssueCommentMentionOutcomeBlocked ||
		result.Comment.Mentions[0].ReasonCode == nil || *result.Comment.Mentions[0].ReasonCode != store.IssueCommentMentionReasonTargetUnavailable {
		t.Fatalf("cross-Project mention=%+v", result.Comment.Mentions)
	}
	foreignRuns, err := f.store.ListRuns(ctx, otherProject.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(foreignRuns) != 0 {
		t.Fatalf("cross-Project mention created foreign execution: %+v", foreignRuns)
	}
}
