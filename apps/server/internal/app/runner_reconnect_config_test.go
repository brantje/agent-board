package app

import (
	"testing"
	"time"
)

func TestConfigureRunnerReconnectTimeoutUpdatesExecutionSessionService(t *testing.T) {
	lowLevel, _, _, _ := runnerOwnedExecutionService(t)
	services := &Services{ExecutionSessions: &AuthorizedExecutionSessionService{sessions: lowLevel}}

	if err := services.ConfigureRunnerReconnectTimeout(2 * time.Minute); err != nil {
		t.Fatal(err)
	}
	if got := lowLevel.runnerReconnectTimeout(); got != 2*time.Minute {
		t.Fatalf("runner reconnect timeout=%v, want 2m", got)
	}
	if err := services.ConfigureRunnerReconnectTimeout(30 * time.Second); err != nil {
		t.Fatal(err)
	}
	if got := lowLevel.runnerReconnectTimeout(); got != 30*time.Second {
		t.Fatalf("reconfigured runner reconnect timeout=%v, want 30s", got)
	}
}

func TestConfigureRunnerReconnectTimeoutRejectsUnavailableOrInvalidConfiguration(t *testing.T) {
	if err := (&Services{}).ConfigureRunnerReconnectTimeout(time.Minute); err == nil {
		t.Fatal("missing execution session service unexpectedly accepted reconnect timeout")
	}
	lowLevel, _, _, _ := runnerOwnedExecutionService(t)
	services := &Services{ExecutionSessions: &AuthorizedExecutionSessionService{sessions: lowLevel}}
	if err := services.ConfigureRunnerReconnectTimeout(0); err == nil {
		t.Fatal("zero reconnect timeout unexpectedly accepted")
	}
}
