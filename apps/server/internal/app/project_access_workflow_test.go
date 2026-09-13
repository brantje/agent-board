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
	runs             map[string]store.Run
	createIssueCalls int
	updateIssueCalls int
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

func (s *projectWorkflowAuthorizationStore) ListIssues(_ context.Context, projectID string) ([]store.Issue, error) {
	if projectID != s.project.ID {
		return nil, store.ErrNotFound
	}
	values := make([]store.Issue, 0, len(s.issues))
	for _, issue := range s.issues {
		values = append(values, issue)
	}
	return values, nil
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

func (s *projectWorkflowAuthorizationStore) GetIssueUUIDByKey(_ context.Context, projectID, key string) (string, error) {
	if projectID != s.project.ID || key != "AB-1" {
		return "", store.ErrNotFound
	}
	return "issue-existing", nil
}

func (s *projectWorkflowAuthorizationStore) UpdateIssue(_ context.Context, input store.Issue) (store.Issue, error) {
	if _, ok := s.issues[input.ID]; !ok || input.ProjectID != s.project.ID {
		return store.Issue{}, store.ErrNotFound
	}
	s.updateIssueCalls++
	s.issues[input.ID] = input
	return input, nil
}

func (s *projectWorkflowAuthorizationStore) ListRuns(_ context.Context, projectID string) ([]store.Run, error) {
	if projectID != s.project.ID {
		return nil, store.ErrNotFound
	}
	values := make([]store.Run, 0, len(s.runs))
	for _, run := range s.runs {
		values = append(values, run)
	}
	return values, nil
}

func (s *projectWorkflowAuthorizationStore) GetRun(_ context.Context, projectID, runID string) (store.Run, error) {
	run, ok := s.runs[runID]
	if !ok || run.ProjectID != projectID {
		return store.Run{}, store.ErrNotFound
	}
	return run, nil
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
	run := store.Run{ID: "run-1", ProjectID: projectID, IssueID: issueID, Status: "QUEUED"}
	s.runs[run.ID] = run
	return issue, run, nil
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
		project: store.Project{ID: projectID, Name: "Project", IssuePrefix: "AB"},
		roles: map[string]string{
			"viewer": store.ProjectRoleViewer,
			"member": store.ProjectRoleMember,
		},
		issues: map[string]store.Issue{
			"issue-existing": {ID: "issue-existing", ProjectID: projectID, Number: 1, Title: "Existing", Status: "BACKLOG"},
		},
		runs: map[string]store.Run{},
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

func TestProjectAccessViewerQueriesAndMemberIssueUpdateUseExistingServices(t *testing.T) {
	projectID := "project-1"
	fake := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, Name: "Project", IssuePrefix: "AB"},
		roles: map[string]string{
			"viewer": store.ProjectRoleViewer,
			"member": store.ProjectRoleMember,
		},
		issues: map[string]store.Issue{
			"issue-existing": {ID: "issue-existing", ProjectID: projectID, Number: 1, Title: "Existing", Status: "BACKLOG"},
		},
		runs: map[string]store.Run{
			"run-existing": {ID: "run-existing", ProjectID: projectID, IssueID: "issue-existing", Status: "COMPLETED"},
		},
	}
	access := newProjectWorkflowAccess(t, fake)
	viewer := activeProjectActor("viewer", store.DeploymentRoleMember)
	member := activeProjectActor("member", store.DeploymentRoleMember)

	resolved, err := access.ResolveIssueUUID(t.Context(), viewer, projectID, "AB-1")
	if err != nil || resolved != "issue-existing" {
		t.Fatalf("ResolveIssueUUID() resolved=%q err=%v", resolved, err)
	}
	issues, err := access.ListIssues(t.Context(), viewer, projectID)
	if err != nil || len(issues) != 1 {
		t.Fatalf("ListIssues() issues=%+v err=%v", issues, err)
	}
	issue, err := access.GetIssue(t.Context(), viewer, projectID, "issue-existing")
	if err != nil || issue.Title != "Existing" {
		t.Fatalf("GetIssue() issue=%+v err=%v", issue, err)
	}
	runs, err := access.ListRuns(t.Context(), viewer, projectID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("ListRuns() runs=%+v err=%v", runs, err)
	}
	run, err := access.GetRun(t.Context(), viewer, projectID, "run-existing")
	if err != nil || run.ID != "run-existing" {
		t.Fatalf("GetRun() run=%+v err=%v", run, err)
	}

	issue.Title = "Updated by member"
	updated, err := access.UpdateIssue(t.Context(), member, issue)
	if err != nil || updated.Title != "Updated by member" || fake.updateIssueCalls != 1 {
		t.Fatalf("UpdateIssue() issue=%+v calls=%d err=%v", updated, fake.updateIssueCalls, err)
	}
	if _, err := access.UpdateIssue(t.Context(), viewer, issue); appErrorCode(err) != "forbidden" {
		t.Fatalf("viewer UpdateIssue() error=%v", err)
	}
}

func TestProjectAccessQuestionAndReviewReadsUseViewerBoundary(t *testing.T) {
	projectID := "project-1"
	fake := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, Name: "Project"},
		roles: map[string]string{
			"viewer": store.ProjectRoleViewer,
		},
		issues: map[string]store.Issue{},
		runs:   map[string]store.Run{},
	}
	access := newProjectWorkflowAccess(t, fake)
	viewer := activeProjectActor("viewer", store.DeploymentRoleMember)

	questionDB := &questionServiceStore{questions: []store.Question{{ID: "question-1", ProjectID: projectID, Prompt: "Continue?", Kind: "TEXT", Status: "OPEN"}}}
	questions, err := NewQuestionService(questionDB)
	if err != nil {
		t.Fatal(err)
	}
	listedQuestions, err := access.ListQuestions(t.Context(), viewer, questions, projectID, store.QuestionFilter{})
	if err != nil || len(listedQuestions) != 1 {
		t.Fatalf("ListQuestions() questions=%+v err=%v", listedQuestions, err)
	}
	question, err := access.GetQuestion(t.Context(), viewer, questions, projectID, "question-1")
	if err != nil || question.ID != "question-1" {
		t.Fatalf("GetQuestion() question=%+v err=%v", question, err)
	}

	reviewDB := &reviewServiceStore{list: []store.Review{{ID: "review-1", ProjectID: projectID, BaseRevision: "base", ReviewRevision: "candidate"}}}
	reviews := &ReviewService{store: reviewDB}
	listedReviews, err := access.ListReviews(t.Context(), viewer, reviews, projectID, store.ReviewFilter{})
	if err != nil || len(listedReviews) != 1 || listedReviews[0].ID != "review-1" {
		t.Fatalf("ListReviews() reviews=%+v err=%v", listedReviews, err)
	}

	if _, err := access.ListQuestions(t.Context(), viewer, nil, projectID, store.QuestionFilter{}); err == nil {
		t.Fatal("ListQuestions() unexpectedly accepted nil QuestionService")
	}
	if _, err := access.GetQuestion(t.Context(), viewer, nil, projectID, "question-1"); err == nil {
		t.Fatal("GetQuestion() unexpectedly accepted nil QuestionService")
	}
	if _, err := access.ListReviews(t.Context(), viewer, nil, projectID, store.ReviewFilter{}); err == nil {
		t.Fatal("ListReviews() unexpectedly accepted nil ReviewService")
	}
}
