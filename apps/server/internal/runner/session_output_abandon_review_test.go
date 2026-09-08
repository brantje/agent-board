package runner

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestAbandonOutputRejectsNilAndUnsupportedReaders(t *testing.T) {
	if err := AbandonStdout(nil); !errors.Is(err, ErrOutputAbandonUnsupported) {
		t.Fatalf("AbandonStdout(nil) error=%v", err)
	}
	if err := AbandonStderr(nil); !errors.Is(err, ErrOutputAbandonUnsupported) {
		t.Fatalf("AbandonStderr(nil) error=%v", err)
	}

	session := &outputFallbackSession{
		stdout: strings.NewReader("stdout"),
		stderr: strings.NewReader("stderr"),
	}
	if err := AbandonStdout(session); !errors.Is(err, ErrOutputAbandonUnsupported) {
		t.Fatalf("AbandonStdout(non-closer) error=%v", err)
	}
	if err := AbandonStderr(session); !errors.Is(err, ErrOutputAbandonUnsupported) {
		t.Fatalf("AbandonStderr(non-closer) error=%v", err)
	}
}

func TestAbandonOutputUsesOptionalCapability(t *testing.T) {
	stdoutErr := errors.New("stdout abandon")
	stderrErr := errors.New("stderr abandon")
	session := &outputAbandonSession{stdoutErr: stdoutErr, stderrErr: stderrErr}

	if err := AbandonStdout(session); !errors.Is(err, stdoutErr) {
		t.Fatalf("AbandonStdout() error=%v", err)
	}
	if err := AbandonStderr(session); !errors.Is(err, stderrErr) {
		t.Fatalf("AbandonStderr() error=%v", err)
	}
	if session.stdoutCalls != 1 || session.stderrCalls != 1 {
		t.Fatalf("abandon calls stdout=%d stderr=%d", session.stdoutCalls, session.stderrCalls)
	}
}

func TestAbandonOutputFallsBackToReaderClose(t *testing.T) {
	closeErr := errors.New("close output")
	stdout := &trackingReadCloser{Reader: strings.NewReader("stdout"), err: closeErr}
	stderr := &trackingReadCloser{Reader: strings.NewReader("stderr")}
	session := &outputFallbackSession{stdout: stdout, stderr: stderr}

	if err := AbandonStdout(session); !errors.Is(err, closeErr) {
		t.Fatalf("AbandonStdout() error=%v", err)
	}
	if err := AbandonStderr(session); err != nil {
		t.Fatalf("AbandonStderr() error=%v", err)
	}
	if !stdout.closed || !stderr.closed {
		t.Fatalf("reader close state stdout=%v stderr=%v", stdout.closed, stderr.closed)
	}
}

func TestSessionAbandonOutputClosesConcreteReaders(t *testing.T) {
	session := newSession("session-1", nil)
	t.Cleanup(func() {
		session.stdout.close(io.EOF)
		session.stderr.close(io.EOF)
	})

	if err := session.AbandonStdout(); err != nil {
		t.Fatalf("AbandonStdout() error=%v", err)
	}
	if err := session.AbandonStderr(); err != nil {
		t.Fatalf("AbandonStderr() error=%v", err)
	}
}

type outputFallbackSession struct {
	stdout io.Reader
	stderr io.Reader
}

func (s *outputFallbackSession) ID() string                    { return "fallback" }
func (s *outputFallbackSession) Stdout() io.Reader             { return s.stdout }
func (s *outputFallbackSession) Stderr() io.Reader             { return s.stderr }
func (s *outputFallbackSession) Stdin() io.WriteCloser         { return nil }
func (s *outputFallbackSession) Wait(context.Context) (Result, error) {
	return Result{}, nil
}
func (s *outputFallbackSession) Terminate(context.Context) error { return nil }
func (s *outputFallbackSession) Kill(context.Context) error      { return nil }

type outputAbandonSession struct {
	outputFallbackSession
	stdoutErr   error
	stderrErr   error
	stdoutCalls int
	stderrCalls int
}

func (s *outputAbandonSession) AbandonStdout() error {
	s.stdoutCalls++
	return s.stdoutErr
}

func (s *outputAbandonSession) AbandonStderr() error {
	s.stderrCalls++
	return s.stderrErr
}

type trackingReadCloser struct {
	io.Reader
	err    error
	closed bool
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return r.err
}
