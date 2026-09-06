package executioncontext

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type fakeProvenanceStore struct {
	snapshot json.RawMessage
	puts     int
}

func (f *fakeProvenanceStore) PutRunProvenance(_ context.Context, _, _ string, snapshot json.RawMessage) error {
	if f.snapshot != nil {
		return store.ErrConflict
	}
	f.puts++
	f.snapshot = append(json.RawMessage(nil), snapshot...)
	return nil
}

func (f *fakeProvenanceStore) GetRunProvenance(context.Context, string, string) (json.RawMessage, error) {
	if f.snapshot == nil {
		return nil, store.ErrNotFound
	}
	return append(json.RawMessage(nil), f.snapshot...), nil
}

type racingProvenanceStore struct {
	reads    int
	snapshot json.RawMessage
}

func (f *racingProvenanceStore) GetRunProvenance(context.Context, string, string) (json.RawMessage, error) {
	f.reads++
	if f.reads == 1 {
		return nil, store.ErrNotFound
	}
	return append(json.RawMessage(nil), f.snapshot...), nil
}

func (f *racingProvenanceStore) PutRunProvenance(context.Context, string, string, json.RawMessage) error {
	return store.ErrConflict
}

func TestEnsureProvenancePersistsSafeContextOnce(t *testing.T) {
	evidence := &fakeProvenanceStore{}
	safe := SafeContext{Run: RunContext{ID: "run-1", Attempt: 1}, Provider: ProviderContext{ID: "provider-1", Name: "provider"}}
	if err := EnsureProvenance(context.Background(), evidence, "project-1", "run-1", safe); err != nil {
		t.Fatal(err)
	}
	if err := EnsureProvenance(context.Background(), evidence, "project-1", "run-1", safe); err != nil {
		t.Fatal(err)
	}
	if evidence.puts != 1 {
		t.Fatalf("puts = %d, want 1", evidence.puts)
	}
	var got Provenance
	if err := json.Unmarshal(evidence.snapshot, &got); err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != ProvenanceSchemaVersion || got.Context.Run.ID != "run-1" {
		t.Fatalf("provenance = %+v", got)
	}
}

func TestEnsureProvenanceRejectsDifferentHistoricalContext(t *testing.T) {
	evidence := &fakeProvenanceStore{}
	first := SafeContext{Run: RunContext{ID: "run-1", Attempt: 1}, Runtime: RuntimeContext{Image: "runtime:v1"}}
	if err := EnsureProvenance(context.Background(), evidence, "project-1", "run-1", first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.Runtime.Image = "runtime:v2"
	err := EnsureProvenance(context.Background(), evidence, "project-1", "run-1", second)
	apiErr, ok := AsError(err)
	if !ok || apiErr.Code != "execution_provenance_conflict" || !errors.Is(err, store.ErrConflict) {
		t.Fatalf("err = %#v", err)
	}
}

func TestEnsureProvenancePreservesConflictFromConcurrentDifferentWriter(t *testing.T) {
	winner := SafeContext{Run: RunContext{ID: "run-1", Attempt: 1}, Runtime: RuntimeContext{Image: "runtime:winner"}}
	winnerSnapshot, err := json.Marshal(Provenance{SchemaVersion: ProvenanceSchemaVersion, Context: winner})
	if err != nil {
		t.Fatal(err)
	}
	evidence := &racingProvenanceStore{snapshot: winnerSnapshot}
	loser := winner
	loser.Runtime.Image = "runtime:loser"

	err = EnsureProvenance(context.Background(), evidence, "project-1", "run-1", loser)
	apiErr, ok := AsError(err)
	if !ok || apiErr.Code != "execution_provenance_conflict" || !errors.Is(err, store.ErrConflict) {
		t.Fatalf("err = %#v", err)
	}
}
