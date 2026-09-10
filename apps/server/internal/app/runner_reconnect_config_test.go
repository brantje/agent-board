package app

import (
	"testing"
	"time"
)

func TestRunnerReconnectTimeoutDefaultsAndOverrides(t *testing.T) {
	service, _, _ := runnerOwnedExecutionService(t)
	if got := service.runnerReconnectTimeout(); got != DefaultRunnerReconnectTimeout {
		t.Fatalf("default timeout=%v want=%v", got, DefaultRunnerReconnectTimeout)
	}
	if err := service.SetRunnerReconnectTimeout(30 * time.Second); err != nil {
		t.Fatal(err)
	}
	if got := service.runnerReconnectTimeout(); got != 30*time.Second {
		t.Fatalf("configured timeout=%v", got)
	}
	if err := service.SetRunnerReconnectTimeout(0); err == nil {
		t.Fatal("expected non-positive timeout to be rejected")
	}
}
