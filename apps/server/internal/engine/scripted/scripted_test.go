package scripted

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"testing"

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
