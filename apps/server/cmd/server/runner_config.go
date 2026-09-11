package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
)

const runnerReconnectTimeoutEnv = "AGENT_BOARD_RUNNER_RECONNECT_TIMEOUT"

func configuredRunnerReconnectTimeout() (time.Duration, error) {
	return parseRunnerReconnectTimeout(os.Getenv(runnerReconnectTimeoutEnv))
}

func parseRunnerReconnectTimeout(raw string) (time.Duration, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return app.DefaultRunnerReconnectTimeout, nil
	}
	timeout, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid positive duration: %w", runnerReconnectTimeoutEnv, err)
	}
	if timeout <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", runnerReconnectTimeoutEnv)
	}
	return timeout, nil
}
