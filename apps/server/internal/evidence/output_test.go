package evidence

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type memoryRawStore struct{ chunks []store.RawOutputChunk }

func (s *memoryRawStore) ListRawOutputChunks(_ context.Context, _, _ string) ([]store.RawOutputChunk, error) {
	return append([]store.RawOutputChunk(nil), s.chunks...), nil
}

func (s *memoryRawStore) CreateRawOutputChunk(_ context.Context, chunk store.RawOutputChunk) (store.RawOutputChunk, error) {
	chunk.ID = "chunk"
	s.chunks = append(s.chunks, chunk)
	return chunk, nil
}

func TestOutputRecorderChunksLargeStreams(t *testing.T) {
	blobs, err := NewFileBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	metadata := &memoryRawStore{}
	recorder, err := NewOutputRecorder(metadata, blobs, 4)
	if err != nil {
		t.Fatal(err)
	}
	chunks, err := recorder.Capture(context.Background(), RunScope{ProjectID: "p", IssueID: "i", RunID: "r"}, "STDOUT", strings.NewReader("abcdefghij"))
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks", len(chunks))
	}
	if chunks[0].SizeBytes != 4 || chunks[1].SizeBytes != 4 || chunks[2].SizeBytes != 2 {
		t.Fatalf("unexpected sizes: %+v", chunks)
	}
	for i, chunk := range chunks {
		if chunk.Sequence != int64(i+1) {
			t.Fatalf("chunk %d sequence %d", i, chunk.Sequence)
		}
	}
	more, err := recorder.Capture(context.Background(), RunScope{ProjectID: "p", IssueID: "i", RunID: "r"}, "STDOUT", strings.NewReader("xy"))
	if err != nil {
		t.Fatal(err)
	}
	if len(more) != 1 || more[0].Sequence != 4 {
		t.Fatalf("sequence did not continue: %+v", more)
	}
}

func TestOutputRecorderReleasesCompletedStreamLocks(t *testing.T) {
	blobs, err := NewFileBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := NewOutputRecorder(&memoryRawStore{}, blobs, 4)
	if err != nil {
		t.Fatal(err)
	}

	for index := 0; index < 64; index++ {
		_, err := recorder.Capture(t.Context(), RunScope{ProjectID: "p", IssueID: "i", RunID: fmt.Sprintf("run-%d", index)}, "STDOUT", strings.NewReader("x"))
		if err != nil {
			t.Fatal(err)
		}
	}

	recorder.locksMu.Lock()
	remaining := len(recorder.locks)
	recorder.locksMu.Unlock()
	if remaining != 0 {
		t.Fatalf("completed stream locks retained=%d, want 0", remaining)
	}
}
