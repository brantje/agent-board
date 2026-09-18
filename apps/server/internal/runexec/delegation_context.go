package runexec

import (
	"context"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type delegationSquadContextStore interface {
	GetIssue(context.Context, string, string) (store.Issue, error)
	GetSquad(context.Context, string, string) (store.Squad, error)
	GetAgentInScope(context.Context, *string, string) (store.Agent, error)
}

func resolveDelegationToolContext(ctx context.Context, candidate any, safe executioncontext.SafeContext) (*engine.DelegationToolContext, error) {
	targetStore, ok := candidate.(store.DelegationTargetStore)
	if !ok {
		return nil, fmt.Errorf("run execution: delegation target context is unavailable")
	}
	targets, err := targetStore.ListDelegationTargets(ctx, safe.Project.ID, safe.Agent.ID)
	if err != nil {
		return nil, fmt.Errorf("run execution: resolve delegation targets: %w", err)
	}
	result := &engine.DelegationToolContext{Targets: make([]engine.DelegationTargetContext, 0, len(targets))}
	for _, target := range targets {
		result.Targets = append(result.Targets, engine.DelegationTargetContext{ID: target.ID, Name: target.Name})
	}

	if safe.Issue.ID == "" {
		return result, nil
	}
	lookup, ok := candidate.(delegationSquadContextStore)
	if !ok {
		return nil, fmt.Errorf("run execution: delegation Squad context is unavailable")
	}
	issue, err := lookup.GetIssue(ctx, safe.Project.ID, safe.Issue.ID)
	if err != nil {
		return nil, fmt.Errorf("run execution: resolve delegation Issue: %w", err)
	}
	if issue.AssigneeType == nil || *issue.AssigneeType != "SQUAD" || issue.AssigneeID == nil {
		return result, nil
	}

	squad, err := lookup.GetSquad(ctx, safe.Project.ID, *issue.AssigneeID)
	if err != nil {
		return nil, fmt.Errorf("run execution: resolve delegation Squad: %w", err)
	}
	scope := safe.Project.ID
	leader, err := lookup.GetAgentInScope(ctx, &scope, squad.LeaderAgentID)
	if err != nil {
		return nil, fmt.Errorf("run execution: resolve Squad leader: %w", err)
	}
	squadContext := &engine.SquadDelegationContext{
		ID: squad.ID, Name: squad.Name, LeaderAgentID: squad.LeaderAgentID, LeaderAgentName: leader.Name,
		Members: make([]engine.SquadDelegationMemberContext, 0, len(squad.Members)),
	}
	for _, member := range squad.Members {
		if member.Type != store.SquadMemberTypeAgent {
			continue
		}
		agent, err := lookup.GetAgentInScope(ctx, &scope, member.ID)
		if err != nil {
			return nil, fmt.Errorf("run execution: resolve Squad Agent member: %w", err)
		}
		squadContext.Members = append(squadContext.Members, engine.SquadDelegationMemberContext{
			ID: agent.ID, Name: agent.Name, Role: cloneDelegationRole(member.Role),
		})
	}
	result.Squad = squadContext
	return result, nil
}

func cloneDelegationRole(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
