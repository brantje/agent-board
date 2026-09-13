package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestIssueCreatorIdentityAndDisplayName(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	project, err := s.CreateProject(ctx, testProjectInput("Creator Project", "/repos/creator", "CRT"))
	if err != nil {
		t.Fatal(err)
	}
	user, err := s.CreateUser(ctx, store.User{
		Username:       "issue-creator",
		Email:          "issue-creator@example.test",
		DisplayName:    "Human Creator",
		DeploymentRole: store.DeploymentRoleMember,
		Status:         store.UserStatusPending,
	})
	if err != nil {
		t.Fatal(err)
	}

	provider, err := s.CreateProvider(ctx, store.Provider{Name: "Creator Provider", Kind: "test", Enabled: true, SafeMetadata: store.EmptyObject})
	if err != nil {
		t.Fatal(err)
	}
	projectID := project.ID
	model, err := s.CreateModelProfile(ctx, store.ModelProfile{ProjectID: &projectID, ProviderID: provider.ID, Name: "Creator Model", Model: "creator-model", GenerationSettings: store.EmptyObject, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := s.CreateAgent(ctx, store.Agent{ProjectID: &projectID, Name: "Creator Agent", Engine: "test", ModelProfileID: model.ID, EngineSettings: store.EmptyObject, ConcurrencyLimit: 1, State: "ENABLED"})
	if err != nil {
		t.Fatal(err)
	}

	// Creator names are resolved from the authoritative identity record; callers
	// persist only actor type + ID and cannot supply presentation text.
	humanType := store.ActorTypeHuman
	humanIssue, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "Human-created", Status: "TODO", CreatedByType: &humanType, CreatedByID: &user.ID})
	if err != nil {
		t.Fatal(err)
	}
	if humanIssue.CreatedByName == nil || *humanIssue.CreatedByName != user.DisplayName {
		t.Fatalf("human creator name=%v want %q", humanIssue.CreatedByName, user.DisplayName)
	}

	agentType := store.ActorTypeAgent
	agentIssue, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "Agent-created", Status: "TODO", CreatedByType: &agentType, CreatedByID: &agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if agentIssue.CreatedByName == nil || *agentIssue.CreatedByName != agent.Name {
		t.Fatalf("agent creator name=%v want %q", agentIssue.CreatedByName, agent.Name)
	}

	persistedHuman, err := s.GetIssue(ctx, project.ID, humanIssue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persistedHuman.CreatedByName == nil || *persistedHuman.CreatedByName != user.DisplayName {
		t.Fatalf("persisted human creator name=%v want %q", persistedHuman.CreatedByName, user.DisplayName)
	}
	persistedAgent, err := s.GetIssue(ctx, project.ID, agentIssue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persistedAgent.CreatedByName == nil || *persistedAgent.CreatedByName != agent.Name {
		t.Fatalf("persisted agent creator name=%v want %q", persistedAgent.CreatedByName, agent.Name)
	}
}

func TestIssueCreatorMustReferenceExistingIdentity(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	project, err := s.CreateProject(ctx, testProjectInput("Creator Validation", "/repos/creator-validation", "CRV"))
	if err != nil {
		t.Fatal(err)
	}
	missingID := "00000000-0000-0000-0000-000000000001"
	for _, creatorType := range []string{store.ActorTypeHuman, store.ActorTypeAgent} {
		creatorType := creatorType
		_, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "Invalid creator", Status: "TODO", CreatedByType: &creatorType, CreatedByID: &missingID})
		if !errors.Is(err, store.ErrInvalidArgument) {
			t.Fatalf("creator type %s error=%v want invalid argument", creatorType, err)
		}
	}
}
