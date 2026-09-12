package app

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type projectWorkflowAuthorizationStore struct {
	store.ControlPlaneStore
	store.ProjectAccessStore
	project          store.Project
	roles            map[string]string
	issues           map[string]store.Issue
	createIssueCalls int
	assignIssueCalls int
}

func (s *projectWorkflowAuthorizationStore) GetProject(_ context.Context, projectID string) (store.Project, error) {
	if projectID != s.project.ID {
		return store.Project{}, store.ErrNotFound
	}
	return s.project, nil
}

func (s *projectWorkflowAuthorizationStore) EffectiveProjectRole(_ context.Context, projectID, userID string) (string, error) {
	if projectID != s.project.ID {
		return "", store.ErrNotFound
	}
	role, ok := s.roles[userID]
	if !ok {
		return "", store.ErrNotFound
	}
	return role, nil
}

func (s *projectWorkflowAuthorizationStore) CreateIssue(_ context.Context, input store.Issue) (store.Issue, error) {
	s.createIssueCalls++
	input.ID = "issue-created"
	input.Number = 1
	s.issues[input.ID] = input
	return input, nil
}

func (s *projectWorkflowAuthorizationStore) GetIssue(_ context.Context, projectID, issueID string) (store.Issue, error) {
	issue, ok := s.issues[issueID]
	if !ok || issue.ProjectID != projectID {
		return store.Issue{}, store.ErrNotFound
	}
	return issue, nil
}

func (s *projectWorkflowAuthorizationStore) GetAgentInScope(_ context.Context, scope *string, agentID string) (store.Agent, error) {
	if scope == nil || *scope != s.project.ID || agentID != "agent-1" {
		return store.Agent{}, store.ErrNotFound
	}
	projectID := s.project.ID
	return store.Agent{ID: agentID, ProjectID: &projectID, State: "ENABLED", ModelProfileID: "model-1"}, nil
}

func (s *projectWorkflowAuthorizationStore) GetModelProfile(_ context.Context, scope *string, modelID string) (store.ModelProfile, error) {
	if scope == nil || *scope != s.project.ID || modelID != "model-1" {
		return store.ModelProfile{}, store.ErrNotFound
	}
	projectID := s.project.ID
	return store.ModelProfile{ID: modelID, ProjectID: &projectID, ProviderID: "provider-1", Enabled: true}, nil
}

func (s *projectWorkflowAuthorizationStore) GetProvider(_ context.Context, scope *string, providerID string) (store.Provider, error) {
	if scope == nil || *scope != s.project.ID || providerID != "provider-1" {
		return store.Provider{}, store.ErrNotFound
	}
	projectID := s.project.ID
	return store.Provider{ID: providerID, ProjectID: &projectID, Enabled: true}, nil
}

func (s *projectWorkflowAuthorizationStore) AssignIssue(_ context.Context, projectID, issueID, agentID string) (store.Issue, store.Run, error) {
	s.assignIssueCalls++
	issue, err := s.GetIssue(context.Background(), projectID, issueID)
	if err != nil {
		return store.Issue{}, store.Run{}, err
	}
	issue.AssignedAgentID = &agentID
	s.issues[issueID] = issue
	return issue, store.Run{ID: "run-1", ProjectID: projectID, IssueID: issueID, Status: "QUEUED"}, nil
}

func newProjectWorkflowAccess(t *testing.T, fake *projectWorkflowAuthorizationStore) *ProjectAccessService {
	t.Helper()
	service, err := NewProjectAccessService(New(fake), fake)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestProjectAccessMemberRunsExistingWorkflowMutations(t *testing.T) {
	projectID := "project-1"
	fake := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, Name: "Project"},
		roles: map[string]string{
			"viewer": store.ProjectRoleViewer,
			"member": store.ProjectRoleMember,
		},
		issues: map[string]store.Issue{
			"issue-existing": {ID: "issue-existing", ProjectID: projectID, Title: "Existing", Status: "BACKLOG"},
		},
	}
	access := newProjectWorkflowAccess(t, fake)
	viewer := activeProjectActor("viewer", store.DeploymentRoleMember)
	member := activeProjectActor("member", store.DeploymentRoleMember)

	if _, err := access.CreateIssue(t.Context(), viewer, store.Issue{ProjectID: projectID, Title: "Denied", Status: "BACKLOG"}); appErrorCode(err) != "forbidden" {
		t.Fatalf("viewer issue mutation error=%v", err)
	}
	if fake.createIssueCalls != 0 {
		t.Fatalf("viewer reached Issue command; calls=%d", fake.createIssueCalls)
	}

	created, err := access.CreateIssue(t.Context(), member, store.Issue{ProjectID: projectID, Title: "Allowed", Status: "BACKLOG"})
	if err != nil || created.ID != "issue-created" || fake.createIssueCalls != 1 {
		t.Fatalf("member CreateIssue() issue=%+v calls=%d err=%v", created, fake.createIssueCalls, err)
	}

	assigned, run, err := access.AssignIssue(t.Context(), member, projectID, "issue-existing", "agent-1")
	if err != nil || assigned.AssignedAgentID == nil || *assigned.AssignedAgentID != "agent-1" || run.ID != "run-1" || fake.assignIssueCalls != 1 {
		t.Fatalf("member AssignIssue() issue=%+v run=%+v calls=%d err=%v", assigned, run, fake.assignIssueCalls, err)
	}

	questionDB := &questionServiceStore{}
	questions, err := NewQuestionService(questionDB)
	if err != nil {
		t.Fatal(err)
	}
	answerText := "Use the safe path"
	if _, err := access.AnswerQuestion(t.Context(), member, questions, projectID, "question-1", store.QuestionAnswer{Kind: "TEXT", Text: &answerText}); err != nil {
		t.Fatalf("member AnswerQuestion() error=%v", err)
	}
	if questionDB.lastCommand.ProjectID != projectID || questionDB.lastCommand.QuestionID != "question-1" {
		t.Fatalf("question command=%+v", questionDB.lastCommand)
	}

	reviewDB := &reviewServiceStore{}
	reviews := &ReviewService{store: reviewDB}
	if _, err := access.RequestReviewChanges(t.Context(), member, reviews, projectID, "review-1", "Please fix the edge case"); err != nil {
		t.Fatalf("member RequestReviewChanges() error=%v", err)
	}
	if reviewDB.requestCommand.ProjectID != projectID || reviewDB.requestCommand.ReviewID != "review-1" || reviewDB.requestCommand.Feedback != "Please fix the edge case" {
		t.Fatalf("review command=%+v", reviewDB.requestCommand)
	}
}
