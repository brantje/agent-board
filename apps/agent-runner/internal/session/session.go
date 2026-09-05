package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

type Request struct {
	Command []string
	Dir     string
	Env     map[string]string
	Secrets map[string]string
}

type Result struct {
	ExitCode int
	Signaled bool
}

type Session struct {
	id     string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	done   chan struct{}
	result Result
	err    error
	mu     sync.RWMutex
}

func start(id, workspaceRoot string, request Request) (*Session, error) {
	if len(request.Command) == 0 || request.Command[0] == "" {
		return nil, errors.New("command is required")
	}

	workingDir, err := resolveWorkingDir(workspaceRoot, request.Dir)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(request.Command[0], request.Command[1:]...)
	cmd.Dir = workingDir
	cmd.Env = mergeEnvironment(request.Env, request.Secrets)
	configureProcessTree(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("create stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start process: %w", err)
	}

	s := &Session{
		id: id, cmd: cmd, stdin: newSafeWriteCloser(stdin), stdout: stdout, stderr: stderr,
		done: make(chan struct{}),
	}
	go s.reap()
	return s, nil
}

func (s *Session) ID() string { return s.id }
func (s *Session) Stdin() io.WriteCloser { return s.stdin }
func (s *Session) Stdout() io.Reader { return s.stdout }
func (s *Session) Stderr() io.Reader { return s.stderr }
func (s *Session) Done() <-chan struct{} { return s.done }

func (s *Session) Wait(ctx context.Context) (Result, error) {
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-s.done:
		s.mu.RLock()
		defer s.mu.RUnlock()
		return s.result, s.err
	}
}

func (s *Session) Terminate() error {
	return s.signal(terminateProcessTree)
}

func (s *Session) Kill() error {
	return s.signal(killProcessTree)
}

func (s *Session) signal(signalTree func(int) error) error {
	s.mu.RLock()
	done := isClosed(s.done)
	pid := s.cmd.Process.Pid
	s.mu.RUnlock()
	if done {
		return nil
	}
	return signalTree(pid)
}

func (s *Session) reap() {
	err := s.cmd.Wait()
	result := Result{}
	if state := s.cmd.ProcessState; state != nil {
		result.ExitCode = state.ExitCode()
		if status, ok := state.Sys().(syscall.WaitStatus); ok {
			result.Signaled = status.Signaled()
		}
	}

	var waitErr error
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		waitErr = fmt.Errorf("wait for process: %w", err)
	}

	s.mu.Lock()
	s.result = result
	s.err = waitErr
	close(s.done)
	s.mu.Unlock()
}

func resolveWorkingDir(workspaceRoot, requested string) (string, error) {
	root, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}

	candidate := requested
	if candidate == "" {
		candidate = root
	} else if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	candidate, err = filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}

	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return "", fmt.Errorf("compare working directory to workspace: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("working directory escapes workspace")
	}
	info, err := os.Stat(candidate)
	if err != nil {
		return "", fmt.Errorf("stat working directory: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("working directory is not a directory")
	}
	return candidate, nil
}

func mergeEnvironment(env, secrets map[string]string) []string {
	values := make(map[string]string)
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	for key, value := range env {
		values[key] = value
	}
	for key, value := range secrets {
		values[key] = value
	}

	result := make([]string, 0, len(values))
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	return result
}

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
