package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type issueCommentWithoutMentionsStore struct {
	store.ControlPlaneStore
	store.IssueCommentStore
}

type issueCommentTestStore struct {
	*projectWorkflowAuthorizationStore
	comments            []store.IssueComment
	events              []store.Event
	commentErr          error
	mutationErr         error
	activityErr         error
	createCalls         int
	lastCreate          store.IssueComment
	mentionPreview      []store.IssueCommentMentionPreview
	mentionErr          error
	mentionCreateCalls  int
	lastMentionAgentIDs []string
	targetPreview       []store.IssueCommentMentionPreview
	targetCreateCalls   int
	lastTargets         []store.IssueCommentTarget
}

func (s *issueCommentTestStore) ListIssueComments(_ context.Context, projectID, issueID string) ([]store.IssueComment, error) {
	if s.commentErr != nil {
		return nil, s.commentErr
	}
	if projectID != s.project.ID {
		return nil, store.ErrNotFound
	}
	if _, err := s.GetIssue(context.Background(), projectID, issueID); err != nil {
		return nil, err
	}
	return append([]store.IssueComment(nil), s.comments...), nil
}

func (s *issueCommentTestStore) CreateIssueComment(_ context.Context, projectID string, input store.IssueComment) (store.IssueCommentMutationResult, error) {
	if s.commentErr != nil {
		return store.IssueCommentMutationResult{}, s.commentErr
	}
	if projectID != s.project.ID {
		return store.IssueCommentMutationResult{}, store.ErrNotFound
	}
	s.createCalls++
	s.lastCreate = input
	input.ID = "comment-created"
	input.AuthorName = "Member"
	input.CreatedAt = time.Date(2026, 9, 19, 0, 0, 2, 0, time.UTC)
	input.UpdatedAt = input.CreatedAt
	s.comments = append(s.comments, input)
	event := store.Event{ID: "comment-event", Type: "issue.comment_created", ProjectID: projectID, IssueID: &input.IssueID, OccurredAt: input.CreatedAt}
	return store.IssueCommentMutationResult{Comment: input, Events: []store.Event{event}}, nil
}

func (s *issueCommentTestStore) CreateIssueCommentWithMentions(ctx context.Context, projectID string, input store.IssueComment, targetAgentIDs []string) (store.IssueCommentMutationResult, error) {
	s.mentionCreateCalls++
	s.lastMentionAgentIDs = append([]string(nil), targetAgentIDs...)
	result, err := s.CreateIssueComment(ctx, projectID, input)
	if err != nil {
		return store.IssueCommentMutationResult{}, err
	}
	result.Comment.Mentions = make([]store.IssueCommentMention, 0, len(targetAgentIDs))
	for index, targetAgentID := range targetAgentIDs {
		result.Comment.Mentions = append(result.Comment.Mentions, store.IssueCommentMention{
			ID:              "mention-" + targetAgentID,
			TargetAgentID:   targetAgentID,
			TargetAgentName: "Agent " + targetAgentID,
			Outcome:         store.IssueCommentMentionOutcomeQueued,
			CreatedAt:       result.Comment.CreatedAt.Add(time.Duration(index) * time.Millisecond),
		})
	}
	s.comments[len(s.comments)-1] = result.Comment
	return result, nil
}

func (s *issueCommentTestStore) CreateIssueCommentWithTriggers(ctx context.Context, projectID string, input store.IssueComment, request store.IssueCommentTriggerRequest) (store.IssueCommentMutationResult, error) {
	if len(request.MentionTargets) != 0 {
		return s.CreateIssueCommentWithTargets(ctx, projectID, input, request.MentionTargets)
	}
	if len(request.MentionAgentIDs) != 0 {
		return s.CreateIssueCommentWithMentions(ctx, projectID, input, request.MentionAgentIDs)
	}
	return s.CreateIssueComment(ctx, projectID, input)
}

func (s *issueCommentTestStore) CreateIssueCommentWithTargets(ctx context.Context, projectID string, input store.IssueComment, targets []store.IssueCommentTarget) (store.IssueCommentMutationResult, error) {
	s.targetCreateCalls++
	s.lastTargets = append([]store.IssueCommentTarget(nil), targets...)
	result, err := s.CreateIssueComment(ctx, projectID, input)
	if err != nil {
		return store.IssueCommentMutationResult{}, err
	}
	for _, target := range targets {
		result.Comment.Mentions = append(result.Comment.Mentions, store.IssueCommentMention{Target: target, TargetName: "Squad", Outcome: store.IssueCommentMentionOutcomeQueued})
	}
	s.comments[len(s.comments)-1] = result.Comment
	return result, nil
}

func (s *issueCommentTestStore) PreviewIssueCommentTriggers(ctx context.Context, projectID, issueID string, _ *string, _ string, request store.IssueCommentTriggerRequest) (store.IssueCommentTriggerPreview, error) {
	mentions, err := s.PreviewIssueCommentMentions(ctx, projectID, issueID, request.MentionAgentIDs)
	if err != nil {
		return store.IssueCommentTriggerPreview{}, err
	}
	return store.IssueCommentTriggerPreview{Mentions: mentions}, nil
}

func (s *issueCommentTestStore) PreviewIssueCommentMentions(_ context.Context, projectID, issueID string, targetAgentIDs []string) ([]store.IssueCommentMentionPreview, error) {
	if s.mentionErr != nil {
		return nil, s.mentionErr
	}
	if projectID != s.project.ID {
		return nil, store.ErrNotFound
	}
	if _, err := s.GetIssue(context.Background(), projectID, issueID); err != nil {
		return nil, err
	}
	if s.mentionPreview != nil {
		return append([]store.IssueCommentMentionPreview(nil), s.mentionPreview...), nil
	}
	result := make([]store.IssueCommentMentionPreview, 0, len(targetAgentIDs))
	for _, targetAgentID := range targetAgentIDs {
		result = append(result, store.IssueCommentMentionPreview{
			TargetAgentID:   targetAgentID,
			TargetAgentName: "Agent " + targetAgentID,
			Eligible:        true,
		})
	}
	return result, nil
}

func (s *issueCommentTestStore) PreviewIssueCommentTargets(_ context.Context, projectID, issueID string, targets []store.IssueCommentTarget) ([]store.IssueCommentMentionPreview, error) {
	if s.mentionErr != nil {
		return nil, s.mentionErr
	}
	if projectID != s.project.ID {
		return nil, store.ErrNotFound
	}
	if s.targetPreview != nil {
		return append([]store.IssueCommentMentionPreview(nil), s.targetPreview...), nil
	}
	result := make([]store.IssueCommentMentionPreview, 0, len(targets))
	for _, target := range targets {
		result = append(result, store.IssueCommentMentionPreview{Target: target, TargetName: "Squad", Eligible: true})
	}
	return result, nil
}

func (s *issueCommentTestStore) GetIssueComment(_ context.Context, projectID, issueID, commentID string) (store.IssueComment, error) {
	if projectID != s.project.ID {
		return store.IssueComment{}, store.ErrNotFound
	}
	for _, comment := range s.comments {
		if comment.IssueID == issueID && comment.ID == commentID {
			return comment, nil
		}
	}
	return store.IssueComment{}, store.ErrNotFound
}

func (s *issueCommentTestStore) UpdateIssueComment(_ context.Context, projectID, issueID, commentID, actorID, body string) (store.IssueCommentMutationResult, error) {
	if s.mutationErr != nil {
		return store.IssueCommentMutationResult{}, s.mutationErr
	}
	for index := range s.comments {
		comment := &s.comments[index]
		if projectID == s.project.ID && comment.IssueID == issueID && comment.ID == commentID {
			if comment.Body == body {
				return store.IssueCommentMutationResult{Comment: *comment}, nil
			}
			comment.Body = body
			comment.UpdatedAt = comment.UpdatedAt.Add(time.Second)
			event := store.Event{ID: "comment-edited", Type: "issue.comment_changed", ProjectID: projectID, IssueID: &issueID, OccurredAt: comment.UpdatedAt}
			return store.IssueCommentMutationResult{Comment: *comment, Events: []store.Event{event}}, nil
		}
	}
	return store.IssueCommentMutationResult{}, store.ErrNotFound
}

func (s *issueCommentTestStore) DeleteIssueComment(_ context.Context, projectID, issueID, commentID, actorID string) (store.IssueCommentDeleteResult, error) {
	if s.mutationErr != nil {
		return store.IssueCommentDeleteResult{}, s.mutationErr
	}
	for index := range s.comments {
		comment := &s.comments[index]
		if projectID != s.project.ID || comment.IssueID != issueID || comment.ID != commentID {
			continue
		}
		hasReplies := false
		for _, candidate := range s.comments {
			if candidate.ParentCommentID != nil && *candidate.ParentCommentID == commentID {
				hasReplies = true
				break
			}
		}
		if hasReplies {
			at := comment.CreatedAt.Add(2 * time.Second)
			comment.Body = ""
			comment.DeletedAt = &at
			comment.ResolvedAt = nil
			comment.ResolvedByUserID = nil
			comment.Reactions = nil
		} else {
			s.comments = append(s.comments[:index], s.comments[index+1:]...)
		}
		event := store.Event{ID: "comment-deleted", Type: "issue.comment_changed", ProjectID: projectID, IssueID: &issueID}
		return store.IssueCommentDeleteResult{Events: []store.Event{event}}, nil
	}
	return store.IssueCommentDeleteResult{}, store.ErrNotFound
}

func (s *issueCommentTestStore) ResolveIssueComment(_ context.Context, projectID, issueID, commentID, actorID string) (store.IssueCommentMutationResult, error) {
	if s.mutationErr != nil {
		return store.IssueCommentMutationResult{}, s.mutationErr
	}
	for index := range s.comments {
		comment := &s.comments[index]
		if projectID == s.project.ID && comment.IssueID == issueID && comment.ID == commentID {
			if comment.ResolvedAt != nil {
				return store.IssueCommentMutationResult{Comment: *comment}, nil
			}
			at := comment.CreatedAt.Add(3 * time.Second)
			comment.ResolvedAt = &at
			comment.ResolvedByUserID = &actorID
			event := store.Event{ID: "comment-resolved", Type: "issue.comment_changed", ProjectID: projectID, IssueID: &issueID}
			return store.IssueCommentMutationResult{Comment: *comment, Events: []store.Event{event}}, nil
		}
	}
	return store.IssueCommentMutationResult{}, store.ErrNotFound
}

func (s *issueCommentTestStore) ReopenIssueComment(_ context.Context, projectID, issueID, commentID, actorID string) (store.IssueCommentMutationResult, error) {
	if s.mutationErr != nil {
		return store.IssueCommentMutationResult{}, s.mutationErr
	}
	for index := range s.comments {
		comment := &s.comments[index]
		if projectID == s.project.ID && comment.IssueID == issueID && comment.ID == commentID {
			if comment.ResolvedAt == nil {
				return store.IssueCommentMutationResult{Comment: *comment}, nil
			}
			comment.ResolvedAt = nil
			comment.ResolvedByUserID = nil
			event := store.Event{ID: "comment-reopened", Type: "issue.comment_changed", ProjectID: projectID, IssueID: &issueID}
			return store.IssueCommentMutationResult{Comment: *comment, Events: []store.Event{event}}, nil
		}
	}
	return store.IssueCommentMutationResult{}, store.ErrNotFound
}

func (s *issueCommentTestStore) AddIssueCommentReaction(_ context.Context, projectID, issueID, commentID, actorID, reaction string) ([]store.Event, error) {
	if s.mutationErr != nil {
		return nil, s.mutationErr
	}
	for index := range s.comments {
		comment := &s.comments[index]
		if projectID != s.project.ID || comment.IssueID != issueID || comment.ID != commentID {
			continue
		}
		for summaryIndex := range comment.Reactions {
			summary := &comment.Reactions[summaryIndex]
			if summary.Reaction != reaction {
				continue
			}
			for _, existing := range summary.ActorIDs {
				if existing == actorID {
					return nil, nil
				}
			}
			summary.Count++
			summary.ActorIDs = append(summary.ActorIDs, actorID)
			event := store.Event{ID: "reaction-added", Type: "issue.comment_changed", ProjectID: projectID, IssueID: &issueID}
			return []store.Event{event}, nil
		}
		comment.Reactions = append(comment.Reactions, store.IssueCommentReactionSummary{Reaction: reaction, Count: 1, ActorIDs: []string{actorID}})
		event := store.Event{ID: "reaction-added", Type: "issue.comment_changed", ProjectID: projectID, IssueID: &issueID}
		return []store.Event{event}, nil
	}
	return nil, store.ErrNotFound
}

func (s *issueCommentTestStore) RemoveIssueCommentReaction(_ context.Context, projectID, issueID, commentID, actorID, reaction string) ([]store.Event, error) {
	if s.mutationErr != nil {
		return nil, s.mutationErr
	}
	for index := range s.comments {
		comment := &s.comments[index]
		if projectID != s.project.ID || comment.IssueID != issueID || comment.ID != commentID {
			continue
		}
		for summaryIndex := range comment.Reactions {
			summary := &comment.Reactions[summaryIndex]
			if summary.Reaction != reaction {
				continue
			}
			for actorIndex, existing := range summary.ActorIDs {
				if existing != actorID {
					continue
				}
				summary.ActorIDs = append(summary.ActorIDs[:actorIndex], summary.ActorIDs[actorIndex+1:]...)
				summary.Count--
				if summary.Count == 0 {
					comment.Reactions = append(comment.Reactions[:summaryIndex], comment.Reactions[summaryIndex+1:]...)
				}
				event := store.Event{ID: "reaction-removed", Type: "issue.comment_changed", ProjectID: projectID, IssueID: &issueID}
				return []store.Event{event}, nil
			}
		}
		return nil, nil
	}
	return nil, store.ErrNotFound
}

func (s *issueCommentTestStore) ListIssueTimelineEvents(_ context.Context, projectID, issueID string) ([]store.Event, error) {
	if s.activityErr != nil {
		return nil, s.activityErr
	}
	if projectID != s.project.ID {
		return nil, store.ErrNotFound
	}
	if _, err := s.GetIssue(context.Background(), projectID, issueID); err != nil {
		return nil, err
	}
	return append([]store.Event(nil), s.events...), nil
}

func TestProjectAccessIssueCommentsUseAuthenticatedHumanAndMemberPolicy(t *testing.T) {
	const projectID = "project-1"
	const issueID = "issue-1"
	base := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, Name: "Project", IssuePrefix: "AB"},
		roles: map[string]string{
			"viewer": store.ProjectRoleViewer,
			"member": store.ProjectRoleMember,
		},
		issues: map[string]store.Issue{
			issueID: {ID: issueID, ProjectID: projectID, Number: 1, Title: "Issue", Status: "TODO"},
		},
		runs: map[string]store.Run{},
	}
	fake := &issueCommentTestStore{projectWorkflowAuthorizationStore: base}
	service := New(fake)
	publisher := &assigneePublisher{}
	service.SetEventRecorder(publisher)
	access, err := NewProjectAccessService(service, fake)
	if err != nil {
		t.Fatal(err)
	}

	viewer := activeProjectActor("viewer", store.DeploymentRoleMember)
	if _, err := access.CreateIssueComment(t.Context(), viewer, CreateIssueCommentInput{ProjectID: projectID, IssueID: issueID, Body: "viewer"}); err == nil {
		t.Fatal("viewer comment creation unexpectedly succeeded")
	}
	if fake.createCalls != 0 {
		t.Fatalf("viewer reached comment store %d times", fake.createCalls)
	}

	member := activeProjectActor("member", store.DeploymentRoleMember)
	parentID := "comment-parent"
	created, err := access.CreateIssueComment(t.Context(), member, CreateIssueCommentInput{
		ProjectID:       projectID,
		IssueID:         issueID,
		ParentCommentID: &parentID,
		Body:            "Please inspect @name literally",
		RequestKey:      "comment-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.AuthorType != store.ActorTypeHuman || created.AuthorID != member.ID {
		t.Fatalf("created author=%s/%s want HUMAN/%s", created.AuthorType, created.AuthorID, member.ID)
	}
	if fake.lastCreate.AuthorType != store.ActorTypeHuman || fake.lastCreate.AuthorID != member.ID {
		t.Fatalf("store author=%s/%s want authenticated member", fake.lastCreate.AuthorType, fake.lastCreate.AuthorID)
	}
	if fake.lastCreate.Body != "Please inspect @name literally" || fake.lastCreate.ParentCommentID == nil || *fake.lastCreate.ParentCommentID != parentID {
		t.Fatalf("store input=%+v", fake.lastCreate)
	}
	if len(publisher.published) != 1 || publisher.published[0].Type != "issue.comment_created" {
		t.Fatalf("published=%+v", publisher.published)
	}

	comments, err := access.ListIssueComments(t.Context(), viewer, projectID, issueID)
	if err != nil || len(comments) != 1 || comments[0].ID != created.ID {
		t.Fatalf("viewer comments=%+v err=%v", comments, err)
	}
	timeline, err := access.ListIssueTimeline(t.Context(), viewer, projectID, issueID)
	if err != nil || len(timeline) != 1 || timeline[0].Kind != store.IssueTimelineKindComment {
		t.Fatalf("viewer timeline=%+v err=%v", timeline, err)
	}

	outside := activeProjectActor("outside", store.DeploymentRoleMember)
	if _, err := access.ListIssueComments(t.Context(), outside, projectID, issueID); err == nil {
		t.Fatal("outside comment read unexpectedly succeeded")
	}
	if _, err := access.ListIssueTimeline(t.Context(), outside, projectID, issueID); err == nil {
		t.Fatal("outside timeline read unexpectedly succeeded")
	}
}

func TestIssueCommentMentionApplicationFailsClosedWithoutMentionStore(t *testing.T) {
	const projectID = "project-1"
	const issueID = "issue-1"
	base := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, IssuePrefix: "AB"},
		issues: map[string]store.Issue{
			issueID: {ID: issueID, ProjectID: projectID, Number: 1, Title: "Issue", Status: "TODO"},
		},
		runs: map[string]store.Run{},
	}
	full := &issueCommentTestStore{projectWorkflowAuthorizationStore: base}
	withoutMentions := &issueCommentWithoutMentionsStore{
		ControlPlaneStore: full,
		IssueCommentStore: full,
	}
	service := New(withoutMentions)
	if _, err := service.PreviewIssueCommentMentions(t.Context(), projectID, issueID, []string{"agent-2"}); err == nil {
		t.Fatal("preview unexpectedly succeeded without structured mention store")
	}
	if _, err := service.PreviewIssueCommentTargets(t.Context(), projectID, issueID, []store.IssueCommentTarget{{Type: store.IssueCommentTargetTypeSquad, ID: "squad-2"}}); err == nil {
		t.Fatal("typed preview unexpectedly succeeded without structured mention store")
	}

	noComments := New(base)
	if _, err := noComments.CreateHumanIssueComment(t.Context(), CreateIssueCommentInput{
		ProjectID: projectID, IssueID: issueID, Body: "body",
	}, "author"); err == nil {
		t.Fatal("comment creation unexpectedly succeeded without Issue comment store")
	}
}

func TestIssueCommentMentionApplicationPreviewAndCreate(t *testing.T) {
	const projectID = "project-1"
	const issueID = "issue-1"
	base := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, IssuePrefix: "AB"},
		issues: map[string]store.Issue{
			issueID: {ID: issueID, ProjectID: projectID, Number: 1, Title: "Issue", Status: "TODO"},
		},
		runs: map[string]store.Run{},
	}
	reason := store.IssueCommentMentionReasonTargetBusy
	fake := &issueCommentTestStore{
		projectWorkflowAuthorizationStore: base,
		mentionPreview: []store.IssueCommentMentionPreview{{
			TargetAgentID: "agent-2", TargetAgentName: "Verifier", Eligible: false, ReasonCode: &reason,
		}},
		targetPreview: []store.IssueCommentMentionPreview{{
			Target: store.IssueCommentTarget{Type: store.IssueCommentTargetTypeSquad, ID: "squad-2"}, TargetName: "Backend", Eligible: true,
		}},
	}
	service := New(fake)

	preview, err := service.PreviewIssueCommentMentions(t.Context(), projectID, issueID, []string{"agent-2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview) != 1 || preview[0].TargetAgentID != "agent-2" || preview[0].TargetAgentName != "Verifier" ||
		preview[0].Eligible || preview[0].ReasonCode == nil || *preview[0].ReasonCode != reason {
		t.Fatalf("preview=%+v", preview)
	}

	created, err := service.CreateHumanIssueComment(t.Context(), CreateIssueCommentInput{
		ProjectID: projectID, IssueID: issueID, Body: "Please inspect this.", RequestKey: "mention-request",
		MentionAgentIDs: []string{"agent-2"},
	}, "author")
	if err != nil {
		t.Fatal(err)
	}
	if fake.mentionCreateCalls != 1 || len(fake.lastMentionAgentIDs) != 1 || fake.lastMentionAgentIDs[0] != "agent-2" ||
		len(created.Mentions) != 1 || created.Mentions[0].TargetAgentID != "agent-2" {
		t.Fatalf("created=%+v calls=%d targets=%+v", created, fake.mentionCreateCalls, fake.lastMentionAgentIDs)
	}

	targets := []store.IssueCommentTarget{{Type: store.IssueCommentTargetTypeSquad, ID: "squad-2"}}
	targetPreview, err := service.PreviewIssueCommentTargets(t.Context(), projectID, issueID, targets)
	if err != nil {
		t.Fatal(err)
	}
	if len(targetPreview) != 1 || targetPreview[0].Target != targets[0] || targetPreview[0].TargetName != "Backend" {
		t.Fatalf("target preview=%+v", targetPreview)
	}
	fake.mentionErr = store.ErrInvalidArgument
	if _, err := service.PreviewIssueCommentTargets(t.Context(), projectID, issueID, targets); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("typed preview error=%v want invalid argument", err)
	}
	fake.mentionErr = nil
	targetCreated, err := service.CreateHumanIssueComment(t.Context(), CreateIssueCommentInput{
		ProjectID: projectID, IssueID: issueID, Body: "Please ask the squad.", RequestKey: "squad-request", MentionTargets: targets,
	}, "author")
	if err != nil {
		t.Fatal(err)
	}
	if fake.targetCreateCalls != 1 || len(fake.lastTargets) != 1 || fake.lastTargets[0] != targets[0] ||
		len(targetCreated.Mentions) != 1 || targetCreated.Mentions[0].Target != targets[0] {
		t.Fatalf("target created=%+v calls=%d targets=%+v", targetCreated, fake.targetCreateCalls, fake.lastTargets)
	}

	fake.mentionErr = store.ErrInvalidArgument
	if _, err := service.PreviewIssueCommentMentions(t.Context(), projectID, issueID, []string{"agent-2"}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("preview error=%v want invalid argument", err)
	}
}

func TestIssueCommentTriggerApplicationPreviewUsesSharedCapabilityAndErrors(t *testing.T) {
	const projectID = "project-1"
	const issueID = "issue-1"
	base := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, IssuePrefix: "AB"},
		issues: map[string]store.Issue{
			issueID: {ID: issueID, ProjectID: projectID, Number: 1, Title: "Issue", Status: "TODO"},
		},
		runs: map[string]store.Run{},
	}
	reason := store.IssueCommentMentionReasonTargetBusy
	fake := &issueCommentTestStore{
		projectWorkflowAuthorizationStore: base,
		mentionPreview: []store.IssueCommentMentionPreview{{
			TargetAgentID: "agent-2", TargetAgentName: "Verifier", Eligible: false, ReasonCode: &reason,
		}},
	}
	service := New(fake)
	parentID := "comment-parent"

	preview, err := service.PreviewIssueCommentTriggers(
		t.Context(), projectID, issueID, &parentID, "Please inspect this.",
		store.IssueCommentTriggerRequest{MentionAgentIDs: []string{"agent-2"}, SuppressImplicit: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Mentions) != 1 || preview.Mentions[0].TargetAgentID != "agent-2" ||
		preview.Mentions[0].ReasonCode == nil || *preview.Mentions[0].ReasonCode != reason || preview.Implicit != nil {
		t.Fatalf("trigger preview=%+v", preview)
	}

	fake.mentionErr = store.ErrInvalidArgument
	if _, err := service.PreviewIssueCommentTriggers(
		t.Context(), projectID, issueID, nil, "body", store.IssueCommentTriggerRequest{},
	); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("trigger preview error=%v want invalid argument", err)
	}
	fake.mentionErr = nil

	if _, err := service.PreviewIssueCommentTriggers(
		t.Context(), projectID, "missing", nil, "body", store.IssueCommentTriggerRequest{},
	); err == nil {
		t.Fatal("missing Issue trigger preview unexpectedly succeeded")
	}

	withoutTriggers := &issueCommentWithoutMentionsStore{
		ControlPlaneStore: fake,
		IssueCommentStore: fake,
	}
	withoutTriggerService := New(withoutTriggers)
	if _, err := withoutTriggerService.PreviewIssueCommentTriggers(
		t.Context(), projectID, issueID, nil, "body", store.IssueCommentTriggerRequest{},
	); err == nil {
		t.Fatal("trigger preview unexpectedly succeeded without trigger capability")
	}
	if _, err := withoutTriggerService.CreateHumanIssueComment(t.Context(), CreateIssueCommentInput{
		ProjectID: projectID, IssueID: issueID, Body: "human trigger requires capability", RequestKey: "trigger-capability",
	}, "author"); err == nil {
		t.Fatal("human comment unexpectedly succeeded without trigger capability")
	}

	fake.commentErr = store.ErrConflict
	if _, err := service.CreateHumanIssueComment(t.Context(), CreateIssueCommentInput{
		ProjectID: projectID, IssueID: issueID, Body: "propagate trigger store failure", RequestKey: "trigger-store-error",
	}, "author"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("trigger create error=%v want conflict", err)
	}
}

func TestPublishAgentIssueCommentDerivesTrustedRunContext(t *testing.T) {
	const projectID = "project-1"
	const issueID = "issue-1"
	const runID = "run-1"
	agentID := "agent-1"
	base := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, IssuePrefix: "AB"},
		issues: map[string]store.Issue{
			issueID: {ID: issueID, ProjectID: projectID, Number: 1, Title: "Issue", Status: "IN_PROGRESS"},
		},
		runs: map[string]store.Run{
			runID: {ID: runID, ProjectID: projectID, IssueID: issueID, AgentID: &agentID, Status: "RUNNING"},
		},
	}
	fake := &issueCommentTestStore{projectWorkflowAuthorizationStore: base}
	service := New(fake)
	publisher := &assigneePublisher{}
	service.SetEventRecorder(publisher)

	if _, err := service.PublishAgentIssueComment(
		t.Context(), projectID, runID, "tool-call-multi", "Please split this work.", []string{"agent-2", "agent-3"},
	); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("multiple Agent mention error=%v want invalid argument", err)
	}
	if fake.createCalls != 0 {
		t.Fatalf("multiple Agent mention reached durable comment creation %d times", fake.createCalls)
	}

	created, err := service.PublishAgentIssueComment(t.Context(), projectID, runID, "tool-call-1", "Concise finding @name remains plain text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if created.AuthorType != store.ActorTypeAgent || created.AuthorID != agentID || created.SourceRunID == nil || *created.SourceRunID != runID {
		t.Fatalf("created provenance=%+v", created)
	}
	if fake.lastCreate.IssueID != issueID || fake.lastCreate.AuthorType != store.ActorTypeAgent ||
		fake.lastCreate.AuthorID != agentID || fake.lastCreate.SourceRunID == nil || *fake.lastCreate.SourceRunID != runID {
		t.Fatalf("store input=%+v", fake.lastCreate)
	}
	if len(base.runs) != 1 || base.issues[issueID].Status != "IN_PROGRESS" {
		t.Fatalf("publishing changed execution/workflow state runs=%+v issue=%+v", base.runs, base.issues[issueID])
	}
	if len(publisher.published) != 1 || publisher.published[0].Type != "issue.comment_created" {
		t.Fatalf("published=%+v", publisher.published)
	}
	targetCreated, err := service.PublishAgentIssueCommentTargets(t.Context(), projectID, runID, "tool-call-target", "Targeted finding", []store.IssueCommentTarget{{Type: store.IssueCommentTargetTypeSquad, ID: "squad-2"}})
	if err != nil {
		t.Fatal(err)
	}
	if targetCreated.AuthorID != agentID || len(targetCreated.Mentions) != 1 || targetCreated.Mentions[0].Target.Type != store.IssueCommentTargetTypeSquad ||
		fake.targetCreateCalls != 1 || len(fake.lastTargets) != 1 {
		t.Fatalf("typed Agent comment=%+v calls=%d targets=%+v", targetCreated, fake.targetCreateCalls, fake.lastTargets)
	}
	if _, err := service.PublishAgentIssueCommentTargets(t.Context(), projectID, runID, "tool-call-too-many", "body", []store.IssueCommentTarget{{Type: store.IssueCommentTargetTypeAgent, ID: "agent-2"}, {Type: store.IssueCommentTargetTypeSquad, ID: "squad-2"}}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("multiple typed targets error=%v want invalid argument", err)
	}
	if _, err := service.PublishAgentIssueCommentTargets(t.Context(), projectID, "missing", "tool-call-missing-target", "body", nil); err == nil {
		t.Fatal("missing Run unexpectedly published a typed comment")
	}

	if _, err := service.PublishAgentIssueComment(t.Context(), projectID, "missing", "tool-call-2", "body", nil); err == nil {
		t.Fatal("missing Run unexpectedly published a comment")
	}
	runWithoutAgent := "run-no-agent"
	base.runs[runWithoutAgent] = store.Run{ID: runWithoutAgent, ProjectID: projectID, IssueID: issueID, Status: "RUNNING"}
	if _, err := service.PublishAgentIssueComment(t.Context(), projectID, runWithoutAgent, "tool-call-3", "body", nil); err == nil {
		t.Fatal("Run without Agent unexpectedly published a comment")
	}
	if _, err := service.PublishAgentIssueComment(t.Context(), projectID, runID, "tool-call-4", "   ", nil); err == nil {
		t.Fatal("blank Agent comment unexpectedly succeeded")
	}
}

func TestIssueCommentApplicationValidationAndUnavailableCapabilities(t *testing.T) {
	const projectID = "project-1"
	const issueID = "issue-1"
	base := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, IssuePrefix: "AB"},
		issues: map[string]store.Issue{
			issueID: {ID: issueID, ProjectID: projectID, Number: 1, Title: "Issue", Status: "TODO"},
		},
		runs: map[string]store.Run{},
	}
	service := New(base)

	if _, err := service.ListIssueComments(t.Context(), projectID, issueID); err == nil {
		t.Fatal("ListIssueComments succeeded without comment capability")
	}
	if _, err := service.CreateHumanIssueComment(t.Context(), CreateIssueCommentInput{
		ProjectID: projectID, IssueID: issueID, Body: "hello",
	}, "user"); err == nil {
		t.Fatal("CreateHumanIssueComment succeeded without comment capability")
	}

	fake := &issueCommentTestStore{projectWorkflowAuthorizationStore: base}
	service = New(fake)
	blankParent := " "
	cases := map[string]struct {
		input    CreateIssueCommentInput
		authorID string
	}{
		"blank body":   {input: CreateIssueCommentInput{ProjectID: projectID, IssueID: issueID, Body: "   "}, authorID: "user"},
		"blank author": {input: CreateIssueCommentInput{ProjectID: projectID, IssueID: issueID, Body: "hello"}},
		"blank parent": {input: CreateIssueCommentInput{ProjectID: projectID, IssueID: issueID, ParentCommentID: &blankParent, Body: "hello"}, authorID: "user"},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := service.CreateHumanIssueComment(t.Context(), testCase.input, testCase.authorID); err == nil {
				t.Fatalf("CreateHumanIssueComment(%s) unexpectedly succeeded", name)
			}
		})
	}
}
func TestIssueCommentApplicationPropagatesReadWriteAndTimelineFailures(t *testing.T) {
	const projectID = "project-1"
	const issueID = "issue-1"
	base := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, IssuePrefix: "AB"},
		issues: map[string]store.Issue{
			issueID: {ID: issueID, ProjectID: projectID, Number: 1, Title: "Issue", Status: "TODO"},
		},
		runs: map[string]store.Run{},
	}
	fake := &issueCommentTestStore{projectWorkflowAuthorizationStore: base}
	service := New(fake)

	if _, err := service.ListIssueComments(t.Context(), projectID, "missing"); err == nil {
		t.Fatal("missing Issue comment read unexpectedly succeeded")
	}
	if _, err := service.CreateHumanIssueComment(t.Context(), CreateIssueCommentInput{
		ProjectID: projectID, IssueID: "missing", Body: "hello",
	}, "user"); err == nil {
		t.Fatal("missing Issue comment creation unexpectedly succeeded")
	}

	fake.commentErr = store.ErrConflict
	if _, err := service.ListIssueComments(t.Context(), projectID, issueID); err == nil {
		t.Fatal("comment store read failure was swallowed")
	}
	if _, err := service.CreateHumanIssueComment(t.Context(), CreateIssueCommentInput{
		ProjectID: projectID, IssueID: issueID, Body: "hello",
	}, "user"); err == nil {
		t.Fatal("comment store write failure was swallowed")
	}
	if _, err := service.ListIssueTimeline(t.Context(), projectID, issueID); err == nil {
		t.Fatal("timeline swallowed comment read failure")
	}
	fake.commentErr = nil

	service.issueActivity = nil
	if _, err := service.ListIssueTimeline(t.Context(), projectID, issueID); err == nil {
		t.Fatal("timeline unexpectedly succeeded without activity capability")
	}
	service.issueActivity = fake

	fake.activityErr = store.ErrConflict
	if _, err := service.ListIssueTimeline(t.Context(), projectID, issueID); err == nil {
		t.Fatal("timeline activity failure was swallowed")
	}
}

func TestIssueTimelineTieBreaksByKindThenID(t *testing.T) {
	const projectID = "project-1"
	const issueID = "issue-1"
	base := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, IssuePrefix: "AB"},
		issues: map[string]store.Issue{
			issueID: {ID: issueID, ProjectID: projectID, Number: 1, Title: "Issue", Status: "TODO"},
		},
		runs: map[string]store.Run{},
	}
	at := time.Date(2026, 9, 19, 2, 0, 0, 0, time.UTC)
	fake := &issueCommentTestStore{
		projectWorkflowAuthorizationStore: base,
		comments: []store.IssueComment{
			{ID: "comment-b", IssueID: issueID, AuthorType: store.ActorTypeHuman, AuthorID: "user", Body: "B", CreatedAt: at, UpdatedAt: at},
			{ID: "comment-a", IssueID: issueID, AuthorType: store.ActorTypeHuman, AuthorID: "user", Body: "A", CreatedAt: at, UpdatedAt: at},
		},
		events: []store.Event{
			{ID: "event-b", Type: "issue.updated", ProjectID: projectID, IssueID: stringPointer(issueID), OccurredAt: at},
			{ID: "event-a", Type: "run.completed", ProjectID: projectID, IssueID: stringPointer(issueID), OccurredAt: at},
		},
	}
	entries, err := New(fake).ListIssueTimeline(t.Context(), projectID, issueID)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.ID)
	}
	want := []string{"event-a", "event-b", "comment-a", "comment-b"}
	if len(got) != len(want) {
		t.Fatalf("timeline=%v want=%v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("timeline=%v want=%v", got, want)
		}
	}
}

func TestIssueTimelineMergesCommentsWithRelevantDurableActivity(t *testing.T) {
	const projectID = "project-1"
	const issueID = "issue-1"
	base := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, IssuePrefix: "AB"},
		roles:   map[string]string{"viewer": store.ProjectRoleViewer},
		issues: map[string]store.Issue{
			issueID: {ID: issueID, ProjectID: projectID, Number: 1, Title: "Issue", Status: "TODO"},
		},
		runs: map[string]store.Run{},
	}
	at := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	comment := store.IssueComment{ID: "comment-1", IssueID: issueID, AuthorType: store.ActorTypeHuman, AuthorID: "viewer", AuthorName: "Viewer", Body: "Hello", CreatedAt: at.Add(time.Second), UpdatedAt: at.Add(time.Second)}
	fake := &issueCommentTestStore{
		projectWorkflowAuthorizationStore: base,
		comments:                          []store.IssueComment{comment},
		events: []store.Event{
			{ID: "issue-created", Type: "issue.created", ProjectID: projectID, IssueID: stringPointer(issueID), OccurredAt: at},
			{ID: "run", Type: "run.completed", ProjectID: projectID, IssueID: stringPointer(issueID), OccurredAt: at.Add(3 * time.Second)},
		},
	}
	service := New(fake)
	entries, err := service.ListIssueTimeline(t.Context(), projectID, issueID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries=%+v", entries)
	}
	if entries[0].Kind != store.IssueTimelineKindActivity || entries[0].ID != "issue-created" || entries[1].Kind != store.IssueTimelineKindComment || entries[1].ID != comment.ID || entries[2].ID != "run" {
		t.Fatalf("timeline order=%+v", entries)
	}
}

func TestProjectAccessIssueCommentLifecycleAuthorizationAndExecutionNeutrality(t *testing.T) {
	const projectID = "project-1"
	const issueID = "issue-1"
	at := time.Date(2026, 9, 19, 6, 0, 0, 0, time.UTC)
	base := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, Name: "Project", IssuePrefix: "AB"},
		roles: map[string]string{
			"author":  store.ProjectRoleMember,
			"member":  store.ProjectRoleMember,
			"viewer":  store.ProjectRoleViewer,
			"outside": store.ProjectRoleViewer,
		},
		issues: map[string]store.Issue{
			issueID: {ID: issueID, ProjectID: projectID, Number: 1, Title: "Issue", Status: "TODO"},
		},
		runs: map[string]store.Run{},
	}
	root := store.IssueComment{
		ID: "root", IssueID: issueID, AuthorType: store.ActorTypeHuman, AuthorID: "author",
		AuthorName: "Author", Body: "original", CreatedAt: at, UpdatedAt: at,
	}
	parentID := root.ID
	reply := store.IssueComment{
		ID: "reply", IssueID: issueID, ParentCommentID: &parentID, AuthorType: store.ActorTypeHuman,
		AuthorID: "member", AuthorName: "Member", Body: "reply", CreatedAt: at.Add(time.Second), UpdatedAt: at.Add(time.Second),
	}
	agentRunID := "agent-run"
	agentComment := store.IssueComment{
		ID: "agent-comment", IssueID: issueID, AuthorType: store.ActorTypeAgent, AuthorID: "agent-1",
		AuthorName: "Agent", SourceRunID: &agentRunID, Body: "Agent-authored", CreatedAt: at.Add(2 * time.Second), UpdatedAt: at.Add(2 * time.Second),
	}
	fake := &issueCommentTestStore{projectWorkflowAuthorizationStore: base, comments: []store.IssueComment{root, reply, agentComment}}
	service := New(fake)
	publisher := &assigneePublisher{}
	service.SetEventRecorder(publisher)
	access, err := NewProjectAccessService(service, fake)
	if err != nil {
		t.Fatal(err)
	}

	author := activeProjectActor("author", store.DeploymentRoleMember)
	member := activeProjectActor("member", store.DeploymentRoleMember)
	viewer := activeProjectActor("viewer", store.DeploymentRoleMember)

	if _, err := access.UpdateIssueComment(t.Context(), member, projectID, issueID, root.ID, "forbidden edit"); err == nil {
		t.Fatal("non-author edit unexpectedly succeeded")
	}
	if err := access.DeleteIssueComment(t.Context(), member, projectID, issueID, root.ID); err == nil {
		t.Fatal("non-author delete unexpectedly succeeded")
	}
	if _, err := access.UpdateIssueComment(t.Context(), author, projectID, issueID, agentComment.ID, "forged human edit"); err == nil {
		t.Fatal("human edit of Agent-authored comment unexpectedly succeeded")
	}
	if err := access.DeleteIssueComment(t.Context(), author, projectID, issueID, agentComment.ID); err == nil {
		t.Fatal("human delete of Agent-authored comment unexpectedly succeeded")
	}
	unchangedAgentComment, err := service.GetIssueComment(t.Context(), projectID, issueID, agentComment.ID)
	if err != nil || unchangedAgentComment.Body != agentComment.Body || unchangedAgentComment.DeletedAt != nil {
		t.Fatalf("Agent-authored comment changed after human mutation attempts=%+v err=%v", unchangedAgentComment, err)
	}
	updated, err := access.UpdateIssueComment(t.Context(), author, projectID, issueID, root.ID, "edited")
	if err != nil || updated.Body != "edited" || !updated.UpdatedAt.After(at) {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}

	if _, err := access.ResolveIssueComment(t.Context(), viewer, projectID, issueID, root.ID); err == nil {
		t.Fatal("viewer resolve unexpectedly succeeded")
	}
	if _, err := access.UpdateIssueComment(t.Context(), viewer, projectID, issueID, root.ID, "viewer edit"); err == nil {
		t.Fatal("viewer edit unexpectedly succeeded")
	}
	if err := access.DeleteIssueComment(t.Context(), viewer, projectID, issueID, root.ID); err == nil {
		t.Fatal("viewer delete unexpectedly succeeded")
	}
	if _, err := access.ReopenIssueComment(t.Context(), viewer, projectID, issueID, root.ID); err == nil {
		t.Fatal("viewer reopen unexpectedly succeeded")
	}
	if err := access.AddIssueCommentReaction(t.Context(), viewer, projectID, issueID, root.ID, store.IssueCommentReactionHeart); err == nil {
		t.Fatal("viewer reaction add unexpectedly succeeded")
	}
	if err := access.RemoveIssueCommentReaction(t.Context(), viewer, projectID, issueID, root.ID, store.IssueCommentReactionHeart); err == nil {
		t.Fatal("viewer reaction remove unexpectedly succeeded")
	}
	resolved, err := access.ResolveIssueComment(t.Context(), member, projectID, issueID, root.ID)
	if err != nil || resolved.ResolvedAt == nil || resolved.ResolvedByUserID == nil || *resolved.ResolvedByUserID != member.ID {
		t.Fatalf("resolved=%+v err=%v", resolved, err)
	}
	if _, err := access.ResolveIssueComment(t.Context(), member, projectID, issueID, reply.ID); err == nil {
		t.Fatal("reply resolve unexpectedly succeeded")
	}
	reopened, err := access.ReopenIssueComment(t.Context(), author, projectID, issueID, root.ID)
	if err != nil || reopened.ResolvedAt != nil {
		t.Fatalf("reopened=%+v err=%v", reopened, err)
	}

	if err := access.AddIssueCommentReaction(t.Context(), member, projectID, issueID, root.ID, store.IssueCommentReactionHeart); err != nil {
		t.Fatal(err)
	}
	if err := access.AddIssueCommentReaction(t.Context(), member, projectID, issueID, root.ID, store.IssueCommentReactionHeart); err != nil {
		t.Fatal(err)
	}
	reacted, err := service.GetIssueComment(t.Context(), projectID, issueID, root.ID)
	if err != nil || len(reacted.Reactions) != 1 || reacted.Reactions[0].Count != 1 || reacted.Reactions[0].ActorIDs[0] != member.ID {
		t.Fatalf("reacted=%+v err=%v", reacted, err)
	}
	if err := access.RemoveIssueCommentReaction(t.Context(), member, projectID, issueID, root.ID, store.IssueCommentReactionHeart); err != nil {
		t.Fatal(err)
	}
	if err := access.RemoveIssueCommentReaction(t.Context(), member, projectID, issueID, root.ID, store.IssueCommentReactionHeart); err != nil {
		t.Fatal(err)
	}
	if err := access.AddIssueCommentReaction(t.Context(), member, projectID, issueID, root.ID, "PARTY"); err == nil {
		t.Fatal("unsupported reaction unexpectedly succeeded")
	}

	beforeIssue := base.issues[issueID]
	if err := access.DeleteIssueComment(t.Context(), author, projectID, issueID, root.ID); err != nil {
		t.Fatal(err)
	}
	tombstone, err := service.GetIssueComment(t.Context(), projectID, issueID, root.ID)
	if err != nil || tombstone.DeletedAt == nil || tombstone.Body != "" {
		t.Fatalf("tombstone=%+v err=%v", tombstone, err)
	}
	eventsAfterDelete := len(publisher.published)
	deletedAt := *tombstone.DeletedAt
	updatedAt := tombstone.UpdatedAt
	if err := access.DeleteIssueComment(t.Context(), author, projectID, issueID, root.ID); err != nil {
		t.Fatalf("repeated author delete: %v", err)
	}
	if len(publisher.published) != eventsAfterDelete {
		t.Fatalf("repeated author delete published events=%+v", publisher.published[eventsAfterDelete:])
	}
	if err := access.DeleteIssueComment(t.Context(), member, projectID, issueID, root.ID); err == nil {
		t.Fatal("non-author tombstone delete unexpectedly succeeded")
	}
	if len(publisher.published) != eventsAfterDelete {
		t.Fatalf("non-author tombstone delete published events=%+v", publisher.published[eventsAfterDelete:])
	}
	unchangedTombstone, err := service.GetIssueComment(t.Context(), projectID, issueID, root.ID)
	if err != nil || unchangedTombstone.DeletedAt == nil || !unchangedTombstone.DeletedAt.Equal(deletedAt) || !unchangedTombstone.UpdatedAt.Equal(updatedAt) || unchangedTombstone.Body != "" {
		t.Fatalf("tombstone changed after repeated deletes=%+v err=%v", unchangedTombstone, err)
	}
	if err := access.AddIssueCommentReaction(t.Context(), member, projectID, issueID, root.ID, store.IssueCommentReactionEyes); err == nil {
		t.Fatal("reaction on deleted comment unexpectedly succeeded")
	}
	afterIssue := base.issues[issueID]
	if beforeIssue.Status != afterIssue.Status || beforeIssue.AssigneeType != afterIssue.AssigneeType || beforeIssue.AssigneeID != afterIssue.AssigneeID || len(base.runs) != 0 {
		t.Fatalf("comment lifecycle changed workflow before=%+v after=%+v runs=%+v", beforeIssue, afterIssue, base.runs)
	}
	if len(publisher.published) != 6 {
		t.Fatalf("published lifecycle events=%+v", publisher.published)
	}
	for _, event := range publisher.published {
		if event.Type != "issue.comment_changed" {
			t.Fatalf("unexpected lifecycle event %+v", event)
		}
	}
}

func TestIssueCommentLifecycleRejectsMissingOrDeletedTargets(t *testing.T) {
	const projectID = "project-1"
	const issueID = "issue-1"
	base := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, IssuePrefix: "AB"},
		issues:  map[string]store.Issue{issueID: {ID: issueID, ProjectID: projectID, Number: 1, Title: "Issue", Status: "TODO"}},
		runs:    map[string]store.Run{},
	}
	at := time.Now()
	deletedAt := at.Add(time.Second)
	fake := &issueCommentTestStore{
		projectWorkflowAuthorizationStore: base,
		comments: []store.IssueComment{
			{ID: "deleted", IssueID: issueID, AuthorType: store.ActorTypeHuman, AuthorID: "author", DeletedAt: &deletedAt, CreatedAt: at, UpdatedAt: at},
		},
	}
	service := New(fake)

	if _, err := service.UpdateIssueComment(t.Context(), projectID, issueID, "missing", "author", "body"); err == nil {
		t.Fatal("missing edit unexpectedly succeeded")
	}
	if _, err := service.UpdateIssueComment(t.Context(), projectID, issueID, "deleted", "author", "body"); err == nil {
		t.Fatal("deleted edit unexpectedly succeeded")
	}
	if _, err := service.ResolveIssueComment(t.Context(), projectID, issueID, "deleted", "author"); err == nil {
		t.Fatal("deleted resolve unexpectedly succeeded")
	}
	if err := service.AddIssueCommentReaction(t.Context(), projectID, issueID, "deleted", "author", store.IssueCommentReactionHeart); err == nil {
		t.Fatal("deleted reaction unexpectedly succeeded")
	}
}

func TestIssueCommentLifecycleValidationAndDeletedNoOpBranches(t *testing.T) {
	const projectID = "project-1"
	const issueID = "issue-1"
	at := time.Date(2026, 9, 19, 7, 0, 0, 0, time.UTC)
	deletedAt := at.Add(time.Second)
	base := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, IssuePrefix: "AB"},
		issues: map[string]store.Issue{
			issueID: {ID: issueID, ProjectID: projectID, Number: 1, Title: "Issue", Status: "TODO"},
		},
		runs: map[string]store.Run{},
	}

	withoutComments := New(base)
	if _, err := withoutComments.GetIssueComment(t.Context(), projectID, issueID, "root"); err == nil {
		t.Fatal("GetIssueComment unexpectedly succeeded without comment capability")
	}

	fake := &issueCommentTestStore{
		projectWorkflowAuthorizationStore: base,
		comments: []store.IssueComment{
			{ID: "root", IssueID: issueID, AuthorType: store.ActorTypeHuman, AuthorID: "author", Body: "root", CreatedAt: at, UpdatedAt: at},
			{ID: "deleted", IssueID: issueID, AuthorType: store.ActorTypeHuman, AuthorID: "author", DeletedAt: &deletedAt, CreatedAt: at, UpdatedAt: at},
		},
	}
	service := New(fake)

	if _, err := service.GetIssueComment(t.Context(), projectID, "missing", "root"); err == nil {
		t.Fatal("comment lookup on missing Issue unexpectedly succeeded")
	}
	if _, err := service.UpdateIssueComment(t.Context(), projectID, issueID, "root", "author", "   "); err == nil {
		t.Fatal("blank edit unexpectedly succeeded")
	}
	if _, err := service.UpdateIssueComment(t.Context(), projectID, issueID, "root", "", "edited"); err == nil {
		t.Fatal("edit without actor unexpectedly succeeded")
	}
	if err := service.DeleteIssueComment(t.Context(), projectID, issueID, "root", ""); err == nil {
		t.Fatal("delete without actor unexpectedly succeeded")
	}
	if err := service.DeleteIssueComment(t.Context(), projectID, issueID, "deleted", "author"); err != nil {
		t.Fatalf("repeated tombstone delete should be idempotent: %v", err)
	}
	if _, err := service.ResolveIssueComment(t.Context(), projectID, issueID, "root", ""); err == nil {
		t.Fatal("resolve without actor unexpectedly succeeded")
	}
	if _, err := service.ReopenIssueComment(t.Context(), projectID, issueID, "root", ""); err == nil {
		t.Fatal("reopen without actor unexpectedly succeeded")
	}
	if _, err := service.ReopenIssueComment(t.Context(), projectID, issueID, "deleted", "author"); err == nil {
		t.Fatal("reopen of deleted discussion unexpectedly succeeded")
	}
	if err := service.AddIssueCommentReaction(t.Context(), projectID, issueID, "root", "", store.IssueCommentReactionHeart); err == nil {
		t.Fatal("reaction without actor unexpectedly succeeded")
	}
	if err := service.RemoveIssueCommentReaction(t.Context(), projectID, issueID, "root", "author", "PARTY"); err == nil {
		t.Fatal("unsupported reaction removal unexpectedly succeeded")
	}
	if err := service.RemoveIssueCommentReaction(t.Context(), projectID, issueID, "deleted", "author", store.IssueCommentReactionHeart); err == nil {
		t.Fatal("reaction removal from deleted comment unexpectedly succeeded")
	}
	if err := service.RemoveIssueCommentReaction(t.Context(), projectID, issueID, "missing", "author", store.IssueCommentReactionHeart); err == nil {
		t.Fatal("reaction removal from missing comment unexpectedly succeeded")
	}
}

func TestIssueCommentLifecycleValidationAndMutationFailures(t *testing.T) {
	const projectID = "project-1"
	const issueID = "issue-1"
	at := time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC)
	base := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, IssuePrefix: "AB"},
		issues: map[string]store.Issue{
			issueID: {ID: issueID, ProjectID: projectID, Number: 1, Title: "Issue", Status: "TODO"},
		},
		runs: map[string]store.Run{},
	}
	root := store.IssueComment{
		ID: "root", IssueID: issueID, AuthorType: store.ActorTypeHuman, AuthorID: "author",
		Body: "body", CreatedAt: at, UpdatedAt: at,
	}
	fake := &issueCommentTestStore{projectWorkflowAuthorizationStore: base, comments: []store.IssueComment{root}}
	service := New(fake)

	if _, err := service.UpdateIssueComment(t.Context(), projectID, issueID, root.ID, "author", "   "); err == nil {
		t.Fatal("blank edit unexpectedly succeeded")
	}
	if _, err := service.UpdateIssueComment(t.Context(), projectID, issueID, root.ID, "", "updated"); err == nil {
		t.Fatal("blank edit actor unexpectedly succeeded")
	}
	if err := service.DeleteIssueComment(t.Context(), projectID, issueID, root.ID, ""); err == nil {
		t.Fatal("blank delete actor unexpectedly succeeded")
	}
	if _, err := service.ResolveIssueComment(t.Context(), projectID, issueID, root.ID, ""); err == nil {
		t.Fatal("blank resolver unexpectedly succeeded")
	}
	if err := service.AddIssueCommentReaction(t.Context(), projectID, issueID, root.ID, "", store.IssueCommentReactionHeart); err == nil {
		t.Fatal("blank reaction actor unexpectedly succeeded")
	}
	if err := service.RemoveIssueCommentReaction(t.Context(), projectID, issueID, root.ID, "author", "PARTY"); err == nil {
		t.Fatal("unsupported reaction removal unexpectedly succeeded")
	}

	fake.mutationErr = store.ErrConflict
	if _, err := service.UpdateIssueComment(t.Context(), projectID, issueID, root.ID, "author", "updated"); err == nil {
		t.Fatal("edit mutation failure was swallowed")
	}
	if err := service.DeleteIssueComment(t.Context(), projectID, issueID, root.ID, "author"); err == nil {
		t.Fatal("delete mutation failure was swallowed")
	}
	if _, err := service.ResolveIssueComment(t.Context(), projectID, issueID, root.ID, "author"); err == nil {
		t.Fatal("resolve mutation failure was swallowed")
	}
	if _, err := service.ReopenIssueComment(t.Context(), projectID, issueID, root.ID, "author"); err == nil {
		t.Fatal("reopen mutation failure was swallowed")
	}
	if err := service.AddIssueCommentReaction(t.Context(), projectID, issueID, root.ID, "author", store.IssueCommentReactionHeart); err == nil {
		t.Fatal("reaction add failure was swallowed")
	}
	if err := service.RemoveIssueCommentReaction(t.Context(), projectID, issueID, root.ID, "author", store.IssueCommentReactionHeart); err == nil {
		t.Fatal("reaction remove failure was swallowed")
	}
}
