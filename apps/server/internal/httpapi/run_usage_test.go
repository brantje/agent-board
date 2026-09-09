package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/modelusage"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type httpRunUsageStore struct {
	*httpRunEvidenceStore
}

func (s *httpRunUsageStore) ListRunEvents(_ context.Context, pid, id string, after int64, _ int) ([]store.Event, error) {
	if pid != projectID || id != runID || after > 0 {
		return nil, nil
	}
	limit := int64(200000)
	payload, err := evidence.EncodePayload(modelusage.Sample{
		SampleID: "opencode:ses:finish", ProviderID: "openrouter", ModelID: "model",
		InputTokens: 120, OutputTokens: 30, CacheReadTokens: 80, ContextTokens: 240, ContextLimitTokens: &limit,
	})
	if err != nil {
		return nil, err
	}
	sequence := int64(1)
	return []store.Event{{
		ID: "11111111-bbbb-4bbb-8bbb-111111111111", SchemaVersion: 1, Type: "model.usage",
		ProjectID: projectID, RunID: httpEvidenceStringPtr(runID), Sequence: &sequence, Actor: store.EmptyObject, Payload: payload,
	}}, nil
}

func TestRunUsageRouteMatchesEvidenceProjection(t *testing.T) {
	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	storeFake := &httpRunUsageStore{httpRunEvidenceStore: &httpRunEvidenceStore{}}
	service, err := app.NewRunEvidenceService(storeFake, blobs)
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouterWithApplication(&app.Services{ControlPlane: app.New(&fakeControlPlaneStore{}), RunEvidence: service})

	usageReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/projects/"+projectID+"/runs/"+runID+"/usage", nil)
	usageRec := httptest.NewRecorder()
	router.ServeHTTP(usageRec, usageReq)
	if usageRec.Code != http.StatusOK {
		t.Fatalf("usage status=%d body=%s", usageRec.Code, usageRec.Body.String())
	}
	var usage RunUsageEvidenceDTO
	if err := json.Unmarshal(usageRec.Body.Bytes(), &usage); err != nil {
		t.Fatal(err)
	}
	if usage.ContextTokens != 240 || usage.ContextLimitTokens == nil || *usage.ContextLimitTokens != 200000 || usage.InputTokens != 120 || usage.OutputTokens != 30 || usage.CacheReadTokens != 80 {
		t.Fatalf("usage=%+v", usage)
	}

	evidenceReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/projects/"+projectID+"/runs/"+runID+"/evidence", nil)
	evidenceRec := httptest.NewRecorder()
	router.ServeHTTP(evidenceRec, evidenceReq)
	if evidenceRec.Code != http.StatusOK {
		t.Fatalf("evidence status=%d body=%s", evidenceRec.Code, evidenceRec.Body.String())
	}
	var full RunEvidenceDTO
	if err := json.Unmarshal(evidenceRec.Body.Bytes(), &full); err != nil {
		t.Fatal(err)
	}
	if full.Usage == nil || full.Usage.ContextTokens != usage.ContextTokens || full.Usage.InputTokens != usage.InputTokens || full.Usage.OutputTokens != usage.OutputTokens || full.Usage.CacheReadTokens != usage.CacheReadTokens {
		t.Fatalf("evidence usage=%+v direct=%+v", full.Usage, usage)
	}
}

func TestRunUsageDTOAllowsUnavailableTelemetry(t *testing.T) {
	if runUsageEvidenceDTO(nil) != nil {
		t.Fatal("nil usage must remain nil")
	}
}
