package scripted

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/engine"
)

const Name = "scripted"

const cancellationCleanupTimeout = 5 * time.Second

type Engine struct{}

func New() *Engine             { return &Engine{} }
func (e *Engine) Name() string { return Name }

func (e *Engine) Execute(ctx context.Context, request engine.Request) (engine.Result, error) {
	if request.Launcher == nil {
		return engine.Result{}, fmt.Errorf("scripted engine: process launcher is required")
	}
	steps := []engine.ProcessRequest{
		{Kind: "tool", Name: "modify-staged", CWD: "/workspace", Command: []string{"sh", "-lc", `printf 'scripted staged\n' >> staged.txt && git add staged.txt`}},
		{Kind: "tool", Name: "modify-unstaged", CWD: "/workspace", Command: []string{"sh", "-lc", `printf 'scripted unstaged\n' >> unstaged.txt`}},
		{Kind: "tool", Name: "create-untracked", CWD: "/workspace", Command: []string{"sh", "-lc", `printf 'scripted new\n' > new-scripted.txt`}},
		{Kind: "tool", Name: "delete-file", CWD: "/workspace", Command: []string{"sh", "-lc", `rm -f delete.txt`}},
		{Kind: "tool", Name: "rename-file", CWD: "/workspace", Command: []string{"sh", "-lc", `if [ -e rename.txt ]; then git mv rename.txt renamed.txt; fi`}},
		{Kind: "tool", Name: "command-output", CWD: "/workspace", Command: []string{"sh", "-lc", `printf 'scripted stdout\n'; printf 'scripted stderr\n' >&2`}},
		{Kind: "tool", Name: "large-output", CWD: "/workspace", Command: []string{"sh", "-lc", `dd if=/dev/zero bs=1024 count=80 2>/dev/null | tr '\000' x; printf '\n'`}},
		{Kind: "test", Name: "scripted-fixture", CWD: "/workspace", Command: []string{"sh", "-lc", `test -f staged.txt && test -f new-scripted.txt && test ! -e delete.txt && (test -f renamed.txt || test -f rename.txt) && printf 'scripted test passed\n'`}},
	}
	for _, step := range steps {
		if err := executeStep(ctx, request.Launcher, step); err != nil {
			return engine.Result{}, fmt.Errorf("scripted engine: %s: %w", step.Name, err)
		}
	}
	return engine.Result{Summary: "deterministic scripted execution completed"}, nil
}

func executeStep(ctx context.Context, launcher engine.ProcessLauncher, request engine.ProcessRequest) error {
	process, err := launcher.Start(ctx, request)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	drainErrs := make(chan error, 2)
	for _, source := range []io.Reader{process.Stdout(), process.Stderr()} {
		wg.Add(1)
		go func(source io.Reader) {
			defer wg.Done()
			_, err := io.Copy(io.Discard, source)
			drainErrs <- err
		}(source)
	}
	result, waitErr := process.Wait(ctx)
	if waitErr != nil && ctx.Err() != nil {
		releaseCancelledProcess(ctx, process)
	}
	wg.Wait()
	close(drainErrs)
	for drainErr := range drainErrs {
		if drainErr != nil {
			return fmt.Errorf("drain process output: %w", drainErr)
		}
	}
	if waitErr != nil {
		return waitErr
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("process exited with code %d", result.ExitCode)
	}
	return nil
}

func releaseCancelledProcess(ctx context.Context, process engine.Process) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cancellationCleanupTimeout)
	defer cancel()
	_ = process.Terminate(cleanupCtx)
	_ = process.Kill(cleanupCtx)
}
