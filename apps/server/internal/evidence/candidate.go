package evidence

import (
	"context"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

// CandidateCollector and CandidateSnapshotter are temporary compatibility
// types for older processor test fixtures. Git commits now carry durable code
// state; these types do not collect or snapshot Workspace contents.
type CandidateCollector struct{}

type CandidateSnapshotter struct{}

type ArtifactStore interface {
	CreateArtifact(context.Context, store.Artifact) (store.Artifact, error)
}

func NewCandidateCollector() *CandidateCollector { return &CandidateCollector{} }

func NewCandidateSnapshotter(collector *CandidateCollector, artifactStore ArtifactStore, blobs BlobStore) (*CandidateSnapshotter, error) {
	if collector == nil || artifactStore == nil || blobs == nil {
		return nil, fmt.Errorf("evidence: candidate collector, artifact store and blob store are required")
	}
	return &CandidateSnapshotter{}, nil
}
