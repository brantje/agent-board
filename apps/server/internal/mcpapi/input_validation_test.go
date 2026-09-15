package mcpapi

import (
	"context"
	"testing"
)

func TestMCPHandlersRejectInvalidEntityIDsBeforeServiceLookup(t *testing.T) {
	ctx := context.Background()
	server := &Server{}
	validRunID := "55555555-5555-5555-5555-555555555555"

	checks := []struct {
		name string
		want string
		call func() error
	}{
		{
			name: "get_agent",
			want: "invalid_argument: agentId must be a UUID",
			call: func() error {
				_, _, err := server.getAgent(ctx, nil, AgentInput{ProjectID: mcpTestProjectID, AgentID: "bad"})
				return err
			},
		},
		{
			name: "get_run",
			want: "invalid_argument: runId must be a UUID",
			call: func() error {
				_, _, err := server.getRun(ctx, nil, RunInput{ProjectID: mcpTestProjectID, RunID: "bad"})
				return err
			},
		},
		{
			name: "get_question",
			want: "invalid_argument: questionId must be a UUID",
			call: func() error {
				_, _, err := server.getQuestion(ctx, nil, QuestionInput{ProjectID: mcpTestProjectID, QuestionID: "bad"})
				return err
			},
		},
		{
			name: "get_review",
			want: "invalid_argument: reviewId must be a UUID",
			call: func() error {
				_, _, err := server.getReview(ctx, nil, ReviewInput{ProjectID: mcpTestProjectID, ReviewID: "bad"})
				return err
			},
		},
		{
			name: "read_run_output_chunk_run",
			want: "invalid_argument: runId must be a UUID",
			call: func() error {
				_, _, err := server.readRunOutputChunk(ctx, nil, ReadRunOutputInput{ProjectID: mcpTestProjectID, RunID: "bad", ChunkID: "bad"})
				return err
			},
		},
		{
			name: "read_run_output_chunk_chunk",
			want: "invalid_argument: chunkId must be a UUID",
			call: func() error {
				_, _, err := server.readRunOutputChunk(ctx, nil, ReadRunOutputInput{ProjectID: mcpTestProjectID, RunID: validRunID, ChunkID: "bad"})
				return err
			},
		},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			err := check.call()
			if err == nil || err.Error() != check.want {
				t.Fatalf("error = %v, want %q", err, check.want)
			}
		})
	}
}
