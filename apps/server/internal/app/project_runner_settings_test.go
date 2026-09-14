package app

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type projectRunnerSettingsStore struct {
	store.RunnerStore
	runnerIDs []string
	runners   []store.Runner
}

func (s *projectRunnerSettingsStore) ListProjectRunnerIDs(context.Context, string) ([]string, error) {
	return append([]string(nil), s.runnerIDs...), nil
}

func (s *projectRunnerSettingsStore) ListRunners(context.Context) ([]store.Runner, error) {
	return append([]store.Runner(nil), s.runners...), nil
}

func TestProjectRunnerSettingsClassifiesOwnedSharedAndInternalCapacity(t *testing.T) {
	ownerID := "project-1"
	foreignID := "project-2"
	memory := &projectRunnerSettingsStore{
		runnerIDs: []string{"shared-selected"},
		runners: []store.Runner{
			{ID: "shared-selected"},
			{ID: "owned", ProjectID: &ownerID},
			{ID: "foreign", ProjectID: &foreignID},
			{ID: "internal", Internal: true},
		},
	}

	settings, err := NewRunnerService(memory).ProjectRunnerSettings(context.Background(), "PROJECT-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(settings.RunnerIDs) != 1 || settings.RunnerIDs[0] != "shared-selected" {
		t.Fatalf("runner ids=%v", settings.RunnerIDs)
	}
	if len(settings.SharedRunners) != 1 || settings.SharedRunners[0].ID != "shared-selected" {
		t.Fatalf("shared runners=%v", settings.SharedRunners)
	}
	if len(settings.ProjectRunners) != 1 || settings.ProjectRunners[0].ID != "owned" {
		t.Fatalf("project runners=%v", settings.ProjectRunners)
	}
	if settings.InternalRunner == nil || settings.InternalRunner.ID != "internal" {
		t.Fatalf("internal runner=%v", settings.InternalRunner)
	}
}
