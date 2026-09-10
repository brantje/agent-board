package main

import (
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
)

func TestParseRunnerReconnectTimeout(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    time.Duration
		wantErr string
	}{
		{name: "default", want: app.DefaultRunnerReconnectTimeout},
		{name: "override", value: "45s", want: 45 * time.Second},
		{name: "invalid", value: "later", wantErr: runnerReconnectTimeoutEnv},
		{name: "non-positive", value: "0s", wantErr: "positive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRunnerReconnectTimeout(tt.value)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parse error=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("timeout=%v want=%v", got, tt.want)
			}
		})
	}
}

func TestConfiguredRunnerReconnectTimeoutReadsDeploymentEnv(t *testing.T) {
	t.Setenv(runnerReconnectTimeoutEnv, "2m30s")
	got, err := configuredRunnerReconnectTimeout()
	if err != nil {
		t.Fatal(err)
	}
	if got != 150*time.Second {
		t.Fatalf("timeout=%v", got)
	}
}
