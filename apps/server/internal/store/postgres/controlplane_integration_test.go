package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestControlPlanePersistenceAndProjectIsolation(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	p1, err := s.CreateProject(ctx, store.Project{Name: "Project One", RepositoryPath: "/repos/one", DefaultBranch: "main", WorkflowSettings: store.EmptyObject})
	if err != nil { t.Fatal(err) }
	p2, err := s.CreateProject(ctx, store.Project{Name: "Project Two", RepositoryPath: "/repos/two", DefaultBranch: "main", WorkflowSettings: store.EmptyObject})
	if err != nil { t.Fatal(err) }
	projects, err := s.ListProjects(ctx)
	if err != nil || len(projects) != 2 { t.Fatalf("projects=%d err=%v", len(projects), err) }
	p1.Name = "Project One Updated"
	if _, err = s.UpdateProject(ctx, p1); err != nil { t.Fatal(err) }

	provider, err := s.CreateProvider(ctx, store.Provider{Name: "Provider", Kind: "test", Enabled: true, SafeMetadata: store.EmptyObject})
	if err != nil { t.Fatal(err) }
	if _, err = s.GetProvider(ctx, provider.ID); err != nil { t.Fatal(err) }
	providers, err := s.ListProviders(ctx)
	if err != nil || len(providers) != 1 { t.Fatalf("providers=%d err=%v", len(providers), err) }
	provider.BaseURL = stringPtrPG("https://example.test")
	if _, err = s.UpdateProvider(ctx, provider); err != nil { t.Fatal(err) }
	if _, err = s.CreateProvider(ctx, store.Provider{Name: "Provider", Kind: "test", Enabled: true}); !errors.Is(err, store.ErrConflict) { t.Fatalf("duplicate provider err=%v", err) }

	p1id, p2id := p1.ID, p2.ID
	scope1, scope2 := &p1id, &p2id
	model, err := s.CreateModelProfile(ctx, store.ModelProfile{ProjectID: scope1, ProviderID: provider.ID, Name: "Model", Model: "model", GenerationSettings: store.EmptyObject, Enabled: true})
	if err != nil { t.Fatal(err) }
	if _, err = s.GetModelProfile(ctx, scope1, model.ID); err != nil { t.Fatal(err) }
	if _, err = s.GetModelProfile(ctx, scope2, model.ID); !errors.Is(err, store.ErrNotFound) { t.Fatalf("cross-project model err=%v", err) }
	models, err := s.ListModelProfiles(ctx, scope1)
	if err != nil || len(models) != 1 { t.Fatalf("models=%d err=%v", len(models), err) }
	model.Model = "model-v2"
	if _, err = s.UpdateModelProfile(ctx, scope1, model); err != nil { t.Fatal(err) }

	runtime, err := s.CreateRuntime(ctx, store.Runtime{ProjectID: scope1, Name: "Runtime", Kind: "docker", Image: "agent-board:test", NetworkPolicy: "none", WorkspacePolicy: "issue", Capabilities: store.EmptyObject, Enabled: true})
	if err != nil { t.Fatal(err) }
	if _, err = s.GetRuntime(ctx, scope1, runtime.ID); err != nil { t.Fatal(err) }
	if _, err = s.GetRuntime(ctx, scope2, runtime.ID); !errors.Is(err, store.ErrNotFound) { t.Fatalf("cross-project runtime err=%v", err) }
	runtimes, err := s.ListRuntimes(ctx, scope1)
	if err != nil || len(runtimes) != 1 { t.Fatalf("runtimes=%d err=%v", len(runtimes), err) }
	runtime.Image = "agent-board:test2"
	if _, err = s.UpdateRuntime(ctx, scope1, runtime); err != nil { t.Fatal(err) }

	executor, err := s.CreateExecutorProfile(ctx, store.ExecutorProfile{ProjectID: scope1, Name: "Executor", Engine: "test", ModelProfileID: model.ID, RuntimeID: runtime.ID, EngineSettings: store.EmptyObject, Enabled: true})
	if err != nil { t.Fatal(err) }
	if _, err = s.GetExecutorProfile(ctx, scope1, executor.ID); err != nil { t.Fatal(err) }
	if _, err = s.GetExecutorProfile(ctx, scope2, executor.ID); !errors.Is(err, store.ErrNotFound) { t.Fatalf("cross-project executor err=%v", err) }
	executors, err := s.ListExecutorProfiles(ctx, scope1)
	if err != nil || len(executors) != 1 { t.Fatalf("executors=%d err=%v", len(executors), err) }
	executor.Engine = "test-v2"
	if _, err = s.UpdateExecutorProfile(ctx, scope1, executor); err != nil { t.Fatal(err) }

	agent, err := s.CreateAgent(ctx, store.Agent{ProjectID: scope1, Name: "Agent", ExecutorProfileID: executor.ID, ConcurrencyLimit: 1, State: "ENABLED"})
	if err != nil { t.Fatal(err) }
	if _, err = s.GetAgentInScope(ctx, scope1, agent.ID); err != nil { t.Fatal(err) }
	if _, err = s.GetAgentInScope(ctx, scope2, agent.ID); !errors.Is(err, store.ErrNotFound) { t.Fatalf("cross-project agent err=%v", err) }
	agents, err := s.ListAgents(ctx, scope1)
	if err != nil || len(agents) != 1 { t.Fatalf("agents=%d err=%v", len(agents), err) }
	agent.RoleInstructions = "updated"
	if _, err = s.UpdateAgent(ctx, scope1, agent); err != nil { t.Fatal(err) }

	issue, err := s.CreateIssue(ctx, store.Issue{ProjectID: p1.ID, Title: "Issue", Status: "TODO"})
	if err != nil { t.Fatal(err) }
	issues, err := s.ListIssues(ctx, p1.ID)
	if err != nil || len(issues) != 1 { t.Fatalf("issues=%d err=%v", len(issues), err) }
	issue.Description = "updated"
	if _, err = s.UpdateIssue(ctx, issue); err != nil { t.Fatal(err) }

	assigned, run, err := s.AssignIssue(ctx, p1.ID, issue.ID, agent.ID)
	if err != nil { t.Fatal(err) }
	if assigned.Status != "IN_PROGRESS" || assigned.AssignedAgentID == nil || *assigned.AssignedAgentID != agent.ID { t.Fatalf("unexpected assigned issue: %+v", assigned) }
	if run.Status != "QUEUED" || run.Attempt != 1 || run.WorkspaceID == "" { t.Fatalf("unexpected run: %+v", run) }
	workspace, err := s.GetWorkspaceByIssue(ctx, p1.ID, issue.ID)
	if err != nil { t.Fatal(err) }
	if workspace.BootstrapStatus != "PENDING" || !strings.HasPrefix(workspace.Path, pendingWorkspacePrefix) { t.Fatalf("unexpected workspace: %+v", workspace) }
	runs, err := s.ListRuns(ctx, p1.ID)
	if err != nil || len(runs) != 1 { t.Fatalf("runs=%d err=%v", len(runs), err) }
	sameIssue, sameRun, err := s.AssignIssue(ctx, p1.ID, issue.ID, agent.ID)
	if err != nil { t.Fatal(err) }
	if sameIssue.ID != assigned.ID || sameRun.ID != run.ID { t.Fatalf("same assignment created duplicate run: %s vs %s", sameRun.ID, run.ID) }
	var runCount, jobCount int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM runs WHERE issue_id=$1`, issue.ID).Scan(&runCount); err != nil { t.Fatal(err) }
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM scheduler_jobs WHERE run_id=$1 AND kind='START' AND state='QUEUED'`, run.ID).Scan(&jobCount); err != nil { t.Fatal(err) }
	if runCount != 1 || jobCount != 1 { t.Fatalf("runCount=%d jobCount=%d", runCount, jobCount) }

	otherAgent, err := s.CreateAgent(ctx, store.Agent{ProjectID: scope1, Name: "Agent Two", ExecutorProfileID: executor.ID, ConcurrencyLimit: 1, State: "ENABLED"})
	if err != nil { t.Fatal(err) }
	_, replacement, err := s.AssignIssue(ctx, p1.ID, issue.ID, otherAgent.ID)
	if err != nil { t.Fatal(err) }
	if replacement.Attempt != 2 || replacement.ID == run.ID { t.Fatalf("unexpected replacement: %+v", replacement) }
	old, err := s.GetRun(ctx, p1.ID, run.ID)
	if err != nil { t.Fatal(err) }
	if old.Status != "CANCELLED" { t.Fatalf("old status=%s", old.Status) }

	if _, _, err = s.AssignIssue(ctx, p2.ID, issue.ID, otherAgent.ID); !errors.Is(err, store.ErrNotFound) { t.Fatalf("cross-project assignment err=%v", err) }
	done, err := s.CreateIssue(ctx, store.Issue{ProjectID: p1.ID, Title: "Done", Status: "DONE"})
	if err != nil { t.Fatal(err) }
	if _, _, err = s.AssignIssue(ctx, p1.ID, done.ID, otherAgent.ID); !errors.Is(err, store.ErrConflict) { t.Fatalf("done assignment err=%v", err) }
	disabled, err := s.CreateAgent(ctx, store.Agent{ProjectID: scope1, Name: "Disabled", ExecutorProfileID: executor.ID, ConcurrencyLimit: 1, State: "DISABLED"})
	if err != nil { t.Fatal(err) }
	blocked, err := s.CreateIssue(ctx, store.Issue{ProjectID: p1.ID, Title: "Blocked", Status: "TODO"})
	if err != nil { t.Fatal(err) }
	if _, _, err = s.AssignIssue(ctx, p1.ID, blocked.ID, disabled.ID); !errors.Is(err, store.ErrConflict) { t.Fatalf("disabled assignment err=%v", err) }
}

func stringPtrPG(v string) *string { return &v }
