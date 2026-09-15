package postgres

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestExecutionConfigurationValidToValidEditsDoNotRecoverTerminalIssue(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *Store, *app.Service, store.Project, store.Agent, store.ModelProfile, store.Provider)
	}{
		{
			name: "agent-engine",
			mutate: func(t *testing.T, _ *Store, svc *app.Service, _ store.Project, agent store.Agent, _ store.ModelProfile, _ store.Provider) {
				agent.Engine = agent.Engine + "-other"
				if _, err := svc.UpdateAgent(t.Context(), agent.ProjectID, agent); err != nil {
					t.Fatalf("update agent engine: %v", err)
				}
			},
		},
		{
			name: "agent-model-profile",
			mutate: func(t *testing.T, s *Store, svc *app.Service, _ store.Project, agent store.Agent, model store.ModelProfile, _ store.Provider) {
				replacement := model
				replacement.ID = ""
				replacement.Name = model.Name + "-other"
				replacement.Model = model.Model + "-other"
				var err error
				replacement, err = s.CreateModelProfile(t.Context(), replacement)
				if err != nil {
					t.Fatalf("create replacement model profile: %v", err)
				}
				agent.ModelProfileID = replacement.ID
				if _, err = svc.UpdateAgent(t.Context(), agent.ProjectID, agent); err != nil {
					t.Fatalf("switch agent model profile: %v", err)
				}
			},
		},
		{
			name: "model-profile-provider",
			mutate: func(t *testing.T, s *Store, svc *app.Service, _ store.Project, _ store.Agent, model store.ModelProfile, provider store.Provider) {
				replacement := provider
				replacement.ID = ""
				replacement.Name = provider.Name + "-other"
				var err error
				replacement, err = s.CreateProvider(t.Context(), replacement)
				if err != nil {
					t.Fatalf("create replacement provider: %v", err)
				}
				model.ProviderID = replacement.ID
				if _, err = svc.UpdateModelProfile(t.Context(), model.ProjectID, model); err != nil {
					t.Fatalf("switch model profile provider: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(testPool(t))
			ctx := t.Context()
			project, issue, agent, previousRun := assignedReadyForReviewRun(t, s, "valid-edit-"+tt.name)
			if _, err := s.pool.Exec(ctx, `UPDATE runs SET status='COMPLETED', completed_at=now(), updated_at=now() WHERE id=$1`, previousRun.ID); err != nil {
				t.Fatalf("complete previous run: %v", err)
			}
			model, err := s.GetModelProfile(ctx, &project.ID, agent.ModelProfileID)
			if err != nil {
				t.Fatalf("get model profile: %v", err)
			}
			provider, err := s.GetProvider(ctx, &project.ID, model.ProviderID)
			if err != nil {
				t.Fatalf("get provider: %v", err)
			}
			beforeRuns, beforeEvents := issueExecutionCounts(t, s, issue.ID)
			if beforeRuns != 1 || beforeEvents != 1 {
				t.Fatalf("fixture runs=%d run.created=%d, want 1/1", beforeRuns, beforeEvents)
			}

			tt.mutate(t, s, app.New(s), project, agent, model, provider)

			afterRuns, afterEvents := issueExecutionCounts(t, s, issue.ID)
			if afterRuns != beforeRuns || afterEvents != beforeEvents {
				t.Fatalf("configuration edit implicitly started Run: runs %d->%d run.created %d->%d", beforeRuns, afterRuns, beforeEvents, afterEvents)
			}
			got, err := s.GetIssue(ctx, project.ID, issue.ID)
			if err != nil {
				t.Fatalf("get issue: %v", err)
			}
			if got.Status != "TODO" || got.AssigneeType == nil || *got.AssigneeType != "AGENT" || got.AssigneeID == nil || *got.AssigneeID != agent.ID {
				t.Fatalf("configuration edit mutated issue: %+v", got)
			}
		})
	}
}

func issueExecutionCounts(t *testing.T, s *Store, issueID string) (runs, createdEvents int) {
	t.Helper()
	ctx := t.Context()
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM runs WHERE issue_id=$1`, issueID).Scan(&runs); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE issue_id=$1 AND type='run.created'`, issueID).Scan(&createdEvents); err != nil {
		t.Fatalf("count run.created events: %v", err)
	}
	return runs, createdEvents
}
