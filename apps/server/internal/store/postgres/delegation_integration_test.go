package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type delegationFixture struct {
	store     *Store
	project   store.Project
	issue     store.Issue
	parent    store.Agent
	target    store.Agent
	parentRun store.Run
	model     store.ModelProfile
	provider  store.Provider
}

func TestRequestDelegationCreatesOneNormalRunAndIsIdempotent(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	input := store.RequestDelegationCommand{
		ProjectID: f.project.ID, ParentRunID: f.parentRun.ID, TargetAgentID: f.target.ID,
		Task: "Inspect the current implementation and report the bounded finding.", RequestKey: "tool-part-1",
	}
	first, err := f.store.RequestDelegation(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Delegation.ParentRunID != f.parentRun.ID || first.Delegation.ParentAgentID != f.parent.ID || first.Delegation.TargetAgentID != f.target.ID {
		t.Fatalf("unexpected lineage: %+v", first.Delegation)
	}
	if first.DelegatedRun.Status != "QUEUED" || first.DelegatedRun.Attempt != 2 || first.DelegatedRun.WorkspaceID != f.parentRun.WorkspaceID || first.DelegatedRun.AgentID == nil || *first.DelegatedRun.AgentID != f.target.ID {
		t.Fatalf("unexpected delegated Run: %+v", first.DelegatedRun)
	}
	if first.SchedulerJob.Kind != "START" || first.SchedulerJob.RunID != first.DelegatedRun.ID {
		t.Fatalf("unexpected scheduler job: %+v", first.SchedulerJob)
	}
	if first.Delegation.Outcome != nil || first.Delegation.ResultSummary != nil || first.Delegation.ResultEventID != nil || first.Delegation.WorkspaceChangesAccepted != nil || first.Delegation.ContinuationJobID != nil || first.Delegation.CompletedAt != nil {
		t.Fatalf("new delegation unexpectedly has terminal result state: %+v", first.Delegation)
	}

	retry, err := f.store.RequestDelegation(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if retry.Delegation.ID != first.Delegation.ID || retry.DelegatedRun.ID != first.DelegatedRun.ID || retry.SchedulerJob.ID != first.SchedulerJob.ID {
		t.Fatalf("idempotent retry created different state: first=%+v retry=%+v", first, retry)
	}
	runs, err := f.store.ListRuns(ctx, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("Run count=%d want 2", len(runs))
	}
	lineage, err := f.store.GetDelegationByRun(ctx, f.project.ID, first.DelegatedRun.ID)
	if err != nil || lineage.ID != first.Delegation.ID {
		t.Fatalf("delegated Run lineage=%+v err=%v", lineage, err)
	}
	children, err := f.store.ListDelegationsByParentRun(ctx, f.project.ID, f.parentRun.ID)
	if err != nil || len(children) != 1 || children[0].ID != first.Delegation.ID {
		t.Fatalf("parent delegations=%+v err=%v", children, err)
	}
	issue, err := f.store.GetIssue(ctx, f.project.ID, f.issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if issue.Status != f.issue.Status || issue.AssigneeType == nil || *issue.AssigneeType != "AGENT" || issue.AssigneeID == nil || *issue.AssigneeID != f.parent.ID {
		t.Fatalf("delegation changed Issue authority: before=%+v after=%+v", f.issue, issue)
	}
}

func TestRequestDelegationRejectsPolicySelfActiveAndChangedRetry(t *testing.T) {
	t.Run("policy disabled", func(t *testing.T) {
		f := newDelegationFixture(t, false)
		_, err := f.store.RequestDelegation(t.Context(), store.RequestDelegationCommand{
			ProjectID: f.project.ID, ParentRunID: f.parentRun.ID, TargetAgentID: f.target.ID, Task: "task", RequestKey: "one",
		})
		if !errors.Is(err, store.ErrConflict) {
			t.Fatalf("err=%v want conflict", err)
		}
	})

	t.Run("self target", func(t *testing.T) {
		f := newDelegationFixture(t, true)
		_, err := f.store.RequestDelegation(t.Context(), store.RequestDelegationCommand{
			ProjectID: f.project.ID, ParentRunID: f.parentRun.ID, TargetAgentID: f.parent.ID, Task: "task", RequestKey: "one",
		})
		if !errors.Is(err, store.ErrConflict) {
			t.Fatalf("err=%v want conflict", err)
		}
	})

	t.Run("changed retry and active duplicate", func(t *testing.T) {
		f := newDelegationFixture(t, true)
		ctx := t.Context()
		base := store.RequestDelegationCommand{ProjectID: f.project.ID, ParentRunID: f.parentRun.ID, TargetAgentID: f.target.ID, Task: "task", RequestKey: "one"}
		if _, err := f.store.RequestDelegation(ctx, base); err != nil {
			t.Fatal(err)
		}
		changed := base
		changed.Task = "different task"
		if _, err := f.store.RequestDelegation(ctx, changed); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("changed retry err=%v want conflict", err)
		}
		active := base
		active.RequestKey = "two"
		if _, err := f.store.RequestDelegation(ctx, active); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("active duplicate err=%v want conflict", err)
		}
	})
}

func TestRequestDelegationRejectsCrossProjectAndNestedDelegation(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	otherProject, err := f.store.CreateProject(ctx, testProjectInput("Other delegation project", "/repos/delegation-other", "DO"))
	if err != nil {
		t.Fatal(err)
	}
	otherTarget := createDelegationAgent(t, ctx, f.store, otherProject, "Other target", false)
	if _, err := f.store.RequestDelegation(ctx, store.RequestDelegationCommand{
		ProjectID: f.project.ID, ParentRunID: f.parentRun.ID, TargetAgentID: otherTarget.ID, Task: "task", RequestKey: "cross",
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("cross-project err=%v want conflict", err)
	}

	first, err := f.store.RequestDelegation(ctx, store.RequestDelegationCommand{
		ProjectID: f.project.ID, ParentRunID: f.parentRun.ID, TargetAgentID: f.target.ID, Task: "first", RequestKey: "first",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.pool.Exec(ctx, `UPDATE runs SET status='RUNNING', started_at=now() WHERE project_id=$1 AND id=$2`, f.project.ID, first.DelegatedRun.ID); err != nil {
		t.Fatal(err)
	}
	third := createDelegationAgent(t, ctx, f.store, f.project, "Third agent", true)
	if _, err := f.store.RequestDelegation(ctx, store.RequestDelegationCommand{
		ProjectID: f.project.ID, ParentRunID: first.DelegatedRun.ID, TargetAgentID: third.ID, Task: "nested", RequestKey: "nested",
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("nested delegation err=%v want conflict", err)
	}
}

func newDelegationFixture(t *testing.T, allowDelegation bool) delegationFixture {
	t.Helper()
	s := New(testPool(t))
	ctx := context.Background()
	project, err := s.CreateProject(ctx, testProjectInput("Delegation Project", "/repos/delegation", "DG"))
	if err != nil {
		t.Fatal(err)
	}
	parent := createDelegationAgent(t, ctx, s, project, "Parent agent", allowDelegation)
	target := createDelegationAgent(t, ctx, s, project, "Target agent", false)
	issue, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "Delegation issue", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}
	assignment, err := s.SetIssueAssignee(ctx, project.ID, issue.ID, &store.Assignee{Type: "AGENT", ID: parent.ID}, store.EmptyObject)
	if err != nil {
		t.Fatal(err)
	}
	runs, err := s.ListRuns(ctx, project.ID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("parent Runs=%d err=%v", len(runs), err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE runs SET status='RUNNING', started_at=now(), updated_at=now() WHERE project_id=$1 AND id=$2`, project.ID, runs[0].ID); err != nil {
		t.Fatal(err)
	}
	parentRun, err := s.GetRun(ctx, project.ID, runs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	return delegationFixture{store: s, project: project, issue: assignment.Issue, parent: parent, target: target, parentRun: parentRun}
}

func createDelegationAgent(t *testing.T, ctx context.Context, s *Store, project store.Project, name string, allowDelegation bool) store.Agent {
	t.Helper()
	scope := project.ID
	provider, err := s.CreateProvider(ctx, store.Provider{ProjectID: &scope, Name: name + " Provider", Kind: "test", Enabled: true, HealthStatus: "HEALTHY", SafeMetadata: store.EmptyObject})
	if err != nil {
		t.Fatal(err)
	}
	model, err := s.CreateModelProfile(ctx, store.ModelProfile{ProjectID: &scope, ProviderID: provider.ID, Name: name + " Model", Model: "test-model", GenerationSettings: store.EmptyObject, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := s.CreateAgent(ctx, store.Agent{ProjectID: &scope, Name: name, Engine: "scripted", ModelProfileID: model.ID, EngineSettings: store.EmptyObject, ConcurrencyLimit: 1, AllowDelegation: allowDelegation, State: "ENABLED"})
	if err != nil {
		t.Fatal(err)
	}
	return agent
}
