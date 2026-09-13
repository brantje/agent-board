package redaction

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestCredentialSentinelsAreAbsentFromApplicationLogs(t *testing.T) {
	secrets := []string{
		"PASSWORD-SENTINEL-79",
		"ACCESS-TOKEN-SENTINEL-79",
		"REFRESH-TOKEN-SENTINEL-79",
		"PROVIDER-CREDENTIAL-SENTINEL-79",
	}
	registry := NewRegistry()
	registry.Register("credential-leak-run", secrets)
	var output bytes.Buffer
	logger := slog.New(NewSlogHandler(slog.NewTextHandler(&output, nil), registry))
	joined := strings.Join(secrets, " | ")

	logger.LogAttrs(context.Background(), slog.LevelError, "request failed: "+joined,
		slog.String("error", joined),
		slog.String("credential", joined),
	)

	for _, secret := range secrets {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("application log leaked %q: %s", secret, output.String())
		}
	}
}
