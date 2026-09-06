package redaction

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestSlogHandlerRedactsByteSliceBeforeFormatting(t *testing.T) {
	const secret = "canary-secret"
	cases := []struct {
		name  string
		value any
	}{
		{name: "plain byte slice", value: []byte(secret)},
		{name: "defined byte slice", value: json.RawMessage(secret)},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			registry := NewRegistry()
			registry.Register("run-1", []string{secret})
			var output bytes.Buffer
			logger := slog.New(NewSlogHandler(slog.NewTextHandler(&output, nil), registry))
			logger.Info("bytes", "value", testCase.value)

			got := output.String()
			if strings.Contains(got, secret) {
				t.Fatalf("application log leaked byte-slice secret: %s", got)
			}
			if encoded := fmt.Sprint(testCase.value); strings.Contains(got, encoded) {
				t.Fatalf("application log leaked reversible decimal bytes %q: %s", encoded, got)
			}
		})
	}
}
