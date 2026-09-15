package mcpapi

import (
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func eventDTO(value store.Event, keys map[string]string) EventDTO {
	out := EventDTO{
		ID: value.ID, SchemaVersion: value.SchemaVersion, Type: value.Type, OccurredAt: value.OccurredAt,
		ProjectID: value.ProjectID, RunID: value.RunID, AgentID: value.AgentID, WorkspaceID: value.WorkspaceID,
		RuntimeInstanceID: value.RuntimeInstanceID, CorrelationID: value.CorrelationID, ParentEventID: value.ParentEventID,
		Sequence: value.Sequence, Actor: jsonValue(value.Actor), Payload: jsonValue(value.Payload),
	}
	if value.IssueID != nil {
		if key := keys[*value.IssueID]; key != "" {
			out.IssueID = &key
		}
	}
	return out
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
		Status: value.Status, CWD: value.CWD, Command: jsonValue(value.CommandArgv), ExitCode: value.ExitCode,
		CreatedAt: value.CreatedAt, StartedAt: value.StartedAt, CompletedAt: value.CompletedAt, UpdatedAt: value.UpdatedAt,
	}
}

func evidenceDTO(value app.RunEvidence, keys map[string]string) RunEvidenceDTO {
	out := RunEvidenceDTO{
		Run: runDTO(value.Run, keys), Provenance: jsonValue(value.Provenance),
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
