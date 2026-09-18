package runexec

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/engine/opencode"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestOpenCodeDockerOpenRouterDelegationEndToEnd(t *testing.T) {
	fixture := newOpenCodeIntegrationFixture(t)
	if !strings.EqualFold(fixture.env.providerKind, "openrouter") {
		t.Skip("OpenRouter provider is required for the delegation E2E")
	}

	project, parentRun := fixture.createRun(t, openCodeRunSpec{
		roleInstructions: "Follow the issue instructions exactly. Do not change Issue status. Do not edit files before the requested delegation.",
		title:            "Prove OpenCode delegation",
		description:      "Use the delegation capability exactly as directed by your role instructions. Do not perform the delegated file task yourself.",
	})
	if parentRun.AgentID == nil {
		t.Fatal("parent Run has no Agent")
	}
	scope := project.ID
	parentAgent, err := fixture.services.ControlPlane.GetAgent(fixture.ctx, &scope, *parentRun.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	target, err := fixture.services.ControlPlane.CreateAgent(fixture.ctx, store.Agent{
		ProjectID: &scope,
		Name: "OpenCode delegation target",
		RoleInstructions: "Perform only the bounded delegated task. Do not attempt to change Issue status or delegate further.",
		Engine: opencode.Name,
		ModelProfileID: parentAgent.ModelProfileID,
		EngineSettings: store.EmptyObject,
		ConcurrencyLimit: 1,
		State: "ENABLED",
	})
	if err != nil {
		t.Fatal(err)
	}
	parentAgent.AllowDelegation = true
	parentAgent.RoleInstructions = "Immediately call delegate_task with targetAgentId " + target.ID + " and task 'Create delegated-result.txt containing exactly delegated-run-ok with no trailing newline. Do not modify any other file.' Do not change Issue status or perform that file task yourself. After the delegation tool completes, stop; do not ask a Question or perform additional work."
	if _, err := fixture.services.ControlPlane.UpdateAgent(fixture.ctx, &scope, parentAgent); err != nil {
		t.Fatal(err)
	}

	fixture.startScheduler(t)
	delegation := waitForOpenCodeDelegation(t, fixture, project.ID, parentRun.ID)
	if delegation.ParentAgentID != parentAgent.ID || delegation.TargetAgentID != target.ID || delegation.DelegatedRunID == "" {
		t.Fatalf("delegation=%+v", delegation)
	}
	parentPaused := waitForOpenCodeRunStatus(t, fixture, project.ID, parentRun.ID, "PAUSED")
	assertOpenCodeDelegationToolEvidence(t, fixture, project.ID, parentRun.ID)
	if parentPaused.WorkspaceID != parentRun.WorkspaceID {
		t.Fatalf("paused parent Workspace=%s want %s", parentPaused.WorkspaceID, parentRun.WorkspaceID)
	}

	beforeChild, err := fixture.services.ControlPlane.GetIssue(fixture.ctx, project.ID, parentRun.IssueID)
	if err != nil {
		t.Fatal(err)
	}
	owner := beforeChild.AssignedTo()
	if owner == nil || owner.Type != "AGENT" || owner.ID != parentAgent.ID {
		t.Fatalf("delegation changed Issue owner before child execution: %+v", owner)
	}
	statusBeforeChild := beforeChild.Status

	child := waitForScriptedRun(t, fixture.ctx, fixture.database, project.ID, delegation.DelegatedRunID)
	if child.Status != "READY_FOR_REVIEW" {
		t.Fatalf("delegated Run status=%s failure=%q", child.Status, openCodeFailureReason(child.FailureReason))
	}
	if child.AgentID == nil || *child.AgentID != target.ID {
		t.Fatalf("delegated Run Agent=%v want %s", child.AgentID, target.ID)
	}
	if child.WorkspaceID != parentRun.WorkspaceID {
		t.Fatalf("delegated Run workspace=%s want parent workspace %s", child.WorkspaceID, parentRun.WorkspaceID)
	}
	assertOpenCodeWorkspaceFile(t, fixture.ctx, fixture.database, project.ID, child.WorkspaceID, "delegated-result.txt", "delegated-run-ok")
	assertOpenCodeServerSession(t, fixture.ctx, fixture.database, project.ID, child.ID, false)
	assertOpenCodeDelegationHandoffOrder(t, fixture, project.ID, parentRun.ID, child.ID)

	persisted, err := fixture.database.GetDelegationByRun(fixture.ctx, project.ID, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ID != delegation.ID || persisted.ParentRunID != parentRun.ID || persisted.Task != delegation.Task {
		t.Fatalf("persisted child lineage=%+v want %+v", persisted, delegation)
	}
	delegations, err := fixture.database.ListDelegationsByParentRun(fixture.ctx, project.ID, parentRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(delegations) != 1 || delegations[0].DelegatedRunID != child.ID {
		t.Fatalf("delegation replay multiplied records: %+v", delegations)
	}
	runs, err := fixture.services.ControlPlane.ListRuns(fixture.ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	children := 0
	for _, run := range runs {
		if run.AgentID != nil && *run.AgentID == target.ID && run.IssueID == parentRun.IssueID {
			children++
		}
	}
	if children != 1 {
		t.Fatalf("target ordinary Run count=%d want 1", children)
	}

	afterChild, err := fixture.services.ControlPlane.GetIssue(fixture.ctx, project.ID, parentRun.IssueID)
	if err != nil {
		t.Fatal(err)
	}
	afterOwner := afterChild.AssignedTo()
	if afterOwner == nil || afterOwner.Type != "AGENT" || afterOwner.ID != parentAgent.ID {
		t.Fatalf("delegated execution changed Issue owner: %+v", afterOwner)
	}
	if afterChild.Status != statusBeforeChild {
		t.Fatalf("delegated execution changed Issue status from %s to %s", statusBeforeChild, afterChild.Status)
	}
	assertNoAuthoritativeDelegationTools(t, fixture, project.ID, child.ID)
}

func waitForOpenCodeDelegation(t *testing.T, fixture *openCodeIntegrationFixture, projectID, parentRunID string) store.Delegation {
	t.Helper()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		values, err := fixture.database.ListDelegationsByParentRun(fixture.ctx, projectID, parentRunID)
		if err != nil {
			t.Fatal(err)
		}
		if len(values) == 1 {
			return values[0]
		}
		if len(values) > 1 {
			t.Fatalf("parent produced multiple delegation records: %+v", values)
		}
		parent, err := fixture.database.GetRun(fixture.ctx, projectID, parentRunID)
		if err != nil {
			t.Fatal(err)
		}
		if parent.Status == "FAILED" || parent.Status == "CANCELLED" || parent.Status == "READY_FOR_REVIEW" {
			t.Fatalf("parent terminated before delegation: status=%s failure=%q", parent.Status, openCodeFailureReason(parent.FailureReason))
		}
		select {
		case <-fixture.ctx.Done():
			t.Fatalf("timed out waiting for OpenCode delegation: %v", fixture.ctx.Err())
		case <-ticker.C:
		}
	}
}

func waitForOpenCodeRunStatus(t *testing.T, fixture *openCodeIntegrationFixture, projectID, runID, status string) store.Run {
	t.Helper()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		run, err := fixture.database.GetRun(fixture.ctx, projectID, runID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status == status {
			return run
		}
		select {
		case <-fixture.ctx.Done():
			t.Fatalf("timed out waiting for Run %s status %s: current=%s", runID, status, run.Status)
		case <-ticker.C:
		}
	}
}

func assertOpenCodeDelegationToolEvidence(t *testing.T, fixture *openCodeIntegrationFixture, projectID, runID string) {
	t.Helper()
	events, err := fixture.database.ListRunEvents(fixture.ctx, projectID, runID, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Type != "tool.completed" {
			continue
		}
		var payload struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(event.Payload, &payload) == nil && payload.Name == "delegate_task" {
			return
		}
	}
	t.Fatalf("parent Run has no durable delegate_task completion evidence: %v", eventTypes(events))
}

func assertOpenCodeDelegationHandoffOrder(t *testing.T, fixture *openCodeIntegrationFixture, projectID, parentRunID, childRunID string) {
	t.Helper()
	parentEvents, err := fixture.database.ListRunEvents(fixture.ctx, projectID, parentRunID, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	var delegateCompleted, workspaceReturned, paused *store.Event
	for index := range parentEvents {
		event := &parentEvents[index]
		switch event.Type {
		case "tool.completed":
			var payload struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(event.Payload, &payload) == nil && payload.Name == "delegate_task" {
				delegateCompleted = event
			}
		case "workspace.transfer.completed":
			var payload struct {
				Direction string `json:"direction"`
			}
			if json.Unmarshal(event.Payload, &payload) == nil && (payload.Direction == "from_runner" || payload.Direction == "git_publish") {
				workspaceReturned = event
			}
		case "run.paused":
			paused = event
		}
	}
	if delegateCompleted == nil || workspaceReturned == nil || paused == nil ||
		delegateCompleted.Sequence == nil || workspaceReturned.Sequence == nil || paused.Sequence == nil {
		t.Fatalf("missing durable phase-2 handoff evidence: %v", eventTypes(parentEvents))
	}
	if !(*delegateCompleted.Sequence < *workspaceReturned.Sequence && *workspaceReturned.Sequence < *paused.Sequence) {
		t.Fatalf(
			"phase-2 parent event order delegate=%d workspace=%d paused=%d",
			*delegateCompleted.Sequence,
			*workspaceReturned.Sequence,
			*paused.Sequence,
		)
	}

	childEvents, err := fixture.database.ListRunEvents(fixture.ctx, projectID, childRunID, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	var childStarted *store.Event
	for index := range childEvents {
		if childEvents[index].Type == "run.started" {
			childStarted = &childEvents[index]
			break
		}
	}
	if childStarted == nil {
		t.Fatalf("delegated child has no run.started event: %v", eventTypes(childEvents))
	}
	if childStarted.OccurredAt.Before(paused.OccurredAt) {
		t.Fatalf("delegated child started before parent PAUSED handoff: child=%s parent=%s", childStarted.OccurredAt, paused.OccurredAt)
	}
}

func assertNoAuthoritativeDelegationTools(t *testing.T, fixture *openCodeIntegrationFixture, projectID, runID string) {
	t.Helper()
	events, err := fixture.database.ListRunEvents(fixture.ctx, projectID, runID, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Type != "tool.started" && event.Type != "tool.completed" && event.Type != "tool.failed" {
			continue
		}
		var payload struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(event.Payload, &payload) != nil {
			continue
		}
		if payload.Name == "set_issue_status" || payload.Name == "delegate_task" {
			t.Fatalf("delegated Run invoked prohibited authoritative tool %q", payload.Name)
		}
	}
}
