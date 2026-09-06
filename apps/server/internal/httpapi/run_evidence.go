package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

type RunEvidenceDTO struct {
	Run              RunDTO                        `json:"run"`
	Provenance       json.RawMessage               `json:"provenance"`
	RuntimeInstances []RuntimeInstanceEvidenceDTO  `json:"runtimeInstances"`
	Commands         []ExecutionSessionEvidenceDTO `json:"commands"`
	Events           []EventEvidenceDTO            `json:"events"`
	Tests            []EventEvidenceDTO            `json:"tests"`
	FileChanges      []EventEvidenceDTO            `json:"fileChanges"`
	RawOutput        []RawOutputChunkEvidenceDTO   `json:"rawOutput"`
	Artifacts        []ArtifactEvidenceDTO         `json:"artifacts"`
}

type RuntimeInstanceEvidenceDTO struct {
	ID           string     `json:"id"`
	RuntimeID    string     `json:"runtimeId"`
	Status       string     `json:"status"`
	RunnerStatus string     `json:"runnerStatus"`
	CreatedAt    time.Time  `json:"createdAt"`
	StartedAt    *time.Time `json:"startedAt"`
	StoppedAt    *time.Time `json:"stoppedAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

type ExecutionSessionEvidenceDTO struct {
	ID                string          `json:"id"`
	RuntimeInstanceID string          `json:"runtimeInstanceId"`
	Status            string          `json:"status"`
	CWD               string          `json:"cwd"`
	Command           json.RawMessage `json:"command"`
	ExitCode          *int            `json:"exitCode"`
	CreatedAt         time.Time       `json:"createdAt"`
	StartedAt         *time.Time      `json:"startedAt"`
	CompletedAt       *time.Time      `json:"completedAt"`
	UpdatedAt         time.Time       `json:"updatedAt"`
}

type EventEvidenceDTO struct {
	ID                string          `json:"id"`
	SchemaVersion     int             `json:"schemaVersion"`
	Type              string          `json:"type"`
	OccurredAt        time.Time       `json:"occurredAt"`
	Sequence          *int64          `json:"sequence"`
	AgentID           *string         `json:"agentId"`
	WorkspaceID       *string         `json:"workspaceId"`
	RuntimeInstanceID *string         `json:"runtimeInstanceId"`
	CorrelationID     *string         `json:"correlationId"`
	ParentEventID     *string         `json:"parentEventId"`
	Actor             json.RawMessage `json:"actor"`
	Payload           json.RawMessage `json:"payload"`
}

type RawOutputChunkEvidenceDTO struct {
	ID          string    `json:"id"`
	Stream      string    `json:"stream"`
	Sequence    int64     `json:"sequence"`
	SizeBytes   int64     `json:"sizeBytes"`
	Digest      *string   `json:"digest"`
	ContentPath string    `json:"contentPath"`
	CreatedAt   time.Time `json:"createdAt"`
}

type ArtifactEvidenceDTO struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Kind         string          `json:"kind"`
	MediaType    *string         `json:"mediaType"`
	SizeBytes    int64           `json:"sizeBytes"`
	Digest       *string         `json:"digest"`
	SafeMetadata json.RawMessage `json:"safeMetadata"`
	ContentPath  string          `json:"contentPath"`
	CreatedAt    time.Time       `json:"createdAt"`
}

func (a *api) registerRunEvidenceRoutes(r chi.Router) {
	if a.runEvidence == nil {
		return
	}
	r.Get("/projects/{projectID}/runs/{runID}/evidence", a.getRunEvidence)
	r.Get("/projects/{projectID}/runs/{runID}/raw-output/{chunkID}", a.getRawOutputChunk)
	r.Get("/projects/{projectID}/runs/{runID}/artifacts/{artifactID}", a.getRunArtifact)
}

func (a *api) getRunEvidence(w http.ResponseWriter, r *http.Request) {
	projectID, runID, ok := evidenceRunPath(w, r)
	if !ok {
		return
	}
	value, err := a.runEvidence.Inspect(r.Context(), projectID, runID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runEvidenceDTO(value))
}

func (a *api) getRawOutputChunk(w http.ResponseWriter, r *http.Request) {
	projectID, runID, ok := evidenceRunPath(w, r)
	if !ok {
		return
	}
	chunkID, ok := pathUUID(w, r, "chunkID")
	if !ok {
		return
	}
	chunk, reader, err := a.runEvidence.OpenRawOutput(r.Context(), projectID, runID, chunkID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Length", strconv.FormatInt(chunk.SizeBytes, 10))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if chunk.Digest != nil {
		w.Header().Set("ETag", strconv.Quote(*chunk.Digest))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, io.LimitReader(reader, chunk.SizeBytes))
}

func (a *api) getRunArtifact(w http.ResponseWriter, r *http.Request) {
	projectID, runID, ok := evidenceRunPath(w, r)
	if !ok {
		return
	}
	artifactID, ok := pathUUID(w, r, "artifactID")
	if !ok {
		return
	}
	artifact, reader, err := a.runEvidence.OpenArtifact(r.Context(), projectID, runID, artifactID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", safeMediaType(artifact.MediaType))
	w.Header().Set("Content-Length", strconv.FormatInt(artifact.SizeBytes, 10))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": artifact.Name}))
	if artifact.Digest != nil {
		w.Header().Set("ETag", strconv.Quote(*artifact.Digest))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, io.LimitReader(reader, artifact.SizeBytes))
}

func evidenceRunPath(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return "", "", false
	}
	runID, ok := pathUUID(w, r, "runID")
	if !ok {
		return "", "", false
	}
	return projectID, runID, true
}

func runEvidenceDTO(value app.RunEvidence) RunEvidenceDTO {
	out := RunEvidenceDTO{
		Run:              runDTO(value.Run),
		Provenance:       value.Provenance,
		RuntimeInstances: make([]RuntimeInstanceEvidenceDTO, 0, len(value.RuntimeInstances)),
		Commands:         make([]ExecutionSessionEvidenceDTO, 0, len(value.Sessions)),
		Events:           make([]EventEvidenceDTO, 0, len(value.Events)),
		Tests:            make([]EventEvidenceDTO, 0),
		FileChanges:      make([]EventEvidenceDTO, 0),
		RawOutput:        make([]RawOutputChunkEvidenceDTO, 0, len(value.RawOutput)),
		Artifacts:        make([]ArtifactEvidenceDTO, 0, len(value.Artifacts)),
	}
	for _, instance := range value.RuntimeInstances {
		out.RuntimeInstances = append(out.RuntimeInstances, runtimeInstanceEvidenceDTO(instance))
	}
	for _, session := range value.Sessions {
		out.Commands = append(out.Commands, executionSessionEvidenceDTO(session))
	}
	for _, event := range value.Events {
		dto := eventEvidenceDTO(event)
		out.Events = append(out.Events, dto)
		if strings.HasPrefix(event.Type, "test.") {
			out.Tests = append(out.Tests, dto)
		}
		if strings.HasPrefix(event.Type, "file.") {
			out.FileChanges = append(out.FileChanges, dto)
		}
	}
	for _, chunk := range value.RawOutput {
		out.RawOutput = append(out.RawOutput, rawOutputChunkEvidenceDTO(value.Run.ProjectID, value.Run.ID, chunk))
	}
	for _, artifact := range value.Artifacts {
		out.Artifacts = append(out.Artifacts, artifactEvidenceDTO(value.Run.ProjectID, value.Run.ID, artifact))
	}
	return out
}

func runtimeInstanceEvidenceDTO(value store.RuntimeInstance) RuntimeInstanceEvidenceDTO {
	return RuntimeInstanceEvidenceDTO{
		ID: value.ID, RuntimeID: value.RuntimeID, Status: value.Status, RunnerStatus: value.RunnerStatus,
		CreatedAt: value.CreatedAt, StartedAt: value.StartedAt, StoppedAt: value.StoppedAt, UpdatedAt: value.UpdatedAt,
	}
}

func executionSessionEvidenceDTO(value store.ExecutionSession) ExecutionSessionEvidenceDTO {
	return ExecutionSessionEvidenceDTO{
		ID: value.ID, RuntimeInstanceID: value.RuntimeInstanceID, Status: value.Status, CWD: value.CWD,
		Command: append(json.RawMessage(nil), value.CommandArgv...), ExitCode: value.ExitCode, CreatedAt: value.CreatedAt,
		StartedAt: value.StartedAt, CompletedAt: value.CompletedAt, UpdatedAt: value.UpdatedAt,
	}
}

func eventEvidenceDTO(value store.Event) EventEvidenceDTO {
	return EventEvidenceDTO{
		ID: value.ID, SchemaVersion: value.SchemaVersion, Type: value.Type, OccurredAt: value.OccurredAt,
		Sequence: value.Sequence, AgentID: value.AgentID, WorkspaceID: value.WorkspaceID,
		RuntimeInstanceID: value.RuntimeInstanceID, CorrelationID: value.CorrelationID, ParentEventID: value.ParentEventID,
		Actor: append(json.RawMessage(nil), value.Actor...), Payload: append(json.RawMessage(nil), value.Payload...),
	}
}

func rawOutputChunkEvidenceDTO(projectID, runID string, value store.RawOutputChunk) RawOutputChunkEvidenceDTO {
	return RawOutputChunkEvidenceDTO{
		ID: value.ID, Stream: value.Stream, Sequence: value.Sequence, SizeBytes: value.SizeBytes, Digest: value.Digest,
		ContentPath: fmt.Sprintf("/api/projects/%s/runs/%s/raw-output/%s", projectID, runID, value.ID), CreatedAt: value.CreatedAt,
	}
}

func artifactEvidenceDTO(projectID, runID string, value store.Artifact) ArtifactEvidenceDTO {
	return ArtifactEvidenceDTO{
		ID: value.ID, Name: value.Name, Kind: value.Kind, MediaType: value.MediaType, SizeBytes: value.SizeBytes,
		Digest: value.Digest, SafeMetadata: append(json.RawMessage(nil), value.SafeMetadata...),
		ContentPath: fmt.Sprintf("/api/projects/%s/runs/%s/artifacts/%s", projectID, runID, value.ID), CreatedAt: value.CreatedAt,
	}
}

func safeMediaType(value *string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "application/octet-stream"
	}
	mediaType, params, err := mime.ParseMediaType(*value)
	if err != nil {
		return "application/octet-stream"
	}
	return mime.FormatMediaType(mediaType, params)
}
