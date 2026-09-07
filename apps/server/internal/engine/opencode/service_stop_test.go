package opencode

import (
	"bytes"
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/engine"
)

type serviceStopProcess struct {
	done             chan struct{}
	closeOnce        sync.Once
	waitCalls        atomic.Int32
	terminateCalls   atomic.Int32
	killCalls        atomic.Int32
	exitOnTerminate  bool
}

func newServiceStopProcess(exitOnTerminate bool) *serviceStopProcess {
	return &serviceStopProcess{done: make(chan struct{}), exitOnTerminate: exitOnTerminate}
}

func (p *serviceStopProcess) ID() string            { return "service-stop" }
func (p *serviceStopProcess) Stdout() io.Reader     { return bytes.NewReader(nil) }
func (p *serviceStopProcess) Stderr() io.Reader     { return bytes.NewReader(nil) }
func (p *serviceStopProcess) Stdin() io.WriteCloser { return nopWriteCloser{Writer: io.Discard} }
func (p *serviceStopProcess) Wait(ctx context.Context) (engine.ProcessResult, error) {
	p.waitCalls.Add(1)
	select {
	case <-p.done:
		return engine.ProcessResult{ExitCode: -1}, nil
	case <-ctx.Done():
		return engine.ProcessResult{}, ctx.Err()
	}
}
func (p *serviceStopProcess) Terminate(context.Context) error {
	p.terminateCalls.Add(1)
	if p.exitOnTerminate {
		p.closeOnce.Do(func() { close(p.done) })
	}
	return nil
}
func (p *serviceStopProcess) Kill(context.Context) error {
	p.killCalls.Add(1)
	p.closeOnce.Do(func() { close(p.done) })
	return nil
}

func TestStopServiceCompletesAfterGracefulTermination(t *testing.T) {
	process := newServiceStopProcess(true)
	if err := stopServiceWithin(context.Background(), process, 10*time.Millisecond, 100*time.Millisecond); err != nil {
		t.Fatalf("stopServiceWithin() error=%v", err)
	}
	if process.waitCalls.Load() != 1 || process.terminateCalls.Load() != 1 || process.killCalls.Load() != 0 {
		t.Fatalf("wait=%d terminate=%d kill=%d", process.waitCalls.Load(), process.terminateCalls.Load(), process.killCalls.Load())
	}
}

func TestStopServiceForceStopsWithoutSecondWait(t *testing.T) {
	process := newServiceStopProcess(false)
	if err := stopServiceWithin(context.Background(), process, 10*time.Millisecond, 100*time.Millisecond); err != nil {
		t.Fatalf("stopServiceWithin() error=%v", err)
	}
	if process.waitCalls.Load() != 1 || process.terminateCalls.Load() != 1 || process.killCalls.Load() != 1 {
		t.Fatalf("wait=%d terminate=%d kill=%d", process.waitCalls.Load(), process.terminateCalls.Load(), process.killCalls.Load())
	}
}

var _ engine.Process = (*serviceStopProcess)(nil)
