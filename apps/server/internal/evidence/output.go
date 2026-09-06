package evidence

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type RawOutputStore interface {
	CreateRawOutputChunk(context.Context, store.RawOutputChunk) (store.RawOutputChunk, error)
	ListRawOutputChunks(context.Context, string, string) ([]store.RawOutputChunk, error)
}

type RunScope struct {
	ProjectID string
	IssueID   string
	RunID     string
}

type OutputRecorder struct {
	store     RawOutputStore
	blobs     BlobStore
	chunkSize int
	locksMu   sync.Mutex
	locks     map[string]*streamLockEntry
}

type streamLockEntry struct {
	mu    sync.Mutex
	users int
}

func NewOutputRecorder(store RawOutputStore, blobs BlobStore, chunkSize int) (*OutputRecorder, error) {
	if store == nil || blobs == nil {
		return nil, fmt.Errorf("evidence: raw output store and blob store are required")
	}
	if chunkSize <= 0 {
		return nil, fmt.Errorf("evidence: output chunk size must be positive")
	}
	return &OutputRecorder{store: store, blobs: blobs, chunkSize: chunkSize, locks: make(map[string]*streamLockEntry)}, nil
}

func (r *OutputRecorder) Capture(ctx context.Context, scope RunScope, stream string, source io.Reader) ([]store.RawOutputChunk, error) {
	if source == nil {
		return nil, fmt.Errorf("evidence: output source is required")
	}
	key, lock := r.acquireStreamLock(scope.RunID, stream)
	defer r.releaseStreamLock(key, lock)

	existing, err := r.store.ListRawOutputChunks(ctx, scope.ProjectID, scope.RunID)
	if err != nil {
		return nil, err
	}
	var sequence int64
	for _, chunk := range existing {
		if chunk.Stream == stream && chunk.Sequence > sequence {
			sequence = chunk.Sequence
		}
	}
	chunks := make([]store.RawOutputChunk, 0)
	buffer := make([]byte, r.chunkSize)
	for {
		n, err := io.ReadFull(source, buffer)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return nil, fmt.Errorf("evidence: read %s output: %w", stream, err)
		}
		if n > 0 {
			sequence++
			blob, putErr := r.blobs.Put(ctx, scope.RunID, bytes.NewReader(buffer[:n]))
			if putErr != nil {
				return nil, putErr
			}
			digest := blob.Digest
			chunk, createErr := r.store.CreateRawOutputChunk(ctx, store.RawOutputChunk{
				ProjectID:  scope.ProjectID,
				IssueID:    scope.IssueID,
				RunID:      scope.RunID,
				Stream:     stream,
				Sequence:   sequence,
				StorageRef: blob.Ref,
				SizeBytes:  blob.SizeBytes,
				Digest:     &digest,
			})
			if createErr != nil {
				return nil, createErr
			}
			chunks = append(chunks, chunk)
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return chunks, nil
		}
	}
}

func (r *OutputRecorder) acquireStreamLock(runID, stream string) (string, *streamLockEntry) {
	key := runID + "\x00" + stream
	r.locksMu.Lock()
	entry := r.locks[key]
	if entry == nil {
		entry = &streamLockEntry{}
		r.locks[key] = entry
	}
	entry.users++
	r.locksMu.Unlock()

	entry.mu.Lock()
	return key, entry
}

func (r *OutputRecorder) releaseStreamLock(key string, entry *streamLockEntry) {
	entry.mu.Unlock()

	r.locksMu.Lock()
	defer r.locksMu.Unlock()
	entry.users--
	if entry.users == 0 && r.locks[key] == entry {
		delete(r.locks, key)
	}
}
