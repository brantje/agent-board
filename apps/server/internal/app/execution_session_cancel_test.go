package app

import (
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/runner"
)

type escalatingTransport struct {
	id         string
	terminated atomic.Bool
	killed     atomic.Bool
	result     chan struct{}
}

func newEscalatingTransport(id string) *escalatingTransport {
	return &escalatingTransport{id: id, result: make(chan struct{})}
}
func (t *escalatingTransport) ID() string { return t.id }
func (t *escalatingTransport) Stdout() io.Reader { return strings.NewReader("") }
func (t *escalatingTransport) Stderr() io.Reader { return strings.NewReader("") }
func (t *escalatingTransport) Stdin() io.WriteCloser { return nopBuffer{Buffer: nil} }
func (t *escalatingTransport) Wait(ctx context.Context) (runner.Result, error) {
	select {
	case <-t.result:
		return runner.Result{ExitCode: 137, Signaled: true}, nil
	case <-ctx.Done():
		return runner.Result{}, ctx.Err()
	}
}
func (t *escalatingTransport) Terminate(context.Context) error { t.terminated.Store(true); return nil }
func (t *escalatingTransport) Kill(context.Context) error { t.killed.Store(true); close(t.result); return nil }

func TestCancelEscalatesTerminateToKillForExactSession(t *testing.T) {
	service, storeFake, _ := executionServiceFixture(t)
	transport := newEscalatingTransport("session-1")
	client := &fakeExecutionClient{transport: transport, done: make(chan struct{})}
	service.runners = &fakeExecutionManager{client: client}
	process, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", ExecutionRequest{Command: []string{"sleep", "30"}})
	if err != nil { t.Fatal(err) }
	if process.ID() != "session-1" { t.Fatalf("process id=%q", process.ID()) }
	if err := service.Cancel(context.Background(), "project-1", "session-1", 5*time.Millisecond); err != nil {
		t.Fatalf("Cancel() error=%v", err)
	}
	if !transport.terminated.Load() || !transport.killed.Load() {
		t.Fatalf("terminated=%v killed=%v", transport.terminated.Load(), transport.killed.Load())
	}
	if process.Record().Status != "CANCELLED" || storeFake.instance.RunnerStatus != "READY" {
		t.Fatalf("record=%+v instance=%+v", process.Record(), storeFake.instance)
	}
}
