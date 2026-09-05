package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestMainLogsBootstrapStatus(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	t.Cleanup(func() {
		slog.SetDefault(previous)
	})

	main()

	logged := output.String()
	if !strings.Contains(logged, "agent-runner bootstrap binary") {
		t.Fatalf("expected bootstrap log message, got %q", logged)
	}
	if !strings.Contains(logged, "status=protocol-not-implemented") {
		t.Fatalf("expected bootstrap status, got %q", logged)
	}
}
