package postgres

import (
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestEngineRegistryControlsRunCreationAndConfigurationRecovery(t *testing.T) {
	s := New(testPool(t))
	s.SetEngineRegistered(func(name string) bool { return name == "scripted" })
	ctx := t.Context()

	project, err := s.CreateProject(ctx, testProjectInput("engine-registry-authority", "/repo/engine-registry-authority", prefixForTestName("engine-registry-authority")))
	if err != nil {
		t.Fatal(err)
	}
	provider, err := s.CreateProvider(ctx, store.Provider{Name: "engine-registry-authority", Kind: "test", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	model, err := s.CreateModelProfile(ctx, store.ModelProfile{
		ProjectID:  &project.ID,
		ProviderID: provider.ID,
		Name:       "engine-registry-authority",
		Model:      "test",
		Enabled:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := s.CreateAgent(ctx, store.Agent{
		ProjectID:        &project.ID,
		Name:             "engine-registry-authority",
		Engine:           "not-registered",
		ModelProfileID:   model.ID,
		EngineSettings:   store.EmptyObject,
		ConcurrencyLimit: 1,
		State:            "ENABLED",
	})
	if err != nil {
		t.Fatal(err)
	}
	issue, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "registry blocked", Status: "BACKLOG"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetIssueAssignee(ctx, project.ID, issue.ID, &store.Assignee{Type: "AGENT", ID: agent.ID}, store.EmptyObject); err != nil {
		t.Fatalf("invalid Engine ownership must remain valid: %v", err)
	}
	assertIssueEnqueueCounts(t, s, project.ID, issue.ID, 0)

	issue, err = s.GetIssue(ctx, project.ID, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	issue.Status = "TODO"
	if _, err = s.UpdateIssue(ctx, issue); err != nil {
		t.Fatal(err)
	}
	assertIssueEnqueueCounts(t, s, project.ID, issue.ID, 0)
	persisted, err := s.GetIssue(ctx, project.ID, issue.ID)
	if err != nil || persisted.AssigneeID == nil || *persisted.AssigneeID != agent.ID {
		t.Fatalf("ownership changed while Engine was unavailable: issue=%+v err=%v", persisted, err)
	}

	svc := app.New(s)
	state, err := svc.GetIssueExecutionState(ctx, project.ID, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.State != store.IssueExecutionConfigurationUnavailable || state.CanStart {
		t.Fatalf("execution state=%+v, want configuration unavailable", state)
	}
	if _, _, err = s.StartIssueRun(ctx, project.ID, issue.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("StartIssueRun error=%v, want conflict", err)
	}

	agent.Engine = "scripted"
	if _, err = svc.UpdateAgent(ctx, agent.ProjectID, agent); err != nil {
		t.Fatalf("correct Engine: %v", err)
	}
	assertIssueEnqueueCounts(t, s, project.ID, issue.ID, 1)

	agent.Name += " renamed"
	if _, err = svc.UpdateAgent(ctx, agent.ProjectID, agent); err != nil {
		t.Fatalf("non-readiness Agent update: %v", err)
	}
	assertIssueEnqueueCounts(t, s, project.ID, issue.ID, 1)

	// Run creation depends on execution configuration, not current Runner/source
	// capacity. Admission will wait until a compatible Runner is available.
	s.SetRunnerCandidates(func(string) []string { return nil })
	second, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "queues without runner", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetIssueAssignee(ctx, project.ID, second.ID, &store.Assignee{Type: "AGENT", ID: agent.ID}, store.EmptyObject); err != nil {
		t.Fatalf("assign valid Agent without Runner: %v", err)
	}
	assertIssueEnqueueCounts(t, s, project.ID, second.ID, 1)
}
