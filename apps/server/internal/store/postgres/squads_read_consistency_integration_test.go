package postgres

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSquadReadsRemainCoherentDuringConcurrentMixedMembershipUpdates(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	project, agents := createSquadProjectWithAgents(t, s, "squad-read-coherent", 2)
	user := createSquadWorkflowUser(t, s, project.ID, "squad-read-user", store.ProjectRoleAdmin, store.DeploymentRoleMember, store.UserStatusActive)
	roleB := "member-b"
	userRole := "product"
	created, err := s.CreateSquad(ctx, store.Squad{
		ProjectID: project.ID, Name: "Coherent", LeaderAgentID: agents[0].ID,
		Members: []store.SquadMember{
			{Type: store.SquadMemberTypeAgent, ID: agents[1].ID, Role: &roleB},
			{Type: store.SquadMemberTypeUser, ID: user.ID, Role: &userRole},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	const iterations = 100
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			leader, member := agents[0], agents[1]
			role := "member-b"
			if i%2 == 0 {
				leader, member = agents[1], agents[0]
				role = "member-a"
			}
			if _, err := s.UpdateSquad(ctx, store.Squad{
				ID: created.ID, ProjectID: project.ID, Name: "Coherent", LeaderAgentID: leader.ID,
				Members: []store.SquadMember{
					{Type: store.SquadMemberTypeAgent, ID: member.ID, Role: &role},
					{Type: store.SquadMemberTypeUser, ID: user.ID, Role: &userRole},
				},
			}); err != nil {
				errCh <- fmt.Errorf("update %d: %w", i, err)
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			got, err := s.GetSquad(ctx, project.ID, created.ID)
			if err != nil {
				errCh <- fmt.Errorf("get %d: %w", i, err)
				return
			}
			if err := assertCoherentSquadState(got, agents[0].ID, agents[1].ID, user.ID); err != nil {
				errCh <- fmt.Errorf("get %d: %w", i, err)
				return
			}

			listed, err := s.ListSquads(ctx, project.ID)
			if err != nil {
				errCh <- fmt.Errorf("list %d: %w", i, err)
				return
			}
			if len(listed) != 1 {
				errCh <- fmt.Errorf("list %d returned %d Squads", i, len(listed))
				return
			}
			if err := assertCoherentSquadState(listed[0], agents[0].ID, agents[1].ID, user.ID); err != nil {
				errCh <- fmt.Errorf("list %d: %w", i, err)
				return
			}
		}
	}()

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}

func assertCoherentSquadState(value store.Squad, agentA, agentB, userID string) error {
	if len(value.Members) != 2 {
		return fmt.Errorf("incomplete aggregate: %+v", value)
	}
	var agentMember, userMember *store.SquadMember
	for i := range value.Members {
		member := &value.Members[i]
		switch member.Type {
		case store.SquadMemberTypeAgent:
			agentMember = member
		case store.SquadMemberTypeUser:
			userMember = member
		}
	}
	if agentMember == nil || agentMember.Role == nil || userMember == nil || userMember.ID != userID || userMember.Role == nil || *userMember.Role != "product" {
		return fmt.Errorf("mixed membership lost identity or role: %+v", value)
	}
	switch value.LeaderAgentID {
	case agentA:
		if agentMember.ID != agentB || *agentMember.Role != "member-b" {
			return fmt.Errorf("mixed A-leader aggregate: %+v", value)
		}
	case agentB:
		if agentMember.ID != agentA || *agentMember.Role != "member-a" {
			return fmt.Errorf("mixed B-leader aggregate: %+v", value)
		}
	default:
		return fmt.Errorf("unexpected leader in aggregate: %+v", value)
	}
	return nil
}
