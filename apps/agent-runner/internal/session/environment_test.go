package session

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestSessionDoesNotInheritUnapprovedRunnerEnvironment(t *testing.T) {
	const runnerSecretName = "AGENT_BOARD_RUNNER_INTERNAL_SECRET_TEST"
	const runnerSecretValue = "must-not-reach-child"
	t.Setenv(runnerSecretName, runnerSecretValue)

	manager := NewManagerWithWorkspace(1, t.TempDir())
	execution, err := manager.Start("environment", Request{
		Command: []string{"sh", "-c", "printf '%s|%s' \"${" + runnerSecretName + "-unset}\" \"${EXPLICIT_VALUE-unset}\""},
		Env:     map[string]string{"EXPLICIT_VALUE": "approved"},
	})
	if err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(execution.Stdout())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := execution.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(output)); got != "unset|approved" {
		t.Fatalf("unexpected child environment %q", got)
	}
}

func TestExplicitEnvironmentMayOverrideAllowlistedValue(t *testing.T) {
	t.Setenv("HOME", "/runner-home")
	values := mergeEnvironment(map[string]string{"HOME": "/session-home"}, nil)
	joined := strings.Join(values, "\n")
	if !strings.Contains(joined, "HOME=/session-home") {
		t.Fatalf("expected explicit HOME override in %q", joined)
	}
	if strings.Contains(joined, "HOME=/runner-home") {
		t.Fatalf("runner HOME leaked through explicit override in %q", joined)
	}
}
