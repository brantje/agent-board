package evidence

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/redaction"
)

func TestBlobLimitIsVisibleThroughRedactionBoundary(t *testing.T) {
	base, err := NewFileBlobStore(t.TempDir(), 128)
	if err != nil {
		t.Fatal(err)
	}
	if got := base.MaxBlobBytes(); got != 128 {
		t.Fatalf("base max bytes=%d", got)
	}
	secured, err := NewRedactingBlobStore(base, redaction.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if got := secured.MaxBlobBytes(); got != 128 {
		t.Fatalf("redacting max bytes=%d", got)
	}
	if got := maxBlobBytes(secured); got != 128 {
		t.Fatalf("helper max bytes=%d", got)
	}
	reader, err := secured.Open(t.Context(), "blob:00000000000000000000000000000000")
	if err == nil {
		_ = reader.Close()
		t.Fatal("expected missing blob to fail open")
	}
}

func TestEncodePayloadCoversEmptyAndStructuredValues(t *testing.T) {
	empty, err := EncodePayload(nil)
	if err != nil || !bytes.Equal(empty, []byte(`{}`)) {
		t.Fatalf("empty payload=%s err=%v", empty, err)
	}
	structured, err := EncodePayload(map[string]any{"ok": true, "count": 2})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(structured, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["ok"] != true || decoded["count"] != float64(2) {
		t.Fatalf("structured payload=%v", decoded)
	}
}

func TestEvidenceRecorderRejectsMissingStore(t *testing.T) {
	if _, err := NewRecorder(nil, nil); err == nil {
		t.Fatal("expected nil event store to fail")
	}
}
