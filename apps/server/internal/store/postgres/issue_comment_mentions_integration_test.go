package postgres

import (
	"errors"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
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
		mention.WorkRequestID == nil || mention.DelegationID == nil || mention.DelegatedRunID == nil {
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

func TestHumanIssueCommentSquadMentionPersistsSquadIdentityAndLeaderProvenance(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	author, err := f.store.CreateUser(ctx, authUser("squad-mention-author", "squad-mention@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	squad, err := f.store.CreateSquad(ctx, store.Squad{ProjectID: f.project.ID, Name: "Backend Squad", LeaderAgentID: f.target.ID})
	if err != nil {
		t.Fatal(err)
	}
	requestKey := "squad-mention-request"
	result, err := f.store.CreateIssueCommentWithTargets(ctx, f.project.ID, store.IssueComment{
		IssueID: f.issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID,
		SourceActionKey: &requestKey, Body: "Please ask the backend squad.",
	}, []store.IssueCommentTarget{{Type: store.IssueCommentTargetTypeSquad, ID: squad.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Comment.Mentions) != 1 {
		t.Fatalf("mentions=%+v", result.Comment.Mentions)
	}
	mention := result.Comment.Mentions[0]
	if mention.Target.Type != store.IssueCommentTargetTypeSquad || mention.Target.ID != squad.ID ||
		mention.TargetName != squad.Name || mention.ResolvedAgentID != f.target.ID ||
		mention.ResolvedAgentName != f.target.Name || mention.Outcome != store.IssueCommentMentionOutcomeQueued ||
		mention.DelegationID == nil || mention.DelegatedRunID == nil {
		t.Fatalf("squad mention=%+v", mention)
	}
	delegation, err := f.store.GetDelegationByRun(ctx, f.project.ID, *mention.DelegatedRunID)
	if err != nil {
		t.Fatal(err)
	}
	if delegation.TargetAgentID != f.target.ID || delegation.SourceCommentID == nil || *delegation.SourceCommentID != result.Comment.ID {
		t.Fatalf("squad delegation=%+v", delegation)
	}
}

func TestHumanIssueCommentImplicitSquadOwnerRoutesThroughCanonicalAgentWork(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	author, err := f.store.CreateUser(ctx, authUser("implicit-squad-author", "implicit-squad@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	squad, err := app.New(f.store).CreateSquad(ctx, store.Squad{
		ProjectID:     f.project.ID,
		Name:          "Backend Squad",
		LeaderAgentID: f.parent.ID,
		Members:       []store.SquadMember{{Type: store.SquadMemberTypeAgent, ID: f.target.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, &store.Assignee{Type: "SQUAD", ID: squad.ID}, store.EmptyObject); err != nil {
		t.Fatal(err)
	}
	before, err := f.store.GetIssue(ctx, f.project.ID, f.issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	key := "implicit-squad-owner"
	result, err := app.New(f.store).CreateHumanIssueComment(ctx, app.CreateIssueCommentInput{
		ProjectID: f.project.ID, IssueID: f.issue.ID, Body: "Please continue the backend work.", RequestKey: key,
	}, author.ID)
	if err != nil {
		t.Fatal(err)
	}
	trigger := result.ImplicitTrigger
	if len(result.Mentions) != 0 || trigger == nil || trigger.Target.Type != store.IssueCommentTargetTypeSquad ||
		trigger.Target.ID != squad.ID || trigger.ResolvedAgentID != f.parent.ID ||
		trigger.RoutingReason != store.IssueCommentImplicitRoutingReasonIssueSquadAssignee ||
		trigger.Outcome != store.IssueCommentImplicitOutcomeDeferred || trigger.WorkRequestID == nil ||
		trigger.DelegationID != nil || trigger.DelegatedRunID != nil {
		t.Fatalf("implicit Squad trigger=%+v", trigger)
	}
	var targetAgentID, authorityKind string
	if err := f.store.pool.QueryRow(ctx, `
		SELECT target_agent_id::text, authority_kind
		FROM agent_work_requests
		WHERE project_id=$1 AND id=$2
	`, f.project.ID, *trigger.WorkRequestID).Scan(&targetAgentID, &authorityKind); err != nil {
		t.Fatal(err)
	}
	if targetAgentID != f.parent.ID || authorityKind != store.AgentWorkRequestAuthorityIssue {
		t.Fatalf("work request target=%q authority=%q", targetAgentID, authorityKind)
	}
	comments, err := f.store.ListIssueComments(ctx, f.project.ID, f.issue.ID)
	if err != nil || len(comments) != 1 || comments[0].ID != result.ID {
		t.Fatalf("comments=%+v err=%v", comments, err)
	}
	if owner := before.AssignedTo(); owner == nil || owner.Type != "SQUAD" || owner.ID != squad.ID {
		t.Fatalf("before owner=%+v", owner)
	}
	after, err := f.store.GetIssue(ctx, f.project.ID, f.issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != before.Status || after.AssignedTo() == nil || after.AssignedTo().Type != "SQUAD" || after.AssignedTo().ID != squad.ID {
		t.Fatalf("implicit comment changed Issue state: before=%+v after=%+v", before, after)
	}
	if count := countIssueRunsForAgent(t, f.store, f.issue.ID, f.target.ID); count != 0 {
		t.Fatalf("Squad member received implicit fanout Run count=%d", count)
	}
}

func TestHumanIssueCommentSquadMentionPreservesHistoricalLeaderProvenance(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	author, err := f.store.CreateUser(ctx, authUser("leader-history-author", "leader-history@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	issue, err := f.store.CreateIssue(ctx, store.Issue{ProjectID: f.project.ID, Title: "Squad leader history", Status: "BACKLOG"})
	if err != nil {
		t.Fatal(err)
	}
	squad, err := app.New(f.store).CreateSquad(ctx, store.Squad{ProjectID: f.project.ID, Name: "Backend Squad", LeaderAgentID: f.parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SetIssueAssignee(ctx, f.project.ID, issue.ID, &store.Assignee{Type: "SQUAD", ID: squad.ID}, store.EmptyObject); err != nil {
		t.Fatal(err)
	}
	create := func(key string) store.IssueComment {
		t.Helper()
		result, err := app.New(f.store).CreateHumanIssueComment(ctx, app.CreateIssueCommentInput{
			ProjectID: f.project.ID, IssueID: issue.ID, Body: "Please handle this Squad request.", RequestKey: key,
			MentionTargets: []store.IssueCommentTarget{{Type: store.IssueCommentTargetTypeSquad, ID: squad.ID}},
		}, author.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Mentions) != 1 {
			t.Fatalf("mentions=%+v", result.Mentions)
		}
		return result
	}
	first := create("leader-history-first")
	firstMention := first.Mentions[0]
	if firstMention.Target.Type != store.IssueCommentTargetTypeSquad || firstMention.Target.ID != squad.ID ||
		firstMention.ResolvedAgentID != f.parent.ID || firstMention.DelegatedRunID == nil || firstMention.DelegationID == nil {
		t.Fatalf("first mention=%+v", firstMention)
	}
	firstDelegation, err := f.store.GetDelegationByRun(ctx, f.project.ID, *firstMention.DelegatedRunID)
	if err != nil {
		t.Fatal(err)
	}
	if firstDelegation.TargetAgentID != f.parent.ID || firstDelegation.SourceCommentID == nil || *firstDelegation.SourceCommentID != first.ID {
		t.Fatalf("first delegation=%+v", firstDelegation)
	}

	squad.LeaderAgentID = f.target.ID
	if _, err := app.New(f.store).UpdateSquad(ctx, squad); err != nil {
		t.Fatal(err)
	}
	second := create("leader-history-second")
	secondMention := second.Mentions[0]
	if secondMention.Target.Type != store.IssueCommentTargetTypeSquad || secondMention.Target.ID != squad.ID ||
		secondMention.ResolvedAgentID != f.target.ID || secondMention.DelegatedRunID == nil || secondMention.DelegationID == nil {
		t.Fatalf("second mention=%+v", secondMention)
	}
	secondDelegation, err := f.store.GetDelegationByRun(ctx, f.project.ID, *secondMention.DelegatedRunID)
	if err != nil {
		t.Fatal(err)
	}
	if secondDelegation.TargetAgentID != f.target.ID || secondDelegation.SourceCommentID == nil || *secondDelegation.SourceCommentID != second.ID {
		t.Fatalf("second delegation=%+v", secondDelegation)
	}

	reloaded, err := f.store.GetIssueComment(ctx, f.project.ID, issue.ID, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Mentions) != 1 || reloaded.Mentions[0].Target.Type != store.IssueCommentTargetTypeSquad ||
		reloaded.Mentions[0].Target.ID != squad.ID || reloaded.Mentions[0].ResolvedAgentID != f.parent.ID {
		t.Fatalf("historical first mention=%+v", reloaded.Mentions)
	}
	owner, err := f.store.GetIssue(ctx, f.project.ID, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if owner.Status != "BACKLOG" || owner.AssignedTo() == nil || owner.AssignedTo().Type != "SQUAD" || owner.AssignedTo().ID != squad.ID {
		t.Fatalf("Squad ownership/status changed: %+v", owner)
	}
}

func TestHumanIssueCommentUnavailableSquadPersistsBlockedWithoutResolvedAgent(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	author, err := f.store.CreateUser(ctx, authUser("unavailable-squad-author", "unavailable-squad@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	issue, err := f.store.CreateIssue(ctx, store.Issue{ProjectID: f.project.ID, Title: "Unavailable Squad", Status: "BACKLOG"})
	if err != nil {
		t.Fatal(err)
	}
	squad, err := app.New(f.store).CreateSquad(ctx, store.Squad{ProjectID: f.project.ID, Name: "Backend Squad", LeaderAgentID: f.parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SetIssueAssignee(ctx, f.project.ID, issue.ID, &store.Assignee{Type: "SQUAD", ID: squad.ID}, store.EmptyObject); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.pool.Exec(ctx, `UPDATE agents SET state='DISABLED' WHERE id=$1`, f.parent.ID); err != nil {
		t.Fatal(err)
	}
	target := store.IssueCommentTarget{Type: store.IssueCommentTargetTypeSquad, ID: squad.ID}
	preview, err := f.store.PreviewIssueCommentTargets(ctx, f.project.ID, issue.ID, []store.IssueCommentTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview) != 1 || preview[0].Eligible || preview[0].ResolvedAgentID != "" || preview[0].ReasonCode == nil ||
		*preview[0].ReasonCode != store.IssueCommentMentionReasonTargetUnavailable {
		t.Fatalf("unavailable Squad preview=%+v", preview)
	}
	before, err := f.store.GetIssue(ctx, f.project.ID, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	key := "unavailable-squad-request"
	result, err := app.New(f.store).CreateHumanIssueComment(ctx, app.CreateIssueCommentInput{
		ProjectID: f.project.ID, IssueID: issue.ID, Body: "Please ask the unavailable Squad.", RequestKey: key, MentionTargets: []store.IssueCommentTarget{target},
	}, author.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Mentions) != 1 {
		t.Fatalf("mentions=%+v", result.Mentions)
	}
	mention := result.Mentions[0]
	if mention.Target != target || mention.ResolvedAgentID != "" || mention.Outcome != store.IssueCommentMentionOutcomeBlocked ||
		mention.ReasonCode == nil || *mention.ReasonCode != store.IssueCommentMentionReasonTargetUnavailable ||
		mention.WorkRequestID != nil || mention.DelegationID != nil || mention.DelegatedRunID != nil {
		t.Fatalf("unavailable Squad mention=%+v", mention)
	}
	if countIssueRuns(t, f.store, issue.ID) != 0 {
		t.Fatalf("unavailable Squad created a Run")
	}
	after, err := f.store.GetIssue(ctx, f.project.ID, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != before.Status || after.AssignedTo() == nil || after.AssignedTo().Type != "SQUAD" || after.AssignedTo().ID != squad.ID {
		t.Fatalf("blocked mention changed Issue state: before=%+v after=%+v", before, after)
	}
}

func countIssueRuns(t *testing.T, s *Store, issueID string) int {
	t.Helper()
	var count int
	if err := s.pool.QueryRow(t.Context(), `SELECT count(*) FROM runs WHERE issue_id=$1`, issueID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func countIssueRunsForAgent(t *testing.T, s *Store, issueID, agentID string) int {
	t.Helper()
	var count int
	if err := s.pool.QueryRow(t.Context(), `SELECT count(*) FROM runs WHERE issue_id=$1 AND agent_id=$2`, issueID, agentID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestHumanIssueCommentMultipleMentionsPersistMixedOutcomesAndRetryWithoutDuplicateDelegation(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	author, err := f.store.CreateUser(ctx, authUser("plural-mention-author", "plural-mention@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	disabledTarget := createDelegationAgent(t, ctx, f.store, f.project, "Disabled mention target", false)
	disabledTarget.State = "DISABLED"
	scope := f.project.ID
	if _, err := f.store.UpdateAgent(ctx, &scope, disabledTarget); err != nil {
		t.Fatal(err)
	}
	beforeIssue, err := f.store.GetIssue(ctx, f.project.ID, f.issue.ID)
	if err != nil {
		t.Fatal(err)
	}

	requestKey := "human-plural-mention-request"
	body := "Queue the available Agent and preserve the blocked outcome for the unavailable Agent."
	targets := []string{f.target.ID, disabledTarget.ID}
	result, err := f.store.CreateIssueCommentWithMentions(ctx, f.project.ID, store.IssueComment{
		IssueID: f.issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID,
		SourceActionKey: &requestKey, Body: body,
	}, targets)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Comment.Mentions) != 2 {
		t.Fatalf("mentions=%+v want two deterministic outcomes", result.Comment.Mentions)
	}
	queued, blocked := result.Comment.Mentions[0], result.Comment.Mentions[1]
	if queued.TargetAgentID != f.target.ID || queued.Outcome != store.IssueCommentMentionOutcomeQueued ||
		queued.ReasonCode != nil || queued.DelegationID == nil || queued.DelegatedRunID == nil {
		t.Fatalf("queued mention=%+v", queued)
	}
	if blocked.TargetAgentID != disabledTarget.ID || blocked.Outcome != store.IssueCommentMentionOutcomeBlocked ||
		blocked.ReasonCode == nil || *blocked.ReasonCode != store.IssueCommentMentionReasonTargetUnavailable ||
		blocked.DelegationID != nil || blocked.DelegatedRunID != nil {
		t.Fatalf("blocked mention=%+v", blocked)
	}

	delegation, err := f.store.GetDelegationByRun(ctx, f.project.ID, *queued.DelegatedRunID)
	if err != nil {
		t.Fatal(err)
	}
	if delegation.SourceCommentID == nil || *delegation.SourceCommentID != result.Comment.ID ||
		delegation.ParentRunID != "" || delegation.TargetAgentID != f.target.ID {
		t.Fatalf("queued human mention delegation=%+v", delegation)
	}

	retry, err := f.store.CreateIssueCommentWithMentions(ctx, f.project.ID, store.IssueComment{
		IssueID: f.issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID,
		SourceActionKey: &requestKey, Body: body,
	}, targets)
	if err != nil {
		t.Fatal(err)
	}
	if retry.Comment.ID != result.Comment.ID || len(retry.Events) != 0 || len(retry.Comment.Mentions) != 2 {
		t.Fatalf("idempotent retry=%+v", retry)
	}
	if retry.Comment.Mentions[0].DelegationID == nil || *retry.Comment.Mentions[0].DelegationID != *queued.DelegationID ||
		retry.Comment.Mentions[1].Outcome != store.IssueCommentMentionOutcomeBlocked ||
		retry.Comment.Mentions[1].ReasonCode == nil || *retry.Comment.Mentions[1].ReasonCode != store.IssueCommentMentionReasonTargetUnavailable {
		t.Fatalf("retry changed mention outcomes=%+v", retry.Comment.Mentions)
	}

	runs, err := f.store.ListRuns(ctx, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("Runs=%+v want parent plus exactly one delegated Run", runs)
	}
	comments, err := f.store.ListIssueComments(ctx, f.project.ID, f.issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 || comments[0].ID != result.Comment.ID {
		t.Fatalf("comments=%+v want one durable plural-mention comment", comments)
	}

	reloaded, err := New(f.store.pool).GetIssueComment(ctx, f.project.ID, f.issue.ID, result.Comment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Mentions) != 2 {
		t.Fatalf("reloaded mentions=%+v want two persisted outcomes", reloaded.Mentions)
	}
	reloadedQueued, reloadedBlocked := reloaded.Mentions[0], reloaded.Mentions[1]
	if reloadedQueued.TargetAgentID != f.target.ID ||
		reloadedQueued.Outcome != store.IssueCommentMentionOutcomeQueued ||
		reloadedQueued.DelegationID == nil || *reloadedQueued.DelegationID != *queued.DelegationID ||
		reloadedQueued.DelegatedRunID == nil || *reloadedQueued.DelegatedRunID != *queued.DelegatedRunID {
		t.Fatalf("reloaded queued mention=%+v want persisted queued delegation", reloadedQueued)
	}
	if reloadedBlocked.TargetAgentID != disabledTarget.ID ||
		reloadedBlocked.Outcome != store.IssueCommentMentionOutcomeBlocked ||
		reloadedBlocked.ReasonCode == nil || *reloadedBlocked.ReasonCode != store.IssueCommentMentionReasonTargetUnavailable ||
		reloadedBlocked.DelegationID != nil || reloadedBlocked.DelegatedRunID != nil {
		t.Fatalf("reloaded blocked mention=%+v want persisted blocked outcome", reloadedBlocked)
	}

	afterIssue, err := f.store.GetIssue(ctx, f.project.ID, f.issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterIssue.Status != beforeIssue.Status ||
		afterIssue.AssigneeType == nil || beforeIssue.AssigneeType == nil || *afterIssue.AssigneeType != *beforeIssue.AssigneeType ||
		afterIssue.AssigneeID == nil || beforeIssue.AssigneeID == nil || *afterIssue.AssigneeID != *beforeIssue.AssigneeID {
		t.Fatalf("plural mention changed Issue assignment/status: before=%+v after=%+v", beforeIssue, afterIssue)
	}
}

func TestIssueCommentMentionOversizedBodyPersistsBlockedWithoutExecution(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	author, err := f.store.CreateUser(ctx, authUser("oversized-mention-author", "oversized-mention@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	requestKey := "oversized-mention-request"
	body := strings.Repeat("x", store.MaxDelegationTaskCharacters+1)
	result, err := f.store.CreateIssueCommentWithMentions(ctx, f.project.ID, store.IssueComment{
		IssueID: f.issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID,
		SourceActionKey: &requestKey, Body: body,
	}, []string{f.target.ID})
	if err != nil {
		t.Fatal(err)
	}
	if result.Comment.ID == "" || len(result.Comment.Mentions) != 1 {
		t.Fatalf("comment=%+v", result.Comment)
	}
	mention := result.Comment.Mentions[0]
	if mention.Outcome != store.IssueCommentMentionOutcomeBlocked || mention.ReasonCode == nil ||
		*mention.ReasonCode != store.IssueCommentMentionReasonDelegationBlocked || mention.WorkRequestID != nil ||
		mention.DelegationID != nil || mention.DelegatedRunID != nil {
		t.Fatalf("oversized mention=%+v", mention)
	}
	runs, err := f.store.ListRuns(ctx, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("oversized mention created execution: %+v", runs)
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
	if len(busy) != 1 || !busy[0].Eligible || busy[0].ReasonCode != nil {
		t.Fatalf("active-target preview should remain eligible for coalesced/deferred work: %+v", busy)
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

func TestNormalizeIssueCommentMentionAgentIDsRejectsInvalidInput(t *testing.T) {
	valid := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	tooMany := make([]string, store.MaxIssueCommentMentions+1)
	for index := range tooMany {
		tooMany[index] = valid
	}
	for name, values := range map[string][]string{
		"too many":  tooMany,
		"blank":     {""},
		"invalid":   {"not-a-uuid"},
		"duplicate": {valid, valid},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeIssueCommentMentionAgentIDs(values); !errors.Is(err, store.ErrInvalidArgument) {
				t.Fatalf("error=%v want invalid argument", err)
			}
		})
	}
}

func TestHumanIssueCommentMentionsCoalesceQueuedAndDeferRunningTarget(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	author, err := f.store.CreateUser(ctx, authUser("coalesce-mention-author", "coalesce-mention@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}

	create := func(key, body string) store.IssueCommentMention {
		t.Helper()
		result, err := f.store.CreateIssueCommentWithMentions(ctx, f.project.ID, store.IssueComment{
			IssueID: f.issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID,
			SourceActionKey: &key, Body: body,
		}, []string{f.target.ID})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Comment.Mentions) != 1 {
			t.Fatalf("mentions=%+v", result.Comment.Mentions)
		}
		return result.Comment.Mentions[0]
	}

	first := create("coalesce-first", "First queued request")
	if first.Outcome != store.IssueCommentMentionOutcomeQueued || first.WorkRequestID == nil || first.DelegatedRunID == nil {
		t.Fatalf("first=%+v", first)
	}
	second := create("coalesce-second", "Second request before execution starts")
	if second.Outcome != store.IssueCommentMentionOutcomeCoalesced || second.WorkRequestID == nil || *second.WorkRequestID != *first.WorkRequestID ||
		second.DelegatedRunID == nil || *second.DelegatedRunID != *first.DelegatedRunID {
		t.Fatalf("coalesced=%+v first=%+v", second, first)
	}
	if _, err := f.store.pool.Exec(ctx, `UPDATE runs SET status='RUNNING', started_at=now(), updated_at=now() WHERE project_id=$1 AND id=$2`, f.project.ID, *first.DelegatedRunID); err != nil {
		t.Fatal(err)
	}
	deferred := create("coalesce-deferred", "Follow up after execution started")
	if deferred.Outcome != store.IssueCommentMentionOutcomeDeferred || deferred.WorkRequestID == nil || *deferred.WorkRequestID == *first.WorkRequestID || deferred.DelegatedRunID != nil {
		t.Fatalf("deferred=%+v first=%+v", deferred, first)
	}
	folded := create("coalesce-deferred-fold", "Another follow up while execution is active")
	if folded.Outcome != store.IssueCommentMentionOutcomeCoalesced || folded.WorkRequestID == nil || *folded.WorkRequestID != *deferred.WorkRequestID || folded.DelegatedRunID != nil {
		t.Fatalf("folded=%+v deferred=%+v", folded, deferred)
	}

	runs, err := f.store.ListRuns(ctx, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("comment burst created a Run storm: %+v", runs)
	}
}
