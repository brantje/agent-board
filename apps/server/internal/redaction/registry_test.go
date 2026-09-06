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
	if got := registry.RedactBytes("run-1", []byte("before "+secret+" after")); bytes.Contains(got, []byte(secret)) {
		t.Fatalf("bytes still contain secret: %q", got)
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

func TestRegistryRedactsAcrossAllRunsAndHandlesJSONEdgeCases(t *testing.T) {
	registry := NewRegistry()
	registry.Register("", []string{"ignored"})
	registry.Register("run-1", []string{"secret-one", ""})
	registry.Register("run-2", []string{"secret-two"})

	if got := registry.RedactAllString("secret-one secret-two"); strings.Contains(got, "secret-one") || strings.Contains(got, "secret-two") {
		t.Fatalf("all-run string leaked: %q", got)
	}
	redacted, err := registry.RedactAllJSON(json.RawMessage(`{"one":"secret-one","nested":["secret-two",1,true,null]}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(redacted), "secret-one") || strings.Contains(string(redacted), "secret-two") {
		t.Fatalf("all-run JSON leaked: %s", redacted)
	}
	if empty, err := registry.RedactAllJSON(nil); err != nil || empty != nil {
		t.Fatalf("empty all-run JSON = %q err=%v", empty, err)
	}
	if _, err := registry.RedactAllJSON(json.RawMessage(`{not-json`)); err == nil {
		t.Fatal("expected invalid JSON error")
	}

	emptyRegistry := NewRegistry()
	input := json.RawMessage(`{"safe":"value"}`)
	got, err := emptyRegistry.RedactAllJSON(input)
	if err != nil || string(got) != string(input) {
		t.Fatalf("no-secret all-run JSON=%s err=%v", got, err)
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
	if WrapError(nil, registry.Values("run-1")) != nil {
		t.Fatal("nil error must remain nil")
	}
	if got := WrapError(cause, nil); got != cause {
		t.Fatalf("error without redaction values should be unchanged: %v", got)
	}
}
