package mcpapi

import (
	"encoding/json"
	"path"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type publicRunProvenance struct {
	SchemaVersion int    `json:"schemaVersion"`
	ProjectID     string `json:"projectId,omitempty"`
	IssueID       string `json:"issueId,omitempty"`
	RunID         string `json:"runId,omitempty"`
	Attempt       int    `json:"attempt,omitempty"`
	AgentID       string `json:"agentId,omitempty"`
}

type publicEventActor struct {
	Type string `json:"type,omitempty"`
}

type publicTestEventPayload struct {
	Status         string   `json:"status,omitempty"`
	ExitCode       *int     `json:"exitCode,omitempty"`
	OutputChunkIDs []string `json:"outputChunkIds,omitempty"`
}

type publicFileEventPayload struct {
	Path       string `json:"path,omitempty"`
	OldPath    string `json:"oldPath,omitempty"`
	Staged     bool   `json:"staged,omitempty"`
	Unstaged   bool   `json:"unstaged,omitempty"`
	ArtifactID string `json:"artifactId,omitempty"`
	Added      *int   `json:"added,omitempty"`
	Removed    *int   `json:"removed,omitempty"`
	Source     string `json:"source,omitempty"`
}

type fileEventPayload struct {
	Path       string `json:"path"`
	OldPath    string `json:"oldPath,omitempty"`
	Staged     bool   `json:"staged,omitempty"`
	Unstaged   bool   `json:"unstaged,omitempty"`
	ArtifactID string `json:"artifactId,omitempty"`
	Added      *int   `json:"added,omitempty"`
	Removed    *int   `json:"removed,omitempty"`
	Source     string `json:"source,omitempty"`
}

func eventDTO(value store.Event, keys map[string]string) EventDTO {
	out := EventDTO{
		ID: value.ID, SchemaVersion: value.SchemaVersion, Type: value.Type, OccurredAt: value.OccurredAt,
		ProjectID: value.ProjectID, RunID: value.RunID, AgentID: value.AgentID, WorkspaceID: value.WorkspaceID,
		RuntimeInstanceID: value.RuntimeInstanceID, CorrelationID: value.CorrelationID, ParentEventID: value.ParentEventID,
		Sequence: value.Sequence, Actor: publicEventActorDTO(value.Actor), Payload: publicEventPayload(value.Type, value.Payload),
	}
	if value.IssueID != nil {
		if key := keys[*value.IssueID]; key != "" {
			out.IssueID = &key
		}
	}
	return out
}

func publicEventActorDTO(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var actor publicEventActor
	if err := json.Unmarshal(raw, &actor); err != nil {
		return publicEventActor{}
	}
	return actor
}

func publicEventPayload(eventType string, raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	switch {
	case strings.HasPrefix(eventType, "test."):
		var value publicTestEventPayload
		if err := json.Unmarshal(raw, &value); err != nil {
			return publicTestEventPayload{}
		}
		value.OutputChunkIDs = append([]string(nil), value.OutputChunkIDs...)
		return value
	case strings.HasPrefix(eventType, "file."):
		var value fileEventPayload
		if err := json.Unmarshal(raw, &value); err != nil {
			return publicFileEventPayload{}
		}
		filePath, ok := publicRepositoryPath(value.Path)
		if !ok {
			return publicFileEventPayload{}
		}
		out := publicFileEventPayload{
			Path: filePath, Staged: value.Staged, Unstaged: value.Unstaged, ArtifactID: value.ArtifactID,
			Added: value.Added, Removed: value.Removed,
		}
		if oldPath, ok := publicRepositoryPath(value.OldPath); ok {
			out.OldPath = oldPath
		}
		if value.Source == "git" {
			out.Source = value.Source
		}
		return out
	default:
		// Other durable payloads can contain execution-only fields such as cwd,
		// command argv, absolute paths, or backend references. Keep their event
		// envelopes useful without forwarding an unversioned backend payload.
		return map[string]any{}
	}
}

func publicRepositoryPath(value string) (string, bool) {
	if value == "" || value != strings.TrimSpace(value) || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") {
		return "", false
	}
	firstSegment := value
	if index := strings.IndexByte(value, '/'); index >= 0 {
		firstSegment = value[:index]
	}
	if strings.Contains(firstSegment, ":") {
		return "", false
	}
	cleaned := path.Clean(value)
	if cleaned != value || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return cleaned, true
}

func publicRunProvenanceDTO(raw json.RawMessage, run store.Run, keys map[string]string) any {
	if len(raw) == 0 {
		return nil
	}
	var provenance executioncontext.Provenance
	if err := json.Unmarshal(raw, &provenance); err != nil {
		return publicRunProvenance{}
	}
	agentID := ""
	if run.AgentID != nil {
		agentID = *run.AgentID
	}
	return publicRunProvenance{
		SchemaVersion: provenance.SchemaVersion,
		ProjectID:     run.ProjectID,
		IssueID:       keys[run.IssueID],
		RunID:         run.ID,
		Attempt:       run.Attempt,
		AgentID:       agentID,
	}
}

func rawOutputChunkDTO(value store.RawOutputChunk) RawOutputChunkDTO {
	return RawOutputChunkDTO{ID: value.ID, Stream: value.Stream, Sequence: value.Sequence, SizeBytes: value.SizeBytes, Digest: value.Digest, CreatedAt: value.CreatedAt}
}

func artifactDTO(value store.Artifact) ArtifactDTO {
	return ArtifactDTO{
		ID: value.ID, Name: value.Name, Kind: value.Kind, MediaType: value.MediaType, SizeBytes: value.SizeBytes,
		Digest: value.Digest, SafeMetadata: jsonValue(value.SafeMetadata), CreatedAt: value.CreatedAt,
	}
}

func runtimeInstanceDTO(value store.RuntimeInstance) RuntimeInstanceDTO {
	return RuntimeInstanceDTO{
		ID: value.ID, RuntimeID: value.RuntimeID, Status: value.Status, RunnerStatus: value.RunnerStatus,
		CreatedAt: value.CreatedAt, StartedAt: value.StartedAt, StoppedAt: value.StoppedAt, UpdatedAt: value.UpdatedAt,
	}
}

func executionSessionDTO(value store.ExecutionSession) ExecutionSessionDTO {
	return ExecutionSessionDTO{
		ID: value.ID, RuntimeInstanceID: optionalString(value.RuntimeInstanceID), RunnerID: optionalString(value.RunnerID),
		Status: value.Status, ExitCode: value.ExitCode,
		CreatedAt: value.CreatedAt, StartedAt: value.StartedAt, CompletedAt: value.CompletedAt, UpdatedAt: value.UpdatedAt,
	}
}

func evidenceDTO(value app.RunEvidence, keys map[string]string) RunEvidenceDTO {
	out := RunEvidenceDTO{
		Run:              runDTO(value.Run, keys),
		Provenance:       publicRunProvenanceDTO(value.Provenance, value.Run, keys),
		RuntimeInstances: make([]RuntimeInstanceDTO, 0, len(value.RuntimeInstances)),
		Sessions: make([]ExecutionSessionDTO, 0, len(value.Sessions)), Events: make([]EventDTO, 0, len(value.Events)),
		Tests: make([]EventDTO, 0), FileChanges: make([]EventDTO, 0), Usage: valueFromJSONMarshal(value.Usage),
		RawOutput: make([]RawOutputChunkDTO, 0, len(value.RawOutput)), Artifacts: make([]ArtifactDTO, 0, len(value.Artifacts)),
	}
	for _, instance := range value.RuntimeInstances {
		out.RuntimeInstances = append(out.RuntimeInstances, runtimeInstanceDTO(instance))
	}
	for _, session := range value.Sessions {
		out.Sessions = append(out.Sessions, executionSessionDTO(session))
	}
	for _, event := range value.Events {
		dto := eventDTO(event, keys)
		out.Events = append(out.Events, dto)
		if strings.HasPrefix(event.Type, "test.") {
			out.Tests = append(out.Tests, dto)
		}
		if strings.HasPrefix(event.Type, "file.") {
			out.FileChanges = append(out.FileChanges, dto)
		}
	}
	for _, chunk := range value.RawOutput {
		out.RawOutput = append(out.RawOutput, rawOutputChunkDTO(chunk))
	}
	for _, artifact := range value.Artifacts {
		out.Artifacts = append(out.Artifacts, artifactDTO(artifact))
	}
	return out
}
