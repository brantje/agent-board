package runexec

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type registeredExternalRunner struct {
	Runner     store.Runner
	Command    *exec.Cmd
	Stdout     *bytes.Buffer
	Stderr     *bytes.Buffer
	ConfigPath string
}

func createExternalRunnerCredential(ctx context.Context, control *app.Service) (store.Runner, string, error) {
	created, registrationToken, err := control.Runners.Create(ctx)
	if err != nil {
		return store.Runner{}, "", err
	}
	runner, token, err := control.Runners.Register(ctx, registrationToken, "External integration host "+created.ID)
	if err != nil {
		return store.Runner{}, "", err
	}
	return runner, token, nil
}

func startRegisteredExternalAgentRunner(t *testing.T, ctx context.Context, control *app.Service, binary, serverURL, workspaceRoot string, extraEnv ...string) registeredExternalRunner {
	t.Helper()
	created, registrationToken, err := control.Runners.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(t.TempDir(), "agent-runner.env")
	register := exec.CommandContext(ctx, binary, "register")
	register.Env = environmentWith(os.Environ(), map[string]string{
		"AGENT_RUNNER_CONFIG_PATH":    configPath,
		"AGENT_RUNNER_WORKSPACE_ROOT": workspaceRoot,
		"HOME":                        t.TempDir(),
	})
	register.Stdin = strings.NewReader(serverURL + "\n" + registrationToken + "\n")
	var registerStdout, registerStderr bytes.Buffer
	register.Stdout = &registerStdout
	register.Stderr = &registerStderr
	if err := register.Run(); err != nil {
		t.Fatalf("register external agent-runner: %v; stdout=%q stderr=%q", err, redactTestSecret(registerStdout.String(), registrationToken), redactTestSecret(registerStderr.String(), registrationToken))
	}

	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat persisted runner config: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("persisted runner config mode=%#o want 0600", info.Mode().Perm())
	}
	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read persisted runner config: %v", err)
	}
	if bytes.Contains(configData, []byte(registrationToken)) {
		t.Fatal("persisted runner config contains one-time registration token")
	}
	persistedEnv, err := parseRunnerEnvironment(configData)
	if err != nil {
		t.Fatalf("parse persisted runner config: %v", err)
	}
	if persistedEnv["AGENT_RUNNER_ID"] != created.ID {
		t.Fatalf("persisted runner id=%q want %q", persistedEnv["AGENT_RUNNER_ID"], created.ID)
	}
	if persistedEnv["AGENT_BOARD_URL"] == "" || persistedEnv["AGENT_RUNNER_TOKEN"] == "" {
		t.Fatal("persisted runner config is incomplete")
	}
	if persistedEnv["AGENT_RUNNER_WORKSPACE_ROOT"] != workspaceRoot {
		t.Fatalf("persisted workspace root=%q want %q", persistedEnv["AGENT_RUNNER_WORKSPACE_ROOT"], workspaceRoot)
	}

	runner, err := control.Runners.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("load registered runner: %v", err)
	}
	if runner.RegisteredAt == nil {
		t.Fatal("runner registration was not persisted")
	}
	if _, _, err := control.Runners.Register(ctx, registrationToken, "registration-token-reuse"); err == nil {
		t.Fatal("one-time runner registration token was reusable")
	}

	values := make(map[string]string, len(persistedEnv)+2)
	for name, value := range persistedEnv {
		values[name] = value
	}
	values["HOME"] = t.TempDir()
	for _, assignment := range extraEnv {
		name, value, ok := strings.Cut(assignment, "=")
		if !ok || name == "" {
			t.Fatalf("invalid external runner environment assignment %q", assignment)
		}
		values[name] = value
	}

	command := exec.CommandContext(ctx, binary)
	command.Env = environmentWith(os.Environ(), values)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		t.Fatalf("start registered external agent-runner: %v", err)
	}
	t.Cleanup(func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		_ = command.Wait()
	})
	waitForRunnerConnected(t, control.Runners.Connections, runner.ID)

	return registeredExternalRunner{Runner: runner, Command: command, Stdout: stdout, Stderr: stderr, ConfigPath: configPath}
}

func parseRunnerEnvironment(data []byte) (map[string]string, error) {
	values := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, raw, ok := strings.Cut(line, "=")
		if !ok || name == "" {
			return nil, fmt.Errorf("invalid environment line %q", line)
		}
		value, err := strconv.Unquote(raw)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", name, err)
		}
		values[name] = value
	}
	return values, nil
}

func environmentWith(base []string, values map[string]string) []string {
	result := make([]string, 0, len(base)+len(values))
	for _, assignment := range base {
		name, _, ok := strings.Cut(assignment, "=")
		if ok {
			if _, overridden := values[name]; overridden {
				continue
			}
		}
		result = append(result, assignment)
	}
	for name, value := range values {
		result = append(result, name+"="+value)
	}
	return result
}

func redactTestSecret(value, secret string) string {
	if secret == "" {
		return value
	}
	return strings.ReplaceAll(value, secret, "<redacted>")
}
