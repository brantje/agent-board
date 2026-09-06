package scripted

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/engine"
)

type fakeLauncher struct{ requests []engine.ProcessRequest }

func (l *fakeLauncher) Start(_ context.Context, request engine.ProcessRequest) (engine.Process, error) {
	l.requests = append(l.requests, request)
	return &fakeProcess{stdout: bytes.NewBufferString("stdout"), stderr: bytes.NewBufferString("stderr")}, nil
}

type fakeProcess struct{ stdout, stderr *bytes.Buffer }

func (p *fakeProcess) ID() string            { return "session" }
func (p *fakeProcess) Stdout() io.Reader     { return p.stdout }
func (p *fakeProcess) Stderr() io.Reader     { return p.stderr }
func (p *fakeProcess) Stdin() io.WriteCloser { return nopWriteCloser{io.Discard} }
func (p *fakeProcess) Wait(context.Context) (engine.ProcessResult, error) {
	return engine.ProcessResult{ExitCode: 0}, nil
}
func (p *fakeProcess) Terminate(context.Context) error { return nil }
func (p *fakeProcess) Kill(context.Context) error      { return nil }

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func TestScriptedEngineUsesDeterministicExecutionSequence(t *testing.T) {
	launcher := &fakeLauncher{}
	result, err := New().Execute(context.Background(), engine.Request{Launcher: launcher})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary == "" {
		t.Fatal("expected summary")
	}
	got := make([]string, 0, len(launcher.requests))
	for _, request := range launcher.requests {
		got = append(got, request.Name)
	}
	want := []string{"modify-staged", "modify-unstaged", "create-untracked", "delete-file", "rename-file", "command-output", "large-output", "scripted-fixture"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if launcher.requests[len(launcher.requests)-1].Kind != "test" {
		t.Fatalf("last step is not a test: %+v", launcher.requests[len(launcher.requests)-1])
	}
}

type cancellationLauncher struct{ process *cancellationProcess }

func (l cancellationLauncher) Start(context.Context, engine.ProcessRequest) (engine.Process, error) {
	return l.process, nil
}

type cancellationProcess struct {
	stdoutR *io.PipeReader
	stdoutW *io.PipeWriter
	stderrR *io.PipeReader
	stderrW *io.PipeWriter
	once    sync.Once
	stopped chan struct{}
}

func newCancellationProcess() *cancellationProcess {
	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()
	return &cancellationProcess{stdoutR: stdoutR, stdoutW: stdoutW, stderrR: stderrR, stderrW: stderrW, stopped: make(chan struct{})}
}

func (p *cancellationProcess) ID() string            { return "cancel-session" }
func (p *cancellationProcess) Stdout() io.Reader     { return p.stdoutR }
func (p *cancellationProcess) Stderr() io.Reader     { return p.stderrR }
func (p *cancellationProcess) Stdin() io.WriteCloser { return nopWriteCloser{io.Discard} }
func (p *cancellationProcess) Wait(ctx context.Context) (engine.ProcessResult, error) {
	<-ctx.Done()
	return engine.ProcessResult{}, ctx.Err()
}
func (p *cancellationProcess) Terminate(context.Context) error { p.release(); return nil }
func (p *cancellationProcess) Kill(context.Context) error      { p.release(); return nil }
func (p *cancellationProcess) release() {
	p.once.Do(func() {
		_ = p.stdoutW.Close()
		_ = p.stderrW.Close()
		close(p.stopped)
	})
}

func TestExecuteStepReleasesOutputOnCancellation(t *testing.T) {
	process := newCancellationProcess()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() {
		done <- executeStep(ctx, cancellationLauncher{process: process}, engine.ProcessRequest{Name: "cancelled"})
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("executeStep() error=%v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("executeStep blocked while joining output drains after cancellation")
	}
	select {
	case <-process.stopped:
	default:
		t.Fatal("cancelled process was not terminated")
	}
}
