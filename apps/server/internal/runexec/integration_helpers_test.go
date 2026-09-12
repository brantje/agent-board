package runexec

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/brantje/agent-board/apps/server/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func createScriptedFixtureRepository(t *testing.T, ctx context.Context) string {
	t.Helper()
	repositoryPath := filepath.Join(t.TempDir(), "fixture")
	if err := os.MkdirAll(repositoryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, command := range [][]string{{"git", "init", "-q", "-b", "main"}, {"git", "config", "user.email", "integration@example.invalid"}, {"git", "config", "user.name", "Agent Board Integration"}} {
		runIntegrationCommand(t, ctx, repositoryPath, command...)
	}
	for _, name := range []string{"staged.txt", "unstaged.txt", "delete.txt", "rename.txt"} {
		if err := os.WriteFile(filepath.Join(repositoryPath, name), []byte("baseline\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repositoryPath, ".gitignore"), []byte("ignored-scripted.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runIntegrationCommand(t, ctx, repositoryPath, "git", "add", ".")
	runIntegrationCommand(t, ctx, repositoryPath, "git", "commit", "-qm", "baseline")
	return repositoryPath
}

func runIntegrationCommand(t *testing.T, ctx context.Context, dir string, command ...string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %v: %s", command, err, output)
	}
}

func resetRunexecIntegrationDatabase(t *testing.T, databaseURL string) {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var databaseName string
	if err := pool.QueryRow(context.Background(), `SELECT current_database()`).Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	if databaseName != "agent_board_test" || os.Getenv("AGENT_BOARD_TEST_DATABASE_RESET") != "1" {
		t.Fatalf("refusing destructive integration reset for database %q", databaseName)
	}
	if _, err := pool.Exec(context.Background(), `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packages", "database", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), string(schema)); err != nil {
		t.Fatal(err)
	}
}

func waitForScriptedRun(t *testing.T, ctx context.Context, database *postgres.Store, projectID, runID string) store.Run {
	t.Helper()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		run, err := database.GetRun(ctx, projectID, runID)
		if err != nil {
			t.Fatal(err)
		}
		switch run.Status {
		case "READY_FOR_REVIEW", "FAILED", "CANCELLED":
			return run
		}
		select {
		case <-ctx.Done():
			reason := ""
			if run.FailureReason != nil {
				reason = *run.FailureReason
			}
			events, _ := database.ListRunEvents(context.Background(), projectID, runID, 0, 50)
			types := make([]string, 0, len(events))
			for _, event := range events {
				types = append(types, event.Type)
			}
			t.Fatalf("timed out waiting for Run: %v status=%s failure=%s events=%v", ctx.Err(), run.Status, reason, types)
		case <-ticker.C:
		}
	}
}
