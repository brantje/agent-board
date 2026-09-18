package runexec

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type delegationRecoveryStore struct {
	processTestStore
	delegations []store.Delegation
	events      []store.Event
	marks       []delegationHandoffMark
	current     string
	persisted   string
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
	var result []store.Delegation
	for _, delegation := range s.delegations {
		if delegation.ProjectID == projectID && delegation.ParentRunID == parentRunID {
			result = append(result, delegation)
		}
	}
	return result, nil
}

func (s *delegationRecoveryStore) MarkDelegationWorkspaceHandoffReady(_ context.Context, projectID, parentRunID, delegationID, delegatedRunID string) error {
	for _, mark := range s.marks {
		if mark.projectID == projectID && mark.parentRunID == parentRunID && mark.delegationID == delegationID && mark.delegatedRunID == delegatedRunID {
			return nil
		}
	}
	s.marks = append(s.marks, delegationHandoffMark{projectID: projectID, parentRunID: parentRunID, delegationID: delegationID, delegatedRunID: delegatedRunID})
	return nil
}

func (s *delegationRecoveryStore) ListRunEvents(_ context.Context, projectID, runID string, afterSequence int64, limit int) ([]store.Event, error) {
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
		name     string
		status   string
		runnerID string
	}{
		{name: "no completed tool", status: "COMPLETED", runnerID: "runner-1"},
		{name: "failed execution", status: "FAILED", runnerID: "runner-1"},
		{name: "cancelled execution", status: "CANCELLED", runnerID: "runner-1"},
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
