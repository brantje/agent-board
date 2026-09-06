package redaction

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestSlogHandlerRedactsByteSliceBeforeFormatting(t *testing.T) {
	const secret = "canary-secret"
	registry := NewRegistry()
	registry.Register("run-1", []string{secret})
	var output bytes.Buffer
	logger := slog.New(NewSlogHandler(slog.NewTextHandler(&output, nil), registry))
	logger.Info("bytes", "value", []byte(secret))

	got := output.String()
	if strings.Contains(got, secret) {
		t.Fatalf("application log leaked byte-slice secret: %s", got)
	}
	if encoded := fmt.Sprint([]byte(secret)); strings.Contains(got, encoded) {
		t.Fatalf("application log leaked reversible decimal bytes %q: %s", encoded, got)
	}
}
