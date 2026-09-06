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

type cancelAfterClaimReporter struct {
	cancel context.CancelFunc

	mu           sync.Mutex
	status       string
	statusCtxErr error
}

func (r *cancelAfterClaimReporter) ClaimRunnerConnection(context.Context, string, string) (int64, error) {
	r.cancel()
	return 1, nil
}

func (r *cancelAfterClaimReporter) SetRunnerStatusGeneration(ctx context.Context, _, _, status string, _ int64) error {
	r.mu.Lock()
	r.status = status
	r.statusCtxErr = ctx.Err()
	r.mu.Unlock()
	return ctx.Err()
}

func (r *cancelAfterClaimReporter) snapshot() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status, r.statusCtxErr
}

func TestManagerReportsReadyWithRecoveryContextAfterCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reporter := &cancelAfterClaimReporter{cancel: cancel}
	client := newFakeManagerClient()
	manager, err := newManager(&fakeEndpointResolver{}, reporter, func(context.Context, string) (Client, error) {
		return client, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	got, err := manager.Connect(ctx, "project-1", "runtime-1")
	if err != nil || got != client {
		t.Fatalf("Connect() client=%v err=%v", got, err)
	}
	status, statusCtxErr := reporter.snapshot()
	if status != "READY" || statusCtxErr != nil {
		t.Fatalf("status=%q report context error=%v", status, statusCtxErr)
	}
}

func TestManagerReportsUnavailableWithRecoveryContextAfterCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reporter := &cancelAfterClaimReporter{cancel: cancel}
	dialErr := errors.New("dial failed")
	manager, err := newManager(&fakeEndpointResolver{}, reporter, func(context.Context, string) (Client, error) {
		return nil, dialErr
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	if _, err := manager.Connect(ctx, "project-1", "runtime-1"); !errors.Is(err, dialErr) {
		t.Fatalf("Connect() error=%v", err)
	}
	status, statusCtxErr := reporter.snapshot()
	if status != "UNAVAILABLE" || statusCtxErr != nil {
		t.Fatalf("status=%q report context error=%v", status, statusCtxErr)
	}
}
