package mcpapi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func mcpString(value string) *string { return &value }
func mcpTime(value time.Time) *time.Time { return &value }
func mcpInt(value int) *int { return &value }
func mcpInt64(value int64) *int64 { return &value }

func TestBasicDTOConversionsPreservePublicContract(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 30, 0, 0, time.UTC)
	projectID := "11111111-1111-1111-1111-111111111111"
	issueID := "33333333-3333-3333-3333-333333333333"
	targetID := "44444444-4444-4444-4444-444444444444"
	allowInternal := true

	project := projectDTO(store.Project{
		ID: projectID, Name: "Project", IssuePrefix: "AB", SourceType: store.ProjectSourceGit,
		DefaultBranch: "main", AllowInternalRunner: &allowInternal, CreatedAt: now, UpdatedAt: now,
	})
	if project.ID != projectID || project.Name != "Project" || project.AllowInternalRunner == nil || !*project.AllowInternalRunner {
		t.Fatalf("project dto = %+v", project)
	}

	agentProjectID := projectID
	agent := agentDTO(store.Agent{
		ID: "22222222-2222-2222-2222-222222222222", ProjectID: &agentProjectID, Name: "Builder",
		RoleInstructions: "Build", Engine: "opencode", ModelProfileID: "model", ConcurrencyLimit: 2,
		State: "ACTIVE", CreatedAt: now, UpdatedAt: now,
	})
	if agent.Name != "Builder" || agent.ProjectID == nil || *agent.ProjectID != projectID || agent.ConcurrencyLimit != 2 {
		t.Fatalf("agent dto = %+v", agent)
	}

	assignee := assigneeDTO(store.Assignee{Type: "AGENT", ID: agent.ID, Name: agent.Name})
	if assignee.Type != "AGENT" || assignee.ID != agent.ID || assignee.Name != agent.Name {
		t.Fatalf("assignee dto = %+v", assignee)
	}

	issue := issueDTO(store.Issue{
		ID: issueID, ProjectID: projectID, Key: "AB-7", Title: "Ship MCP", Description: "Expose control plane",
		Status: "IN_PROGRESS", Priority: 3, AssigneeType: mcpString("AGENT"), AssigneeID: mcpString(agent.ID),
		AssigneeName: mcpString("Builder"), CreatedByType: mcpString(store.ActorTypeHuman),
		CreatedByID: mcpString(mcpTestUserID), CreatedByName: mcpString("Admin"), CurrentBranch: mcpString("feat/mcp"),
		LastEvent: &store.Event{ID: "event-1", Type: "issue.updated", OccurredAt: now}, CreatedAt: now, UpdatedAt: now,
	})
	if issue.ID != "AB-7" || issue.AssignedTo == nil || issue.AssignedTo.Name != "Builder" || issue.CreatedBy == nil || issue.CreatedBy.Name != "Admin" || issue.LastEvent == nil || issue.LastEvent.ID != "event-1" {
		t.Fatalf("issue dto = %+v", issue)
	}

	keys := map[string]string{issueID: "AB-7", targetID: "AB-8"}
	relationship := relationshipDTO(store.IssueRelationship{
		ID: "relationship-1", ProjectID: projectID, SourceIssueID: issueID, TargetIssueID: targetID, Type: "blocks", CreatedAt: now,
	}, keys)
	if relationship.SourceIssueID != "AB-7" || relationship.TargetIssueID != "AB-8" || relationship.Type != "blocks" {
		t.Fatalf("relationship dto = %+v", relationship)
	}

	run := runDTO(store.Run{
		ID: "55555555-5555-5555-5555-555555555555", ProjectID: projectID, IssueID: issueID, AgentID: mcpString(agent.ID),
		Attempt: 2, Status: "RUNNING", QueueReason: mcpString("queued"), CurrentBranch: mcpString("feat/mcp"),
		CreatedAt: now, StartedAt: mcpTime(now), UpdatedAt: now,
	}, keys)
	if run.IssueID != "AB-7" || run.Status != "RUNNING" || run.AgentID == nil || *run.AgentID != agent.ID {
		t.Fatalf("run dto = %+v", run)
	}

	question := questionDTO(store.Question{
		ID: "66666666-6666-6666-6666-666666666666", ProjectID: projectID, IssueID: issueID, RunID: run.ID,
		Prompt: "Continue?", Kind: "single_choice", Options: json.RawMessage(`[{"id":"yes"}]`),
		Recommendation: mcpString("yes"), Blocking: true, Status: "OPEN", CreatedAt: now,
	}, keys)
	if question.IssueID != "AB-7" || question.Options == nil || question.Recommendation == nil || *question.Recommendation != "yes" {
		t.Fatalf("question dto = %+v", question)
	}

	decisionIssueID := issueID
	decision := decisionDTO(store.Decision{
		ID: "77777777-7777-7777-7777-777777777777", ProjectID: projectID, IssueID: &decisionIssueID,
		RunID: mcpString(run.ID), QuestionID: mcpString(question.ID), Kind: "question_answer", Outcome: "yes",
		ActorType: store.ActorTypeHuman, ActorID: mcpString(mcpTestUserID), SafeDetails: json.RawMessage(`{"source":"mcp"}`), CreatedAt: now,
	}, keys)
	if decision.IssueID == nil || *decision.IssueID != "AB-7" || decision.SafeDetails == nil {
		t.Fatalf("decision dto = %+v", decision)
	}

	review := reviewDTO(store.Review{
		ID: "88888888-8888-8888-8888-888888888888", ProjectID: projectID, IssueID: issueID, RunID: run.ID,
		Status: "PENDING", BaseRevision: "base", ReviewRevision: "head", RequestedAt: now, CreatedAt: now, UpdatedAt: now,
	}, keys)
	if review.IssueID != "AB-7" || review.BaseRevision != "base" || review.ReviewRevision != "head" {
		t.Fatalf("review dto = %+v", review)
	}
}

func TestEvidenceDTOClassifiesEventsAndPreservesSafeData(t *testing.T) {
	now := time.Date(2026, 9, 15, 13, 0, 0, 0, time.UTC)
	projectID := "11111111-1111-1111-1111-111111111111"
	issueID := "33333333-3333-3333-3333-333333333333"
	runID := "55555555-5555-5555-5555-555555555555"
	sequence := int64(4)

	value := app.RunEvidence{
		Run: store.Run{ID: runID, ProjectID: projectID, IssueID: issueID, Status: "RUNNING", CreatedAt: now, UpdatedAt: now},
		Provenance: json.RawMessage(`{"source":"git"}`),
		RuntimeInstances: []store.RuntimeInstance{{ID: "runtime-instance", RuntimeID: "runtime", Status: "RUNNING", RunnerStatus: "CONNECTED", CreatedAt: now, UpdatedAt: now}},
		Sessions: []store.ExecutionSession{{ID: "session", RuntimeInstanceID: "runtime-instance", RunnerID: "runner", Status: "RUNNING", CWD: "/workspace", CommandArgv: json.RawMessage(`["go","test","./..."]`), ExitCode: mcpInt(0), CreatedAt: now, UpdatedAt: now}},
		Events: []store.Event{
			{ID: "test-event", SchemaVersion: 1, Type: "test.completed", OccurredAt: now, ProjectID: projectID, IssueID: &issueID, RunID: &runID, Sequence: &sequence, Actor: json.RawMessage(`{"type":"AGENT"}`), Payload: json.RawMessage(`{"passed":true}`)},
			{ID: "file-event", SchemaVersion: 1, Type: "file.changed", OccurredAt: now, ProjectID: projectID, IssueID: &issueID, RunID: &runID, Sequence: mcpInt64(5)},
			{ID: "run-event", SchemaVersion: 1, Type: "run.progress", OccurredAt: now, ProjectID: projectID, RunID: &runID, Sequence: mcpInt64(6)},
		},
		RawOutput: []store.RawOutputChunk{{ID: "chunk", Stream: "stdout", Sequence: 1, SizeBytes: 12, Digest: mcpString("sha256:abc"), CreatedAt: now}},
		Artifacts: []store.Artifact{{ID: "artifact", Name: "report.json", Kind: "report", MediaType: mcpString("application/json"), SizeBytes: 42, Digest: mcpString("sha256:def"), SafeMetadata: json.RawMessage(`{"kind":"test"}`), CreatedAt: now}},
	}

	out := evidenceDTO(value, map[string]string{issueID: "AB-7"})
	if out.Run.IssueID != "AB-7" || out.Provenance == nil || len(out.RuntimeInstances) != 1 || len(out.Sessions) != 1 {
		t.Fatalf("evidence summary = %+v", out)
	}
	if len(out.Events) != 3 || len(out.Tests) != 1 || out.Tests[0].ID != "test-event" || len(out.FileChanges) != 1 || out.FileChanges[0].ID != "file-event" {
		t.Fatalf("classified events = events:%d tests:%+v files:%+v", len(out.Events), out.Tests, out.FileChanges)
	}
	if out.Events[0].IssueID == nil || *out.Events[0].IssueID != "AB-7" || out.Events[0].Actor == nil || out.Events[0].Payload == nil {
		t.Fatalf("event dto = %+v", out.Events[0])
	}
	if out.Sessions[0].RuntimeInstanceID == nil || *out.Sessions[0].RuntimeInstanceID != "runtime-instance" || out.Sessions[0].RunnerID == nil || *out.Sessions[0].RunnerID != "runner" || out.Sessions[0].Command == nil {
		t.Fatalf("session dto = %+v", out.Sessions[0])
	}
	if len(out.RawOutput) != 1 || out.RawOutput[0].ID != "chunk" || len(out.Artifacts) != 1 || out.Artifacts[0].SafeMetadata == nil {
		t.Fatalf("evidence output = raw:%+v artifacts:%+v", out.RawOutput, out.Artifacts)
	}
}

func TestMCPValidationAndJSONHelpers(t *testing.T) {
	valid := "11111111-1111-1111-1111-111111111111"
	if !validUUID(valid) || validUUID("not-a-uuid") || validUUID("11111111-1111-1111-1111-11111111111z") {
		t.Fatal("UUID validation mismatch")
	}
	if err := requireUUID(valid, "id"); err != nil {
		t.Fatalf("valid UUID rejected: %v", err)
	}
	if err := requireUUID("bad", "projectId"); err == nil {
		t.Fatal("invalid UUID accepted")
	}

	if toolError(t.Context(), nil) != nil {
		t.Fatal("nil tool error should stay nil")
	}
	apiErr := app.NewError("invalid_argument", "bad input", store.ErrInvalidArgument)
	if got := toolError(t.Context(), apiErr); got == nil || got.Error() != "invalid_argument: bad input" {
		t.Fatalf("application tool error = %v", got)
	}
	if got := toolError(t.Context(), errors.New("database details")); got == nil || got.Error() != "internal_error: internal server error" {
		t.Fatalf("internal tool error = %v", got)
	}

	if jsonValue(nil) != nil || jsonValue(json.RawMessage(`{`)) != nil {
		t.Fatal("invalid or empty JSON should map to nil")
	}
	decoded := jsonValue(json.RawMessage(`{"ok":true}`))
	object, ok := decoded.(map[string]any)
	if !ok || object["ok"] != true {
		t.Fatalf("decoded JSON = %#v", decoded)
	}
	if valueFromJSONMarshal(nil) != nil || valueFromJSONMarshal(make(chan int)) != nil {
		t.Fatal("nil or unmarshalable values should map to nil")
	}
	if valueFromJSONMarshal(map[string]any{"count": 2}) == nil {
		t.Fatal("marshalable value unexpectedly mapped to nil")
	}
	if optionalString("  ") != nil {
		t.Fatal("blank optional string should be nil")
	}
	trimmed := optionalString(" runner ")
	if trimmed == nil || *trimmed != "runner" {
		t.Fatalf("optional string = %v", trimmed)
	}
}

func TestMCPHandlersRejectInvalidProjectIDsBeforeCallingServices(t *testing.T) {
	ctx := context.Background()
	server := &Server{}
	checks := []struct {
		name string
		call func() error
	}{
		{"get_project", func() error { _, _, err := server.getProject(ctx, nil, ProjectInput{ProjectID: "bad"}); return err }},
		{"get_project_role", func() error { _, _, err := server.getProjectRole(ctx, nil, ProjectInput{ProjectID: "bad"}); return err }},
		{"list_project_members", func() error { _, _, err := server.listProjectMembers(ctx, nil, ProjectInput{ProjectID: "bad"}); return err }},
		{"list_agents", func() error { _, _, err := server.listAgents(ctx, nil, ProjectInput{ProjectID: "bad"}); return err }},
		{"get_agent", func() error { _, _, err := server.getAgent(ctx, nil, AgentInput{ProjectID: "bad"}); return err }},
		{"list_issue_assignees", func() error { _, _, err := server.listIssueAssignees(ctx, nil, ProjectInput{ProjectID: "bad"}); return err }},
		{"list_issues", func() error { _, _, err := server.listIssues(ctx, nil, ProjectInput{ProjectID: "bad"}); return err }},
		{"get_issue", func() error { _, _, err := server.getIssue(ctx, nil, IssueInput{ProjectID: "bad"}); return err }},
		{"create_issue", func() error { _, _, err := server.createIssue(ctx, nil, CreateIssueInput{ProjectID: "bad"}); return err }},
		{"update_issue", func() error { _, _, err := server.updateIssue(ctx, nil, UpdateIssueInput{ProjectID: "bad"}); return err }},
		{"set_issue_status", func() error { _, _, err := server.setIssueStatus(ctx, nil, SetIssueStatusInput{ProjectID: "bad"}); return err }},
		{"set_issue_assignee", func() error { _, _, err := server.setIssueAssignee(ctx, nil, SetIssueAssigneeInput{ProjectID: "bad"}); return err }},
		{"list_issue_relationships", func() error { _, _, err := server.listIssueRelationships(ctx, nil, IssueInput{ProjectID: "bad"}); return err }},
		{"create_issue_relationship", func() error { _, _, err := server.createIssueRelationship(ctx, nil, CreateRelationshipInput{ProjectID: "bad"}); return err }},
		{"delete_issue_relationship", func() error { _, _, err := server.deleteIssueRelationship(ctx, nil, DeleteRelationshipInput{ProjectID: "bad"}); return err }},
		{"get_issue_execution_state", func() error { _, _, err := server.getIssueExecutionState(ctx, nil, IssueInput{ProjectID: "bad"}); return err }},
		{"start_issue_run", func() error { _, _, err := server.startIssueRun(ctx, nil, IssueInput{ProjectID: "bad"}); return err }},
		{"list_runs", func() error { _, _, err := server.listRuns(ctx, nil, ProjectInput{ProjectID: "bad"}); return err }},
		{"get_run", func() error { _, _, err := server.getRun(ctx, nil, RunInput{ProjectID: "bad"}); return err }},
		{"cancel_run", func() error { _, _, err := server.cancelRun(ctx, nil, RunInput{ProjectID: "bad"}); return err }},
		{"inspect_run", func() error { _, _, err := server.inspectRun(ctx, nil, RunInput{ProjectID: "bad"}); return err }},
		{"read_run_output_chunk", func() error { _, _, err := server.readRunOutputChunk(ctx, nil, ReadRunOutputInput{ProjectID: "bad"}); return err }},
		{"list_questions", func() error { _, _, err := server.listQuestions(ctx, nil, ListQuestionsInput{ProjectID: "bad"}); return err }},
		{"get_question", func() error { _, _, err := server.getQuestion(ctx, nil, QuestionInput{ProjectID: "bad"}); return err }},
		{"answer_question", func() error { _, _, err := server.answerQuestion(ctx, nil, AnswerQuestionInput{ProjectID: "bad"}); return err }},
		{"list_reviews", func() error { _, _, err := server.listReviews(ctx, nil, ListReviewsInput{ProjectID: "bad"}); return err }},
		{"get_review", func() error { _, _, err := server.getReview(ctx, nil, ReviewInput{ProjectID: "bad"}); return err }},
		{"approve_review", func() error { _, _, err := server.approveReview(ctx, nil, ReviewInput{ProjectID: "bad"}); return err }},
		{"request_review_changes", func() error { _, _, err := server.requestReviewChanges(ctx, nil, RequestReviewChangesInput{ProjectID: "bad"}); return err }},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			err := check.call()
			if err == nil || err.Error() != "invalid_argument: projectId must be a UUID" {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
