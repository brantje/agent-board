package runner

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	dockerruntime "github.com/brantje/agent-board/apps/server/internal/runtime/docker"
)

func TestDockerRunnerIntegration(t *testing.T) {
	if os.Getenv("AGENT_BOARD_TEST_DOCKER") != "1" {
		t.Skip("AGENT_BOARD_TEST_DOCKER=1 is required for live Docker runner integration")
	}
	image := os.Getenv("AGENT_BOARD_TEST_RUNNER_IMAGE")
	if image == "" {
		image = "agent-board-agent-runner:ci"
	}
	workspace := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rt, err := dockerruntime.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	spec := runtimepkg.RuntimeSpec{
		RuntimeInstanceID: "runner-integration",
		ProjectID: "runner-project",
		IssueID: "runner-issue",
		WorkspaceID: "runner-workspace",
		RuntimeID: "runner-runtime",
		Image: image,
		WorkingDirectory: runtimepkg.WorkspaceTarget,
		Workspace: runtimepkg.WorkspaceMount{WorkspaceID: "runner-workspace", Source: workspace, Target: runtimepkg.WorkspaceTarget},
		Network: runtimepkg.NetworkOutbound,
	}
	handle, err := rt.Create(ctx, spec)
	if err != nil {
		t.Fatalf("Create() error=%v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_ = rt.Destroy(cleanupCtx, handle)
	})
	if err := rt.Start(ctx, handle); err != nil {
		t.Fatalf("Start() error=%v", err)
	}

	conn := dialRuntimeRunner(t, ctx, rt, handle)
	defer conn.Close()
	first, err := conn.Start(ctx, "session-first", Request{Command: []string{"sh", "-c", "printf stdout; printf stderr >&2; echo durable > /workspace/from-runner; exit 7"}, Dir: "/workspace"})
	if err != nil {
		t.Fatalf("first Start() error=%v", err)
	}
	stdout, err := io.ReadAll(first.Stdout())
	if err != nil { t.Fatal(err) }
	stderr, err := io.ReadAll(first.Stderr())
	if err != nil { t.Fatal(err) }
	result, err := first.Wait(ctx)
	if err != nil || result.ExitCode != 7 || string(stdout) != "stdout" || string(stderr) != "stderr" {
		t.Fatalf("first stdout=%q stderr=%q result=%+v err=%v", stdout, stderr, result, err)
	}

	second, err := conn.Start(ctx, "session-second", Request{Command: []string{"sh", "-c", "test -f /workspace/from-runner && printf reused"}, Dir: "/workspace"})
	if err != nil { t.Fatal(err) }
	secondOut, err := io.ReadAll(second.Stdout())
	if err != nil { t.Fatal(err) }
	secondResult, err := second.Wait(ctx)
	if err != nil || secondResult.ExitCode != 0 || string(secondOut) != "reused" {
		t.Fatalf("second stdout=%q result=%+v err=%v", secondOut, secondResult, err)
	}

	stdinSession, err := conn.Start(ctx, "session-stdin", Request{Command: []string{"sh", "-c", "read value; printf 'stdin:%s' \"$value\""}, Dir: "/workspace"})
	if err != nil { t.Fatal(err) }
	if _, err := stdinSession.Stdin().Write([]byte("hello\n")); err != nil { t.Fatal(err) }
	if err := stdinSession.Stdin().Close(); err != nil { t.Fatal(err) }
	stdinOut, err := io.ReadAll(stdinSession.Stdout())
	if err != nil { t.Fatal(err) }
	stdinResult, err := stdinSession.Wait(ctx)
	if err != nil || stdinResult.ExitCode != 0 || string(stdinOut) != "stdin:hello" {
		t.Fatalf("stdin stdout=%q result=%+v err=%v", stdinOut, stdinResult, err)
	}

	cancelSession, err := conn.Start(ctx, "session-cancel", Request{Command: []string{"sh", "-c", "trap '' TERM; while :; do sleep 1; done"}, Dir: "/workspace"})
	if err != nil { t.Fatal(err) }
	if err := cancelSession.Terminate(ctx); err != nil { t.Fatal(err) }
	time.Sleep(100 * time.Millisecond)
	if err := cancelSession.Kill(ctx); err != nil { t.Fatal(err) }
	cancelResult, err := cancelSession.Wait(ctx)
	if err != nil || !cancelResult.Signaled {
		t.Fatalf("cancel result=%+v err=%v", cancelResult, err)
	}

	if err := rt.Destroy(ctx, handle); err != nil {
		t.Fatalf("Destroy() error=%v", err)
	}
	contents, err := os.ReadFile(filepath.Join(workspace, "from-runner"))
	if err != nil || strings.TrimSpace(string(contents)) != "durable" {
		t.Fatalf("workspace state=%q err=%v", contents, err)
	}
}

func dialRuntimeRunner(t *testing.T, ctx context.Context, rt *dockerruntime.Runtime, handle runtimepkg.Handle) *Connection {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		endpoint, err := rt.RunnerEndpoint(ctx, handle)
		if err == nil {
			conn, dialErr := Dial(ctx, endpoint.URL)
			if dialErr == nil {
				return conn
			}
			lastErr = dialErr
		} else {
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out connecting to runner: %v", lastErr)
	return nil
}
