package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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

func start(id string, request Request) (*Session, error) {
	if len(request.Command) == 0 || request.Command[0] == "" {
		return nil, errors.New("command is required")
	}

	cmd := exec.Command(request.Command[0], request.Command[1:]...)
	cmd.Dir = request.Dir
	cmd.Env = mergeEnvironment(request.Env, request.Secrets)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

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
		id: id, cmd: cmd, stdin: stdin, stdout: stdout, stderr: stderr,
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
	return s.signal(syscall.SIGTERM)
}

func (s *Session) Kill() error {
	return s.signal(syscall.SIGKILL)
}

func (s *Session) signal(sig syscall.Signal) error {
	s.mu.RLock()
	done := isClosed(s.done)
	pid := s.cmd.Process.Pid
	s.mu.RUnlock()
	if done {
		return nil
	}
	if err := syscall.Kill(-pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("signal process tree: %w", err)
	}
	return nil
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

func mergeEnvironment(env, secrets map[string]string) []string {
	values := make(map[string]string)
	for _, item := range os.Environ() {
		for i := 0; i < len(item); i++ {
			if item[i] == '=' {
				values[item[:i]] = item[i+1:]
				break
			}
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
