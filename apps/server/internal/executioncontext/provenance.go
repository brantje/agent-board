package executioncontext

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

const ProvenanceSchemaVersion = 1

type ProvenanceStore interface {
	PutRunProvenance(context.Context, string, string, json.RawMessage) error
	GetRunProvenance(context.Context, string, string) (json.RawMessage, error)
}

type Provenance struct {
	SchemaVersion int         `json:"schemaVersion"`
	Context       SafeContext `json:"context"`
}

func EnsureProvenance(ctx context.Context, evidence ProvenanceStore, projectID, runID string, safe SafeContext) error {
	if evidence == nil {
		return fmt.Errorf("provenance store is required")
	}
	snapshot, err := json.Marshal(Provenance{SchemaVersion: ProvenanceSchemaVersion, Context: safe})
	if err != nil {
		return fmt.Errorf("encode execution provenance: %w", err)
	}

	existing, err := evidence.GetRunProvenance(ctx, projectID, runID)
	if err == nil {
		if sameJSON(existing, snapshot) {
			return nil
		}
		return fail("execution_provenance_conflict", "Run already has different immutable execution provenance", store.ErrConflict)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("read execution provenance: %w", err)
	}
	if err := evidence.PutRunProvenance(ctx, projectID, runID, snapshot); err != nil {
		// Handle a concurrent writer without making provenance mutable.
		existing, getErr := evidence.GetRunProvenance(ctx, projectID, runID)
		if getErr == nil {
			if sameJSON(existing, snapshot) {
				return nil
			}
			return fail("execution_provenance_conflict", "Run already has different immutable execution provenance", store.ErrConflict)
		}
		return fmt.Errorf("persist execution provenance: %w", err)
	}
	return nil
}

func sameJSON(left, right []byte) bool {
	var leftValue any
	var rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return bytes.Equal(left, right)
	}
	leftCanonical, _ := json.Marshal(leftValue)
	rightCanonical, _ := json.Marshal(rightValue)
	return bytes.Equal(leftCanonical, rightCanonical)
}
