package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

const (
	foreignProjectID  = "20202020-2020-4020-8020-202020202020"
	foreignIssueID    = "21212121-2121-4121-8121-212121212121"
	foreignRunID      = "22222222-aaaa-4222-8222-222222222222"
	foreignQuestionID = "23232323-2323-4323-8323-232323232323"
	foreignReviewID   = "24242424-2424-4424-8424-242424242424"
	foreignChunkID    = "25252525-2525-4525-8525-252525252525"
	foreignArtifactID = "26262626-2626-4626-8626-262626262626"
	foreignEventID    = "27272727-2727-4727-8727-272727272727"
)

type nestedIsolationControlPlaneStore struct {
	*fakeControlPlaneStore
}

func (s *nestedIsolationControlPlaneStore) GetProject(_ context.Context, id string) (store.Project, error) {
	switch id {
	case projectID:
		return store.Project{ID: projectID, Name: "Project A", IssuePrefix: "AB", RepositoryPath: "/repo/a", DefaultBranch: "main", WorkflowSettings: store.EmptyObject}, nil
	case foreignProjectID:
		return store.Project{ID: foreignProjectID, Name: "Project B", IssuePrefix: "FB", RepositoryPath: "/repo/b", DefaultBranch: "main", WorkflowSettings: store.EmptyObject}, nil
	default:
		return store.Project{}, store.ErrNotFound
	}
}

func (s *nestedIsolationControlPlaneStore) GetIssue(_ context.Context, pid, id string) (store.Issue, error) {
	if pid == projectID && id == issueID {
		return issueFixture("TODO"), nil
	}
	if pid == foreignProjectID && id == foreignIssueID {
		return store.Issue{ID: foreignIssueID, ProjectID: foreignProjectID, Key: "FB-1", Number: 1, Title: "Foreign Issue", Status: "TODO"}, nil
	}
	return store.Issue{}, store.ErrNotFound
}

func (s *nestedIsolationControlPlaneStore) GetRun(_ context.Context, pid, id string) (store.Run, error) {
	if pid == projectID && id == runID {
		return store.Run{ID: runID, ProjectID: projectID, IssueID: issueID, WorkspaceID: workspaceID, Status: "RUNNING"}, nil
	}
	if pid == foreignProjectID && id == foreignRunID {
		return store.Run{ID: foreignRunID, ProjectID: foreignProjectID, IssueID: foreignIssueID, WorkspaceID: workspaceID, Status: "RUNNING"}, nil
	}
	return store.Run{}, store.ErrNotFound
}

func (s *nestedIsolationControlPlaneStore) ListProjectEventsAfter(_ context.Context, pid, afterID string, _ int) ([]store.Event, error) {
	if pid == foreignProjectID && afterID == foreignEventID {
		return nil, nil
	}
	if pid == projectID && afterID == "" {
		return nil, nil
	}
	return nil, store.ErrNotFound
}

type nestedIsolationQuestionStore struct {
	apiQuestionStore
	foreign store.Question
}

func (s *nestedIsolationQuestionStore) GetQuestion(_ context.Context, projectID, id string) (store.Question, error) {
	if projectID == s.foreign.ProjectID && id == s.foreign.ID {
		return s.foreign, nil
	}
	return store.Question{}, store.ErrNotFound
}

func (s *nestedIsolationQuestionStore) AnswerQuestion(_ context.Context, command store.AnswerQuestionCommand) (store.AnswerQuestionResult, error) {
	if command.ProjectID != s.foreign.ProjectID || command.QuestionID != s.foreign.ID {
		return store.AnswerQuestionResult{}, store.ErrNotFound
	}
	return store.AnswerQuestionResult{}, nil
}

type nestedIsolationEvidenceStore struct {
	*httpRunEvidenceStore
	foreignRun store.Run
}

func (s *nestedIsolationEvidenceStore) GetRun(_ context.Context, pid, id string) (store.Run, error) {
	if pid == projectID && id == runID {
		return store.Run{ID: runID, ProjectID: projectID, IssueID: issueID, WorkspaceID: workspaceID, Status: "RUNNING"}, nil
	}
	if pid == s.foreignRun.ProjectID && id == s.foreignRun.ID {
		return s.foreignRun, nil
	}
	return store.Run{}, store.ErrNotFound
}

func (s *nestedIsolationEvidenceStore) ListRunEvents(_ context.Context, pid, id string, _ int64, _ int) ([]store.Event, error) {
	if pid == projectID && id == runID {
		return nil, nil
	}
	if pid == s.foreignRun.ProjectID && id == s.foreignRun.ID {
		return []store.Event{{ID: foreignEventID, SchemaVersion: 1, Type: "agent.message", ProjectID: foreignProjectID, RunID: &s.foreignRun.ID, Actor: store.EmptyObject, Payload: store.EmptyObject}}, nil
	}
	return nil, store.ErrNotFound
}

func (s *nestedIsolationEvidenceStore) ListRawOutputChunks(_ context.Context, pid, id string) ([]store.RawOutputChunk, error) {
	if pid == projectID && id == runID {
		return nil, nil
	}
	if pid == s.foreignRun.ProjectID && id == s.foreignRun.ID {
		return []store.RawOutputChunk{{ID: foreignChunkID, ProjectID: foreignProjectID, IssueID: foreignIssueID, RunID: foreignRunID, Stream: "STDOUT", Sequence: 1, StorageRef: "foreign-raw", SizeBytes: 6}}, nil
	}
	return nil, store.ErrNotFound
}

func (s *nestedIsolationEvidenceStore) ListArtifacts(_ context.Context, pid, id string) ([]store.Artifact, error) {
	if pid == projectID && id == runID {
		return nil, nil
	}
	if pid == s.foreignRun.ProjectID && id == s.foreignRun.ID {
		return []store.Artifact{{ID: foreignArtifactID, ProjectID: foreignProjectID, IssueID: foreignIssueID, RunID: foreignRunID, Name: "foreign.txt", Kind: "log", StorageRef: "foreign-artifact", SizeBytes: 8, SafeMetadata: store.EmptyObject}}, nil
	}
	return nil, store.ErrNotFound
}

type nestedIsolationReviewStore struct {
	*httpReviewStore
	foreign store.Review
}

func (s *nestedIsolationReviewStore) GetReview(_ context.Context, projectID, id string) (store.Review, error) {
	if projectID == s.foreign.ProjectID && id == s.foreign.ID {
		return s.foreign, nil
	}
	return store.Review{}, store.ErrNotFound
}

func (s *nestedIsolationReviewStore) BeginReviewApproval(ctx context.Context, command store.BeginReviewApprovalCommand) (store.BeginReviewApprovalResult, error) {
	review, err := s.GetReview(ctx, command.ProjectID, command.ReviewID)
	if err != nil {
		return store.BeginReviewApprovalResult{}, err
	}
	return store.BeginReviewApprovalResult{Review: review, Run: store.Run{ID: review.RunID, ProjectID: review.ProjectID}}, nil
}

func (s *nestedIsolationReviewStore) RequestReviewChanges(_ context.Context, command store.RequestReviewChangesCommand) (store.RequestReviewChangesResult, error) {
	if command.ProjectID != s.foreign.ProjectID || command.ReviewID != s.foreign.ID {
		return store.RequestReviewChangesResult{}, store.ErrNotFound
	}
	return store.RequestReviewChangesResult{}, nil
}

func TestNestedResourceIDsCannotCrossProjectBoundaries(t *testing.T) {
	fixture := newProjectAccessHTTPFixture(t)
	user, token := fixture.createUser(t, "nested-resource-member", store.DeploymentRoleMember)
	fixture.access.roles[projectGrantKey(projectID, user.ID)] = store.ProjectRoleMember

	controlStore := &nestedIsolationControlPlaneStore{fakeControlPlaneStore: &fakeControlPlaneStore{}}
	controlPlane := app.New(controlStore)
	accessService, err := app.NewProjectAccessService(controlPlane, fixture.access)
	if err != nil {
		t.Fatal(err)
	}

	questionStore := &nestedIsolationQuestionStore{foreign: store.Question{
		ID: foreignQuestionID, ProjectID: foreignProjectID, IssueID: foreignIssueID, RunID: foreignRunID,
		Prompt: "Foreign question", Kind: "TEXT", Options: json.RawMessage(`[]`), Blocking: true, Status: "OPEN",
	}}
	questionService, err := app.NewQuestionService(questionStore)
	if err != nil {
		t.Fatal(err)
	}

	foreignRun := store.Run{ID: foreignRunID, ProjectID: foreignProjectID, IssueID: foreignIssueID, WorkspaceID: workspaceID, Status: "RUNNING"}
	evidenceStore := &nestedIsolationEvidenceStore{httpRunEvidenceStore: &httpRunEvidenceStore{}, foreignRun: foreignRun}
	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	evidenceService, err := app.NewRunEvidenceService(evidenceStore, blobs)
	if err != nil {
		t.Fatal(err)
	}

	foreignReview := store.Review{
		ID: foreignReviewID, ProjectID: foreignProjectID, IssueID: foreignIssueID, RunID: foreignRunID,
		Status: "PENDING", BaseRevision: reviewBaseRevision, ReviewRevision: reviewHeadRevision,
	}
	reviewStore := &nestedIsolationReviewStore{
		httpReviewStore: &httpReviewStore{httpRunEvidenceStore: &httpRunEvidenceStore{}},
		foreign:         foreignReview,
	}
	reviewService, err := app.NewReviewService(reviewStore, controlStore, evidenceService, &httpReviewApplier{revision: "unused"})
	if err != nil {
		t.Fatal(err)
	}

	// Prove the foreign IDs identify real resources in Project B before trying
	// them through authorized Project A routes.
	if _, err := controlStore.GetIssue(t.Context(), foreignProjectID, foreignIssueID); err != nil {
		t.Fatalf("foreign issue fixture does not exist: %v", err)
	}
	if _, err := controlStore.GetRun(t.Context(), foreignProjectID, foreignRunID); err != nil {
		t.Fatalf("foreign run fixture does not exist: %v", err)
	}
	if _, err := questionStore.GetQuestion(t.Context(), foreignProjectID, foreignQuestionID); err != nil {
		t.Fatalf("foreign question fixture does not exist: %v", err)
	}
	if _, err := reviewStore.GetReview(t.Context(), foreignProjectID, foreignReviewID); err != nil {
		t.Fatalf("foreign review fixture does not exist: %v", err)
	}
	if chunks, err := evidenceStore.ListRawOutputChunks(t.Context(), foreignProjectID, foreignRunID); err != nil || len(chunks) != 1 || chunks[0].ID != foreignChunkID {
		t.Fatalf("foreign raw-output fixture=%+v err=%v", chunks, err)
	}
	if artifacts, err := evidenceStore.ListArtifacts(t.Context(), foreignProjectID, foreignRunID); err != nil || len(artifacts) != 1 || artifacts[0].ID != foreignArtifactID {
		t.Fatalf("foreign artifact fixture=%+v err=%v", artifacts, err)
	}
	if _, err := controlStore.ListProjectEventsAfter(t.Context(), foreignProjectID, foreignEventID, 1); err != nil {
		t.Fatalf("foreign event fixture does not exist: %v", err)
	}

	hub := evidence.NewHub()
	handler := newRouterWithProjectAccess(
		controlPlane,
		evidenceService,
		nil,
		nil,
		nil,
		questionService,
		reviewService,
		hub,
		accessService,
		fixture.auth,
	)

	cases := []struct {
		name, method, path, body string
	}{
		{name: "issue read", method: http.MethodGet, path: "/api/projects/" + projectID + "/issues/" + foreignIssueID},
		{name: "issue mutation", method: http.MethodPatch, path: "/api/projects/" + projectID + "/issues/" + foreignIssueID, body: `{"title":"must not cross scope"}`},
		{name: "run read", method: http.MethodGet, path: "/api/projects/" + projectID + "/runs/" + foreignRunID},
		{name: "run evidence read", method: http.MethodGet, path: "/api/projects/" + projectID + "/runs/" + foreignRunID + "/evidence"},
		{name: "run event subscription", method: http.MethodGet, path: "/api/projects/" + projectID + "/runs/" + foreignRunID + "/events"},
		{name: "raw output foreign id", method: http.MethodGet, path: "/api/projects/" + projectID + "/runs/" + runID + "/raw-output/" + foreignChunkID},
		{name: "artifact foreign id", method: http.MethodGet, path: "/api/projects/" + projectID + "/runs/" + runID + "/artifacts/" + foreignArtifactID},
		{name: "question read", method: http.MethodGet, path: "/api/projects/" + projectID + "/questions/" + foreignQuestionID},
		{name: "question answer", method: http.MethodPost, path: "/api/projects/" + projectID + "/questions/" + foreignQuestionID + "/answer", body: `{"kind":"TEXT","text":"must not cross scope"}`},
		{name: "review read", method: http.MethodGet, path: "/api/projects/" + projectID + "/reviews/" + foreignReviewID},
		{name: "review approve", method: http.MethodPost, path: "/api/projects/" + projectID + "/reviews/" + foreignReviewID + "/approve"},
		{name: "review request changes", method: http.MethodPost, path: "/api/projects/" + projectID + "/reviews/" + foreignReviewID + "/request-changes", body: `{"feedback":"must not cross scope"}`},
		{name: "project event cursor", method: http.MethodGet, path: "/api/projects/" + projectID + "/events?afterId=" + foreignEventID},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := authHTTPRequest(t, handler, testCase.method, testCase.path, testCase.body, bearer(token))
			if response.Code != http.StatusNotFound {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if !stringsContainsJSONCode(response.Body.String(), "not_found") {
				t.Fatalf("foreign resource response disclosed unexpected detail: %s", response.Body.String())
			}
		})
	}
}
