package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

const runEvidenceEventPageSize = 500

type RunEvidenceStore interface {
	GetRun(context.Context, string, string) (store.Run, error)
	GetRunProvenance(context.Context, string, string) (json.RawMessage, error)
	ListExecutionSessions(context.Context, string, []string) ([]store.ExecutionSession, error)
	GetRuntimeInstance(context.Context, string, string) (store.RuntimeInstance, error)
	ListRunEvents(context.Context, string, string, int64, int) ([]store.Event, error)
	ListRawOutputChunks(context.Context, string, string) ([]store.RawOutputChunk, error)
	ListArtifacts(context.Context, string, string) ([]store.Artifact, error)
}

type RunEvidenceService struct {
	store RunEvidenceStore
	blobs evidence.BlobStore
}

type RunEvidence struct {
	Run              store.Run
	Provenance       json.RawMessage
	Sessions         []store.ExecutionSession
	RuntimeInstances []store.RuntimeInstance
	Events           []store.Event
	RawOutput        []store.RawOutputChunk
	Artifacts        []store.Artifact
}

func NewRunEvidenceService(evidenceStore RunEvidenceStore, blobs evidence.BlobStore) (*RunEvidenceService, error) {
	if evidenceStore == nil || blobs == nil {
		return nil, fmt.Errorf("run evidence: store and blob storage are required")
	}
	return &RunEvidenceService{store: evidenceStore, blobs: blobs}, nil
}

func (s *RunEvidenceService) Inspect(ctx context.Context, projectID, runID string) (RunEvidence, error) {
	run, err := s.requireRun(ctx, projectID, runID)
	if err != nil {
		return RunEvidence{}, err
	}

	provenance, err := s.store.GetRunProvenance(ctx, projectID, runID)
	if errors.Is(err, store.ErrNotFound) {
		provenance = nil
	} else if err != nil {
		return RunEvidence{}, fmt.Errorf("read run provenance: %w", err)
	}

	allSessions, err := s.store.ListExecutionSessions(ctx, projectID, nil)
	if err != nil {
		return RunEvidence{}, fmt.Errorf("list execution sessions: %w", err)
	}
	sessions := make([]store.ExecutionSession, 0)
	for _, session := range allSessions {
		if session.RunID == runID {
			sessions = append(sessions, session)
		}
	}

	events, err := s.listAllRunEvents(ctx, projectID, runID)
	if err != nil {
		return RunEvidence{}, err
	}
	rawOutput, err := s.store.ListRawOutputChunks(ctx, projectID, runID)
	if err != nil {
		return RunEvidence{}, fmt.Errorf("list raw output: %w", err)
	}
	artifacts, err := s.store.ListArtifacts(ctx, projectID, runID)
	if err != nil {
		return RunEvidence{}, fmt.Errorf("list artifacts: %w", err)
	}
	runtimeInstances, err := s.runtimeInstances(ctx, projectID, sessions, events)
	if err != nil {
		return RunEvidence{}, err
	}

	return RunEvidence{
		Run:              run,
		Provenance:       append(json.RawMessage(nil), provenance...),
		Sessions:         sessions,
		RuntimeInstances: runtimeInstances,
		Events:           events,
		RawOutput:        rawOutput,
		Artifacts:        artifacts,
	}, nil
}

func (s *RunEvidenceService) OpenRawOutput(ctx context.Context, projectID, runID, chunkID string) (store.RawOutputChunk, io.ReadCloser, error) {
	if _, err := s.requireRun(ctx, projectID, runID); err != nil {
		return store.RawOutputChunk{}, nil, err
	}
	if strings.TrimSpace(chunkID) == "" {
		return store.RawOutputChunk{}, nil, NewError("invalid_argument", "raw output chunk id is required", store.ErrInvalidArgument)
	}
	chunks, err := s.store.ListRawOutputChunks(ctx, projectID, runID)
	if err != nil {
		return store.RawOutputChunk{}, nil, fmt.Errorf("list raw output: %w", err)
	}
	for _, chunk := range chunks {
		if chunk.ID != chunkID {
			continue
		}
		reader, err := s.blobs.Open(ctx, chunk.StorageRef)
		if err != nil {
			return store.RawOutputChunk{}, nil, fmt.Errorf("open raw output blob: %w", err)
		}
		return chunk, reader, nil
	}
	return store.RawOutputChunk{}, nil, NewError("raw_output_not_found", "raw output chunk not found", store.ErrNotFound)
}

func (s *RunEvidenceService) OpenArtifact(ctx context.Context, projectID, runID, artifactID string) (store.Artifact, io.ReadCloser, error) {
	if _, err := s.requireRun(ctx, projectID, runID); err != nil {
		return store.Artifact{}, nil, err
	}
	if strings.TrimSpace(artifactID) == "" {
		return store.Artifact{}, nil, NewError("invalid_argument", "artifact id is required", store.ErrInvalidArgument)
	}
	artifacts, err := s.store.ListArtifacts(ctx, projectID, runID)
	if err != nil {
		return store.Artifact{}, nil, fmt.Errorf("list artifacts: %w", err)
	}
	for _, artifact := range artifacts {
		if artifact.ID != artifactID {
			continue
		}
		reader, err := s.blobs.Open(ctx, artifact.StorageRef)
		if err != nil {
			return store.Artifact{}, nil, fmt.Errorf("open artifact blob: %w", err)
		}
		return artifact, reader, nil
	}
	return store.Artifact{}, nil, NewError("artifact_not_found", "artifact not found", store.ErrNotFound)
}

func (s *RunEvidenceService) requireRun(ctx context.Context, projectID, runID string) (store.Run, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(runID) == "" {
		return store.Run{}, NewError("invalid_argument", "projectId and runId are required", store.ErrInvalidArgument)
	}
	run, err := s.store.GetRun(ctx, projectID, runID)
	return run, translateStoreError(err, "run")
}

func (s *RunEvidenceService) listAllRunEvents(ctx context.Context, projectID, runID string) ([]store.Event, error) {
	events := make([]store.Event, 0)
	var after int64
	for {
		batch, err := s.store.ListRunEvents(ctx, projectID, runID, after, runEvidenceEventPageSize)
		if err != nil {
			return nil, fmt.Errorf("list run events: %w", err)
		}
		if len(batch) == 0 {
			return events, nil
		}
		events = append(events, batch...)
		last := batch[len(batch)-1].Sequence
		if last == nil || *last <= after {
			return nil, fmt.Errorf("run evidence: invalid event sequence while reading run %s", runID)
		}
		after = *last
		if len(batch) < runEvidenceEventPageSize {
			return events, nil
		}
	}
}

func (s *RunEvidenceService) runtimeInstances(ctx context.Context, projectID string, sessions []store.ExecutionSession, events []store.Event) ([]store.RuntimeInstance, error) {
	ids := make(map[string]struct{})
	for _, session := range sessions {
		if session.RuntimeInstanceID != "" {
			ids[session.RuntimeInstanceID] = struct{}{}
		}
	}
	for _, event := range events {
		if event.RuntimeInstanceID != nil && *event.RuntimeInstanceID != "" {
			ids[*event.RuntimeInstanceID] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	instances := make([]store.RuntimeInstance, 0, len(ordered))
	for _, id := range ordered {
		instance, err := s.store.GetRuntimeInstance(ctx, projectID, id)
		if err != nil {
			return nil, fmt.Errorf("read runtime instance %s: %w", id, err)
		}
		instances = append(instances, instance)
	}
	return instances, nil
}
