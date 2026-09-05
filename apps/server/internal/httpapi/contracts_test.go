package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicProviderDTOOmitsCredentialReference(t *testing.T) {
	payload, err := json.Marshal(ProviderDTO{ID: "provider-id", Name: "OpenAI", Kind: "openai"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "credential") {
		t.Fatalf("public provider DTO leaked credential field: %s", payload)
	}
}

func TestWriteErrorUsesStableEnvelope(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeError(recorder, 404, "project_not_found", "project not found")
	if recorder.Code != 404 {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q", got)
	}
	if got := strings.TrimSpace(recorder.Body.String()); got != `{"error":{"code":"project_not_found","message":"project not found"}}` {
		t.Fatalf("body = %s", got)
	}
}
