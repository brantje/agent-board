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
	createCalls int
	lastCreate  store.IssueComment
}

func (s *issueCommentTestStore) ListIssueComments(_ context.Context, projectID, issueID string) ([]store.IssueComment, error) {
	if projectID != s.project.ID {
		return nil, store.ErrNotFound
	}
	if _, err := s.GetIssue(context.Background(), projectID, issueID); err != nil {
		return nil, err
	}
	return append([]store.IssueComment(nil), s.comments...), nil
}

func (s *issueCommentTestStore) CreateIssueComment(_ context.Context, projectID string, input store.IssueComment) (store.IssueCommentMutationResult, error) {
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

func (s *issueCommentTestStore) ListIssueEvents(_ context.Context, projectID, issueID string) ([]store.Event, error) {
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
			{ID: "comment-notify", Type: "issue.comment_created", ProjectID: projectID, IssueID: stringPointer(issueID), OccurredAt: at.Add(time.Second)},
			{ID: "tool", Type: "tool.completed", ProjectID: projectID, IssueID: stringPointer(issueID), OccurredAt: at.Add(2 * time.Second)},
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
	for _, entry := range entries {
		if entry.ID == "comment-notify" || entry.ID == "tool" {
			t.Fatalf("unexpected timeline entry %+v", entry)
		}
	}
}
