package evidence

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type RawOutputStore interface {
	CreateRawOutputChunk(context.Context, store.RawOutputChunk) (store.RawOutputChunk, error)
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
}

func NewOutputRecorder(store RawOutputStore, blobs BlobStore, chunkSize int) (*OutputRecorder, error) {
	if store == nil || blobs == nil {
		return nil, fmt.Errorf("evidence: raw output store and blob store are required")
	}
	if chunkSize <= 0 {
		return nil, fmt.Errorf("evidence: output chunk size must be positive")
	}
	return &OutputRecorder{store: store, blobs: blobs, chunkSize: chunkSize}, nil
}

func (r *OutputRecorder) Capture(ctx context.Context, scope RunScope, stream string, source io.Reader) ([]store.RawOutputChunk, error) {
	if source == nil {
		return nil, fmt.Errorf("evidence: output source is required")
	}
	chunks := make([]store.RawOutputChunk, 0)
	buffer := make([]byte, r.chunkSize)
	var sequence int64
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
