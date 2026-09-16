package postgres

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSquadReadsRemainCoherentDuringConcurrentUpdates(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	project, agents := createSquadProjectWithAgents(t, s, "squad-read-coherent", 2)
	roleB := "member-b"
	created, err := s.CreateSquad(ctx, store.Squad{
		ProjectID:     project.ID,
		Name:          "Coherent",
		LeaderAgentID: agents[0].ID,
		Members:       []store.SquadMember{{AgentID: agents[1].ID, Role: &roleB}},
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
				ID:            created.ID,
				ProjectID:     project.ID,
				Name:          "Coherent",
				LeaderAgentID: leader.ID,
				Members:       []store.SquadMember{{AgentID: member.ID, Role: &role}},
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
			if err := assertCoherentSquadState(got, agents[0].ID, agents[1].ID); err != nil {
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
			if err := assertCoherentSquadState(listed[0], agents[0].ID, agents[1].ID); err != nil {
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

func assertCoherentSquadState(value store.Squad, agentA, agentB string) error {
	if len(value.Members) != 1 || value.Members[0].Role == nil {
		return fmt.Errorf("incomplete aggregate: %+v", value)
	}
	member := value.Members[0]
	switch value.LeaderAgentID {
	case agentA:
		if member.AgentID != agentB || *member.Role != "member-b" {
			return fmt.Errorf("mixed A-leader aggregate: %+v", value)
		}
	case agentB:
		if member.AgentID != agentA || *member.Role != "member-a" {
			return fmt.Errorf("mixed B-leader aggregate: %+v", value)
		}
	default:
		return fmt.Errorf("unexpected leader in aggregate: %+v", value)
	}
	return nil
}
