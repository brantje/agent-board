package executioncontext

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"

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
	leftValue, leftErr := decodeJSONLosslessly(left)
	rightValue, rightErr := decodeJSONLosslessly(right)
	if leftErr != nil || rightErr != nil {
		return bytes.Equal(left, right)
	}
	return sameJSONValue(leftValue, rightValue)
}

func decodeJSONLosslessly(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values")
		}
		return nil, err
	}
	return value, nil
}

func sameJSONValue(left, right any) bool {
	switch leftValue := left.(type) {
	case nil:
		return right == nil
	case bool:
		rightValue, ok := right.(bool)
		return ok && leftValue == rightValue
	case string:
		rightValue, ok := right.(string)
		return ok && leftValue == rightValue
	case json.Number:
		rightValue, ok := right.(json.Number)
		return ok && sameJSONNumber(leftValue, rightValue)
	case []any:
		rightValue, ok := right.([]any)
		if !ok || len(leftValue) != len(rightValue) {
			return false
		}
		for index := range leftValue {
			if !sameJSONValue(leftValue[index], rightValue[index]) {
				return false
			}
		}
		return true
	case map[string]any:
		rightValue, ok := right.(map[string]any)
		if !ok || len(leftValue) != len(rightValue) {
			return false
		}
		for key, value := range leftValue {
			rightEntry, exists := rightValue[key]
			if !exists || !sameJSONValue(value, rightEntry) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func sameJSONNumber(left, right json.Number) bool {
	leftValue, leftOK := new(big.Rat).SetString(left.String())
	rightValue, rightOK := new(big.Rat).SetString(right.String())
	return leftOK && rightOK && leftValue.Cmp(rightValue) == 0
}
