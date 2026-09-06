package redaction

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestSlogHandlerPreservesWithAttributeGroupOrder(t *testing.T) {
	registry := NewRegistry()
	registry.Register("run-1", []string{"canary-secret"})
	var output bytes.Buffer
	logger := slog.New(NewSlogHandler(slog.NewJSONHandler(&output, nil), registry))
	logger.With("a", 1).WithGroup("g").With("b", "canary-secret").Info("message", "c", 3)

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["a"] != float64(1) {
		t.Fatalf("top-level a=%v, want 1; record=%v", record["a"], record)
	}
	group, ok := record["g"].(map[string]any)
	if !ok {
		t.Fatalf("group g=%T %v", record["g"], record["g"])
	}
	if _, exists := group["a"]; exists {
		t.Fatalf("pre-group attribute a was incorrectly nested: %v", group)
	}
	if group["c"] != float64(3) {
		t.Fatalf("record attribute c was not kept in active group: %v", group)
	}
	if value, _ := group["b"].(string); value == "canary-secret" || strings.Contains(value, "canary-secret") {
		t.Fatalf("grouped attribute leaked secret: %v", group)
	}
}
