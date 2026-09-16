package mcpapi

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestIssueExecutionStateDTOPreservesResolvedExecutionAgent(t *testing.T) {
	agentID := "22222222-2222-4222-8222-222222222222"
	issueID := "33333333-3333-4333-8333-333333333333"
	active := store.Run{ID: "44444444-4444-4444-8444-444444444444", IssueID: issueID, AgentID: &agentID, Attempt: 2, Status: "RUNNING"}
	out := issueExecutionStateDTO(store.IssueExecutionState{
		State:          store.IssueExecutionActive,
		ExecutionAgent: &store.IssueExecutionAgent{ID: agentID, Name: "Squad leader"},
		ActiveRun:      &active,
	}, map[string]string{issueID: "AB-7"})

	if out.ExecutionAgent == nil || out.ExecutionAgent.ID != agentID || out.ExecutionAgent.Name != "Squad leader" {
		t.Fatalf("execution agent = %+v", out.ExecutionAgent)
	}
	if out.ActiveRun == nil || out.ActiveRun.IssueID != "AB-7" || out.ActiveRun.AgentID == nil || *out.ActiveRun.AgentID != agentID {
		t.Fatalf("active run = %+v", out.ActiveRun)
	}
}

func TestIssueExecutionStateDTOLeavesExecutionAgentNullWhenOwnershipIsNotExecutable(t *testing.T) {
	out := issueExecutionStateDTO(store.IssueExecutionState{State: store.IssueExecutionNotAgentOwned}, nil)
	if out.ExecutionAgent != nil || out.ActiveRun != nil || out.CanStart {
		t.Fatalf("unexpected execution state dto = %+v", out)
	}
}
