package mcpapi

import (
	"context"
	"io"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerRunTools(server *mcp.Server) {
	mcp.AddTool(server, readOnlyTool("list_runs", "List Runs in one visible Project using public Issue keys."), objectListHandler(s.listRuns))
	mcp.AddTool(server, readOnlyTool("get_run", "Read one Project Run using a public Issue key in the response."), s.getRun)
	mcp.AddTool(server, mutationTool("cancel_run", "Explicitly cancel an eligible Run through the canonical scheduler-owned cancellation lifecycle.", true, false), s.cancelRun)
	mcp.AddTool(server, readOnlyTool("inspect_run", "Inspect safe structured Run evidence including sessions, events, tests, file changes, usage, raw-output metadata, and Artifact metadata."), s.inspectRun)
	mcp.AddTool(server, readOnlyTool("read_run_output_chunk", "Read one existing durable raw-output chunk through the evidence/redaction boundary."), s.readRunOutputChunk)
}

func (s *Server) listRuns(ctx context.Context, _ *mcp.CallToolRequest, input ProjectInput) (*mcp.CallToolResult, []RunDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, nil, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, nil, toolError(ctx, err)
	}
	values, err := s.services.ProjectAccess.ListRuns(ctx, actor, input.ProjectID)
	if err != nil {
		return nil, nil, toolError(ctx, err)
	}
	keys, err := s.issueKeys(ctx, actor, input.ProjectID)
	if err != nil {
		return nil, nil, toolError(ctx, err)
	}
	out := make([]RunDTO, 0, len(values))
	for _, value := range values {
		out = append(out, runDTO(value, keys))
	}
	return nil, out, nil
}

func (s *Server) getRun(ctx context.Context, _ *mcp.CallToolRequest, input RunInput) (*mcp.CallToolResult, RunDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, RunDTO{}, toolError(ctx, err)
	}
	if err := requireUUID(input.RunID, "runId"); err != nil {
		return nil, RunDTO{}, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, RunDTO{}, toolError(ctx, err)
	}
	value, err := s.services.ProjectAccess.GetRun(ctx, actor, input.ProjectID, input.RunID)
	if err != nil {
		return nil, RunDTO{}, toolError(ctx, err)
	}
	keys, err := s.issueKeys(ctx, actor, input.ProjectID)
	if err != nil {
		return nil, RunDTO{}, toolError(ctx, err)
	}
	return nil, runDTO(value, keys), nil
}

func (s *Server) cancelRun(ctx context.Context, _ *mcp.CallToolRequest, input RunInput) (*mcp.CallToolResult, MutationAck, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, MutationAck{}, toolError(ctx, err)
	}
	if err := requireUUID(input.RunID, "runId"); err != nil {
		return nil, MutationAck{}, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, MutationAck{}, toolError(ctx, err)
	}
	if err := s.services.CancelRunForUser(ctx, actor, input.ProjectID, input.RunID); err != nil {
		return nil, MutationAck{}, toolError(ctx, err)
	}
	return nil, MutationAck{Success: true}, nil
}

func (s *Server) inspectRun(ctx context.Context, _ *mcp.CallToolRequest, input RunInput) (*mcp.CallToolResult, RunEvidenceDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, RunEvidenceDTO{}, toolError(ctx, err)
	}
	if err := requireUUID(input.RunID, "runId"); err != nil {
		return nil, RunEvidenceDTO{}, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, RunEvidenceDTO{}, toolError(ctx, err)
	}
	value, err := s.services.ProjectAccess.InspectRun(ctx, actor, s.services.RunEvidence, input.ProjectID, input.RunID)
	if err != nil {
		return nil, RunEvidenceDTO{}, toolError(ctx, err)
	}
	keys, err := s.issueKeys(ctx, actor, input.ProjectID)
	if err != nil {
		return nil, RunEvidenceDTO{}, toolError(ctx, err)
	}
	return nil, evidenceDTO(value, keys), nil
}

func (s *Server) readRunOutputChunk(ctx context.Context, _ *mcp.CallToolRequest, input ReadRunOutputInput) (*mcp.CallToolResult, ReadRunOutputDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, ReadRunOutputDTO{}, toolError(ctx, err)
	}
	if err := requireUUID(input.RunID, "runId"); err != nil {
		return nil, ReadRunOutputDTO{}, toolError(ctx, err)
	}
	if err := requireUUID(input.ChunkID, "chunkId"); err != nil {
		return nil, ReadRunOutputDTO{}, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, ReadRunOutputDTO{}, toolError(ctx, err)
	}
	chunk, reader, err := s.services.ProjectAccess.OpenRunRawOutput(ctx, actor, s.services.RunEvidence, input.ProjectID, input.RunID, input.ChunkID)
	if err != nil {
		return nil, ReadRunOutputDTO{}, toolError(ctx, err)
	}
	defer reader.Close()
	content, err := io.ReadAll(io.LimitReader(reader, chunk.SizeBytes))
	if err != nil {
		return nil, ReadRunOutputDTO{}, toolError(ctx, err)
	}
	return nil, ReadRunOutputDTO{Chunk: rawOutputChunkDTO(chunk), Content: string(content)}, nil
}
