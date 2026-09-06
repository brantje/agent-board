package evidence

import (
	"context"
	"io"
	"strings"
	"testing"
)

type replacingRedactor struct{ old, replacement string }

func (r replacingRedactor) Reader(_ string, source io.Reader) io.Reader {
	data, _ := io.ReadAll(source)
	return strings.NewReader(strings.ReplaceAll(string(data), r.old, r.replacement))
}

func TestFileBlobStoreRoundTripAndBounds(t *testing.T) {
	store, err := NewFileBlobStore(t.TempDir(), 8)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := store.Put(context.Background(), "run", strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if blob.SizeBytes != 5 || !strings.HasPrefix(blob.Digest, "sha256:") {
		t.Fatalf("unexpected blob metadata: %+v", blob)
	}
	reader, err := store.Open(context.Background(), blob.Ref)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("got %q", data)
	}
	if _, err := store.Put(context.Background(), "run", strings.NewReader("012345678")); err != ErrBlobTooLarge {
		t.Fatalf("expected ErrBlobTooLarge, got %v", err)
	}
	if _, err := store.Open(context.Background(), "../../etc/passwd"); err != ErrInvalidBlobRef {
		t.Fatalf("expected invalid ref, got %v", err)
	}
}

func TestRedactingBlobStoreSanitizesBeforePersistence(t *testing.T) {
	base, err := NewFileBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewRedactingBlobStore(base, replacingRedactor{old: "secret", replacement: "[REDACTED]"})
	if err != nil {
		t.Fatal(err)
	}
	blob, err := store.Put(context.Background(), "run-1", strings.NewReader("token=secret"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := base.Open(context.Background(), blob.Ref)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, _ := io.ReadAll(reader)
	if strings.Contains(string(data), "secret") || string(data) != "token=[REDACTED]" {
		t.Fatalf("unexpected persisted data %q", data)
	}
}

func TestBlobStoreRejectsInvalidConstructionAndCancelledWrites(t *testing.T) {
	if _, err := NewFileBlobStore("", 1); err == nil {
		t.Fatal("expected empty root to be rejected")
	}
	if _, err := NewFileBlobStore(t.TempDir(), 0); err == nil {
		t.Fatal("expected non-positive size limit to be rejected")
	}
	store, err := NewFileBlobStore(t.TempDir(), 16)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(t.Context(), "run", nil); err == nil {
		t.Fatal("expected nil blob source to be rejected")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.Put(ctx, "run", strings.NewReader("data")); err != context.Canceled {
		t.Fatalf("cancelled write error=%v", err)
	}
}

func TestRedactingBlobStoreValidatesDependenciesSourceAndDelegatesOpen(t *testing.T) {
	base, err := NewFileBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	redactor := replacingRedactor{old: "secret", replacement: "[REDACTED]"}
	if _, err := NewRedactingBlobStore(nil, redactor); err == nil {
		t.Fatal("expected nil base store to be rejected")
	}
	if _, err := NewRedactingBlobStore(base, nil); err == nil {
		t.Fatal("expected nil redactor to be rejected")
	}
	store, err := NewRedactingBlobStore(base, redactor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(t.Context(), "run", nil); err == nil {
		t.Fatal("expected nil source to be rejected")
	}
	blob, err := store.Put(t.Context(), "run", strings.NewReader("safe"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := store.Open(t.Context(), blob.Ref)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "safe" {
		t.Fatalf("opened data=%q err=%v", data, err)
	}
}
