package runner

import (
	"context"
	"errors"
	"io"
	"sync"
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

type ProcessSession interface {
	ID() string
	Stdout() io.Reader
	Stderr() io.Reader
	Stdin() io.WriteCloser
	Wait(context.Context) (Result, error)
	Terminate(context.Context) error
	Kill(context.Context) error
}

type waitResult struct {
	result Result
	err    error
}

type Session struct {
	id   string
	conn *Connection

	stdout *outputStream
	stderr *outputStream
	stdin  *stdinWriter

	started chan error
	result  chan waitResult

	mu          sync.Mutex
	startedDone bool
	finished    bool
}

func newSession(id string, conn *Connection) *Session {
	s := &Session{
		id:      id,
		conn:    conn,
		stdout:  newOutputStream(),
		stderr:  newOutputStream(),
		started: make(chan error, 1),
		result:  make(chan waitResult, 1),
	}
	s.stdin = &stdinWriter{session: s}
	return s
}

func (s *Session) ID() string            { return s.id }
func (s *Session) Stdout() io.Reader     { return s.stdout.reader }
func (s *Session) Stderr() io.Reader     { return s.stderr.reader }
func (s *Session) Stdin() io.WriteCloser { return s.stdin }

func (s *Session) Wait(ctx context.Context) (Result, error) {
	select {
	case value := <-s.result:
		return value.result, value.err
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

func (s *Session) Terminate(context.Context) error {
	return s.conn.writeSessionSignal(s.id, false)
}

func (s *Session) Kill(context.Context) error {
	return s.conn.writeSessionSignal(s.id, true)
}

func (s *Session) markStarted(err error) {
	s.mu.Lock()
	if s.startedDone {
		s.mu.Unlock()
		return
	}
	s.startedDone = true
	s.mu.Unlock()
	s.started <- err
}

func (s *Session) finish(result Result, err error) {
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		return
	}
	s.finished = true
	startedDone := s.startedDone
	s.mu.Unlock()
	if !startedDone {
		s.markStarted(err)
	}
	s.stdout.close(err)
	s.stderr.close(err)
	s.result <- waitResult{result: result, err: err}
}

func (s *Session) fail(err error) {
	if err == nil {
		err = ErrDisconnected
	}
	s.finish(Result{}, err)
}

func (c *Connection) writeSessionSignal(sessionID string, force bool) error {
	select {
	case <-c.done:
		return c.connectionError()
	default:
	}
	if force {
		return c.writeMessageKill(sessionID)
	}
	return c.writeMessageTerminate(sessionID)
}

func (c *Connection) writeMessageTerminate(sessionID string) error {
	return c.write("terminate", sessionID, nil)
}

func (c *Connection) writeMessageKill(sessionID string) error {
	return c.write("kill", sessionID, nil)
}

type stdinWriter struct {
	mu      sync.Mutex
	session *Session
	closed  bool
}

func (w *stdinWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, io.ErrClosedPipe
	}
	chunk := append([]byte(nil), p...)
	if err := w.session.conn.write("stdin", w.session.id, struct {
		Data []byte `json:"data"`
	}{Data: chunk}); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *stdinWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	return w.session.conn.write("stdin_close", w.session.id, nil)
}

type outputStream struct {
	reader *io.PipeReader
	writer *io.PipeWriter
	queue  chan []byte
	done   chan struct{}
	once   sync.Once

	mu       sync.Mutex
	closed   bool
	closeErr error
	pushes   sync.WaitGroup
}

func newOutputStream() *outputStream {
	reader, writer := io.Pipe()
	stream := &outputStream{
		reader: reader,
		writer: writer,
		queue:  make(chan []byte, 32),
		done:   make(chan struct{}),
	}
	go stream.run()
	return stream
}

func (s *outputStream) run() {
	failed := false
	for chunk := range s.queue {
		if failed {
			continue
		}
		if _, err := s.writer.Write(chunk); err != nil {
			failed = true
		}
	}

	s.mu.Lock()
	closeErr := s.closeErr
	s.mu.Unlock()
	if closeErr != nil && !errors.Is(closeErr, io.EOF) {
		_ = s.writer.CloseWithError(closeErr)
		return
	}
	_ = s.writer.Close()
}

func (s *outputStream) push(data []byte) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.pushes.Add(1)
	s.mu.Unlock()
	defer s.pushes.Done()

	chunk := append([]byte(nil), data...)
	select {
	case s.queue <- chunk:
	case <-s.done:
	}
}

func (s *outputStream) close(err error) {
	s.once.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.closeErr = err
		close(s.done)
		s.mu.Unlock()

		// No producer can register after closed becomes true. Waiting here lets
		// in-flight producers either enqueue their already-received chunk or
		// observe done before the queue is closed, avoiding send-on-closed races.
		s.pushes.Wait()
		close(s.queue)
	})
}
