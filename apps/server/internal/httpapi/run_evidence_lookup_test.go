package httpapi

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *httpRunEvidenceStore) GetRawOutputChunk(_ context.Context, projectIDValue, runIDValue, chunkID string) (store.RawOutputChunk, error) {
	if projectIDValue != projectID || runIDValue != runID || chunkID != evidenceChunkID {
		return store.RawOutputChunk{}, store.ErrNotFound
	}
	chunks, err := s.ListRawOutputChunks(context.Background(), projectIDValue, runIDValue)
	if err != nil || len(chunks) == 0 {
		return store.RawOutputChunk{}, err
	}
	return chunks[0], nil
}

func (s *httpRunEvidenceStore) GetArtifact(_ context.Context, projectIDValue, runIDValue, artifactID string) (store.Artifact, error) {
	if projectIDValue != projectID || runIDValue != runID || artifactID != evidenceArtifactID {
		return store.Artifact{}, store.ErrNotFound
	}
	artifacts, err := s.ListArtifacts(context.Background(), projectIDValue, runIDValue)
	if err != nil || len(artifacts) == 0 {
		return store.Artifact{}, err
	}
	return artifacts[0], nil
}
