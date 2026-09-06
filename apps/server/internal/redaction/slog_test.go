package redaction

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestSlogHandlerRedactsMessagesAttributesErrorsAndDerivedContext(t *testing.T) {
	registry := NewRegistry()
	var output bytes.Buffer
	logger := slog.New(NewSlogHandler(slog.NewTextHandler(&output, nil), registry)).With("bound", "canary-secret").WithGroup("group-canary-secret")

	// Register after deriving the logger to prove contextual values are redacted
	// at emission time rather than when With/WithGroup is called.
	registry.Register("run-1", []string{"canary-secret"})
	logger.LogAttrs(context.Background(), slog.LevelError, "message canary-secret",
		slog.String("token", "canary-secret"),
		slog.Any("error", fmt.Errorf("wrapped canary-secret")),
		slog.Any("bytes", []byte("canary-secret")),
	)

	if got := output.String(); strings.Contains(got, "canary-secret") {
		t.Fatalf("application log leaked secret: %s", got)
	}
}
