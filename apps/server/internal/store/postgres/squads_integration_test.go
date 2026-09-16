package postgres

import (
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func createSquadTestAgent(t *testing.T, s *Store, projectID *string, name string) store.Agent {
	t.Helper()
	ctx := t.Context()
	provider, err := s.CreateProvider(ctx, store.Provider{
		ProjectID:    projectID,
		Name:         name + " provider",
		Kind:         "test",
		Enabled:      true,
		HealthStatus: "HEALTHY",
		SafeMetadata: store.EmptyObject,
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	model, err := s.CreateModelProfile(ctx, store.ModelProfile{
		ProjectID:          projectID,
		ProviderID:         provider.ID,
		Name:               name + " model",
		Model:              "test-model",
		GenerationSettings: store.EmptyObject,
		Enabled:            true,
	})
	if err != nil {
		t.Fatalf("create model profile: %v", err)
	}
	agent, err := s.CreateAgent(ctx, store.Agent{
		ProjectID:        projectID,
		Name:             name,
		Engine:           "scripted",
		ModelProfileID:   model.ID,
		EngineSettings:   store.EmptyObject,
		ConcurrencyLimit: 1,
		State:            "ENABLED",
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	return agent
}

func TestSquadStoreCRUDAndProjectIsolation(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := t.Context()
	projectA, err := s.CreateProject(ctx, testProjectInput("squad-a", "/tmp/squad-a", "SQA"))
	if err != nil {
		t.Fatal(err)
	}
	projectB, err := s.CreateProject(ctx, testProjectInput("squad-b", "/tmp/squad-b", "SQB"))
	if err != nil {
		t.Fatal(err)
	}
	leader := createSquadTestAgent(t, s, &projectA.ID, "leader")
	member := createSquadTestAgent(t, s, &projectA.ID, "member")
	global := createSquadTestAgent(t, s, nil, "global")
	role := "review"

	created, err := s.CreateSquad(ctx, store.Squad{
		ProjectID:     projectA.ID,
		Name:          "Core",
		LeaderAgentID: leader.ID,
		Members: []store.SquadMember{
			{AgentID: member.ID, Role: &role},
			{AgentID: global.ID},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.ProjectID != projectA.ID || created.LeaderAgentID != leader.ID || len(created.Members) != 2 {
		t.Fatalf("created squad = %+v", created)
	}
	got, err := s.GetSquad(ctx, projectA.ID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID || len(got.Members) != 2 {
		t.Fatalf("get squad = %+v", got)
	}
	if _, err := s.GetSquad(ctx, projectB.ID, created.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project get error = %v", err)
	}
	values, err := s.ListSquads(ctx, projectA.ID)
	if err != nil || len(values) != 1 || values[0].ID != created.ID {
		t.Fatalf("project A squads=%+v err=%v", values, err)
	}
	values, err = s.ListSquads(ctx, projectB.ID)
	if err != nil || len(values) != 0 {
		t.Fatalf("project B squads=%+v err=%v", values, err)
	}

	updatedRole := "lead reviewer"
	updated, err := s.UpdateSquad(ctx, store.Squad{
		ID:            created.ID,
		ProjectID:     projectA.ID,
		Name:          "Core team",
		LeaderAgentID: member.ID,
		Members: []store.SquadMember{
			{AgentID: leader.ID, Role: &updatedRole},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.LeaderAgentID != member.ID || len(updated.Members) != 1 || updated.Members[0].AgentID != leader.ID || updated.Members[0].Role == nil || *updated.Members[0].Role != updatedRole {
		t.Fatalf("updated squad = %+v", updated)
	}
	if err := s.DeleteSquad(ctx, projectB.ID, created.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project delete error = %v", err)
	}
	if err := s.DeleteSquad(ctx, projectA.ID, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSquad(ctx, projectA.ID, created.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted squad get error = %v", err)
	}
}

func TestSquadStoreRejectsInvalidMembershipScopeAndLeaderDuplication(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := t.Context()
	projectA, err := s.CreateProject(ctx, testProjectInput("scope-a", "/tmp/scope-a", "SPA"))
	if err != nil {
		t.Fatal(err)
	}
	projectB, err := s.CreateProject(ctx, testProjectInput("scope-b", "/tmp/scope-b", "SPB"))
	if err != nil {
		t.Fatal(err)
	}
	leaderA := createSquadTestAgent(t, s, &projectA.ID, "leader-a")
	memberA := createSquadTestAgent(t, s, &projectA.ID, "member-a")
	agentB := createSquadTestAgent(t, s, &projectB.ID, "agent-b")

	if _, err := s.CreateSquad(ctx, store.Squad{ProjectID: projectA.ID, Name: "bad leader", LeaderAgentID: agentB.ID}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("cross-project leader error = %v", err)
	}
	if _, err := s.CreateSquad(ctx, store.Squad{
		ProjectID:     projectA.ID,
		Name:          "duplicate leader",
		LeaderAgentID: leaderA.ID,
		Members:       []store.SquadMember{{AgentID: leaderA.ID}},
	}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("leader duplication error = %v", err)
	}
	if _, err := s.CreateSquad(ctx, store.Squad{
		ProjectID:     projectA.ID,
		Name:          "bad member",
		LeaderAgentID: leaderA.ID,
		Members:       []store.SquadMember{{AgentID: agentB.ID}},
	}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("cross-project member error = %v", err)
	}

	created, err := s.CreateSquad(ctx, store.Squad{
		ProjectID:     projectA.ID,
		Name:          "valid",
		LeaderAgentID: leaderA.ID,
		Members:       []store.SquadMember{{AgentID: memberA.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateSquad(ctx, store.Squad{
		ID:            created.ID,
		ProjectID:     projectA.ID,
		Name:          "invalid update",
		LeaderAgentID: memberA.ID,
		Members:       []store.SquadMember{{AgentID: memberA.ID}},
	}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("updated leader duplication error = %v", err)
	}
	unchanged, err := s.GetSquad(ctx, projectA.ID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.LeaderAgentID != leaderA.ID || len(unchanged.Members) != 1 || unchanged.Members[0].AgentID != memberA.ID {
		t.Fatalf("failed update was not atomic: %+v", unchanged)
	}
}

func TestSquadStoreRestrictsReferencedAgentDeletion(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := t.Context()
	project, err := s.CreateProject(ctx, testProjectInput("delete-ref", "/tmp/delete-ref", "SDR"))
	if err != nil {
		t.Fatal(err)
	}
	leader := createSquadTestAgent(t, s, &project.ID, "delete-leader")
	member := createSquadTestAgent(t, s, &project.ID, "delete-member")
	squad, err := s.CreateSquad(ctx, store.Squad{
		ProjectID:     project.ID,
		Name:          "protected",
		LeaderAgentID: leader.ID,
		Members:       []store.SquadMember{{AgentID: member.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM agents WHERE id=$1`, leader.ID); err == nil {
		t.Fatal("referenced leader agent deletion unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM agents WHERE id=$1`, member.ID); err == nil {
		t.Fatal("referenced member agent deletion unexpectedly succeeded")
	}
	if err := s.DeleteSquad(ctx, project.ID, squad.ID); err != nil {
		t.Fatal(err)
	}
	if tag, err := pool.Exec(ctx, `DELETE FROM agents WHERE id IN ($1,$2)`, leader.ID, member.ID); err != nil || tag.RowsAffected() != 2 {
		t.Fatalf("delete released agents affected=%d err=%v", tag.RowsAffected(), err)
	}
}
