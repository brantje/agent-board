package opencode

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/engine"
)

type serviceStopProcess struct {
	done            chan struct{}
	waitExited      chan struct{}
	closeOnce       sync.Once
	waitCalls       atomic.Int32
	terminateCalls  atomic.Int32
	killCalls       atomic.Int32
	exitOnTerminate bool
	exitOnKill      bool
	waitErr         error
}

func newServiceStopProcess(exitOnTerminate bool) *serviceStopProcess {
	return &serviceStopProcess{
		done:            make(chan struct{}),
		waitExited:      make(chan struct{}),
		exitOnTerminate: exitOnTerminate,
		exitOnKill:      true,
	}
}

func (p *serviceStopProcess) ID() string            { return "service-stop" }
func (p *serviceStopProcess) Stdout() io.Reader     { return bytes.NewReader(nil) }
func (p *serviceStopProcess) Stderr() io.Reader     { return bytes.NewReader(nil) }
func (p *serviceStopProcess) Stdin() io.WriteCloser { return nopWriteCloser{Writer: io.Discard} }
func (p *serviceStopProcess) Wait(ctx context.Context) (engine.ProcessResult, error) {
	p.waitCalls.Add(1)
	defer close(p.waitExited)
	select {
	case <-p.done:
		if p.waitErr != nil {
			return engine.ProcessResult{}, p.waitErr
		}
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
	if p.exitOnKill {
		p.closeOnce.Do(func() { close(p.done) })
	}
	return nil
}

func TestStopServiceCompletesAfterGracefulTermination(t *testing.T) {
	process := newServiceStopProcess(true)
	if err := stopServiceWithin(context.Background(), process, 100*time.Millisecond, 100*time.Millisecond); err != nil {
		t.Fatalf("stopServiceWithin() error=%v", err)
	}
	if process.waitCalls.Load() != 1 || process.terminateCalls.Load() != 1 || process.killCalls.Load() != 0 {
		t.Fatalf("wait=%d terminate=%d kill=%d", process.waitCalls.Load(), process.terminateCalls.Load(), process.killCalls.Load())
	}
}

func TestStopServiceForceStopsWithoutSecondWait(t *testing.T) {
	process := newServiceStopProcess(false)
	if err := stopServiceWithin(context.Background(), process, 100*time.Millisecond, 100*time.Millisecond); err != nil {
		t.Fatalf("stopServiceWithin() error=%v", err)
	}
	if process.waitCalls.Load() != 1 || process.terminateCalls.Load() != 1 || process.killCalls.Load() != 1 {
		t.Fatalf("wait=%d terminate=%d kill=%d", process.waitCalls.Load(), process.terminateCalls.Load(), process.killCalls.Load())
	}
}

func TestStopServicePropagatesForcedWaitFailure(t *testing.T) {
	waitErr := errors.New("forced wait failed")
	process := newServiceStopProcess(false)
	process.waitErr = waitErr
	if err := stopServiceWithin(context.Background(), process, 10*time.Millisecond, 100*time.Millisecond); !errors.Is(err, waitErr) {
		t.Fatalf("stopServiceWithin() error=%v want wrapped %v", err, waitErr)
	}
	if process.waitCalls.Load() != 1 || process.terminateCalls.Load() != 1 || process.killCalls.Load() != 1 {
		t.Fatalf("wait=%d terminate=%d kill=%d", process.waitCalls.Load(), process.terminateCalls.Load(), process.killCalls.Load())
	}
}

func TestStopServiceFailsBoundedlyWhenProcessIgnoresSignals(t *testing.T) {
	process := newServiceStopProcess(false)
	process.exitOnKill = false
	err := stopServiceWithin(context.Background(), process, 5*time.Millisecond, 20*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stopServiceWithin() error=%v want deadline exceeded", err)
	}
	select {
	case <-process.waitExited:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("process waiter did not exit after bounded stop returned")
	}
	if process.waitCalls.Load() != 1 || process.terminateCalls.Load() != 1 || process.killCalls.Load() != 1 {
		t.Fatalf("wait=%d terminate=%d kill=%d", process.waitCalls.Load(), process.terminateCalls.Load(), process.killCalls.Load())
	}
}

func TestWaitDrainedReturnsBoundedly(t *testing.T) {
	blocked := make(chan struct{})
	started := time.Now()
	waitDrained(blocked, 10*time.Millisecond)
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("waitDrained blocked for %v", elapsed)
	}

	drained := make(chan struct{})
	close(drained)
	started = time.Now()
	waitDrained(drained, time.Second)
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("waitDrained did not return promptly for a drained process: %v", elapsed)
	}
}

var _ engine.Process = (*serviceStopProcess)(nil)
