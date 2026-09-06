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
