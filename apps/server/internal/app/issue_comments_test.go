package app

import (
	"context"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type issueCommentTestStore struct {
	*projectWorkflowAuthorizationStore
	comments    []store.IssueComment
	events      []store.Event
	commentErr  error
	activityErr error
	createCalls int
	lastCreate  store.IssueComment
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
		AuthorType:      store.ActorTypeAgent,
		AuthorID:        "forged-agent",
		Body:            "Please inspect @name literally",
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
	if _, err := service.CreateIssueComment(t.Context(), CreateIssueCommentInput{
		ProjectID: projectID, IssueID: issueID, AuthorType: store.ActorTypeHuman, AuthorID: "user", Body: "hello",
	}); err == nil {
		t.Fatal("CreateIssueComment succeeded without comment capability")
	}

	fake := &issueCommentTestStore{projectWorkflowAuthorizationStore: base}
	service = New(fake)
	blankParent := " "
	cases := map[string]CreateIssueCommentInput{
		"blank body":   {ProjectID: projectID, IssueID: issueID, AuthorType: store.ActorTypeHuman, AuthorID: "user", Body: "   "},
		"blank author": {ProjectID: projectID, IssueID: issueID, AuthorType: store.ActorTypeHuman, Body: "hello"},
		"invalid actor": {ProjectID: projectID, IssueID: issueID, AuthorType: "OTHER", AuthorID: "user", Body: "hello"},
		"blank parent": {ProjectID: projectID, IssueID: issueID, ParentCommentID: &blankParent, AuthorType: store.ActorTypeHuman, AuthorID: "user", Body: "hello"},
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := service.CreateIssueComment(t.Context(), input); err == nil {
				t.Fatalf("CreateIssueComment(%s) unexpectedly succeeded", name)
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
	if _, err := service.CreateIssueComment(t.Context(), CreateIssueCommentInput{
		ProjectID: projectID, IssueID: "missing", AuthorType: store.ActorTypeHuman, AuthorID: "user", Body: "hello",
	}); err == nil {
		t.Fatal("missing Issue comment creation unexpectedly succeeded")
	}

	fake.commentErr = store.ErrConflict
	if _, err := service.ListIssueComments(t.Context(), projectID, issueID); err == nil {
		t.Fatal("comment store read failure was swallowed")
	}
	if _, err := service.CreateIssueComment(t.Context(), CreateIssueCommentInput{
		ProjectID: projectID, IssueID: issueID, AuthorType: store.ActorTypeHuman, AuthorID: "user", Body: "hello",
	}); err == nil {
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
	fake := &issueCommentTestStore{projectWorkflowAuthorizationStore: base, comments: []store.IssueComment{root, reply}}
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
	updated, err := access.UpdateIssueComment(t.Context(), author, projectID, issueID, root.ID, "edited")
	if err != nil || updated.Body != "edited" || !updated.UpdatedAt.After(at) {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}

	if _, err := access.ResolveIssueComment(t.Context(), viewer, projectID, issueID, root.ID); err == nil {
		t.Fatal("viewer resolve unexpectedly succeeded")
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
		issues: map[string]store.Issue{issueID: {ID: issueID, ProjectID: projectID, Number: 1, Title: "Issue", Status: "TODO"}},
		runs: map[string]store.Run{},
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
