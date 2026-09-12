package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type providerHealthStore struct {
	fakeStore
	provider        store.Provider
	healthUpdates   []providerHealthUpdate
	healthUpdatesMu sync.Mutex
}

type providerHealthUpdate struct {
	id       string
	health   string
	filtered *int
	total    *int
}

func (s *providerHealthStore) GetProvider(_ context.Context, _ *string, id string) (store.Provider, error) {
	if id != s.provider.ID {
		return store.Provider{}, store.ErrNotFound
	}
	return s.provider, nil
}

func (s *providerHealthStore) ListAllProviders(context.Context) ([]store.Provider, error) {
	return []store.Provider{s.provider}, nil
}

func (s *providerHealthStore) UpdateProviderHealth(_ context.Context, id string, health string, filtered, total *int) error {
	s.healthUpdatesMu.Lock()
	s.healthUpdates = append(s.healthUpdates, providerHealthUpdate{id: id, health: health, filtered: filtered, total: total})
	s.healthUpdatesMu.Unlock()
	if health == "HEALTHY" && filtered != nil {
		s.provider.HealthStatus = health
		s.provider.FilteredModelCount = filtered
		s.provider.TotalModelCount = total
	}
	if health == "UNHEALTHY" {
		s.provider.HealthStatus = health
	}
	return nil
}

func (s *providerHealthStore) lastHealthUpdate() providerHealthUpdate {
	s.healthUpdatesMu.Lock()
	defer s.healthUpdatesMu.Unlock()
	if len(s.healthUpdates) == 0 {
		return providerHealthUpdate{}
	}
	return s.healthUpdates[len(s.healthUpdates)-1]
}

func TestListProviderModelsPersistsHealthyCounts(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "model-a"}, {"id": "model-b"}, {"id": "model-c"}},
		})
	}))
	defer upstream.Close()

	ref := "provider:" + testProviderID
	store := &providerHealthStore{provider: store.Provider{
		ID:            testProviderID,
		Kind:          "openai-compatible",
		BaseURL:       &[]string{upstream.URL}[0],
		CredentialRef: &ref,
		Enabled:       true,
		SafeMetadata:  store.EmptyObject,
	}}
	service := New(store)
	resolver := &fakeProviderModelSecretResolver{values: map[string][]byte{ref: []byte("secret")}}

	result, err := service.ListProviderModels(context.Background(), nil, testProviderID, resolver, upstream.Client())
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(result.Models) != 3 || result.Total != 3 {
		t.Fatalf("result=%+v", result)
	}
	update := store.lastHealthUpdate()
	if update.health != "HEALTHY" || update.filtered == nil || *update.filtered != 3 || update.total == nil || *update.total != 3 {
		t.Fatalf("update=%+v", update)
	}
}

func TestListProviderModelsPersistsUnhealthyAndKeepsPriorCounts(t *testing.T) {
	priorFiltered, priorTotal := 5, 5
	store := &providerHealthStore{provider: store.Provider{
		ID:                 testProviderID,
		Kind:               "llamarack",
		Enabled:            true,
		HealthStatus:       "HEALTHY",
		FilteredModelCount: &priorFiltered,
		TotalModelCount:    &priorTotal,
		SafeMetadata:       store.EmptyObject,
	}}
	service := New(store)

	_, err := service.ListProviderModels(context.Background(), nil, testProviderID, &fakeProviderModelSecretResolver{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	update := store.lastHealthUpdate()
	if update.health != "UNHEALTHY" || update.filtered != nil || update.total != nil {
		t.Fatalf("update=%+v", update)
	}
	if store.provider.FilteredModelCount == nil || *store.provider.FilteredModelCount != priorFiltered {
		t.Fatalf("filtered count changed: %+v", store.provider.FilteredModelCount)
	}
}

func TestProviderHealthWorkerProbesAllProviders(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "model-a"}},
		})
	}))
	defer upstream.Close()

	sharedID := "11111111-1111-4111-8111-111111111111"
	projectID := "22222222-2222-4222-8222-222222222222"
	ownedID := "33333333-3333-4333-8333-333333333333"
	ref := "provider:" + sharedID

	store := &multiProviderHealthStore{
		fakeStore: fakeStore{project: store.Project{ID: projectID, Name: "Project", IssuePrefix: "AB", RepositoryPath: "/repo", DefaultBranch: "main", WorkflowSettings: store.EmptyObject}},
		providers: []store.Provider{
			{ID: sharedID, Kind: "openai-compatible", BaseURL: &[]string{upstream.URL}[0], CredentialRef: &ref, Enabled: true, SafeMetadata: store.EmptyObject},
			{ID: ownedID, ProjectID: &projectID, Kind: "llamarack", Enabled: true, SafeMetadata: store.EmptyObject},
		},
		byID: map[string]store.Provider{
			sharedID: {ID: sharedID, Kind: "openai-compatible", BaseURL: &[]string{upstream.URL}[0], CredentialRef: &ref, Enabled: true, SafeMetadata: store.EmptyObject},
			ownedID:  {ID: ownedID, ProjectID: &projectID, Kind: "llamarack", Enabled: true, SafeMetadata: store.EmptyObject},
		},
	}
	service := New(store)
	worker := NewProviderHealthWorker(service, store, &fakeProviderModelSecretResolver{values: map[string][]byte{ref: []byte("secret")}}, upstream.Client())
	worker.probeAll(context.Background())

	store.healthUpdatesMu.Lock()
	updates := append([]providerHealthUpdate(nil), store.healthUpdates...)
	store.healthUpdatesMu.Unlock()
	if len(updates) < 2 {
		t.Fatalf("updates=%d", len(updates))
	}
	healthy := 0
	unhealthy := 0
	for _, update := range updates {
		switch update.health {
		case "HEALTHY":
			healthy++
		case "UNHEALTHY":
			unhealthy++
		}
	}
	if healthy == 0 || unhealthy == 0 {
		t.Fatalf("updates=%+v", updates)
	}
}

type multiProviderHealthStore struct {
	fakeStore
	providers     []store.Provider
	byID          map[string]store.Provider
	healthUpdates []providerHealthUpdate
	healthUpdatesMu sync.Mutex
}

func (s *multiProviderHealthStore) GetProvider(_ context.Context, _ *string, id string) (store.Provider, error) {
	provider, ok := s.byID[id]
	if !ok {
		return store.Provider{}, store.ErrNotFound
	}
	return provider, nil
}

func (s *multiProviderHealthStore) ListAllProviders(context.Context) ([]store.Provider, error) {
	return append([]store.Provider(nil), s.providers...), nil
}

func (s *multiProviderHealthStore) UpdateProviderHealth(_ context.Context, id string, health string, filtered, total *int) error {
	s.healthUpdatesMu.Lock()
	s.healthUpdates = append(s.healthUpdates, providerHealthUpdate{id: id, health: health, filtered: filtered, total: total})
	s.healthUpdatesMu.Unlock()
	return nil
}

func TestPersistProviderHealthLogsStoreErrors(t *testing.T) {
	store := &failingUpdateProviderHealthStore{
		providerHealthStore: providerHealthStore{
			provider: store.Provider{ID: testProviderID, Kind: "test", Enabled: true, SafeMetadata: store.EmptyObject},
		},
		err: errors.New("update failed"),
	}
	service := New(store)
	filtered, total := 2, 4
	service.persistProviderHealth(context.Background(), testProviderID, true, &filtered, &total)
	service.persistProviderHealth(context.Background(), testProviderID, false, nil, nil)
	service.persistProviderHealth(context.Background(), "", true, &filtered, &total)
}

func TestServiceListAllProviders(t *testing.T) {
	store := &providerHealthStore{provider: store.Provider{ID: testProviderID, Kind: "test", Enabled: true, SafeMetadata: store.EmptyObject}}
	service := New(store)
	providers, err := service.ListAllProviders(context.Background())
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(providers) != 1 || providers[0].ID != testProviderID {
		t.Fatalf("providers=%v", providers)
	}
}

func TestServiceEnqueueProviderHealthProbe(t *testing.T) {
	service := New(&providerHealthStore{provider: store.Provider{ID: testProviderID, Kind: "test", Enabled: true, SafeMetadata: store.EmptyObject}})
	worker := NewProviderHealthWorker(service, service.store, nil, nil)
	service.SetProviderHealthWorker(worker)
	service.EnqueueProviderHealthProbe("")
	service.EnqueueProviderHealthProbe(testProviderID)
}

func TestProviderHealthWorkerRunStopsOnCancel(t *testing.T) {
	store := &providerHealthStore{provider: store.Provider{ID: testProviderID, Kind: "llamarack", Enabled: true, SafeMetadata: store.EmptyObject}}
	service := New(store)
	worker := NewProviderHealthWorker(service, store, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	cancel()
	if err := waitFor(done, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestProviderHealthWorkerProbeByID(t *testing.T) {
	store := &providerHealthStore{provider: store.Provider{ID: testProviderID, Kind: "llamarack", Enabled: true, SafeMetadata: store.EmptyObject}}
	service := New(store)
	worker := NewProviderHealthWorker(service, store, nil, nil)
	worker.probeByID(context.Background(), testProviderID)
	worker.probeByID(context.Background(), "missing")
}

func TestProviderHealthWorkerProbeByIDHandlesListError(t *testing.T) {
	store := &failingListProviderHealthStore{err: errors.New("list failed")}
	worker := NewProviderHealthWorker(New(store), store, nil, nil)
	worker.probeByID(context.Background(), testProviderID)
}

func TestProviderHealthWorkerProbeProviderSkipsWithoutService(t *testing.T) {
	worker := &ProviderHealthWorker{store: &providerHealthStore{}}
	worker.probeProvider(context.Background(), store.Provider{ID: testProviderID})
}

func TestProviderHealthWorkerRunProcessesEnqueueAndTicker(t *testing.T) {
	store := &providerHealthStore{provider: store.Provider{ID: testProviderID, Kind: "llamarack", Enabled: true, SafeMetadata: store.EmptyObject}}
	service := New(store)
	worker := NewProviderHealthWorker(service, store, nil, nil)
	worker.interval = 30 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	time.Sleep(20 * time.Millisecond)
	worker.Enqueue(testProviderID)
	time.Sleep(80 * time.Millisecond)
	cancel()
	if err := waitFor(done, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestProviderHealthWorkerEnqueueOverflowDefersWithoutSpawningProbe(t *testing.T) {
	store := &providerHealthStore{provider: store.Provider{ID: testProviderID, Kind: "llamarack", Enabled: true, SafeMetadata: store.EmptyObject}}
	service := New(store)
	worker := NewProviderHealthWorker(service, store, nil, nil)
	for range 65 {
		worker.Enqueue(testProviderID)
	}
	time.Sleep(50 * time.Millisecond)
	store.healthUpdatesMu.Lock()
	count := len(store.healthUpdates)
	store.healthUpdatesMu.Unlock()
	if count != 0 {
		t.Fatalf("expected overflow enqueue to defer without spawning probes, updates=%d", count)
	}
}

func TestProviderHealthWorkerProbeAllHandlesListError(t *testing.T) {
	store := &failingListProviderHealthStore{err: errors.New("list failed")}
	worker := NewProviderHealthWorker(New(store), store, nil, nil)
	worker.probeAll(context.Background())
}

func TestProviderHealthWorkerNilReceiverIsSafe(t *testing.T) {
	var worker *ProviderHealthWorker
	if err := worker.Run(context.Background()); err != nil {
		t.Fatalf("err=%v", err)
	}
	worker.Enqueue("id")
}

type failingUpdateProviderHealthStore struct {
	providerHealthStore
	err error
}

func (s *failingUpdateProviderHealthStore) UpdateProviderHealth(context.Context, string, string, *int, *int) error {
	return s.err
}

type failingListProviderHealthStore struct {
	fakeStore
	err error
}

func (s *failingListProviderHealthStore) ListAllProviders(context.Context) ([]store.Provider, error) {
	return nil, s.err
}

func waitFor(done <-chan error, timeout time.Duration) error {
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return errors.New("timed out")
	}
}

var _ executioncontext.SecretResolver = (*fakeProviderModelSecretResolver)(nil)
