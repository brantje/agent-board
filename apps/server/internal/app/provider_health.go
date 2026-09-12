package app

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

const (
	defaultProviderHealthInterval = 5 * time.Minute
	defaultProviderHealthWorkers  = 3
)

type providerHealthProbeStore interface {
	ListAllProviders(context.Context) ([]store.Provider, error)
	UpdateProviderHealth(context.Context, string, string, *int, *int) error
}

// ProviderHealthWorker probes Provider model discovery in the background and persists health_status.
type ProviderHealthWorker struct {
	service    *Service
	store      providerHealthProbeStore
	resolver   executioncontext.SecretResolver
	interval   time.Duration
	concurrency int
	client     *http.Client
	enqueue    chan string
}

func NewProviderHealthWorker(service *Service, store providerHealthProbeStore, resolver executioncontext.SecretResolver, client *http.Client) *ProviderHealthWorker {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &ProviderHealthWorker{
		service:     service,
		store:       store,
		resolver:    resolver,
		interval:    defaultProviderHealthInterval,
		concurrency: defaultProviderHealthWorkers,
		client:      client,
		enqueue:     make(chan string, 64),
	}
}

func (s *Service) SetProviderHealthWorker(worker *ProviderHealthWorker) {
	if s != nil {
		s.providerHealth = worker
	}
}

func (s *Service) EnqueueProviderHealthProbe(providerID string) {
	if s == nil || s.providerHealth == nil || providerID == "" {
		return
	}
	s.providerHealth.Enqueue(providerID)
}

func (w *ProviderHealthWorker) Enqueue(providerID string) {
	if w == nil || providerID == "" {
		return
	}
	select {
	case w.enqueue <- providerID:
	default:
		slog.Debug("provider health: probe queue full, deferring to periodic probe", "providerId", providerID)
	}
}

func (w *ProviderHealthWorker) Run(ctx context.Context) error {
	if w == nil {
		return nil
	}
	w.probeAll(ctx)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case id := <-w.enqueue:
			w.probeByID(ctx, id)
		case <-ticker.C:
			w.probeAll(ctx)
		}
	}
}

func (w *ProviderHealthWorker) probeAll(ctx context.Context) {
	providers, err := w.store.ListAllProviders(ctx)
	if err != nil {
		slog.Error("provider health: list providers", "error", err)
		return
	}
	sem := make(chan struct{}, w.concurrency)
	var wg sync.WaitGroup
	for _, provider := range providers {
		wg.Add(1)
		go func(provider store.Provider) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			w.probeProvider(ctx, provider)
		}(provider)
	}
	wg.Wait()
}

func (w *ProviderHealthWorker) probeByID(ctx context.Context, providerID string) {
	providers, err := w.store.ListAllProviders(ctx)
	if err != nil {
		slog.Error("provider health: list providers for enqueue", "error", err, "providerId", providerID)
		return
	}
	for _, provider := range providers {
		if provider.ID == providerID {
			w.probeProvider(ctx, provider)
			return
		}
	}
}

func (w *ProviderHealthWorker) probeProvider(ctx context.Context, provider store.Provider) {
	if w.service == nil {
		return
	}
	_, err := w.service.ListProviderModels(ctx, provider.ProjectID, provider.ID, w.resolver, w.client)
	if err != nil {
		slog.Debug("provider health probe failed", "providerId", provider.ID, "error", err)
	}
}
