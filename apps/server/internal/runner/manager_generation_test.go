package runner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type generationStatusReporter struct {
	mu         sync.Mutex
	generation int64
	status     string

	staleStarted chan struct{}
	releaseStale chan struct{}
	staleOnce    sync.Once
}

func (r *generationStatusReporter) ClaimRunnerConnection(context.Context, string, string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.generation++
	r.status = "CONNECTING"
	return r.generation, nil
}

func (r *generationStatusReporter) SetRunnerStatusGeneration(_ context.Context, _, _, status string, generation int64) error {
	if generation == 1 && status == "UNAVAILABLE" && r.staleStarted != nil {
		r.staleOnce.Do(func() { close(r.staleStarted) })
		<-r.releaseStale
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if generation != r.generation {
		return errors.New("stale runner generation")
	}
	r.status = status
	return nil
}

func (r *generationStatusReporter) snapshot() (int64, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.generation, r.status
}

func TestManagerStaleWatcherCannotOverwriteReplacementStatus(t *testing.T) {
	resolver := &fakeEndpointResolver{}
	first, second := newFakeManagerClient(), newFakeManagerClient()
	clients := []Client{first, second}
	var dialMu sync.Mutex
	dialIndex := 0
	reporter := &generationStatusReporter{
		staleStarted: make(chan struct{}),
		releaseStale: make(chan struct{}),
	}
	manager, err := newManager(resolver, reporter, func(context.Context, string) (Client, error) {
		dialMu.Lock()
		defer dialMu.Unlock()
		client := clients[dialIndex]
		dialIndex++
		return client, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	got, err := manager.Connect(context.Background(), "project-1", "runtime-1")
	if err != nil || got != first {
		t.Fatalf("first Connect() client=%v err=%v", got, err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first client: %v", err)
	}
	select {
	case <-reporter.staleStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("old watcher did not reach UNAVAILABLE report")
	}

	got, err = manager.Connect(context.Background(), "project-1", "runtime-1")
	if err != nil || got != second {
		t.Fatalf("replacement Connect() client=%v err=%v", got, err)
	}
	generation, status := reporter.snapshot()
	if generation != 2 || status != "READY" {
		t.Fatalf("replacement state generation=%d status=%q", generation, status)
	}

	close(reporter.releaseStale)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		generation, status = reporter.snapshot()
		if generation == 2 && status == "READY" {
			time.Sleep(10 * time.Millisecond)
			generation, status = reporter.snapshot()
			break
		}
		time.Sleep(time.Millisecond)
	}
	if generation != 2 || status != "READY" {
		t.Fatalf("stale watcher overwrote replacement: generation=%d status=%q", generation, status)
	}
}
