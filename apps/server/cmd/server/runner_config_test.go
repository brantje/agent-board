package main

import (
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
)

func TestRunnerReconnectTimeoutConfiguration(t *testing.T) {
	if got, err := parseRunnerReconnectTimeout(""); err != nil || got != app.DefaultRunnerReconnectTimeout {
		t.Fatalf("default timeout=%v err=%v", got, err)
	}
	if got, err := parseRunnerReconnectTimeout(" 45s "); err != nil || got != 45*time.Second {
		t.Fatalf("explicit timeout=%v err=%v", got, err)
	}
	for _, value := range []string{"nope", "0s", "-1s"} {
		if _, err := parseRunnerReconnectTimeout(value); err == nil {
			t.Fatalf("invalid timeout %q accepted", value)
		}
	}

	t.Setenv(runnerReconnectTimeoutEnv, "90s")
	if got, err := configuredRunnerReconnectTimeout(); err != nil || got != 90*time.Second {
		t.Fatalf("environment timeout=%v err=%v", got, err)
	}
}
