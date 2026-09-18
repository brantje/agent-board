package postgres

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestListDelegationTargetsUsesCanonicalRunnableAgentEligibility(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	scope := f.project.ID

	if _, err := f.store.CreateAgent(ctx, store.Agent{
		ProjectID: &scope, Name: "Disabled target", Engine: f.target.Engine,
		ModelProfileID: f.target.ModelProfileID, EngineSettings: store.EmptyObject,
		ConcurrencyLimit: 1, State: "DISABLED",
	}); err != nil {
		t.Fatal(err)
	}

	targets, err := f.store.ListDelegationTargets(ctx, f.project.ID, f.parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets=%+v want only the runnable target", targets)
	}
	if targets[0].ID != f.target.ID || targets[0].Name != f.target.Name {
		t.Fatalf("target=%+v want %s/%s", targets[0], f.target.ID, f.target.Name)
	}
}
