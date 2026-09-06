package redaction

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestRegistryRedactsStringsJSONAndStreams(t *testing.T) {
	registry := NewRegistry()
	secret := "line\n\"quoted\"-secret"
	registry.Register("run-1", []string{secret, secret})

	if got := registry.RedactString("run-1", "before "+secret+" after"); strings.Contains(got, secret) {
		t.Fatalf("string still contains secret: %q", got)
	}
	raw, err := json.Marshal(map[string]any{"message": "before " + secret + " after", secret: secret})
	if err != nil {
		t.Fatal(err)
	}
	redacted, err := registry.RedactJSON("run-1", raw)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(redacted, &decoded); err != nil {
		t.Fatal(err)
	}
	encoded := string(redacted)
	if strings.Contains(encoded, "quoted") || strings.Contains(encoded, "secret") {
		t.Fatalf("JSON still exposes secret material: %s", redacted)
	}

	streamed, err := io.ReadAll(registry.Reader("run-1", bytes.NewBufferString("x-"+secret+"-y")))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(streamed, []byte(secret)) {
		t.Fatalf("stream still contains secret: %q", streamed)
	}
}

func TestRegistryKeepsRunScopesSeparateAndWrapsErrors(t *testing.T) {
	registry := NewRegistry()
	registry.Register("run-1", []string{"secret-one"})
	registry.Register("run-2", []string{"secret-two"})
	if got := registry.RedactString("run-1", "secret-two"); got != "secret-two" {
		t.Fatalf("cross-run redaction = %q", got)
	}
	cause := errors.New("runner failed with secret-one")
	wrapped := WrapError(cause, registry.Values("run-1"))
	if !errors.Is(wrapped, cause) || strings.Contains(wrapped.Error(), "secret-one") || !IsSafeWrapped(wrapped) {
		t.Fatalf("wrapped error = %v", wrapped)
	}
}
