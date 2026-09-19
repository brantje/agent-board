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

func (s *issueCommentTestStore) UpdateIssueComment(context.Context, string, string, string, string, string) (store.IssueCommentMutationResult, error) {
	return store.IssueCommentMutationResult{}, store.ErrInvalidArgument
}
func (s *issueCommentTestStore) DeleteIssueComment(context.Context, string, string, string, string) (store.IssueCommentDeleteResult, error) {
	return store.IssueCommentDeleteResult{}, store.ErrInvalidArgument
}
func (s *issueCommentTestStore) ResolveIssueComment(context.Context, string, string, string, string) (store.IssueCommentMutationResult, error) {
	return store.IssueCommentMutationResult{}, store.ErrInvalidArgument
}
func (s *issueCommentTestStore) ReopenIssueComment(context.Context, string, string, string, string) (store.IssueCommentMutationResult, error) {
	return store.IssueCommentMutationResult{}, store.ErrInvalidArgument
}
func (s *issueCommentTestStore) AddIssueCommentReaction(context.Context, string, string, string, string, string) ([]store.Event, error) {
	return nil, store.ErrInvalidArgument
}
func (s *issueCommentTestStore) RemoveIssueCommentReaction(context.Context, string, string, string, string, string) ([]store.Event, error) {
	return nil, store.ErrInvalidArgument
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
