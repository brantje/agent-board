package runexec

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode"
	"github.com/brantje/agent-board/apps/server/internal/httpapi"
)

const openCodeGoldenAnswer = `package golden

func Answer() int {
	return 0
}
`

const openCodeGoldenFixedAnswer = `package golden

func Answer() int {
	return 42
}
`

const openCodeGoldenTest = `package golden

import "testing"

func TestAnswer(t *testing.T) {
	if got := Answer(); got != 42 {
		t.Fatalf("Answer() = %d, want 42", got)
	}
}
`

func TestOpenCodeExternalRunnerGoldenPath(t *testing.T) {
	fixture := newOpenCodeIntegrationFixture(t)
	prepareOpenCodeGoldenRepository(t, fixture.ctx, fixture.repositoryPath)
	initialRevision := integrationGitOutput(t, fixture.ctx, fixture.repositoryPath, "rev-parse", "HEAD")
	assertOpenCodeGoldenRepositoryBeforeRun(t, fixture.ctx, fixture.repositoryPath)

	if _, err := exec.LookPath("opencode"); err != nil {
		t.Fatalf("repository-pinned opencode must be available on external runner PATH: %v", err)
	}
	server := httptest.NewServer(httpapi.NewRouterWithApplication(fixture.services))
	t.Cleanup(server.Close)
	external := startRegisteredExternalAgentRunner(
		t,
		fixture.ctx,
		fixture.services.ControlPlane,
		buildAgentRunnerBinary(t),
		server.URL,
		filepath.Join(t.TempDir(), "external-runner-workspaces"),
	)

	// The shared OpenCode fixture starts its Docker-backed compatibility runner for
	// the existing detailed live tests. The golden path disables those pre-existing
	// runners before scheduling so placement still belongs to the scheduler while
	// this Run can only execute on the standalone registered process above.
	runners, err := fixture.services.ControlPlane.Runners.List(fixture.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, runner := range runners {
		if runner.ID == external.Runner.ID {
			continue
		}
		if _, err := fixture.services.ControlPlane.Runners.Revoke(fixture.ctx, runner.ID, false); err != nil {
			t.Fatalf("revoke non-golden runner %s: %v", runner.ID, err)
		}
	}
	fixture.database.SetRunnerCandidates(fixture.services.ControlPlane.Runners.Connections.Candidates)
	waitForOpenCodeRunnerCandidate(t, fixture.ctx, fixture, external.Runner.ID)

	project, run := fixture.createRun(t, openCodeRunSpec{
		roleInstructions: "Follow the issue instructions exactly. Do not modify tests or make unrelated changes.",
		title:            "Fix the deterministic failing test",
		description:      "Fix Answer() so the existing tests pass. Do not modify the tests. Do not change any other files. Run go test ./... to verify the fix, then stop.",
	})
	fixture.startScheduler(t)

	terminal := waitForScriptedRun(t, fixture.ctx, fixture.database, project.ID, run.ID)
	if terminal.Status != "READY_FOR_REVIEW" {
		t.Fatalf("golden Run status=%s failure=%q", terminal.Status, openCodeFailureReason(terminal.FailureReason))
	}
	workspaceRecord, err := fixture.database.GetWorkspace(fixture.ctx, project.ID, terminal.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	review := reviewForRun(t, fixture.ctx, fixture.database, project.ID, run.ID)
	candidateAnswer := integrationGitOutput(t, fixture.ctx, workspaceRecord.Path, "show", review.ReviewRevision+":answer.go")
	if candidateAnswer != strings.TrimSuffix(openCodeGoldenFixedAnswer, "\n") {
		t.Fatalf("review candidate answer.go=%q", candidateAnswer)
	}
	candidateTest := integrationGitOutput(t, fixture.ctx, workspaceRecord.Path, "show", review.ReviewRevision+":answer_test.go")
	if candidateTest != strings.TrimSuffix(openCodeGoldenTest, "\n") {
		t.Fatalf("review candidate modified answer_test.go:\n%s", candidateTest)
	}

	assertOpenCodeGoldenAuthoritativeUnchanged(t, fixture.ctx, fixture.repositoryPath)
	assertOpenCodeGoldenSession(t, fixture.ctx, fixture, project.ID, run.ID, external.Runner.ID)

	reviews := app.ReviewServiceFromServices(fixture.services)
	if reviews == nil {
		t.Fatal("Review service is unavailable")
	}
	approved, err := reviews.Approve(fixture.ctx, project.ID, review.ID, nil)
	if err != nil {
		t.Fatalf("approve golden Review: %v", err)
	}
	if approved.Review.Status != "APPROVED" || approved.Run.Status != "COMPLETED" || approved.Issue.Status != "DONE" {
		t.Fatalf("golden approval result=%+v", approved)
	}

	assertOpenCodeGoldenSession(t, fixture.ctx, fixture, project.ID, run.ID, external.Runner.ID)
	waitForOpenCodeRunnerCandidate(t, fixture.ctx, fixture, external.Runner.ID)
	assertOpenCodeGoldenAcceptedRepository(t, fixture.ctx, fixture.repositoryPath, initialRevision)
}

func prepareOpenCodeGoldenRepository(t *testing.T, ctx context.Context, repositoryPath string) {
	t.Helper()
	runIntegrationCommand(t, ctx, repositoryPath, "git", "reset", "--hard", "HEAD")
	runIntegrationCommand(t, ctx, repositoryPath, "git", "clean", "-fdx")
	runIntegrationCommand(t, ctx, repositoryPath, "git", "rm", "-rf", ".")
	files := map[string]string{
		"go.mod":         "module example.com/agentboard-golden\n\ngo 1.25.0\n",
		"answer.go":      openCodeGoldenAnswer,
		"answer_test.go": openCodeGoldenTest,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(repositoryPath, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write golden fixture %s: %v", name, err)
		}
	}
	runIntegrationCommand(t, ctx, repositoryPath, "git", "add", "--", "go.mod", "answer.go", "answer_test.go")
	runIntegrationCommand(t, ctx, repositoryPath, "git", "commit", "-m", "golden failing fixture")
}

func assertOpenCodeGoldenRepositoryBeforeRun(t *testing.T, ctx context.Context, repositoryPath string) {
	t.Helper()
	if status := integrationGitOutput(t, ctx, repositoryPath, "status", "--short"); status != "" {
		t.Fatalf("golden repository is not clean before Run:\n%s", status)
	}
	assertFileContent(t, filepath.Join(repositoryPath, "answer.go"), openCodeGoldenAnswer)
	assertFileContent(t, filepath.Join(repositoryPath, "answer_test.go"), openCodeGoldenTest)
	if output, err := runOpenCodeGoldenTests(ctx, repositoryPath); err == nil {
		t.Fatalf("golden fixture unexpectedly passes before Run:\n%s", output)
	}
}

func assertOpenCodeGoldenAuthoritativeUnchanged(t *testing.T, ctx context.Context, repositoryPath string) {
	t.Helper()
	assertFileContent(t, filepath.Join(repositoryPath, "answer.go"), openCodeGoldenAnswer)
	assertFileContent(t, filepath.Join(repositoryPath, "answer_test.go"), openCodeGoldenTest)
	if status := integrationGitOutput(t, ctx, repositoryPath, "status", "--short"); status != "" {
		t.Fatalf("authoritative Project Workspace changed before approval:\n%s", status)
	}
	if output, err := runOpenCodeGoldenTests(ctx, repositoryPath); err == nil {
		t.Fatalf("authoritative Project tests unexpectedly pass before approval:\n%s", output)
	}
}

func assertOpenCodeGoldenAcceptedRepository(t *testing.T, ctx context.Context, repositoryPath, initialRevision string) {
	t.Helper()
	assertFileContent(t, filepath.Join(repositoryPath, "answer.go"), openCodeGoldenFixedAnswer)
	assertFileContent(t, filepath.Join(repositoryPath, "answer_test.go"), openCodeGoldenTest)
	if status := integrationGitOutput(t, ctx, repositoryPath, "status", "--short"); status != "" {
		t.Fatalf("accepted Project Workspace is not clean:\n%s", status)
	}
	if diff := integrationGitOutput(t, ctx, repositoryPath, "diff", "--name-status", initialRevision+"..HEAD"); diff != "M\tanswer.go" {
		t.Fatalf("accepted Project change set=%q want only answer.go", diff)
	}
	if output, err := runOpenCodeGoldenTests(ctx, repositoryPath); err != nil {
		t.Fatalf("accepted Project tests failed: %v\n%s", err, output)
	}
}

func assertOpenCodeGoldenSession(t *testing.T, ctx context.Context, fixture *openCodeIntegrationFixture, projectID, runID, runnerID string) {
	t.Helper()
	sessions, err := fixture.database.ListExecutionSessionsByRun(ctx, projectID, runID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("golden execution sessions=%+v", sessions)
	}
	session := sessions[0]
	if session.RunnerID != runnerID || session.RuntimeInstanceID != "" {
		t.Fatalf("golden execution target runner=%q runtimeInstance=%q want runner=%q without Runtime Instance", session.RunnerID, session.RuntimeInstanceID, runnerID)
	}
	if session.Status != "COMPLETED" {
		t.Fatalf("golden execution session status=%q want COMPLETED", session.Status)
	}
	argv := string(session.CommandArgv)
	if !strings.Contains(argv, "opencode") || !strings.Contains(argv, "serve") {
		t.Fatalf("golden execution command=%s", session.CommandArgv)
	}
}

func waitForOpenCodeRunnerCandidate(t *testing.T, ctx context.Context, fixture *openCodeIntegrationFixture, runnerID string) {
	t.Helper()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		for _, candidate := range fixture.services.ControlPlane.Runners.Connections.Candidates(opencode.Name) {
			if candidate == runnerID {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for OpenCode runner %s to become available", runnerID)
		case <-ticker.C:
		}
	}
}

func runOpenCodeGoldenTests(ctx context.Context, repositoryPath string) (string, error) {
	command := exec.CommandContext(ctx, "go", "test", "./...")
	command.Dir = repositoryPath
	output, err := command.CombinedOutput()
	return string(output), err
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(content) != want {
		t.Fatalf("%s=%q want %q", path, content, want)
	}
}
