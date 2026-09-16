package mcpapi

import "github.com/brantje/agent-board/apps/server/internal/store"

func issueExecutionStateDTO(value store.IssueExecutionState, issueKeys map[string]string) IssueExecutionStateDTO {
	out := IssueExecutionStateDTO{State: value.State, CanStart: value.CanStart}
	if value.ExecutionAgent != nil {
		out.ExecutionAgent = &IssueExecutionAgentDTO{ID: value.ExecutionAgent.ID, Name: value.ExecutionAgent.Name}
	}
	if value.ActiveRun != nil {
		run := runDTO(*value.ActiveRun, issueKeys)
		out.ActiveRun = &run
	}
	return out
}
