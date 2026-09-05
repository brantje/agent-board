package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestCoreStoreIsProjectScoped(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	projectA, err := s.CreateProject(ctx, store.Project{Name: "A", RepositoryPath: "/repo/a"})
	if err != nil {
		t.Fatalf("create project A: %v", err)
	}
	projectB, err := s.CreateProject(ctx, store.Project{Name: "B", RepositoryPath: "/repo/b"})
	if err != nil {
		t.Fatalf("create project B: %v", err)
	}
	if projectA.DefaultBranch != "main" {
		t.Fatalf("default branch = %q, want main", projectA.DefaultBranch)
	}
	if _, err := s.GetProject(ctx, projectA.ID); err != nil {
		t.Fatalf("get project: %v", err)
	}

	issue, err := s.CreateIssue(ctx, store.Issue{ProjectID: projectA.ID, Title: "Scoped issue", Status: "TODO"})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if _, err := s.GetIssue(ctx, projectA.ID, issue.ID); err != nil {
		t.Fatalf("get issue in owning project: %v", err)
	}
	if _, err := s.GetIssue(ctx, projectB.ID, issue.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project read error = %v, want ErrNotFound", err)
	}
}

func TestConfigurationStoreAllowsGlobalButRejectsForeignProjectReferences(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	projectA, _ := s.CreateProject(ctx, store.Project{Name: "A", RepositoryPath: "/repo/a"})
	projectB, _ := s.CreateProject(ctx, store.Project{Name: "B", RepositoryPath: "/repo/b"})
	provider, err := s.CreateProvider(ctx, store.Provider{Name: "provider", Kind: "openai-compatible", Enabled: true})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	modelA, err := s.CreateModelProfile(ctx, store.ModelProfile{ProjectID: &projectA.ID, ProviderID: provider.ID, Name: "model-a", Model: "test", Enabled: true})
	if err != nil {
		t.Fatalf("create project model: %v", err)
	}
	runtimeA, err := s.CreateRuntime(ctx, store.Runtime{ProjectID: &projectA.ID, Name: "runtime-a", Kind: "docker", Image: "agent-board/runtime:test", NetworkPolicy: "restricted", Enabled: true})
	if err != nil {
		t.Fatalf("create project runtime: %v", err)
	}
	if _, err := s.CreateExecutorProfile(ctx, store.ExecutorProfile{ProjectID: &projectB.ID, Name: "bad", Engine: "scripted", ModelProfileID: modelA.ID, RuntimeID: runtimeA.ID, Enabled: true}); err == nil {
		t.Fatal("expected foreign project configuration reference to fail")
	}

	globalModel, err := s.CreateModelProfile(ctx, store.ModelProfile{ProviderID: provider.ID, Name: "global-model", Model: "test", Enabled: true})
	if err != nil {
		t.Fatalf("create global model: %v", err)
	}
	globalRuntime, err := s.CreateRuntime(ctx, store.Runtime{Name: "global-runtime", Kind: "docker", Image: "agent-board/runtime:test", NetworkPolicy: "restricted", Enabled: true})
	if err != nil {
		t.Fatalf("create global runtime: %v", err)
	}
	profile, err := s.CreateExecutorProfile(ctx, store.ExecutorProfile{Name: "global-profile", Engine: "scripted", ModelProfileID: globalModel.ID, RuntimeID: globalRuntime.ID, Enabled: true})
	if err != nil {
		t.Fatalf("create global executor profile: %v", err)
	}
	agent, err := s.CreateAgent(ctx, store.Agent{Name: "global-agent", ExecutorProfileID: profile.ID, ConcurrencyLimit: 2})
	if err != nil {
		t.Fatalf("create global agent: %v", err)
	}
	if got, err := s.GetAgent(ctx, projectB.ID, agent.ID); err != nil || got.ID != agent.ID {
		t.Fatalf("get global agent from project: got=%+v err=%v", got, err)
	}

	profileA, err := s.CreateExecutorProfile(ctx, store.ExecutorProfile{ProjectID: &projectA.ID, Name: "profile-a", Engine: "scripted", ModelProfileID: modelA.ID, RuntimeID: runtimeA.ID, Enabled: true})
	if err != nil {
		t.Fatalf("create project executor profile: %v", err)
	}
	agentA, err := s.CreateAgent(ctx, store.Agent{ProjectID: &projectA.ID, Name: "agent-a", ExecutorProfileID: profileA.ID})
	if err != nil {
		t.Fatalf("create project agent: %v", err)
	}
	if _, err := s.GetAgent(ctx, projectB.ID, agentA.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project agent read error = %v, want ErrNotFound", err)
	}
}
