package runner

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
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
	if err := os.Chmod(workspace, 0o777); err != nil {
		t.Fatalf("make test Workspace writable by non-root runner: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	registry := NewRegistry(registryIdentity{}, registryIdentity{})
	defer registry.Close()
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/runner/ws", registry)
	server := httptest.NewUnstartedServer(mux)
	server.Listener = listener
	server.Start()
	defer server.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	dockerHost := os.Getenv("AGENT_BOARD_TEST_DOCKER_HOST")
	if dockerHost == "" {
		dockerHost = "host.docker.internal"
	}
	name := "agent-board-runner-it-" + filepath.Base(t.TempDir())
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--detach",
		"--name", name,
		"--add-host", "host.docker.internal:host-gateway",
		"-e", "AGENT_BOARD_URL=http://"+net.JoinHostPort(dockerHost, strconv.Itoa(port)),
		"-e", "AGENT_RUNNER_ID=runner",
		"-e", "AGENT_RUNNER_TOKEN=secret",
		"-e", "AGENT_RUNNER_WORKSPACE_ROOT=/workspace",
		"-v", workspace+":/workspace",
		image,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker run: %v: %s", err, out)
	}
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", name).Run()
	})

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if registry.Connected("runner") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !registry.Connected("runner") {
		logs, _ := exec.Command("docker", "logs", name).CombinedOutput()
		t.Fatalf("timed out waiting for outbound runner connection: %s", logs)
	}

	conn, err := registry.Connect(ctx, "", "runner")
	if err != nil {
		t.Fatalf("Connect() error=%v", err)
	}
	first, err := conn.Start(ctx, "session-first", Request{Command: []string{"sh", "-c", "printf stdout; printf stderr >&2; echo durable > /workspace/from-runner; exit 7"}, Dir: "/workspace"})
	if err != nil {
		t.Fatalf("first Start() error=%v", err)
	}
	stdout, err := io.ReadAll(first.Stdout())
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := io.ReadAll(first.Stderr())
	if err != nil {
		t.Fatal(err)
	}
	result, err := first.Wait(ctx)
	if err != nil || result.ExitCode != 7 || string(stdout) != "stdout" || string(stderr) != "stderr" {
		t.Fatalf("first stdout=%q stderr=%q result=%+v err=%v", stdout, stderr, result, err)
	}

	second, err := conn.Start(ctx, "session-second", Request{Command: []string{"sh", "-c", "test -f /workspace/from-runner && printf reused"}, Dir: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	secondOut, err := io.ReadAll(second.Stdout())
	if err != nil {
		t.Fatal(err)
	}
	secondResult, err := second.Wait(ctx)
	if err != nil || secondResult.ExitCode != 0 || string(secondOut) != "reused" {
		t.Fatalf("second stdout=%q result=%+v err=%v", secondOut, secondResult, err)
	}

	stdinSession, err := conn.Start(ctx, "session-stdin", Request{Command: []string{"sh", "-c", "read value; printf 'stdin:%s' \"$value\""}, Dir: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stdinSession.Stdin().Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	if err := stdinSession.Stdin().Close(); err != nil {
		t.Fatal(err)
	}
	stdinOut, err := io.ReadAll(stdinSession.Stdout())
	if err != nil {
		t.Fatal(err)
	}
	stdinResult, err := stdinSession.Wait(ctx)
	if err != nil || stdinResult.ExitCode != 0 || string(stdinOut) != "stdin:hello" {
		t.Fatalf("stdin stdout=%q result=%+v err=%v", stdinOut, stdinResult, err)
	}

	cancelSession, err := conn.Start(ctx, "session-cancel", Request{Command: []string{"sh", "-c", "trap '' TERM; while :; do sleep 1; done"}, Dir: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if err := cancelSession.Terminate(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if err := cancelSession.Kill(ctx); err != nil {
		t.Fatal(err)
	}
	cancelResult, err := cancelSession.Wait(ctx)
	if err != nil || !cancelResult.Signaled {
		t.Fatalf("cancel result=%+v err=%v", cancelResult, err)
	}

	if err := exec.CommandContext(ctx, "docker", "rm", "-f", name).Run(); err != nil {
		t.Fatalf("docker rm: %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(workspace, "from-runner"))
	if err != nil || strings.TrimSpace(string(contents)) != "durable" {
		t.Fatalf("workspace state=%q err=%v", contents, err)
	}
}
