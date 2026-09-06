package app

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type cancelRaceStore struct {
	*branchExecutionStore
	terminal chan struct{}
	once     sync.Once
}

func (s *cancelRaceStore) TransitionExecutionSession(ctx context.Context, transition store.ExecutionSessionTransition) (store.ExecutionSession, error) {
	updated, err := s.branchExecutionStore.TransitionExecutionSession(ctx, transition)
	if err == nil && (transition.Status == "COMPLETED" || transition.Status == "CANCELLED") {
		s.once.Do(func() { close(s.terminal) })
	}
	return updated, err
}

type cancelRaceTransport struct {
	id       string
	resultCh chan struct{}
	terminal <-chan struct{}
	once     sync.Once
}

func (t *cancelRaceTransport) ID() string            { return t.id }
func (t *cancelRaceTransport) Stdout() io.Reader     { return strings.NewReader("") }
func (t *cancelRaceTransport) Stderr() io.Reader     { return strings.NewReader("") }
func (t *cancelRaceTransport) Stdin() io.WriteCloser { return nopBuffer{} }
func (t *cancelRaceTransport) Wait(ctx context.Context) (runner.Result, error) {
	select {
	case <-t.resultCh:
		return runner.Result{ExitCode: 143, Signaled: true}, nil
	case <-ctx.Done():
		return runner.Result{}, ctx.Err()
	}
}
func (t *cancelRaceTransport) Terminate(context.Context) error {
	t.once.Do(func() { close(t.resultCh) })
	<-t.terminal
	return nil
}
func (t *cancelRaceTransport) Kill(context.Context) error { return t.Terminate(context.Background()) }

func TestTerminateIntentPrecedesSynchronousExit(t *testing.T) {
	base := newBranchExecutionStore()
	storeFake := &cancelRaceStore{branchExecutionStore: base, terminal: make(chan struct{})}
	transport := &cancelRaceTransport{id: "session-1", resultCh: make(chan struct{}), terminal: storeFake.terminal}
	service := newBranchExecutionService(t, storeFake, transport, nil)
	process, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", ExecutionRequest{Command: []string{"sleep", "30"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Terminate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := process.Wait(context.Background()); err != nil || process.Record().Status != "CANCELLED" {
		t.Fatalf("record=%+v err=%v", process.Record(), err)
	}
}
