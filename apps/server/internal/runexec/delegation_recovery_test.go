package runexec

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/brantje/agent-board/apps/server/internal/workspace"
)

type delegationRecoveryStore struct {
	processTestStore
	delegations       []store.Delegation
	marks             []delegationHandoffMark
	listDelegationsErr error
	listEventsErr      error
	markErr            error
	current            string
	persisted          string
}

func (s *delegationRecoveryStore) RequestDelegation(context.Context, store.RequestDelegationCommand) (store.RequestDelegationResult, error) {
	return store.RequestDelegationResult{}, store.ErrConflict
}

func (s *delegationRecoveryStore) GetDelegationByRun(_ context.Context, projectID, runID string) (store.Delegation, error) {
	for _, delegation := range s.delegations {
		if delegation.ProjectID == projectID && delegation.DelegatedRunID == runID {
			return delegation, nil
		}
	}
	return store.Delegation{}, store.ErrNotFound
}

func (s *delegationRecoveryStore) ListDelegationsByParentRun(_ context.Context, projectID, parentRunID string) ([]store.Delegation, error) {
	if s.listDelegationsErr != nil {
		return nil, s.listDelegationsErr
	}
	var result []store.Delegation
	for _, delegation := range s.delegations {
		if delegation.ProjectID == projectID && delegation.ParentRunID == parentRunID {
			result = append(result, delegation)
		}
	}
	return result, nil
}

func (s *delegationRecoveryStore) MarkDelegationWorkspaceHandoffReady(_ context.Context, projectID, parentRunID, delegationID, delegatedRunID string) error {
	if s.markErr != nil {
		return s.markErr
	}
	for _, mark := range s.marks {
		if mark.projectID == projectID && mark.parentRunID == parentRunID && mark.delegationID == delegationID && mark.delegatedRunID == delegatedRunID {
			return nil
		}
	}
	s.marks = append(s.marks, delegationHandoffMark{projectID: projectID, parentRunID: parentRunID, delegationID: delegationID, delegatedRunID: delegatedRunID})
	return nil
}

func (s *delegationRecoveryStore) ListRunEvents(_ context.Context, projectID, runID string, afterSequence int64, limit int) ([]store.Event, error) {
	if s.listEventsErr != nil {
		return nil, s.listEventsErr
	}
	if limit <= 0 {
		limit = len(s.events)
	}
	result := make([]store.Event, 0, limit)
	for _, event := range s.events {
		if event.ProjectID != projectID || event.RunID == nil || *event.RunID != runID || event.Sequence == nil || *event.Sequence <= afterSequence {
			continue
		}
		result = append(result, event)
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func (s *delegationRecoveryStore) AcquireWorkspaceExecutionLock(context.Context, string, string) (store.WorkspaceBootstrapLock, error) {
	return noopWorkspaceLock{}, nil
}

func (s *delegationRecoveryStore) GetWorkspaceCurrentRevision(context.Context, string, string) (string, error) {
	return s.current, nil
}

func (s *delegationRecoveryStore) UpdateWorkspaceCurrentRevision(_ context.Context, _, _, revision string) (string, error) {
	s.persisted = revision
	s.current = revision
	return revision, nil
}

func delegationRecoveryFixture(t *testing.T) (*delegationRecoveryStore, store.Run, store.Delegation, executioncontext.SafeContext) {
	t.Helper()
	safe := processTestSafeContext("/workspace")
	agentID := safe.Agent.ID
	run := store.Run{
		ID: safe.Run.ID,
		ProjectID: safe.Project.ID,
		IssueID: safe.Issue.ID,
		WorkspaceID: safe.Workspace.ID,
		AgentID: &agentID,
		Status: "RUNNING",
	}
	delegation := store.Delegation{
		ID: "delegation-1",
		ProjectID: safe.Project.ID,
		IssueID: safe.Issue.ID,
		ParentRunID: safe.Run.ID,
		ParentAgentID: safe.Agent.ID,
		TargetAgentID: "agent-2",
		Task: "continue delegated work",
		DelegatedRunID: "delegated-run-1",
		RequestKey: "call-1",
	}
	return &delegationRecoveryStore{delegations: []store.Delegation{delegation}}, run, delegation, safe
}

func delegationRecoveryEvent(t *testing.T, projectID, runID string, sequence int64, eventType string, payload any) store.Event {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	run := runID
	seq := sequence
	return store.Event{ProjectID: projectID, RunID: &run, Type: eventType, Sequence: &seq, Payload: encoded}
}

func TestReconcileRecoversSynchronizedDelegationWithoutEngineReplay(t *testing.T) {
	storeFake, run, delegation, _ := delegationRecoveryFixture(t)
	storeFake.sessions = []store.ExecutionSession{{ID: "session-1", RunID: run.ID, Status: "COMPLETED", RunnerID: "runner-1"}}
	storeFake.events = []store.Event{
		delegationRecoveryEvent(t, run.ProjectID, run.ID, 1, "tool.completed", evidence.ToolPayload{
			Name: "delegate_task",
			ToolCallID: delegation.RequestKey,
			Input: map[string]any{"targetAgentId": delegation.TargetAgentID, "task": delegation.Task},
		}),
		delegationRecoveryEvent(t, run.ProjectID, run.ID, 2, "workspace.transfer.completed", map[string]any{"direction": "from_runner"}),
	}
	processor := &Processor{store: storeFake, sessions: reconcileSessions{}}

	outcome, _, err := processor.Reconcile(t.Context(), &store.SchedulerAdmission{Run: run})
	if err != nil {
		t.Fatal(err)
	}
	if outcome != store.SchedulerReconciliationUnknown {
		t.Fatalf("outcome=%s want UNKNOWN", outcome)
	}
	if len(storeFake.marks) != 1 {
		t.Fatalf("handoff marks=%+v", storeFake.marks)
	}

	outcome, _, err = processor.Reconcile(t.Context(), &store.SchedulerAdmission{Run: run})
	if err != nil || outcome != store.SchedulerReconciliationUnknown || len(storeFake.marks) != 1 {
		t.Fatalf("repeated recovery outcome=%s err=%v marks=%+v", outcome, err, storeFake.marks)
	}
}

func TestReconcileRecoversDelegationFromRetainedRunnerWorkspace(t *testing.T) {
	repository := initProcessTestRepository(t)
	storeFake, run, delegation, safe := delegationRecoveryFixture(t)
	safe.Workspace.Path = repository
	storeFake.sessions = []store.ExecutionSession{{ID: "session-1", RunID: run.ID, Status: "COMPLETED", RunnerID: "runner-1"}}
	storeFake.events = []store.Event{
		delegationRecoveryEvent(t, run.ProjectID, run.ID, 1, "tool.completed", evidence.ToolPayload{
			Name: "delegate_task",
			ToolCallID: delegation.RequestKey,
			Input: map[string]any{"targetAgentId": delegation.TargetAgentID, "task": delegation.Task},
		}),
	}
	provenance, err := json.Marshal(executioncontext.Provenance{SchemaVersion: executioncontext.ProvenanceSchemaVersion, Context: safe})
	if err != nil {
		t.Fatal(err)
	}
	storeFake.provenance = provenance

	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	client := &successfulSyncClient{payload: runnerTransferPayload(t, repository)}
	recorder, err := evidence.NewRecorder(storeFake, nil)
	if err != nil {
		t.Fatal(err)
	}
	processor := &Processor{
		store: storeFake,
		sessions: reconcileSessions{},
		git: git,
		events: recorder,
		runners: runnerSyncConnector{client: client},
	}

	outcome, _, err := processor.Reconcile(t.Context(), &store.SchedulerAdmission{Run: run})
	if err != nil {
		t.Fatal(err)
	}
	if outcome != store.SchedulerReconciliationUnknown {
		t.Fatalf("outcome=%s want UNKNOWN", outcome)
	}
	if len(storeFake.marks) != 1 || !client.confirmed {
		t.Fatalf("marks=%+v runnerConfirmed=%v", storeFake.marks, client.confirmed)
	}
	if len(client.directions) != 1 || client.directions[0] != "from_runner" {
		t.Fatalf("runner recovery directions=%v", client.directions)
	}

	eventsBefore := len(storeFake.events)
	outcome, _, err = processor.Reconcile(t.Context(), &store.SchedulerAdmission{Run: run})
	if err != nil || outcome != store.SchedulerReconciliationUnknown {
		t.Fatalf("repeated outcome=%s err=%v", outcome, err)
	}
	if len(storeFake.marks) != 1 || len(client.directions) != 1 || len(storeFake.events) != eventsBefore {
		t.Fatalf("repeated recovery was not idempotent: marks=%+v directions=%v events=%d->%d", storeFake.marks, client.directions, eventsBefore, len(storeFake.events))
	}
}

func TestReconcileRecoversLocalDelegationByRefinalizingWorkspace(t *testing.T) {
	storeFake, run, delegation, safe := delegationRecoveryFixture(t)
	storeFake.sessions = []store.ExecutionSession{{ID: "session-1", RunID: run.ID, Status: "COMPLETED", RuntimeInstanceID: "runtime-instance-1"}}
	storeFake.events = []store.Event{
		delegationRecoveryEvent(t, run.ProjectID, run.ID, 1, "tool.completed", evidence.ToolPayload{
			Name: "delegate_task",
			ToolCallID: delegation.RequestKey,
			Input: map[string]any{"targetAgentId": delegation.TargetAgentID, "task": delegation.Task},
		}),
	}
	storeFake.current = "start-sha"
	provenance, err := json.Marshal(executioncontext.Provenance{SchemaVersion: executioncontext.ProvenanceSchemaVersion, Context: safe})
	if err != nil {
		t.Fatal(err)
	}
	storeFake.provenance = provenance
	git := &serverFinalizeGit{revision: "review-sha"}
	processor := &Processor{store: storeFake, sessions: reconcileSessions{}, git: git}

	outcome, _, err := processor.Reconcile(t.Context(), &store.SchedulerAdmission{Run: run})
	if err != nil {
		t.Fatal(err)
	}
	if outcome != store.SchedulerReconciliationUnknown {
		t.Fatalf("outcome=%s want UNKNOWN", outcome)
	}
	if len(storeFake.marks) != 1 || storeFake.persisted != "review-sha" {
		t.Fatalf("marks=%+v persisted=%q", storeFake.marks, storeFake.persisted)
	}
	if git.branch != safe.Workspace.WorkingBranch || git.start != "start-sha" {
		t.Fatalf("re-finalize branch=%q start=%q", git.branch, git.start)
	}
}

func TestReconcileDoesNotRecoverDelegationWithoutSafeBoundary(t *testing.T) {
	tests := []struct {
		name         string
		status       string
		runnerID     string
		synchronized bool
	}{
		{name: "no completed tool", status: "COMPLETED", runnerID: "runner-1"},
		{name: "failed execution after synchronized cleanup", status: "FAILED", runnerID: "runner-1", synchronized: true},
		{name: "cancelled execution after synchronized cleanup", status: "CANCELLED", runnerID: "runner-1", synchronized: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			storeFake, run, delegation, _ := delegationRecoveryFixture(t)
			storeFake.sessions = []store.ExecutionSession{{ID: "session-1", RunID: run.ID, Status: tc.status, RunnerID: tc.runnerID}}
			if tc.name != "no completed tool" {
				storeFake.events = []store.Event{
					delegationRecoveryEvent(t, run.ProjectID, run.ID, 1, "tool.completed", evidence.ToolPayload{
						Name: "delegate_task",
						ToolCallID: delegation.RequestKey,
						Input: map[string]any{"targetAgentId": delegation.TargetAgentID, "task": delegation.Task},
					}),
				}
				if tc.synchronized {
					storeFake.events = append(storeFake.events,
						delegationRecoveryEvent(t, run.ProjectID, run.ID, 2, "workspace.transfer.completed", map[string]any{"direction": "from_runner"}),
					)
				}
			}
			processor := &Processor{store: storeFake, sessions: reconcileSessions{}}
			outcome, _, err := processor.Reconcile(t.Context(), &store.SchedulerAdmission{Run: run})
			if err != nil {
				t.Fatal(err)
			}
			if outcome != store.SchedulerReconciliationUnknown {
				t.Fatalf("outcome=%s want UNKNOWN", outcome)
			}
			if len(storeFake.marks) != 0 {
				t.Fatalf("unsafe recovery marked handoff ready: %+v", storeFake.marks)
			}
		})
	}
}

func TestDelegationRecoveryEvidenceRequiresCanonicalIdentityAndOrdering(t *testing.T) {
	_, run, delegation, _ := delegationRecoveryFixture(t)
	completed := evidence.ToolPayload{
		Name: "delegate_task",
		ToolCallID: delegation.RequestKey,
		Input: map[string]any{"targetAgentId": delegation.TargetAgentID, "task": delegation.Task},
	}

	t.Run("workspace return before canonical completion is insufficient", func(t *testing.T) {
		events := []store.Event{
			delegationRecoveryEvent(t, run.ProjectID, run.ID, 1, "workspace.transfer.completed", map[string]any{"direction": "from_runner"}),
			delegationRecoveryEvent(t, run.ProjectID, run.ID, 2, "tool.completed", completed),
		}
		gotCompleted, synchronized := delegationRecoveryEvidence(events, delegation)
		if !gotCompleted || synchronized {
			t.Fatalf("completed=%v synchronized=%v want true,false", gotCompleted, synchronized)
		}
	})

	t.Run("mismatched canonical fields fail closed", func(t *testing.T) {
		for _, payload := range []evidence.ToolPayload{
			{Name: "delegate_task", ToolCallID: "wrong-call", Input: map[string]any{"targetAgentId": delegation.TargetAgentID, "task": delegation.Task}},
			{Name: "delegate_task", ToolCallID: delegation.RequestKey, Input: map[string]any{"targetAgentId": "wrong-agent", "task": delegation.Task}},
			{Name: "delegate_task", ToolCallID: delegation.RequestKey, Input: map[string]any{"targetAgentId": delegation.TargetAgentID, "task": "wrong task"}},
			{Name: "different_tool", ToolCallID: delegation.RequestKey, Input: map[string]any{"targetAgentId": delegation.TargetAgentID, "task": delegation.Task}},
		} {
			event := delegationRecoveryEvent(t, run.ProjectID, run.ID, 1, "tool.completed", payload)
			gotCompleted, synchronized := delegationRecoveryEvidence([]store.Event{event}, delegation)
			if gotCompleted || synchronized {
				t.Fatalf("mismatched payload recovered handoff: %+v", payload)
			}
		}
	})

	t.Run("only authoritative handback direction after completion counts", func(t *testing.T) {
		completedEvent := delegationRecoveryEvent(t, run.ProjectID, run.ID, 1, "tool.completed", completed)
		for _, tc := range []struct {
			name      string
			payload   json.RawMessage
			wantSync  bool
		}{
			{name: "malformed", payload: json.RawMessage(`{"direction":`), wantSync: false},
			{name: "to runner", payload: json.RawMessage(`{"direction":"to_runner"}`), wantSync: false},
			{name: "local return", payload: json.RawMessage(`{"direction":"from_runner"}`), wantSync: true},
			{name: "remote publication", payload: json.RawMessage(`{"direction":"git_publish"}`), wantSync: true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				seq := int64(2)
				runID := run.ID
				transfer := store.Event{
					ProjectID: run.ProjectID,
					RunID: &runID,
					Type: "workspace.transfer.completed",
					Sequence: &seq,
					Payload: tc.payload,
				}
				gotCompleted, synchronized := delegationRecoveryEvidence([]store.Event{completedEvent, transfer}, delegation)
				if !gotCompleted || synchronized != tc.wantSync {
					t.Fatalf("completed=%v synchronized=%v want true,%v", gotCompleted, synchronized, tc.wantSync)
				}
			})
		}
	})
}

func TestTerminalDelegationExecutionSessionFailsClosedOnAmbiguousAuthority(t *testing.T) {
	runID := "parent-run"
	tests := []struct {
		name     string
		sessions []store.ExecutionSession
	}{
		{name: "no authoritative owner", sessions: []store.ExecutionSession{{ID: "one", RunID: runID, Status: "COMPLETED"}}},
		{name: "multiple terminal owners", sessions: []store.ExecutionSession{
			{ID: "one", RunID: runID, Status: "COMPLETED", RunnerID: "runner-1"},
			{ID: "two", RunID: runID, Status: "CANCELLED", RunnerID: "runner-2"},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if session, ok := terminalDelegatedExecutionSession(tc.sessions, runID); ok {
				t.Fatalf("ambiguous or ownerless sessions selected authority: %+v", session)
			}
		})
	}
}

func TestDelegationRecoveryContextRejectsIdentitySubstitution(t *testing.T) {
	storeFake, run, _, safe := delegationRecoveryFixture(t)
	processor := &Processor{store: storeFake}

	t.Run("malformed provenance", func(t *testing.T) {
		storeFake.provenance = json.RawMessage(`{"schemaVersion":`)
		if _, err := processor.delegationRecoveryContext(t.Context(), run); err == nil {
			t.Fatal("malformed provenance was accepted")
		}
	})

	for _, tc := range []struct {
		name   string
		mutate func(*executioncontext.SafeContext)
	}{
		{name: "project", mutate: func(value *executioncontext.SafeContext) { value.Project.ID = "other-project" }},
		{name: "run", mutate: func(value *executioncontext.SafeContext) { value.Run.ID = "other-run" }},
		{name: "issue", mutate: func(value *executioncontext.SafeContext) { value.Issue.ID = "other-issue" }},
		{name: "workspace", mutate: func(value *executioncontext.SafeContext) { value.Workspace.ID = "other-workspace" }},
		{name: "agent", mutate: func(value *executioncontext.SafeContext) { value.Agent.ID = "other-agent" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			substituted := safe
			tc.mutate(&substituted)
			encoded, err := json.Marshal(executioncontext.Provenance{
				SchemaVersion: executioncontext.ProvenanceSchemaVersion,
				Context: substituted,
			})
			if err != nil {
				t.Fatal(err)
			}
			storeFake.provenance = encoded
			if _, err := processor.delegationRecoveryContext(t.Context(), run); err == nil {
				t.Fatalf("%s substitution was accepted", tc.name)
			}
		})
	}

	t.Run("schema version", func(t *testing.T) {
		encoded, err := json.Marshal(executioncontext.Provenance{SchemaVersion: executioncontext.ProvenanceSchemaVersion + 1, Context: safe})
		if err != nil {
			t.Fatal(err)
		}
		storeFake.provenance = encoded
		if _, err := processor.delegationRecoveryContext(t.Context(), run); err == nil {
			t.Fatal("unsupported provenance schema was accepted")
		}
	})
}


func TestRecoverDelegationWorkspaceHandoffAcceptsCancelledServiceAfterConfirmedHandoff(t *testing.T) {
	storeFake, run, delegation, _ := delegationRecoveryFixture(t)
	storeFake.sessions = []store.ExecutionSession{{ID: "session-1", RunID: run.ID, Status: "CANCELLED", RunnerID: "runner-1"}}
	storeFake.events = []store.Event{
		delegationRecoveryEvent(t, run.ProjectID, run.ID, 1, "tool.completed", evidence.ToolPayload{
			Name: "delegate_task", ToolCallID: delegation.RequestKey,
			Input: map[string]any{"targetAgentId": delegation.TargetAgentID, "task": delegation.Task},
		}),
		delegationRecoveryEvent(t, run.ProjectID, run.ID, 2, "engine.execution.completed", map[string]any{"boundary": "delegation_handoff"}),
		delegationRecoveryEvent(t, run.ProjectID, run.ID, 3, "workspace.transfer.completed", map[string]any{"direction": "from_runner"}),
	}
	processor := &Processor{store: storeFake}
	if err := processor.recoverDelegationWorkspaceHandoff(t.Context(), run, storeFake.sessions); err != nil {
		t.Fatal(err)
	}
	if len(storeFake.marks) != 1 {
		t.Fatalf("handoff ready marks=%d want 1", len(storeFake.marks))
	}
}

func TestRecoverDelegationWorkspaceHandoffRejectsAmbiguousCancelledService(t *testing.T) {
	storeFake, run, delegation, _ := delegationRecoveryFixture(t)
	storeFake.sessions = []store.ExecutionSession{{ID: "session-1", RunID: run.ID, Status: "CANCELLED", RunnerID: "runner-1"}}
	storeFake.events = []store.Event{
		delegationRecoveryEvent(t, run.ProjectID, run.ID, 1, "tool.completed", evidence.ToolPayload{
			Name: "delegate_task", ToolCallID: delegation.RequestKey,
			Input: map[string]any{"targetAgentId": delegation.TargetAgentID, "task": delegation.Task},
		}),
		delegationRecoveryEvent(t, run.ProjectID, run.ID, 2, "workspace.transfer.completed", map[string]any{"direction": "from_runner"}),
	}
	processor := &Processor{store: storeFake}
	if err := processor.recoverDelegationWorkspaceHandoff(t.Context(), run, storeFake.sessions); err != nil {
		t.Fatal(err)
	}
	if len(storeFake.marks) != 0 {
		t.Fatalf("ambiguous CANCELLED service marked handoff ready %d times", len(storeFake.marks))
	}
}

func TestRecoverDelegationWorkspaceHandoffPropagatesDurableStoreFailures(t *testing.T) {
	sentinel := errors.New("durable store unavailable")

	t.Run("list delegations", func(t *testing.T) {
		storeFake, run, _, _ := delegationRecoveryFixture(t)
		storeFake.listDelegationsErr = sentinel
		processor := &Processor{store: storeFake}
		if err := processor.recoverDelegationWorkspaceHandoff(t.Context(), run, nil); !errors.Is(err, sentinel) {
			t.Fatalf("error=%v want sentinel", err)
		}
	})

	t.Run("list events", func(t *testing.T) {
		storeFake, run, _, _ := delegationRecoveryFixture(t)
		storeFake.listEventsErr = sentinel
		processor := &Processor{store: storeFake}
		if err := processor.recoverDelegationWorkspaceHandoff(t.Context(), run, nil); !errors.Is(err, sentinel) {
			t.Fatalf("error=%v want sentinel", err)
		}
	})

	t.Run("mark ready", func(t *testing.T) {
		storeFake, run, delegation, _ := delegationRecoveryFixture(t)
		storeFake.sessions = []store.ExecutionSession{{ID: "session-1", RunID: run.ID, Status: "COMPLETED", RunnerID: "runner-1"}}
		storeFake.events = []store.Event{
			delegationRecoveryEvent(t, run.ProjectID, run.ID, 1, "tool.completed", evidence.ToolPayload{
				Name: "delegate_task",
				ToolCallID: delegation.RequestKey,
				Input: map[string]any{"targetAgentId": delegation.TargetAgentID, "task": delegation.Task},
			}),
			delegationRecoveryEvent(t, run.ProjectID, run.ID, 2, "workspace.transfer.completed", map[string]any{"direction": "from_runner"}),
		}
		storeFake.markErr = sentinel
		processor := &Processor{store: storeFake}
		if err := processor.recoverDelegationWorkspaceHandoff(t.Context(), run, storeFake.sessions); !errors.Is(err, sentinel) {
			t.Fatalf("error=%v want sentinel", err)
		}
	})

	t.Run("no delegations", func(t *testing.T) {
		storeFake, run, _, _ := delegationRecoveryFixture(t)
		storeFake.delegations = nil
		processor := &Processor{store: storeFake}
		if err := processor.recoverDelegationWorkspaceHandoff(t.Context(), run, nil); err != nil {
			t.Fatalf("error=%v want nil", err)
		}
	})
}

func TestDelegationRecoveryEvidenceIgnoresMalformedAndUnsequencedCompletion(t *testing.T) {
	_, run, delegation, _ := delegationRecoveryFixture(t)
	runID := run.ID
	sequence := int64(2)
	events := []store.Event{
		{ProjectID: run.ProjectID, RunID: &runID, Type: "tool.completed", Payload: json.RawMessage(`{"name":`)},
		{ProjectID: run.ProjectID, RunID: &runID, Type: "tool.completed", Sequence: &sequence, Payload: json.RawMessage(`{"name":`)},
	}
	completed, synchronized := delegationRecoveryEvidence(events, delegation)
	if completed || synchronized {
		t.Fatalf("malformed/unsequenced evidence recovered handoff: completed=%v synchronized=%v", completed, synchronized)
	}
}

func TestDelegationRecoveryContextRejectsMissingProvenanceAndAgent(t *testing.T) {
	storeFake, run, _, safe := delegationRecoveryFixture(t)
	processor := &Processor{store: storeFake}

	if _, err := processor.delegationRecoveryContext(t.Context(), run); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing provenance error=%v want ErrNotFound", err)
	}

	encoded, err := json.Marshal(executioncontext.Provenance{
		SchemaVersion: executioncontext.ProvenanceSchemaVersion,
		Context: safe,
	})
	if err != nil {
		t.Fatal(err)
	}
	storeFake.provenance = encoded
	run.AgentID = nil
	if _, err := processor.delegationRecoveryContext(t.Context(), run); err == nil {
		t.Fatal("Run without authoritative Agent was accepted for recovery")
	}
}

func delegatedChildRecoveryFixture(t *testing.T) (*delegationRecoveryStore, store.Run, store.Delegation, executioncontext.SafeContext) {
	t.Helper()
	safe := processTestSafeContext("/workspace")
	safe.Run.ID = "delegated-run-1"
	safe.Delegation = &executioncontext.DelegationContext{
		ID:            "delegation-child-1",
		ParentRunID:   "parent-run-1",
		ParentAgentID: "parent-agent-1",
		TargetAgentID: safe.Agent.ID,
		Task:          "perform bounded delegated work",
		RequestKey:    "delegate-call-1",
	}
	agentID := safe.Agent.ID
	run := store.Run{
		ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID,
		WorkspaceID: safe.Workspace.ID, AgentID: &agentID, Status: "RUNNING",
	}
	delegation := store.Delegation{
		ID: safe.Delegation.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID,
		ParentRunID: safe.Delegation.ParentRunID, ParentAgentID: safe.Delegation.ParentAgentID,
		TargetAgentID: safe.Delegation.TargetAgentID, Task: safe.Delegation.Task,
		DelegatedRunID: run.ID, RequestKey: safe.Delegation.RequestKey,
	}
	return &delegationRecoveryStore{delegations: []store.Delegation{delegation}}, run, delegation, safe
}

func TestReconcileRecoversTerminalDelegatedChildWithoutEngineReplay(t *testing.T) {
	storeFake, run, delegation, _ := delegatedChildRecoveryFixture(t)
	storeFake.sessions = []store.ExecutionSession{{ID: "session-1", RunID: run.ID, Status: "COMPLETED", RunnerID: "runner-1"}}
	storeFake.events = []store.Event{
		delegationRecoveryEvent(t, run.ProjectID, run.ID, 1, "delegation.workspace_accepted", map[string]any{"delegationId": delegation.ID}),
	}
	processor := &Processor{store: storeFake, sessions: reconcileSessions{}}
	outcome, reason, err := processor.Reconcile(t.Context(), &store.SchedulerAdmission{Run: run})
	if err != nil {
		t.Fatal(err)
	}
	if outcome != store.SchedulerReconciliationCompleted || reason != nil {
		t.Fatalf("outcome=%s reason=%v want COMPLETED", outcome, reason)
	}
	outcome, reason, err = processor.Reconcile(t.Context(), &store.SchedulerAdmission{Run: run})
	if err != nil || outcome != store.SchedulerReconciliationCompleted || reason != nil {
		t.Fatalf("repeated outcome=%s reason=%v err=%v", outcome, reason, err)
	}
}

func TestReconcileRecoversCancelledOpenCodeServiceAsSuccessfulDelegatedExecution(t *testing.T) {
	storeFake, run, delegation, _ := delegatedChildRecoveryFixture(t)
	storeFake.sessions = []store.ExecutionSession{{ID: "session-1", RunID: run.ID, Status: "CANCELLED", RunnerID: "runner-1"}}
	storeFake.events = []store.Event{
		delegationRecoveryEvent(t, run.ProjectID, run.ID, 1, "engine.execution.completed", map[string]any{"boundary": "completed"}),
		delegationRecoveryEvent(t, run.ProjectID, run.ID, 2, "delegation.workspace_accepted", map[string]any{"delegationId": delegation.ID}),
	}
	processor := &Processor{store: storeFake, sessions: reconcileSessions{}}
	outcome, reason, err := processor.Reconcile(t.Context(), &store.SchedulerAdmission{Run: run})
	if err != nil || outcome != store.SchedulerReconciliationCompleted || reason != nil {
		t.Fatalf("outcome=%s reason=%v err=%v want COMPLETED", outcome, reason, err)
	}
}

func TestReconcileRecoversCancelledOpenCodeServiceWithDurableFailureAsFailed(t *testing.T) {
	storeFake, run, delegation, _ := delegatedChildRecoveryFixture(t)
	storeFake.sessions = []store.ExecutionSession{{ID: "session-1", RunID: run.ID, Status: "CANCELLED", RunnerID: "runner-1"}}
	storeFake.events = []store.Event{
		delegationRecoveryEvent(t, run.ProjectID, run.ID, 1, "run.failed", map[string]any{"reason": "native execution failed"}),
		delegationRecoveryEvent(t, run.ProjectID, run.ID, 2, "delegation.workspace_accepted", map[string]any{"delegationId": delegation.ID}),
	}
	processor := &Processor{store: storeFake, sessions: reconcileSessions{}}
	outcome, reason, err := processor.Reconcile(t.Context(), &store.SchedulerAdmission{Run: run})
	if err != nil || outcome != store.SchedulerReconciliationFailed || reason == nil || *reason != "native execution failed" {
		t.Fatalf("outcome=%s reason=%v err=%v want FAILED with durable reason", outcome, reason, err)
	}
}

func TestReconcileLeavesAmbiguousCancelledDelegatedExecutionUnknown(t *testing.T) {
	storeFake, run, delegation, _ := delegatedChildRecoveryFixture(t)
	storeFake.sessions = []store.ExecutionSession{{ID: "session-1", RunID: run.ID, Status: "CANCELLED", RunnerID: "runner-1"}}
	storeFake.events = []store.Event{
		delegationRecoveryEvent(t, run.ProjectID, run.ID, 1, "delegation.workspace_accepted", map[string]any{"delegationId": delegation.ID}),
	}
	processor := &Processor{store: storeFake, sessions: reconcileSessions{}}
	outcome, reason, err := processor.Reconcile(t.Context(), &store.SchedulerAdmission{Run: run})
	if err != nil || outcome != store.SchedulerReconciliationUnknown || reason != nil {
		t.Fatalf("outcome=%s reason=%v err=%v want UNKNOWN", outcome, reason, err)
	}
}

func TestReconcileRejectsDelegatedChildTargetAgentLineageMismatch(t *testing.T) {
	storeFake, run, delegation, _ := delegatedChildRecoveryFixture(t)
	wrongAgent := "other-agent"
	run.AgentID = &wrongAgent
	storeFake.sessions = []store.ExecutionSession{{ID: "session-1", RunID: run.ID, Status: "COMPLETED", RunnerID: "runner-1"}}
	storeFake.events = []store.Event{
		delegationRecoveryEvent(t, run.ProjectID, run.ID, 1, "delegation.workspace_accepted", map[string]any{"delegationId": delegation.ID}),
	}
	processor := &Processor{store: storeFake, sessions: reconcileSessions{}}

	outcome, reason, err := processor.Reconcile(t.Context(), &store.SchedulerAdmission{Run: run})
	if outcome != store.SchedulerReconciliationUnknown || reason != nil || err == nil {
		t.Fatalf("outcome=%s reason=%v err=%v want UNKNOWN target-Agent lineage error", outcome, reason, err)
	}
}

func TestReconcileRecoversFailedAndCancelledDelegatedChildren(t *testing.T) {
	tests := []struct {
		name string
		session string
		want store.SchedulerReconciliationOutcome
		wantReason string
	}{
		{name: "failed", session: "FAILED", want: store.SchedulerReconciliationFailed, wantReason: "delegate failed safely"},
		{name: "cancelled", session: "CANCELLED", want: store.SchedulerReconciliationCancelled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storeFake, run, _, _ := delegatedChildRecoveryFixture(t)
			storeFake.sessions = []store.ExecutionSession{{ID: "session-1", RunID: run.ID, Status: tt.session, RunnerID: "runner-1"}}
			sequence := int64(1)
			if tt.wantReason != "" {
				storeFake.events = append(storeFake.events, delegationRecoveryEvent(t, run.ProjectID, run.ID, sequence, "run.failed", map[string]any{"reason": tt.wantReason}))
				sequence++
			}
			if tt.session == "CANCELLED" {
				storeFake.events = append(storeFake.events, delegationRecoveryEvent(t, run.ProjectID, run.ID, sequence, "run.cancellation_requested", map[string]any{"source": "run_cancel"}))
				sequence++
			}
			storeFake.events = append(storeFake.events, delegationRecoveryEvent(t, run.ProjectID, run.ID, sequence, "workspace.transfer.completed", map[string]any{"direction": "from_runner"}))
			processor := &Processor{store: storeFake, sessions: reconcileSessions{}}
			outcome, reason, err := processor.Reconcile(t.Context(), &store.SchedulerAdmission{Run: run})
			if err != nil {
				t.Fatal(err)
			}
			if outcome != tt.want {
				t.Fatalf("outcome=%s want %s", outcome, tt.want)
			}
			if tt.wantReason == "" {
				if reason != nil {
					t.Fatalf("reason=%v want nil", reason)
				}
			} else if reason == nil || *reason != tt.wantReason {
				t.Fatalf("reason=%v want %q", reason, tt.wantReason)
			}
		})
	}
}

func TestReconcileDelegatedChildRemainsUnknownWhenWorkspaceHandbackCannotBeVerified(t *testing.T) {
	storeFake, run, _, safe := delegatedChildRecoveryFixture(t)
	storeFake.sessions = []store.ExecutionSession{{ID: "session-1", RunID: run.ID, Status: "COMPLETED", RunnerID: "runner-1"}}
	provenance, err := json.Marshal(executioncontext.Provenance{SchemaVersion: executioncontext.ProvenanceSchemaVersion, Context: safe})
	if err != nil {
		t.Fatal(err)
	}
	storeFake.provenance = provenance
	processor := &Processor{store: storeFake, sessions: reconcileSessions{}}
	outcome, reason, err := processor.Reconcile(t.Context(), &store.SchedulerAdmission{Run: run})
	if outcome != store.SchedulerReconciliationUnknown || reason != nil || err == nil {
		t.Fatalf("outcome=%s reason=%v err=%v want UNKNOWN with recovery error", outcome, reason, err)
	}
}

func TestReconcileRecoversDelegatedChildFromRetainedRunnerWorkspace(t *testing.T) {
	repository := initProcessTestRepository(t)
	storeFake, run, delegation, safe := delegatedChildRecoveryFixture(t)
	safe.Workspace.Path = repository
	storeFake.sessions = []store.ExecutionSession{{ID: "session-1", RunID: run.ID, Status: "COMPLETED", RunnerID: "runner-1"}}
	provenance, err := json.Marshal(executioncontext.Provenance{SchemaVersion: executioncontext.ProvenanceSchemaVersion, Context: safe})
	if err != nil {
		t.Fatal(err)
	}
	storeFake.provenance = provenance
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	client := &successfulSyncClient{payload: runnerTransferPayload(t, repository)}
	recorder, err := evidence.NewRecorder(storeFake, nil)
	if err != nil {
		t.Fatal(err)
	}
	processor := &Processor{store: storeFake, sessions: reconcileSessions{}, git: git, events: recorder, runners: runnerSyncConnector{client: client}}
	outcome, reason, err := processor.Reconcile(t.Context(), &store.SchedulerAdmission{Run: run})
	if err != nil {
		t.Fatal(err)
	}
	if outcome != store.SchedulerReconciliationCompleted || reason != nil {
		t.Fatalf("outcome=%s reason=%v want COMPLETED", outcome, reason)
	}
	if !client.confirmed || len(client.directions) != 1 || client.directions[0] != "from_runner" {
		t.Fatalf("runner handback confirmed=%v directions=%v", client.confirmed, client.directions)
	}
	if !delegationWorkspaceAcceptedEvidence(storeFake.events, delegation.ID) {
		t.Fatalf("delegation Workspace acceptance was not persisted: %+v", storeFake.events)
	}
}

type terminalParentDelegationRecoveryStore struct {
	*delegationRecoveryStore
	parent store.Run
}

func (s *terminalParentDelegationRecoveryStore) GetRun(_ context.Context, projectID, runID string) (store.Run, error) {
	if s.parent.ProjectID == projectID && s.parent.ID == runID {
		return s.parent, nil
	}
	return store.Run{}, store.ErrNotFound
}

type cancellingReconcileSessions struct {
	reconcileSessions
	store *delegationRecoveryStore
	err   error
	calls int
}

func (s *cancellingReconcileSessions) Cancel(_ context.Context, projectID, sessionID string, _ time.Duration) error {
	s.calls++
	if s.err != nil {
		return s.err
	}
	for index := range s.store.sessions {
		if s.store.sessions[index].ID == sessionID {
			s.store.sessions[index].Status = "CANCELLED"
			return nil
		}
	}
	return store.ErrNotFound
}

func terminalParentDelegatedChildFixture(t *testing.T) (*terminalParentDelegationRecoveryStore, store.Run, store.Delegation) {
	t.Helper()
	base, child, delegation, _ := delegatedChildRecoveryFixture(t)
	parentAgentID := delegation.ParentAgentID
	wrapped := &terminalParentDelegationRecoveryStore{
		delegationRecoveryStore: base,
		parent: store.Run{
			ID: delegation.ParentRunID, ProjectID: child.ProjectID, IssueID: child.IssueID,
			WorkspaceID: child.WorkspaceID, AgentID: &parentAgentID, Status: "CANCELLED",
		},
	}
	return wrapped, child, delegation
}

func TestReconcileCancelsRunningDelegatedChildWhenParentIsDurablyTerminal(t *testing.T) {
	storeFake, child, delegation := terminalParentDelegatedChildFixture(t)
	storeFake.sessions = []store.ExecutionSession{{ID: "session-1", RunID: child.ID, Status: "RUNNING", RunnerID: "runner-1"}}
	storeFake.events = []store.Event{
		delegationRecoveryEvent(t, child.ProjectID, child.ID, 1, "workspace.transfer.completed", map[string]any{"direction": "from_runner"}),
	}
	sessions := &cancellingReconcileSessions{store: storeFake.delegationRecoveryStore}
	processor := &Processor{store: storeFake, sessions: sessions}

	outcome, reason, err := processor.Reconcile(t.Context(), &store.SchedulerAdmission{Run: child})
	if err != nil {
		t.Fatal(err)
	}
	if outcome != store.SchedulerReconciliationCancelled || reason != nil {
		t.Fatalf("outcome=%s reason=%v want CANCELLED", outcome, reason)
	}
	if sessions.calls != 1 || storeFake.sessions[0].Status != "CANCELLED" {
		t.Fatalf("cancel calls=%d sessions=%+v", sessions.calls, storeFake.sessions)
	}
	if delegation.ID == "" {
		t.Fatal("delegation fixture unexpectedly empty")
	}
}

func TestReconcileKeepsRunningDelegatedChildFencedWhenRestartCancellationIsUncertain(t *testing.T) {
	storeFake, child, _ := terminalParentDelegatedChildFixture(t)
	storeFake.sessions = []store.ExecutionSession{{ID: "session-1", RunID: child.ID, Status: "RUNNING", RunnerID: "runner-1"}}
	sentinel := errors.New("runner disconnected")
	sessions := &cancellingReconcileSessions{store: storeFake.delegationRecoveryStore, err: sentinel}
	processor := &Processor{store: storeFake, sessions: sessions}

	outcome, reason, err := processor.Reconcile(t.Context(), &store.SchedulerAdmission{Run: child})
	if outcome != store.SchedulerReconciliationUnknown || reason != nil || !errors.Is(err, sentinel) {
		t.Fatalf("outcome=%s reason=%v err=%v want UNKNOWN sentinel", outcome, reason, err)
	}
	if sessions.calls != 1 || storeFake.sessions[0].Status != "RUNNING" {
		t.Fatalf("uncertain cancellation calls=%d sessions=%+v", sessions.calls, storeFake.sessions)
	}
}

func TestReconcileCancelsUnstartedDelegatedChildWhenParentIsDurablyTerminal(t *testing.T) {
	storeFake, child, _ := terminalParentDelegatedChildFixture(t)
	processor := &Processor{store: storeFake, sessions: reconcileSessions{}}

	outcome, reason, err := processor.Reconcile(t.Context(), &store.SchedulerAdmission{Run: child})
	if err != nil {
		t.Fatal(err)
	}
	if outcome != store.SchedulerReconciliationCancelled || reason != nil {
		t.Fatalf("outcome=%s reason=%v want CANCELLED", outcome, reason)
	}
}

func TestReconcileCancelledDelegatedChildRefinalizesDurableLocalWorkspaceBeforeTerminalizing(t *testing.T) {
	storeFake, run, delegation, safe := delegatedChildRecoveryFixture(t)
	storeFake.sessions = []store.ExecutionSession{{ID: "session-1", RunID: run.ID, Status: "CANCELLED", RuntimeInstanceID: "runtime-instance-1"}}
	storeFake.current = "accepted-sha"
	provenance, err := json.Marshal(executioncontext.Provenance{SchemaVersion: executioncontext.ProvenanceSchemaVersion, Context: safe})
	if err != nil {
		t.Fatal(err)
	}
	storeFake.provenance = provenance
	git := &serverFinalizeGit{revision: "accepted-sha"}
	recorder, err := evidence.NewRecorder(storeFake, nil)
	if err != nil {
		t.Fatal(err)
	}
	processor := &Processor{store: storeFake, sessions: reconcileSessions{}, git: git, events: recorder}

	outcome, reason, err := processor.Reconcile(t.Context(), &store.SchedulerAdmission{Run: run})
	if err != nil {
		t.Fatal(err)
	}
	if outcome != store.SchedulerReconciliationCancelled || reason != nil {
		t.Fatalf("outcome=%s reason=%v want CANCELLED", outcome, reason)
	}
	if git.start != "accepted-sha" || storeFake.persisted != "accepted-sha" {
		t.Fatalf("re-finalized start=%q persisted=%q", git.start, storeFake.persisted)
	}
	if !delegationWorkspaceAcceptedEvidence(storeFake.events, delegation.ID) {
		t.Fatalf("local handback was not durably marked accepted: %+v", storeFake.events)
	}
}

func TestReconcileDelegatedChildWithEligibleParentKeepsNormalActiveExecution(t *testing.T) {
	storeFake, child, _ := terminalParentDelegatedChildFixture(t)
	storeFake.parent.Status = "PAUSED"
	storeFake.sessions = []store.ExecutionSession{{ID: "session-1", RunID: child.ID, Status: "RUNNING", RunnerID: "runner-1"}}
	processor := &Processor{store: storeFake, sessions: reconcileSessions{}}

	outcome, reason, err := processor.Reconcile(t.Context(), &store.SchedulerAdmission{Run: child})
	if err != nil {
		t.Fatal(err)
	}
	if outcome != store.SchedulerReconciliationActive || reason != nil {
		t.Fatalf("outcome=%s reason=%v want ACTIVE", outcome, reason)
	}
}

func TestReconcileDelegatedChildRejectsMismatchedParentLineage(t *testing.T) {
	storeFake, child, _ := terminalParentDelegatedChildFixture(t)
	storeFake.parent.WorkspaceID = "different-workspace"
	processor := &Processor{store: storeFake, sessions: reconcileSessions{}}

	outcome, reason, err := processor.Reconcile(t.Context(), &store.SchedulerAdmission{Run: child})
	if outcome != store.SchedulerReconciliationUnknown || reason != nil || err == nil {
		t.Fatalf("outcome=%s reason=%v err=%v want UNKNOWN lineage error", outcome, reason, err)
	}
}

func TestReconcileTerminalParentRunningDelegateRequiresCancellationCapability(t *testing.T) {
	storeFake, child, _ := terminalParentDelegatedChildFixture(t)
	storeFake.sessions = []store.ExecutionSession{{ID: "session-1", RunID: child.ID, Status: "RUNNING", RunnerID: "runner-1"}}
	processor := &Processor{store: storeFake, sessions: reconcileSessions{}}

	outcome, reason, err := processor.Reconcile(t.Context(), &store.SchedulerAdmission{Run: child})
	if outcome != store.SchedulerReconciliationUnknown || reason != nil || err == nil {
		t.Fatalf("outcome=%s reason=%v err=%v want UNKNOWN cancellation capability error", outcome, reason, err)
	}
}

func TestReconcileTerminalParentMultipleLiveDelegateSessionsStaysUnknown(t *testing.T) {
	storeFake, child, _ := terminalParentDelegatedChildFixture(t)
	storeFake.sessions = []store.ExecutionSession{
		{ID: "session-1", RunID: child.ID, Status: "RUNNING", RunnerID: "runner-1"},
		{ID: "session-2", RunID: child.ID, Status: "STARTING", RunnerID: "runner-1"},
	}
	sessions := &cancellingReconcileSessions{store: storeFake.delegationRecoveryStore}
	processor := &Processor{store: storeFake, sessions: sessions}

	outcome, reason, err := processor.Reconcile(t.Context(), &store.SchedulerAdmission{Run: child})
	if err != nil {
		t.Fatal(err)
	}
	if outcome != store.SchedulerReconciliationUnknown || reason != nil || sessions.calls != 0 {
		t.Fatalf("outcome=%s reason=%v cancelCalls=%d want UNKNOWN without cancellation", outcome, reason, sessions.calls)
	}
}
